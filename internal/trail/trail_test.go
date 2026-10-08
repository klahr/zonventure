package trail

import (
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/valid"
)

func sample() []Card {
	return []Card{
		{ID: "a", Kind: Info, Text: "Welcome!"},
		{ID: "b", Kind: Quiz, Text: "How old?", Options: []string{"7", "8", "9"}, Correct: "X"},
		{ID: "c", Kind: Answer, Text: "Cat's name?", Answers: []string{"Måns", "Mons"}},
	}
}

// A finished trail compiles to a graph the validator accepts: no places,
// a quiz step per quiz card asking "Card N", a one-try puzzle per answer
// card.
func TestCompileValid(t *testing.T) {
	cards := sample()
	if p := Problems(cards); len(p) != 0 {
		t.Fatalf("problems: %+v", p)
	}
	g := Compile(cards, "sv")
	if r := valid.Check(g); r.HasErrors() {
		t.Fatalf("invalid graph: %+v", r)
	}
	if len(g.Zones) != 0 {
		t.Error("a trail has no places")
	}
	q := g.Nodes[2]
	if q.Type != graph.Story || q.StoryCfg.Blocks[0].Kind != graph.BlockQuiz || q.Title != "Kort 1" || q.StoryCfg.Blocks[0].Text != "Välj svaret på kortet." {
		t.Errorf("quiz card: %+v", q)
	}
	if strings.Contains(q.StoryCfg.Blocks[0].Text, "How old") {
		t.Error("the question is on paper, not the phone")
	}
	a := g.Nodes[3]
	if a.Type != graph.Puzzle || a.Title != "Kort 2" || a.PuzzleCfg.Outputs[0].Points != 1 || a.PuzzleCfg.Outputs[1].AfterAttempts != 1 {
		t.Errorf("answer card: %+v", a)
	}
	if n := Number(cards); n[0] != 0 || n[1] != 1 || n[2] != 2 {
		t.Errorf("numbers = %v", n)
	}
}

func TestProblems(t *testing.T) {
	codes := func(cards []Card) string {
		var s []string
		for _, p := range Problems(cards) {
			s = append(s, p.Card+":"+p.Code)
		}
		return strings.Join(s, " ")
	}
	if got := codes(nil); got != ":empty" {
		t.Errorf("no cards: %q", got)
	}
	if got := codes([]Card{{ID: "a", Kind: Info, Text: "Hi"}}); got != ":no_question" {
		t.Errorf("info only: %q", got)
	}
	if got := codes([]Card{{ID: "a", Kind: Quiz, Text: "Q", Options: []string{"1", "", "3"}}}); got != "a:options" {
		t.Errorf("empty option: %q", got)
	}
	if got := codes([]Card{{ID: "a", Kind: Quiz, Text: "Q", Options: []string{"1", "2", "3"}}}); got != "a:correct" {
		t.Errorf("no right option: %q", got)
	}
	if got := codes([]Card{{ID: "a", Kind: Answer, Text: "", Answers: []string{" "}}}); got != "a:text a:answers" {
		t.Errorf("empty answer card: %q", got)
	}
}

func TestCheck(t *testing.T) {
	if err := Check(sample()); err != nil {
		t.Fatal(err)
	}
	dup := sample()
	dup[1].ID = "a"
	if Check(dup) == nil {
		t.Error("duplicate ids pass")
	}
	long := sample()
	long[0].Text = strings.Repeat("x", MaxTextLen+1)
	if Check(long) == nil {
		t.Error("over-long text passes")
	}
	many := make([]Card, MaxCards+1)
	for i := range many {
		many[i] = Card{ID: NewID(), Kind: Info}
	}
	if Check(many) == nil {
		t.Error("too many cards pass")
	}
}

// Every template is a sensible start: known kinds, fresh ids, and the
// filled-in ones compile valid.
func TestTemplates(t *testing.T) {
	for _, lang := range []string{"en", "sv"} {
		for _, name := range TemplateNames {
			tp, ok := NewFromTemplate(name, lang)
			if !ok || tp.Title == "" || len(tp.Cards) == 0 {
				t.Fatalf("%s/%s: %+v", lang, name, tp)
			}
			if err := Check(tp.Cards); err != nil {
				t.Errorf("%s/%s: %v", lang, name, err)
			}
			if name == "birthday" || name == "kids" {
				if p := Problems(tp.Cards); len(p) != 0 {
					t.Errorf("%s/%s problems: %+v", lang, name, p)
				}
				if r := valid.Check(Compile(tp.Cards, lang)); r.HasErrors() {
					t.Errorf("%s/%s invalid: %+v", lang, name, r)
				}
			}
		}
	}
	if _, ok := NewFromTemplate("nope", "en"); ok {
		t.Error("unknown template")
	}
}

// TestCover: every look the party adventure creator offers
// (web/packages/trail/src/themes.ts TRAIL_THEMES) has its print picture.
func TestCover(t *testing.T) {
	for _, id := range []string{"party", "sunshine", "candy", "rainbow", "unicorn", "ice-cream", "jungle",
		"pirate", "ocean", "space", "detective", "retro-80s", "office", "pen-and-paper"} {
		if len(Cover(id)) == 0 {
			t.Errorf("no cover for %s", id)
		}
	}
	for _, id := range []string{"", "nope", "../trail"} {
		if Cover(id) != nil {
			t.Errorf("cover for %q", id)
		}
	}
}
