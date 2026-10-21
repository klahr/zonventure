package session

import (
	"errors"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/valid"
)

// Edge cases of parallel paths: the shapes an author can build around a
// split that the plain church-and-well expedition does not.

// gb builds a graph by hand, a node and its wires at a time.
type gb struct{ g *graph.Graph }

func newGB() *gb {
	return &gb{g: &graph.Graph{Nodes: []graph.Node{{Slug: "start", Type: graph.Start}}}}
}

func (b *gb) add(n graph.Node) *gb { b.g.Nodes = append(b.g.Nodes, n); return b }

func (b *gb) wire(from, port, to string) *gb {
	b.g.Edges = append(b.g.Edges, graph.Edge{From: from, FromPort: port, To: to})
	return b
}

func (b *gb) puzzle(slug, answer string) *gb {
	return b.add(graph.Node{Slug: slug, Type: graph.Puzzle, Title: slug, PuzzleCfg: &graph.PuzzleConfig{
		Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Points: 1, Matchers: []match.Matcher{{Kind: match.Exact, Value: answer}}}},
	}})
}

func (b *gb) split(slug string, ports ...string) *gb {
	cfg := &graph.SplitConfig{}
	for _, p := range ports {
		cfg.Paths = append(cfg.Paths, graph.SplitPath{Port: p})
	}
	return b.add(graph.Node{Slug: slug, Type: graph.Split, Title: slug, SplitCfg: cfg})
}

func (b *gb) join(slug string) *gb {
	return b.add(graph.Node{Slug: slug, Type: graph.Join, Title: slug})
}

func (b *gb) end(slug string) *gb {
	return b.add(graph.Node{Slug: slug, Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}})
}

// valid fails the test unless the graph passes the validator, so every
// case here is one an author could actually publish.
func (b *gb) valid(t *testing.T) *graph.Graph {
	t.Helper()
	if r := valid.Check(b.g); r.HasErrors() {
		t.Fatalf("graph does not validate: %v", r.Errors())
	}
	return b.g
}

func begin(t *testing.T, g *graph.Graph) *Team {
	t.Helper()
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

// Two splits back to back: the last group into the first join walks
// straight on into the second split, and the team must split again.
func TestSplitStraightAfterAJoin(t *testing.T) {
	g := newGB().split("s1", "a", "b").puzzle("a1", "x").puzzle("b1", "x").join("j1").
		split("s2", "c", "d").puzzle("c1", "x").puzzle("d1", "x").join("j2").end("end").
		wire("start", "out", "s1").wire("s1", "a", "a1").wire("s1", "b", "b1").
		wire("a1", "solved", "j1").wire("b1", "solved", "j1").wire("j1", "out", "s2").
		wire("s2", "c", "c1").wire("s2", "d", "d1").wire("c1", "solved", "j2").wire("d1", "solved", "j2").
		wire("j2", "out", "end").valid(t)
	tm := begin(t, g)
	moved(t, tm, "a")(tm.Continue("s1"))
	moved(t, tm, "a1")(tm.Answer("a1", "x"))
	moved(t, tm, "b")(tm.Continue("s1"))
	moved(t, tm, "b1")(tm.Answer("b1", "x"))
	if tm.Split != "s2" || len(tm.Strands) != 2 || tm.Strands[0].Current != "s2" {
		t.Fatalf("after the first join the team should split again at s2: split %q strands %+v current %q", tm.Split, tm.Strands, tm.Current)
	}
	if v := tm.View(); v.Node != "s2" || v.Type != graph.Split {
		t.Fatalf("view %+v", v)
	}
	moved(t, tm, "c")(tm.Continue("s2"))
	moved(t, tm, "c1")(tm.Answer("c1", "x"))
	moved(t, tm, "d")(tm.Continue("s2"))
	moved(t, tm, "d1")(tm.Answer("d1", "x"))
	if !tm.Completed || tm.Split != "" {
		t.Fatalf("completed %v split %q", tm.Completed, tm.Split)
	}
}

// A path that is nothing but the way to the join: its group sets off and
// is there at once.
func TestEmptyPath(t *testing.T) {
	g := newGB().split("s", "a", "b").puzzle("a1", "x").join("j").end("end").
		wire("start", "out", "s").wire("s", "a", "a1").wire("s", "b", "j").
		wire("a1", "solved", "j").wire("j", "out", "end").valid(t)
	tm := begin(t, g)
	moved(t, tm, "a")(tm.Continue("s"))
	moved(t, tm, "b")(tm.Continue("s"))
	if tm.Strands[1].Current != "j" {
		t.Fatalf("b should wait at the join: %+v", tm.Strands)
	}
	moved(t, tm, "a1")(tm.Answer("a1", "x"))
	if !tm.Completed {
		t.Fatal("the team should finish")
	}
}

// A choice hub on one path is that group's alone: it chooses, comes
// back, and the hub's done takes it on to the join.
func TestHubOnAPath(t *testing.T) {
	g := newGB().split("s", "a", "b").
		add(graph.Node{Slug: "hub", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Branches: []graph.Branch{{Port: "x"}, {Port: "y"}}}}).
		puzzle("hx", "x").puzzle("hy", "y").puzzle("b1", "b").join("j").end("end").
		wire("start", "out", "s").wire("s", "a", "hub").wire("s", "b", "b1").
		wire("hub", "x", "hx").wire("hub", "y", "hy").wire("hx", "solved", "hub").wire("hy", "solved", "hub").
		wire("hub", "done", "j").wire("b1", "solved", "j").wire("j", "out", "end").valid(t)
	tm := begin(t, g)
	moved(t, tm, "a")(tm.Continue("s"))
	if v := tm.View(); v.Node != "hub" || len(v.Offer) != 2 {
		t.Fatalf("view %+v", v)
	}
	moved(t, tm, "x")(tm.Choose("hub", "x"))
	moved(t, tm, "hx")(tm.Answer("hx", "x"))
	moved(t, tm, "y")(tm.Choose("hub", "y"))
	moved(t, tm, "hy")(tm.Answer("hy", "y"))
	if tm.Strands[0].Current != "j" {
		t.Fatalf("both branches done: the hub's done should take a to the join: %+v", tm.Strands)
	}
	moved(t, tm, "b")(tm.Continue("s"))
	moved(t, tm, "b1")(tm.Answer("b1", "b"))
	if !tm.Completed {
		t.Fatal("finish")
	}
}

