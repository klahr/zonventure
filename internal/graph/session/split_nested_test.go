package session

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/valid"
)

// Nested splits: a group that reaches a split on its path divides again,
// and its groups meet at their own join before going on as the group
// they were.

// citadel: start → split{a "North", b "South"}; on a: inner{x "Tower",
// y "Crypt"} → x1 / y1 → ij → a2 → join; b: b1 → join; join → end.
func citadel(t *testing.T) *graph.Graph {
	t.Helper()
	b := newGB().split("split", "a", "b").split("inner", "x", "y").
		puzzle("x1", "x").puzzle("y1", "y").join("ij").puzzle("a2", "a").puzzle("b1", "b").join("join").end("end").
		wire("start", "out", "split").wire("split", "a", "inner").wire("split", "b", "b1").
		wire("inner", "x", "x1").wire("inner", "y", "y1").wire("x1", "solved", "ij").wire("y1", "solved", "ij").
		wire("ij", "out", "a2").wire("a2", "solved", "join").wire("b1", "solved", "join").wire("join", "out", "end")
	label := func(slug string, labels ...string) {
		for i := range b.g.Nodes {
			if b.g.Nodes[i].Slug == slug {
				for k, l := range labels {
					b.g.Nodes[i].SplitCfg.Paths[k].Label = l
				}
			}
		}
	}
	label("split", "North", "South")
	label("inner", "Tower", "Crypt")
	return b.valid(t)
}

func keys(tm *Team) []string {
	var out []string
	for _, st := range tm.Strands {
		out = append(out, st.Key()+"@"+st.Current)
	}
	return out
}

// The simulator's team, all players at once, walks every group of every
// level in turn.
func TestNestedTeamAsWhole(t *testing.T) {
	tm := begin(t, citadel(t))
	moved(t, tm, "a")(tm.Continue("split"))
	if got := keys(tm); !slices.Equal(got, []string{"a>x@inner", "a>y@inner", "b@split"}) {
		t.Fatalf("after North reached its own split: %v", got)
	}
	v := tm.View()
	if v.Node != "inner" || v.Type != graph.Split || v.Group != "North › Tower" || len(v.Groups) != 3 {
		t.Fatalf("view %+v", v)
	}
	moved(t, tm, "x")(tm.Continue("inner"))
	moved(t, tm, "x1")(tm.Answer("x1", "x"))
	if got := keys(tm); !slices.Equal(got, []string{"a>x@ij", "a>y@inner", "b@split"}) {
		t.Fatalf("Tower waits for Crypt: %v", got)
	}
	if v := tm.View(); v.Node != "inner" || v.Group != "North › Crypt" {
		t.Fatalf("on to the Crypt: %+v", v)
	}
	moved(t, tm, "y")(tm.Continue("inner"))
	moved(t, tm, "y1")(tm.Answer("y1", "y"))
	if got := keys(tm); !slices.Equal(got, []string{"a@a2", "b@split"}) {
		t.Fatalf("Tower and Crypt are North again, at a2: %v", got)
	}
	moved(t, tm, "a2")(tm.Answer("a2", "a"))
	if got := keys(tm); !slices.Equal(got, []string{"a@join", "b@split"}) {
		t.Fatalf("North waits for South: %v", got)
	}
	moved(t, tm, "b")(tm.Continue("split"))
	moved(t, tm, "b1")(tm.Answer("b1", "b"))
	if !tm.Completed || tm.Split != "" || tm.Points != 4 {
		t.Fatalf("completed %v split %q points %v", tm.Completed, tm.Split, tm.Points)
	}
}

