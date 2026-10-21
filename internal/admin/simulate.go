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
//
// Without a crew the team plays as a whole, every player at once. With
// one it is that crew's phones: each action is one player's, move moves
// that player, and the reply has every phone's screen, for steps where
// players see and do different things — roles, a saboteur, a mission.

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"math/rand/v2"
	"net/http"
	"slices"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graph/walk"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/play"
)

// maxCrew is the most phones a simulated team may have.
const maxCrew = 6

// maxSimulateWait caps a single clock jump, so a typo cannot push a
// simulated team a century past every deadline.
const maxSimulateWait = 24 * 60 * 60

type simulateAction struct {
	// Kind is start, place, begin, answer, continue, choose, skip, timeout,
	// hint, back, timeup, language, pick, force, walk, wait or move.
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

// simCrew is a simulated team's phones. The server keeps nothing: like
// the team, the client sends it with each action and gets it back.
type simCrew struct {
	Players []simPlayer `json:"players"`
	// As is the slot of the player the action is from; 0 is the first.
	As int64 `json:"as,omitempty"`
	// Roles are public roles the author gave to a player; the rest are
	// dealt as a live team deals them.
	Roles map[string]int64 `json:"roles,omitempty"`
	// Secrets are who holds each secret role, 0 for no one. One missing
	// is dealt at random once there are players enough to hide among.
	Secrets map[string]int64 `json:"secrets,omitempty"`
	// Seals are the picks at missions, and Presses each player's part in
	// a step done together, until the team leaves it.
	Seals   map[string]map[int64]string `json:"seals,omitempty"`
	Presses map[int64]session.Press     `json:"presses,omitempty"`
	// PressesAt are the presses by step while the team is split up: each
	// group has a step of its own, done together by its players.
	PressesAt map[string]map[int64]session.Press `json:"presses_at,omitempty"`
}

// simPlayer is one phone: its slot, a name to show, and where it is.
type simPlayer struct {
	Slot     int64             `json:"slot"`
	Name     string            `json:"name,omitempty"`
	Position *session.Position `json:"position,omitempty"`
}

// simPhone is what one of the crew's phones shows.
type simPhone struct {
	Slot  int64        `json:"slot"`
	Roles []string     `json:"roles,omitempty"`
	View  session.View `json:"view"`
}

type simulateRequest struct {
	Envelope json.RawMessage `json:"envelope"`
	Team     *session.Team   `json:"team,omitempty"`
	Crew     *simCrew        `json:"crew,omitempty"`
	Action   simulateAction  `json:"action"`
	// AnyPlace lets the team begin wherever it stands, as when a draft is
	// tried away from the site: the adventure's starting point, if it
	// requires one, is not asked for.
	AnyPlace bool `json:"any_place,omitempty"`
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
	// Crew and Phones are a crew's, when the request had one: Result's
	// view is then the acting player's.
	Crew   *simCrew   `json:"crew,omitempty"`
	Phones []simPhone `json:"phones,omitempty"`
}

// SimulateAdventureGraph runs one simulated action.
func (h *Handlers) SimulateAdventureGraph(w http.ResponseWriter, r *http.Request) {
	advID, ok := h.graphTarget(w, r)
	if !ok {
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

	place, err := startPlaceOf(r.Context(), h.DB, advID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading the starting point failed", err)
		return
	}
	if req.AnyPlace {
		place = nil
	}
	resp, status, msg := simulateCrew(&env.Graph, place, req.Team, req.Crew, req.Action, time.Now().UTC())
	if status != http.StatusOK {
		httpx.JSON(w, status, graphRefusal{Error: msg, Code: "simulate_malformed"})
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// simulate applies one action. It is separate from the handler so the
// rules of the simulation — not the HTTP around it — can be tested.
// place is the adventure's required starting point, if any: like a live
// event, the simulated team waits at the start screen until it is there.
func simulate(g *graph.Graph, place *graph.Zone, team *session.Team, a simulateAction, now time.Time) (simulateResponse, int, string) {
	return simulateCrew(g, place, team, nil, a, now)
}

// simulateCrew is simulate as one of a crew's phones, or as the team as
// a whole when crew is nil.
func simulateCrew(g *graph.Graph, place *graph.Zone, team *session.Team, crew *simCrew, a simulateAction, now time.Time) (simulateResponse, int, string) {
	if crew != nil {
		if msg := crew.check(); msg != "" {
			return simulateResponse{}, http.StatusBadRequest, msg
		}
	}
	resp, status, msg := simulateAs(g, place, team, crew, a, now)
	if status != http.StatusOK || crew == nil || resp.Team == nil {
		return resp, status, msg
	}
	crew.deal(g)
	resp.Crew = crew
	resp.Phones = crew.phones(resp.Team)
	for _, ph := range resp.Phones {
		if ph.Slot == crew.As {
			resp.Result.View = ph.View
		}
	}
	return resp, status, msg
}

func simulateAs(g *graph.Graph, place *graph.Zone, team *session.Team, crew *simCrew, a simulateAction, now time.Time) (simulateResponse, int, string) {
	var (
		res  session.Result
		step *walk.Step
		err  error
	)

	switch a.Kind {
	case "start":
		var st walk.Step
		team, st, err = session.StartAt(g, now, place)
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
	// The starting point is the adventure's, not the team's: it is not in
	// the state the client sends back, so it is set again on every action.
	team.StartPlace = place
	team.Init()
	var crewBefore *simCrew
	if crew != nil {
		crew.deal(g)
		crewBefore = crew.clone()
		crew.seat(team)
	}

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
		act(func() (session.Result, *walk.Step, error) { return team.ChooseStart(a.Port, "") })
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
	case "seal":
		act(func() (session.Result, *walk.Step, error) { return team.Seal(a.Node, a.Block, a.Choice) })
	case "task":
		act(func() (session.Result, *walk.Step, error) { return team.Task(a.Node, a.Block, a.Guess) })
	case "walk":
		// The phone asking walks another of its groups: Port is its key.
		act(func() (session.Result, *walk.Step, error) { return team.Walk(a.Port) })
	case "sabotage":
		act(func() (session.Result, *walk.Step, error) { return team.Sabotage(a.Node, a.Block) })
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
		if crew != nil {
			crew.player(crew.As).Position = team.Position
		}
	default:
		return simulateResponse{}, http.StatusBadRequest, "unknown action " + a.Kind
	}

	if err != nil {
		out := refused(before, err)
		out.Result = res
		if crew != nil {
			*crew = *crewBefore
			crew.seat(before)
		}
		out.Result.View = before.View()
		return out, http.StatusOK, ""
	}
	// The step the action was on: a split team's group's.
	acted, split := team.Current, team.Split != ""
	var passed []string
	if step != nil {
		team.Apply(*step)
		passed = step.Passed
	}
	if crew != nil {
		crew.Seals = team.Seals
		switch {
		case split:
			// Kept by step, so one group moving on leaves another's be.
			if crew.PressesAt == nil {
				crew.PressesAt = map[string]map[int64]session.Press{}
			}
			crew.PressesAt[acted] = team.Presses
			if step != nil {
				delete(crew.PressesAt, acted)
			}
			if team.Split == "" {
				crew.PressesAt = nil
			}
			crew.Presses = nil
		default:
			crew.Presses = team.Presses
			// A step done together starts afresh at the next one.
			if step != nil {
				crew.Presses, team.Presses = nil, nil
			}
		}
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

// startPlaceOf is the adventure's required starting point, or nil.
func startPlaceOf(ctx context.Context, db *sql.DB, advID int64) (*graph.Zone, error) {
	var (
		lat, lng, radius sql.NullFloat64
		label            string
	)
	if err := db.QueryRowContext(ctx, `SELECT start_lat, start_lng, start_radius_m, start_label FROM adventures WHERE id = $1`, advID).
		Scan(&lat, &lng, &radius, &label); err != nil {
		return nil, err
	}
	f := func(v sql.NullFloat64) *float64 {
		if !v.Valid {
			return nil
		}
		return &v.Float64
	}
	return session.NewStartPlace(f(lat), f(lng), f(radius), label), nil
}

// check refuses a crew the simulator cannot seat.
func (c *simCrew) check() string {
	if len(c.Players) == 0 || len(c.Players) > maxCrew {
		return "a crew has 1 to 6 players"
	}
	seen := map[int64]bool{}
	for _, p := range c.Players {
		if p.Slot <= 0 || seen[p.Slot] {
			return "each player needs a slot of their own"
		}
		seen[p.Slot] = true
	}
	if c.As == 0 {
		c.As = c.Players[0].Slot
	}
	if !seen[c.As] {
		return "the acting player is not in the crew"
	}
	return ""
}

func (c *simCrew) slots() []int64 {
	out := make([]int64, len(c.Players))
	for i, p := range c.Players {
		out[i] = p.Slot
	}
	return out
}

func (c *simCrew) player(slot int64) *simPlayer {
	for i := range c.Players {
		if c.Players[i].Slot == slot {
			return &c.Players[i]
		}
	}
	return nil
}

// deal gives each secret role nobody was given to a player at random,
// once the crew is big enough to hide among, as a live team's first move
// does. A role the author gave to no one (0) stays no one's.
func (c *simCrew) deal(g *graph.Graph) {
	if len(c.Players) < graph.MinSecretPlayers {
		return
	}
	for _, role := range g.SecretRoles() {
		if _, dealt := c.Secrets[role]; dealt {
			continue
		}
		if c.Secrets == nil {
			c.Secrets = map[string]int64{}
		}
		c.Secrets[role] = c.Players[rand.IntN(len(c.Players))].Slot
	}
}

// seat puts the crew on the team — who plays, where each phone is, who
// holds which role — with the acting player's phone asking, and returns
// the team.
func (c *simCrew) seat(t *session.Team) *session.Team {
	// A split team's group narrows all of the below: put it back first.
	t.Unfocus()
	slots := c.slots()
	t.Players, t.Size = slots, len(slots)
	t.Phones, t.PhoneAt = []session.Position{}, map[int64]session.Position{}
	for _, p := range c.Players {
		if p.Position != nil {
			t.Phones = append(t.Phones, *p.Position)
			t.PhoneAt[p.Slot] = *p.Position
		}
	}
	t.Secrets, t.Seals, t.Presses = c.Secrets, c.Seals, c.Presses
	t.PressesAt = nil
	if t.Split != "" {
		// One map, the crew's and the team's alike.
		if c.PressesAt == nil {
			c.PressesAt = map[string]map[int64]session.Press{}
		}
		t.PressesAt = c.PressesAt
	}
	t.RoleHolders = map[string]int64{}
	for role, h := range play.DealAmong(slots, c.Roles, t.G.DealtRoles()) {
		t.RoleHolders[role] = h.Slot
	}
	// Into the groups of a split, as the live engine deals them.
	t.Deal(slots, t.RoleHolders)
	t.Viewer = c.viewer(t, c.As)
	return t
}

// viewer is slot's phone: where it is and the roles its player holds.
func (c *simCrew) viewer(t *session.Team, slot int64) *session.Viewer {
	v := &session.Viewer{Slot: slot}
	if p := c.player(slot); p != nil {
		v.Position = p.Position
	}
	for _, role := range t.G.DealtRoles() {
		if t.RoleHolders[role] == slot {
			v.Roles = append(v.Roles, role)
		}
	}
	if d := t.G.DefaultRole(); d != "" && (len(v.Roles) == 0 || len(c.Players) <= 1) {
		v.Roles = append(v.Roles, d)
	}
	for _, role := range t.G.SecretRoles() {
		if c.Secrets[role] == slot {
			v.Roles = append(v.Roles, role)
		}
	}
	return v
}

// phones are what each of the crew's phones shows.
func (c *simCrew) phones(t *session.Team) []simPhone {
	c.seat(t)
	out := make([]simPhone, 0, len(c.Players))
	for i, p := range c.Players {
		t.Viewer = c.viewer(t, p.Slot)
		// Its own roles: not those a group lends it (session.Team.Focus).
		own := slices.Clone(t.Viewer.Roles)
		v := t.View()
		t.Unfocus()
		// Each phone its roles marked, and who holds the others, as the
		// player endpoints tell them.
		v.Roles = slices.Clone(v.Roles)
		for j := range v.Roles {
			if r := &v.Roles[j]; !r.Secret {
				r.Mine = slices.Contains(own, r.Slug)
				if holder, ok := t.RoleHolders[r.Slug]; ok && !r.Default {
					r.Holder = c.player(holder).Name
				}
			}
		}
		// Each phone its own part of a split secret, as the player
		// endpoints deal them.
		v.Blocks = slices.Clone(v.Blocks)
		for j := range v.Blocks {
			if b := &v.Blocks[j]; b.Kind == graph.BlockSplit && !b.Locked {
				b.Parts = session.SplitParts(b.Parts, i, len(c.Players))
				b.Player, b.Players = i+1, len(c.Players)
			}
		}
		out = append(out, simPhone{Slot: p.Slot, Roles: own, View: v})
	}
	t.Viewer = c.viewer(t, c.As)
	return out
}

func (c *simCrew) clone() *simCrew {
	b, _ := json.Marshal(c)
	var out simCrew
	_ = json.Unmarshal(b, &out)
	return &out
}
