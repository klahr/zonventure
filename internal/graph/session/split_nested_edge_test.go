package session

import (
	"errors"
	"slices"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

// More edge cases of nested splits: who is in which group when players
// come, go and hold roles; the steps that sit inside an inner path; and
// the ways a team can arrive inside one without having split there.

// rolesCitadel is citadel with roles: the outer paths the scout's and
// the medic's; the inner paths the climber's and nobody's.
func rolesCitadel(t *testing.T) *graph.Graph {
	g := citadel(t)
	g.Roles = []graph.Role{{Slug: "scout", Name: "Scout"}, {Slug: "medic", Name: "Medic"}, {Slug: "climber", Name: "Climber"}, {Slug: "crew", Name: "Crew", Default: true}}
	for i := range g.Nodes {
		switch g.Nodes[i].Slug {
		case "split":
			g.Nodes[i].SplitCfg.Paths[0].Role, g.Nodes[i].SplitCfg.Paths[1].Role = "scout", "medic"
		case "inner":
			g.Nodes[i].SplitCfg.Paths[0].Role = "climber"
		}
	}
	return g
}

func members(tm *Team) map[string][]int64 {
	tm.Unfocus()
	out := map[string][]int64{}
	for _, st := range tm.Strands {
		out[st.Key()] = st.Members
	}
	return out
}

// An inner path's role takes its player if they are in the group that
// splits; a holder in the other outer group leaves it only a share.
func TestNestedRoleHolderElsewhere(t *testing.T) {
	tm := begin(t, rolesCitadel(t))
	tm.Unfocus()
	// 1 scout, 2 medic, 3 climber — but the climber is shared out to
	// the south's... no: 3 holds a role of no outer path, so is shared:
	// north 1, 3; south 2, 4.
	tm.Players = []int64{1, 2, 3, 4}
	tm.RoleHolders = map[string]int64{"scout": 1, "medic": 2, "climber": 3}
	tm.Deal(tm.Players, tm.RoleHolders)
	moved(t, tm, "a")(asNobody(tm, 1).Continue("split"))
	m := members(tm)
	if !slices.Equal(m["a>x"], []int64{3}) || !slices.Equal(m["a>y"], []int64{1}) || !slices.Equal(m["b"], []int64{2, 4}) {
		t.Fatalf("the climber climbs; the scout goes to the crypt: %v", m)
	}

	// Now with the climber in the south: the tower is a share only.
	tm = begin(t, rolesCitadel(t))
	tm.Unfocus()
	tm.Players = []int64{1, 2, 3, 4, 5}
	tm.RoleHolders = map[string]int64{"scout": 1, "medic": 2, "climber": 4}
	tm.Deal(tm.Players, tm.RoleHolders) // north 1, 3, 5; south 2, 4
	m = members(tm)
	if !slices.Equal(m["a"], []int64{1, 3, 5}) || !slices.Equal(m["b"], []int64{2, 4}) {
		t.Fatalf("outer groups: %v", m)
	}
	moved(t, tm, "a")(asNobody(tm, 1).Continue("split"))
	m = members(tm)
	if len(m["a>x"])+len(m["a>y"]) != 3 || len(m["a>x"]) == 0 || len(m["a>y"]) == 0 {
		t.Fatalf("the north shared out with no climber among them: %v", m)
	}
}

// An inner group nobody playing is in is anyone's — anyone of the whole
// team who has finished their own: the crypt's only player went home.
func TestNestedEmptyInnerGroupIsAnyones(t *testing.T) {
	tm := begin(t, citadel(t))
	tm.Unfocus()
	tm.Players = []int64{1, 2, 3, 4} // north 1, 3: tower 1, crypt 3; south 2, 4
	tm.Deal(tm.Players, nil)
	moved(t, tm, "a")(asNobody(tm, 1).Continue("split"))
	tm.Unfocus()
	tm.Players = []int64{1, 2, 4} // 3 leaves
	moved(t, tm, "x")(asNobody(tm, 1).Continue("inner"))
	moved(t, tm, "x1")(asNobody(tm, 1).Answer("x1", "x"))
	if v := asNobody(tm, 1).View(); v.Node != "inner" || v.Group != "North › Crypt" {
		t.Fatalf("player 1, done with the tower, should be offered the empty crypt: %+v", v)
	}
	moved(t, tm, "y")(asNobody(tm, 1).Continue("inner"))
	moved(t, tm, "y1")(asNobody(tm, 1).Answer("y1", "y"))
	if m := members(tm); !slices.Equal(m["a"], []int64{1, 3}) {
		t.Fatalf("the north keeps the one who left, for when they come back: %v", m)
	}
}

// A player alone walks three levels of groups, every one in turn.
func TestNestedAlone(t *testing.T) {
	g := newGB().split("s1", "a", "b").split("s2", "c", "d").split("s3", "e", "f").
		puzzle("e1", "x").puzzle("f1", "x").join("j3").puzzle("d1", "x").join("j2").puzzle("b1", "x").join("j1").end("end").
		wire("start", "out", "s1").wire("s1", "a", "s2").wire("s1", "b", "b1").
		wire("s2", "c", "s3").wire("s2", "d", "d1").
		wire("s3", "e", "e1").wire("s3", "f", "f1").wire("e1", "solved", "j3").wire("f1", "solved", "j3").
		wire("j3", "out", "j2").wire("d1", "solved", "j2").wire("j2", "out", "j1").wire("b1", "solved", "j1").wire("j1", "out", "end").valid(t)
	tm := begin(t, g)
	tm.Unfocus()
	tm.Players = []int64{9}
	tm.Deal(tm.Players, nil)
	for range 20 {
		if tm.Completed {
			return
		}
		v := asNobody(tm, 9).View()
		switch v.Type {
		case graph.Split:
			moved(t, tm, v.Node)(asNobody(tm, 9).Continue(v.Node))
		case graph.Puzzle:
			moved(t, tm, v.Node)(asNobody(tm, 9).Answer(v.Node, "x"))
		default:
			t.Fatalf("the player alone is stuck on %s %q, groups %v", v.Type, v.Node, keys(tm))
		}
	}
	t.Fatalf("not finished: %v", keys(tm))
}

// A home screen on an inner path is the inner group's to go back to;
// one on the outer path, before the inner split, is not.
func TestNestedHomeHub(t *testing.T) {
	g := newGB().split("split", "a", "b").split("inner", "x", "y").
		add(graph.Node{Slug: "home", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Home: true, Branches: []graph.Branch{{Port: "look"}, {Port: "go"}}}}).
		puzzle("look1", "x").puzzle("go1", "x").puzzle("y1", "y").join("ij").puzzle("b1", "b").join("join").end("end").
		wire("start", "out", "split").wire("split", "a", "inner").wire("split", "b", "b1").
		wire("inner", "x", "home").wire("home", "look", "look1").wire("look1", "solved", "home").
		wire("home", "go", "go1").wire("go1", "solved", "ij").
		wire("inner", "y", "y1").wire("y1", "solved", "ij").
		wire("ij", "out", "join").wire("b1", "solved", "join").wire("join", "out", "end").valid(t)
	tm := begin(t, g)
	moved(t, tm, "a")(tm.Continue("split"))
	moved(t, tm, "x")(tm.Continue("inner"))
	moved(t, tm, "look")(tm.Choose("home", "look"))
	if v := tm.View(); v.BackTo != "home" {
		t.Fatalf("the tower may go back to its own home screen: %+v", v.BackTo)
	}
	moved(t, tm, "back")(tm.Back("look1"))
	moved(t, tm, "go")(tm.Choose("home", "go"))
	moved(t, tm, "go1")(tm.Answer("go1", "x"))
	moved(t, tm, "y")(tm.Continue("inner"))
	moved(t, tm, "y1")(tm.Answer("y1", "y"))
	moved(t, tm, "b")(tm.Continue("split"))
	moved(t, tm, "b1")(tm.Answer("b1", "b"))
	if !tm.Completed {
		t.Fatalf("strands %v", keys(tm))
	}
}