// A home screen on a path: the group goes into a branch and back to its
// home screen, and leaves it by a branch that leads on to the join.
func TestHomeHubOnAPath(t *testing.T) {
	g := newGB().split("s", "a", "b").
		add(graph.Node{Slug: "home", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Home: true, Branches: []graph.Branch{{Port: "look"}, {Port: "leave"}}}}).
		puzzle("look1", "x").puzzle("leave1", "go").puzzle("b1", "b").join("j").end("end").
		wire("start", "out", "s").wire("s", "a", "home").wire("s", "b", "b1").
		wire("home", "look", "look1").wire("look1", "solved", "home").
		wire("home", "leave", "leave1").wire("leave1", "solved", "j").
		wire("b1", "solved", "j").wire("j", "out", "end").valid(t)
	tm := begin(t, g)
	moved(t, tm, "a")(tm.Continue("s"))
	moved(t, tm, "look")(tm.Choose("home", "look"))
	// Back to the home screen, which is on the group's own path.
	if v := tm.View(); v.BackTo != "home" {
		t.Fatalf("a group inside its own home screen's branch may go back: %+v", v)
	}
	moved(t, tm, "back")(tm.Back("look1"))
	if tm.Strands[0].Current != "home" {
		t.Fatalf("strands %+v", tm.Strands)
	}
	moved(t, tm, "leave")(tm.Choose("home", "leave"))
	moved(t, tm, "leave1")(tm.Answer("leave1", "go"))
	moved(t, tm, "b")(tm.Continue("s"))
	moved(t, tm, "b1")(tm.Answer("b1", "b"))
	if !tm.Completed {
		t.Fatal("finish")
	}
}

