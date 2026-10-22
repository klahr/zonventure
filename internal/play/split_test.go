package play

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graphstore"
)

// heist splits at "split" into the scout's way (pa, which grants the
// key) and everyone else's (pb); they meet at "join", and the vault
// after it opens with the key. Paths with roles but no zones, so the
// fixture's locator — every phone at the team's position — will do.
func heist(roles bool) *graph.Graph {
	paths := []graph.SplitPath{{Port: "a"}, {Port: "b"}}
	g := &graph.Graph{
		Items: []graph.Item{{Slug: "key", Name: "Key"}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "split", Type: graph.Split, Title: "Split", SplitCfg: &graph.SplitConfig{Paths: paths}},
			puzzle("pa", "bell"),
			{Slug: "gift", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.GrantItem, Item: "key"}},
			puzzle("pb", "water"),
			{Slug: "join", Type: graph.Join, Title: "Square"},
			{Slug: "vault", Type: graph.Puzzle, Title: "Vault", PuzzleCfg: &graph.PuzzleConfig{
				Answer:  &graph.Answer{Kind: graph.AnswerItems, Slots: []graph.ItemSlot{{Item: "key"}}},
				Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Points: 50}},
			}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
		},
		Edges: []graph.Edge{
			edge("start", graph.PortOut, "split"),
			edge("split", "a", "pa"), edge("pa", "solved", "gift"), edge("gift", graph.PortOut, "join"),
			edge("split", "b", "pb"), edge("pb", "solved", "join"),
			edge("join", graph.PortOut, "vault"), edge("vault", "solved", "end"),
		},
	}
	if roles {
		g.Roles = []graph.Role{{Slug: "scout", Name: "Scout"}, {Slug: "crew", Name: "Crew", Default: true}}
		g.Nodes[1].SplitCfg.Paths[0].Role = "scout"
	}
	return g
}

// players adds n players who have opened their links to a team, besides
// the fixture's own (which never has), and returns their slots in order.
func (f *fixture) players(team int64, n int) []int64 {
	f.t.Helper()
	if _, err := f.db.Exec(`UPDATE player_slots SET name = '(forgotten)' WHERE team_id = $1`, team); err != nil {
		f.t.Fatal(err)
	}
	var out []int64
	for i := range n {
		var id int64
		mustScan(f.t, f.db.QueryRow(`INSERT INTO player_slots(team_id, name, token, last_login_at) VALUES ($1, $2, $3, now_text()) RETURNING id`,
			team, fmt.Sprintf("p%d", i), fmt.Sprintf("slot-%d-%d-%d", team, i, seq.Add(1))), &id)
		out = append(out, id)
	}
	return out
}

// strands reads a team's stored groups.
func (f *fixture) strands(team int64) (split sql.NullString, strands []session.Strand, current string) {
	f.t.Helper()
	var raw []byte
	mustScan(f.t, f.db.QueryRow(`SELECT split, strands, COALESCE(current_node, '') FROM team_graph_state WHERE team_id = $1`, team), &split, &raw, &current)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &strands); err != nil {
			f.t.Fatal(err)
		}
	}
	return
}

func phone(slot int64) context.Context { return ForPhone(context.Background(), slot) }

func (f *fixture) phoneView(team, slot int64) View {
	f.t.Helper()
	v, err := f.eng.Snapshot(phone(slot), team)
	if err != nil {
		f.t.Fatalf("Snapshot for %d: %v", slot, err)
	}
	return v
}

// A team splits as it arrives, each phone walks its group's path, and
// the last group to the join takes the team on with what each found.
func TestSplitLive(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(true), 1)
	team := f.teams[0]
	p := f.players(team, 4) // the scout is dealt to p[0]

	split, strands, current := f.strands(team)
	if split.String != "split" || len(strands) != 2 || current != "split" {
		t.Fatalf("after the start: split %v, strands %+v, node %q", split, strands, current)
	}
	// The organisers see the team at the split, and its groups.
	if v := f.view(team); v.Node != "split" || len(v.Groups) != 2 {
		t.Fatalf("the organisers' view: %+v", v)
	}
	v := f.phoneView(team, p[0])
	if v.Type != graph.Split || v.Group != "Scout" || !v.Groups[0].Mine {
		t.Fatalf("the scout's view: %+v", v)
	}
	if v := f.phoneView(team, p[1]); v.Group != "b" || !v.Groups[1].Mine {
		t.Fatalf("p1's view: %+v", v)
	}

	if _, err := f.eng.Continue(phone(p[1]), team, "split"); err != nil {
		t.Fatal(err)
	}
	_, strands, current = f.strands(team)
	// The scout's path takes the scout and a share of the crew: p0, p2;
	// the other p1, p3 — stored as the first action dealt them.
	if current != "split" || strands[1].Current != "pb" || strands[0].Current != "split" ||
		!slices.Equal(strands[0].Members, []int64{p[0], p[2]}) || !slices.Equal(strands[1].Members, []int64{p[1], p[3]}) {
		t.Fatalf("after b set off: node %q, strands %+v", current, strands)
	}
	if v := f.phoneView(team, p[3]); v.Node != "pb" {
		t.Fatalf("p3 goes with p1: %+v", v)
	}
	if v := f.phoneView(team, p[2]); v.Node != "split" {
		t.Fatalf("p2 is still at the split: %+v", v)
	}

	// Another group's step is not the phone's.
	if _, err := f.eng.Answer(phone(p[0]), team, "pb", "water"); !errors.Is(err, session.ErrOtherGroup) {
		t.Fatalf("the scout answering the other group's step: %v", err)
	}
	if _, err := f.eng.Answer(phone(p[3]), team, "pb", "water"); err != nil {
		t.Fatal(err)
	}
	if v := f.phoneView(team, p[1]); v.Type != graph.Join || !v.Groups[1].Arrived {
		t.Fatalf("p1 waits at the join: %+v", v)
	}
	if _, err := f.eng.Continue(phone(p[2]), team, "split"); err != nil {
		t.Fatal(err)
	}
	res, err := f.eng.Answer(phone(p[0]), team, "pa", "bell")
	if err != nil {
		t.Fatal(err)
	}
	if res.View.Node != "vault" || res.View.Groups != nil || res.View.Score != 20 {
		t.Fatalf("together at the vault: %+v", res.View)
	}
	split, strands, current = f.strands(team)
	if split.Valid || strands != nil || current != "vault" {
		t.Fatalf("after the join: split %v, strands %+v, node %q", split, strands, current)
	}
	for _, slot := range p {
		if v := f.phoneView(team, slot); v.Node != "vault" {
			t.Fatalf("player %d: %+v", slot, v)
		}
	}
	// The key the scout's group found opens the vault for anyone.
	if _, err := f.eng.Answer(phone(p[3]), team, "vault", "key"); err != nil {
		t.Fatal(err)
	}
	if v := f.view(team); !v.Finished || v.Score != 70 {
		t.Fatalf("finished: %+v", v)
	}
}