// The last inner group in may take its siblings and the outer groups on
// in one step, when the inner join leads straight to the outer one.
func TestNestedCascade(t *testing.T) {
	g := newGB().split("split", "a", "b").split("inner", "x", "y").
		puzzle("x1", "x").puzzle("y1", "y").join("ij").puzzle("b1", "b").join("join").puzzle("after", "z").end("end").
		wire("start", "out", "split").wire("split", "a", "inner").wire("split", "b", "b1").
		wire("inner", "x", "x1").wire("inner", "y", "y1").wire("x1", "solved", "ij").wire("y1", "solved", "ij").
		wire("ij", "out", "join").wire("b1", "solved", "join").wire("join", "out", "after").wire("after", "solved", "end").valid(t)
	tm := begin(t, g)
	moved(t, tm, "a")(tm.Continue("split"))
	moved(t, tm, "x")(tm.Continue("inner"))
	moved(t, tm, "x1")(tm.Answer("x1", "x"))
	// The South sets off and arrives before the Crypt.
	tm.Unfocus()
	tm.focusOn(2)
	moved(t, tm, "b")(tm.Continue("split"))
	moved(t, tm, "b1")(tm.Answer("b1", "b"))
	if got := keys(tm); !slices.Equal(got, []string{"a>x@ij", "a>y@inner", "b@join"}) {
		t.Fatalf("before the Crypt: %v", got)
	}
	moved(t, tm, "y")(tm.Continue("inner"))
	moved(t, tm, "y1")(tm.Answer("y1", "y"))
	if tm.Split != "" || tm.Current != "after" {
		t.Fatalf("Crypt, last in, should take everyone on: split %q current %q strands %v", tm.Split, tm.Current, keys(tm))
	}
}

// A group's players are dealt among its own groups: the North's ten
// split in two fives, the South's never among them.
func TestNestedDeal(t *testing.T) {
	tm := begin(t, citadel(t))
	tm.Unfocus()
	tm.Players, tm.Size = nil, 20
	for i := int64(1); i <= 20; i++ {
		tm.Players = append(tm.Players, i)
	}
	tm.Deal(tm.Players, nil)
	north := slices.Clone(tm.Strands[0].Members)
	moved(t, tm, "a")(asNobody(tm, 1).Continue("split"))
	tm.Unfocus()
	if len(tm.Strands) != 3 {
		t.Fatalf("strands %v", keys(tm))
	}
	x, y := tm.Strands[0].Members, tm.Strands[1].Members
	if len(x) != 5 || len(y) != 5 {
		t.Fatalf("Tower %v, Crypt %v", x, y)
	}
	for _, m := range append(slices.Clone(x), y...) {
		if !slices.Contains(north, m) {
			t.Fatalf("player %d of a North group was not North's", m)
		}
	}
	// Merged again, the North has all ten back.
	moved(t, tm, "x")(asNobody(tm, x[0]).Continue("inner"))
	moved(t, tm, "x1")(asNobody(tm, x[0]).Answer("x1", "x"))
	moved(t, tm, "y")(asNobody(tm, y[0]).Continue("inner"))
	moved(t, tm, "y1")(asNobody(tm, y[0]).Answer("y1", "y"))
	tm.Unfocus()
	if got := tm.Strands[0].Members; len(got) != 10 || !sameSet(got, north) {
		t.Fatalf("North merged: %v, want %v", got, north)
	}
}

// asNobody makes the next action player slot's, holding no role.
func asNobody(tm *Team, slot int64) *Team {
	tm.Unfocus()
	tm.Viewer = &Viewer{Slot: slot}
	tm.PressChanged = nil
	return tm
}

func sameSet(a, b []int64) bool {
	a, b = slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b))
	return slices.Equal(a, b)
}

