package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

func stepGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g := &graph.Graph{}
	mustApply(t, g, graphOp{Op: "put_node", Node: json.RawMessage(`{"slug":"fish","type":"puzzle","title":"The fish","puzzle":{
		"blocks":[{"id":"b1","kind":"text","text":"Count the fish on the fountain."},{"id":"b2","kind":"text","text":"Then count the fish again."}],
		"hints":[{"text":"Look down."}]}}`)})
	return g
}

func TestSetText(t *testing.T) {
	t.Parallel()
	g := stepGraph(t)
	mustApply(t, g, graphOp{Op: "set_text", Slug: "fish", Find: "on the fountain", Replace: "in the **pond**"})
	if b := g.Nodes[0].PuzzleCfg.Blocks[0].Text; b != "Count the fish in the **pond**." {
		t.Errorf("text = %q", b)
	}

	// The slug "fish" is a name, not a word: only the texts change.
	err := applyGraphOp(g, graphOp{Op: "set_text", Slug: "fish", Find: "fish", Replace: "frog"}, nil)
	if err == nil || !strings.Contains(err.Error(), "3 times") {
		t.Fatalf("ambiguous find: %v", err)
	}
	mustApply(t, g, graphOp{Op: "set_text", Slug: "fish", Find: "fish", Replace: "frog", All: true})
	if n := g.Nodes[0]; n.Slug != "fish" || n.Title != "The frog" || n.PuzzleCfg.Blocks[1].Text != "Then count the frog again." {
		t.Errorf("all: %+v", n)
	}

	err = applyGraphOp(g, graphOp{Op: "set_text", Slug: "fish", Find: "whale", Replace: "x"}, nil)
	if err == nil || !strings.Contains(err.Error(), "Look down.") {
		t.Errorf("a missing text should list the step's texts: %v", err)
	}
	if applyGraphOp(g, graphOp{Op: "set_text", Slug: "ghost", Find: "a"}, nil) == nil {
		t.Error("a missing step should fail")
	}
}

func TestPatchNode(t *testing.T) {
	t.Parallel()
	g := stepGraph(t)
	mustApply(t, g, graphOp{Op: "patch_node", Slug: "fish", Patch: json.RawMessage(`{"title":"Fishy","puzzle":{
		"blocks":[{"id":"b2","text":"Now the ducks."},{"id":"b1","_delete":true},{"id":"b3","kind":"text","text":"New."}],
		"hints":[{"text":"Look up."}]}}`)})
	p := g.Nodes[0].PuzzleCfg
	if g.Nodes[0].Title != "Fishy" || len(p.Blocks) != 2 || p.Blocks[0].ID != "b2" || p.Blocks[0].Text != "Now the ducks." || p.Blocks[0].Kind != "text" || p.Blocks[1].ID != "b3" {
		t.Errorf("blocks merged by id: %+v", p.Blocks)
	}
	if len(p.Hints) != 1 || p.Hints[0].Text != "Look up." {
		t.Errorf("a list without ids is replaced: %+v", p.Hints)
	}
	// A new accepted answer: the output found by its port, the matcher
	// added by its id, the other outputs kept.
	mustApply(t, g, graphOp{Op: "put_node", Node: json.RawMessage(`{"slug":"q","type":"puzzle","puzzle":{"outputs":[
		{"port":"right","trigger":"answer","matchers":[{"id":"n","kind":"exact","value":"7"}]},
		{"port":"late","trigger":"timeout"}],"time_limit_seconds":60}}`)})
	mustApply(t, g, graphOp{Op: "patch_node", Slug: "q", Patch: json.RawMessage(`{"puzzle":{"outputs":[{"port":"right","matchers":[{"id":"w","kind":"exact","value":"seven"}]}]}}`)})
	out := g.Nodes[1].PuzzleCfg.Outputs
	if len(out) != 2 || out[1].Port != "late" || len(out[0].Matchers) != 2 || out[0].Matchers[1].ID != "w" || out[0].Matchers[0].ID != "n" {
		t.Errorf("outputs merged by port: %+v", out)
	}
	mustApply(t, g, graphOp{Op: "patch_node", Slug: "q", Patch: json.RawMessage(`{"puzzle":{"outputs":[{"port":"late","_delete":true}],"time_limit_seconds":null}}`)})
	if out := g.Nodes[1].PuzzleCfg.Outputs; len(out) != 1 || out[0].Port != "right" {
		t.Errorf("an output removed by port: %+v", out)
	}

	for name, patch := range map[string]string{
		"rename":        `{"slug":"other"}`,
		"unknown field": `{"sparkle":1}`,
		"not an object": `[1]`,
	} {
		if applyGraphOp(g, graphOp{Op: "patch_node", Slug: "fish", Patch: json.RawMessage(patch)}, nil) == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
