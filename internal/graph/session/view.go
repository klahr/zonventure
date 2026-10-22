package session

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/look"
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
	// HereMe says whether the phone asking is inside Zone itself, and
	// HereCount how many of the team's players are: one teammate at the
	// place opens the step for all, but only they are told they are
	// there. Nil and zero for the team as a whole.
	HereMe    *bool `json:"here_me,omitempty"`
	HereCount int   `json:"here_count,omitempty"`

	// Starts are set while the team is choosing where to begin: Node is
	// empty, Type is "start", and the team picks one with ChooseStart.
	Starts []StartOption `json:"starts,omitempty"`
	// StartRequiresCode is true on the start screen when the adventure
	// has a start code: the client shows a code field and sends it with
	// the begin action. The clock only starts once the code is accepted.
	StartRequiresCode bool `json:"start_requires_code,omitempty"`

	// Blocks are what a puzzle, story or end shows, in the author's
	// order: texts, and pictures and sounds the client fetches from
	// /media/{id}.
	Blocks []BlockView `json:"blocks,omitempty"`
	// Quiz is how the team did on the quiz walk, once it has finished.
	Quiz *QuizResult `json:"quiz,omitempty"`
	// RecheckAt is when a hidden block could appear with no action from
	// the team; the phone asks again then. WatchPosition says one waits
	// for the team to reach a zone, so the phone reports its position
	// often.
	RecheckAt     *time.Time `json:"recheck_at,omitempty"`
	WatchPosition bool       `json:"watch_position,omitempty"`
	// Stations says the zones are cards to scan, not places to reach:
	// the phone offers a scanner where it would show the way, and never
	// asks where it is. See graph.Graph.Stations.
	Stations bool `json:"stations,omitempty"`
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

	// Groups are set while the team is split up (graph.SplitConfig):
	// every group, where it is and whether it waits at the join. Group is
	// the name of the one this view is for — on a split, the group the
	// phone goes with.
	Groups []GroupView `json:"groups,omitempty"`
	Group  string      `json:"group,omitempty"`
	// Split is the split the groups took (its slug), and SplitAt when the
	// team came to it: a group of another split, or of another time at
	// this one, though its path be named the same, is not one of these.
	Split   string     `json:"split,omitempty"`
	SplitAt *time.Time `json:"split_at,omitempty"`
	// Awaiting is, at a join, how many of the groups it waits for are
	// still on their way.
	Awaiting int `json:"awaiting,omitempty"`

	// Together is how far the players are with a puzzle they do
	// together: keys turned, players agreed.
	Together *Together `json:"together,omitempty"`

	// Languages the adventure can be played in, main first, and the one
	// this view is in — for the phone's language switch.
	Languages []string `json:"languages,omitempty"`
	Lang      string   `json:"lang,omitempty"`
	// Meta is the adventure's own title, intro, outro and meeting point
	// (start_label) in the team's language, where they are translated —
	// they live on the adventure, not in the graph, so the caller swaps
	// them in.
	Meta map[string]string `json:"meta,omitempty"`

	// Roles are the adventure's roles, in the team's language. Who holds
	// each is the player endpoints' to add: the team's state does not
	// know its players.
	Roles []RoleView `json:"roles,omitempty"`
	// RolesChosen says every player playing has chosen a role, or every
	// role is chosen: a roles block's step can be left. The endpoints fill
	// it; it is absent where no one chooses, as in the simulator.
	RolesChosen *bool `json:"roles_chosen,omitempty"`
	// Reveal unmasks the secret roles once the team has finished.
	Reveal []RevealView `json:"reveal,omitempty"`

	// EndsAt is when the adventure's time limit falls, for a countdown.
	// TimedOut says it has moved the team, and TimeUpText is the author's
	// message for that — empty for the phone's own "time is up".
	EndsAt     *time.Time `json:"ends_at,omitempty"`
	TimedOut   bool       `json:"timed_out,omitempty"`
	TimeUpText string     `json:"time_up_text,omitempty"`

	// MovedOn says the organisers moved the team onto the step shown — a
	// split team's group the view is for (Cursor.Moved) — and
	// MovedByOther another phone than the one the view is for did
	// (Cursor.MovedBy): whose is no one's to know, a saboteur's say.
	MovedOn      bool `json:"moved_on,omitempty"`
	MovedByOther bool `json:"moved_by_other,omitempty"`

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

	Score        float64 `json:"score"`
	PenaltyScore float64 `json:"penalty_score"`
	// HideScore is graph.HideScore: the phone leaves the score out of its header.
	HideScore bool `json:"hide_score,omitempty"`
	// PlayersIn and PlayersMin are set while the team waits for players:
	// how many have joined, of the adventure's least.
	PlayersIn  int `json:"players_in,omitempty"`
	PlayersMin int `json:"players_min,omitempty"`
	// Tour is graph.Tour: the phone words itself for a walk.
	Tour bool `json:"tour,omitempty"`
	// ResultsHeld says the game keeps its results until the host finishes
	// it: the play engine leaves out the score, quiz walk and standings.
	ResultsHeld    bool          `json:"results_held,omitempty"`
	PenaltySeconds int           `json:"penalty_seconds"`
	Finished       bool          `json:"finished,omitempty"`
	Outcome        graph.Outcome `json:"outcome,omitempty"`
	Items          []ItemView    `json:"items,omitempty"`
	Journal        []JournalView `json:"journal,omitempty"`
	// HasJournal says the adventure keeps a journal at all, so the
	// player shows its button from the start, empty until notes arrive.
	HasJournal bool   `json:"has_journal,omitempty"`
	Theme      string `json:"theme,omitempty"`
	// ThemeLook is the look of a saved theme Theme names, or of the main
	// theme while none is on, which replaces the adventure's own; empty
	// for a built-in theme the phone knows.
	ThemeLook *look.Look `json:"theme_look,omitempty"`

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
	// can offer the number pad, or the author asked for it; otherwise
	// "text". A way of answering other than typing has its own kind.
	Kind string `json:"kind"`
	// Parts is set when the answer is a list: how many items to give.
	Parts int `json:"parts,omitempty"`
	// Ordered says the list must be in the right order.
	Ordered bool `json:"ordered,omitempty"`

	// Wheels, Symbols and Start describe a combination lock (Kind "lock").
	Wheels  int                `json:"wheels,omitempty"`
	Symbols []graph.LockSymbol `json:"symbols,omitempty"`
	Start   []int              `json:"start,omitempty"`

	// Cards are an order puzzle's cards (Kind "order"), as dealt: the
	// phone answers with their positions here, in the team's order.
	Cards []graph.OrderCard `json:"cards,omitempty"`

	// Slots are an item puzzle's slots (Kind "items"), by label; the
	// items they want stay on the server.
	Slots []string `json:"slots,omitempty"`
	// Missing says the team lacks an item the slots need, and
	// MissingItems names them when the author chose to (graph.MissingName).
	// Leave offers the puzzle's OnMissing route meanwhile; LeaveLabel is
	// that route's label, empty for the phone's own words.
	Missing      bool     `json:"missing,omitempty"`
	MissingItems []string `json:"missing_items,omitempty"`
	Leave        bool     `json:"leave,omitempty"`
	LeaveLabel   string   `json:"leave_label,omitempty"`

	// Fields are the questions of a choice (Kind "choice"): labels and
	// options only — the answers stay on the server.
	Fields []ChoiceFieldView `json:"fields,omitempty"`

	// By names the role whose player alone answers, and NotYours says
	// the player asking does not hold it: the phone shows whose it is in
	// place of the answer box.
	By       string `json:"by,omitempty"`
	NotYours bool   `json:"not_yours,omitempty"`

	// Posts are a two-key launch's posts (Kind "keys"), by name: where
	// the keys are turned, one player at each. Empty turns them anywhere.
	Posts []string `json:"posts,omitempty"`
	// PostRoles, beside Posts, names the role whose player turns the key
	// at each — "" for anyone's — and MyPosts says which the player asking
	// may turn theirs at. Both empty when no post is a role's.
	PostRoles []string `json:"post_roles,omitempty"`
	MyPosts   []bool   `json:"my_posts,omitempty"`
	// PostsHeld says, beside Posts, which have their player standing at
	// them now — "the Mechanic is in place" — when posts are roles'.
	PostsHeld []bool `json:"posts_held,omitempty"`

	// At is, for a search (Kind "search"), the zone the team stands in —
	// "sector C3" — or empty when it is in none of them.
	At string `json:"at,omitempty"`

	// Rows, Cols and Image describe a tile puzzle (Kind "tiles"): the
	// picture to cut up and the grid to cut it into.
	Rows  int          `json:"rows,omitempty"`
	Cols  int          `json:"cols,omitempty"`
	Image *graph.Media `json:"image,omitempty"`

	// Placeholder, Button and NoScan are the author's answer bar (see
	// graph.AnswerBar): its words, and no QR button. Button also labels
	// a scan's (Kind "scan") one button.
	Placeholder string `json:"placeholder,omitempty"`
	Button      string `json:"button,omitempty"`
	NoScan      bool   `json:"no_scan,omitempty"`
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

