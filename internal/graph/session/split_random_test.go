package session

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/valid"
	"github.com/klahr/zonventure/internal/graph/walk"
)

// Random play of random splits: many graphs, many teams, players acting
// in random order, joining late and leaving, the simulator's JSON round
// trip in between — and after every action, the invariants a split team
// must keep. Every team must finish, or the engine has stranded it.

// randomSplitGraph is start → [split → paths → join]×1–2 → end, each
// path a chain of 0–3 puzzles answered "x", maybe a role's, now and then
// and now and then a path whose last puzzle can also end the adventure.
func randomSplitGraph(r *rand.Rand) *graph.Graph {
	g := &graph.Graph{
		Roles: []graph.Role{{Slug: "r1", Name: "R1"}, {Slug: "r2", Name: "R2"}, {Slug: "crew", Name: "Crew", Default: true}},
		Nodes: []graph.Node{{Slug: "start", Type: graph.Start}, {Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}}},
	}
	wire := func(from, port, to string) {
		g.Edges = append(g.Edges, graph.Edge{From: from, FromPort: port, To: to})
	}
	puzzle := func(slug string, outs ...graph.Output) {
		g.Nodes = append(g.Nodes, graph.Node{Slug: slug, Type: graph.Puzzle, Title: slug, PuzzleCfg: &graph.PuzzleConfig{Outputs: outs}})
	}
	right := graph.Output{Port: "ok", Trigger: graph.OnAnswer, Points: 1, Matchers: []match.Matcher{{Kind: match.Exact, Value: "x"}}}
	// block wires a split from (at, out), its paths and its join, and
	// returns the join's way on.
	block := func(name string, at, out string) (string, string) {
		sp, jn := "s"+name, "j"+name
		cfg := &graph.SplitConfig{}
		roles := []string{"r1", "r2", "crew", "", ""}
		r.Shuffle(len(roles), func(i, j int) { roles[i], roles[j] = roles[j], roles[i] })
		used := map[string]bool{}
		for p := range 2 + r.IntN(3) {
			role := roles[p%len(roles)]
			if used[role] {
				role = ""
			}
			used[role] = role != ""
			cfg.Paths = append(cfg.Paths, graph.SplitPath{Port: fmt.Sprintf("p%d", p), Role: role})
		}
		g.Nodes = append(g.Nodes, graph.Node{Slug: sp, Type: graph.Split, Title: sp, SplitCfg: cfg}, graph.Node{Slug: jn, Type: graph.Join, Title: jn})
		wire(at, out, sp)
		for _, path := range cfg.Paths {
			a, o := sp, path.Port
			for n := range r.IntN(4) {
				slug := fmt.Sprintf("%s-%s-%d", sp, path.Port, n)
				if r.IntN(12) == 0 {
					// A way out that ends the adventure for everyone.
					fail := graph.Output{Port: "fall", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "fall"}}}
					puzzle(slug, right, fail)
					wire(slug, "fall", "end")
				} else {
					puzzle(slug, right)
				}
				wire(a, o, slug)
				a, o = slug, "ok"
			}
			wire(a, o, jn)
		}
		return jn, "out"
	}
	prev, port := "start", "out"
	for si := range 1 + r.IntN(2) {
		if r.IntN(2) == 0 {
			slug := fmt.Sprintf("pre%d", si)
			puzzle(slug, right)
			wire(prev, port, slug)
			prev, port = slug, "ok"
		}
		prev, port = block(fmt.Sprint(si), prev, port)
	}
	wire(prev, port, "end")
	return g
}

// splitInvariants are what a split team keeps between actions: its
// groups cover the split's paths once each, and each group stands on
// its own path, the split or the join.
func splitInvariants(tm *Team) error {
	tm.unfocus()
	if tm.Split == "" {
		if tm.Strands != nil {
			return fmt.Errorf("strands %+v without a split", tm.Strands)
		}
		return nil
	}
	if tm.Current != tm.Split || tm.Whole() != tm.Split {
		return fmt.Errorf("unfocused, the team is at %q / %q, not its split %q", tm.Current, tm.Whole(), tm.Split)
	}
	n, ok := tm.G.Node(tm.Split)
	if !ok || n.Type != graph.Split {
		return fmt.Errorf("split %q is not a split", tm.Split)
	}
	if len(tm.Strands) != len(n.SplitCfg.Paths) {
		return fmt.Errorf("%d groups for the %d paths of %s", len(tm.Strands), len(n.SplitCfg.Paths), n.Slug)
	}
	join := tm.joinOf(tm.Split)
	keys := map[string]bool{}
	for _, st := range tm.Strands {
		if keys[st.Port] {
			return fmt.Errorf("two groups %q", st.Port)
		}
		keys[st.Port] = true
		if _, ok := n.Path(st.Port); !ok {
			return fmt.Errorf("group %s took %q, not a path of %s", st.Port, st.Port, n.Slug)
		}
		if st.Current != tm.Split && st.Current != join && !slices.Contains(tm.Splits().Paths(tm.Split).Steps[st.Port], st.Current) {
			return fmt.Errorf("group %s at %q, off its path", st.Port, st.Current)
		}
	}
	return nil
}