// A home screen on a path owns nothing past its join: the team, together
// again after it, is not sent back onto the path.
func TestHomeHubOnAPathThenOn(t *testing.T) {
	g := newGB().split("s", "a", "b").
		add(graph.Node{Slug: "home", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Home: true, Branches: []graph.Branch{{Port: "look"}, {Port: "leave"}}}}).
		puzzle("look1", "x").puzzle("leave1", "go").puzzle("b1", "b").join("j").puzzle("post", "p").end("end").
		wire("start", "out", "s").wire("s", "a", "home").wire("s", "b", "b1").
		wire("home", "look", "look1").wire("look1", "solved", "home").
		wire("home", "leave", "leave1").wire("leave1", "solved", "j").
		wire("b1", "solved", "j").wire("j", "out", "post").wire("post", "solved", "end").valid(t)
	tm := begin(t, g)
	moved(t, tm, "a")(tm.Continue("s"))
	moved(t, tm, "leave")(tm.Choose("home", "leave"))
	moved(t, tm, "leave1")(tm.Answer("leave1", "go"))
	moved(t, tm, "b")(tm.Continue("s"))
	moved(t, tm, "b1")(tm.Answer("b1", "b"))
	if v := tm.View(); v.Node != "post" || v.BackTo != "" {
		t.Fatalf("together after the join, no going back onto path a: %+v", v)
	}
	if _, _, err := tm.Back("post"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("back from post: %v", err)
	}
	moved(t, tm, "post")(tm.Answer("post", "p"))
	if !tm.Completed {
		t.Fatal("finish")
	}
}

// A split on a home screen's branch: no group goes back to the home
// screen alone, and once together again the team may.
func TestSplitInsideAHomeBranch(t *testing.T) {
	g := newGB().
		add(graph.Node{Slug: "home", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Home: true, Branches: []graph.Branch{{Port: "go"}, {Port: "finish"}}}}).
		split("s", "a", "b").puzzle("a1", "x").puzzle("b1", "x").join("j").puzzle("after", "x").puzzle("last", "x").end("end").
		wire("start", "out", "home").wire("home", "go", "s").wire("s", "a", "a1").wire("s", "b", "b1").
		wire("a1", "solved", "j").wire("b1", "solved", "j").wire("j", "out", "after").wire("after", "solved", "home").
		wire("home", "finish", "last").wire("last", "solved", "end").valid(t)
	tm := begin(t, g)
	moved(t, tm, "go")(tm.Choose("home", "go"))
	if v := tm.View(); v.BackTo != "" {
		t.Fatalf("no going back to the home screen from the split: %+v", v.BackTo)
	}
	moved(t, tm, "a")(tm.Continue("s"))
	if _, _, err := tm.Back("a1"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("a group going back alone: %v", err)
	}
	moved(t, tm, "a1")(tm.Answer("a1", "x"))
	moved(t, tm, "b")(tm.Continue("s"))
	moved(t, tm, "b1")(tm.Answer("b1", "x"))
	if v := tm.View(); v.Node != "after" || v.BackTo != "home" {
		t.Fatalf("together again, the team may go back: %+v", v)
	}
}

// A hub that leads into a split again and again: a team coming back to
// a split it has played splits afresh, and its groups pass the steps
// they solved the first time straight through to the join.
func TestSplitAgainFromAHub(t *testing.T) {
	g := newGB().
		add(graph.Node{Slug: "hub", Type: graph.Hub, HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Branches: []graph.Branch{{Port: "one"}, {Port: "two"}}}}).
		split("s", "a", "b").puzzle("a1", "x").puzzle("b1", "x").join("j").puzzle("two1", "x").end("end").
		wire("start", "out", "hub").wire("hub", "one", "s").wire("hub", "two", "two1").wire("two1", "solved", "hub").
		wire("s", "a", "a1").wire("s", "b", "b1").wire("a1", "solved", "j").wire("b1", "solved", "j").
		wire("j", "out", "hub").wire("hub", "done", "end").valid(t)
	tm := begin(t, g)
	moved(t, tm, "one")(tm.Choose("hub", "one"))
	moved(t, tm, "a")(tm.Continue("s"))
	moved(t, tm, "a1")(tm.Answer("a1", "x"))
	moved(t, tm, "b")(tm.Continue("s"))
	moved(t, tm, "b1")(tm.Answer("b1", "x"))
	if tm.Current != "hub" || tm.Split != "" {
		t.Fatalf("back at the hub, together: current %q split %q", tm.Current, tm.Split)
	}
	moved(t, tm, "two")(tm.Choose("hub", "two"))
	moved(t, tm, "two1")(tm.Answer("two1", "x"))
	if !tm.Completed {
		t.Fatalf("both branches done, the team should finish: current %q", tm.Current)
	}
}

