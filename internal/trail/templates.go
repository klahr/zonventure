package trail

import (
	"strconv"
	"strings"
)

// Template is a trail to start from: a title to replace, whether it is
// for children (which asks for a guardian's consent when bought), and a
// few example cards that show how to write them. Person says that the
// title and the cards are about someone (a birthday child), whose name
// and age NewFromTemplateFor writes in.
type Template struct {
	Name   string `json:"name"`
	Title  string `json:"title"`
	Kids   bool   `json:"kids"`
	Person bool   `json:"person"`
	Cards  []Card `json:"cards"`
}

// Person is who a template is about. The zero value is the example,
// Elsa, 8.
type Person struct {
	Name string
	Age  int
}

// TemplateNames are the templates, in the order the creator offers them.
var TemplateNames = []string{"birthday", "kids", "office", "family", "blank"}

type example struct {
	kind    Kind
	text    string
	options []string
	correct string
	answers []string
}

var templates = map[string]map[string]struct {
	title string
	kids  bool
	cards []example
}{
	"en": {
		"birthday": {"{name} turns {age}", true, []example{
			{kind: Info, text: "Welcome to the birthday adventure! Find the cards in order and answer on your phone."},
			{kind: Quiz, text: "How old is {name} today?", options: []string{"{age-1}", "{age}", "{age+1}"}, correct: "X"},
			{kind: Quiz, text: "What is {name's} favourite animal?", options: []string{"Horse", "Cat", "Dog"}, correct: "1"},
			{kind: Answer, text: "What is the name of {name's} best friend?", answers: []string{"{friend}"}},
		}},
		"kids": {"Treasure trail", true, []example{
			{kind: Quiz, text: "How many legs does a spider have?", options: []string{"6", "8", "10"}, correct: "X"},
			{kind: Quiz, text: "Which planet is closest to the sun?", options: []string{"Mercury", "Venus", "Mars"}, correct: "1"},
			{kind: Answer, text: "What colour do you get if you mix blue and yellow?", answers: []string{"Green"}},
		}},
		"office": {"Office party adventure", false, []example{
			{kind: Quiz, text: "What year was the company founded?", options: []string{"1998", "2004", "2012"}, correct: "X"},
			{kind: Quiz, text: "How many offices do we have?", options: []string{"2", "3", "5"}, correct: "2"},
			{kind: Answer, text: "What is the name of the meeting room on the top floor?", answers: []string{""}},
		}},
		"family": {"Family party adventure", false, []example{
			{kind: Quiz, text: "Where did we go on holiday last summer?", options: []string{"The mountains", "The sea", "The city"}, correct: "X"},
			{kind: Answer, text: "What is grandma's favourite cake?", answers: []string{""}},
			{kind: Info, text: "Well done! The last card is by the front door."},
		}},
		"blank": {"My party adventure", false, []example{
			{kind: Quiz, options: []string{"", "", ""}},
		}},
	},
	"sv": {
		"birthday": {"{name} fyller {age}", true, []example{
			{kind: Info, text: "Välkomna till födelsedagsäventyret! Leta upp korten i ordning och svara i mobilen."},
			{kind: Quiz, text: "Hur gammal är {name} i dag?", options: []string{"{age-1}", "{age}", "{age+1}"}, correct: "X"},
			{kind: Quiz, text: "Vilket är {name's} favoritdjur?", options: []string{"Häst", "Katt", "Hund"}, correct: "1"},
			{kind: Answer, text: "Vad heter {name's} bästa kompis?", answers: []string{"{friend}"}},
		}},
		"kids": {"Skattjakt", true, []example{
			{kind: Quiz, text: "Hur många ben har en spindel?", options: []string{"6", "8", "10"}, correct: "X"},
			{kind: Quiz, text: "Vilken planet ligger närmast solen?", options: []string{"Merkurius", "Venus", "Mars"}, correct: "1"},
			{kind: Answer, text: "Vilken färg får du om du blandar blått och gult?", answers: []string{"Grön"}},
		}},
		"office": {"Kontorets partyäventyr", false, []example{
			{kind: Quiz, text: "Vilket år grundades företaget?", options: []string{"1998", "2004", "2012"}, correct: "X"},
			{kind: Quiz, text: "Hur många kontor har vi?", options: []string{"2", "3", "5"}, correct: "2"},
			{kind: Answer, text: "Vad heter mötesrummet högst upp?", answers: []string{""}},
		}},
		"family": {"Familjens partyäventyr", false, []example{
			{kind: Quiz, text: "Vart åkte vi på semester i somras?", options: []string{"Fjällen", "Havet", "Stan"}, correct: "X"},
			{kind: Answer, text: "Vilken är mormors favoritkaka?", answers: []string{""}},
			{kind: Info, text: "Bra jobbat! Sista kortet finns vid ytterdörren."},
		}},
		"blank": {"Mitt partyäventyr", false, []example{
			{kind: Quiz, options: []string{"", "", ""}},
		}},
	},
}

// NewFromTemplate is the named template in lang ("sv" or "en"), with
// fresh card ids, each card marked Example, and the example person; false when there is no such
// template.
func NewFromTemplate(name, lang string) (Template, bool) {
	return NewFromTemplateFor(name, lang, Person{})
}

// NewFromTemplateFor is NewFromTemplate about p: their name and age in
// the title and the cards, where the template has them.
func NewFromTemplateFor(name, lang string, p Person) (Template, bool) {
	byLang, ok := templates[lang]
	if !ok {
		lang, byLang = "en", templates["en"]
	}
	t, ok := byLang[name]
	if !ok {
		return Template{}, false
	}
	fill := personFiller(lang, p)
	out := Template{Name: name, Title: fill(t.title), Kids: t.kids, Person: t.title != fill(t.title), Cards: make([]Card, len(t.cards))}
	for i, e := range t.cards {
		c := Card{ID: NewID(), Kind: e.kind, Text: fill(e.text), Correct: e.correct, Example: true}
		for _, o := range e.options {
			c.Options = append(c.Options, fill(o))
		}
		for _, a := range e.answers {
			c.Answers = append(c.Answers, fill(a))
		}
		out.Cards[i] = c
	}
	return out, true
}

// personFiller writes p into a template's text: {name}, {name's} (the
// genitive, as lang forms it), {friend}, and {age}, {age-1}, {age+1}.
func personFiller(lang string, p Person) func(string) string {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = "Elsa"
	}
	age := p.Age
	if age < 1 {
		age = 8
	}
	gen := name + "'s"
	if lang == "sv" {
		// Swedish adds s, but not to a name that ends in one already.
		gen = name + "s"
		if r := []rune(strings.ToLower(name)); strings.ContainsRune("sxz", r[len(r)-1]) {
			gen = name
		}
	}
	// The example best friend, never the birthday child themself.
	friend := "Alva"
	if strings.EqualFold(name, friend) {
		friend = "Elsa"
	}
	r := strings.NewReplacer("{name's}", gen, "{name}", name, "{friend}", friend,
		"{age-1}", strconv.Itoa(age-1), "{age+1}", strconv.Itoa(age+1), "{age}", strconv.Itoa(age))
	return r.Replace
}