// The organisers' rescue moves an inner group past its step, the team
// as a whole acting, and the others stay where they are.
func TestNestedRescue(t *testing.T) {
	tm := begin(t, citadel(t))
	moved(t, tm, "a")(tm.Continue("split"))
	moved(t, tm, "x")(tm.Continue("inner"))
	tm.Unfocus()
	tm.Viewer = nil
	moved(t, tm, "rescue")(tm.Rescue("x1"))
	if got := keys(tm); !slices.Equal(got, []string{"a>x@ij", "a>y@inner", "b@split"}) {
		t.Fatalf("after the rescue: %v", got)
	}
	if _, _, err := tm.Rescue("x1"); !errors.Is(err, ErrStale) {
		t.Fatalf("a second rescue of the step it left: %v", err)
	}
}

// Put straight onto an inner split, as the designer's preview and a dev
// phone do, a team splits there as if it were the team's own, and walks
// through the outer join it never split for.
func TestPlaceOnAnInnerSplit(t *testing.T) {
	tm, err := Place(citadel(t), "inner", t0)
	if err != nil {
		t.Fatal(err)
	}
	if tm.Split != "inner" || len(tm.Strands) != 2 {
		t.Fatalf("split %q, %v", tm.Split, keys(tm))
	}
	moved(t, tm, "x")(tm.Continue("inner"))
	moved(t, tm, "x1")(tm.Answer("x1", "x"))
	moved(t, tm, "y")(tm.Continue("inner"))
	moved(t, tm, "y1")(tm.Answer("y1", "y"))
	moved(t, tm, "a2")(tm.Answer("a2", "a"))
	if !tm.Completed {
		t.Fatalf("through the outer join, unsplit: current %q, %v", tm.Current, keys(tm))
	}
}

