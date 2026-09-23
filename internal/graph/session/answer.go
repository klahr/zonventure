package session

import (
	"strings"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
)

// How each graph.AnswerKind is played: the box the phone shows, and how
// an answer output judges what the phone sends back. A puzzle with no
// Answer is typed, and its outputs' matchers decide.

// answerInput is the answer box the phone shows for a puzzle.
func (t *Team) answerInput(cfg *graph.PuzzleConfig) *Input {
	a := cfg.Answer
	switch cfg.Kind() {
	case graph.AnswerTiles:
		return &Input{Kind: "tiles", Rows: a.Rows, Cols: a.Cols, Image: a.Image}
	case graph.AnswerSearch:
		return &Input{Kind: "search", At: t.searchAt(cfg)}
	case graph.AnswerChoice:
		in := &Input{Kind: "choice"}
		for _, f := range a.Fields {
			// Labels and options only: the answers stay on the server.
			in.Fields = append(in.Fields, ChoiceFieldView{Label: f.Label, Options: f.Options})
		}
		return in
	case graph.AnswerLock:
		return &Input{Kind: "lock", Wheels: a.Wheels, Symbols: a.Symbols, Start: a.Start}
	}
	return inputFor(cfg)
}

// answerOrder is the order a puzzle's answer outputs are tried in: the
// author's, except that a search tries its outputs with no zone — which
// match anywhere — only after every placed one, as match.Any treats a
// catch-all.
func answerOrder(cfg *graph.PuzzleConfig) []graph.Output {
	var placed, anywhere []graph.Output
	for _, o := range cfg.Outputs {
		switch {
		case o.Trigger != graph.OnAnswer:
		case cfg.Kind() == graph.AnswerSearch && o.Zone == "":
			anywhere = append(anywhere, o)
		default:
			placed = append(placed, o)
		}
	}
	return append(placed, anywhere...)
}

// judge decides whether answer output o accepts guess. Its zone
// constraint is the caller's to apply; for a search, that is the whole
// decision.
func (t *Team) judge(n graph.Node, o graph.Output, guess string) match.Result {
	cfg := n.PuzzleCfg
	a := cfg.Answer
	switch cfg.Kind() {
	case graph.AnswerTiles:
		// One right answer, the solved order. Spaces are ignored;
		// anything else — a short list, a repeat — is wrong.
		return match.Result{OK: strings.ReplaceAll(guess, " ", "") == a.Solution(), Multiplier: 1}
	case graph.AnswerSearch:
		return match.Result{OK: true, Multiplier: 1}
	case graph.AnswerChoice:
		if a.Graded() {
			// A marked form is never wrong, only incomplete: it pays the
			// output's points in shares for the fields answered right.
			correct, ok := a.Mark(guess)
			return match.Result{OK: ok, Multiplier: float64(correct) / float64(len(a.Fields))}
		}
	}
	m := match.Any(o.Matchers, guess)
	if !m.OK {
		// An answer in a language the adventure is translated into
		// counts too.
		m = match.Any(t.G.AlternativeAnswers(n.Slug, o.Port), guess)
	}
	return m
}

// searchAt names the zone a searching team stands in: one of the search's
// group, or of the puzzle's own output zones when it names no group.
func (t *Team) searchAt(cfg *graph.PuzzleConfig) string {
	if t.Position == nil {
		return ""
	}
	group := cfg.Answer.Group
	mine := map[string]bool{}
	for _, o := range cfg.Outputs {
		if o.Zone != "" {
			mine[o.Zone] = true
		}
	}
	for _, z := range t.G.Zones {
		in := group != "" && z.Group == group || group == "" && mine[z.Slug]
		if in && ZoneContains(z, t.Position.Lat, t.Position.Lng) {
			name := z.Name
			if name == "" {
				name = z.Slug
			}
			return t.G.Tr(t.Lang, graph.ZoneKey(z.Slug), name)
		}
	}
	return ""
}