// Each group's clock starts when it sets off: a puzzle's time limit, its
// hints and its timeout count from the group's arrival, not the team's.
func TestGroupClocks(t *testing.T) {
	b := newGB().split("s", "a", "b").puzzle("a1", "x").puzzle("b1", "x").puzzle("late", "x").join("j").end("end").
		wire("start", "out", "s").wire("s", "a", "a1").wire("s", "b", "b1").
		wire("a1", "solved", "j").wire("b1", "solved", "j").wire("late", "solved", "j").wire("j", "out", "end")
	for i := range b.g.Nodes {
		if b.g.Nodes[i].Slug == "b1" {
			cfg := b.g.Nodes[i].PuzzleCfg
			cfg.TimeLimitSeconds = 60
			cfg.Outputs = append(cfg.Outputs, graph.Output{Port: "slow", Trigger: graph.OnTimeout})
			cfg.Hints = []graph.Hint{{Text: "h", DelaySeconds: 30}}
		}
	}
	b.wire("b1", "slow", "late")
	tm := begin(t, b.valid(t))
	moved(t, tm, "a")(tm.Continue("s"))
	tm.Now = t0.Add(10 * time.Minute) // group a dawdles at a1
	moved(t, tm, "b")(tm.Continue("s"))
	tm.Now = t0.Add(10*time.Minute + 20*time.Second)
	if _, _, err := tm.RevealHint("b1", 0); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("a hint 20 s after b arrived is not due: %v", err)
	}
	if _, _, err := tm.Timeout("b1"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("no timeout yet: %v", err)
	}
	tm.Now = t0.Add(10*time.Minute + 40*time.Second)
	moved(t, tm, "hint")(tm.RevealHint("b1", 0))
	tm.Now = t0.Add(11*time.Minute + 1*time.Second)
	moved(t, tm, "timeout")(tm.Timeout("b1"))
	if tm.Strands[1].Current != "late" {
		t.Fatalf("strands %+v", tm.Strands)
	}
	// And a, still at a1 all this time, is unaffected; once it waits at
	// the join, the team as a whole is shown b's step and its own clock.
	moved(t, tm, "a1")(tm.Answer("a1", "x"))
	if v := tm.View(); v.Node != "late" || v.Deadline != nil {
		t.Fatalf("view %+v", v)
	}
}

// What one group carries, a gate after the join sees: the team's state
// is one, whichever group earned it.
func TestGateAfterJoinSeesEveryGroup(t *testing.T) {
	b := newGB().split("s", "a", "b").puzzle("a1", "x").puzzle("b1", "x").join("j").end("end").end("short").
		add(graph.Node{Slug: "ka", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.GrantItem, Item: "ka"}}).
		add(graph.Node{Slug: "kb", Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.GrantItem, Item: "kb"}}).
		add(graph.Node{Slug: "gate", Type: graph.Gate, GateCfg: &graph.GateConfig{Condition: cond.Predicate{Op: cond.OpAll, Of: []cond.Predicate{
			{Op: cond.OpItem, Item: "ka"}, {Op: cond.OpItem, Item: "kb"},
		}}}}).
		wire("start", "out", "s").wire("s", "a", "a1").wire("s", "b", "b1").
		wire("a1", "solved", "ka").wire("ka", "out", "j").wire("b1", "solved", "kb").wire("kb", "out", "j").
		wire("j", "out", "gate").wire("gate", "pass", "end").wire("gate", "fail", "short")
	b.g.Items = []graph.Item{{Slug: "ka"}, {Slug: "kb"}}
	tm := begin(t, b.valid(t))
	moved(t, tm, "a")(tm.Continue("s"))
	moved(t, tm, "a1")(tm.Answer("a1", "x"))
	moved(t, tm, "b")(tm.Continue("s"))
	moved(t, tm, "b1")(tm.Answer("b1", "x"))
	if tm.Current != "end" {
		t.Fatalf("the gate should pass on both keys: %q", tm.Current)
	}
}