// A time limit that sends the team into an inner path: the team walks it
// as one, and splits at whatever split it meets next.
func TestTimeLimitIntoAnInnerPath(t *testing.T) {
	g := citadel(t)
	g.TimeLimit = &graph.TimeLimit{Seconds: 60, To: "inner"}
	tm := begin(t, g)
	// The south sets off; the north, never reaching the inner split, has
	// not yet been where the limit sends the team.
	tm.Unfocus()
	tm.focusOn(1)
	moved(t, tm, "b")(tm.Continue("split"))
	tm.Now = tm.Now.Add(120e9)
	res := moved(t, tm, "time")(tm.Act(func() (Result, *walkStep, error) { return tm.Answer("b1", "b") }))
	if !res.TimeUp || tm.Split != "inner" || len(tm.Strands) != 2 {
		t.Fatalf("res %+v: split %q, %v", res, tm.Split, keys(tm))
	}
}

// Nested group names reach the phones in their language.
func TestNestedLabelsTranslated(t *testing.T) {
	g := citadel(t)
	g.Language = "en"
	g.Translations = map[string]map[string]string{"sv": {
		graph.NodeKey("split", "path:a:label"): "Norr",
		graph.NodeKey("inner", "path:x:label"): "Tornet",
	}}
	tm := begin(t, g)
	tm.Lang = "sv"
	moved(t, tm, "a")(tm.Continue("split"))
	if v := tm.View(); v.Group != "Norr › Tornet" || v.Groups[1].Label != "Norr › Crypt" || v.Groups[2].Label != "South" {
		t.Fatalf("view %+v", v.Groups)
	}
}

// An inner path for the default role takes everyone in the splitting
// group who holds no other role.
func TestNestedDefaultRolePath(t *testing.T) {
	g := rolesCitadel(t)
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "inner" {
			g.Nodes[i].SplitCfg.Paths[1].Role = "crew"
		}
	}
	tm := begin(t, g)
	tm.Unfocus()
	tm.Players = []int64{1, 2, 3, 4, 5, 6}
	tm.RoleHolders = map[string]int64{"scout": 1, "medic": 2, "climber": 3} // north 1, 3, 5; south 2, 4, 6
	tm.Deal(tm.Players, tm.RoleHolders)
	moved(t, tm, "a")(asNobody(tm, 1).Continue("split"))
	m := members(tm)
	// The climber climbs; 5, holding no role, is crew; the scout, holding
	// a role that is no inner path's, is shared to the smaller: the tower.
	if !slices.Equal(m["a>x"], []int64{3, 1}) || !slices.Equal(m["a>y"], []int64{5}) {
		t.Fatalf("inner groups %v", m)
	}
}

// A player holding the roles of both outer paths walks both, and the
// inner groups of one of them, in turn — the others go about theirs.
func TestNestedTwoRoles(t *testing.T) {
	tm := begin(t, rolesCitadel(t))
	tm.Unfocus()
	tm.Players = []int64{1, 2}
	tm.RoleHolders = map[string]int64{"scout": 1, "medic": 1, "climber": 2} // 1 is north and south; 2 shared: north
	tm.Deal(tm.Players, tm.RoleHolders)
	m := members(tm)
	if !slices.Equal(m["a"], []int64{1, 2}) || !slices.Equal(m["b"], []int64{1}) {
		t.Fatalf("outer %v", m)
	}
	if v := asNobody(tm, 1).View(); v.Group != "North" {
		t.Fatalf("1 starts with the north: %+v", v.Group)
	}
	moved(t, tm, "b")(asNobody(tm, 1).Continue("split"))  // 1 sets the north off: lands at the inner split
	moved(t, tm, "x")(asNobody(tm, 2).Continue("inner"))  // the climber climbs
	moved(t, tm, "x1")(asNobody(tm, 2).Answer("x1", "x")) // and waits in the hall
	moved(t, tm, "y")(asNobody(tm, 1).Continue("inner"))  // 1, in the crypt
	moved(t, tm, "y1")(asNobody(tm, 1).Answer("y1", "y")) // the north meets
	moved(t, tm, "a2")(asNobody(tm, 2).Answer("a2", "a")) // and waits at the gate
	if v := asNobody(tm, 1).View(); v.Group != "South" || v.Node != "split" {
		t.Fatalf("1, done with the north, goes on with the south: %+v", v)
	}
	moved(t, tm, "s")(asNobody(tm, 1).Continue("split"))
	moved(t, tm, "b1")(asNobody(tm, 1).Answer("b1", "b"))
	if !tm.Completed {
		t.Fatalf("strands %v", keys(tm))
	}
}

