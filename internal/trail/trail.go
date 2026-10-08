// Package trail is the quiz trail: a simple adventure written as an
// ordered list of cards — a question with three options (1, X, 2), a
// question with a typed answer, or a card of information — that the
// simple creator edits and Compile turns into a graph, one way. The
// questions are on paper, hidden along the way; the phone shows "Card N"
// and takes the answer. No card has a place: answering is arriving.
//
// The cards are the source of truth (adventures.cards); the graph is
// made from them on every save and never read back.
package trail

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
)

// Kind is what a card asks.
type Kind string

const (
	// Quiz is a question with three options, for 1, X and 2: the classic
	// tipspromenad question. Answered on the phone, scored at the end.
	Quiz Kind = "quiz"
	// Answer is a question the team types the answer to: one try.
	Answer Kind = "answer"
	// Info is a card to read: a story, a hint, where to go next.
	Info Kind = "info"
)

// Card is one card of a trail. Options and Correct are a quiz's; Answers
// the accepted spellings of an answer card's answer.
type Card struct {
	ID      string   `json:"id"`
	Kind    Kind     `json:"kind"`
	Text    string   `json:"text"`
	Options []string `json:"options,omitempty"`
	Correct string   `json:"correct,omitempty"`
	Answers []string `json:"answers,omitempty"`
}

// Limits on a trail and its cards.
const (
	MaxCards       = 40
	MaxTextLen     = 300
	MaxOptionLen   = 80
	MaxAnswers     = 5
	MaxAnswerLen   = 80
	maxIDLen       = 16
	optionsPerQuiz = 3
)

