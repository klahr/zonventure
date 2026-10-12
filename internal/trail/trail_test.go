package trail

import (
	"strings"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graph/valid"
	"github.com/klahr/zonventure/internal/graph/walk"
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
	if q.Type != graph.Story || q.StoryCfg.Blocks[0].Kind != graph.BlockQuiz || q.Title != "Fråga 1" || q.StoryCfg.Blocks[0].Text != "Ert svar på fråga 1:" {
		t.Errorf("quiz card: %+v", q)
	}
	if strings.Contains(q.StoryCfg.Blocks[0].Text, "How old") {
		t.Error("the question is on paper, not the phone")
	}
	a := g.Nodes[3]
	if a.Type != graph.Puzzle || a.Title != "Fråga 2" || a.PuzzleCfg.Outputs[0].Points != 1 || a.PuzzleCfg.Outputs[1].AfterAttempts != 1 {
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

// TestTemplatePerson: a birthday template is about the child named,
// genitive and age options included; the example is Elsa, 8.
func TestTemplatePerson(t *testing.T) {
	tp, _ := NewFromTemplate("birthday", "en")
	if !tp.Person || tp.Title != "Elsa turns 8" {
		t.Fatalf("example: %+v", tp)
	}
	cases := []struct {
		lang, name string
		age        int
		title, q   string
	}{
		{"en", "Mia", 7, "Mia turns 7", "What is Mia's favourite animal?"},
		{"sv", "Mia", 7, "Mia fyller 7", "Vilket är Mias favoritdjur?"},
		{"sv", "Jonas", 40, "Jonas fyller 40", "Vilket är Jonas favoritdjur?"},
		{"sv", "  ", 0, "Elsa fyller 8", "Vilket är Elsas favoritdjur?"},
	}
	for _, c := range cases {
		tp, _ := NewFromTemplateFor("birthday", c.lang, Person{Name: c.name, Age: c.age})
		if tp.Title != c.title || tp.Cards[2].Text != c.q {
			t.Errorf("%+v: %q, %q", c, tp.Title, tp.Cards[2].Text)
		}
	}
	tp, _ = NewFromTemplateFor("birthday", "en", Person{Name: "Mia", Age: 7})
	if got := strings.Join(tp.Cards[1].Options, ","); got != "6,7,8" || tp.Cards[1].Correct != "X" {
		t.Errorf("age options: %s %s", got, tp.Cards[1].Correct)
	}
	if kids, _ := NewFromTemplate("kids", "en"); kids.Person {
		t.Error("kids is about no one")
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

// The end screen's quiz walk lists the answer card with the quiz card,
// right when the team typed an accepted answer.
func TestQuizListsAnswerCards(t *testing.T) {
	play := func(guess string) session.QuizResult {
		t.Helper()
		g := Compile(sample(), "en")
		tm, step, err := session.Start(g, time.Unix(0, 0))
		if err != nil {
			t.Fatal(err)
		}
		tm.Apply(step)
		do := func(_ session.Result, step *walk.Step, err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
			if step != nil {
				tm.Apply(*step)
			}
		}
		do(tm.Continue("card-a"))
		do(tm.Pick("card-b", "q", "X"))
		do(tm.Continue("card-b"))
		do(tm.Answer("card-c", guess))
		if !tm.Completed {
			t.Fatal("not finished")
		}
		return tm.Quiz()
	}

	q := play("mons")
	if q.Right != 2 || q.Total != 2 || len(q.Items) != 2 || q.Points != 1 {
		t.Fatalf("quiz = %+v", q)
	}
	if r := q.Items[1]; !r.Typed || !r.Right || r.Picked != "mons" || r.Question != "Your answer to question 2:" || r.Answer != "Måns" {
		t.Errorf("typed row = %+v", r)
	}
	if q := play("Tiger"); q.Right != 1 || q.Total != 2 || q.Items[1].Right || q.Items[1].Picked != "" {
		t.Errorf("a wrong answer = %+v", q)
	}
}