// A hundred players split in two even groups, and each phone acts for
// its own group.
func TestSplitAHundredPlayers(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 100)
	// Any action deals and stores the groups; a refused one too is fine.
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	_, strands, _ := f.strands(team)
	if len(strands[0].Members) != 50 || len(strands[1].Members) != 50 {
		t.Fatalf("groups of %d and %d", len(strands[0].Members), len(strands[1].Members))
	}
	for i, slot := range p {
		want := "pa"
		if i%2 == 1 {
			want = "split"
		}
		if v := f.phoneView(team, slot); v.Node != want || v.Groups[i%2].Players != 50 {
			t.Fatalf("player %d: at %q, groups %+v", i, v.Node, v.Groups)
		}
	}
}

// Many phones of one group answering at once move it once; the other
// group, acting at the same moment, is not held up or refused.
func TestSplitConcurrentGroups(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 10) // a: even, b: odd
	for _, slot := range p[:2] {
		if _, err := f.eng.Continue(phone(slot), team, "split"); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	var okA, okB, stale atomic.Int32
	start := make(chan struct{})
	for i, slot := range p {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			node, guess, ok := "pa", "bell", &okA
			if i%2 == 1 {
				node, guess, ok = "pb", "water", &okB
			}
			_, err := f.eng.Answer(phone(slot), team, node, guess)
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrStale):
				stale.Add(1)
			default:
				t.Errorf("player %d: %v", i, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if okA.Load() != 1 || okB.Load() != 1 || stale.Load() != 8 {
		t.Fatalf("a %d, b %d, stale %d; want one move for each group", okA.Load(), okB.Load(), stale.Load())
	}
	if _, strands, current := f.strands(team); current != "vault" || strands != nil {
		t.Fatalf("both groups arrived: node %q, strands %+v", current, strands)
	}
}

// A player who opens their link mid-split is dealt to the smallest
// group, and stored, without moving anyone else.
func TestSplitLateJoiner(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 3) // a: 0, 2; b: 1
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	var late int64
	mustScan(t, f.db.QueryRow(`INSERT INTO player_slots(team_id, name, token, last_login_at) VALUES ($1, 'late', 'late-tok', now_text()) RETURNING id`, team), &late)
	if v := f.phoneView(team, late); v.Node != "split" || !v.Groups[1].Mine {
		t.Fatalf("the newcomer's view: %+v", v)
	}
	if _, err := f.eng.Continue(phone(late), team, "split"); err != nil {
		t.Fatal(err)
	}
	_, strands, _ := f.strands(team)
	if !slices.Equal(strands[0].Members, []int64{p[0], p[2]}) || !slices.Equal(strands[1].Members, []int64{p[1], late}) {
		t.Fatalf("strands %+v", strands)
	}
}

// The adventure's time limit sends the whole team on, and its groups
// are forgotten.
func TestSplitTimeLimitLive(t *testing.T) {
	t.Parallel()
	g := heist(false)
	g.TimeLimit = &graph.TimeLimit{Seconds: 600, To: "vault"}
	f := setup(t, g, 1)
	team := f.teams[0]
	p := f.players(team, 2)
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(11 * time.Minute)
	res, err := f.eng.Answer(phone(p[0]), team, "pa", "bell")
	if err != nil || !res.TimeUp {
		t.Fatalf("res %+v, err %v", res, err)
	}
	if split, strands, current := f.strands(team); split.Valid || strands != nil || current != "vault" {
		t.Fatalf("split %v, strands %+v, node %q", split, strands, current)
	}
}

// The time limit ending a split leaves no group's presses behind: the
// other group's step is behind the team too. Regression: they were
// dropped only in memory, and came back on a later visit.
func TestSplitTimeLimitClearsPresses(t *testing.T) {
	t.Parallel()
	g := heist(false)
	g.TimeLimit = &graph.TimeLimit{Seconds: 600, To: "vault"}
	f := setup(t, g, 1)
	team := f.teams[0]
	p := f.players(team, 2)
	for _, slot := range p {
		if _, err := f.eng.Continue(phone(slot), team, "split"); err != nil {
			t.Fatal(err)
		}
	}
	_, strands, _ := f.strands(team)
	for _, st := range strands {
		if _, err := f.db.Exec(`INSERT INTO team_presses(team_id, node, slot_id, value, pressed_at) VALUES ($1, $2, $3, 'x', $4)`,
			team, st.Current, p[0], f.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	f.clock.Advance(11 * time.Minute)
	if res, err := f.eng.TimeUp(phone(p[0]), team); err != nil || !res.TimeUp {
		t.Fatalf("res %+v, err %v", res, err)
	}
	var n int
	mustScan(t, f.db.QueryRow(`SELECT count(*) FROM team_presses WHERE team_id = $1`, team), &n)
	if n != 0 {
		t.Fatalf("%d presses left behind", n)
	}
}

// A unanimous step on a path waits for its group, whose answers are kept
// with the group's step while the other group's stand elsewhere.
func TestSplitUnanimousLive(t *testing.T) {
	t.Parallel()
	g := heist(false)
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pb" {
			g.Nodes[i].PuzzleCfg.Unanimous = true
		}
	}
	f := setup(t, g, 1)
	team := f.teams[0]
	p := f.players(team, 6) // b: 1, 3, 5
	if _, err := f.eng.Continue(phone(p[1]), team, "split"); err != nil {
		t.Fatal(err)
	}
	res, err := f.eng.Answer(phone(p[1]), team, "pb", "water")
	if err != nil || res.Waiting == nil || res.Waiting.Of != 3 {
		t.Fatalf("p1 agrees: %+v %v", res.Waiting, err)
	}
	if v := f.phoneView(team, p[3]); v.Together == nil || v.Together.Done != 1 || v.Together.Of != 3 {
		t.Fatalf("p3 sees one of three: %+v", v.Together)
	}
	f.eng.Answer(phone(p[3]), team, "pb", "water")
	if res, err := f.eng.Answer(phone(p[5]), team, "pb", "water"); err != nil || !res.Accepted {
		t.Fatalf("the third: %+v %v", res, err)
	}
	var left int
	mustScan(t, f.db.QueryRow(`SELECT COUNT(*) FROM team_presses WHERE team_id = $1`, team), &left)
	if left != 0 {
		t.Errorf("%d presses left behind on the step the group left", left)
	}
}