// BlockView is a block as the player sees it, with what the phone
// cannot work out itself: the zone a compass, map or radar shows, when a
// countdown ends, a status's counter values, and — filled by the play
// engine, which can see other teams — a standings block's standings.
//
// A block whose show condition does not hold is left out, or with Tease
// is sent Locked with only its id and kind (and Place, when it waits for
// a zone). Show itself is never sent: the condition is the author's.
type BlockView struct {
	graph.Block
	Place *graph.Zone `json:"place,omitempty"`
	// Here is a compass's and a mission's: whether the phone asking is
	// inside its zone,
	// by the same positions an answer is judged on, not the phone's own.
	Here          *bool         `json:"here,omitempty"`
	Locked        bool          `json:"locked,omitempty"`
	EndsAt        *time.Time    `json:"ends_at,omitempty"`
	CounterValues []CounterView `json:"counter_values,omitempty"`
	Standings     *Standings    `json:"standings,omitempty"`
	// Points is what a points block's puzzle is worth: its best answer.
	Points float64 `json:"points,omitempty"`
	// Picked is the team's answer to a quiz question or tie-breaker, and
	// Final whether it can no longer change it: it has left the step. The
	// right Answer and Target stay out of the view until it finishes.
	// Picked is also the cards a suspect board has struck ("0,2"), and
	// the index of the card a draw dealt.
	Picked string `json:"picked,omitempty"`
	Final  bool   `json:"final,omitempty"`
	// Deck is how many cards a draw holds; they stay face down, out of
	// the view, and Dealt is the one the team drew.
	Deck  int    `json:"deck,omitempty"`
	Dealt string `json:"dealt,omitempty"`
	// Seals is how many players have picked at a mission, of Need, and
	// Sealed whether the phone asking has: never what anyone picked.
	// A sabotage's Sealed is whether this phone pressed it.
	Seals  int  `json:"seals,omitempty"`
	Need   int  `json:"need,omitempty"`
	Sealed bool `json:"sealed,omitempty"`
	// Mine is whether a task is the phone asking's to do; its Picked is
	// "1" once it is done.
	Mine bool `json:"mine,omitempty"`

	// The rest is filled by the player endpoints, which know the player
	// asking. Player is which of Players a split secret's phone is,
	// counted from 1, and Parts are only that player's.
	Player  int `json:"player,omitempty"`
	Players int `json:"players,omitempty"`
	// Tally counts a vote's votes per option, Voted is the option this
	// player chose ("" for none yet), and Voters how many players the
	// team has.
	Tally  []int  `json:"tally,omitempty"`
	Voted  string `json:"voted,omitempty"`
	Voters int    `json:"voters,omitempty"`
	// Suspects are whom an accusation may name: the player's teammates,
	// in Tally's order; Voted is then the slot they named.
	Suspects []Suspect `json:"suspects,omitempty"`
	// Photo is the team's latest picture for a photo block.
	Photo *PhotoView `json:"photo,omitempty"`
}

