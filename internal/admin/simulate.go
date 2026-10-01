package admin

// Simulation: play the unsaved draft of an adventure, one action at a
// time, with the same rules a real event runs.
//
//	POST /api/admin/adventures/{slug}/simulate
//
// The server keeps nothing. The client sends the draft graph, the team's
// state from the previous call, and one action; it gets back the new
// state and what the player would see. Every rule is applied by
// internal/graph/session — the package the live engine also calls — so
// what the designer shows cannot drift from what players get.
//
// Two actions exist only here, because a real team cannot do them: wait
// moves the clock forward so hint delays and time limits can be tried
// without waiting, and move puts the team in a zone (or nowhere) without
// walking there.

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graph/walk"
	"github.com/klahr/zonventure/internal/httpx"
)

// maxSimulateWait caps a single clock jump, so a typo cannot push a
// simulated team a century past every deadline.
const maxSimulateWait = 24 * 60 * 60

type simulateAction struct {
	// Kind is start, place, begin, answer, continue, choose, skip, timeout,
	// hint, back, timeup, language, pick, force, wait or move.
	Kind  string `json:"kind"`
	Node  string `json:"node,omitempty"`
	Guess string `json:"guess,omitempty"`
	Port  string `json:"port,omitempty"`
	Index int    `json:"index,omitempty"`
	// Seconds is the clock jump for wait.
	Seconds int `json:"seconds,omitempty"`
	// Zone is where move puts the team; empty means nowhere.
	Zone string `json:"zone,omitempty"`
	// Lang is the language for the language action.
	Lang string `json:"lang,omitempty"`
	// Block and Choice are the pick action's: a quiz question and the answer.
	Block  string `json:"block,omitempty"`
	Choice string `json:"choice,omitempty"`
	// Lat and Lng put the team at a point instead, as picked on a map.
	Lat *float64 `json:"lat,omitempty"`
	Lng *float64 `json:"lng,omitempty"`
}

type simulateRequest struct {
	Envelope json.RawMessage `json:"envelope"`
	Team     *session.Team   `json:"team,omitempty"`
	Action   simulateAction  `json:"action"`
}

type simulateRefusal struct {
	// Code is session.Code for a player-facing refusal, or "graph" when
	// the draft itself stopped the walk — an unconnected output, a hub
	// with nowhere to go. The latter is the author's problem, and the
	// message says which.
	Code    string `json:"code"`
	Message string `json:"message"`
}

type simulateResponse struct {
	Team   *session.Team    `json:"team"`
	Result session.Result   `json:"result"`
	Passed []string         `json:"passed,omitempty"`
	Refuse *simulateRefusal `json:"refusal,omitempty"`
}

