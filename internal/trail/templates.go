package trail

// Template is a trail to start from: a title to replace, whether it is
// for children (which asks for a guardian's consent when bought), and a
// few example cards that show how to write them.
type Template struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Kids  bool   `json:"kids"`
	Cards []Card `json:"cards"`
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
		"birthday": {"Elsa turns 8", true, []example{
			{kind: Info, text: "Welcome to the birthday trail! Find the cards in order and answer on your phone."},
			{kind: Quiz, text: "How old is Elsa today?", options: []string{"7", "8", "9"}, correct: "X"},
			{kind: Quiz, text: "What is Elsa's favourite animal?", options: []string{"Horse", "Cat", "Dog"}, correct: "1"},
			{kind: Answer, text: "What is the name of Elsa's best friend?", answers: []string{"Alva"}},
		}},
		"kids": {"Treasure trail", true, []example{
			{kind: Quiz, text: "How many legs does a spider have?", options: []string{"6", "8", "10"}, correct: "X"},
			{kind: Quiz, text: "Which planet is closest to the sun?", options: []string{"Mercury", "Venus", "Mars"}, correct: "1"},
			{kind: Answer, text: "What colour do you get if you mix blue and yellow?", answers: []string{"Green"}},
		}},
		"office": {"Office quiz trail", false, []example{
			{kind: Quiz, text: "What year was the company founded?", options: []string{"1998", "2004", "2012"}, correct: "X"},
			{kind: Quiz, text: "How many offices do we have?", options: []string{"2", "3", "5"}, correct: "2"},
			{kind: Answer, text: "What is the name of the meeting room on the top floor?", answers: []string{""}},
		}},
		"family": {"Family quiz trail", false, []example{
			{kind: Quiz, text: "Where did we go on holiday last summer?", options: []string{"The mountains", "The sea", "The city"}, correct: "X"},
			{kind: Answer, text: "What is grandma's favourite cake?", answers: []string{""}},
			{kind: Info, text: "Well done! The last card is by the front door."},
		}},
		"blank": {"My quiz trail", false, []example{
			{kind: Quiz, options: []string{"", "", ""}},
		}},
	},
	"sv": {
		"birthday": {"Elsa fyller 8", true, []example{
			{kind: Info, text: "Välkomna till födelsedagspromenaden! Leta upp korten i ordning och svara i mobilen."},
			{kind: Quiz, text: "Hur gammal är Elsa i dag?", options: []string{"7", "8", "9"}, correct: "X"},
			{kind: Quiz, text: "Vilket är Elsas favoritdjur?", options: []string{"Häst", "Katt", "Hund"}, correct: "1"},
			{kind: Answer, text: "Vad heter Elsas bästa kompis?", answers: []string{"Alva"}},
		}},
		"kids": {"Skattjakt", true, []example{
			{kind: Quiz, text: "Hur många ben har en spindel?", options: []string{"6", "8", "10"}, correct: "X"},
			{kind: Quiz, text: "Vilken planet ligger närmast solen?", options: []string{"Merkurius", "Venus", "Mars"}, correct: "1"},
			{kind: Answer, text: "Vilken färg får du om du blandar blått och gult?", answers: []string{"Grön"}},
		}},
		"office": {"Tipspromenad på kontoret", false, []example{
			{kind: Quiz, text: "Vilket år grundades företaget?", options: []string{"1998", "2004", "2012"}, correct: "X"},
			{kind: Quiz, text: "Hur många kontor har vi?", options: []string{"2", "3", "5"}, correct: "2"},
			{kind: Answer, text: "Vad heter mötesrummet högst upp?", answers: []string{""}},
		}},
		"family": {"Familjens tipspromenad", false, []example{
			{kind: Quiz, text: "Vart åkte vi på semester i somras?", options: []string{"Fjällen", "Havet", "Stan"}, correct: "X"},
			{kind: Answer, text: "Vilken är mormors favoritkaka?", answers: []string{""}},
			{kind: Info, text: "Bra jobbat! Sista kortet finns vid ytterdörren."},
		}},
		"blank": {"Min tipspromenad", false, []example{
			{kind: Quiz, options: []string{"", "", ""}},
		}},
	},
}

// NewFromTemplate is the named template in lang ("sv" or "en"), with
// fresh card ids; false when there is no such template.
func NewFromTemplate(name, lang string) (Template, bool) {
	byLang, ok := templates[lang]
	if !ok {
		byLang = templates["en"]
	}
	t, ok := byLang[name]
	if !ok {
		return Template{}, false
	}
	out := Template{Name: name, Title: t.title, Kids: t.kids, Cards: make([]Card, len(t.cards))}
	for i, e := range t.cards {
		out.Cards[i] = Card{ID: NewID(), Kind: e.kind, Text: e.text, Options: append([]string(nil), e.options...),
			Correct: e.correct, Answers: append([]string(nil), e.answers...)}
	}
	return out, true
}