// SplitParts is what player i of n sees of a split secret: the parts
// dealt round to them, or — with more players than parts — the part
// their turn comes round to.
func SplitParts(parts []string, i, n int) []string {
	if n <= 1 || len(parts) == 0 {
		return parts
	}
	if n > len(parts) {
		return []string{parts[i%len(parts)]}
	}
	var out []string
	for j, p := range parts {
		if j%n == i {
			out = append(out, p)
		}
	}
	return out
}

// Suspect is a teammate an accusation may name.
type Suspect struct {
	Slot int64  `json:"slot"`
	Name string `json:"name"`
}

// PhotoView is a picture a team took for a photo block, and where the
// organisers' review of it stands: "pending", "approved" or "denied".
type PhotoView struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

// CounterView is one counter's value, for a status block.
type CounterView struct {
	Slug  string  `json:"slug"`
	Name  string  `json:"name,omitempty"`
	Value float64 `json:"value"`
}

// Standings is the team's place in its event: Rank of Of, and the teams
// just ahead and behind with the gap in points.
type Standings struct {
	Rank   int    `json:"rank"`
	Of     int    `json:"of"`
	Ahead  *Rival `json:"ahead,omitempty"`
	Behind *Rival `json:"behind,omitempty"`
}

// Rival is a neighbouring team in the standings.
type Rival struct {
	Name string  `json:"name"`
	Gap  float64 `json:"gap"`
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
	// Role names the role whose kit the item is from, and NotMine says
	// the player asking does not hold it, so cannot use it.
	Role    string `json:"role,omitempty"`
	NotMine bool   `json:"not_mine,omitempty"`
	// HolderSlot is the player the item was handed to, if it was, and
	// Holder their name, which the player endpoints fill. Give says the
	// player asking may hand it on.
	HolderSlot int64  `json:"-"`
	Holder     string `json:"holder,omitempty"`
	Give       bool   `json:"give,omitempty"`
}

// JournalView is one unlocked journal entry.
// RoleView is one of the adventure's roles. Mine and Holder are filled
// by the player endpoints: whether the phone asking holds it, and the
// name of the player who does; Claimed says they chose it, rather than
// being dealt it.
type RoleView struct {
	Slug string `json:"slug"`
	// Secret is a saboteur's role, listed only to its player — and to
	// the team as a whole.
	Secret bool `json:"secret,omitempty"`
	// Default is the role everyone without another holds: never dealt
	// or claimed, so it has no one holder.
	Default bool         `json:"default,omitempty"`
	Name    string       `json:"name,omitempty"`
	Text    string       `json:"text,omitempty"`
	Image   *graph.Media `json:"image,omitempty"`
	Mine    bool         `json:"mine,omitempty"`
	Holder  string       `json:"holder,omitempty"`
	Claimed bool         `json:"claimed,omitempty"`
	// Final says the team's roles are locked: no one takes or gives one back.
	Final bool `json:"final,omitempty"`

	// cover is a secret role's cover, shown in its place to a player who
	// does not hold it: its Name is the cover's.
	cover bool
}

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
func (t *Team) setBlocks(v *View, n graph.Node) {
	bs := n.Blocks()
	v.Blocks = make([]BlockView, 0, len(bs))
	var shown []graph.Block
	for _, b := range bs {
		if !t.roleShows(b) {
			// Not a teaser either: an accusation with no one to name is not there.
			continue
		}
		if b.Show != nil && !cond.Eval(b.Show.When, t) {
			t.watch(v, b.Show.When)
			if b.Show.Tease {
				bv := BlockView{Block: graph.Block{ID: b.ID, Kind: b.Kind}, Locked: true}
				if z := waitsForZone(b.Show.When); z != "" {
					bv.Place = t.zoneBySlug(z)
				}
				v.Blocks = append(v.Blocks, bv)
			}
			continue
		}
		b.Show = nil
		b.Text = t.fill(b.Text)
		shown = append(shown, b)
		bv := BlockView{Block: b}
		switch b.Kind {
		case graph.BlockCompass, graph.BlockMap, graph.BlockRadar:
			bv.Place = t.zoneBySlug(b.Zone)
			if b.Kind == graph.BlockCompass && bv.Place != nil {
				here := t.Here(b.Zone)
				bv.Here = &here
			}
		case graph.BlockCountdown:
			from := t.nodeClock()
			if b.From == "start" {
				from = t.startClock()
			}
			end := from.Add(time.Duration(b.Seconds) * time.Second)
			bv.EndsAt = &end
		case graph.BlockQuiz, graph.BlockTiebreak:
			bv.Picked, bv.Final = t.Picks[PickKey(n.Slug, b.ID)], t.PickFinal(n.Slug)
			if !t.Completed {
				bv.Answer, bv.Target = "", nil
			}
		case graph.BlockSuspects:
			bv.Picked = t.Picked(n.Slug, b.ID)
		case graph.BlockDraw:
			bv.Deck, bv.Options = len(b.Options), nil
			if p := t.Picked(n.Slug, b.ID); p != "" {
				if i, err := strconv.Atoi(p); err == nil && i >= 0 && i < len(b.Options) {
					bv.Picked, bv.Dealt = p, b.Options[i]
				}
			}
		case graph.BlockSabotage:
			// The same on every phone: whether this one pressed, never
			// whether it did anything, nor which note it plants.
			bv.Journal = ""
			if t.Viewer != nil {
				_, bv.Sealed = t.Seals[PickKey(n.Slug, b.ID)][t.Viewer.Slot]
			} else {
				bv.Sealed = t.Picks[PickKey(n.Slug, b.ID)] != ""
			}
		case graph.BlockTask:
			// Whose it is and whether it is done, never its answer.
			bv.Answer = ""
			bv.Picked = t.Picks[PickKey(n.Slug, b.ID)]
			bv.Mine = t.mayTask(b)
			if b.Zone != "" {
				bv.Place = t.zoneBySlug(b.Zone)
				here := t.Here(b.Zone)
				bv.Here = &here
				if bv.Mine && bv.Picked == "" && !t.G.Stations {
					v.WatchPosition = true
				}
			}
		case graph.BlockMission:
			picks := t.Seals[PickKey(n.Slug, b.ID)]
			bv.Place, bv.Seals, bv.Need = t.zoneBySlug(b.Zone), len(picks), t.sealsNeeded(b)
			here := t.Here(b.Zone)
			bv.Here = &here
			if t.Viewer != nil {
				_, bv.Sealed = picks[t.Viewer.Slot]
			}
			if !bv.Sealed && bv.Seals < bv.Need && !t.G.Stations {
				v.WatchPosition = true
			}
		case graph.BlockTitle:
			bv.Text = n.Title
		case graph.BlockPoints:
			if n.PuzzleCfg != nil {
				for _, o := range n.PuzzleCfg.Outputs {
					if o.Trigger == graph.OnAnswer {
						bv.Points = max(bv.Points, float64(o.Points))
					}
				}
			}
		case graph.BlockStatus:
			for _, c := range b.Counters {
				cv := CounterView{Slug: c, Value: t.CounterValue(c)}
				for _, def := range t.G.Counters {
					if def.Slug == c {
						cv.Name = def.Name
					}
				}
				bv.CounterValues = append(bv.CounterValues, cv)
			}
		}
		v.Blocks = append(v.Blocks, bv)
	}
	v.Text, v.Media = graph.TextOf(shown), legacyMedia(shown)
}