// SimulateAdventureGraph runs one simulated action.
func (h *Handlers) SimulateAdventureGraph(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.graphTarget(w, r); !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxGraphBodyBytes))
	if err != nil {
		httpx.JSON(w, http.StatusRequestEntityTooLarge, graphRefusal{Error: "request is too large", Code: "graph_too_large"})
		return
	}
	var req simulateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.JSON(w, http.StatusBadRequest, graphRefusal{Error: err.Error(), Code: "simulate_malformed"})
		return
	}
	env, err := graph.ParseEnvelope(req.Envelope)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, graphRefusal{Error: err.Error(), Code: "graph_malformed"})
		return
	}

	resp, status, msg := simulate(&env.Graph, req.Team, req.Action, time.Now().UTC())
	if status != http.StatusOK {
		httpx.JSON(w, status, graphRefusal{Error: msg, Code: "simulate_malformed"})
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// simulate applies one action. It is separate from the handler so the
// rules of the simulation — not the HTTP around it — can be tested.
func simulate(g *graph.Graph, team *session.Team, a simulateAction, now time.Time) (simulateResponse, int, string) {
	var (
		res  session.Result
		step *walk.Step
		err  error
	)

	switch a.Kind {
	case "start":
		var st walk.Step
		team, st, err = session.Start(g, now)
		if err != nil {
			return refused(nil, err), http.StatusOK, ""
		}
		res.View = team.View()
		return simulateResponse{Team: team, Result: res, Passed: st.Passed}, http.StatusOK, ""
	case "place":
		team, err = session.Place(g, a.Node, now)
		if err != nil {
			return simulateResponse{}, http.StatusBadRequest, err.Error()
		}
		res.View = team.View()
		return simulateResponse{Team: team, Result: res}, http.StatusOK, ""
	}

	if team == nil {
		return simulateResponse{}, http.StatusBadRequest, "this action needs the team from a previous start or place"
	}
	team.G = g
	team.Init()

	// An action that fails part-way may already have changed the team —
	// a right answer marks the puzzle solved before the walk finds its
	// output unconnected. The live engine rolls that back with the
	// transaction; the simulator keeps a copy to do the same.
	before, err := cloneTeam(team)
	if err != nil {
		return simulateResponse{}, http.StatusInternalServerError, "copying team failed"
	}

	// The team's own actions go through Act, which applies a time limit
	// that has passed before anything else, as the live engine does.
	act := func(action func() (session.Result, *walk.Step, error)) {
		res, step, err = team.Act(action)
	}
	switch a.Kind {
	case "begin":
		act(func() (session.Result, *walk.Step, error) { return team.ChooseStart(a.Port) })
	case "answer":
		act(func() (session.Result, *walk.Step, error) { return team.Answer(a.Node, a.Guess) })
	case "continue":
		act(func() (session.Result, *walk.Step, error) { return team.Continue(a.Node) })
	case "back":
		act(func() (session.Result, *walk.Step, error) { return team.Back(a.Node) })
	case "timeup":
		act(func() (session.Result, *walk.Step, error) { return team.TimeUp() })
	case "language":
		act(func() (session.Result, *walk.Step, error) { return session.Result{}, nil, team.SetLanguage(a.Lang) })
	case "choose":
		act(func() (session.Result, *walk.Step, error) { return team.Choose(a.Node, a.Port) })
	case "skip":
		act(func() (session.Result, *walk.Step, error) { return team.Skip(a.Node) })
	case "leave":
		act(func() (session.Result, *walk.Step, error) { return team.Leave(a.Node) })
	case "turn":
		// The simulated team is all its players: every key turns at once.
		act(func() (session.Result, *walk.Step, error) { return team.Turn(a.Node) })
	case "timeout":
		act(func() (session.Result, *walk.Step, error) { return team.Timeout(a.Node) })
	case "force":
		act(func() (session.Result, *walk.Step, error) { return team.ForceSolve(a.Node, a.Port) })
	case "hint":
		act(func() (session.Result, *walk.Step, error) { return team.RevealHint(a.Node, a.Index) })
	case "pick":
		act(func() (session.Result, *walk.Step, error) { return team.Pick(a.Node, a.Block, a.Choice) })
	case "wait":
		if a.Seconds <= 0 || a.Seconds > maxSimulateWait {
			return simulateResponse{}, http.StatusBadRequest, "seconds must be between 1 and 86400"
		}
		team.Now = team.Now.Add(time.Duration(a.Seconds) * time.Second)
		// The simulated clock has no countdown on a phone to call time
		// up; moving it past the limit applies the limit there and then.
		if team.TimeUpDue() {
			res, step, err = team.TimeUp()
		}
	case "move":
		from := team.Position
		team.Position = nil
		if a.Lat != nil && a.Lng != nil {
			if *a.Lat < -90 || *a.Lat > 90 || *a.Lng < -180 || *a.Lng > 180 {
				return simulateResponse{}, http.StatusBadRequest, "position is off the globe"
			}
			team.Position = &session.Position{Lat: *a.Lat, Lng: *a.Lng}
		} else if a.Zone != "" {
			found := false
			for _, z := range g.Zones {
				if z.Slug == a.Zone {
					team.Position = &session.Position{Lat: z.Lat, Lng: z.Lng}
					found = true
				}
			}
			if !found {
				return simulateResponse{}, http.StatusBadRequest, "no zone " + a.Zone
			}
		}
		// The simulated team walks straight from where it was.
		if from != nil && team.Position != nil {
			team.WalkedMetres += session.HaversineMeters(from.Lat, from.Lng, team.Position.Lat, team.Position.Lng)
		}
	default:
		return simulateResponse{}, http.StatusBadRequest, "unknown action " + a.Kind
	}

	if err != nil {
		out := refused(before, err)
		out.Result = res
		out.Result.View = before.View()
		return out, http.StatusOK, ""
	}
	var passed []string
	if step != nil {
		team.Apply(*step)
		passed = step.Passed
	}
	res.View = team.View()
	return simulateResponse{Team: team, Result: res, Passed: passed}, http.StatusOK, ""
}

func cloneTeam(t *session.Team) (*session.Team, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	var c session.Team
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	c.G = t.G
	c.Init()
	return &c, nil
}

func refused(team *session.Team, err error) simulateResponse {
	code := session.Code(err)
	if code == "" {
		// Not a refusal a player could meet: the walk stopped on the
		// draft itself. The wrapped text names the node and port.
		code = "graph"
	}
	return simulateResponse{Team: team, Refuse: &simulateRefusal{Code: code, Message: err.Error()}}
}