// The organisers can move a stuck group on.
func TestSplitRescue(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 2)
	if _, err := f.eng.Continue(phone(p[1]), team, "split"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.RescueGroup(context.Background(), team, "pb", ""); err != nil {
		t.Fatal(err)
	}
	// Named by its key alone, a group's rescue is recorded on the step it
	// moved the group from.
	if _, err := f.eng.RescueGroup(context.Background(), team, "", "a"); err != nil {
		t.Fatal(err)
	}
	var node string
	mustScan(t, f.db.QueryRow(`SELECT COALESCE(payload::jsonb->>'node', '') FROM event_result_log WHERE team_id = $1 AND payload::jsonb->>'action' = 'rescue' ORDER BY seq DESC LIMIT 1`, team), &node)
	if node != "split" {
		t.Fatalf("the rescue by key recorded on %q, want the split", node)
	}
	// The organisers may move on the group out, not the one at the join.
	gs, err := f.eng.Groups(context.Background(), team)
	if err != nil || !slices.Equal(gs.Rescuable, []bool{true, false}) {
		t.Fatalf("groups %+v err %v", gs, err)
	}
}

// A time limit due and not run out — no phone has asked — is the
// organisers' to run out: the read says so, with no group to move on,
// and a group's rescue meeting it runs it out instead, recorded as the
// limit's doing — a time up, not that group's rescue.
func TestRescueMeetingTheTimeLimit(t *testing.T) {
	t.Parallel()
	g := heist(false)
	g.TimeLimit = &graph.TimeLimit{Seconds: 60, To: "vault"}
	f := setup(t, g, 1)
	team := f.teams[0]
	f.players(team, 2)
	ctx := context.Background()
	gs, err := f.eng.Groups(ctx, team)
	if err != nil || gs.TimeUp || !slices.Equal(gs.Rescuable, []bool{true, true}) {
		t.Fatalf("before the limit: %+v %v", gs, err)
	}
	f.clock.Advance(time.Hour)
	if gs, err = f.eng.Groups(ctx, team); err != nil || !gs.TimeUp || !slices.Equal(gs.Rescuable, []bool{false, false}) {
		t.Fatalf("the limit due: %+v %v", gs, err)
	}
	res, err := f.eng.RescueGroup(ctx, team, "", "b")
	if err != nil || !res.TimeUp {
		t.Fatalf("running out the time: %+v %v", res, err)
	}
	var action, input, to string
	mustScan(t, f.db.QueryRow(`SELECT payload::jsonb->>'action', COALESCE(payload::jsonb->>'input', ''), COALESCE(payload::jsonb->>'to', '')
		FROM event_result_log WHERE team_id = $1 AND kind LIKE 'play.%' ORDER BY seq DESC LIMIT 1`, team), &action, &input, &to)
	if action != "time_up" || input != "" || to != "vault" {
		t.Fatalf("recorded %q input %q, to %q", action, input, to)
	}
}

// The organisers see whether a group can be moved on as their rescue
// finds it, then: a hub's branch opening after a while on it opens the
// way on toward the join, with no one acting or looking.
func TestGroupsAsTheRescueFindsThem(t *testing.T) {
	t.Parallel()
	g := heist(false)
	g.Nodes = append(g.Nodes, graph.Node{Slug: "h", Type: graph.Hub, Title: "Hub", HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Required: 1, Branches: []graph.Branch{
		{Port: "b1", Condition: cond.Predicate{Op: cond.OpNodeElapsed, Cmp: cond.Gte, N: 600}},
		{Port: "b2"},
	}}})
	for i, e := range g.Edges {
		if e.From == "split" && e.FromPort == "b" {
			g.Edges[i].To = "h"
		}
	}
	g.Edges = append(g.Edges, edge("h", "b1", "pb"), edge("h", "b2", "end"), edge("h", graph.PortDone, "join"))
	f := setup(t, g, 1)
	team := f.teams[0]
	p := f.players(team, 2)
	if _, err := f.eng.Continue(phone(p[1]), team, "split"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	gs, err := f.eng.Groups(ctx, team)
	if err != nil || len(gs.Strands) != 2 || gs.Strands[1].Current != "h" || !slices.Equal(gs.Rescuable, []bool{true, false}) {
		t.Fatalf("groups %+v err %v", gs, err)
	}
	if _, err := f.eng.RescueGroup(ctx, team, "h", "b"); !errors.Is(err, session.ErrNotAllowed) {
		t.Fatalf("rescue with only an end open: %v", err)
	}
	f.clock.Advance(601 * time.Second)
	if gs, err = f.eng.Groups(ctx, team); err != nil || !slices.Equal(gs.Rescuable, []bool{true, true}) {
		t.Fatalf("groups once b1 opens %+v err %v", gs, err)
	}
	if _, err := f.eng.RescueGroup(ctx, team, "h", "b"); err != nil {
		t.Fatalf("rescue once b1 opens: %v", err)
	}
}

// phoneLocator places each phone where the test says.
type phoneLocator struct {
	mu sync.Mutex
	at map[int64]session.Position
}

func (l *phoneLocator) put(slot int64, p session.Position) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.at == nil {
		l.at = map[int64]session.Position{}
	}
	l.at[slot] = p
}

func (l *phoneLocator) Position(context.Context, Querier, int64) (float64, float64, bool, error) {
	return 0, 0, false, nil
}

func (l *phoneLocator) Phones(ctx context.Context, q Querier, team int64, _ time.Time) ([]Phone, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rows, err := q.QueryContext(ctx, `SELECT id FROM player_slots WHERE team_id = $1 ORDER BY id`, team)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Phone{}
	for rows.Next() {
		var slot int64
		if err := rows.Scan(&slot); err != nil {
			return nil, err
		}
		if p, ok := l.at[slot]; ok {
			out = append(out, Phone{Slot: slot, Position: p})
		}
	}
	return out, rows.Err()
}

// A place on a path is reached by a player of its group: the other
// group's player standing there does not open it.
func TestSplitZonesLive(t *testing.T) {
	t.Parallel()
	g := heist(false)
	g.Zones = []graph.Zone{{Slug: "well", Lat: 59.4, Lng: 18.1, RadiusM: 50}}
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pb" {
			g.Nodes[i].PuzzleCfg.Zone = "well"
		}
	}
	f := setup(t, g, 1)
	loc := &phoneLocator{}
	f.eng.Locator = loc
	team := f.teams[0]
	p := f.players(team, 2) // a: p0, b: p1
	well, away := session.Position{Lat: 59.4, Lng: 18.1}, session.Position{Lat: 0, Lng: 0}
	loc.put(p[0], well)
	loc.put(p[1], away)
	if _, err := f.eng.Continue(phone(p[1]), team, "split"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.Answer(phone(p[1]), team, "pb", "water"); !errors.Is(err, ErrNotHere) {
		t.Fatalf("p1 away from the well, p0 at it: %v, want ErrNotHere", err)
	}
	loc.put(p[1], well)
	if _, err := f.eng.Answer(phone(p[1]), team, "pb", "water"); err != nil {
		t.Fatal(err)
	}
}

