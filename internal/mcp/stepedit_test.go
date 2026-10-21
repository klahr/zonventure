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

// A port and a role are names, like a slug: set_text changes the texts
// around them and never them, so a wire never comes loose. An output's
// port "left" survives replacing "left" in the step's words.
func TestSetTextLeavesPortsAndRoles(t *testing.T) {
	t.Parallel()
	g := &graph.Graph{}
	mustApply(t, g, graphOp{Op: "put_node", Node: json.RawMessage(`{"slug":"door","type":"puzzle","puzzle":{
		"by":"left",
		"blocks":[{"id":"b1","kind":"text","text":"Turn left or right?"}],
		"outputs":[{"port":"left","label":"Go left","trigger":"answer","matchers":[{"id":"m1","kind":"exact","value":"left"}]}]}}`)})
	mustApply(t, g, graphOp{Op: "set_text", Slug: "door", Find: "left", Replace: "west", All: true})
	p := g.Nodes[0].PuzzleCfg
	if p.Outputs[0].Port != "left" || p.By != "left" {
		t.Errorf("port %q, role %q: names must not change", p.Outputs[0].Port, p.By)
	}
	if p.Blocks[0].Text != "Turn west or right?" || p.Outputs[0].Label != "Go west" || p.Outputs[0].Matchers[0].Value != "west" {
		t.Errorf("the texts should change: %+v", p)
	}
}

// A condition, a block's settings and its references are names too:
// renaming "tower" and "left" in the words leaves the step, port, zones
// and block a condition reads, a sabotage's role and note, a status's
// counter and the style and animation alone, while a set answer's
// members, which are words, change.
func TestSetTextLeavesReferences(t *testing.T) {
	t.Parallel()
	g := &graph.Graph{}
	mustApply(t, g, graphOp{Op: "put_node", Node: json.RawMessage(`{"slug":"tower","type":"puzzle","puzzle":{
		"blocks":[
			{"id":"b1","kind":"text","text":"Climb the tower, check the left gate","style":"tower",
				"show":{"when":{"op":"any","of":[
					{"op":"outcome","node":"tower","is":"left"},
					{"op":"zones_held","members":["tower","left"]},
					{"op":"drawn","node":"tower","block":"left","is":"1","cmp":"left"}]}},
				"animation":{"entrance":"left","attention":"tower","repeat":"left"}},
			{"id":"b2","kind":"sabotage","text":"Jam the left gate","role":"left","journal":"tower"},
			{"id":"b3","kind":"status","counters":["tower","left"]},
			{"id":"b4","kind":"cipher","cipher":"left"}],
		"outputs":[{"port":"left","trigger":"answer","matchers":[{"id":"m1","kind":"set","members":["tower","left"]}]}]}}`)})
	mustApply(t, g, graphOp{Op: "put_node", Node: json.RawMessage(`{"slug":"plaza","type":"hub","hub":{"policy":"any","text":"Up the tower",
		"branches":[{"port":"left","label":"The left gate","condition":{"op":"outcome","node":"tower","is":"left"}}]}}`)})
	before, _ := json.Marshal(g.Nodes)
	for _, slug := range []string{"tower", "plaza"} {
		mustApply(t, g, graphOp{Op: "set_text", Slug: slug, Find: "tower", Replace: "spire", All: true})
		mustApply(t, g, graphOp{Op: "set_text", Slug: slug, Find: "left", Replace: "west", All: true})
	}
	p := g.Nodes[0].PuzzleCfg
	b := p.Blocks
	if b[0].Text != "Climb the spire, check the west gate" || b[1].Text != "Jam the west gate" {
		t.Errorf("the texts should change: %q, %q", b[0].Text, b[1].Text)
	}
	if m := p.Outputs[0].Matchers[0].Members; m[0] != "spire" || m[1] != "west" {
		t.Errorf("a set answer's members are words: %v", m)
	}
	if h := g.Nodes[1].HubCfg; h.Text != "Up the spire" || h.Branches[0].Label != "The west gate" {
		t.Errorf("the hub's texts should change: %+v", h)
	}

	// Put the words back: everything else must be as it was.
	p.Blocks[0].Text, p.Blocks[1].Text = "Climb the tower, check the left gate", "Jam the left gate"
	p.Outputs[0].Matchers[0].Members = []string{"tower", "left"}
	g.Nodes[1].HubCfg.Text, g.Nodes[1].HubCfg.Branches[0].Label = "Up the tower", "The left gate"
	if after, _ := json.Marshal(g.Nodes); string(after) != string(before) {
		t.Errorf("references changed:\n%s\n%s", before, after)
	}
}
