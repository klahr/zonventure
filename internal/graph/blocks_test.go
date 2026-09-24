package graph

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/klahr/zonventure/internal/graph/match"
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
		{ID: "title", Kind: BlockTitle},
		{ID: "points", Kind: BlockPoints},
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
	if c := g.Nodes[0].PuzzleCfg; len(c.Blocks) != 3 || c.Blocks[2].Text != "New." || c.LegacyPrompt != "" {
		t.Errorf("blocks = %+v", c)
	}
}

// Every puzzle has its title and points and every story its title: a
// missing one is added after the one before it, the others stay where the
// author put them, and a second pass changes nothing.
func TestNormalizeAddsFixedBlocks(t *testing.T) {
	g := Graph{Nodes: []Node{
		{Slug: "q", Type: Puzzle, PuzzleCfg: &PuzzleConfig{Blocks: []Block{
			{ID: "b1", Kind: BlockText, Text: "Look."},
			{ID: "title", Kind: BlockTitle},
			{ID: "points", Kind: BlockImage, Media: 1},
		}}},
		{Slug: "s", Type: Story, StoryCfg: &StoryConfig{}},
		{Slug: "e", Type: End, EndCfg: &EndConfig{}},
	}}
	g.Normalize()
	ids := func(bs []Block) (out []string) {
		for _, b := range bs {
			out = append(out, b.ID+":"+string(b.Kind))
		}
		return out
	}
	if got := ids(g.Nodes[0].PuzzleCfg.Blocks); !reflect.DeepEqual(got, []string{"b1:text", "title:title", "points-:points", "points:image"}) {
		t.Errorf("puzzle = %v", got)
	}
	if got := ids(g.Nodes[1].StoryCfg.Blocks); !reflect.DeepEqual(got, []string{"title:title"}) {
		t.Errorf("story = %v", got)
	}
	if g.Nodes[2].EndCfg.Blocks != nil {
		t.Errorf("an end has no fixed blocks: %+v", g.Nodes[2].EndCfg.Blocks)
	}
	before := ids(g.Nodes[0].PuzzleCfg.Blocks)
	g.Normalize()
	if got := ids(g.Nodes[0].PuzzleCfg.Blocks); !reflect.DeepEqual(got, before) {
		t.Errorf("a second pass moved blocks: %v", got)
	}
}

// The web side's sample of every kind parses strictly, so every field it
// sends is one this struct knows. valid.TestSharedBlockVectors checks the
// same sample validates clean.
func TestBlocksMatchSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("../../web/packages/graph/testdata/blocks.json")
	if err != nil {
		t.Fatalf("reading shared vectors: %v", err)
	}
	var vectors struct {
		Comment string            `json:"comment"`
		Blocks  []json.RawMessage `json:"blocks"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	seen := map[BlockKind]bool{}
	var blocks []Block
	for _, r := range vectors.Blocks {
		var b Block
		dec := json.NewDecoder(bytes.NewReader(r))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&b); err != nil {
			t.Errorf("%s: %v", r, err)
			continue
		}
		seen[b.Kind] = true
		blocks = append(blocks, b)
	}
	for _, k := range BlockKinds {
		if !seen[k] {
			t.Errorf("kind %q has no shared vector", k)
		}
	}
	if len(seen) != len(BlockKinds) {
		t.Errorf("vectors have %d kinds, BlockKinds %d", len(seen), len(BlockKinds))
	}

	env, err := json.Marshal(Envelope{Format: FormatV2, Graph: Graph{
		Nodes: []Node{
			{Slug: "start", Type: Start},
			{Slug: "s", Type: Puzzle, PuzzleCfg: &PuzzleConfig{Blocks: blocks, Outputs: []Output{{Port: "solved", Trigger: OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "x"}}}}}},
			{Slug: "end", Type: End},
		},
		Edges:    []Edge{{From: "start", FromPort: "out", To: "s"}, {From: "s", FromPort: "out", To: "end"}},
		Zones:    []Zone{{Slug: "park", Lat: 59.3, Lng: 18.0, RadiusM: 50}},
		Counters: []Counter{{Slug: "coins"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseEnvelope(env); err != nil {
		t.Fatalf("a graph of every kind does not parse: %v", err)
	}
}