// A dev team put on a split splits there afresh; put anywhere else, it
// is one team.
func TestDevMoveToASplit(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	if _, err := f.db.Exec(`UPDATE events SET dev = TRUE WHERE id = $1`, f.eventID); err != nil {
		t.Skipf("no dev events here: %v", err)
	}
	p := f.players(team, 2)
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	// A step on a path is one group's: the team is put on its split, that
	// group on the step and the other on the split.
	if err := f.eng.DevMoveTo(context.Background(), team, "pa"); err != nil {
		t.Fatal(err)
	}
	if split, strands, current := f.strands(team); split.String != "split" || current != "split" || len(strands) != 2 || strands[0].Current != "pa" || strands[1].Current != "split" {
		t.Fatalf("moved to a path's step: split %v at %q, strands %+v", split, current, strands)
	}
	var visited int
	mustScan(t, f.db.QueryRow(`SELECT count(*) FROM team_node_state WHERE team_id = $1 AND node = 'split'`, team), &visited)
	if visited != 1 {
		t.Fatalf("the split, its group come by it, visited %d times on record", visited)
	}
	// Each phone shows the step asked for: moved from the console, every
	// player is with its group; moved from a phone, that phone.
	for _, slot := range p {
		if v := f.phoneView(team, slot); v.Node != "pa" || !v.MovedOn {
			t.Fatalf("phone %d after the console's move shows %q, the organisers' %v: want pa, theirs", slot, v.Node, v.MovedOn)
		}
	}
	if err := f.eng.DevMoveTo(phone(p[1]), team, "pb"); err != nil {
		t.Fatal(err)
	}
	if v := f.phoneView(team, p[1]); v.Node != "pb" || v.MovedByOther {
		t.Fatalf("the phone that moved to pb shows %q, moved by another %v", v.Node, v.MovedByOther)
	}
	if v := f.phoneView(team, p[0]); !v.MovedByOther {
		t.Fatalf("another phone not told one moved the team: %+v", v)
	}
	if err := f.eng.DevMoveTo(context.Background(), team, "vault"); err != nil {
		t.Fatal(err)
	}
	if split, strands, current := f.strands(team); split.Valid || strands != nil || current != "vault" {
		t.Fatalf("moved to the vault: split %v, strands %+v, node %q", split, strands, current)
	}
	// The event paused ten minutes in all so far: the groups' clocks
	// start from that, as the team's does.
	if _, err := f.db.Exec(`UPDATE events SET paused_seconds = 600 WHERE id = $1`, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.eng.DevMoveTo(context.Background(), team, "split"); err != nil {
		t.Fatal(err)
	}
	if split, strands, _ := f.strands(team); split.String != "split" || len(strands) != 2 || strands[0].Current != "split" || strands[0].NodePaused != 600 {
		t.Fatalf("moved to the split: split %v, strands %+v", split, strands)
	}
	var nodePaused int
	mustScan(t, f.db.QueryRow(`SELECT node_paused FROM team_graph_state WHERE team_id = $1`, team), &nodePaused)
	if nodePaused != 600 {
		t.Fatalf("the team's node clock paused %d, want 600", nodePaused)
	}
	if v := f.phoneView(team, p[1]); v.Type != graph.Split {
		t.Fatalf("view %+v", v)
	}
}

// A team whose stored groups no longer fit its graph — a dev team's,
// edited under it — splits afresh on the split it stands on, and plays
// on. Regression: it stood there for good, every press the wrong step.
func TestSplitThatNoLongerFitsSplitsAfresh(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 2)
	if _, err := f.db.Exec(`UPDATE team_graph_state SET strands = '[{"port":"gone","current":"split"}]', moved_by = 999 WHERE team_id = $1`, team); err != nil {
		t.Fatal(err)
	}
	if v := f.phoneView(team, p[0]); v.Type != graph.Split || len(v.Groups) != 2 {
		t.Fatalf("view %+v", v)
	}
	// Split afresh by no one: stored so, for the next phone to read.
	var by int64
	mustScan(t, f.db.QueryRow(`SELECT moved_by FROM team_graph_state WHERE team_id = $1`, team), &by)
	if by != 0 {
		t.Fatalf("stored moved by %d after a refit", by)
	}
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	if split, strands, _ := f.strands(team); split.String != "split" || len(strands) != 2 || strands[0].Current != "pa" {
		t.Fatalf("split %v strands %+v", split, strands)
	}
}

// Groups that no longer fit are split afresh for the organisers, as the
// team's next move will split them, each to be set off — and the rescue
// that names one finds it so. The read stores nothing.
func TestGroupsSplitAfresh(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	f.players(team, 2)
	if _, err := f.db.Exec(`UPDATE team_graph_state SET strands = '[{"port":"gone","current":"split"}]' WHERE team_id = $1`, team); err != nil {
		t.Fatal(err)
	}
	gs, err := f.eng.Groups(context.Background(), team)
	if err != nil || gs.Split != "split" || len(gs.Strands) != 2 || !slices.Equal(gs.Rescuable, []bool{true, true}) {
		t.Fatalf("groups %+v err %v", gs, err)
	}
	groups := gs.Strands
	if _, strands, _ := f.strands(team); len(strands) != 1 {
		t.Fatalf("the read stored %+v", strands)
	}
	if _, err := f.eng.RescueGroup(context.Background(), team, "split", groups[1].Port); err != nil {
		t.Fatal(err)
	}
	if _, strands, _ := f.strands(team); strands[1].Current == "split" {
		t.Fatalf("group %s not set off: %+v", groups[1].Port, strands)
	}
}