// NewID is a card id: short, random, stable for the card's life.
func NewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Problem is what keeps a card, or the trail, from being played. Card is
// the card's id, empty for the trail as a whole; Code is stable for the
// creator to word in the reader's language.
type Problem struct {
	Card    string `json:"card,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Check reports what keeps the cards from being saved at all: too many,
// too long, of no known kind. A draft may be unfinished; it may not be
// unbounded.
func Check(cards []Card) error {
	if len(cards) > MaxCards {
		return fmt.Errorf("a trail has at most %d cards", MaxCards)
	}
	seen := map[string]bool{}
	for i, c := range cards {
		switch {
		case c.ID == "" || len(c.ID) > maxIDLen || seen[c.ID]:
			return fmt.Errorf("card %d has no id of its own", i+1)
		case c.Kind != Quiz && c.Kind != Answer && c.Kind != Info:
			return fmt.Errorf("card %d is of no known kind", i+1)
		case utf8.RuneCountInString(c.Text) > MaxTextLen:
			return fmt.Errorf("card %d is longer than %d characters", i+1, MaxTextLen)
		case len(c.Options) > optionsPerQuiz || len(c.Answers) > MaxAnswers:
			return fmt.Errorf("card %d has too many answers", i+1)
		}
		for _, o := range append(slices.Clone(c.Options), c.Answers...) {
			if utf8.RuneCountInString(o) > MaxOptionLen {
				return fmt.Errorf("card %d has an answer longer than %d characters", i+1, MaxOptionLen)
			}
		}
		seen[c.ID] = true
	}
	return nil
}

// Problems reports what keeps a trail from being played, card by card in
// order. None means it compiles to a valid graph.
func Problems(cards []Card) []Problem {
	var out []Problem
	asks := false
	for _, c := range cards {
		bad := func(code, msg string) { out = append(out, Problem{Card: c.ID, Code: code, Message: msg}) }
		if strings.TrimSpace(c.Text) == "" {
			bad("text", "has no text")
		}
		switch c.Kind {
		case Quiz:
			asks = true
			if len(c.Options) != optionsPerQuiz || slices.ContainsFunc(c.Options, func(o string) bool { return strings.TrimSpace(o) == "" }) {
				bad("options", "needs three options, for 1, X and 2")
			} else if !slices.Contains(graph.QuizChoices, c.Correct) {
				bad("correct", "has no right option marked")
			}
		case Answer:
			asks = true
			if !slices.ContainsFunc(c.Answers, func(a string) bool { return strings.TrimSpace(a) != "" }) {
				bad("answers", "has no answer")
			}
		}
	}
	switch {
	case len(cards) == 0:
		out = append(out, Problem{Code: "empty", Message: "has no cards"})
	case !asks:
		out = append(out, Problem{Code: "no_question", Message: "has no question"})
	}
	return out
}

// Compile makes the graph a trail plays, in lang ("sv" or "en"). The
// cards should have no Problems; ones that do compile all the same, to a
// graph the validator refuses.
//
// Each card is a step without a place. A quiz card is a story with a
// quiz block whose question is "Card N" — the question is on paper — and
// whose answer counts at the end, as a tipspromenad's does. An answer
// card is a puzzle the team answers once: right scores a point, wrong
// goes on all the same. An info card is a story with its text.
func Compile(cards []Card, lang string) *graph.Graph {
	w := words(lang)
	g := &graph.Graph{}
	add := func(n graph.Node) { g.Nodes = append(g.Nodes, n) }
	link := func(from, port, to string) {
		g.Edges = append(g.Edges, graph.Edge{From: from, FromPort: port, To: to})
	}
	add(graph.Node{Slug: "start", Type: graph.Start})
	prev, ports := "start", []string{graph.PortOut}
	n := 0
	for _, c := range cards {
		slug := "card-" + c.ID
		var node graph.Node
		switch c.Kind {
		case Quiz:
			n++
			node = graph.Node{Slug: slug, Type: graph.Story, Title: fmt.Sprintf(w.card, n), StoryCfg: &graph.StoryConfig{
				Blocks: []graph.Block{{ID: "q", Kind: graph.BlockQuiz, Text: w.pickIt, Options: trimmed(c.Options), Answer: c.Correct}},
			}}
		case Answer:
			n++
			var ms []match.Matcher
			for _, a := range c.Answers {
				if a = strings.TrimSpace(a); a != "" {
					ms = append(ms, match.Matcher{Kind: match.Exact, Value: a})
				}
			}
			node = graph.Node{Slug: slug, Type: graph.Puzzle, Title: fmt.Sprintf(w.card, n), PuzzleCfg: &graph.PuzzleConfig{
				Blocks: []graph.Block{{ID: "t", Kind: graph.BlockText, Text: w.typeIt}},
				Outputs: []graph.Output{
					{Port: "right", Trigger: graph.OnAnswer, Matchers: ms, Points: 1},
					{Port: "wrong", Trigger: graph.OnAttempts, AfterAttempts: 1},
				},
			}}
		default:
			node = graph.Node{Slug: slug, Type: graph.Story, Title: w.info, StoryCfg: &graph.StoryConfig{
				Blocks: []graph.Block{{ID: "t", Kind: graph.BlockText, Text: strings.TrimSpace(c.Text)}},
			}}
		}
		add(node)
		for _, p := range ports {
			link(prev, p, slug)
		}
		prev = slug
		if c.Kind == Answer {
			ports = []string{"right", "wrong"}
		} else {
			ports = []string{graph.PortOut}
		}
	}
	add(graph.Node{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{
		Blocks: []graph.Block{{ID: "t", Kind: graph.BlockText, Text: w.done}},
	}})
	for _, p := range ports {
		link(prev, p, "end")
	}
	return g
}

func trimmed(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.TrimSpace(s)
	}
	return out
}

// Number is each card's number on paper and on the phone: the questions
// counted in order, info cards unnumbered (0).
func Number(cards []Card) []int {
	out := make([]int, len(cards))
	n := 0
	for i, c := range cards {
		if c.Kind != Info {
			n++
			out[i] = n
		}
	}
	return out
}

type wording struct{ card, info, pickIt, typeIt, done string }

func words(lang string) wording {
	if lang == "sv" {
		return wording{card: "Kort %d", info: "Information", pickIt: "Välj svaret på kortet.", typeIt: "Skriv svaret på kortet.", done: "Det var sista kortet. Bra jobbat!"}
	}
	return wording{card: "Card %d", info: "Information", pickIt: "Pick the answer to the card.", typeIt: "Type the answer to the card.", done: "That was the last card. Well done!"}
}
