package session

import (
	"fmt"
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

	// Starts are set while the team is choosing where to begin: Node is
	// empty, Type is "start", and the team picks one with ChooseStart.
	Starts []StartOption `json:"starts,omitempty"`

	// Blocks are what a puzzle, story or end shows, in the author's
	// order: texts, and pictures and sounds the client fetches from
	// /media/{id}.
	Blocks []BlockView `json:"blocks,omitempty"`
	// Media, with Text, is Blocks for a player still running the bundle
	// from before blocks: its pictures and sounds, and Text its texts.
	Media []graph.Media `json:"media,omitempty"`

	// Input describes the answer box of a puzzle.
	Input *Input `json:"input,omitempty"`

	// Hints are the revealed hints, with their media.
	Hints     []HintView `json:"hints,omitempty"`
	HintCount int        `json:"hint_count,omitempty"`
	NextHint  *NextHint  `json:"next_hint,omitempty"`
	Attempts  int        `json:"attempts,omitempty"`

	Offer     []OfferEntry `json:"offer,omitempty"`
	CanFinish bool         `json:"can_finish,omitempty"`

	// Languages the adventure can be played in, main first, and the one
	// this view is in — for the phone's language switch.
	Languages []string `json:"languages,omitempty"`
	Lang      string   `json:"lang,omitempty"`
	// Meta is the adventure's own title, intro and outro in the team's
	// language, where they are translated — they live on the adventure,
	// not in the graph, so the caller swaps them in.
	Meta map[string]string `json:"meta,omitempty"`

	// EndsAt is when the adventure's time limit falls, for a countdown.
	// TimedOut says it has moved the team, and TimeUpText is the author's
	// message for that — empty for the phone's own "time is up".
	EndsAt     *time.Time `json:"ends_at,omitempty"`
	TimedOut   bool       `json:"timed_out,omitempty"`
	TimeUpText string     `json:"time_up_text,omitempty"`

	// BackTo is the home hub a team may go back to from here, and
	// BackLabel its title, for the button. Empty when there is none.
	BackTo    string `json:"back_to,omitempty"`
	BackLabel string `json:"back_label,omitempty"`

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
	// HasJournal says the adventure keeps a journal at all, so the
	// player shows its button from the start, empty until notes arrive.
	HasJournal bool   `json:"has_journal,omitempty"`
	Theme      string `json:"theme,omitempty"`

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

	// Wheels, Symbols and Start describe a combination lock (Kind "lock").
	Wheels  int                `json:"wheels,omitempty"`
	Symbols []graph.LockSymbol `json:"symbols,omitempty"`
	Start   []int              `json:"start,omitempty"`

	// Fields are the questions of a choice (Kind "choice"): labels and
	// options only — the answers stay on the server.
	Fields []ChoiceFieldView `json:"fields,omitempty"`

	// At is, for a search (Kind "search"), the zone the team stands in —
	// "sector C3" — or empty when it is in none of them.
	At string `json:"at,omitempty"`

	// Rows, Cols and Image describe a tile puzzle (Kind "tiles"): the
	// picture to cut up and the grid to cut it into.
	Rows  int          `json:"rows,omitempty"`
	Cols  int          `json:"cols,omitempty"`
	Image *graph.Media `json:"image,omitempty"`
}

// ChoiceFieldView is a question as the phone shows it.
type ChoiceFieldView struct {
	Label   string               `json:"label,omitempty"`
	Options []graph.ChoiceOption `json:"options"`
}

// StartOption is one start a team may begin at.
type StartOption struct {
	Node  string      `json:"node"`
	Title string      `json:"title,omitempty"`
	Zone  *graph.Zone `json:"zone,omitempty"`
	// Here reports whether the team is inside Zone, so the start can be
	// chosen now. Always true for a start without a zone.
	Here bool `json:"here"`
}

// BlockView is a block as the player sees it. A compass or map carries
// the zone it shows, so the phone needs nothing else to draw it.
type BlockView struct {
	graph.Block
	Place *graph.Zone `json:"place,omitempty"`
}