// A dev team whose split was taken out of its graph is whole for the
// organisers, as its next move will have it: no groups to show.
func TestGroupsMakeWhole(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	f.players(team, 2)
	if split, _, _ := f.strands(team); split.String != "split" {
		t.Fatalf("not split: %v", split)
	}
	g := heist(false)
	g.Nodes = slices.DeleteFunc(g.Nodes, func(n graph.Node) bool { return n.Type == graph.Split })
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "split" || e.To == "split" })
	// The team plays its adventure's dev row, edited under it.
	var adv int64
	mustScan(t, f.db.QueryRow(`SELECT g.adventure_id FROM team_graph_state gs JOIN adventure_graphs g ON g.id = gs.graph_id WHERE gs.team_id = $1`, team), &adv)
	dev, err := graphstore.EnsureDev(context.Background(), f.db, adv, heist(false))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE team_graph_state SET graph_id = $1 WHERE team_id = $2`, dev, team); err != nil {
		t.Fatal(err)
	}
	if _, err := graphstore.EnsureDev(context.Background(), f.db, adv, g); err != nil {
		t.Fatal(err)
	}
	gs, err := f.eng.Groups(context.Background(), team)
	if err != nil || gs.Split != "" || gs.Strands != nil || gs.Rescuable != nil {
		t.Fatalf("groups %+v err %v", gs, err)
	}
}

// A dev team put on a path's step plays it in the look the group reaches
// it with: a theme set on the path, past the split, is the step's.
func TestDevMoveToAPathStepTakesItsTheme(t *testing.T) {
	t.Parallel()
	g := heist(false)
	g.Nodes = append(g.Nodes, graph.Node{Slug: "dusk", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.SetTheme, Theme: "night"}})
	for i, e := range g.Edges {
		if e.From == "split" && e.FromPort == "a" {
			g.Edges[i].To = "dusk"
		}
	}
	g.Edges = append(g.Edges, edge("dusk", graph.PortOut, "pa"))
	f := setup(t, g, 1)
	team := f.teams[0]
	if _, err := f.db.Exec(`UPDATE events SET dev = TRUE WHERE id = $1`, f.eventID); err != nil {
		t.Skipf("no dev events here: %v", err)
	}
	f.players(team, 2)
	if err := f.eng.DevMoveTo(context.Background(), team, "pa"); err != nil {
		t.Fatal(err)
	}
	var theme string
	mustScan(t, f.db.QueryRow(`SELECT theme FROM team_graph_state WHERE team_id = $1`, team), &theme)
	if theme != "night" {
		t.Fatalf("theme %q, want the path's night", theme)
	}
}

// The organisers' move is kept with the team for its phones to say so:
// the whole team moved into its split, each group shown so, until it
// sets off itself.
func TestOrganisersMoveIsKept(t *testing.T) {
	t.Parallel()
	g := heist(false)
	g.Nodes = append(g.Nodes, puzzle("p0", "go"))
	for i, e := range g.Edges {
		if e.From == "start" {
			g.Edges[i].To = "p0"
		}
	}
	g.Edges = append(g.Edges, edge("p0", "solved", "split"))
	f := setup(t, g, 1)
	team := f.teams[0]
	p := f.players(team, 2)
	if _, err := f.eng.RescueGroup(context.Background(), team, "p0", ""); err != nil {
		t.Fatal(err)
	}
	v := f.phoneView(team, p[0])
	if v.Type != graph.Split || !v.MovedOn || !v.Groups[0].Moved || !v.Groups[1].Moved {
		t.Fatalf("moved into the split: %+v", v)
	}
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	v = f.phoneView(team, p[0])
	if v.MovedOn || v.MovedByOther {
		t.Fatalf("set off by its own phone: %+v", v)
	}
}

// Roles locked on a step of one group's path are dealt over the whole
// team: the other group's role stays its holder's. Regression: the
// group leaving took every role among its own.
func TestLockRolesOnAPath(t *testing.T) {
	t.Parallel()
	g := heist(true)
	g.Roles = append(g.Roles, graph.Role{Slug: "medic", Name: "Medic"})
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pa" {
			g.Nodes[i].PuzzleCfg.Blocks = []graph.Block{{ID: "parts", Kind: graph.BlockRoles, Text: "Your parts.", Lock: true}}
		}
	}
	f := setup(t, g, 1)
	team := f.teams[0]
	p := f.players(team, 4) // scout p0, medic p1; a: p0, p2; b: p1, p3
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.Answer(phone(p[0]), team, "pa", "bell"); err != nil {
		t.Fatal(err)
	}
	var medic int64
	mustScan(t, f.db.QueryRow(`SELECT slot_id FROM team_roles WHERE team_id = $1 AND role = 'medic'`, team), &medic)
	if medic != p[1] {
		t.Fatalf("the medic locked to %d, want %d", medic, p[1])
	}
}

// A group whose players have gone is walked by whoever takes it on, and
// its walk is theirs. Regression: it was its gone players', none, and a
// walk on its path never opened.
func TestWalkOfAGroupWithNoOneLeft(t *testing.T) {
	t.Parallel()
	g := heist(false)
	// How far a group walked is asked for on its path.
	g.Nodes = append(g.Nodes, graph.Node{Slug: "far", Type: graph.Gate, GateCfg: &graph.GateConfig{Condition: cond.Predicate{Op: cond.OpWalked, N: 1000}}})
	for i, e := range g.Edges {
		if e.From == "pb" {
			g.Edges[i].To = "far"
		}
	}
	g.Edges = append(g.Edges, edge("far", graph.PortPass, "join"), edge("far", graph.PortFail, "join"))
	f := setup(t, g, 1)
	team := f.teams[0]
	p := f.players(team, 4) // a: p0, p2; b: p1, p3
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	// b's players gone: neither counts as playing.
	if _, err := f.db.Exec(`UPDATE player_slots SET last_login_at = NULL WHERE id IN ($1, $2)`, p[1], p[3]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.Answer(phone(p[0]), team, "pa", "bell"); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(time.Minute)
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	// p0 walks b for its gone players: its pings are tagged b's, as the
	// ping handler tags them (WalkingGroup).
	if split, port, err := f.eng.WalkingGroup(context.Background(), team, p[0]); err != nil || port != "b" {
		t.Fatalf("p0 walks %q of %q (%v), want b", port, split, err)
	}
	for i, lat := range []float64{59.0, 59.01} {
		if _, err := f.db.Exec(`
			INSERT INTO location_pings(slot_id, team_id, event_id, lat, lng, server_ts, groups)
			VALUES ($1, $2, $3, $4, 18.0, $5, $6)
		`, p[0], team, f.eventID, lat, f.clock.Now().Add(time.Duration(i+1)*time.Minute).Format("2006-01-02 15:04:05"), []string{GroupTag("split", "b")}); err != nil {
			t.Fatal(err)
		}
	}
	s, err := f.eng.loadCore(context.Background(), f.db, team, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.divide(context.Background(), f.db, s); err != nil {
		t.Fatal(err)
	}
	if w := s.Walks["b"]; w < 1000 || s.Walks["a"] != 0 {
		t.Fatalf("walks %v: b's walkers walked 1.1 km", s.Walks)
	}
}

// A phone's choice of group is kept: its next view, and its actions, are
// that group's.
func TestWalkLive(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 1)
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.Walk(phone(p[0]), team, "b"); err != nil {
		t.Fatal(err)
	}
	if v := f.phoneView(team, p[0]); v.Node != "split" || !v.Groups[1].Current {
		t.Fatalf("walking b: %+v", v)
	}
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	if _, strands, _ := f.strands(team); strands[0].Current != "pa" || strands[1].Current != "pb" || !slices.Equal(strands[1].Chosen, []int64{p[0]}) {
		t.Fatalf("strands %+v", strands)
	}
	if _, err := f.eng.Walk(phone(p[0]), team, "nowhere"); !errors.Is(err, session.ErrStale) {
		t.Fatalf("walking no group: %v", err)
	}
	// A group chosen to walk is no play: the result chain is not told.
	var walks int
	mustScan(t, f.db.QueryRow(`SELECT count(*) FROM event_result_log WHERE team_id = $1 AND kind = 'play.walk'`, team), &walks)
	if walks != 0 {
		t.Fatalf("%d walks in the result chain", walks)
	}
}

// A group whose players' phones are all away is anyone's until one is
// back: the other group, done, walks it.
func TestAwayGroupIsAnyonesLive(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 2) // a: p0; b: p1
	// p1 opened their link long ago and has not been heard from since.
	if _, err := f.db.Exec(`UPDATE player_slots SET last_login_at = '2026-09-22 09:00:00' WHERE id = $1`, p[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	if v := f.phoneView(team, p[0]); !v.Groups[1].Open || v.Groups[1].Mine {
		t.Fatalf("b should be p0's to walk too: %+v", v.Groups)
	}
	if _, err := f.eng.Walk(phone(p[0]), team, "b"); err != nil {
		t.Fatal(err)
	}
}

// A phone heard from lately — the app open, sending no location — is not
// away, however long ago its player opened their link; silent for
// minutes, it is, and a ping counts as hearing from it too. Regression:
// only pings and the link's opening counted, so in an adventure without
// places every other phone was away three minutes in.
func TestSeenPhoneIsNotAway(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 2) // a: p0; b: p1
	seen := func(at string) {
		t.Helper()
		if _, err := f.db.Exec(`UPDATE player_slots SET last_login_at = '2026-09-22 09:00:00', last_seen_at = $2 WHERE id = $1`, p[1], at); err != nil {
			t.Fatal(err)
		}
	}
	seen("2026-09-22 09:59:30")
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	if v := f.phoneView(team, p[0]); v.Groups[1].Open {
		t.Fatalf("p1 was seen half a minute ago: %+v", v.Groups)
	}
	seen("2026-09-22 09:50:00")
	if v := f.phoneView(team, p[0]); !v.Groups[1].Open {
		t.Fatalf("p1 silent for ten minutes: %+v", v.Groups)
	}
	if _, err := f.db.Exec(`
		INSERT INTO location_pings(slot_id, team_id, event_id, lat, lng, server_ts)
		VALUES ($1, $2, $3, 59.0, 18.0, '2026-09-22 09:59:50')
	`, p[1], team, f.eventID); err != nil {
		t.Fatal(err)
	}
	if v := f.phoneView(team, p[0]); v.Groups[1].Open {
		t.Fatalf("p1 pinged ten seconds ago: %+v", v.Groups)
	}
}

// A team split afresh off a split — its split edited into another kind
// of step — is stored as one team by its next action, though it moves
// nowhere. Regression: only a deal or a choice stored the groups, and the
// stale ones stayed.
func TestSplitAfreshOffASplitIsStored(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 1)
	if _, err := f.db.Exec(`UPDATE team_graph_state SET split = 'pa', current_node = 'pa' WHERE team_id = $1`, team); err != nil {
		t.Fatal(err)
	}
	if res, err := f.eng.Answer(phone(p[0]), team, "pa", "nope"); err != nil || res.Accepted {
		t.Fatalf("a wrong answer: %+v %v", res, err)
	}
	if split, strands, current := f.strands(team); split.Valid || strands != nil || current != "pa" {
		t.Fatalf("split %v strands %+v at %q", split, strands, current)
	}
}

// An organiser moving a team onto a split, knowing none of its players,
// still deals them there and then: the groups are stored as first shown.
func TestSplitDealtOnOrganiserMove(t *testing.T) {
	t.Parallel()
	g := heist(false)
	g.Nodes = append(g.Nodes, puzzle("intro", "go"))
	for i, e := range g.Edges {
		if e.From == "start" {
			g.Edges[i].To = "intro"
		}
	}
	g.Edges = append(g.Edges, edge("intro", "solved", "split"))
	f := setup(t, g, 1)
	team := f.teams[0]
	p := f.players(team, 2)
	if _, err := f.eng.RescueGroup(context.Background(), team, "intro", ""); err != nil {
		t.Fatal(err)
	}
	_, strands, _ := f.strands(team)
	if len(strands) != 2 || len(strands[0].Members)+len(strands[1].Members) != len(p) {
		t.Fatalf("the organisers' move should deal the players: %+v", strands)
	}
}

// Stored groups fit only while there is one to each of the split's
// paths, each on its own path, the split or the join, since a time: a
// path added or taken out, a group on a step wired elsewhere, or one with
// no time, splits the team afresh.
func TestSplitFitsOneGroupPerPath(t *testing.T) {
	t.Parallel()
	g := heist(false)
	at := func(ports ...string) []session.Strand {
		var out []session.Strand
		for _, p := range ports {
			out = append(out, session.Strand{Port: p, Cursor: session.Cursor{Current: "split", NodeEnteredAt: time.Now()}})
		}
		return out
	}
	// on is groups at steps: port, step, port, step.
	on := func(ps ...string) []session.Strand {
		var out []session.Strand
		for i := 0; i < len(ps); i += 2 {
			out = append(out, session.Strand{Port: ps[i], Cursor: session.Cursor{Current: ps[i+1], NodeEnteredAt: time.Now()}})
		}
		return out
	}
	for _, c := range []struct {
		strands []session.Strand
		want    bool
	}{
		{at("a", "b"), true},
		{at("b", "a"), true},
		{at("a"), false},
		{at("a", "a"), false},
		{at("a", "b", "c"), false},
		{at("a", "gone"), false},
		{[]session.Strand{{Port: "a", Cursor: session.Cursor{Current: "split"}}, at("b")[0]}, false},
		{on("a", "pa", "b", "join"), true},
		{on("a", "pb", "b", "split"), false},
		{on("a", "vault", "b", "split"), false},
		{nil, false},
	} {
		if got := session.SplitFits(g.Splits(), "split", c.strands); got != c.want {
			t.Errorf("%+v: %v, want %v", c.strands, got, c.want)
		}
	}
}

// A group moving on leaves the team's own clock where it was: at the
// split since the team came there. Regression: each group's move stored
// its own time as the team's.
func TestGroupMoveKeepsTheTeamsClock(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 2)
	var before time.Time
	mustScan(t, f.db.QueryRow(`SELECT node_entered_at FROM team_graph_state WHERE team_id = $1`, team), &before)
	f.clock.Advance(20 * time.Minute)
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	var after time.Time
	mustScan(t, f.db.QueryRow(`SELECT node_entered_at FROM team_graph_state WHERE team_id = $1`, team), &after)
	if !after.Equal(before) {
		t.Fatalf("the team at the split since %v, now %v", before, after)
	}
	if _, strands, _ := f.strands(team); !strands[0].NodeEnteredAt.Equal(f.clock.Now()) && !strands[1].NodeEnteredAt.Equal(f.clock.Now()) {
		t.Fatalf("the group that moved keeps its own time: %+v", strands)
	}
}

// Groups stored without the time they came to their step — in another
// shape — split afresh, rather than have their clocks run out at once.
func TestSplitWithNoTimeSplitsAfresh(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 2)
	if _, err := f.db.Exec(`UPDATE team_graph_state SET strands = '[{"port":"a","current":"pa","entered_at":"2026-01-01T00:00:00Z"},{"port":"b","current":"split"}]' WHERE team_id = $1`, team); err != nil {
		t.Fatal(err)
	}
	if v := f.phoneView(team, p[0]); v.Type != graph.Split || len(v.Groups) != 2 {
		t.Fatalf("view %+v", v)
	}
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	_, strands, _ := f.strands(team)
	for _, st := range strands {
		if st.NodeEnteredAt.IsZero() {
			t.Fatalf("a group with no time: %+v", strands)
		}
	}
}

// A team standing on a step its draft has since made a split splits
// there, rather than being stuck on it.
func TestStandingOnANewSplitSplits(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 2)
	if _, err := f.db.Exec(`UPDATE team_graph_state SET split = NULL, strands = NULL, current_node = 'split' WHERE team_id = $1`, team); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	if split, strands, _ := f.strands(team); split.String != "split" || len(strands) != 2 {
		t.Fatalf("split %v strands %+v", split, strands)
	}
}

// A wrong answer on a group's step is kept with that step: its attempts
// count there, not on the split the team as a whole stands on.
func TestGroupWrongAnswerKeptOnItsStep(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 2)
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	_, strands, _ := f.strands(team)
	at, guess := "pa", "nope"
	if strands[0].Current != "pa" {
		at = "pb"
	}
	if res, err := f.eng.Answer(phone(p[0]), team, at, guess); err != nil || res.Accepted {
		t.Fatalf("res %+v, err %v", res, err)
	}
	var attempts int
	mustScan(t, f.db.QueryRow(`SELECT attempts FROM team_node_state WHERE team_id = $1 AND node = $2`, team, at), &attempts)
	if attempts != 1 {
		t.Fatalf("%d attempts on %s, want 1", attempts, at)
	}
}

// An organiser moving the last group through a join that leads straight
// back into the same split deals the players into its new groups there
// and then. Regression: only a different split was dealt, and the new
// groups were stored with no one in them.
func TestBackIntoTheSameSplitDealtOnOrganiserMove(t *testing.T) {
	t.Parallel()
	g := heist(false)
	g.Nodes = slices.DeleteFunc(g.Nodes, func(n graph.Node) bool { return n.Slug == "vault" })
	g.Edges = slices.DeleteFunc(g.Edges, func(e graph.Edge) bool { return e.From == "join" || e.From == "vault" })
	g.Edges = append(g.Edges, edge("join", graph.PortOut, "split"))
	// A way out of the loop, for the adventure to have an end.
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "pb" {
			g.Nodes[i].PuzzleCfg.Outputs = append(g.Nodes[i].PuzzleCfg.Outputs, graph.Output{Port: "quit", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "quit"}}})
		}
	}
	g.Edges = append(g.Edges, edge("pb", "quit", "end"))
	f := setup(t, g, 1)
	team := f.teams[0]
	p := f.players(team, 2)
	for _, slot := range p {
		if _, err := f.eng.Continue(phone(slot), team, "split"); err != nil {
			t.Fatal(err)
		}
	}
	for _, grp := range []struct{ port, node string }{{"a", "pa"}, {"b", "pb"}} {
		if _, err := f.eng.RescueGroup(context.Background(), team, grp.node, grp.port); err != nil {
			t.Fatalf("rescue %s: %v", grp.port, err)
		}
	}
	split, strands, _ := f.strands(team)
	if split.String != "split" || len(strands) != 2 || len(strands[0].Members)+len(strands[1].Members) != len(p) {
		t.Fatalf("back at the split, dealt: split %v strands %+v", split, strands)
	}
	for _, st := range strands {
		if st.Current != "split" {
			t.Fatalf("a group not back at the split: %+v", strands)
		}
	}
}

// A group's name in a ping reads back whatever its slug and port hold.
func TestGroupTag(t *testing.T) {
	if split, port, ok := ParseGroupTag(GroupTag("s.1", "a.b>")); !ok || split != "s.1" || port != "a.b>" {
		t.Errorf("ParseGroupTag(GroupTag) = %q %q %v", split, port, ok)
	}
	if _, _, ok := ParseGroupTag("s.a"); ok {
		t.Error("a tag not JSON read as a group")
	}
}

// A group's walk is what was walked in it: a phone that walked a's step
// and then went to walk b leaves a's walk with a, and takes none of it
// to b. Regression: the walk went with the phone.
func TestWalkStaysWithTheGroupWalked(t *testing.T) {
	t.Parallel()
	g := heist(false)
	g.Nodes = append(g.Nodes, graph.Node{Slug: "far", Type: graph.Gate, GateCfg: &graph.GateConfig{Condition: cond.Predicate{Op: cond.OpWalked, N: 1000}}})
	for i, e := range g.Edges {
		if e.From == "pb" {
			g.Edges[i].To = "far"
		}
	}
	g.Edges = append(g.Edges, edge("far", graph.PortPass, "join"), edge("far", graph.PortFail, "join"))
	f := setup(t, g, 1)
	team := f.teams[0]
	p := f.players(team, 2) // a: p0, b: p1
	for _, slot := range p {
		if _, err := f.eng.Continue(phone(slot), team, "split"); err != nil {
			t.Fatal(err)
		}
	}
	for i, lat := range []float64{59.0, 59.01} {
		if _, err := f.db.Exec(`
			INSERT INTO location_pings(slot_id, team_id, event_id, lat, lng, server_ts, groups)
			VALUES ($1, $2, $3, $4, 18.0, $5, $6)
		`, p[0], team, f.eventID, lat, f.clock.Now().Add(time.Duration(i+1)*time.Minute).Format("2006-01-02 15:04:05"), []string{GroupTag("split", "a")}); err != nil {
			t.Fatal(err)
		}
	}
	// b's player gone: p0 may walk b, and goes to.
	if _, err := f.db.Exec(`UPDATE player_slots SET last_login_at = NULL WHERE id = $1`, p[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.Walk(phone(p[0]), team, "b"); err != nil {
		t.Fatal(err)
	}
	s, err := f.eng.loadCore(context.Background(), f.db, team, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.divide(phone(p[0]), f.db, s); err != nil {
		t.Fatal(err)
	}
	if s.Walks["a"] < 1000 || s.Walks["b"] != 0 {
		t.Fatalf("walks %v: a's 1.1 km should stay with a", s.Walks)
	}
}

// A phone that left a group to walk another, and came back, walked the
// first nothing in between: where it comes back to starts a new leg.
func TestWalkLeftAndCameBackTo(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 1)
	a, b := GroupTag("split", "a"), GroupTag("split", "b")
	since := f.clock.Now()
	for i, ping := range []struct {
		lat float64
		tag string
	}{{59.0, a}, {59.002, b}, {59.004, b}, {59.004, a}} {
		if _, err := f.db.Exec(`
			INSERT INTO location_pings(slot_id, team_id, event_id, lat, lng, server_ts, groups)
			VALUES ($1, $2, $3, $4, 18.0, $5, $6)
		`, p[0], team, f.eventID, ping.lat, since.Add(time.Duration(i+1)*time.Minute).Format("2006-01-02 15:04:05"), []string{ping.tag}); err != nil {
			t.Fatal(err)
		}
	}
	walked, err := walkedEach(context.Background(), f.db, team, []walkers{{since: since, tag: a}, {since: since, tag: b}})
	if err != nil {
		t.Fatal(err)
	}
	if walked[0] != 0 || walked[1] < 200 {
		t.Fatalf("walked %v: a nothing — the phone was walking b — and b its 220 m", walked)
	}
}

// A ping in the second a group came to its step may be from the step it
// left: its walk there starts from the next whole second.
func TestWalkStartsAfterTheStepWasReached(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 1)
	a := GroupTag("split", "a")
	reached := time.Date(2026, 10, 22, 10, 0, 5, 800_000_000, time.UTC)
	for _, ping := range []struct {
		lat float64
		at  time.Time
	}{{59.0, reached.Truncate(time.Second)}, {59.002, reached.Add(time.Minute)}} {
		if _, err := f.db.Exec(`
			INSERT INTO location_pings(slot_id, team_id, event_id, lat, lng, server_ts, groups)
			VALUES ($1, $2, $3, $4, 18.0, $5, $6)
		`, p[0], team, f.eventID, ping.lat, ping.at.Format("2006-01-02 15:04:05"), []string{a}); err != nil {
			t.Fatal(err)
		}
	}
	walked, err := walkedEach(context.Background(), f.db, team, []walkers{{since: reached, tag: a}})
	if err != nil {
		t.Fatal(err)
	}
	if walked[0] != 0 {
		t.Fatalf("walked %v from a ping of the second before the step was reached", walked)
	}
}

// A phone with its app open but sending no location is heard from, so
// not away — its group is not anyone's — but has no fix, so a post of
// its role is anyone's.
func TestAwayAndNoFix(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 1)
	long := f.clock.Now().Add(-time.Hour).UTC().Format(time.DateTime)
	if _, err := f.db.Exec(`UPDATE player_slots SET last_login_at = $2, last_seen_at = now_text() WHERE id = $1`, p[0], long); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	gone, err := away(context.Background(), f.db, team, now)
	if err != nil {
		t.Fatal(err)
	}
	unplaced, err := noFix(context.Background(), f.db, team, now)
	if err != nil {
		t.Fatal(err)
	}
	if gone[p[0]] || !unplaced[p[0]] {
		t.Fatalf("away %v, no fix %v: want heard from, but unplaced", gone[p[0]], unplaced[p[0]])
	}
}

// The group a phone is first shown is its group: a teammate opening
// their link later is dealt beside it, not dealt first and taking it.
// Regression: until someone acted, every view dealt afresh in slot
// order, and the phone's group changed as others opened their links.
func TestFirstShownGroupStays(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 2)
	// The first slot has not opened its link yet.
	if _, err := f.db.Exec(`UPDATE player_slots SET last_login_at = NULL WHERE id = $1`, p[0]); err != nil {
		t.Fatal(err)
	}
	first := f.phoneView(team, p[1]).Group
	if first == "" {
		t.Fatal("the phone is shown no group")
	}
	if _, err := f.db.Exec(`UPDATE player_slots SET last_login_at = now_text() WHERE id = $1`, p[0]); err != nil {
		t.Fatal(err)
	}
	if late := f.phoneView(team, p[0]).Group; late == first {
		t.Fatalf("the latecomer joined the first phone's group %q, alone with it", late)
	}
	if again := f.phoneView(team, p[1]).Group; again != first {
		t.Fatalf("the first phone's group changed from %q to %q", first, again)
	}
}

// A view's deal is stored only over the team as the view read it: on
// the step it read. One the team has moved off since, by an action, is
// not written back over it.
func TestKeepDealOnlyOverWhatWasRead(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 2)
	ctx := phone(p[0])
	s, err := f.eng.loadCore(ctx, f.db, team, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.divide(ctx, f.db, s); err != nil {
		t.Fatal(err)
	}
	// Meanwhile an action moves the team on.
	if _, err := f.db.Exec(`UPDATE team_graph_state SET current_node = 'vault' WHERE team_id = $1`, team); err != nil {
		t.Fatal(err)
	}
	f.eng.keepDeal(ctx, s)
	_, strands, current := f.strands(team)
	if current != "vault" || slices.ContainsFunc(strands, func(st session.Strand) bool { return len(st.Members) > 0 }) {
		t.Fatalf("the view's deal written over a team moved on: at %q, groups %+v", current, strands)
	}
	// Unmoved, it is kept.
	if _, err := f.db.Exec(`UPDATE team_graph_state SET current_node = 'split' WHERE team_id = $1`, team); err != nil {
		t.Fatal(err)
	}
	f.eng.keepDeal(ctx, s)
	if _, strands, _ := f.strands(team); !slices.ContainsFunc(strands, func(st session.Strand) bool { return len(st.Members) > 0 }) {
		t.Fatalf("the view's deal not kept: %+v", strands)
	}
}

// A view's deal is stored over the groups exactly as read, though they
// hold what this server does not know — another version's field.
// Regression: they were compared re-written, never matched, and the deal
// was never stored.
func TestKeepDealOverFieldsItDoesNotKnow(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 2)
	at := f.clock.Now().UTC().Format(time.RFC3339)
	doc := `[{"port":"a","current":"split","node_entered_at":"` + at + `","later":1},{"port":"b","current":"split","node_entered_at":"` + at + `"}]`
	if _, err := f.db.Exec(`UPDATE team_graph_state SET strands = $1 WHERE team_id = $2`, doc, team); err != nil {
		t.Fatal(err)
	}
	f.phoneView(team, p[0])
	if _, strands, _ := f.strands(team); !slices.ContainsFunc(strands, func(st session.Strand) bool { return len(st.Members) > 0 }) {
		t.Fatalf("the view's deal not stored: %+v", strands)
	}
}

// Groups a view makes whole — stored at a step no longer a split — are
// stored whole, not left for the map to draw as groups until an action.
func TestKeepDealStoresWhole(t *testing.T) {
	t.Parallel()
	f := setup(t, heist(false), 1)
	team := f.teams[0]
	p := f.players(team, 2)
	if _, err := f.db.Exec(`UPDATE team_graph_state SET split = 'pa', current_node = 'pa' WHERE team_id = $1`, team); err != nil {
		t.Fatal(err)
	}
	if v := f.phoneView(team, p[0]); v.Node != "pa" || len(v.Groups) != 0 {
		t.Fatalf("view %+v", v)
	}
	if split, strands, _ := f.strands(team); split.Valid || strands != nil {
		t.Fatalf("still stored split at %v: %+v", split, strands)
	}
}