// legacyMedia is a step's media for a player from before blocks, which
// shows anything that is not a picture as a sound: no videos.
func legacyMedia(bs []graph.Block) []graph.Media {
	var out []graph.Media
	for _, m := range graph.MediaOf(bs) {
		if m.Kind != graph.MediaVideo {
			out = append(out, m)
		}
	}
	return out
}

// HidePlaces takes the hidden zones out of a view bound for a team's
// phone, keeping only that there is a place to find: the engine's own
// view keeps them whole, for the simulator and the designer.
func (v *View) HidePlaces() {
	hide := func(z *graph.Zone) *graph.Zone {
		switch {
		case z == nil:
			return nil
		case z.Hidden:
			return &graph.Zone{Slug: z.Slug, Hidden: true}
		case v.Stations:
			// A station is a card, named on it: where the author drew the
			// zone is no help, and not the team's to know.
			return &graph.Zone{Slug: z.Slug, Name: z.Name}
		}
		return z
	}
	v.Zone = hide(v.Zone)
	for i := range v.Starts {
		v.Starts[i].Zone = hide(v.Starts[i].Zone)
	}
	for i := range v.Blocks {
		v.Blocks[i].Place = hide(v.Blocks[i].Place)
	}
}

func (t *Team) zoneBySlug(slug string) *graph.Zone {
	for _, z := range t.G.Zones {
		if z.Slug == slug {
			zc := z
			return &zc
		}
	}
	return nil
}

// blockShown reports whether the team at its current step sees a block.
// A block with a show condition on any other step is hidden: its file
// comes with it, never ahead of it.
func (t *Team) blockShown(node string, b graph.Block) bool {
	if !t.roleShows(b) {
		return false
	}
	return b.Show == nil || node == t.Current && cond.Eval(b.Show.When, t)
}

// watch tells the phone when a hidden block's condition could next come
// true without the team doing anything: at a time (RecheckAt), or when
// it moves (WatchPosition).
func (t *Team) watch(v *View, p cond.Predicate) {
	at, zone := t.wakeup(p)
	if zone && !t.G.Stations {
		v.WatchPosition = true
	}
	if at != nil && at.After(t.Now) && (v.RecheckAt == nil || at.Before(*v.RecheckAt)) {
		v.RecheckAt = at
	}
}

// recheckEvery is how often a phone re-asks about a clock condition
// whose moment cannot be worked out ahead, such as one inside a not.
const recheckEvery = 15 * time.Second

func (t *Team) wakeup(p cond.Predicate) (at *time.Time, zone bool) {
	later := func(from time.Time) *time.Time {
		d := from.Add(time.Duration(p.N * float64(time.Second)))
		if !d.After(t.Now) {
			d = t.Now.Add(recheckEvery)
		}
		return &d
	}
	switch p.Op {
	case cond.OpInZone, cond.OpPlayersIn, cond.OpZonesHeld, cond.OpHere, cond.OpWalked:
		return nil, true
	case cond.OpNodeElapsed:
		return later(t.nodeClock()), false
	case cond.OpElapsed:
		return later(t.startClock()), false
	case cond.OpTimeOfDay, cond.OpWeekday:
		d := t.Now.Add(recheckEvery)
		return &d, false
	case cond.OpAll, cond.OpAny, cond.OpNot:
		for _, q := range p.Of {
			a, z := t.wakeup(q)
			zone = zone || z
			if a != nil && (at == nil || a.Before(*at)) {
				at = a
			}
		}
	}
	return at, zone
}