// The organisers set off one named group from a split several groups
// wait on — the one they mean, not the first — and are told when the
// group has moved or met its siblings since they looked.
func TestRescueAGroupFromItsSplit(t *testing.T) {
	tm := begin(t, citadel(t))
	tm.Unfocus()
	tm.Viewer = nil
	moved(t, tm, "set the south off")(tm.RescueGroup("split", "b"))
	if got := keys(tm); !slices.Equal(got, []string{"a@split", "b@b1"}) {
		t.Fatalf("the south, not the north: %v", got)
	}
	if _, _, err := tm.RescueGroup("split", "b"); !errors.Is(err, ErrStale) {
		t.Fatalf("the south again: %v", err)
	}
	moved(t, tm, "set the north off")(tm.RescueGroup("split", "a"))
	moved(t, tm, "set the crypt off")(tm.RescueGroup("inner", "a>y"))
	if got := keys(tm); !slices.Equal(got, []string{"a>x@inner", "a>y@y1", "b@b1"}) {
		t.Fatalf("the crypt set off: %v", got)
	}
	moved(t, tm, "past the crypt's puzzle")(tm.RescueGroup("y1", "a>y"))
	moved(t, tm, "set the tower off")(tm.RescueGroup("inner", "a>x"))
	moved(t, tm, "past the tower's")(tm.RescueGroup("x1", "a>x"))
	// The tower and crypt are the north again: a rescue naming either is
	// stale, the north's is not.
	if _, _, err := tm.RescueGroup("a2", "a>x"); !errors.Is(err, ErrStale) {
		t.Fatalf("a group that has met its siblings: %v", err)
	}
	moved(t, tm, "the north")(tm.RescueGroup("a2", "a"))
	if got := keys(tm); !slices.Equal(got, []string{"a@join", "b@b1"}) {
		t.Fatalf("north at the gate: %v", got)
	}
	if !CanRescue(graph.Node{Type: graph.Split}) {
		t.Error("a split is somewhere a group can be moved on from")
	}
}

// A rescue at a split must say which group: the split names none.
func TestRescueAtASplitNamesTheGroup(t *testing.T) {
	tm := begin(t, citadel(t))
	if _, _, err := tm.RescueGroup("split", ""); !errors.Is(err, ErrWhichGroup) || Code(err) != "which_group" {
		t.Fatalf("no group named: %v", err)
	}
	moved(t, tm, "a")(tm.RescueGroup("split", "a"))
	// A group's own step needs no name.
	moved(t, tm, "x")(tm.RescueGroup("inner", "a>x"))
	moved(t, tm, "x1")(tm.RescueGroup("x1", ""))
}

// An organiser's move takes a group to its inner split knowing neither
// who plays nor who holds which role: the group divides by the roles it
// was dealt by all the same. Regression: the climber went to the crypt.
func TestNestedDealOnAnOrganisersMove(t *testing.T) {
	tm := begin(t, rolesCitadel(t))
	tm.Unfocus()
	tm.Deal([]int64{1, 2, 3, 4}, map[string]int64{"scout": 1, "medic": 2, "climber": 3}) // north 1, 3
	tm.Players, tm.RoleHolders, tm.Viewer = nil, nil, nil
	moved(t, tm, "set the north off")(tm.RescueGroup("split", "a"))
	if m := members(tm); !slices.Equal(m["a>x"], []int64{3}) || !slices.Equal(m["a>y"], []int64{1}) {
		t.Fatalf("the climber climbs, whoever moved the north: %v", m)
	}
}

// A group divides among its players still playing: those gone are put
// in after, for when they come back, but neither dealt nor counted as
// there. Regression: the two who stayed went up the tower together.
func TestNestedDealsThoseStillPlaying(t *testing.T) {
	tm := begin(t, citadel(t))
	tm.Unfocus()
	tm.Players = []int64{1, 2, 3, 4, 5, 6, 7, 8}
	tm.Deal(tm.Players, nil)               // north 1, 3, 5, 7
	tm.Players = []int64{1, 2, 4, 5, 6, 8} // 3 and 7 go home
	moved(t, tm, "a")(asNobody(tm, 1).Continue("split"))
	m := members(tm)
	if !slices.Equal(m["a>x"], []int64{1, 3}) || !slices.Equal(m["a>y"], []int64{5, 7}) {
		t.Fatalf("one of 1 and 5 to each, the ones gone shared after: %v", m)
	}
}
