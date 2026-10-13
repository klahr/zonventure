package mcp

// playtest_start / playtest_act / playtest_end: an assistant plays the adventure's
// saved draft as a team would, one action at a time, from what the
// phone shows — the prompt, the blocks, the pictures, the answer box,
// the hints as they open — and never the answers. It is a blind
// playtest: can a team solve this from what it is given, do the hints
// help, is any step unclear or a dead end?
//
// Every action goes through the designer's simulator endpoint
// (internal/admin/simulate.go), so the rules are the live engine's. The
// simulator is stateless; the session here keeps the team between
// calls so the model does not carry it. Sessions live in memory, belong
// to the caller who started them, and lapse after playTTL.
//
// The clock is the team's own and moves only as the model says: a wait,
// a walk (at walking pace, from where the team stands to the place), and
// a little thinking before each action. A hint that opens after ten
// minutes is ten minutes of waiting away, not ten minutes of real time.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/imageutil"
)

const (
	playTTL         = 2 * time.Hour
	maxPlaySessions = 200
	// walkPace is metres per second: a team walking, map in hand.
	walkPace = 1.3
	// thinkSeconds is the clock a team spends before an action unless
	// the model says otherwise: reading, talking, typing.
	thinkSeconds = 20
	// maxPlayImages bounds the pictures one screen sends.
	maxPlayImages = 4
	playImageDim  = 768
)

type playSession struct {
	mu      sync.Mutex
	owner   int64
	slug    string
	env     json.RawMessage
	g       *graph.Graph
	team    json.RawMessage
	view    session.View
	touched time.Time
	// anywhere skips the adventure's required starting point (playtest_start).
	anywhere bool
	// shown is the node whose pictures were last sent; they are sent
	// again only on a new step or a look.
	shown string
	// used is the gadgets the team has worked on this step — shaken,
	// tilted, torch-lit — whose hidden text the screen may now show.
	used map[string]bool

	actions, wrong, hints, skips int
	walked                       float64
	steps                        []string
	refused                      []string
}

type playStore struct {
	mu sync.Mutex
	m  map[string]*playSession
}

// get returns the caller's session, sweeping lapsed ones.
func (p *playStore) get(id string, owner int64) (*playSession, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweep()
	ps, ok := p.m[id]
	if !ok || ps.owner != owner {
		return nil, false
	}
	return ps, true
}

func (p *playStore) put(ps *playSession) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweep()
	if p.m == nil {
		p.m = map[string]*playSession{}
	}
	if len(p.m) >= maxPlaySessions {
		return "", false
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	p.m[id] = ps
	return id, true
}

func (p *playStore) drop(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, id)
}

func (p *playStore) sweep() {
	for id, ps := range p.m {
		if time.Since(ps.touched) > playTTL {
			delete(p.m, id)
		}
	}
}

func (s *Server) registerPlayTools() {
	s.tools.register(ToolDescriptor{
		Name:     "playtest_start",
		Audience: audienceAuthor,
		Scope:    scopeRead,
		Description: "Start a blind playtest of the adventure's saved draft: you play as a team, from what the phone shows, with the real rules. " +
			"Returns a session id and the first screen. You never see the answers — solve each step from its text, pictures and hints, as a team would. " +
			"Then call playtest_act once per action and playtest_end for a summary. Nothing is stored; the session lapses after two hours.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug"},
			"properties": map[string]any{
				"slug":      map[string]any{"type": "string", "description": "Adventure slug."},
				"team_size": map[string]any{"type": "integer", "minimum": 1, "maximum": 12, "description": "Players in the team; some steps read it. Default 3."},
				"lang":      map[string]any{"type": "string", "description": "Language to play in, if the adventure is translated."},
				"anywhere":  map[string]any{"type": "boolean", "description": "Begin wherever the team is, skipping the adventure's required starting point — for a playtest away from the site. Default false: the team must walk there (move to the starting point) first."},
			},
		},
	}, playStart)

	s.tools.register(ToolDescriptor{
		Name:     "playtest_act",
		Audience: audienceAuthor,
		Scope:    scopeRead,
		Description: "Do one thing in a playtest started with playtest_start, and see the screen after it. Actions (kind): " +
			"begin {start} picks where to begin; answer {guess} tries an answer; continue moves on from a story step; " +
			"choose {port} takes a branch at a choice (port \"done\" moves on when allowed); back returns to the hub above; " +
			"hint opens the next hint once it is available; skip and timeout take those routes when offered; leave goes to find missing items; " +
			"turn turns the team's keys in a two-key launch — you are all the players, so every key at once, each at its post if it has posts; " +
			"pick {block, choice} answers a quiz question, tie-breaker, suspect board or draw; use {block} works a gadget (shake, tilt, torch, tap the picture) to see what it shows; " +
			"move {place} walks the team to a place by name or slug, taking walking time; wait {seconds} lets time pass, as for a hint to open; " +
			"language {lang}; look shows the screen again with its pictures. " +
			"Every action but move, wait and look first spends seconds (default 20) of thinking time. Play fairly: you are the players.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"session", "kind"},
			"properties": map[string]any{
				"session": map[string]any{"type": "string"},
				"kind":    map[string]any{"type": "string", "enum": []string{"begin", "answer", "continue", "choose", "back", "hint", "skip", "timeout", "leave", "turn", "pick", "use", "move", "wait", "language", "look"}},
				"start":   map[string]any{"type": "string"},
				"guess":   map[string]any{"type": "string"},
				"port":    map[string]any{"type": "string"},
				"block":   map[string]any{"type": "string"},
				"choice":  map[string]any{"type": "string"},
				"place":   map[string]any{"type": "string"},
				"lang":    map[string]any{"type": "string"},
				"seconds": map[string]any{"type": "integer", "minimum": 0, "maximum": 86400},
			},
		},
	}, playAct)

	s.tools.register(ToolDescriptor{
		Name:        "playtest_end",
		Audience:    audienceAuthor,
		Scope:       scopeRead,
		Description: "End a playtest and get its summary: time played, steps passed, wrong answers, hints, skips, distance walked and refused actions.",
		InputSchema: map[string]any{
			"type":       "object",
			"required":   []string{"session"},
			"properties": map[string]any{"session": map[string]any{"type": "string"}},
		},
	}, playEnd)
}