// waitsForZone is the zone a condition is simply "inside", for a teaser
// to name; "" for anything more involved.
func waitsForZone(p cond.Predicate) string {
	if p.Op == cond.OpInZone || p.Op == cond.OpHere {
		return p.Zone
	}
	if p.Op == cond.OpAll && len(p.Of) == 1 {
		return waitsForZone(p.Of[0])
	}
	return ""
}

// tokens are the team's state an author may write into a text:
// {{score}}, {{counter:<slug>}}, {{item:<slug>}}, the answer the team
// gave at a step, {{answer:<slug>}}, and a party host's words,
// {{host:<slug>}} (graph.HostValues). {{team}} is left for the phone,
// which knows the team's name.
var tokens = regexp.MustCompile(`\{\{\s*(score|counter:[\w-]+|item:[\w-]+|answer:[\w-]+|host:[\w-]+)\s*\}\}`)

func (t *Team) fill(s string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	return tokens.ReplaceAllStringFunc(s, func(m string) string {
		name := strings.TrimSpace(strings.Trim(m, "{}"))
		switch {
		case name == "score":
			return number(t.Score())
		case strings.HasPrefix(name, "counter:"):
			return number(t.CounterValue(strings.TrimPrefix(name, "counter:")))
		case strings.HasPrefix(name, "host:"):
			v, _ := t.G.HostValue(strings.TrimPrefix(name, "host:"))
			return v
		case strings.HasPrefix(name, "answer:"):
			v, _ := t.AnswerTo(strings.TrimPrefix(name, "answer:"))
			return strings.TrimSpace(v)
		default:
			return strconv.Itoa(t.ItemQty(strings.TrimPrefix(name, "item:")))
		}
	})
}

