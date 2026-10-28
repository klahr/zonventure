package valid

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

func TestSight(t *testing.T) {
	sight := func(g *graph.Graph) Report { return Report{Issues: Sight(g)} }
	t.Run("a story told in words", func(t *testing.T) {
		g := valid3()
		g.Nodes[1].PuzzleCfg.Blocks = []graph.Block{
			{ID: "b1", Kind: graph.BlockText, Text: "Listen."},
			{ID: "b2", Kind: graph.BlockImage, Media: 1, Alt: "A bronze lion"},
			{ID: "b3", Kind: graph.BlockFlasher, Text: "SOS"},
		}
		if is := Sight(g); len(is) != 0 {
			t.Errorf("got %v", is)
		}
	})
	t.Run("a picture nobody described", func(t *testing.T) {
		g := valid3()
		g.Nodes[1].PuzzleCfg.Blocks = []graph.Block{{ID: "b1", Kind: graph.BlockImage, Media: 1}}
		assertCode(t, sight(g), "sight.alt")
	})
	t.Run("a hint's picture", func(t *testing.T) {
		g := valid3()
		g.Nodes[1].PuzzleCfg.Hints = []graph.Hint{{Text: "Look up", Media: []graph.Media{{ID: 1, Kind: graph.MediaImage}}}}
		assertCode(t, sight(g), "sight.alt")
	})
	t.Run("a block that needs eyes", func(t *testing.T) {
		g := valid3()
		g.Nodes[1].PuzzleCfg.Blocks = []graph.Block{{ID: "b1", Kind: graph.BlockTorch, Media: 1, Alt: "A dark cellar"}}
		assertCode(t, sight(g), "sight.block")
	})
	t.Run("a tile puzzle", func(t *testing.T) {
		g := valid3()
		g.Nodes[1].PuzzleCfg.Answer = &graph.Answer{Kind: graph.AnswerTiles}
		assertCode(t, sight(g), "sight.answer")
	})
	t.Run("a hidden place", func(t *testing.T) {
		g := valid3()
		g.Zones = []graph.Zone{{Slug: "oak", Lat: 57.7, Lng: 11.9, RadiusM: 30, Hidden: true}}
		g.Nodes[1].PuzzleCfg.Zone = "oak"
		assertCode(t, sight(g), "sight.zone.hidden")
	})
	t.Run("not part of Check", func(t *testing.T) {
		g := valid3()
		g.Nodes[1].PuzzleCfg.Blocks = []graph.Block{{ID: "b1", Kind: graph.BlockImage, Media: 1}}
		assertNoCode(t, Check(g), "sight.alt")
	})
}