func playStart(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Slug     string `json:"slug"`
		TeamSize int    `json:"team_size"`
		Lang     string `json:"lang"`
		Anywhere bool   `json:"anywhere"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if a.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if a.TeamSize == 0 {
		a.TeamSize = 3
	}
	if a.TeamSize < 1 || a.TeamSize > 12 {
		return errorResult("team_size must be between 1 and 12"), nil
	}
	me, ok := auth.FromContext(ctx)
	if !ok {
		return errorResult("no authenticated caller"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	g, bad := s.loadDraftForEdit(ctx, a.Slug)
	if bad != nil {
		return *bad, nil
	}
	env, err := graph.MarshalEnvelope(g)
	if err != nil {
		return errorResult("encoding draft: " + err.Error()), nil
	}
	ps := &playSession{owner: me.ID, slug: a.Slug, env: env, g: g, anywhere: a.Anywhere, touched: time.Now()}
	out, bad := ps.step(ctx, s, map[string]any{"kind": "start"})
	if bad != nil {
		return *bad, nil
	}
	if out.refusal != "" {
		return errorResult("the draft would not start: " + out.refusal), nil
	}
	// The simulator starts a team of one; this one has players.
	var t map[string]json.RawMessage
	if err := json.Unmarshal(ps.team, &t); err == nil {
		t["team_size"] = json.RawMessage(strconv.Itoa(a.TeamSize))
		ps.team, _ = json.Marshal(t)
	}
	if a.Lang != "" {
		if out, bad = ps.step(ctx, s, map[string]any{"kind": "language", "lang": a.Lang}); bad != nil {
			return *bad, nil
		}
		if out.refusal != "" {
			return errorResult("cannot play in " + a.Lang + ": " + out.refusal), nil
		}
	}
	id, ok := s.plays.put(ps)
	if !ok {
		return errorResult("too many playtests running; end one with playtest_end"), nil
	}
	intro := fmt.Sprintf("Playtest of %q started. Session: %s\nYou are a team of %d. Play from what the screen shows; the answers are not shown to you.\n", a.Slug, id, a.TeamSize)
	return ps.screen(ctx, s, intro, true), nil
}

func playAct(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Session string `json:"session"`
		Kind    string `json:"kind"`
		Start   string `json:"start"`
		Guess   string `json:"guess"`
		Port    string `json:"port"`
		Block   string `json:"block"`
		Choice  string `json:"choice"`
		Place   string `json:"place"`
		Lang    string `json:"lang"`
		Seconds *int   `json:"seconds"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	me, ok := auth.FromContext(ctx)
	if !ok {
		return errorResult("no authenticated caller"), nil
	}
	ps, ok := s.plays.get(a.Session, me.ID)
	if !ok {
		return errorResult("no such playtest; it may have lapsed — start another with playtest_start"), nil
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.touched = time.Now()
	ps.actions++

	node := ps.view.Node
	var note string
	switch a.Kind {
	case "look":
		return ps.screen(ctx, s, "", true), nil
	case "wait":
		if a.Seconds == nil || *a.Seconds <= 0 {
			return errorResult("wait needs seconds"), nil
		}
		out, bad := ps.wait(ctx, s, *a.Seconds)
		if bad != nil {
			return *bad, nil
		}
		note = fmt.Sprintf("Waited %s.%s", clock(float64(*a.Seconds)), out.outcome())
		return ps.screen(ctx, s, note, false), nil
	case "move":
		z, ok := ps.place(a.Place)
		if !ok {
			return errorResult("no place called " + strconv.Quote(a.Place) + "; the screen lists the places"), nil
		}
		d := ps.distance(z)
		secs := int(math.Round(d / walkPace))
		var out stepOut
		if secs > 0 {
			var bad *ToolCallResult
			if out, bad = ps.wait(ctx, s, secs); bad != nil {
				return *bad, nil
			}
		}
		if _, bad := ps.step(ctx, s, map[string]any{"kind": "move", "zone": z.Slug}); bad != nil {
			return *bad, nil
		}
		ps.walked += d
		note = fmt.Sprintf("Walked to %s: %.0f m, %s.%s", zoneLabel(z), d, clock(float64(secs)), out.outcome())
		return ps.screen(ctx, s, note, false), nil
	case "use":
		if blockIn(ps.view, a.Block) == nil {
			return errorResult("no block " + strconv.Quote(a.Block) + " on this screen"), nil
		}
		if ps.used == nil {
			ps.used = map[string]bool{}
		}
		ps.used[node+"\x00"+a.Block] = true
		return ps.screen(ctx, s, "You work the gadget.", true), nil
	}

	act := map[string]any{"kind": a.Kind, "node": node}
	switch a.Kind {
	case "begin":
		act["port"] = a.Start
	case "answer":
		act["guess"] = a.Guess
	case "choose":
		act["port"] = a.Port
	case "pick":
		act["block"], act["choice"] = a.Block, a.Choice
	case "language":
		act["lang"] = a.Lang
	case "hint":
		if ps.view.NextHint == nil {
			return errorResult("there is no hint to open on this screen"), nil
		}
		act["index"] = ps.view.NextHint.Index
	case "continue", "back", "skip", "timeout", "leave", "turn":
	default:
		return errorResult("unknown action " + strconv.Quote(a.Kind)), nil
	}
	think := thinkSeconds
	if a.Seconds != nil {
		think = *a.Seconds
	}
	if think > 0 {
		out, bad := ps.wait(ctx, s, think)
		if bad != nil {
			return *bad, nil
		}
		if out.TimeUp || ps.view.Node != node {
			// The clock moved the team while it was thinking.
			return ps.screen(ctx, s, "While you were thinking:"+out.outcome(), true), nil
		}
	}
	out, bad := ps.step(ctx, s, act)
	if bad != nil {
		return *bad, nil
	}
	switch {
	case out.refusal != "":
		ps.refused = append(ps.refused, fmt.Sprintf("%s at %s: %s", a.Kind, node, out.refusal))
		note = "Not possible: " + out.refusal
	case a.Kind == "answer" && out.Accepted:
		note = "Right!"
		if out.Points != 0 {
			note += fmt.Sprintf(" %+g points.", out.Points)
		}
	case a.Kind == "answer":
		ps.wrong++
		note = "Wrong."
		if out.Response != "" {
			note += " The phone says: " + out.Response
		}
		if out.Port != "" {
			note += " The step moved you on anyway."
		}
	case a.Kind == "turn" && out.Accepted:
		note = "Every key turned: it opens."
	case a.Kind == "turn":
		note = "Not enough keys turned yet."
		if w := out.Waiting; w != nil {
			note = fmt.Sprintf("%d of %d keys turned: the rest are at posts where the team is not.", w.Done, w.Of)
		}
	case a.Kind == "hint":
		ps.hints++
		note = "Hint: " + out.Hint
	case a.Kind == "skip":
		ps.skips++
		note = "Skipped."
	case a.Kind == "pick":
		note = "Picked."
	}
	note += out.outcome()
	return ps.screen(ctx, s, note, false), nil
}

func playEnd(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Session string `json:"session"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	me, ok := auth.FromContext(ctx)
	if !ok {
		return errorResult("no authenticated caller"), nil
	}
	ps, ok := s.plays.get(a.Session, me.ID)
	if !ok {
		return errorResult("no such playtest"), nil
	}
	s.plays.drop(a.Session)
	ps.mu.Lock()
	defer ps.mu.Unlock()
	var t struct {
		StartedAt time.Time `json:"started_at"`
		Now       time.Time `json:"now"`
		Current   string    `json:"current"`
	}
	_ = json.Unmarshal(ps.team, &t)
	out, _ := json.Marshal(map[string]any{
		"finished":       ps.view.Finished,
		"outcome":        ps.view.Outcome,
		"at":             t.Current,
		"played_seconds": math.Round(t.Now.Sub(t.StartedAt).Seconds()),
		"score":          ps.view.Score - ps.view.PenaltyScore,
		"steps":          ps.steps,
		"actions":        ps.actions,
		"wrong_answers":  ps.wrong,
		"hints":          ps.hints,
		"skips":          ps.skips,
		"walked_m":       math.Round(ps.walked),
		"refused":        ps.refused,
	})
	return textResult(string(out)), nil
}

// stepOut is what one simulator call reported.
type stepOut struct {
	session.Result
	refusal string
}

// outcome is what the clock or the walk did on its own, if anything.
func (o stepOut) outcome() string {
	if o.TimeUp {
		return " Time is up: the adventure's time limit has moved you on."
	}
	return ""
}

// step runs one simulator action and keeps the team it returns.
func (ps *playSession) step(ctx context.Context, s *Server, act map[string]any) (stepOut, *ToolCallResult) {
	body := map[string]any{"envelope": ps.env, "action": act}
	if ps.team != nil {
		body["team"] = ps.team
	}
	if ps.anywhere {
		body["any_place"] = true
	}
	status, raw, err := callAdmin(ctx, s.Admin.SimulateAdventureGraph, "POST", "/api/admin/adventures/"+ps.slug+"/simulate",
		map[string]string{"slug": ps.slug}, nil, body)
	if err != nil {
		r := errorResult("call: " + err.Error())
		return stepOut{}, &r
	}
	if status != http.StatusOK {
		r := adminCallResult(status, raw)
		return stepOut{}, &r
	}
	var resp struct {
		Team    json.RawMessage `json:"team"`
		Result  session.Result  `json:"result"`
		Refusal *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"refusal"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		r := errorResult("decoding simulation: " + err.Error())
		return stepOut{}, &r
	}
	if len(resp.Team) > 0 && string(resp.Team) != "null" {
		ps.team = resp.Team
	}
	before := ps.view.Node
	ps.view = resp.Result.View
	if ps.view.Node != before && ps.view.Node != "" {
		ps.steps = append(ps.steps, ps.view.Node)
		ps.used = nil
	}
	out := stepOut{Result: resp.Result}
	if resp.Refusal != nil {
		out.refusal = refusalText(resp.Refusal.Code, resp.Refusal.Message)
	}
	return out, nil
}

// wait moves the team's clock on, in the simulator's largest steps.
func (ps *playSession) wait(ctx context.Context, s *Server, secs int) (stepOut, *ToolCallResult) {
	var out stepOut
	for secs > 0 {
		n := min(secs, 86400)
		o, bad := ps.step(ctx, s, map[string]any{"kind": "wait", "seconds": n})
		if bad != nil {
			return o, bad
		}
		out.TimeUp = out.TimeUp || o.TimeUp
		secs -= n
	}
	return out, nil
}

func refusalText(code, msg string) string {
	switch code {
	case "not_here":
		return "you are not at the place this needs."
	case "no_fix":
		return "the phone does not know where it is yet; wait a moment and try again."
	case "locked":
		return "answering is locked for now after wrong answers; wait."
	case "time_up":
		return "the time for this step has run out."
	case "unanswered":
		return "answer the questions on this step first."
	case "finished":
		return "the team has finished."
	case "stale":
		return "the team has already moved on."
	case "graph":
		return "the draft stopped the team here (an author problem): " + msg
	}
	return msg
}

// place finds a zone by slug or name.
func (ps *playSession) place(name string) (graph.Zone, bool) {
	name = strings.TrimSpace(name)
	for _, z := range ps.g.Zones {
		if z.Slug == name {
			return z, true
		}
	}
	for _, z := range ps.g.Zones {
		if strings.EqualFold(z.Name, name) {
			return z, true
		}
	}
	return graph.Zone{}, false
}

func (ps *playSession) position() *session.Position {
	var t struct {
		Position *session.Position `json:"position"`
	}
	_ = json.Unmarshal(ps.team, &t)
	return t.Position
}

// distance is how far the team is from z; nothing before it has been
// anywhere — it arrives at its first place by bus.
func (ps *playSession) distance(z graph.Zone) float64 {
	p := ps.position()
	if p == nil {
		return 0
	}
	return session.HaversineMeters(p.Lat, p.Lng, z.Lat, z.Lng)
}

func zoneLabel(z graph.Zone) string {
	if z.Name != "" {
		return z.Name
	}
	return z.Slug
}

func blockIn(v session.View, id string) *session.BlockView {
	for i := range v.Blocks {
		if v.Blocks[i].ID == id {
			return &v.Blocks[i]
		}
	}
	return nil
}

// --- the screen ---------------------------------------------------------------

// screen renders what the phone shows now, after note, with the step's
// pictures when it is new or the model asked to look.
func (ps *playSession) screen(ctx context.Context, s *Server, note string, look bool) ToolCallResult {
	v := ps.view
	var b strings.Builder
	if note != "" {
		b.WriteString(note + "\n\n")
	}
	var t struct {
		StartedAt time.Time `json:"started_at"`
		Now       time.Time `json:"now"`
	}
	_ = json.Unmarshal(ps.team, &t)
	fmt.Fprintf(&b, "⏱ %s played", clock(t.Now.Sub(t.StartedAt).Seconds()))
	if !v.HideScore {
		fmt.Fprintf(&b, " · score %g", v.Score-v.PenaltyScore)
	}
	if v.EndsAt != nil {
		fmt.Fprintf(&b, " · adventure ends in %s", clock(v.EndsAt.Sub(v.Now).Seconds()))
	}
	b.WriteString("\n\n")

	var media []int64
	addMedia := func(ms ...graph.Media) {
		for _, m := range ms {
			if m.Kind == graph.MediaImage {
				media = append(media, m.ID)
			} else if m.Alt != "" {
				fmt.Fprintf(&b, "[%s: %s]\n", m.Kind, m.Alt)
			} else {
				fmt.Fprintf(&b, "[%s — you cannot hear or watch it here]\n", m.Kind)
			}
		}
	}

	switch {
	case v.Finished:
		b.WriteString("## The adventure is over\n")
		if v.Outcome != "" {
			fmt.Fprintf(&b, "Outcome: %s\n", v.Outcome)
		}
		if v.Text != "" {
			b.WriteString(v.Text + "\n")
		}
		if q := v.Quiz; q != nil && len(q.Items) > 0 {
			fmt.Fprintf(&b, "Quiz: %d of %d right.\n", q.Right, len(q.Items))
		}
	case len(v.Starts) > 0:
		b.WriteString("## Choose where to begin\n")
		for _, st := range v.Starts {
			fmt.Fprintf(&b, "- start %q: %s", st.Node, st.Title)
			if st.Zone != nil {
				fmt.Fprintf(&b, " — at %s", zoneLabel(*st.Zone))
				if !st.Here {
					b.WriteString(" (go there first)")
				}
			}
			b.WriteString("\n")
		}
	default:
		if v.Title != "" {
			fmt.Fprintf(&b, "## %s\n", v.Title)
		}
		if v.Zone != nil {
			if v.Here {
				fmt.Fprintf(&b, "You are at %s.\n", zoneLabel(*v.Zone))
			} else {
				fmt.Fprintf(&b, "This step is at %s — go there (move) to play it.\n", zoneLabel(*v.Zone))
			}
		}
		// Text and Media repeat the blocks for phones from before them;
		// a hub's text is its own.
		if v.Text != "" && (len(v.Blocks) == 0 || v.Type == graph.Hub) {
			b.WriteString(v.Text + "\n")
		}
		if len(v.Blocks) == 0 {
			addMedia(v.Media...)
		}
		for _, bl := range v.Blocks {
			media = append(media, ps.block(&b, v, bl)...)
		}
		ps.input(&b, v)
		ps.offer(&b, v)
		for i, h := range v.Hints {
			fmt.Fprintf(&b, "Hint %d: %s\n", i+1, h.Text)
			addMedia(h.Media...)
		}
		if h := v.NextHint; h != nil {
			cost := ""
			if h.Cost > 0 {
				cost = fmt.Sprintf(" (costs %d points)", h.Cost)
			}
			if wait := h.AvailableAt.Sub(v.Now).Seconds(); wait > 0 {
				fmt.Fprintf(&b, "A hint opens in %s%s.\n", clock(wait), cost)
			} else {
				fmt.Fprintf(&b, "A hint is available now%s (hint).\n", cost)
			}
		}
		if v.BlockedUntil != nil && v.BlockedUntil.After(v.Now) {
			fmt.Fprintf(&b, "Answering is locked for %s.\n", clock(v.BlockedUntil.Sub(v.Now).Seconds()))
		}
		if v.Deadline != nil {
			fmt.Fprintf(&b, "This step's time runs out in %s.", clock(v.Deadline.Sub(v.Now).Seconds()))
			if v.HasTimeout {
				b.WriteString(" Then you can take its timeout route (timeout).")
			}
			b.WriteString("\n")
		}
		if v.CanSkip {
			b.WriteString("You may skip this step (skip).\n")
		}
		if v.BackTo != "" {
			label := v.BackLabel
			if label == "" {
				label = "back"
			}
			fmt.Fprintf(&b, "You can go back (back): %s.\n", label)
		}
	}

	if len(v.Items) > 0 {
		b.WriteString("\nBackpack:")
		for _, it := range v.Items {
			fmt.Fprintf(&b, " %s (%s)×%d;", it.Name, it.Slug, it.Qty)
		}
		b.WriteString("\n")
	}
	if len(v.Journal) > 0 {
		b.WriteString("\nJournal:\n")
		for _, j := range v.Journal {
			fmt.Fprintf(&b, "- %s: %s\n", j.Title, j.Body)
		}
	}
	if len(v.Languages) > 1 {
		fmt.Fprintf(&b, "\nLanguages: %s (playing in %s).\n", strings.Join(v.Languages, ", "), v.Lang)
	}
	if !v.Finished {
		ps.places(&b)
	}

	res := textResult(b.String())
	if look || ps.shown != v.Node {
		ps.shown = v.Node
		res.Content = append(res.Content, ps.images(ctx, s, media)...)
	} else if len(media) > 0 {
		res.Content[0].Text += "\n(The pictures were shown earlier; look to see them again.)"
	}
	return res
}

// block renders one block as the phone shows it, returning its pictures.
// What a gadget hides — a level's message, a torch's dark corners —
// shows only once the team has used it.
func (ps *playSession) block(b *strings.Builder, v session.View, bl session.BlockView) []int64 {
	used := ps.used[v.Node+"\x00"+bl.ID]
	use := func(how string) {
		if used {
			if bl.Text != "" {
				fmt.Fprintf(b, "%s It shows: %s\n", how, bl.Text)
			} else {
				fmt.Fprintf(b, "%s\n", how)
			}
			return
		}
		fmt.Fprintf(b, "%s (use block %q)\n", how, bl.ID)
	}
	img := func(ids ...int64) []int64 {
		var out []int64
		for _, id := range ids {
			if id > 0 {
				out = append(out, id)
			}
		}
		return out
	}
	if bl.Locked {
		if bl.Place != nil {
			fmt.Fprintf(b, "(Something here appears at %s.)\n", zoneLabel(*bl.Place))
		} else {
			b.WriteString("(Something here is still hidden.)\n")
		}
		return nil
	}
	switch bl.Kind {
	case graph.BlockTitle, graph.BlockDivider, graph.BlockBulletin:
	case graph.BlockPoints:
		if !v.HideScore && bl.Points != 0 {
			if bl.Points == 1 {
				b.WriteString("Worth 1 point.\n")
			} else {
				fmt.Fprintf(b, "Worth %g points.\n", bl.Points)
			}
		}
	case graph.BlockText:
		if bl.Text != "" {
			b.WriteString(bl.Text + "\n")
		}
	case graph.BlockNote:
		fmt.Fprintf(b, "A note: %s\n", bl.Text)
	case graph.BlockChat, graph.BlockCall:
		if bl.Kind == graph.BlockCall {
			fmt.Fprintf(b, "Incoming call from %s:\n", bl.Who)
		} else {
			b.WriteString("Messages:\n")
		}
		for _, l := range bl.Lines {
			who := l.Who
			if l.Me {
				who = "You"
			}
			fmt.Fprintf(b, "  %s: %s\n", who, l.Text)
		}
		if bl.Text != "" {
			b.WriteString("  " + bl.Text + "\n")
		}
	case graph.BlockCipher:
		fmt.Fprintf(b, "A %s cipher key sheet", bl.Cipher)
		if bl.Shift != 0 {
			fmt.Fprintf(b, " (shift %d)", bl.Shift)
		}
		if bl.Key != "" {
			fmt.Fprintf(b, ": %s", bl.Key)
		}
		b.WriteString(".\n")
	case graph.BlockImage:
		if bl.Alt != "" {
			fmt.Fprintf(b, "[picture: %s]\n", bl.Alt)
		}
		return img(bl.Media)
	case graph.BlockAudio, graph.BlockVideo:
		if bl.Alt != "" {
			fmt.Fprintf(b, "[%s: %s]\n", bl.Kind, bl.Alt)
		} else {
			fmt.Fprintf(b, "[%s — you cannot hear or watch it here]\n", bl.Kind)
		}
	case graph.BlockHotspots:
		b.WriteString("A picture with spots to tap.")
		if used {
			for _, sp := range bl.Spots {
				fmt.Fprintf(b, " Tapping one says: %q.", sp.Text)
			}
			b.WriteString("\n")
		} else {
			fmt.Fprintf(b, " (use block %q to tap them)\n", bl.ID)
		}
		return img(bl.Media)
	case graph.BlockTorch:
		use("A dark picture to explore with a torch.")
		if used {
			return img(bl.Media)
		}
	case graph.BlockSpotDiff:
		b.WriteString("Two pictures: spot the differences.\n")
		return img(bl.Media, bl.Other)
	case graph.BlockOverlay:
		b.WriteString("A picture to line up with the camera's view here.\n")
		return img(bl.Media)
	case graph.BlockLevel:
		use("A spirit level: hold the phone flat.")
	case graph.BlockNoise:
		use("A sound meter: make enough noise.")
	case graph.BlockShake:
		use("Shake the phone.")
	case graph.BlockBearing:
		use(fmt.Sprintf("Point the phone the right way (%d° from north).", bl.Heading))
	case graph.BlockFlasher:
		fmt.Fprintf(b, "The screen flashes in Morse: %s\n", morse(bl.Text))
	case graph.BlockLetters:
		fmt.Fprintf(b, "Letter tiles to rearrange: %s\n", strings.Join(scramble(bl.ID, bl.Text), " "))
	case graph.BlockSplit:
		b.WriteString("A secret split between your phones; between you, you see:")
		for i, p := range bl.Parts {
			fmt.Fprintf(b, " [%d] %s", i+1, p)
		}
		b.WriteString("\n")
	case graph.BlockMap, graph.BlockCompass, graph.BlockRadar:
		ps.gauge(b, bl)
	case graph.BlockCountdown:
		if bl.EndsAt != nil {
			fmt.Fprintf(b, "A countdown: %s left.\n", clock(bl.EndsAt.Sub(v.Now).Seconds()))
		}
	case graph.BlockStatus:
		for _, c := range bl.CounterValues {
			name := c.Name
			if name == "" {
				name = c.Slug
			}
			fmt.Fprintf(b, "%s: %g\n", name, c.Value)
		}
	case graph.BlockStandings:
		if st := bl.Standings; st != nil {
			fmt.Fprintf(b, "You are %d of %d.\n", st.Rank, st.Of)
		}
	case graph.BlockQuiz:
		fmt.Fprintf(b, "Question (pick block %q, choice 1, X or 2): %s\n", bl.ID, bl.Text)
		for i, o := range bl.Options {
			if i < len(graph.QuizChoices) {
				fmt.Fprintf(b, "  %s: %s\n", graph.QuizChoices[i], o)
			}
		}
		if bl.Picked != "" {
			fmt.Fprintf(b, "  You picked %s.\n", bl.Picked)
		}
	case graph.BlockTiebreak:
		fmt.Fprintf(b, "Tie-breaker (pick block %q with a number): %s", bl.ID, bl.Text)
		if bl.Picked != "" {
			fmt.Fprintf(b, " — you said %s", bl.Picked)
		}
		b.WriteString("\n")
	case graph.BlockSuspects:
		fmt.Fprintf(b, "A suspect board (pick block %q with the numbers you strike, e.g. \"0,2\"):\n", bl.ID)
		struck := strings.Split(bl.Picked, ",")
		for i, c := range bl.Cards {
			mark := ""
			if slices.Contains(struck, strconv.Itoa(i)) {
				mark = " (struck)"
			}
			fmt.Fprintf(b, "  %d. %s: %s%s\n", i, c.Name, c.Text, mark)
		}
	case graph.BlockDraw:
		if bl.Picked != "" {
			if i, err := strconv.Atoi(bl.Picked); err == nil && i >= 0 && i < len(bl.Options) {
				fmt.Fprintf(b, "You drew: %s\n", bl.Options[i])
			}
		} else {
			fmt.Fprintf(b, "A card to draw (pick block %q).\n", bl.ID)
		}
	case graph.BlockPhoto:
		fmt.Fprintf(b, "Take a photo: %s (photos are not part of a playtest).\n", bl.Text)
	case graph.BlockVote:
		fmt.Fprintf(b, "A vote among the players: %s — %s (votes are not part of a playtest).\n", bl.Text, strings.Join(bl.Options, " / "))
	case graph.BlockSabotage:
		fmt.Fprintf(b, "The saboteur's secret button: %s (only their phone shows it; sabotage is not part of a playtest).\n", bl.Text)
	case graph.BlockAccuse:
		fmt.Fprintf(b, "An accusation among the players: %s (it needs a dealt saboteur and real phones; not part of a playtest).\n", bl.Text)
	case graph.BlockRoles:
		if bl.Text != "" {
			b.WriteString(bl.Text + "\n")
		}
		b.WriteString("The players' roles, as each phone shows them (roles are not part of a playtest).\n")
	default:
		if bl.Text != "" {
			fmt.Fprintf(b, "%s\n", bl.Text)
		}
	}
	return nil
}

// gauge renders a map, compass or radar from where the team stands.
func (ps *playSession) gauge(b *strings.Builder, bl session.BlockView) {
	z := bl.Place
	if z == nil {
		return
	}
	p := ps.position()
	switch bl.Kind {
	case graph.BlockMap:
		fmt.Fprintf(b, "A map shows %s.\n", zoneLabel(*z))
	case graph.BlockCompass:
		if p == nil {
			b.WriteString("A compass, waiting for your position.\n")
			return
		}
		d := session.HaversineMeters(p.Lat, p.Lng, z.Lat, z.Lng)
		fmt.Fprintf(b, "A compass points %s", heading(p.Lat, p.Lng, z.Lat, z.Lng))
		if !bl.HideDistance {
			fmt.Fprintf(b, ", %.0f m", d)
		}
		b.WriteString(".\n")
	case graph.BlockRadar:
		if p == nil {
			b.WriteString("A radar, waiting for your position.\n")
			return
		}
		d := session.HaversineMeters(p.Lat, p.Lng, z.Lat, z.Lng)
		temp := "cold"
		switch {
		case d < 30:
			temp = "burning hot"
		case d < 100:
			temp = "hot"
		case d < 300:
			temp = "warm"
		}
		fmt.Fprintf(b, "A radar pulses %s.\n", temp)
	}
}

// input renders the answer box and how to answer it.
func (ps *playSession) input(b *strings.Builder, v session.View) {
	in := v.Input
	if in == nil {
		switch v.Type {
		case graph.Story:
			b.WriteString("\n→ continue to go on.\n")
		}
		return
	}
	b.WriteString("\n")
	switch in.Kind {
	case "scan":
		b.WriteString("→ Scan a QR code (answer with the code's text). The code is on site; you cannot see it from here.\n")
	case "tiles":
		n := in.Rows * in.Cols
		fmt.Fprintf(b, "→ A picture cut into %d×%d tiles to put in order. Once you have it, answer \"0,1,…,%d\".\n", in.Rows, in.Cols, n-1)
	case "search":
		b.WriteString("→ Say \"We're here\" where you stand (answer with an empty guess). Walk to the place first.")
		if in.At != "" {
			fmt.Fprintf(b, " You are at %s.", in.At)
		}
		b.WriteString("\n")
	case "choice":
		b.WriteString("→ Pick an option for each question, answer with the values joined by |:\n")
		for _, f := range in.Fields {
			var opts []string
			for _, o := range f.Options {
				if o.Label != "" && o.Label != o.Value {
					opts = append(opts, fmt.Sprintf("%s (%s)", o.Value, o.Label))
				} else {
					opts = append(opts, o.Value)
				}
			}
			fmt.Fprintf(b, "  %s: %s\n", f.Label, strings.Join(opts, ", "))
		}
	case "lock":
		var syms []string
		for _, s := range in.Symbols {
			if s.Label != "" && s.Label != s.Value {
				syms = append(syms, fmt.Sprintf("%s (%s)", s.Value, s.Label))
			} else {
				syms = append(syms, s.Value)
			}
		}
		fmt.Fprintf(b, "→ A lock of %d wheels, each turning through: %s. Answer with the values joined by |.\n", in.Wheels, strings.Join(syms, ", "))
	case "questions":
		b.WriteString("→ Answer the questions above (pick), then answer with an empty guess.\n")
	case "keys":
		b.WriteString("→ A two-key launch: every player turns their key at once (turn)")
		if len(in.Posts) > 0 {
			posts := slices.Clone(in.Posts)
			for i, role := range in.PostRoles {
				if role != "" {
					posts[i] += " (the " + role + ")"
				}
			}
			fmt.Fprintf(b, ", one at each post: %s. Here the team moves as one, so it holds only posts where it stands; the event simulation splits teams up to turn keys apart", strings.Join(posts, ", "))
		}
		b.WriteString(".\n")
	case "items":
		fmt.Fprintf(b, "→ Put an item from the backpack in each slot (%s); answer with item slugs joined by |.\n", strings.Join(in.Slots, " | "))
		if in.Missing {
			b.WriteString("  You are missing something it needs")
			if len(in.MissingItems) > 0 {
				fmt.Fprintf(b, ": %s", strings.Join(in.MissingItems, ", "))
			}
			b.WriteString(".\n")
		}
		if in.Leave {
			fmt.Fprintf(b, "  You can go and find it (leave)%s.\n", labelled(in.LeaveLabel))
		}
	default:
		b.WriteString("→ Type an answer")
		if in.Placeholder != "" {
			fmt.Fprintf(b, " (%s)", in.Placeholder)
		}
		if in.Parts > 1 {
			fmt.Fprintf(b, "; it has %d parts", in.Parts)
		}
		if in.Kind == "number" || in.Kind == "numeric" {
			b.WriteString("; numbers")
		}
		b.WriteString(".\n")
	}
	if v.Attempts > 0 {
		fmt.Fprintf(b, "  Tries so far: %d.\n", v.Attempts)
	}
}

// offer renders a hub's branches.
func (ps *playSession) offer(b *strings.Builder, v session.View) {
	if len(v.Offer) == 0 && !v.CanFinish {
		return
	}
	b.WriteString("\nWhere next (choose port):\n")
	for _, o := range v.Offer {
		label := o.Label
		if label == "" {
			label = o.Port
		}
		fmt.Fprintf(b, "- %q: %s", o.Port, label)
		if o.Note != "" {
			fmt.Fprintf(b, " — %s", o.Note)
		}
		if o.Locked {
			b.WriteString(" (locked)")
		}
		if o.Visited {
			b.WriteString(" (visited)")
		}
		b.WriteString("\n")
	}
	if v.CanFinish {
		b.WriteString("- \"done\": move on\n")
	}
}

// places lists where the team could walk: every place in the adventure,
// nearest first. A team on site sees the streets; it does not know which
// is the right one.
func (ps *playSession) places(b *strings.Builder) {
	if len(ps.g.Zones) == 0 {
		return
	}
	type near struct {
		z graph.Zone
		d float64
	}
	var all []near
	for _, z := range ps.g.Zones {
		all = append(all, near{z, ps.distance(z)})
	}
	slices.SortStableFunc(all, func(a, c near) int { return int(a.d - c.d) })
	if len(all) > 40 {
		all = all[:40]
	}
	named := map[string]int{}
	for _, z := range ps.g.Zones {
		named[zoneLabel(z)]++
	}
	b.WriteString("\nPlaces (move):")
	for _, n := range all {
		label := zoneLabel(n.z)
		if named[label] > 1 {
			// Two places of one name: the slug tells them apart.
			label += " [" + n.z.Slug + "]"
		}
		if ps.position() == nil {
			fmt.Fprintf(b, " %s;", label)
		} else {
			fmt.Fprintf(b, " %s %.0f m;", label, n.d)
		}
	}
	b.WriteString("\n")
}

// images fetches pictures as image content, small enough for a model.
func (ps *playSession) images(ctx context.Context, s *Server, ids []int64) []ToolContent {
	var out []ToolContent
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] || len(out) >= maxPlayImages {
			continue
		}
		seen[id] = true
		key := strconv.FormatInt(id, 10)
		status, raw, err := callAdmin(ctx, s.Admin.ServeMedia, "GET", "/media/"+key, map[string]string{"mediaID": key}, nil, nil)
		if err != nil || status != http.StatusOK || !strings.HasPrefix(http.DetectContentType(raw), "image/") {
			continue
		}
		small, mime, err := imageutil.Shrink(raw, playImageDim)
		if err != nil {
			continue
		}
		out = append(out, ToolContent{Type: "image", Data: base64.StdEncoding.EncodeToString(small), MimeType: mime})
	}
	return out
}

func labelled(s string) string {
	if s == "" {
		return ""
	}
	return ": " + s
}

// clock is a duration as the phone shows one.
func clock(secs float64) string {
	s := int(math.Max(0, math.Round(secs)))
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s%3600/60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// heading is the compass direction from one point to another.
func heading(lat1, lng1, lat2, lng2 float64) string {
	φ1, φ2 := lat1*math.Pi/180, lat2*math.Pi/180
	dλ := (lng2 - lng1) * math.Pi / 180
	y := math.Sin(dλ) * math.Cos(φ2)
	x := math.Cos(φ1)*math.Sin(φ2) - math.Sin(φ1)*math.Cos(φ2)*math.Cos(dλ)
	deg := math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360)
	names := []string{"north", "north-east", "east", "south-east", "south", "south-west", "west", "north-west"}
	return names[int(math.Round(deg/45))%8]
}

var morseCode = map[rune]string{
	'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..", 'E': ".", 'F': "..-.", 'G': "--.", 'H': "....", 'I': "..", 'J': ".---",
	'K': "-.-", 'L': ".-..", 'M': "--", 'N': "-.", 'O': "---", 'P': ".--.", 'Q': "--.-", 'R': ".-.", 'S': "...", 'T': "-",
	'U': "..-", 'V': "...-", 'W': ".--", 'X': "-..-", 'Y': "-.--", 'Z': "--..", 'Å': ".--.-", 'Ä': ".-.-", 'Ö': "---.",
	'0': "-----", '1': ".----", '2': "..---", '3': "...--", '4': "....-", '5': ".....", '6': "-....", '7': "--...", '8': "---..", '9': "----.",
}

// morse is text as the flasher sends it: letters apart, words by " / ".
func morse(text string) string {
	var words []string
	for _, w := range strings.Fields(strings.ToUpper(text)) {
		var letters []string
		for _, r := range w {
			if c, ok := morseCode[r]; ok {
				letters = append(letters, c)
			}
		}
		words = append(words, strings.Join(letters, " "))
	}
	return strings.Join(words, " / ")
}

// scramble is a letters block's tiles in a fixed shuffled order, so the
// same block reads the same each look.
func scramble(seed, text string) []string {
	var tiles []string
	for _, r := range strings.ToUpper(text) {
		if r != ' ' {
			tiles = append(tiles, string(r))
		}
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed + text))
	x := h.Sum64() | 1
	for i := len(tiles) - 1; i > 0; i-- {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		j := int(x % uint64(i+1))
		tiles[i], tiles[j] = tiles[j], tiles[i]
	}
	return tiles
}