func number(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// View renders the team's state for the player. A split team's view is
// that of the group the phone asking is walking (focusView).
func (t *Team) View() View {
	// A split team is shown as the group the view is for, and left
	// unfocused: the focus is the view's, not the caller's.
	t.focusView()
	defer t.unfocus()
	v := t.view()
	v.Stations = t.G.Stations
	for _, r := range t.G.Roles {
		if r.Secret && !t.HasRole(r.Slug) {
			// Its cover, where it has one: every other player's, so the
			// roles read the same on every phone.
			if r.Cover != "" && t.Viewer != nil && t.secretHeld(r.Slug) {
				v.Roles = append(v.Roles, RoleView{Slug: r.Slug, Name: r.Cover, Secret: true, Mine: true, cover: true})
			}
			continue
		}
		rv := RoleView{Slug: r.Slug, Name: r.Name, Text: r.Text, Image: r.Image, Secret: r.Secret, Default: r.Default && !r.Secret}
		if r.Secret {
			// Its player knows; who else holds what is the endpoints' to say.
			rv.Mine = t.Viewer != nil
		}
		v.Roles = append(v.Roles, rv)
	}
	v.Reveal = t.reveal()
	if t.tooFew() {
		v.PlayersIn, v.PlayersMin = len(t.everyone()), t.G.MinPlayers
	}
	t.translate(&v)
	return v
}

// translate puts a view into the language of the player it is for. The walk and the view
// are built in the adventure's main language; this is the only place the
// translations are read, and anything without one stays as it was.
func (t *Team) translate(v *View) {
	// Only a translated adventure has languages to choose between; any
	// other still names its own, so the phone speaks the adventure's.
	if v.Languages = t.G.Languages(); v.Languages != nil {
		v.Lang = t.lang()
		if v.Lang == "" {
			v.Lang = v.Languages[0]
		}
	} else {
		v.Lang = t.G.Language
	}
	l := t.lang()
	if l == "" || l == t.G.Language {
		return
	}
	g := t.G
	tr := func(key, main string) string { return g.Tr(l, key, main) }
	for _, f := range []string{"title", "intro", "outro", "start_label"} {
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
		if z.Slug == StartPlaceSlug {
			// The adventure's starting point, named by its meeting point.
			c.Name = tr(graph.MetaKey("start_label"), z.Name)
		} else {
			c.Name = tr(graph.ZoneKey(z.Slug), z.Name)
		}
		return &c
	}
	for i := range v.Starts {
		s := &v.Starts[i]
		s.Title = tr(graph.NodeKey(s.Node, "title"), s.Title)
		s.Zone = zone(s.Zone)
	}
	media := func(owner string, m *graph.Media) *graph.Media {
		if m == nil {
			return nil
		}
		c := g.TrMedia(l, owner, *m)
		return &c
	}
	medias := func(owner func(i int) string, ms []graph.Media) []graph.Media {
		if ms == nil {
			return nil
		}
		out := make([]graph.Media, len(ms))
		for i, m := range ms {
			out[i] = g.TrMedia(l, owner(i), m)
		}
		return out
	}
	v.Roles = append([]RoleView(nil), v.Roles...)
	for i := range v.Roles {
		r := &v.Roles[i]
		name := "name"
		if r.cover {
			name = "cover"
		}
		r.Name = tr(graph.RoleKey(r.Slug, name), r.Name)
		r.Text = tr(graph.RoleKey(r.Slug, "text"), r.Text)
		r.Image = media(graph.RoleKey(r.Slug, "image"), r.Image)
	}
	v.Journal = append([]JournalView(nil), v.Journal...)
	for i := range v.Journal {
		j := &v.Journal[i]
		j.Title = tr(graph.JournalKey(j.Slug, "title"), j.Title)
		j.Body = tr(graph.JournalKey(j.Slug, "body"), j.Body)
		if j.Section != "" {
			j.Section = tr(graph.SectionKey(j.Section), j.Section)
		}
		j.Media = medias(func(i int) string { return graph.JournalKey(j.Slug, fmt.Sprintf("media:%d", i)) }, j.Media)
	}
	v.Items = append([]ItemView(nil), v.Items...)
	for i := range v.Items {
		if it := &v.Items[i]; it.Name != "" {
			it.Name = tr(graph.ItemKey(it.Slug, "name"), it.Name)
		}
	}
	if th := t.lookTheme(); th != nil && v.ThemeLook != nil && v.ThemeLook.Style != nil {
		lk, st := *v.ThemeLook, *v.ThemeLook.Style
		if st.CheerText != "" {
			st.CheerText = tr(graph.ThemeKey(th.Slug, "cheer_text"), st.CheerText)
		}
		if st.MissText != "" {
			st.MissText = tr(graph.ThemeKey(th.Slug, "miss_text"), st.MissText)
		}
		lk.Style = &st
		v.ThemeLook = &lk
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
		plain := make([]graph.Block, 0, len(v.Blocks))
		for i := range v.Blocks {
			b := &v.Blocks[i]
			switch {
			case b.Kind == graph.BlockTitle:
				b.Text = v.Title // translated above
			case b.Text != "":
				b.Text = t.fill(tr(graph.BlockKey(v.Node, b.ID), b.Text))
			}
			if b.Lines != nil {
				b.Lines = append([]graph.ChatLine(nil), b.Lines...)
				for j := range b.Lines {
					l := &b.Lines[j]
					l.Text = t.fill(tr(graph.BlockFieldKey(v.Node, b.ID, fmt.Sprintf("line:%d", j)), l.Text))
					if l.Who != "" {
						l.Who = tr(graph.BlockFieldKey(v.Node, b.ID, fmt.Sprintf("who:%d", j)), l.Who)
					}
				}
			}
			if b.Options != nil {
				b.Options = append([]string(nil), b.Options...)
				for j := range b.Options {
					b.Options[j] = tr(graph.BlockFieldKey(v.Node, b.ID, fmt.Sprintf("option:%d", j)), b.Options[j])
				}
			}
			if b.Markers != nil {
				b.Markers = append([]graph.Marker(nil), b.Markers...)
				for j := range b.Markers {
					if b.Markers[j].Label != "" {
						b.Markers[j].Label = tr(graph.BlockFieldKey(v.Node, b.ID, fmt.Sprintf("marker:%d", j)), b.Markers[j].Label)
					}
				}
			}
			if b.Spots != nil {
				b.Spots = append([]graph.Spot(nil), b.Spots...)
				for j := range b.Spots {
					b.Spots[j].Text = tr(graph.BlockFieldKey(v.Node, b.ID, fmt.Sprintf("spot:%d", j)), b.Spots[j].Text)
				}
			}
			if b.Parts != nil {
				b.Parts = append([]string(nil), b.Parts...)
				for j := range b.Parts {
					b.Parts[j] = t.fill(tr(graph.BlockFieldKey(v.Node, b.ID, fmt.Sprintf("part:%d", j)), b.Parts[j]))
				}
			}
			if b.Who != "" {
				b.Who = tr(graph.BlockFieldKey(v.Node, b.ID, "who"), b.Who)
			}
			if b.Alt != "" {
				b.Alt = tr(graph.AltKey(graph.BlockKey(v.Node, b.ID)), b.Alt)
			}
			if b.CounterValues != nil {
				b.CounterValues = append([]CounterView(nil), b.CounterValues...)
				for j := range b.CounterValues {
					if c := &b.CounterValues[j]; c.Name != "" {
						c.Name = tr(graph.CounterKey(c.Slug, "name"), c.Name)
					}
				}
			}
			if b.Cards != nil {
				b.Cards = append([]graph.Card(nil), b.Cards...)
				for j := range b.Cards {
					c := &b.Cards[j]
					c.Name = tr(graph.BlockFieldKey(v.Node, b.ID, fmt.Sprintf("card:%d:name", j)), c.Name)
					if c.Text != "" {
						c.Text = t.fill(tr(graph.BlockFieldKey(v.Node, b.ID, fmt.Sprintf("card:%d:text", j)), c.Text))
					}
					if c.Alt != "" {
						c.Alt = tr(graph.AltKey(graph.BlockFieldKey(v.Node, b.ID, fmt.Sprintf("card:%d", j))), c.Alt)
					}
				}
			}
			if b.Dealt != "" {
				b.Dealt = tr(graph.BlockFieldKey(v.Node, b.ID, "option:"+b.Picked), b.Dealt)
			}
			b.Place = zone(b.Place)
			if !b.Locked {
				plain = append(plain, b.Block)
			}
		}
		v.Text, v.Media = graph.TextOf(plain), legacyMedia(plain)
	}
	v.Zone = zone(v.Zone)
	if v.Quiz != nil {
		q := *v.Quiz
		q.Items = append([]QuizRow(nil), q.Items...)
		for i := range q.Items {
			r := &q.Items[i]
			r.Question = tr(graph.BlockKey(r.Node, r.Block), r.Question)
			r.Options = append([]string(nil), r.Options...)
			for j := range r.Options {
				r.Options[j] = tr(graph.BlockFieldKey(r.Node, r.Block, fmt.Sprintf("option:%d", j)), r.Options[j])
			}
		}
		if q.Tiebreak != nil {
			tb := *q.Tiebreak
			tb.Question = tr(graph.BlockKey(tb.Node, tb.Block), tb.Question)
			q.Tiebreak = &tb
		}
		v.Quiz = &q
	}
	v.Hints = append([]HintView(nil), v.Hints...)
	for i := range v.Hints {
		h := &v.Hints[i]
		h.Text = tr(key(fmt.Sprintf("hint:%d", i)), h.Text)
		h.Media = medias(func(j int) string { return key(fmt.Sprintf("hint:%d:media:%d", i, j)) }, h.Media)
	}
	v.Offer = append([]OfferEntry(nil), v.Offer...)
	for i := range v.Offer {
		o := &v.Offer[i]
		o.Label = tr(key("branch:"+o.Port+":label"), o.Label)
		o.Note = tr(key("branch:"+o.Port+":note"), o.Note)
		o.Image = media(key("branch:"+o.Port), o.Image)
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
		if in.Placeholder != "" {
			c.Placeholder = tr(key("bar:placeholder"), in.Placeholder)
		}
		if in.Button != "" {
			c.Button = tr(key("bar:button"), in.Button)
		}
		if len(in.Slots) > 0 {
			c.Slots = make([]string, len(in.Slots))
			for i, l := range in.Slots {
				c.Slots[i] = tr(key(fmt.Sprintf("slot:%d", i)), l)
			}
		}
		if len(in.Symbols) > 0 {
			c.Symbols = make([]graph.LockSymbol, len(in.Symbols))
			for i, s := range in.Symbols {
				s.Label = tr(key("sym:"+s.Value), s.Label)
				s.Image = media(key("sym:"+s.Value), s.Image)
				c.Symbols[i] = s
			}
		}
		c.Image = media(key("image"), in.Image)
		v.Input = &c
	}
}

// lookTheme is the saved theme whose look the team sees: the one a theme
// effect switched to, or else the main theme of the version the team
// plays, not the adventure's look of the latest. An empty main theme
// leaves the organisation's look to show, and so does none.
func (t *Team) lookTheme() *graph.Theme {
	if th := t.G.Theme(t.Theme); th != nil {
		return th
	}
	if th := t.G.Theme(t.G.MainTheme); th != nil && (t.Theme == "" || t.Theme == "default") && !th.Look.Empty() {
		return th
	}
	return nil
}

func (t *Team) view() View {
	v := View{
		Node:           t.Current,
		Score:          t.Points,
		PenaltyScore:   t.PenaltyScore,
		HideScore:      t.G.HideScore,
		Tour:           t.G.Tour,
		PenaltySeconds: t.PenaltySeconds,
		Finished:       t.Completed,
		Outcome:        t.Ending,
		Theme:          t.Theme,
		Now:            t.Now,
	}
	if th := t.lookTheme(); th != nil {
		v.ThemeLook = &th.Look
	}
	for _, slug := range sortedKeys(t.Items) {
		if q := t.Items[slug]; q > 0 {
			iv := ItemView{Slug: slug, Qty: q}
			for _, it := range t.G.Items {
				if it.Slug == slug {
					iv.Name, iv.Icon = it.Name, it.Icon
				}
			}
			if role, ok := t.G.ItemRole(slug); ok {
				iv.Role = t.G.Tr(t.lang(), graph.RoleKey(role, "name"), t.G.RoleName(role))
			}
			iv.HolderSlot = t.Holders[slug]
			iv.NotMine = !t.CanUse(slug)
			iv.Give = t.Giveable(slug)
			v.Items = append(v.Items, iv)
		}
	}
	v.HasJournal = len(t.G.Journal) > 0
	v.Journal = t.journalView()

	// Show the start screen while the team waits to begin — when the
	// adventure lets them choose a start, or when a start code or the
	// starting point gates the begin (which holds even a single-start
	// adventure here instead of auto-advancing). StartRequiresCode tells
	// the client to ask for the code.
	if t.Current == "" && !t.Completed && t.HoldsAtStart() {
		v.Type = graph.Start
		v.StartRequiresCode = t.StartCode != ""
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
			// The adventure's starting point is where a start without a
			// zone of its own is, and is required of every start.
			if t.StartPlace != nil {
				if o.Zone == nil {
					zc := *t.StartPlace
					o.Zone = &zc
				}
				o.Here = o.Here && t.AtStartPlace()
			}
			v.Starts = append(v.Starts, o)
		}
		return v
	}

	if t.Split != "" {
		v.Groups, v.Split = t.groups(), t.Split
		// The team's own cursor, on the split since it came there — to the
		// second, as it reads back from the table as it was in memory.
		at := t.Cursor.NodeEnteredAt
		if t.saved != nil {
			at = t.saved.home.NodeEnteredAt
		}
		at = at.Truncate(time.Second)
		v.SplitAt = &at
		for _, g := range v.Groups {
			if g.Current {
				v.Group = g.Label
			}
		}
	}

	n, ok := t.G.Node(t.Current)
	if !ok {
		return v
	}
	v.Type, v.Title = n.Type, n.Title
	v.MovedOn = t.Moved
	v.MovedByOther = t.movedByOther(t.MovedBy)
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
				if t.Viewer != nil {
					// A team of one has no teammate: where the team is, they are,
					// even when the fix that put it there is not their phone's.
					me := t.Here(slug) || v.Here && len(t.Players) == 1
					v.HereMe, v.HereCount = &me, t.PlayersInZone(slug)
				}
			}
		}
	}

	switch n.Type {
	case graph.Puzzle:
		cfg := n.PuzzleCfg
		if cfg == nil {
			return v
		}
		t.setBlocks(&v, n)
		if cfg.Zone != "" {
			setZone(cfg.Zone)
		}
		v.Input = t.answerInput(n)
		if cfg.By != "" && v.Input != nil {
			v.Input.By = t.G.Tr(t.lang(), graph.RoleKey(cfg.By, "name"), t.G.RoleName(cfg.By))
			v.Input.NotYours = !t.HasRole(cfg.By)
		}
		if v.Together = t.together(n); v.Together != nil && v.Together.Until != nil &&
			(v.RecheckAt == nil || v.Together.Until.Before(*v.RecheckAt)) {
			// A key stops counting then: the phone looks again to say so.
			at := v.Together.Until.Add(time.Second)
			v.RecheckAt = &at
		}
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
				AvailableAt: t.nodeClock().Add(time.Duration(h.DelaySeconds) * time.Second),
				Cost:        h.Cost,
			}
		}
		if cfg.TimeLimitSeconds > 0 && !t.NodeEnteredAt.IsZero() {
			d := t.nodeClock().Add(time.Duration(cfg.TimeLimitSeconds) * time.Second)
			v.Deadline = &d
			_, v.HasTimeout = outputFor(n, graph.OnTimeout)
		}
		v.CanSkip = t.canSkip(n)
	case graph.Story:
		if n.StoryCfg != nil {
			t.setBlocks(&v, n)
			if n.StoryCfg.Zone != "" {
				setZone(n.StoryCfg.Zone)
			}
		}
	case graph.End:
		if n.EndCfg != nil {
			t.setBlocks(&v, n)
		}
		if q := t.Quiz(); t.Completed && (q.Total > 0 || q.Tiebreak != nil) {
			v.Quiz = &q
		}
	case graph.Split:
		if n.SplitCfg != nil {
			v.Text = t.G.Tr(t.lang(), graph.NodeKey(n.Slug, "split-text"), n.SplitCfg.Text)
		}
	case graph.Join:
		v.Awaiting = t.awaited(n.Slug)
	case graph.Hub:
		// Recompute the offer: it is a function of state, not stored.
		if n.HubCfg != nil {
			for _, b := range n.HubCfg.Branches {
				if t.BranchDone(n.Slug, b.Port) && !n.HubCfg.Home {
					continue
				}
				open := t.branchOpen(b)
				if !open {
					// A branch waiting on where the players are, or on the
					// clock, opens without the team doing anything: the
					// phone should look again as it does for a hidden block.
					t.watch(&v, b.Condition)
				}
				if !open && n.HubCfg.HideLocked {
					continue
				}
				visited := t.BranchDone(n.Slug, b.Port)
				if n.HubCfg.Home {
					visited = t.BranchFinished(n.Slug, b.Port)
				}
				v.Offer = append(v.Offer, OfferEntry{Port: b.Port, Label: b.Label, Locked: !open, Visited: visited, Note: b.Note, Image: b.Image})
			}
			v.Text = n.HubCfg.Text
			v.CanFinish = !n.HubCfg.Home && t.CompletedBranches(n.Slug) >= n.HubCfg.RequiredBranches()
		}
	}
	return v
}

