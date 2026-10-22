package graph

import (
	"slices"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/graph/cond"
)

// A party copy plays zones as cards: no map blocks, the flag set, and
// the authored graph left untouched.
func TestAsStations(t *testing.T) {
	g := &Graph{
		Zones: []Zone{{Slug: "oak", Lat: 1, Lng: 2, RadiusM: 30}},
		Nodes: []Node{
			{Slug: "q", Type: Puzzle, PuzzleCfg: &PuzzleConfig{Zone: "oak", Blocks: []Block{
				{ID: "t", Kind: BlockText, Text: "Go"}, {ID: "m", Kind: BlockMap, Zone: "oak"},
				{ID: "c", Kind: BlockCompass, Zone: "oak"}, {ID: "r", Kind: BlockRadar, Zone: "oak"},
			}}},
		},
	}
	c, err := g.AsStations()
	if err != nil {
		t.Fatal(err)
	}
	if !c.Stations || g.Stations {
		t.Errorf("Stations: copy %v, original %v", c.Stations, g.Stations)
	}
	if got := c.Nodes[0].PuzzleCfg.Blocks; len(got) != 1 || got[0].ID != "t" {
		t.Errorf("copy's blocks = %+v, want the text only", got)
	}
	if len(g.Nodes[0].PuzzleCfg.Blocks) != 4 {
		t.Error("the original lost its blocks")
	}
	if c.Nodes[0].PuzzleCfg.Zone != "oak" {
		t.Error("the step's zone gate must stay: the card opens it")
	}
}

func TestStationsProblems(t *testing.T) {
	ok := &Graph{Nodes: []Node{{Slug: "q", Type: Puzzle, PuzzleCfg: &PuzzleConfig{Zone: "oak"}}}}
	if p := ok.StationsProblems(); len(p) != 0 {
		t.Errorf("zone gate: %v", p)
	}
	for name, g := range map[string]*Graph{
		"search": {Nodes: []Node{{Slug: "s", Type: Puzzle, PuzzleCfg: &PuzzleConfig{Answer: &Answer{Kind: AnswerSearch}}}}},
		"posts":  {Nodes: []Node{{Slug: "k", Type: Puzzle, PuzzleCfg: &PuzzleConfig{Answer: &Answer{Kind: AnswerKeys, Posts: []string{"a", "b"}}}}}},
		"walked": {Nodes: []Node{{Slug: "g", Type: Gate, GateCfg: &GateConfig{Condition: cond.Predicate{Op: cond.OpWalked, N: 100}}}}},
		"split":  {Nodes: []Node{{Slug: "s", Type: Split, SplitCfg: &SplitConfig{Paths: []SplitPath{{Port: "a"}, {Port: "b"}}}}}},
	} {
		p := g.StationsProblems()
		if len(p) == 0 {
			t.Errorf("%s: no problem found", name)
			continue
		}
		if name == "walked" && !slices.ContainsFunc(p, func(s string) bool { return strings.Contains(s, "walked") }) {
			t.Errorf("%s: %v", name, p)
		}
	}
}
