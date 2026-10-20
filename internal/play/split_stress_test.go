package play

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graph/valid"
)

// expeditionLive is start → split{scout, medic, everyone} → each path two
// steps, one done together (a unanimous answer on one, a two-key launch
// on another) → join → a second split in two → join → end.
func expeditionLive() *graph.Graph {
	right := func(port string) []graph.Output {
		return []graph.Output{{Port: port, Trigger: graph.OnAnswer, Points: 1, Matchers: []match.Matcher{{Kind: match.Exact, Value: "x"}}}}
	}
	p := func(slug string, cfg graph.PuzzleConfig) graph.Node {
		if cfg.Outputs == nil {
			cfg.Outputs = right("ok")
		}
		return graph.Node{Slug: slug, Type: graph.Puzzle, Title: slug, PuzzleCfg: &cfg}
	}
	g := &graph.Graph{
		Roles: []graph.Role{{Slug: "scout", Name: "Scout"}, {Slug: "medic", Name: "Medic"}, {Slug: "crew", Name: "Crew", Default: true}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "s1", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "a", Role: "scout"}, {Port: "b", Role: "medic"}, {Port: "c"}}}},
			p("a1", graph.PuzzleConfig{}), p("a2", graph.PuzzleConfig{Unanimous: true}),
			p("b1", graph.PuzzleConfig{}), p("b2", graph.PuzzleConfig{Answer: &graph.Answer{Kind: graph.AnswerKeys, Window: 600}, Outputs: []graph.Output{{Port: "ok", Trigger: graph.OnAnswer, Points: 1}}}),
			p("c1", graph.PuzzleConfig{}), p("c2", graph.PuzzleConfig{}),
			{Slug: "j1", Type: graph.Join},
			{Slug: "s2", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "d"}, {Port: "e"}}}},
			p("d1", graph.PuzzleConfig{}), p("e1", graph.PuzzleConfig{}),
			{Slug: "j2", Type: graph.Join},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
		},
	}
	for _, e := range [][3]string{
		{"start", "out", "s1"},
		{"s1", "a", "a1"}, {"a1", "ok", "a2"}, {"a2", "ok", "j1"},
		{"s1", "b", "b1"}, {"b1", "ok", "b2"}, {"b2", "ok", "j1"},
		{"s1", "c", "c1"}, {"c1", "ok", "c2"}, {"c2", "ok", "j1"},
		{"j1", "out", "s2"},
		{"s2", "d", "d1"}, {"d1", "ok", "j2"}, {"s2", "e", "e1"}, {"e1", "ok", "j2"},
		{"j2", "out", "end"},
	} {
		g.Edges = append(g.Edges, graph.Edge{From: e[0], FromPort: e[1], To: e[2]})
	}
	return g
}

// Many phones of several teams play a double split at once, as fast as
// they can, while players keep joining. Every team must finish, each
// step solved once, and nothing of a split left behind.
func TestSplitStressLive(t *testing.T) {
	t.Parallel()
	g := expeditionLive()
	if r := valid.Check(g); r.HasErrors() {
		t.Fatalf("graph: %v", r.Errors())
	}
	const teams, phones, late = 3, 30, 6
	f := setup(t, g, teams)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var (
		wg      sync.WaitGroup
		actions atomic.Int64
		refused sync.Map // error text → count, for the report
	)
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
			node := v.Node
			switch {
			case v.Type == graph.Split:
				_, err = f.eng.Continue(c, team, node)
			case v.Type == graph.Puzzle && v.Input != nil && v.Input.Kind == "keys":
				_, err = f.eng.Turn(c, team, node)
			case v.Type == graph.Puzzle:
				_, err = f.eng.Answer(c, team, node, "x")
			default:
				// At a join, or a step done together this phone did its
				// part of: give the others a moment.
				time.Sleep(5 * time.Millisecond)
				continue
			}
			actions.Add(1)
			switch {
			case err == nil:
			case errors.Is(err, ErrStale), errors.Is(err, ErrFinished):
				// Someone was quicker: look again. Never "not yours": a
				// phone acts only on what it was shown of its own group.
				n, _ := refused.LoadOrStore(session.Code(err), new(atomic.Int64))
				n.(*atomic.Int64).Add(1)
			default:
				t.Errorf("player %d on %s (%s): %v", slot, node, v.Type, err)
				return
			}
			if v.Together != nil && v.Together.Mine {
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
	for _, team := range f.teams {
		for _, slot := range f.players(team, phones) {
			wg.Add(1)
			go play(team, slot)
		}
	}
	// Newcomers, a few at a time, mid-game.
	for range late {
		time.Sleep(20 * time.Millisecond)
		for _, team := range f.teams {
			var id int64
			mustScan(t, f.db.QueryRow(`INSERT INTO player_slots(team_id, name, token, last_login_at) VALUES ($1, $2, $2, now_text()) RETURNING id`,
				team, fmt.Sprintf("late-%d-%d", team, seq.Add(1))), &id)
			wg.Add(1)
			go play(team, id)
		}
	}
	wg.Wait()
	if ctx.Err() != nil {
		for _, team := range f.teams {
			split, strands, current := f.strands(team)
			t.Logf("team %d: node %q split %v strands %+v", team, current, split, strands)
		}
		t.Fatal("the teams did not finish in time: stuck")
	}
	refused.Range(func(k, v any) bool {
		t.Logf("refused %s: %d", k, v.(*atomic.Int64).Load())
		return true
	})
	t.Logf("%d actions by %d phones", actions.Load(), teams*(phones+late))

	for _, team := range f.teams {
		split, strands, current := f.strands(team)
		if split.Valid || strands != nil || current != "end" {
			t.Errorf("team %d: node %q, split %v, strands %+v", team, current, split, strands)
		}
		var solved, presses int
		mustScan(t, f.db.QueryRow(`SELECT COUNT(*) FROM team_node_state WHERE team_id = $1 AND solved_at IS NOT NULL AND node <> 'start'`, team), &solved)
		mustScan(t, f.db.QueryRow(`SELECT COUNT(*) FROM team_presses WHERE team_id = $1`, team), &presses)
		if presses != 0 {
			t.Errorf("team %d: %d presses left behind", team, presses)
		}
		// Eight puzzles, each solved once; the score is one a puzzle.
		if v := f.view(team); !v.Finished || v.Score != 8 {
			t.Errorf("team %d: finished %v, score %v, want 8", team, v.Finished, v.Score)
		}
		if solved < 8 {
			t.Errorf("team %d: %d steps solved, want the 8 puzzles", team, solved)
		}
	}
}
