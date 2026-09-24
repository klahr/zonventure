package graph

import (
	"reflect"
	"testing"
)

// A graph saved before blocks still parses, strictly, and comes out as
// blocks: media first, as it was shown, then the text, which keeps its
// translation.
func TestNormalizeLegacyContent(t *testing.T) {
	body := `{"format":"` + FormatV2 + `","graph":{"nodes":[
		{"slug":"q","type":"puzzle","puzzle":{"prompt":"Which door?","media":[{"id":7,"kind":"image","alt":"a door"},{"id":8,"kind":"audio"}]}},
		{"slug":"s","type":"story","story":{"text":"Walk on.","once":true}},
		{"slug":"e","type":"end","end":{"outcome":"complete","media":[{"id":9,"kind":"image"}]}},
		{"slug":"h","type":"hub","hub":{"policy":"choice","text":"Choose."}}
	],"edges":[],"translations":{"en":{"n:q:text":"Which door, in English?","n:h:hub-text":"Choose, in English."}}}}`
	env, err := ParseEnvelope([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	g := env.Graph
	q, _ := g.Node("q")
	want := []Block{
		{ID: "media-1", Kind: BlockImage, Media: 7, Alt: "a door"},
		{ID: "media-2", Kind: BlockAudio, Media: 8},
		{ID: "text", Kind: BlockText, Text: "Which door?"},
	}
	if !reflect.DeepEqual(q.PuzzleCfg.Blocks, want) || q.PuzzleCfg.LegacyPrompt != "" || q.PuzzleCfg.LegacyMedia != nil {
		t.Errorf("puzzle blocks = %+v", q.PuzzleCfg)
	}
	if s, _ := g.Node("s"); TextOf(s.StoryCfg.Blocks) != "Walk on." || !s.StoryCfg.Once {
		t.Errorf("story = %+v", s.StoryCfg)
	}
	if e, _ := g.Node("e"); len(e.EndCfg.Blocks) != 1 || e.EndCfg.Blocks[0].Media != 9 {
		t.Errorf("end = %+v", e.EndCfg)
	}
	tr := g.Translations["en"]
	if tr[BlockKey("q", "text")] != "Which door, in English?" || tr[NodeKey("q", "text")] != "" || tr[NodeKey("h", "hub-text")] == "" {
		t.Errorf("translations = %+v", tr)
	}

	// Written back, only blocks remain.
	out, err := MarshalEnvelope(&g)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ParseEnvelope(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Graph.Nodes, g.Nodes) {
		t.Errorf("round trip changed the nodes:\n%s", out)
	}
}

// Content already in blocks is left alone, even beside an old field.
func TestNormalizeKeepsBlocks(t *testing.T) {
	g := Graph{Nodes: []Node{{Slug: "q", Type: Puzzle, PuzzleCfg: &PuzzleConfig{
		Blocks:       []Block{{ID: "b1", Kind: BlockText, Text: "New."}},
		LegacyPrompt: "Old.",
	}}}}
	g.Normalize()
	if c := g.Nodes[0].PuzzleCfg; len(c.Blocks) != 1 || c.Blocks[0].Text != "New." || c.LegacyPrompt != "" {
		t.Errorf("blocks = %+v", c)
	}
}
