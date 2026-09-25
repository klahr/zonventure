package play

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graphstore"
)

// --- fixtures --------------------------------------------------------

// clock is a controllable time source, so hint delays, lockouts and time
// limits are tested by moving time rather than by sleeping.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// fixedLocator puts each team wherever the test says.
type fixedLocator struct {
	mu  sync.Mutex
	pos map[int64][2]float64
}

func (l *fixedLocator) Position(_ context.Context, _ Querier, team int64) (float64, float64, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.pos[team]
	return p[0], p[1], ok, nil
}

func (l *fixedLocator) put(team int64, lat, lng float64) {
	l.mu.Lock()
	if l.pos == nil {
		l.pos = map[int64][2]float64{}
	}
	l.pos[team] = [2]float64{lat, lng}
	l.mu.Unlock()
}

type fixture struct {
	t       *testing.T
	db      *sql.DB
	eng     *Engine
	clock   *clock
	loc     *fixedLocator
	eventID int64
	teams   []int64
}

var seq atomic.Int64

// setup saves g as an adventure's graph, creates an event with n teams,
// and starts it.
func setup(t *testing.T, g *graph.Graph, n int) *fixture {
	t.Helper()
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)

	id := seq.Add(1)
	slug := fmt.Sprintf("graph-test-%d", id)

	var orgID, advID int64
	mustScan(t, db.QueryRow(`INSERT INTO organizations(name) VALUES ($1) RETURNING id`, slug), &orgID)
	// The adventure's language is the graph's: publishing writes it in.
	mustScan(t, db.QueryRow(
		`INSERT INTO adventures(slug, title, description, author, language, org_id) VALUES ($1, $1, '', '', COALESCE(NULLIF($2, ''), 'en'), $3) RETURNING id`,
		slug, g.Language, orgID), &advID)

	if _, _, err := graphstore.Publish(context.Background(), db, advID, 0, g, ""); err != nil {
		t.Fatalf("saving graph: %v", err)
	}

	f := &fixture{t: t, db: db, clock: &clock{t: time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)}, loc: &fixedLocator{}}
	f.eng = &Engine{DB: db, Now: f.clock.Now, Locator: f.loc}

	mustScan(t, db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, started_at, live_token, name, show_map)
		VALUES ($1, $2, 'running', now_text(), $3, $1, 0) RETURNING id
	`, slug, orgID, "tok-"+slug), &f.eventID)

	for i := range n {
		var team int64
		mustScan(t, db.QueryRow(`INSERT INTO teams(event_id, name, token) VALUES ($1, $2, $3) RETURNING id`,
			f.eventID, fmt.Sprintf("team-%d", i), fmt.Sprintf("tok-%s-%d", slug, i)), &team)
		if _, err := db.Exec(`INSERT INTO player_slots(team_id, name, token) VALUES ($1, 'p', $2)`,
			team, fmt.Sprintf("slot-%s-%d", slug, i)); err != nil {
			t.Fatal(err)
		}
		f.teams = append(f.teams, team)
	}

	if err := f.eng.StartEvent(context.Background(), f.eventID); err != nil {
		t.Fatalf("StartEvent: %v", err)
	}
	return f
}

func mustScan(t *testing.T, row *sql.Row, dst ...any) {
	t.Helper()
	if err := row.Scan(dst...); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) view(team int64) View {
	f.t.Helper()
	v, err := f.eng.Snapshot(context.Background(), team)
	if err != nil {
		f.t.Fatalf("Snapshot: %v", err)
	}
	return v
}

func (f *fixture) answer(team int64, guess string) Result {
	f.t.Helper()
	r, err := f.eng.Answer(context.Background(), team, f.view(team).Node, guess)
	if err != nil {
		f.t.Fatalf("Answer(%q) at %s: %v", guess, f.view(team).Node, err)
	}
	return r
}

// --- graph builders --------------------------------------------------

func puzzle(slug, answer string, extra ...graph.Output) graph.Node {
	outs := []graph.Output{{
		Port: "solved", Trigger: graph.OnAnswer, Points: 10,
		Matchers: []match.Matcher{{Kind: match.Exact, Value: answer}},
	}}
	return graph.Node{Slug: slug, Type: graph.Puzzle, Title: slug,
		PuzzleCfg: &graph.PuzzleConfig{Blocks: graph.LegacyBlocks("?", nil), Outputs: append(outs, extra...)}}
}

func edge(from, port, to string) graph.Edge { return graph.Edge{From: from, FromPort: port, To: to} }

func linear(answers ...string) *graph.Graph {
	g := &graph.Graph{Nodes: []graph.Node{{Slug: "start", Type: graph.Start}, {Slug: "end", Type: graph.End}}}
	prev, port := "start", graph.PortOut
	for i, a := range answers {
		slug := fmt.Sprintf("p%d", i+1)
		g.Nodes = append(g.Nodes, puzzle(slug, a))
		g.Edges = append(g.Edges, edge(prev, port, slug))
		prev, port = slug, "solved"
	}
	g.Edges = append(g.Edges, edge(prev, port, "end"))
	return g
}

// --- tests -----------------------------------------------------------

func TestLinearAdventureEndToEnd(t *testing.T) {
	g := linear("red", "blue")
	// Nodes are [start, end, p1, p2]; the ladder goes on p2, where the
	// wrong guesses are made.
	g.Nodes[3].PuzzleCfg.Responses = []graph.Response{
		{AfterAttempts: 1, Text: "Not quite."},
		{AfterAttempts: 2, Text: "Think of the sky."},
	}
	f := setup(t, g, 1)
	team := f.teams[0]

	if v := f.view(team); v.Node != "p1" || v.Type != graph.Puzzle {
		t.Fatalf("team should start on p1, got %+v", v)
	}

	f.answer(team, "red")

	// The wrong-answer ladder: the first wrong guess reads differently
	// from the second, and neither moves the team.
	r := f.answer(team, "green")
	if r.Accepted || r.Response != "Not quite." {
		t.Errorf("first wrong guess: accepted=%v response=%q", r.Accepted, r.Response)
	}
	r = f.answer(team, "yellow")
	if r.Response != "Think of the sky." {
		t.Errorf("second wrong guess response = %q", r.Response)
	}
	if r.View.Node != "p2" || r.View.Attempts != 2 {
		t.Errorf("a wrong guess must not move the team: node=%q attempts=%d", r.View.Node, r.View.Attempts)
	}

	r = f.answer(team, "BLUE")
	if !r.Accepted || !r.View.Finished || r.View.Outcome != graph.Complete {
		t.Fatalf("expected the run to finish, got %+v", r)
	}
	if r.View.Score != 20 {
		t.Errorf("score = %v, want 20 for two 10-point puzzles", r.View.Score)
	}

	// A finished team cannot act again.
	if _, err := f.eng.Answer(context.Background(), team, "end", "x"); !errors.Is(err, ErrFinished) {
		t.Errorf("acting after finishing: got %v, want ErrFinished", err)
	}
}

// Answering a node the team has already left must be refused, never
// applied to whatever it is on now — that would answer a question the
// player has never seen.
func TestStaleActionIsRefused(t *testing.T) {
	f := setup(t, linear("a", "b"), 1)
	team := f.teams[0]
	f.answer(team, "a") // now on p2

	_, err := f.eng.Answer(context.Background(), team, "p1", "b")
	if !errors.Is(err, ErrStale) {
		t.Fatalf("answering the previous node: got %v, want ErrStale", err)
	}
	if v := f.view(team); v.Node != "p2" {
		t.Errorf("a stale answer must not move the team; now on %q", v.Node)
	}
}

// Two teammates submitting the right answer at the same moment: exactly
// one advances the team, the other is told it already moved. Without
// the row lock and the cursor compare-and-swap, both would advance and
// the team would skip a puzzle.
func TestConcurrentTeammatesAdvanceOnce(t *testing.T) {
	f := setup(t, linear("a", "b", "c"), 1)
	team := f.teams[0]

	const n = 8
	var wg sync.WaitGroup
	var ok, stale atomic.Int32
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.eng.Answer(context.Background(), team, "p1", "a")
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrStale):
				stale.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if ok.Load() != 1 || stale.Load() != n-1 {
		t.Errorf("got %d successes and %d stale; want exactly 1 and %d", ok.Load(), stale.Load(), n-1)
	}
	if v := f.view(team); v.Node != "p2" {
		t.Errorf("the team should have advanced exactly one node, to p2; it is on %q", v.Node)
	}
}

// The physically visible race: many teams reaching a capacity hub at the
// same moment must never over-fill a station. Six teams, three stations
// of two: every station gets exactly two.
func TestCapacityIsNeverExceededUnderConcurrency(t *testing.T) {
	hub := graph.Node{Slug: "hub", Type: graph.Hub, HubCfg: &graph.HubConfig{
		Policy: graph.PolicyCapacity, Required: 1,
		Branches: []graph.Branch{{Port: "a", Capacity: 2}, {Port: "b", Capacity: 2}, {Port: "c", Capacity: 2}},
	}}
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start}, puzzle("gate", "go"), hub,
			puzzle("st-a", "x"), puzzle("st-b", "x"), puzzle("st-c", "x"),
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "gate"),
			edge("gate", "solved", "hub"),
			edge("hub", "a", "st-a"), edge("hub", "b", "st-b"), edge("hub", "c", "st-c"),
			edge("st-a", "solved", "hub"), edge("st-b", "solved", "hub"), edge("st-c", "solved", "hub"),
			edge("hub", graph.PortDone, "end"),
		},
	}
	f := setup(t, g, 6)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, team := range f.teams {
		wg.Add(1)
		go func(team int64) {
			defer wg.Done()
			<-start
			if _, err := f.eng.Answer(context.Background(), team, "gate", "go"); err != nil {
				t.Errorf("team %d: %v", team, err)
			}
		}(team)
	}
	close(start)
	wg.Wait()

	counts := map[string]int{}
	for _, team := range f.teams {
		counts[f.view(team).Node]++
	}
	for _, st := range []string{"st-a", "st-b", "st-c"} {
		if counts[st] != 2 {
			t.Errorf("station occupancy = %v; want exactly 2 at each station", counts)
			break
		}
	}

	// Leaving a station frees its place.
	var occupied int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM team_hub_branches WHERE event_id = $1 AND returned_at IS NULL`,
		f.eventID).Scan(&occupied); err != nil {
		t.Fatal(err)
	}
	if occupied != 6 {
		t.Errorf("occupied places = %d, want 6", occupied)
	}
	team := f.teams[0]
	f.answer(team, "x")
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM team_hub_branches WHERE event_id = $1 AND returned_at IS NULL`,
		f.eventID).Scan(&occupied); err != nil {
		t.Fatal(err)
	}
	if occupied != 5 {
		t.Errorf("after one team left its station, occupied = %d, want 5", occupied)
	}
}

// The scenario from the plan, on a real database: a choice hub, a gate
// on history with nothing granted, paths that diverge and reconverge,
// and a revisit that goes the way the team left last time.
func TestBranchingAdventureOnPostgres(t *testing.T) {
	hub := graph.Node{Slug: "hub", Type: graph.Hub, HubCfg: &graph.HubConfig{
		Policy: graph.PolicyChoice, Required: 1,
		Branches: []graph.Branch{{Port: "church", Label: "Church"}, {Port: "market", Label: "Market"}},
	}}
	gate := graph.Node{Slug: "has-code", Type: graph.Gate, GateCfg: &graph.GateConfig{
		Condition: cond.Predicate{Op: cond.OpSolved, Node: "task-church"},
	}}
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			puzzle("statue", "horse"), hub,
			puzzle("task-church", "bell"), puzzle("task-market", "fish"),
			gate, puzzle("vault", "open"),
			{Slug: "hint", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("You need the church code.", nil)}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Blocks: graph.LegacyBlocks("Well done.", nil)}},
		},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "statue"),
			edge("statue", "solved", "hub"),
			edge("hub", "church", "task-church"), edge("hub", "market", "task-market"),
			edge("task-church", "solved", "hub"), edge("task-market", "solved", "hub"),
			edge("hub", graph.PortDone, "has-code"),
			edge("has-code", graph.PortPass, "vault"), edge("has-code", graph.PortFail, "hint"),
			edge("hint", graph.PortOut, "hub"),
			edge("vault", "solved", "end"),
		},
	}
	f := setup(t, g, 1)
	team := f.teams[0]
	ctx := context.Background()

	r := f.answer(team, "horse")
	if r.View.Node != "hub" || len(r.View.Offer) != 2 {
		t.Fatalf("expected a 2-way offer at the hub, got %+v", r.View)
	}

	if _, err := f.eng.Choose(ctx, team, "hub", "market"); err != nil {
		t.Fatalf("Choose(market): %v", err)
	}
	r = f.answer(team, "fish")
	if !r.View.CanFinish || len(r.View.Offer) != 1 || r.View.Offer[0].Port != "church" {
		t.Fatalf("after one branch the team may move on or do the church: %+v", r.View)
	}

	// Move on without the code: the gate reads history and says no.
	r, err := f.eng.Choose(ctx, team, "hub", graph.PortDone)
	if err != nil {
		t.Fatalf("Choose(done): %v", err)
	}
	if r.View.Node != "hint" || r.View.Text != "You need the church code." {
		t.Fatalf("without the church the gate should show the hint, got %+v", r.View)
	}

	if _, err := f.eng.Continue(ctx, team, "hint"); err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if _, err := f.eng.Choose(ctx, team, "hub", "church"); err != nil {
		t.Fatalf("Choose(church): %v", err)
	}
	// With both branches done there is nothing left to offer, so the hub
	// leaves on its own — no "move on" choice — and the gate, now reading
	// a solved church, opens the vault.
	r = f.answer(team, "bell")
	if r.View.Node != "vault" {
		t.Fatalf("with the church solved the gate should open the vault; on %q", r.View.Node)
	}
	if _, err := f.eng.Choose(ctx, team, "hub", graph.PortDone); !errors.Is(err, ErrStale) {
		t.Errorf("acting on the hub after leaving it: got %v, want ErrStale", err)
	}

	r = f.answer(team, "open")
	if !r.View.Finished || r.View.Text != "Well done." {
		t.Errorf("expected to finish on the end node's text, got %+v", r.View)
	}
	if r.View.Score != 40 {
		t.Errorf("score = %v, want 40 for four 10-point puzzles", r.View.Score)
	}
}

func TestZoneGate(t *testing.T) {
	g := linear("x")
	g.Zones = []graph.Zone{{Slug: "square", Lat: 59.3293, Lng: 18.0686, RadiusM: 30}}
	g.Nodes[2].PuzzleCfg.Zone = "square"
	f := setup(t, g, 1)
	team := f.teams[0]
	ctx := context.Background()

	// No position at all: refused.
	if _, err := f.eng.Answer(ctx, team, "p1", "x"); !errors.Is(err, ErrNotHere) {
		t.Errorf("with no position: got %v, want ErrNotHere", err)
	}
	// ~110 m north: still outside.
	f.loc.put(team, 59.3303, 18.0686)
	if _, err := f.eng.Answer(ctx, team, "p1", "x"); !errors.Is(err, ErrNotHere) {
		t.Errorf("outside the zone: got %v, want ErrNotHere", err)
	}
	// Inside.
	f.loc.put(team, 59.3294, 18.0686)
	if r, err := f.eng.Answer(ctx, team, "p1", "x"); err != nil || !r.Accepted {
		t.Errorf("inside the zone: got %+v, %v", r, err)
	}
}

// "Correct only if you are NOT here": leave the courtyard, then answer.
func TestInvertedZoneOutput(t *testing.T) {
	g := linear("x")
	g.Zones = []graph.Zone{{Slug: "courtyard", Lat: 59.0, Lng: 18.0, RadiusM: 50}}
	g.Nodes[2].PuzzleCfg.Outputs[0].Zone = "courtyard"
	g.Nodes[2].PuzzleCfg.Outputs[0].Inverted = true
	f := setup(t, g, 1)
	team := f.teams[0]

	f.loc.put(team, 59.0, 18.0) // inside
	if r := f.answer(team, "x"); r.Accepted {
		t.Error("the right answer given inside the courtyard must not count")
	}
	f.loc.put(team, 59.01, 18.0) // ~1 km away
	if r := f.answer(team, "x"); !r.Accepted {
		t.Error("the right answer given outside the courtyard should count")
	}
}

func TestPartialCreditIsScored(t *testing.T) {
	g := linear("unused")
	g.Nodes[2].PuzzleCfg.Outputs[0].Points = 100
	g.Nodes[2].PuzzleCfg.Outputs[0].Matchers = []match.Matcher{{Kind: match.Near, Target: 1500, Tolerance: 300}}
	f := setup(t, g, 1)

	r := f.answer(f.teams[0], "1650")
	if !r.Accepted || r.Points != 50 {
		t.Errorf("1650 against 1500±300 should earn 50 of 100 points, got %+v", r)
	}
}

func TestLockoutAfterWrongAnswer(t *testing.T) {
	g := linear("right")
	g.Nodes[2].PuzzleCfg.LockoutSeconds = 30
	f := setup(t, g, 1)
	team := f.teams[0]
	ctx := context.Background()

	if r := f.answer(team, "wrong"); r.BlockedUntil == nil {
		t.Fatal("a wrong answer should start a lockout")
	}
	if _, err := f.eng.Answer(ctx, team, "p1", "right"); !errors.Is(err, ErrLocked) {
		t.Errorf("answering during the lockout: got %v, want ErrLocked", err)
	}
	f.clock.Advance(31 * time.Second)
	if r, err := f.eng.Answer(ctx, team, "p1", "right"); err != nil || !r.Accepted {
		t.Errorf("after the lockout the right answer should count: %+v, %v", r, err)
	}
}

func TestHintsRevealInOrderAfterTheirDelay(t *testing.T) {
	g := linear("x")
	g.Nodes[2].PuzzleCfg.Hints = []graph.Hint{
		{Text: "first", DelaySeconds: 60, Cost: 5},
		{Text: "second", DelaySeconds: 120, Cost: 5},
	}
	f := setup(t, g, 1)
	team := f.teams[0]
	ctx := context.Background()

	if _, err := f.eng.RevealHint(ctx, team, "p1", 0); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("before its delay a hint must not reveal: %v", err)
	}
	f.clock.Advance(61 * time.Second)
	// Skipping ahead to the more useful hint is refused.
	if _, err := f.eng.RevealHint(ctx, team, "p1", 1); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("revealing out of order must be refused: %v", err)
	}
	r, err := f.eng.RevealHint(ctx, team, "p1", 0)
	if err != nil || r.Hint != "first" {
		t.Fatalf("RevealHint(0) = %+v, %v", r, err)
	}
	if r.View.PenaltyScore != 5 || len(r.View.Hints) != 1 {
		t.Errorf("after one hint: penalty=%v hints=%v", r.View.PenaltyScore, r.View.Hints)
	}
}

// Skipping needs both thresholds. Spamming guesses alone must not work.
func TestSkipNeedsBothThresholds(t *testing.T) {
	g := linear("x")
	cfg := g.Nodes[2].PuzzleCfg
	cfg.SkipAfterGuesses, cfg.SkipAfterSeconds = 2, 300
	cfg.Outputs = append(cfg.Outputs, graph.Output{Port: "skipped", Trigger: graph.OnSkip})
	g.Edges = append(g.Edges, edge("p1", "skipped", "end"))
	f := setup(t, g, 1)
	team := f.teams[0]
	ctx := context.Background()

	f.answer(team, "no")
	f.answer(team, "nope")
	if _, err := f.eng.Skip(ctx, team, "p1"); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("enough guesses but not enough time must not allow a skip: %v", err)
	}
	f.clock.Advance(301 * time.Second)
	r, err := f.eng.Skip(ctx, team, "p1")
	if err != nil || !r.View.Finished {
		t.Errorf("both thresholds met should allow the skip: %+v, %v", r, err)
	}
}

func TestTimeoutPath(t *testing.T) {
	g := linear("x")
	cfg := g.Nodes[2].PuzzleCfg
	cfg.TimeLimitSeconds = 60
	cfg.Outputs = append(cfg.Outputs, graph.Output{Port: "late", Trigger: graph.OnTimeout})
	g.Nodes = append(g.Nodes, graph.Node{Slug: "too-late", Type: graph.End,
		EndCfg: &graph.EndConfig{Outcome: graph.Failed}})
	g.Edges = append(g.Edges, edge("p1", "late", "too-late"))
	f := setup(t, g, 1)
	team := f.teams[0]
	ctx := context.Background()

	if _, err := f.eng.Timeout(ctx, team, "p1"); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("a timeout before the limit must be refused: %v", err)
	}
	f.clock.Advance(61 * time.Second)
	if _, err := f.eng.Answer(ctx, team, "p1", "x"); !errors.Is(err, ErrTimeUp) {
		t.Errorf("answering after the limit: got %v, want ErrTimeUp", err)
	}
	r, err := f.eng.Timeout(ctx, team, "p1")
	if err != nil || r.View.Outcome != graph.Failed {
		t.Errorf("the timeout path should end in failure: %+v, %v", r, err)
	}
}

// Effects persist, and an effect is visible to a gate later in the same
// step: +50 on a team at 10 must clear a 50-point gate.
func TestEffectsPersistAndAreVisibleToGates(t *testing.T) {
	lo, hi := 0.0, 3.0
	g := &graph.Graph{
		Items:    []graph.Item{{Slug: "crowbar", Name: "Crowbar"}},
		Counters: []graph.Counter{{Slug: "alarm", Min: &lo, Max: &hi}},
		Journal:  []graph.JournalEntry{{Slug: "letter", Title: "A letter"}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			puzzle("p1", "go"),
			{Slug: "fx-item", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.GrantItem, Item: "crowbar"}},
			{Slug: "fx-score", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.AdjustScore, Points: 50}},
			{Slug: "fx-alarm", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.SetCounter, Counter: "alarm", Value: 10}},
			{Slug: "fx-letter", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.UnlockJournal, JournalEntry: "letter"}},
			{Slug: "fx-mail", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.SendEmail, Subject: "Welcome", Body: "Hello {{team}}"}},
			{Slug: "rich", Type: graph.Gate, GateCfg: &graph.GateConfig{Condition: cond.Predicate{Op: cond.OpScore, N: 50}}},
			puzzle("vault", "x"), puzzle("poor", "x"),
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "p1"),
			edge("p1", "solved", "fx-item"),
			edge("fx-item", graph.PortOut, "fx-score"),
			edge("fx-score", graph.PortOut, "fx-alarm"),
			edge("fx-alarm", graph.PortOut, "fx-letter"),
			edge("fx-letter", graph.PortOut, "fx-mail"),
			edge("fx-mail", graph.PortOut, "rich"),
			edge("rich", graph.PortPass, "vault"), edge("rich", graph.PortFail, "poor"),
			edge("vault", "solved", "end"), edge("poor", "solved", "end"),
		},
	}
	f := setup(t, g, 1)
	team := f.teams[0]

	r := f.answer(team, "go")
	if r.View.Node != "vault" {
		t.Fatalf("10 + 50 should clear the 50-point gate in the same step; landed on %q", r.View.Node)
	}
	if len(r.View.Items) != 1 || r.View.Items[0].Slug != "crowbar" || r.View.Items[0].Qty != 1 {
		t.Errorf("items = %v, want a crowbar", r.View.Items)
	}
	if len(r.View.Journal) != 1 || r.View.Journal[0].Slug != "letter" || r.View.Journal[0].Title != "A letter" {
		t.Errorf("journal = %v, want the letter", r.View.Journal)
	}

	var alarm float64
	mustScan(t, f.db.QueryRow(`SELECT value FROM event_counters WHERE event_id = $1 AND counter = 'alarm'`, f.eventID), &alarm)
	if alarm != 3 {
		t.Errorf("counter = %v; setting 10 on a counter capped at 3 should clamp to 3", alarm)
	}

	// Email is queued, never sent inline, so a rolled-back move cannot
	// have sent anything.
	var queued int
	mustScan(t, f.db.QueryRow(`SELECT COUNT(*) FROM graph_effect_outbox WHERE team_id = $1 AND kind = 'email'`, team), &queued)
	if queued != 1 {
		t.Errorf("queued emails = %d, want 1", queued)
	}
}

func TestStartEventIsIdempotentAndRequiresAGraph(t *testing.T) {
	f := setup(t, linear("a"), 2)
	ctx := context.Background()

	f.answer(f.teams[0], "a")
	// A second start must not reset a team that is already playing.
	if err := f.eng.StartEvent(ctx, f.eventID); err != nil {
		t.Fatalf("second StartEvent: %v", err)
	}
	if v := f.view(f.teams[0]); !v.Finished {
		t.Error("restarting the event must not reset a team's progress")
	}

	// An event whose adventure has no graph cannot start on this engine.
	var orgID, eventID int64
	mustScan(t, f.db.QueryRow(`INSERT INTO organizations(name) VALUES ('no-graph-org') RETURNING id`), &orgID)
	if _, err := f.db.Exec(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('no-graph', 'x', '', '', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	mustScan(t, f.db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name, show_map)
		VALUES ('no-graph', $1, 'running', 'tok-no-graph', 'x', 0) RETURNING id
	`, orgID), &eventID)
	if err := f.eng.StartEvent(ctx, eventID); !errors.Is(err, ErrNoGraph) {
		t.Errorf("starting without a graph: got %v, want ErrNoGraph", err)
	}
}

