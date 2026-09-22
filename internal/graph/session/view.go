package session

import (
	"math"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
)

// View is what a player needs to render: where the team is, and what
// they can do there.
//
// It is the one contract between the engine and every screen that shows
// a step — the player app, the designer's preview and the simulator all
// render a View with the same component. Nothing in it is a secret: no
// answer, no matcher, no hint the team has not paid for.
type View struct {
	Node  string      `json:"node,omitempty"`
	Type  graph.Type  `json:"type,omitempty"`
	Title string      `json:"title,omitempty"`
	Text  string      `json:"text,omitempty"`
	Zone  *graph.Zone `json:"zone,omitempty"`
	// Here reports whether the team is inside Zone. Meaningless when
	// Zone is nil.
	Here bool `json:"here,omitempty"`

	// Input describes the answer box of a puzzle.
	Input *Input `json:"input,omitempty"`

	Hints     []string  `json:"hints,omitempty"`
	HintCount int       `json:"hint_count,omitempty"`
	NextHint  *NextHint `json:"next_hint,omitempty"`
	Attempts  int       `json:"attempts,omitempty"`

	Offer     []OfferEntry `json:"offer,omitempty"`
	CanFinish bool         `json:"can_finish,omitempty"`

	// CanSkip reports whether the skip thresholds are currently met.
	CanSkip      bool       `json:"can_skip,omitempty"`
	BlockedUntil *time.Time `json:"blocked_until,omitempty"`
	Deadline     *time.Time `json:"deadline,omitempty"`
	// HasTimeout says the puzzle has somewhere to go when Deadline
	// passes, so the client should call Timeout rather than just show
	// that time is up.
	HasTimeout bool `json:"has_timeout,omitempty"`

	Score          float64       `json:"score"`
	PenaltyScore   float64       `json:"penalty_score"`
	PenaltySeconds int           `json:"penalty_seconds"`
	Finished       bool          `json:"finished,omitempty"`
	Outcome        graph.Outcome `json:"outcome,omitempty"`
	Items          []ItemView    `json:"items,omitempty"`
	Journal        []JournalView `json:"journal,omitempty"`
	Theme          string        `json:"theme,omitempty"`

	// Now is the engine's clock when the view was built. Every deadline
	// above is relative to it, so a client counts down from Now rather
	// than from its own clock — which may be wrong on a phone, and is
	// deliberately frozen in the simulator.
	Now time.Time `json:"now"`
}

// Input is the shape of a puzzle's answer box, derived from its
// matchers so the author never has to say it twice.
type Input struct {
	// Kind is "number" when every accepted answer is numeric, so a phone
	// can offer the number pad; otherwise "text".
	Kind string `json:"kind"`
	// Parts is set when the answer is a list: how many items to give.
	Parts int `json:"parts,omitempty"`
	// Ordered says the list must be in the right order.
	Ordered bool `json:"ordered,omitempty"`
}

// NextHint is the next hint the team can reveal.
type NextHint struct {
	Index       int       `json:"index"`
	AvailableAt time.Time `json:"available_at"`
	Cost        int       `json:"cost,omitempty"`
}

// OfferEntry is one hub branch the team may choose.
type OfferEntry struct {
	Port  string `json:"port"`
	Label string `json:"label,omitempty"`
	// Locked is set for a branch whose condition does not hold yet. It
	// is shown greyed out, unless the hub hides locked branches.
	Locked bool `json:"locked,omitempty"`
}

// ItemView is one thing the team is carrying.
type ItemView struct {
	Slug string `json:"slug"`
	Name string `json:"name,omitempty"`
	Icon string `json:"icon,omitempty"`
	Qty  int    `json:"qty"`
}

