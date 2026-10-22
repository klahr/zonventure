package graph

import (
	"slices"
	"testing"
)

func translatedVoicemail() *Graph {
	return &Graph{
		Language: "en",
		Items:    []Item{{Slug: "tape", Blocks: []Block{{ID: "b1", Kind: BlockImage, Media: 30}}}},
		Nodes: []Node{
			{Slug: "msg", Type: Story, StoryCfg: &StoryConfig{Blocks: []Block{{ID: "b1", Kind: BlockAudio, Media: 10}}}},
		},
		Translations: map[string]map[string]string{
			"sv": {MediaKey(BlockKey("msg", "b1")): "11", BlockKey("msg", "b1"): "text, not a file"},
			"de": {MediaKey(BlockKey("msg", "b1")): "12", MediaKey(ItemBlockKey("tape", "b1")): "32"},
		},
		TranslationOriginals: map[string]map[string]string{"sv": {MediaKey(BlockKey("msg", "b1")): "10"}},
	}
}

func TestTrFile(t *testing.T) {
	g := translatedVoicemail()
	owner := BlockKey("msg", "b1")
	for _, c := range []struct {
		lang string
		id   int64
		want int64
	}{
		{"en", 10, 10}, {"", 10, 10}, {"sv", 10, 11}, {"de", 10, 12}, {"fr", 10, 10},
		// A block with no file has none to swap.
		{"sv", 0, 0},
	} {
		if got := g.TrFile(c.lang, owner, c.id); got != c.want {
			t.Errorf("TrFile(%q, %d) = %d, want %d", c.lang, c.id, got, c.want)
		}
	}
	g.Translations["sv"][MediaKey(owner)] = "not a number"
	if got := g.TrFile("sv", owner, 10); got != 10 {
		t.Errorf("a garbled file id gives %d, want the original 10", got)
	}
}

func TestBlockFilesListsEveryLanguage(t *testing.T) {
	g := translatedVoicemail()
	var ids []int64
	for _, m := range g.BlockFiles(BlockKey("msg", "b1"), g.Nodes[0].StoryCfg.Blocks[0]) {
		if m.Kind != MediaAudio {
			t.Errorf("file %d is %q, want audio like the original", m.ID, m.Kind)
		}
		ids = append(ids, m.ID)
	}
	if want := []int64{10, 12, 11}; !slices.Equal(ids, want) {
		t.Errorf("files %v, want %v (the original, then by language)", ids, want)
	}
}

func TestMediaIDsHoldItemsAndTranslations(t *testing.T) {
	ids := translatedVoicemail().MediaIDs()
	for _, id := range []int64{10, 11, 12, 30, 32} {
		if !slices.Contains(ids, id) {
			t.Errorf("MediaIDs %v lacks %d", ids, id)
		}
	}
}

func TestRemapTranslatedMedia(t *testing.T) {
	g := translatedVoicemail()
	g.RemapTranslatedMedia(func(id int64) int64 { return id + 100 })
	owner := MediaKey(BlockKey("msg", "b1"))
	if got := g.Translations["sv"][owner]; got != "111" {
		t.Errorf("Swedish file %q, want 111", got)
	}
	if got := g.TranslationOriginals["sv"][owner]; got != "110" {
		t.Errorf("the original it was made from %q, want 110", got)
	}
	if got := g.Translations["sv"][BlockKey("msg", "b1")]; got != "text, not a file" {
		t.Errorf("a text was rewritten to %q", got)
	}
}
