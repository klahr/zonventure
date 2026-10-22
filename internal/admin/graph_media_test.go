package admin

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

// A copied adventure's items and translated files point at the copies,
// not at the source adventure's rows.
func TestRemapGraphMediaReachesItemsAndTranslations(t *testing.T) {
	owner := graph.MediaKey(graph.BlockKey("msg", "b1"))
	g := &graph.Graph{
		Items:        []graph.Item{{Slug: "tape", Blocks: []graph.Block{{ID: "b1", Kind: graph.BlockImage, Media: 30}}}},
		Nodes:        []graph.Node{{Slug: "msg", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{{ID: "b1", Kind: graph.BlockAudio, Media: 10}}}}},
		Translations: map[string]map[string]string{"sv": {owner: "11"}},
	}
	remapGraphMedia(g, map[int64]int64{10: 110, 11: 111, 30: 130})
	if got := g.Nodes[0].StoryCfg.Blocks[0].Media; got != 110 {
		t.Errorf("step recording %d, want 110", got)
	}
	if got := g.Items[0].Blocks[0].Media; got != 130 {
		t.Errorf("item picture %d, want 130", got)
	}
	if got := g.Translations["sv"][owner]; got != "111" {
		t.Errorf("Swedish recording %q, want 111", got)
	}
}