// JournalView is one unlocked journal entry.
type JournalView struct {
	Slug  string `json:"slug"`
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

// View renders the team's state for the player.
func (t *Team) View() View {
	v := View{
		Node:           t.Current,
		Score:          t.Points,
		PenaltyScore:   t.PenaltyScore,
		PenaltySeconds: t.PenaltySeconds,
		Finished:       t.Completed,
		Outcome:        t.Ending,
		Theme:          t.Theme,
		Now:            t.Now,
	}
	for _, slug := range sortedKeys(t.Items) {
		if q := t.Items[slug]; q > 0 {
			iv := ItemView{Slug: slug, Qty: q}
			for _, it := range t.G.Items {
				if it.Slug == slug {
					iv.Name, iv.Icon = it.Name, it.Icon
				}
			}
			v.Items = append(v.Items, iv)
		}
	}
	for _, slug := range sortedKeys(t.Journal) {
		jv := JournalView{Slug: slug}
		for _, je := range t.G.Journal {
			if je.Slug == slug {
				jv.Title, jv.Body = je.Title, je.Body
			}
		}
		v.Journal = append(v.Journal, jv)
	}

	n, ok := t.G.Node(t.Current)
	if !ok {
		return v
	}
	v.Type, v.Title = n.Type, n.Title
	setZone := func(slug string) {
		for _, z := range t.G.Zones {
			if z.Slug == slug {
				zc := z
				v.Zone, v.Here = &zc, t.InZone(slug)
			}
		}
	}

	switch n.Type {
	case graph.Puzzle:
		cfg := n.PuzzleCfg
		if cfg == nil {
			return v
		}
		v.Text = cfg.Prompt
		if cfg.Zone != "" {
			setZone(cfg.Zone)
		}
		v.Input = inputFor(cfg)
		revealed := 0
		if r := t.Nodes[n.Slug]; r != nil {
			revealed, v.Attempts = r.HintsRevealed, r.Attempts
			if r.BlockedUntil != nil && t.Now.Before(*r.BlockedUntil) {
				v.BlockedUntil = r.BlockedUntil
			}
		}
		for i := 0; i < revealed && i < len(cfg.Hints); i++ {
			v.Hints = append(v.Hints, cfg.Hints[i].Text)
		}
		v.HintCount = len(cfg.Hints)
		if revealed < len(cfg.Hints) {
			h := cfg.Hints[revealed]
			v.NextHint = &NextHint{
				Index:       revealed,
				AvailableAt: t.NodeEnteredAt.Add(time.Duration(h.DelaySeconds) * time.Second),
				Cost:        h.Cost,
			}
		}
		if cfg.TimeLimitSeconds > 0 && !t.NodeEnteredAt.IsZero() {
			d := t.NodeEnteredAt.Add(time.Duration(cfg.TimeLimitSeconds) * time.Second)
			v.Deadline = &d
			_, v.HasTimeout = outputFor(n, graph.OnTimeout)
		}
		v.CanSkip = t.canSkip(n)
	case graph.Story:
		if n.StoryCfg != nil {
			v.Text = n.StoryCfg.Text
			if n.StoryCfg.Zone != "" {
				setZone(n.StoryCfg.Zone)
			}
		}
	case graph.End:
		if n.EndCfg != nil {
			v.Text = n.EndCfg.Text
		}
	case graph.Hub:
		// Recompute the offer: it is a function of state, not stored.
		if n.HubCfg != nil {
			for _, b := range n.HubCfg.Branches {
				if t.BranchDone(n.Slug, b.Port) {
					continue
				}
				open := t.branchOpen(b)
				if !open && n.HubCfg.HideLocked {
					continue
				}
				v.Offer = append(v.Offer, OfferEntry{Port: b.Port, Label: b.Label, Locked: !open})
			}
			v.CanFinish = t.CompletedBranches(n.Slug) >= n.HubCfg.RequiredBranches()
		}
	}
	return v
}

// inputFor derives the answer box from the matchers of a puzzle's
// answer outputs. A catch-all ("anything") says nothing about shape, so
// it does not stop an otherwise numeric puzzle getting the number pad.
func inputFor(cfg *graph.PuzzleConfig) *Input {
	in := &Input{Kind: "text"}
	numeric, seen := true, false
	for _, o := range cfg.Outputs {
		if o.Trigger != graph.OnAnswer {
			continue
		}
		for _, m := range o.Matchers {
			switch m.Kind {
			case match.Interval, match.Near:
				seen = true
			case match.Anything:
			case match.Set:
				seen, numeric = true, false
				parts := m.Required
				if parts <= 0 {
					parts = len(m.Members)
				}
				if parts > in.Parts {
					in.Parts, in.Ordered = parts, m.Ordered
				}
			default:
				seen, numeric = true, false
			}
		}
	}
	if seen && numeric {
		in.Kind = "number"
	}
	return in
}

// HaversineMeters is the great-circle distance, R = 6371000, matching
// the lane runtime exactly so a zone admits the same positions.
func HaversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371000.0
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat, dLng := rad(lat2-lat1), rad(lng2-lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return r * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
