package play

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graph/valid"
)

// citadelLive: split{north, south}; north splits again{tower, crypt},
// tower and crypt meet at the hall and go on to n2; both meet at the gate.
func citadelLive(t *testing.T) *graph.Graph {
	t.Helper()
	right := []graph.Output{{Port: "ok", Trigger: graph.OnAnswer, Points: 1, Matchers: []match.Matcher{{Kind: match.Exact, Value: "x"}}}}
	p := func(slug string) graph.Node {
		return graph.Node{Slug: slug, Type: graph.Puzzle, Title: slug, PuzzleCfg: &graph.PuzzleConfig{Outputs: right}}
	}
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "split", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "north", Label: "North"}, {Port: "south", Label: "South"}}}},
			{Slug: "inner", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "tower", Label: "Tower"}, {Port: "crypt", Label: "Crypt"}}}},
			p("t1"), p("c1"), {Slug: "hall", Type: graph.Join, Title: "Hall"}, p("n2"), p("s1"),
			{Slug: "gate", Type: graph.Join, Title: "Gate"},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
		},
	}
	for _, e := range [][3]string{
		{"start", "out", "split"}, {"split", "north", "inner"}, {"split", "south", "s1"},
		{"inner", "tower", "t1"}, {"inner", "crypt", "c1"}, {"t1", "ok", "hall"}, {"c1", "ok", "hall"},
		{"hall", "out", "n2"}, {"n2", "ok", "gate"}, {"s1", "ok", "gate"}, {"gate", "out", "end"},
	} {
		g.Edges = append(g.Edges, graph.Edge{From: e[0], FromPort: e[1], To: e[2]})
	}
	if r := valid.Check(g); r.HasErrors() {
		t.Fatalf("graph: %v", r.Errors())
	}
	return g
}

func keysOf(strands []session.Strand) []string {
	var out []string
	for _, st := range strands {
		out = append(out, st.Key()+"@"+st.Current)
	}
	return out
}

// A group of a split team splits again on its path, its players dealt
// among its own groups and stored so; the groups meet, then the team.
func TestNestedSplitLive(t *testing.T) {
	t.Parallel()
	f := setup(t, citadelLive(t), 1)
	team := f.teams[0]
	p := f.players(team, 8) // north 0, 2, 4, 6; south 1, 3, 5, 7
	if _, err := f.eng.Continue(phone(p[0]), team, "split"); err != nil {
		t.Fatal(err)
	}
	_, strands, current := f.strands(team)
	if got := keysOf(strands); current != "split" || !slices.Equal(got, []string{"north>tower@inner", "north>crypt@inner", "south@split"}) {
		t.Fatalf("after the north set off: node %q, %v", current, got)
	}
	north := []int64{p[0], p[2], p[4], p[6]}
	if tw, cr := strands[0].Members, strands[1].Members; len(tw) != 2 || len(cr) != 2 || !sameSlots(append(slices.Clone(tw), cr...), north) {
		t.Fatalf("tower %v crypt %v, want the north's %v shared", tw, cr, north)
	}
	tower, crypt := strands[0].Members, strands[1].Members
	if v := f.phoneView(team, crypt[0]); v.Group != "North › Crypt" || v.Node != "inner" {
		t.Fatalf("a crypt player's view: %+v", v)
	}
	mustAct := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	_, err := f.eng.Continue(phone(tower[0]), team, "inner")
	mustAct("tower sets off", err)
	if _, err := f.eng.Answer(phone(crypt[0]), team, "t1", "x"); !errors.Is(err, session.ErrNotYours) {
		t.Fatalf("a crypt player at the tower's step: %v", err)
	}
	_, err = f.eng.Answer(phone(tower[1]), team, "t1", "x")
	mustAct("tower answers", err)
	_, err = f.eng.Continue(phone(crypt[1]), team, "inner")
	mustAct("crypt sets off", err)
	_, err = f.eng.Answer(phone(crypt[0]), team, "c1", "x")
	mustAct("crypt answers", err)
	_, strands, _ = f.strands(team)
	if got := keysOf(strands); !slices.Equal(got, []string{"north@n2", "south@split"}) || !sameSlots(strands[0].Members, north) {
		t.Fatalf("met at the hall: %v, north members %v", got, strands[0].Members)
	}
	_, err = f.eng.Answer(phone(crypt[1]), team, "n2", "x")
	mustAct("north answers n2", err)
	_, err = f.eng.Continue(phone(p[1]), team, "split")
	mustAct("south sets off", err)
	res, err := f.eng.Answer(phone(p[3]), team, "s1", "x")
	mustAct("south answers", err)
	if res.View.Node != "end" && !res.View.Finished {
		t.Fatalf("after the gate: %+v", res.View)
	}
	if split, strands, _ := f.strands(team); split.Valid || strands != nil {
		t.Fatalf("split %v strands %v", split, strands)
	}
}

func sameSlots(a, b []int64) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

// Many phones of several teams play a nested split as fast as they can.
func TestNestedSplitStressLive(t *testing.T) {
	t.Parallel()
	const teams, phones = 3, 24
	f := setup(t, citadelLive(t), teams)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	var actions atomic.Int64
	play := func(team, slot int64) {
		defer wg.Done()
		c := ForPhone(ctx, slot)
		for ctx.Err() == nil {
			v, err := f.eng.Snapshot(c, team)
			if err != nil {
				t.Errorf("snapshot for %d: %v", slot, err)
				return
			}
			if v.Finished {
				return
			}
			switch v.Type {
			case graph.Split:
				_, err = f.eng.Continue(c, team, v.Node)
			case graph.Puzzle:
				_, err = f.eng.Answer(c, team, v.Node, "x")
			default:
				time.Sleep(5 * time.Millisecond)
				continue
			}
			actions.Add(1)
			if err != nil && !errors.Is(err, ErrStale) && !errors.Is(err, ErrFinished) {
				t.Errorf("player %d on %s: %v", slot, v.Node, err)
				return
			}
		}
	}
	for _, team := range f.teams {
		for _, slot := range f.players(team, phones) {
			wg.Add(1)
			go play(team, slot)
		}
	}
	wg.Wait()
	if ctx.Err() != nil {
		for _, team := range f.teams {
			_, strands, current := f.strands(team)
			t.Logf("team %d at %q: %v", team, current, keysOf(strands))
		}
		t.Fatal("stuck")
	}
	t.Logf("%d actions by %d phones", actions.Load(), teams*phones)
	for _, team := range f.teams {
		if v := f.view(team); !v.Finished || v.Score != 4 {
			t.Errorf("team %d: finished %v score %v, want 4", team, v.Finished, v.Score)
		}
	}
}
