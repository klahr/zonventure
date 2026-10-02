package session

import (
	"slices"
	"strings"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
)

// How each graph.AnswerKind is played: the box the phone shows, and how
// an answer output judges what the phone sends back. A puzzle with no
// Answer is typed, and its outputs' matchers decide.

// answerInput is the answer box the phone shows for puzzle n.
func (t *Team) answerInput(n graph.Node) *Input {
	cfg := n.PuzzleCfg
	a := cfg.Answer
	switch cfg.Kind() {
	case graph.AnswerScan:
		in := &Input{Kind: "scan"}
		if b := cfg.Bar; b != nil {
			in.Button = b.Button
		}
		return in
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
	case graph.AnswerQuestions:
		// The questions are blocks; the phone counts their picks.
		return &Input{Kind: "questions"}
	case graph.AnswerKeys:
		in := &Input{Kind: "keys"}
		for i, slug := range a.Posts {
			if z := t.zoneBySlug(slug); z != nil {
				name := z.Name
				if name == "" {
					name = slug
				}
				in.Posts = append(in.Posts, t.G.Tr(t.Lang, graph.ZoneKey(slug), name))
				role := a.PostRole(i)
				if role != "" {
					role = t.G.Tr(t.Lang, graph.RoleKey(role, "name"), t.G.RoleName(role))
				}
				in.PostRoles = append(in.PostRoles, role)
				in.MyPosts = append(in.MyPosts, t.Viewer == nil || t.mayTurnAt(a, i, t.Viewer.Slot))
				in.PostsHeld = append(in.PostsHeld, t.postHeld(a, i))
			}
		}
		if !slices.ContainsFunc(in.PostRoles, func(r string) bool { return r != "" }) {
			in.PostRoles, in.MyPosts, in.PostsHeld = nil, nil, nil
		}
		return in
	case graph.AnswerItems:
		// The slots' labels only: which item fits where stays on the server.
		in := &Input{Kind: "items", Slots: make([]string, len(a.Slots))}
		for i, s := range a.Slots {
			in.Slots[i] = s.Label
		}
		short := t.short(a.Needs())
		if len(short) == 0 {
			return in
		}
		// Short of something: say so, as the author chose, and offer the
		// way out if there is one.
		switch a.Missing {
		case graph.MissingSay:
			in.Missing = true
		case graph.MissingName:
			in.Missing = true
			for _, slug := range short {
				in.MissingItems = append(in.MissingItems, t.itemName(slug))
			}
		}
		if o, ok := outputFor(n, graph.OnMissing); ok {
			label := o.Label
			if label != "" {
				label = t.G.Tr(t.Lang, graph.NodeKey(n.Slug, "out:"+o.Port+":label"), label)
			}
			in.Leave, in.LeaveLabel = true, label
		}
		return in
	}
	in := inputFor(cfg)
	if b := cfg.Bar; b != nil {
		in.Placeholder, in.Button, in.NoScan = b.Placeholder, b.Button, b.NoScan
		if b.Keyboard != "" {
			in.Kind = b.Keyboard
		}
	}
	return in
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
	case graph.AnswerSearch, graph.AnswerQuestions, graph.AnswerKeys:
		// Answer has already refused a team with questions left.
		return match.Result{OK: true, Multiplier: 1}
	case graph.AnswerItems:
		// The right item in every slot, and in the backpack: a phone
		// cannot place what the team does not carry.
		return match.Result{OK: a.Fits(guess) && t.carries(a.Needs()), Multiplier: 1}
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

// carries reports whether the team holds at least need of each item.
func (t *Team) carries(need map[string]int) bool {
	return len(t.short(need)) == 0
}

// short lists, in slug order, the items the team holds fewer of than need.
func (t *Team) short(need map[string]int) []string {
	var out []string
	for _, slug := range sortedKeys(need) {
		if t.Items[slug] < need[slug] {
			out = append(out, slug)
		}
	}
	return out
}

// itemName is an item's name as the backpack shows it.
func (t *Team) itemName(slug string) string {
	for _, it := range t.G.Items {
		if it.Slug == slug && it.Name != "" {
			return it.Name
		}
	}
	return slug
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