// HintView is one revealed hint.
type HintView struct {
	Text  string        `json:"text"`
	Media []graph.Media `json:"media,omitempty"`
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
	// Visited marks a home-screen branch the team has been into. It
	// stays open; this only tells the team where they have been.
	Visited bool         `json:"visited,omitempty"`
	Note    string       `json:"note,omitempty"`
	Image   *graph.Media `json:"image,omitempty"`
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
	Slug    string        `json:"slug"`
	Title   string        `json:"title,omitempty"`
	Body    string        `json:"body,omitempty"`
	Section string        `json:"section,omitempty"`
	Media   []graph.Media `json:"media,omitempty"`
}

// journalView lists the unlocked notes: in the order the author wrote
// them, or — when the adventure asks — the order the team found them.
func (t *Team) journalView() []JournalView {
	bySlug := make(map[string]graph.JournalEntry, len(t.G.Journal))
	for _, je := range t.G.Journal {
		bySlug[je.Slug] = je
	}
	view := func(je graph.JournalEntry) JournalView {
		return JournalView{Slug: je.Slug, Title: je.Title, Body: je.Body, Section: je.Section, Media: je.Media}
	}
	var out []JournalView
	if t.G.JournalByUnlock {
		listed := map[string]bool{}
		for _, slug := range t.JournalOrder {
			if je, ok := bySlug[slug]; ok && t.Journal[slug] && !listed[slug] {
				listed[slug] = true
				out = append(out, view(je))
			}
		}
		// Anything unlocked but not in the order — a team saved before
		// the order was kept — goes last, in the author's order.
		for _, je := range t.G.Journal {
			if t.Journal[je.Slug] && !listed[je.Slug] {
				out = append(out, view(je))
			}
		}
		return out
	}
	for _, je := range t.G.Journal {
		if t.Journal[je.Slug] {
			out = append(out, view(je))
		}
	}
	return out
}

// setBlocks shows a step's blocks, and the same content in the shape
// players from before blocks read.
func (t *Team) setBlocks(v *View, bs []graph.Block) {
	v.Blocks = make([]BlockView, len(bs))
	for i, b := range bs {
		v.Blocks[i].Block = b
		if b.Kind == graph.BlockCompass || b.Kind == graph.BlockMap {
			for _, z := range t.G.Zones {
				if z.Slug == b.Zone {
					zc := z
					v.Blocks[i].Place = &zc
				}
			}
		}
	}
	v.Text, v.Media = graph.TextOf(bs), graph.MediaOf(bs)
}

// View renders the team's state for the player.
func (t *Team) View() View {
	v := t.view()
	t.translate(&v)
	return v
}