// Each phone sees its own group of a group, and may act on no other's.
func TestNestedPhones(t *testing.T) {
	tm := begin(t, citadel(t))
	tm.Unfocus()
	tm.Players, tm.Size = []int64{1, 2, 3, 4, 5, 6}, 6 // North 1, 3, 5; South 2, 4, 6
	tm.Deal(tm.Players, nil)
	moved(t, tm, "a")(asNobody(tm, 1).Continue("split"))
	// North 1, 3, 5: Tower 1, 5; Crypt 3.
	if v := asNobody(tm, 3).View(); v.Group != "North › Crypt" || v.Node != "inner" {
		t.Fatalf("player 3: %+v", v)
	}
	moved(t, tm, "x")(asNobody(tm, 5).Continue("inner"))
	if _, _, err := asNobody(tm, 1).Continue("inner"); !errors.Is(err, ErrStale) {
		t.Fatalf("1 setting off after 5: %v, want stale", err)
	}
	if _, _, err := asNobody(tm, 3).Answer("x1", "x"); !errors.Is(err, ErrNotYours) {
		t.Fatalf("the Crypt's player at the Tower's step: %v", err)
	}
	if _, _, err := asNobody(tm, 2).Answer("x1", "x"); !errors.Is(err, ErrNotYours) {
		t.Fatalf("a South player at the Tower's step: %v", err)
	}
	if v := asNobody(tm, 2).View(); v.Group != "South" || len(v.Groups) != 3 || v.Groups[0].Label != "North › Tower" || v.Groups[0].At != "x1" {
		t.Fatalf("player 2's view: %+v", v)
	}
	moved(t, tm, "x1")(asNobody(tm, 1).Answer("x1", "x"))
	// It waits for the Crypt alone, to go on as the North — whatever the
	// South is doing.
	if v := asNobody(tm, 1).View(); v.Type != graph.Join || v.Node != "ij" || v.Awaiting != 1 || v.Rejoin != "North" {
		t.Fatalf("player 1 waits at the inner join: %+v", v)
	}
}

// A step done together in a group of a group waits for that group only.
func TestNestedUnanimous(t *testing.T) {
	g := citadel(t)
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "x1" {
			g.Nodes[i].PuzzleCfg.Unanimous = true
		}
	}
	tm := begin(t, g)
	tm.Unfocus()
	tm.Players, tm.Size = []int64{1, 2, 3, 4, 5, 6, 7, 8}, 8 // North 1,3,5,7: Tower 1,5; Crypt 3,7
	tm.Deal(tm.Players, nil)
	moved(t, tm, "a")(asNobody(tm, 1).Continue("split"))
	moved(t, tm, "x")(asNobody(tm, 1).Continue("inner"))
	res := moved(t, tm, "1 agrees")(asNobody(tm, 1).Answer("x1", "x"))
	if res.Waiting == nil || res.Waiting.Of != 2 {
		t.Fatalf("waiting %+v: the Tower's two", res.Waiting)
	}
	if res := moved(t, tm, "5 agrees")(asNobody(tm, 5).Answer("x1", "x")); !res.Accepted {
		t.Fatalf("res %+v", res)
	}
}

// A newcomer goes to the smaller outer group, then the smaller of its
// groups; no one else moves.
func TestNestedNewcomer(t *testing.T) {
	tm := begin(t, citadel(t))
	tm.Unfocus()
	tm.Players = []int64{1, 2, 3, 4, 5}
	tm.Deal(tm.Players, nil) // North 1, 3, 5; South 2, 4
	moved(t, tm, "a")(asNobody(tm, 1).Continue("split"))
	tm.Unfocus() // Tower 1, 5; Crypt 3
	tm.Players = []int64{1, 2, 3, 4, 5, 6, 7}
	tm.Deal(tm.Players, nil) // 6 → South (2 < 3); 7 → North (3 = 3, first), its Crypt (1 < 2)
	want := map[string][]int64{"a>x": {1, 5}, "a>y": {3, 7}, "b": {2, 4, 6}}
	for _, st := range tm.Strands {
		if !slices.Equal(st.Members, want[st.Key()]) {
			t.Fatalf("%s: %v, want %v (all: %v)", st.Key(), st.Members, want[st.Key()], tm.Strands)
		}
	}
}