// actor is one phone, or the team as a whole (slot 0).
type actor struct {
	slot  int64
	roles []string
}

func TestSplitRandomPlay(t *testing.T) {
	const runs = 400
	for seed := range uint64(runs) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			r := rand.New(rand.NewPCG(seed, seed*7919))
			g := randomSplitGraph(r)
			if rep := valid.Check(g); rep.HasErrors() {
				t.Fatalf("random graph invalid: %v", rep.Errors())
			}
			tm, _, err := Start(g, t0)
			if err != nil {
				t.Fatal(err)
			}
			n := r.IntN(13) // 0: the team as a whole, as the simulator plays
			var players []int64
			for i := 1; i <= n; i++ {
				players = append(players, int64(i))
			}
			next := int64(n + 1)
			seat := func() {
				tm.unfocus()
				if len(players) == 0 {
					tm.Players, tm.RoleHolders, tm.Viewer = nil, nil, nil
					return
				}
				tm.Players, tm.Size = slices.Clone(players), len(players)
				tm.RoleHolders = map[string]int64{}
				for i, role := range []string{"r1", "r2"} {
					tm.RoleHolders[role] = players[i%len(players)]
				}
				tm.Deal(tm.Players, tm.RoleHolders)
			}
			viewerOf := func(slot int64) *Viewer {
				if slot == 0 {
					return nil
				}
				v := &Viewer{Slot: slot}
				for role, h := range tm.RoleHolders {
					if h == slot {
						v.Roles = append(v.Roles, role)
					}
				}
				if len(v.Roles) == 0 || len(players) == 1 {
					v.Roles = append(v.Roles, "crew")
				}
				return v
			}
			seat()
			for round := range 2000 {
				if tm.Completed {
					return
				}
				// Now and then the world changes under the team.
				switch r.IntN(40) {
				case 0:
					if len(players) > 0 {
						players = slices.Delete(players, 0, 1) // the first leaves
					}
				case 1:
					players = append(players, next) // a newcomer
					next++
				case 2:
					// The simulator's round trip through the browser.
					raw, err := json.Marshal(tm)
					if err != nil {
						t.Fatal(err)
					}
					var back Team
					if err := json.Unmarshal(raw, &back); err != nil {
						t.Fatal(err)
					}
					back.G = g
					back.Init()
					tm = &back
				}
				seat()
				if err := splitInvariants(tm); err != nil {
					t.Fatalf("round %d: %v", round, err)
				}

				// Someone acts on what their phone shows; if no one can,
				// the team is stuck.
				var who []int64
				if len(players) == 0 {
					who = []int64{0}
				} else {
					who = slices.Clone(players)
					r.Shuffle(len(who), func(i, j int) { who[i], who[j] = who[j], who[i] })
				}
				acted := false
				for _, slot := range who {
					tm.unfocus()
					tm.Viewer = viewerOf(slot)
					v := tm.View()
					var res Result
					var stepErr error
					switch v.Type {
					case graph.Split:
						var st = moveOf(tm.Continue(v.Node))
						res, stepErr = st.res, st.err
						if st.err == nil && st.step != nil {
							tm.Apply(*st.step)
						}
					case graph.Puzzle:
						guess := "x"
						if r.IntN(8) == 0 {
							guess = "wrong"
						}
						if r.IntN(40) == 0 {
							guess = "fall"
						}
						st := moveOf(tm.Answer(v.Node, guess))
						res, stepErr = st.res, st.err
						if st.err == nil && st.step != nil {
							tm.Apply(*st.step)
						}
					case graph.Join:
						continue // waits for the others
					default:
						if v.Finished {
							return
						}
						t.Fatalf("round %d: player %d sees a %q step %q", round, slot, v.Type, v.Node)
					}
					_ = res
					if stepErr != nil {
						t.Fatalf("round %d: player %d on %q: %v", round, slot, v.Node, stepErr)
					}
					acted = true
					break
				}
				if !acted {
					tm.unfocus()
					t.Fatalf("round %d: no one can act — every phone waits at a join. split %q strands %+v players %v", round, tm.Split, tm.Strands, players)
				}
				if tm.Viewer != nil && !slices.Contains(tm.Viewer.Roles, "crew") && len(tm.Viewer.Roles) == 0 {
					t.Fatalf("round %d: viewer lost its roles", round)
				}
			}
			t.Fatalf("the team never finished: split %q strands %+v", tm.Split, tm.Strands)
		})
	}
}

type walkStep = walk.Step

type move struct {
	res  Result
	step *walkStep
	err  error
}

func moveOf(res Result, step *walkStep, err error) move { return move{res, step, err} }
