package graph

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph/match"
)

func hostGraph() *Graph {
	return &Graph{
		HostValues: []HostValue{{Slug: "name", Label: "The birthday child's name", Default: "the hero"}},
		Zones: []Zone{
			{Slug: "tree", Name: "Big tree", Lat: 59.3, Lng: 18.0, RadiusM: 10, Movable: true},
			{Slug: "church", Lat: 59.4, Lng: 18.1, RadiusM: 40},
		},
		Nodes: []Node{
			{Slug: "q", Type: Puzzle, Title: "Animals", PuzzleCfg: &PuzzleConfig{
				HostAnswer: true,
				Blocks: []Block{
					{ID: "t", Kind: BlockText, Text: "What is {{host:name}}'s favourite animal?", HostEdit: true},
					{ID: "z", Kind: BlockQuiz, Text: "Capital?", Options: []string{"Oslo", "Stockholm", "Malmö"}, Answer: "X", HostEdit: true},
					{ID: "fixed", Kind: BlockText, Text: "Not yours"},
				},
				Outputs: []Output{{Port: "ok", Trigger: OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "cat"}}}},
			}},
		},
		Translations: map[string]map[string]string{"sv": {
			BlockKey("q", "t"):                  "Vilket är {{host:name}}s favoritdjur?",
			BlockFieldKey("q", "z", "option:0"): "Oslo",
			BlockKey("q", "fixed"):              "Inte ditt",
		}},
	}
}

func TestHostEditables(t *testing.T) {
	es := hostGraph().HostEditables()
	if len(es) != 3 {
		t.Fatalf("editables = %+v, want the text, the quiz and the answer", es)
	}
	if es[0].Key != "q/t" || es[1].Key != "q/z" || es[1].Right != "X" || es[2].Key != "q" || len(es[2].Accepts) != 1 || es[2].Accepts[0] != "cat" {
		t.Errorf("editables = %+v", es)
	}
}

func TestPartyOverridesCheck(t *testing.T) {
	g := hostGraph()
	ok := PartyOverrides{
		Values:  map[string]string{"name": "Elsa"},
		Blocks:  map[string]BlockOverride{"q/t": {Text: "Elsa's pet?"}, "q/z": {Text: "Pet?", Options: []string{"Dog", "Cat", "Fish"}, Answer: "2"}},
		Answers: map[string][]string{"q": {"horse", "pony"}},
		Zones:   map[string]ZonePlace{"tree": {Lat: 57.7, Lng: 11.9}},
	}
	if err := ok.Check(g); err != nil {
		t.Fatalf("valid overrides refused: %v", err)
	}
	for name, o := range map[string]PartyOverrides{
		"unknown value": {Values: map[string]string{"age": "8"}},
		"fixed block":   {Blocks: map[string]BlockOverride{"q/fixed": {Text: "x"}}},
		"two options":   {Blocks: map[string]BlockOverride{"q/z": {Options: []string{"a", "b"}, Answer: "1"}}},
		"bad answer":    {Blocks: map[string]BlockOverride{"q/z": {Options: []string{"a", "b", "c"}, Answer: "3"}}},
		"empty answers": {Answers: map[string][]string{"q": {" "}}},
		"fixed place":   {Zones: map[string]ZonePlace{"church": {Lat: 1, Lng: 1}}},
		"off the map":   {Zones: map[string]ZonePlace{"tree": {Lat: 91, Lng: 1}}},
	} {
		if err := o.Check(g); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestWithOverrides(t *testing.T) {
	g := hostGraph()
	c, err := g.WithOverrides(PartyOverrides{
		Values:  map[string]string{"name": "Elsa"},
		Blocks:  map[string]BlockOverride{"q/t": {Text: "Elsa's pet?"}, "q/z": {Text: "Pet?", Options: []string{"Dog", "Cat", "Fish"}, Answer: "2"}},
		Answers: map[string][]string{"q": {"horse", " pony "}},
		Zones:   map[string]ZonePlace{"tree": {Lat: 57.7, Lng: 11.9}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := c.HostValue("name"); v != "Elsa" {
		t.Errorf("host value = %q", v)
	}
	bs := c.Nodes[0].PuzzleCfg.Blocks
	if bs[0].Text != "Elsa's pet?" || bs[1].Text != "Pet?" || bs[1].Options[1] != "Cat" || bs[1].Answer != "2" || bs[2].Text != "Not yours" {
		t.Errorf("blocks = %+v", bs)
	}
	ms := c.Nodes[0].PuzzleCfg.Outputs[0].Matchers
	if len(ms) != 2 || ms[0].Value != "horse" || ms[1].Value != "pony" {
		t.Errorf("matchers = %+v", ms)
	}
	sv := c.Translations["sv"]
	if _, ok := sv[BlockKey("q", "t")]; ok {
		t.Error("the author's translation of a rewritten text outlived it")
	}
	if _, ok := sv[BlockFieldKey("q", "z", "option:0")]; ok {
		t.Error("the author's translated option outlived the host's")
	}
	if sv[BlockKey("q", "fixed")] == "" {
		t.Error("an untouched block lost its translation")
	}
	tree, church := c.Zones[0], c.Zones[1]
	if tree.Lat != 57.7 || tree.Lng != 11.9 || tree.RadiusM != MinPartyZoneRadius {
		t.Errorf("moved zone = %+v", tree)
	}
	if church.Lat != 59.4 {
		t.Errorf("fixed zone moved: %+v", church)
	}
	// The original is untouched.
	if g.Nodes[0].PuzzleCfg.Blocks[0].Text == "Elsa's pet?" || g.Zones[0].Lat != 59.3 || g.Translations["sv"][BlockKey("q", "t")] == "" {
		t.Error("WithOverrides changed the graph it copied")
	}
}