// The time limit ends every level at once.
func TestNestedTimeLimit(t *testing.T) {
	g := citadel(t)
	g.TimeLimit = &graph.TimeLimit{Seconds: 60, To: "end"}
	tm := begin(t, g)
	moved(t, tm, "a")(tm.Continue("split"))
	moved(t, tm, "x")(tm.Continue("inner"))
	tm.Now = t0.Add(2 * time.Minute)
	res := moved(t, tm, "time")(tm.Act(func() (Result, *walkStep, error) { return tm.Answer("x1", "x") }))
	if !res.TimeUp || tm.Split != "" || !tm.Completed {
		t.Fatalf("res %+v split %q completed %v", res, tm.Split, tm.Completed)
	}
}

// An ending on an inner path ends the adventure for every group.
func TestNestedEnding(t *testing.T) {
	b := newGB().split("split", "a", "b").split("inner", "x", "y").
		puzzle("x1", "x").puzzle("y1", "y").join("ij").puzzle("b1", "b").join("join").end("end").
		add(graph.Node{Slug: "doom", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Failed}}).
		wire("start", "out", "split").wire("split", "a", "inner").wire("split", "b", "b1").
		wire("inner", "x", "x1").wire("inner", "y", "y1").wire("x1", "solved", "doom").wire("y1", "solved", "ij").
		wire("ij", "out", "join").wire("b1", "solved", "join").wire("join", "out", "end")
	tm := begin(t, b.g) // the Tower's path ends: a warning, not an error
	if r := validCheck(b.g); r.HasErrors() {
		t.Fatalf("errors %v", r.Errors())
	}
	moved(t, tm, "a")(tm.Continue("split"))
	moved(t, tm, "x")(tm.Continue("inner"))
	moved(t, tm, "x1")(tm.Answer("x1", "x"))
	if !tm.Completed || tm.Ending != graph.Failed || tm.Split != "" {
		t.Fatalf("completed %v %q split %q", tm.Completed, tm.Ending, tm.Split)
	}
}

// A nested split team survives the simulator's trip through the browser.
func TestNestedJSON(t *testing.T) {
	tm := begin(t, citadel(t))
	moved(t, tm, "a")(tm.Continue("split"))
	moved(t, tm, "x")(tm.Continue("inner"))
	tm.View()
	raw, _ := json.Marshal(tm)
	var back Team
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	back.G = tm.G
	back.Init()
	if got := keys(&back); !slices.Equal(got, []string{"a>x@x1", "a>y@inner", "b@split"}) {
		t.Fatalf("decoded %v", got)
	}
	moved(t, &back, "x1")(back.Answer("x1", "x"))
	moved(t, &back, "y")(back.Continue("inner"))
	moved(t, &back, "y1")(back.Answer("y1", "y"))
	if got := keys(&back); !slices.Equal(got, []string{"a@a2", "b@split"}) {
		t.Fatalf("merged %v", got)
	}
}

// Two splits in a row on one path: the group splits, meets, splits
// again, meets, and goes on.
func TestNestedTwiceOnAPath(t *testing.T) {
	g := newGB().split("split", "a", "b").split("i1", "x", "y").split("i2", "p", "q").
		puzzle("x1", "x").puzzle("y1", "y").join("j1").puzzle("p1", "p").puzzle("q1", "q").join("j2").
		puzzle("b1", "b").join("join").end("end").
		wire("start", "out", "split").wire("split", "a", "i1").wire("split", "b", "b1").
		wire("i1", "x", "x1").wire("i1", "y", "y1").wire("x1", "solved", "j1").wire("y1", "solved", "j1").
		wire("j1", "out", "i2").wire("i2", "p", "p1").wire("i2", "q", "q1").wire("p1", "solved", "j2").wire("q1", "solved", "j2").
		wire("j2", "out", "join").wire("b1", "solved", "join").wire("join", "out", "end").valid(t)
	tm := begin(t, g)
	moved(t, tm, "a")(tm.Continue("split"))
	moved(t, tm, "x")(tm.Continue("i1"))
	moved(t, tm, "x1")(tm.Answer("x1", "x"))
	moved(t, tm, "y")(tm.Continue("i1"))
	moved(t, tm, "y1")(tm.Answer("y1", "y"))
	if got := keys(tm); !slices.Equal(got, []string{"a>p@i2", "a>q@i2", "b@split"}) {
		t.Fatalf("met at j1 and split again at i2: %v", got)
	}
	moved(t, tm, "p")(tm.Continue("i2"))
	moved(t, tm, "p1")(tm.Answer("p1", "p"))
	moved(t, tm, "q")(tm.Continue("i2"))
	moved(t, tm, "q1")(tm.Answer("q1", "q"))
	moved(t, tm, "b")(tm.Continue("split"))
	moved(t, tm, "b1")(tm.Answer("b1", "b"))
	if !tm.Completed {
		t.Fatalf("strands %v", keys(tm))
	}
}