// translate puts a view into the team's language. The walk and the view
// are built in the adventure's main language; this is the only place the
// translations are read, and anything without one stays as it was.
func (t *Team) translate(v *View) {
	// Only a translated adventure has languages to choose between; any
	// other leaves the phone in the player's own.
	if v.Languages = t.G.Languages(); v.Languages != nil {
		v.Lang = t.Lang
		if v.Lang == "" {
			v.Lang = v.Languages[0]
		}
	}
	if t.Lang == "" || t.Lang == t.G.Language {
		return
	}
	g, l := t.G, t.Lang
	tr := func(key, main string) string { return g.Tr(l, key, main) }
	for _, f := range []string{"title", "intro", "outro"} {
		if s := tr(graph.MetaKey(f), ""); s != "" {
			if v.Meta == nil {
				v.Meta = map[string]string{}
			}
			v.Meta[f] = s
		}
	}
	zone := func(z *graph.Zone) *graph.Zone {
		if z == nil {
			return nil
		}
		c := *z
		c.Name = tr(graph.ZoneKey(z.Slug), z.Name)
		return &c
	}
	for i := range v.Starts {
		s := &v.Starts[i]
		s.Title = tr(graph.NodeKey(s.Node, "title"), s.Title)
		s.Zone = zone(s.Zone)
	}
	v.Journal = append([]JournalView(nil), v.Journal...)
	for i := range v.Journal {
		j := &v.Journal[i]
		j.Title = tr(graph.JournalKey(j.Slug, "title"), j.Title)
		j.Body = tr(graph.JournalKey(j.Slug, "body"), j.Body)
		if j.Section != "" {
			j.Section = tr(graph.SectionKey(j.Section), j.Section)
		}
	}
	if v.TimeUpText != "" && g.TimeLimit != nil {
		v.TimeUpText = tr(graph.TimeUpKey, v.TimeUpText)
	}
	if v.BackTo != "" {
		v.BackLabel = tr(graph.NodeKey(v.BackTo, "title"), v.BackLabel)
	}
	if v.Node == "" {
		return
	}
	key := func(field string) string { return graph.NodeKey(v.Node, field) }
	v.Title = tr(key("title"), v.Title)
	if v.Type == graph.Hub {
		v.Text = tr(key("hub-text"), v.Text)
	} else if v.Blocks != nil {
		v.Blocks = append([]BlockView(nil), v.Blocks...)
		plain := make([]graph.Block, len(v.Blocks))
		for i := range v.Blocks {
			b := &v.Blocks[i]
			if b.Kind == graph.BlockText {
				b.Text = tr(graph.BlockKey(v.Node, b.ID), b.Text)
			}
			b.Place = zone(b.Place)
			plain[i] = b.Block
		}
		v.Text = graph.TextOf(plain)
	}
	v.Zone = zone(v.Zone)
	for i := range v.Hints {
		v.Hints[i].Text = tr(key(fmt.Sprintf("hint:%d", i)), v.Hints[i].Text)
	}
	for i := range v.Offer {
		o := &v.Offer[i]
		o.Label = tr(key("branch:"+o.Port+":label"), o.Label)
		o.Note = tr(key("branch:"+o.Port+":note"), o.Note)
	}
	if in := v.Input; in != nil {
		c := *in
		c.Fields = make([]ChoiceFieldView, len(in.Fields))
		for i, f := range in.Fields {
			opts := make([]graph.ChoiceOption, len(f.Options))
			for k, o := range f.Options {
				opts[k] = graph.ChoiceOption{Value: o.Value, Label: tr(key(fmt.Sprintf("field:%d:opt:%s", i, o.Value)), o.Label)}
			}
			c.Fields[i] = ChoiceFieldView{Label: tr(key(fmt.Sprintf("field:%d:label", i)), f.Label), Options: opts}
		}
		if len(in.Fields) == 0 {
			c.Fields = nil
		}
		if len(in.Symbols) > 0 {
			c.Symbols = make([]graph.LockSymbol, len(in.Symbols))
			for i, s := range in.Symbols {
				s.Label = tr(key("sym:"+s.Value), s.Label)
				c.Symbols[i] = s
			}
		}
		v.Input = &c
	}
}

