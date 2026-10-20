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
	"github.com/klahr/zonventure/internal/graph/session"
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
	if _, err := f.eng.Answer(phone(p[0]), team, "pb", "water"); !errors.Is(err, session.ErrNotYours) {
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
	if _, err := f.eng.Rescue(context.Background(), team, "pb"); err != nil {
		t.Fatal(err)
	}
	if _, strands, _ := f.strands(team); strands[1].Current != "join" {
		t.Fatalf("strands %+v", strands)
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
	if err := f.eng.DevMoveTo(context.Background(), team, "vault"); err != nil {
		t.Fatal(err)
	}
	if split, strands, current := f.strands(team); split.Valid || strands != nil || current != "vault" {
		t.Fatalf("moved to the vault: split %v, strands %+v, node %q", split, strands, current)
	}
	if err := f.eng.DevMoveTo(context.Background(), team, "split"); err != nil {
		t.Fatal(err)
	}
	if split, strands, _ := f.strands(team); split.String != "split" || len(strands) != 2 || strands[0].Current != "split" {
		t.Fatalf("moved to the split: split %v, strands %+v", split, strands)
	}
	if v := f.phoneView(team, p[1]); v.Type != graph.Split {
		t.Fatalf("view %+v", v)
	}
}
