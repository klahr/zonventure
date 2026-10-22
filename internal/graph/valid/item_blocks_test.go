package valid

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
)

func TestItemBlocks(t *testing.T) {
	with := func(bs ...graph.Block) *graph.Graph {
		g := valid3()
		g.Items = []graph.Item{{Slug: "map", Name: "Map", Blocks: bs}}
		return g
	}
	t.Run("a letter and a picture", func(t *testing.T) {
		r := Check(with(graph.Block{ID: "b1", Kind: graph.BlockNote, Text: "Dear finder"}, graph.Block{ID: "b2", Kind: graph.BlockImage, Media: 1}))
		if !r.OK() {
			t.Errorf("got %v", r.Errors())
		}
	})
	t.Run("ids", func(t *testing.T) {
		r := Check(with(graph.Block{ID: "b1", Kind: graph.BlockText, Text: "a"}, graph.Block{ID: "b1", Kind: graph.BlockText, Text: "b"}, graph.Block{Kind: graph.BlockText}))
		assertCode(t, r, "item.block.id.duplicate")
		assertCode(t, r, "item.block.id.empty")
	})
	t.Run("a kind an item cannot hold", func(t *testing.T) {
		assertCode(t, Check(with(graph.Block{ID: "b1", Kind: graph.BlockQuiz})), "item.block.kind")
	})
	t.Run("a note style there is not", func(t *testing.T) {
		assertCode(t, Check(with(graph.Block{ID: "b1", Kind: graph.BlockNote, Text: "a", Style: "letter"})), "item.block.style")
	})
	t.Run("a rule for when it shows", func(t *testing.T) {
		b := graph.Block{ID: "b1", Kind: graph.BlockText, Text: "a", Show: &graph.Show{When: cond.Predicate{Op: cond.OpItem, Item: "map"}}}
		assertCode(t, Check(with(b)), "item.block.show")
	})
}