func (t *Team) view() View {
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
	v.HasJournal = len(t.G.Journal) > 0
	v.Journal = t.journalView()

	if t.Current == "" && !t.Completed && t.G.ChoosesStart() {
		v.Type = graph.Start
		for _, st := range t.G.Starts() {
			o := StartOption{Node: st.Slug, Title: st.Title, Here: true}
			if st.StartCfg != nil && st.StartCfg.Zone != "" {
				for _, z := range t.G.Zones {
					if z.Slug == st.StartCfg.Zone {
						zc := z
						o.Zone, o.Here = &zc, t.InZone(z.Slug)
					}
				}
			}
			v.Starts = append(v.Starts, o)
		}
		return v
	}

	n, ok := t.G.Node(t.Current)
	if !ok {
		return v
	}
	v.Type, v.Title = n.Type, n.Title
	if t.TimedOut {
		v.TimedOut = true
		if t.G.TimeLimit != nil {
			v.TimeUpText = t.G.TimeLimit.Text
		}
	} else if d, ok := t.Deadline(); ok && !t.Completed {
		v.EndsAt = &d
	}
	if !t.Completed {
		if hub, ok := t.backTo(); ok {
			h, _ := t.G.Node(hub)
			v.BackTo, v.BackLabel = hub, h.Title
		}
	}
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
		t.setBlocks(&v, cfg.Blocks)
		if cfg.Zone != "" {
			setZone(cfg.Zone)
		}
		v.Input = t.answerInput(cfg)
		revealed := 0
		if r := t.Nodes[n.Slug]; r != nil {
			revealed, v.Attempts = r.HintsRevealed, r.Attempts
			if r.BlockedUntil != nil && t.Now.Before(*r.BlockedUntil) {
				v.BlockedUntil = r.BlockedUntil
			}
		}
		for i := 0; i < revealed && i < len(cfg.Hints); i++ {
			v.Hints = append(v.Hints, HintView{Text: cfg.Hints[i].Text, Media: cfg.Hints[i].Media})
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
			t.setBlocks(&v, n.StoryCfg.Blocks)
			if n.StoryCfg.Zone != "" {
				setZone(n.StoryCfg.Zone)
			}
		}
	case graph.End:
		if n.EndCfg != nil {
			t.setBlocks(&v, n.EndCfg.Blocks)
		}
	case graph.Hub:
		// Recompute the offer: it is a function of state, not stored.
		if n.HubCfg != nil {
			for _, b := range n.HubCfg.Branches {
				if t.BranchDone(n.Slug, b.Port) && !n.HubCfg.Home {
					continue
				}
				open := t.branchOpen(b)
				if !open && n.HubCfg.HideLocked {
					continue
				}
				v.Offer = append(v.Offer, OfferEntry{Port: b.Port, Label: b.Label, Locked: !open, Visited: t.BranchDone(n.Slug, b.Port), Note: b.Note, Image: b.Image})
			}
			v.Text = n.HubCfg.Text
			v.CanFinish = !n.HubCfg.Home && t.CompletedBranches(n.Slug) >= n.HubCfg.RequiredBranches()
		}
	}
	return v
}

// CanSeeMedia reports whether the team may fetch a media file.
//
// Hint media is visible only once the hint is revealed, always — a hint
// the team has not paid for must not leak through its picture.
//
// Step media follows the adventure's strict-assets setting, exactly as
// lane pages do. Strict: only steps the team has reached, for puzzles
// where the picture is the clue. Not strict (the default): any step of
// the adventure, so the player can download pictures ahead and still
// show them in a dead spot.
func (t *Team) CanSeeMedia(id int64, strict bool) bool {
	for _, m := range t.VisibleMedia(strict) {
		if m == id {
			return true
		}
	}
	return false
}

// VisibleMedia lists every media id the team may fetch now, once each,
// in graph order. The player app downloads these ahead of need.
func (t *Team) VisibleMedia(strict bool) []int64 {
	seen := map[int64]bool{}
	var out []int64
	// A journal note's pictures come with the note, never before it.
	for _, je := range t.G.Journal {
		if !t.Journal[je.Slug] {
			continue
		}
		for _, m := range je.Media {
			if !seen[m.ID] {
				seen[m.ID] = true
				out = append(out, m.ID)
			}
		}
	}
	for _, n := range t.G.Nodes {
		r := t.Nodes[n.Slug]
		if r == nil && strict {
			continue
		}
		for i, ms := range n.MediaLists() {
			// List 0 is the step's own; list i is hint i-1.
			if i > 0 && (r == nil || i > r.HintsRevealed) {
				break
			}
			for _, m := range ms {
				if !seen[m.ID] {
					seen[m.ID] = true
					out = append(out, m.ID)
				}
			}
		}
	}
	return out
}

// inputFor derives a typed answer's box from the matchers of a puzzle's
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