// ownMedia is a step's own media the team may see: its answer's, a
// hub's branch pictures, and its blocks' unless a block is still hidden.
func (t *Team) ownMedia(n graph.Node) []graph.Media {
	var shown []graph.Block
	for _, b := range n.Blocks() {
		if t.blockShown(n.Slug, b) {
			shown = append(shown, b)
		}
	}
	out := graph.MediaOf(shown)
	if n.PuzzleCfg != nil && n.PuzzleCfg.Answer != nil {
		out = append(out, n.PuzzleCfg.Answer.Media()...)
	}
	if n.HubCfg != nil {
		for _, br := range n.HubCfg.Branches {
			if br.Image != nil {
				out = append(out, *br.Image)
			}
		}
	}
	return out
}

// PrefetchMedia is VisibleMedia less videos: a phone downloads the rest
// ahead of need, but a film only when it is played.
func (t *Team) PrefetchMedia(strict bool) []int64 {
	videos := map[int64]bool{}
	for _, n := range t.G.Nodes {
		for _, m := range graph.MediaOf(n.Blocks()) {
			if m.Kind == graph.MediaVideo {
				videos[m.ID] = true
			}
		}
	}
	var out []int64
	for _, id := range t.VisibleMedia(strict) {
		if !videos[id] {
			out = append(out, id)
		}
	}
	return out
}