// Steps done together on a path count the group: a two-key launch with
// its keys turned by the group's players alone.
func TestKeysWithinAGroup(t *testing.T) {
	b := newGB().split("s", "a", "b").puzzle("b1", "x").join("j").end("end").
		add(graph.Node{Slug: "launch", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
			Answer:  &graph.Answer{Kind: graph.AnswerKeys},
			Outputs: []graph.Output{{Port: "go", Trigger: graph.OnAnswer}},
		}}).
		wire("start", "out", "s").wire("s", "a", "launch").wire("s", "b", "b1").
		wire("launch", "go", "j").wire("b1", "solved", "j").wire("j", "out", "end")
	tm := begin(t, b.valid(t))
	tm.Unfocus()
	tm.Players, tm.Size = []int64{1, 2, 3, 4}, 4 // a: 1, 3
	tm.Deal(tm.Players, nil)
	moved(t, tm, "a")(asPlayer(tm, 1, nil).Continue("s"))
	res := moved(t, tm, "1 turns")(asPlayer(tm, 1, nil).Turn("launch"))
	if res.Waiting == nil || res.Waiting.Of != 2 {
		t.Fatalf("waiting %+v: the group's two keys", res.Waiting)
	}
	res = moved(t, tm, "3 turns")(asPlayer(tm, 3, nil).Turn("launch"))
	if !res.Accepted || tm.Strands[0].Current != "j" {
		t.Fatalf("res %+v strands %+v", res, tm.Strands)
	}
}

// Tasks on a path: a task whose role is with the other group is anyone's
// in this one.
func TestTasksOnAPath(t *testing.T) {
	b := newGB().split("s", "a", "b").puzzle("b1", "x").join("j").end("end").
		add(graph.Node{Slug: "work", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
			Blocks: []graph.Block{
				{ID: "t1", Kind: graph.BlockTask, Role: "scout", Text: "one", Answer: "1"},
				{ID: "t2", Kind: graph.BlockTask, Role: "medic", Text: "two", Answer: "2"},
			},
			Answer:  &graph.Answer{Kind: graph.AnswerTasks},
			Outputs: []graph.Output{{Port: "done", Trigger: graph.OnAnswer}},
		}}).
		wire("start", "out", "s").wire("s", "a", "work").wire("s", "b", "b1").
		wire("work", "done", "j").wire("b1", "solved", "j").wire("j", "out", "end")
	b.g.Roles = []graph.Role{{Slug: "scout"}, {Slug: "medic"}}
	b.g.Nodes[1].SplitCfg.Paths[0].Role = "scout"
	b.g.Nodes[1].SplitCfg.Paths[1].Role = "medic"
	tm := begin(t, b.valid(t))
	tm.Unfocus()
	tm.Players, tm.Size = []int64{1, 2, 3}, 3
	tm.RoleHolders = map[string]int64{"scout": 1, "medic": 2} // a: 1, 3; b: 2
	tm.Deal(tm.Players, tm.RoleHolders)
	moved(t, tm, "a")(asPlayer(tm, 3, nil).Continue("s"))
	moved(t, tm, "scout's")(asPlayer(tm, 1, nil).Task("work", "t1", "1"))
	// The medic is at the well: their task here is the group's to do.
	res := moved(t, tm, "medic's, by 3")(asPlayer(tm, 3, nil).Task("work", "t2", "2"))
	if !res.Accepted || tm.Strands[0].Current != "j" {
		t.Fatalf("res %+v strands %+v", res, tm.Strands)
	}
}

// A quiz question picked on a path scores as the team finishes.
func TestQuizOnAPath(t *testing.T) {
	b := newGB().split("s", "a", "b").puzzle("b1", "x").join("j").end("end").
		add(graph.Node{Slug: "q", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{
			{ID: "q1", Kind: graph.BlockQuiz, Text: "?", Options: []string{"a", "b", "c"}, Answer: "1", Worth: 5},
		}}}).
		wire("start", "out", "s").wire("s", "a", "q").wire("s", "b", "b1").
		wire("q", "out", "j").wire("b1", "solved", "j").wire("j", "out", "end")
	tm := begin(t, b.valid(t))
	moved(t, tm, "a")(tm.Continue("s"))
	if _, _, err := tm.Continue("q"); !errors.Is(err, ErrUnanswered) {
		t.Fatalf("continuing unanswered: %v", err)
	}
	moved(t, tm, "pick")(tm.Pick("q", "q1", "1"))
	moved(t, tm, "q")(tm.Continue("q"))
	moved(t, tm, "b")(tm.Continue("s"))
	moved(t, tm, "b1")(tm.Answer("b1", "x"))
	if !tm.Completed || tm.Points != 6 {
		t.Fatalf("completed %v points %v, want the quiz's 5 and b1's 1", tm.Completed, tm.Points)
	}
}