// Three deep, every level splitting at once on arrival.
func TestNestedThreeDeep(t *testing.T) {
	g := newGB().split("s1", "a", "b").split("s2", "c", "d").split("s3", "e", "f").
		puzzle("e1", "x").puzzle("f1", "x").join("j3").puzzle("d1", "x").join("j2").puzzle("b1", "x").join("j1").end("end").
		wire("start", "out", "s1").wire("s1", "a", "s2").wire("s1", "b", "b1").
		wire("s2", "c", "s3").wire("s2", "d", "d1").
		wire("s3", "e", "e1").wire("s3", "f", "f1").wire("e1", "solved", "j3").wire("f1", "solved", "j3").
		wire("j3", "out", "j2").wire("d1", "solved", "j2").wire("j2", "out", "j1").wire("b1", "solved", "j1").wire("j1", "out", "end").valid(t)
	tm := begin(t, g)
	moved(t, tm, "a")(tm.Continue("s1"))
	moved(t, tm, "c")(tm.Continue("s2"))
	if got := keys(tm); !slices.Equal(got, []string{"a>c>e@s3", "a>c>f@s3", "a>d@s2", "b@s1"}) {
		t.Fatalf("three deep: %v", got)
	}
	steps := []struct{ node, how string }{
		{"s3", "go"}, {"e1", "x"}, {"s3", "go"}, {"f1", "x"}, {"s2", "go"}, {"d1", "x"}, {"s1", "go"}, {"b1", "x"},
	}
	for _, s := range steps {
		if s.how == "go" {
			moved(t, tm, s.node)(tm.Continue(s.node))
		} else {
			moved(t, tm, s.node)(tm.Answer(s.node, s.how))
		}
	}
	if !tm.Completed {
		t.Fatalf("strands %v", keys(tm))
	}
}

func validCheck(g *graph.Graph) valid.Report { return valid.Check(g) }

// A join counts the sibling paths it waits for, not the groups they have
// split into: the North waiting at the gate waits for the South once,
// though the South is two groups by now.
func TestJoinAwaitsPathsNotGroups(t *testing.T) {
	g := newGB().split("split", "a", "b").split("inner", "x", "y").
		puzzle("x1", "x").puzzle("y1", "y").join("ij").puzzle("a1", "a").join("join").end("end").
		wire("start", "out", "split").wire("split", "a", "a1").wire("split", "b", "inner").
		wire("inner", "x", "x1").wire("inner", "y", "y1").wire("x1", "solved", "ij").wire("y1", "solved", "ij").
		wire("ij", "out", "join").wire("a1", "solved", "join").wire("join", "out", "end").valid(t)
	tm := begin(t, g)
	moved(t, tm, "a")(tm.Continue("split"))
	moved(t, tm, "a1")(tm.Answer("a1", "a"))
	moved(t, tm, "b")(tm.Continue("split"))
	tm.Unfocus()
	tm.focusOn(0) // the North, at the gate
	if tm.Current != "join" {
		t.Fatalf("focused on %q", tm.Current)
	}
	if v := tm.view(); v.Awaiting != 1 || v.Rejoin != "" {
		t.Fatalf("awaiting %d, rejoin %q; want the South once, as the team", v.Awaiting, v.Rejoin)
	}
}