// CanSeeMedia reports whether the team may fetch a media file.
//
// Hint media is visible only once the hint is revealed, always — a hint
// the team has not paid for must not leak through its picture.
//
// Step media follows the adventure's strict-assets setting. Strict: only steps the team has reached, for puzzles
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
// in graph order. The player app downloads these ahead of need. A split
// team's are those of the group a view is for, as View shows it.
func (t *Team) VisibleMedia(strict bool) []int64 {
	if t.Split != "" && t.focus == 0 {
		t.focusView()
		defer t.unfocus()
	}
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
	// A saved theme's picture is part of how the adventure looks, never a
	// secret: a team may fetch it before any step switches to the theme.
	for _, th := range t.G.Themes {
		for _, id := range th.Look.MediaIDs() {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	// A role's picture comes with the role: a secret one only to its player.
	for _, ro := range t.G.Roles {
		if ro.Image == nil || ro.Secret && !t.HasRole(ro.Slug) {
			continue
		}
		if !seen[ro.Image.ID] {
			seen[ro.Image.ID] = true
			out = append(out, ro.Image.ID)
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
			if i == 0 {
				ms = t.ownMedia(n)
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

// HaversineMeters is the great-circle distance, R = 6371000.
func HaversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371000.0
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat, dLng := rad(lat2-lat1), rad(lng2-lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return r * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