// A path that loops — a wrong answer's route back to an earlier step of
// the group's — stays the group's.
func TestLoopOnAPath(t *testing.T) {
	b := newGB().split("s", "a", "b").puzzle("a1", "x").puzzle("b1", "x").join("j").end("end").
		add(graph.Node{Slug: "riddle", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
			Repeatable: true,
			Outputs: []graph.Output{
				{Port: "right", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "r"}}},
				{Port: "again", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "w"}}},
			},
		}}).
		wire("start", "out", "s").wire("s", "a", "riddle").wire("riddle", "again", "a1").wire("a1", "solved", "riddle").
		wire("riddle", "right", "j").wire("s", "b", "b1").wire("b1", "solved", "j").wire("j", "out", "end")
	tm := begin(t, b.valid(t))
	moved(t, tm, "a")(tm.Continue("s"))
	moved(t, tm, "w")(tm.Answer("riddle", "w"))
	moved(t, tm, "a1")(tm.Answer("a1", "x"))
	if tm.Strands[0].Current != "riddle" {
		t.Fatalf("back at the riddle: %+v", tm.Strands)
	}
	moved(t, tm, "r")(tm.Answer("riddle", "r"))
	moved(t, tm, "b")(tm.Continue("s"))
	moved(t, tm, "b1")(tm.Answer("b1", "x"))
	if !tm.Completed {
		t.Fatal("finish")
	}
}

// The adventure's countdown runs while split, and every phone shows it.
func TestCountdownWhileSplit(t *testing.T) {
	g := expedition()
	g.TimeLimit = &graph.TimeLimit{Seconds: 600, To: "door"}
	tm, _, _ := Start(g, t0)
	moved(t, tm, "a")(tm.Continue("split"))
	if v := tm.View(); v.EndsAt == nil || !v.EndsAt.Equal(t0.Add(10*time.Minute)) {
		t.Fatalf("ends at %v", v.EndsAt)
	}
}

// A three-way split, the groups arriving in every order.
func TestThreeWaysAnyOrder(t *testing.T) {
	for _, order := range [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		g := newGB().split("s", "a", "b", "c").puzzle("a1", "x").puzzle("b1", "x").puzzle("c1", "x").join("j").end("end").
			wire("start", "out", "s").wire("s", "a", "a1").wire("s", "b", "b1").wire("s", "c", "c1").
			wire("a1", "solved", "j").wire("b1", "solved", "j").wire("c1", "solved", "j").wire("j", "out", "end").valid(t)
		tm := begin(t, g)
		steps := []string{"a1", "b1", "c1"}
		for _, i := range order {
			tm.Strands[i].Current = "s" // set off in the order given
		}
		for k, i := range order {
			tm.Unfocus()
			tm.focusOn(i)
			moved(t, tm, "set off")(tm.Continue("s"))
			moved(t, tm, steps[i])(tm.Answer(steps[i], "x"))
			if done := k == 2; tm.Completed != done {
				t.Fatalf("order %v after %d groups: completed %v", order, k+1, tm.Completed)
			}
		}
	}
}

// A step's deadline on a path counts from when its group got there.
func TestGroupDeadline(t *testing.T) {
	b := newGB().split("s", "a", "b").puzzle("a1", "x").puzzle("b1", "x").join("j").end("end").
		wire("start", "out", "s").wire("s", "a", "a1").wire("s", "b", "b1").
		wire("a1", "solved", "j").wire("b1", "solved", "j").wire("j", "out", "end")
	b.g.Nodes[3].PuzzleCfg.TimeLimitSeconds = 60 // b1
	tm := begin(t, b.valid(t))
	moved(t, tm, "a")(tm.Continue("s"))
	tm.Now = t0.Add(5 * time.Minute)
	moved(t, tm, "a1")(tm.Answer("a1", "x"))
	tm.Now = t0.Add(10 * time.Minute)
	moved(t, tm, "b")(tm.Continue("s"))
	if v := tm.View(); v.Node != "b1" || v.Deadline == nil || !v.Deadline.Equal(t0.Add(11*time.Minute)) {
		t.Fatalf("b1's deadline: %+v", v.Deadline)
	}
	tm.Now = t0.Add(10*time.Minute + 30*time.Second)
	moved(t, tm, "b1")(tm.Answer("b1", "x"))
}
