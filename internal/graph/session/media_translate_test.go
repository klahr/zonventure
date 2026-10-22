package session

import (
	"slices"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/valid"
)

// voicemail: a step with a recording in English and Swedish, a picture
// shown only to a team carrying the tape, and an item whose film is
// Swedish in Swedish.
func voicemail() *graph.Graph {
	return &graph.Graph{
		Language: "en",
		Items: []graph.Item{{Slug: "tape", Name: "Tape", Blocks: []graph.Block{
			{ID: "b1", Kind: graph.BlockVideo, Media: 30},
		}}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "msg", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: []graph.Block{
				{ID: "b1", Kind: graph.BlockAudio, Media: 10, Alt: "Hello?"},
				{ID: "b2", Kind: graph.BlockImage, Media: 20, Show: &graph.Show{When: cond.Predicate{Op: cond.OpItem, Item: "tape"}}},
			}}},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "msg"},
			{From: "msg", FromPort: "out", To: "end"},
		},
		Translations: map[string]map[string]string{"sv": {
			graph.MediaKey(graph.BlockKey("msg", "b1")):      "11",
			graph.MediaKey(graph.BlockKey("msg", "b2")):      "21",
			graph.MediaKey(graph.ItemBlockKey("tape", "b1")): "31",
		}},
	}
}

func atVoicemail(t *testing.T, lang string) *Team {
	t.Helper()
	g := voicemail()
	if r := valid.Check(g); r.HasErrors() {
		t.Fatalf("valid: %v", r.Errors())
	}
	tm, _, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Lang = lang
	return tm
}

func blockMedia(v View, id string) int64 {
	for _, b := range v.Blocks {
		if b.ID == id {
			return b.Media
		}
	}
	return -1
}

func TestATranslationPlaysItsOwnFile(t *testing.T) {
	if got := blockMedia(atVoicemail(t, "en").View(), "b1"); got != 10 {
		t.Errorf("in English the recording is %d, want 10", got)
	}
	if got := blockMedia(atVoicemail(t, "sv").View(), "b1"); got != 11 {
		t.Errorf("in Swedish the recording is %d, want 11", got)
	}
	// A language with no file of its own plays the original.
	tm := atVoicemail(t, "sv")
	delete(tm.G.Translations["sv"], graph.MediaKey(graph.BlockKey("msg", "b1")))
	if got := blockMedia(tm.View(), "b1"); got != 10 {
		t.Errorf("with no Swedish file the recording is %d, want the original 10", got)
	}
}

func TestAHiddenBlockKeepsItsTranslatedFileBack(t *testing.T) {
	tm := atVoicemail(t, "sv")
	if got := blockMedia(tm.View(), "b2"); got > 0 {
		t.Errorf("the hidden picture shows file %d, want none", got)
	}
	if tm.CanSeeMedia(21, true) || tm.CanSeeMedia(20, true) {
		t.Error("the hidden picture can be fetched in some language before it shows")
	}
	tm.Items = map[string]int{"tape": 1}
	if got := blockMedia(tm.View(), "b2"); got != 21 {
		t.Errorf("once shown the picture is %d, want the Swedish 21", got)
	}
}

func TestATeamMayFetchEveryLanguagesFile(t *testing.T) {
	tm := atVoicemail(t, "en")
	// A phone may switch language mid-step: both recordings are its to fetch.
	for _, id := range []int64{10, 11} {
		if !tm.CanSeeMedia(id, true) {
			t.Errorf("the team may not fetch recording %d", id)
		}
	}
	if tm.CanSeeMedia(31, false) {
		t.Error("the tape's Swedish film can be fetched before the team has the tape")
	}
	tm.Items = map[string]int{"tape": 1}
	if !tm.CanSeeMedia(31, false) {
		t.Error("the tape's Swedish film can't be fetched with the tape in the bag")
	}
	// A film, in any language, is downloaded only when played.
	if pre := tm.PrefetchMedia(false); slices.Contains(pre, 30) || slices.Contains(pre, 31) {
		t.Errorf("prefetch %v holds a film", pre)
	}
}

func TestAnItemShowsItsTranslatedFile(t *testing.T) {
	tm := atVoicemail(t, "sv")
	tm.Items = map[string]int{"tape": 1}
	v := tm.View()
	for _, it := range v.Items {
		if it.Slug == "tape" {
			if got := it.Blocks[0].Media; got != 31 {
				t.Errorf("the tape's film is %d in Swedish, want 31", got)
			}
			return
		}
	}
	t.Fatal("the tape is not in the bag")
}