// An author editing mid-event must not move the ground under a team
// already playing: the event keeps the version it started with.
func TestEventKeepsItsPinnedGraph(t *testing.T) {
	f := setup(t, linear("a", "b"), 1)
	ctx := context.Background()

	var advID int64
	mustScan(t, f.db.QueryRow(`
		SELECT a.id FROM adventures a JOIN events e ON e.adventure_slug = a.slug WHERE e.id = $1
	`, f.eventID), &advID)
	if _, _, err := graphstore.Publish(ctx, f.db, advID, 0, linear("changed"), ""); err != nil {
		t.Fatalf("saving a new version: %v", err)
	}

	// The running team still plays the original: "a" is still right.
	if r := f.answer(f.teams[0], "a"); !r.Accepted {
		t.Error("the event should still run the graph it was started with")
	}
}

// The anti-teleport rule: two pings more than 120 s apart mean the latest
// is not trusted, even if it is inside the zone.
func TestPingLocatorRejectsTeleports(t *testing.T) {
	f := setup(t, linear("a"), 1)
	team := f.teams[0]
	ctx := context.Background()

	var slotID int64
	mustScan(t, f.db.QueryRow(`SELECT id FROM player_slots WHERE team_id = $1`, team), &slotID)
	ping := func(lat, lng float64, ts string) {
		if _, err := f.db.Exec(`
			INSERT INTO location_pings(slot_id, team_id, event_id, lat, lng, server_ts)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, slotID, team, f.eventID, lat, lng, ts); err != nil {
			t.Fatal(err)
		}
	}

	if _, _, ok, _ := (PingLocator{}).Position(ctx, f.db, team); ok {
		t.Error("with no pings there is no position")
	}

	ping(59.0, 18.0, "2026-09-22 10:00:00")
	if lat, _, ok, _ := (PingLocator{}).Position(ctx, f.db, team); !ok || lat != 59.0 {
		t.Error("a single ping should be trusted")
	}

	ping(59.1, 18.0, "2026-09-22 10:00:30") // 30 s later: plausible
	if lat, _, ok, _ := (PingLocator{}).Position(ctx, f.db, team); !ok || lat != 59.1 {
		t.Error("a ping 30 s after the last should be trusted")
	}

	ping(60.0, 18.0, "2026-09-22 10:05:00") // 270 s gap
	if _, _, ok, _ := (PingLocator{}).Position(ctx, f.db, team); ok {
		t.Error("a ping after a 270 s gap must not be trusted")
	}
}

// The answer survives the database between actions: a switch several
// steps later still reads the first puzzle's answer after a reload.
func TestSwitchReadsAnAnswerFromEarlier(t *testing.T) {
	anything := []match.Matcher{{Kind: match.Anything}}
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "door", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Outputs: []graph.Output{{Port: "said", Trigger: graph.OnAnswer, Matchers: anything}},
			}},
			puzzle("middle", "ok"),
			{Slug: "which", Type: graph.Switch, SwitchCfg: &graph.SwitchConfig{From: "door", Cases: []graph.Case{
				{Port: "left", Matchers: []match.Matcher{{Kind: match.Exact, Value: "left"}}},
			}}},
			{Slug: "left-end", Type: graph.End},
			{Slug: "else-end", Type: graph.End},
		},
		Edges: []graph.Edge{
			edge("start", "out", "door"),
			edge("door", "said", "middle"),
			edge("middle", "solved", "which"),
			edge("which", "left", "left-end"),
			edge("which", "else", "else-end"),
		},
	}
	f := setup(t, g, 2)
	f.answer(f.teams[0], "Left")
	f.answer(f.teams[1], "right")
	if r := f.answer(f.teams[0], "ok"); r.View.Node != "left-end" {
		t.Errorf("left should route left, got %q", r.View.Node)
	}
	if r := f.answer(f.teams[1], "ok"); r.View.Node != "else-end" {
		t.Errorf("right should route otherwise, got %q", r.View.Node)
	}
}

// Choosing a start survives the database: the team waits unplaced after
// the event starts, chooses, and its adventure clock starts then.
func TestTeamChoosesItsStart(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "s1", Type: graph.Start, Title: "East"},
			{Slug: "s2", Type: graph.Start, Title: "West"},
			puzzle("east", "e"),
			puzzle("west", "w"),
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			edge("s1", "out", "east"), edge("s2", "out", "west"),
			edge("east", "solved", "end"), edge("west", "solved", "end"),
		},
	}
	f := setup(t, g, 1)
	team := f.teams[0]
	if v := f.view(team); v.Node != "" || len(v.Starts) != 2 {
		t.Fatalf("before choosing: %+v", v)
	}
	f.clock.Advance(10 * time.Minute)
	r, err := f.eng.ChooseStart(context.Background(), team, "s2")
	if err != nil || r.View.Node != "west" {
		t.Fatalf("choose west: %+v %v", r.View, err)
	}
	if _, err := f.eng.ChooseStart(context.Background(), team, "s1"); !errors.Is(err, ErrStale) {
		t.Errorf("choosing twice = %v", err)
	}
	if v := f.view(team); v.Node != "west" {
		t.Errorf("after reload the team is on %q", v.Node)
	}
}

// A home screen, played live: a note unlocked on reaching a step, a
// branch left with Back and marked visited, and a second visit — all of
// it read back from the database, not from memory.
func TestHomeScreenLive(t *testing.T) {
	g := &graph.Graph{
		Journal: []graph.JournalEntry{{Slug: "carl", Title: "Carl"}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "home", Type: graph.Hub, Title: "Obscurium", HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Home: true, Branches: []graph.Branch{
				{Port: "carl", Label: "Carl"}, {Port: "accuse", Label: "Solve"},
			}}},
			{Slug: "carl", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("Carl.", nil), Once: true}, Unlocks: []string{"carl"}},
			puzzle("carl-q", "1997"),
			puzzle("accuse", "erika"),
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			edge("start", "out", "home"),
			edge("home", "carl", "carl"), edge("carl", "out", "carl-q"), edge("carl-q", "solved", "home"),
			edge("home", "accuse", "accuse"), edge("accuse", "solved", "end"),
		},
	}
	ctx := context.Background()
	f := setup(t, g, 1)
	team := f.teams[0]

	if _, err := f.eng.Choose(ctx, team, "home", "carl"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.Continue(ctx, team, "carl"); err != nil {
		t.Fatal(err)
	}
	v := f.view(team)
	if v.Node != "carl-q" || v.BackTo != "home" || len(v.Journal) != 1 {
		t.Fatalf("on Carl's question, with a way back and Carl noted: %+v", v)
	}
	if _, err := f.eng.Back(ctx, team, "carl-q"); err != nil {
		t.Fatal(err)
	}
	v = f.view(team)
	if v.Node != "home" || len(v.Offer) != 2 || !v.Offer[0].Visited || v.Offer[1].Visited {
		t.Fatalf("back home, Carl visited and still offered: %+v", v.Offer)
	}
	if _, err := f.eng.Choose(ctx, team, "home", "carl"); err != nil {
		t.Fatalf("a home branch can be entered again: %v", err)
	}
	if v := f.view(team); v.Node != "carl-q" {
		t.Errorf("the second visit skips the introduction read once, got %q", v.Node)
	}
}

// The time limit, live: past the deadline the team is moved once, and the
// database remembers it — a second action after the move acts normally.
func TestTimeLimitLive(t *testing.T) {
	g := &graph.Graph{
		TimeLimit: &graph.TimeLimit{Seconds: 600, To: "accuse", Text: "Time's up."},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			puzzle("clue", "x"),
			puzzle("accuse", "erika"),
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			edge("start", "out", "clue"), edge("clue", "solved", "accuse"), edge("accuse", "solved", "end"),
		},
	}
	ctx := context.Background()
	f := setup(t, g, 1)
	team := f.teams[0]
	if v := f.view(team); v.EndsAt == nil {
		t.Fatalf("a countdown is shown: %+v", v)
	}
	if _, err := f.eng.TimeUp(ctx, team); err == nil {
		t.Error("time-up before the deadline is refused")
	}
	f.clock.Advance(11 * time.Minute)
	r, err := f.eng.Answer(ctx, team, "clue", "wrong")
	if err != nil || !r.TimeUp || r.View.Node != "accuse" {
		t.Fatalf("past the deadline, the answer moves the team instead: %+v %v", r, err)
	}
	if v := f.view(team); v.Node != "accuse" || v.TimeUpText != "Time's up." {
		t.Fatalf("after reload, at the accusation with the message: %+v", v)
	}
	r, err = f.eng.Answer(ctx, team, "accuse", "erika")
	if err != nil || r.TimeUp || !r.View.Finished {
		t.Fatalf("it fired once; the next answer counts: %+v %v", r, err)
	}
}

// A team's language, live: chosen, remembered by the database, and used
// for the view and the replies.
func TestLanguageLive(t *testing.T) {
	g := &graph.Graph{
		Language: "sv",
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "q", Type: graph.Puzzle, Title: "Frågan", PuzzleCfg: &graph.PuzzleConfig{
				Blocks:    graph.LegacyBlocks("Hur många?", nil),
				Outputs:   []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "sex"}}}},
				Responses: []graph.Response{{Text: "Fel."}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{edge("start", "out", "q"), edge("q", "solved", "end")},
		Translations: map[string]map[string]string{"en": {
			graph.NodeKey("q", "text"):               "How many?",
			graph.NodeKey("q", "reply:0"):            "Wrong.",
			graph.NodeKey("q", "out:solved:answers"): "six",
		}},
	}
	ctx := context.Background()
	f := setup(t, g, 1)
	team := f.teams[0]
	if _, err := f.eng.SetLanguage(ctx, team, "fi"); err == nil {
		t.Error("an untranslated language is refused")
	}
	if _, err := f.eng.SetLanguage(ctx, team, "en"); err != nil {
		t.Fatal(err)
	}
	if v := f.view(team); v.Lang != "en" || v.Text != "How many?" {
		t.Fatalf("after reload, the view is in English: %+v", v)
	}
	if r, _ := f.eng.Answer(ctx, team, "q", "five"); r.Response != "Wrong." {
		t.Errorf("the reply is in English: %q", r.Response)
	}
	if r, err := f.eng.Answer(ctx, team, "q", "six"); err != nil || !r.View.Finished {
		t.Errorf("the English answer counts: %+v %v", r, err)
	}
}
