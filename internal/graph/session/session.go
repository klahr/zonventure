// Package session is one team playing an adventure graph, in memory.
//
// It holds every decision a player action involves: whether an answer
// matches, which output it takes, when a hint is due, whether a skip is
// allowed, what the player sees. internal/graph/walk decides where a team
// goes once it leaves a node; this package decides whether and how it
// leaves.
//
// Three callers run exactly this code, and that is the point of the
// package:
//
//   - internal/play, the live engine, loads a Team from Postgres, calls
//     an action, and persists what changed.
//   - The designer's preview places a fresh Team on the node being
//     edited and renders its View.
//   - The designer's simulator keeps a Team on the client between calls
//     and plays it through the unsaved draft.
//
// A preview or a simulation therefore cannot disagree with a real event
// about what counts as a right answer, when a hint opens or where a
// branch leads — there is no second implementation for it to drift from.
//
// Pure, like the rest of internal/graph: no database, no network, no
// clock. The time is Team.Now, set by the caller.
package session

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/walk"
)

var (
	// ErrStale means the team has moved since the client last looked:
	// the node it acted on is no longer current. Map to 409.
	ErrStale = errors.New("team has already moved on")
	// ErrNotHere means a zone gate is not satisfied. Map to 403.
	ErrNotHere = errors.New("not inside the required zone")
	// ErrNoFix means the action needs to know where the phone is, and
	// it has not said lately: a key turned at a post. Map to 403.
	ErrNoFix = errors.New("the phone's position is not known yet")
	// ErrNotYours means the action is another role's: a puzzle only the
	// Locksmith answers, an item in the Locksmith's kit. Map to 403.
	ErrNotYours = errors.New("that is another role's to do")
	// ErrLocked means answering is blocked after a wrong guess. Map to
	// 429 with Retry-After from Result.BlockedUntil.
	ErrLocked = errors.New("answering is temporarily locked")
	// ErrTimeUp means the puzzle's time limit has passed. The client
	// should call Timeout to take the timeout path, if there is one.
	ErrTimeUp = errors.New("time limit has passed")
	// ErrNotAllowed means the action is not available right now: a skip
	// whose thresholds are unmet, a hint not yet due, a timeout that has
	// not happened. Map to 403.
	ErrNotAllowed = errors.New("action not available")
	// ErrFinished means the team has already finished. Map to 409.
	ErrFinished = errors.New("team has finished")
	// ErrWrongNode means the action does not fit the node — answering a
	// story, choosing a branch at a puzzle. Map to 400.
	ErrWrongNode = errors.New("action does not apply to this node")
	// ErrUnanswered means a story, or a puzzle answered by its
	// questions, still has quiz questions the team has not answered, so
	// it cannot continue. Map to 403.
	ErrUnanswered = errors.New("answer the questions on this step first")
)

// NodeState is what a team has done at one node.
type NodeState struct {
	FirstSeen     time.Time  `json:"first_seen"`
	Solved        bool       `json:"solved,omitempty"`
	Outcome       string     `json:"outcome,omitempty"`
	HasOutcome    bool       `json:"has_outcome,omitempty"`
	Attempts      int        `json:"attempts,omitempty"`
	BlockedUntil  *time.Time `json:"blocked_until,omitempty"`
	HintsRevealed int        `json:"hints_revealed,omitempty"`
	// Answer is what the team typed when it left this puzzle by an
	// answer. A switch routes on it.
	Answer string `json:"answer,omitempty"`
	// MatchID is the ID of the accepted answer that matched, when it has
	// one — what a crossroads path that refers to answers compares.
	MatchID string `json:"match_id,omitempty"`
}

// Position is where the team is. Nil means unknown, or not trusted —
// the live engine's anti-teleport rule has already been applied by the
// time a position gets here.
type Position struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Team is one team's whole state. It implements walk.State.
//
// Every field except G and Occ round-trips through JSON, so the
// simulator can hand the state to the browser and take it back on the
// next call without the server remembering anything.
type Team struct {
	G *graph.Graph `json:"-"`

	Current       string        `json:"current,omitempty"`
	NodeEnteredAt time.Time     `json:"node_entered_at"`
	StartedAt     time.Time     `json:"started_at"`
	Completed     bool          `json:"completed,omitempty"`
	Ending        graph.Outcome `json:"outcome,omitempty"`

	Points         float64 `json:"points"`
	PenaltyScore   float64 `json:"penalty_score"`
	PenaltySeconds int     `json:"penalty_seconds"`
	Theme          string  `json:"theme,omitempty"`
	// LatestAnswer is the answer that most recently moved the team on:
	// what a switch reads when it names no puzzle.
	LatestAnswer string `json:"last_answer,omitempty"`

	Nodes   map[string]*NodeState `json:"nodes,omitempty"`
	Items   map[string]int        `json:"items,omitempty"`
	Journal map[string]bool       `json:"journal,omitempty"`
	// JournalOrder is the journal in the order it was unlocked, for an
	// adventure that lists notes as they were found.
	JournalOrder []string `json:"journal_order,omitempty"`
	// TimedOut records that the adventure's time limit has moved the
	// team, so it does not again.
	TimedOut bool `json:"timed_out,omitempty"`
	// Cheated records that the team has solved a puzzle with the
	// adventure's cheat password, which leaves it out of the rankings.
	Cheated bool `json:"cheated,omitempty"`
	// Lang is the language the team plays in; empty is the adventure's
	// main language.
	Lang     string             `json:"lang,omitempty"`
	Counters map[string]float64 `json:"counters,omitempty"`
	// Picks are the team's quiz and tie-breaker answers, keyed by
	// PickKey. Set once each; they score when the team finishes.
	Picks   map[string]string          `json:"picks,omitempty"`
	Entered map[string]map[string]bool `json:"entered,omitempty"` // hub -> port -> ever entered

	// Occ is how many other teams are inside each hub branch, keyed
	// "hub\x00port". It is a fact about the event, not the team, so it
	// is not serialised: the live engine reads it under a lock, and a
	// simulated team is alone.
	Occ map[string]int `json:"-"`

	SlotIndex int `json:"slot"`
	Size      int `json:"team_size"`

	Now      time.Time `json:"now"`
	Position *Position `json:"position,omitempty"`
	// Phones are where each of the team's phones that reported lately
	// is, for conditions that ask where the players are rather than the
	// team. Like Occ it is read fresh for every action, never stored.
	//
	// Nil means the team keeps together: every player stands at
	// Position, as a simulated team does. The live engine sets it, empty
	// when no phone has reported lately; then Position, wherever the
	// team was last seen, stands for one player.
	Phones []Position `json:"-"`
	// Viewer is the phone a view or an action is for, which a block shown
	// "here" asks about; nil builds it for the team as a whole — the
	// organisers' look at it, a simulated team — where "here" is wherever
	// one of its players is. Never stored.
	Viewer *Viewer `json:"-"`
	// Players are the slots of the players who are playing, for steps
	// done together (see together.go), and Presses what each did on the
	// current step; PressChanged is the press an action recorded or
	// withdrew, for the engine to persist. Players is nil for the team as
	// a whole. None is stored with the team.
	Players      []int64         `json:"-"`
	Presses      map[int64]Press `json:"-"`
	PressChanged *PressChange    `json:"-"`
	// Secrets are who holds each secret role, by slot (0: no one), and
	// Accused whom most of the team named at each accusation block, by
	// PickKey; see secret.go. NewJournal are notes an action unlocked
	// that the engine must persist. None is stored with the team.
	Secrets    map[string]int64 `json:"-"`
	Accused    map[string]int64 `json:"-"`
	NewJournal []string         `json:"-"`
	// Holders are the kit items handed to a player, by slot: theirs to
	// use, not the role's. Not stored with the team.
	Holders map[string]int64 `json:"-"`
	// RoleHolders is the slot holding each public role, for a two-key
	// launch whose posts are roles' (graph.Answer.PostRoles). Nil is the
	// team as a whole, every role its own. Not stored with the team.
	RoleHolders map[string]int64 `json:"-"`
	// Away are the players whose phones have been silent a while — put
	// away, out of battery, gone home — so a post of their role is
	// anyone's rather than a wait for them. PhoneAt is where each phone
	// that reported lately is, by slot. Neither is stored with the team.
	Away    map[int64]bool     `json:"-"`
	PhoneAt map[int64]Position `json:"-"`
	// WalkedMetres is how far the team has walked on its current node:
	// read from its phones' positions by the live engine, added up from
	// the moves the simulator makes.
	WalkedMetres float64 `json:"walked,omitempty"`
}

// New returns an empty team on g at time now, not yet placed anywhere.
func New(g *graph.Graph, now time.Time) *Team {
	t := &Team{G: g, Now: now, StartedAt: now, NodeEnteredAt: now, Size: 1}
	t.Init()
	for slug, q := range g.Kit() {
		t.Items[slug] += q
	}
	return t
}

// Init allocates any nil maps. Call it after decoding a Team from JSON.
func (t *Team) Init() {
	if t.Nodes == nil {
		t.Nodes = map[string]*NodeState{}
	}
	if t.Items == nil {
		t.Items = map[string]int{}
	}
	if t.Journal == nil {
		t.Journal = map[string]bool{}
	}
	if t.Counters == nil {
		t.Counters = map[string]float64{}
	}
	if t.Entered == nil {
		t.Entered = map[string]map[string]bool{}
	}
	if t.Occ == nil {
		t.Occ = map[string]int{}
	}
}

// Start places a new team on the graph's first stop — or, when the
// adventure lets teams choose where to begin (graph.ChoosesStart), leaves
// it waiting at the starts with no current node until it calls
// ChooseStart.
func Start(g *graph.Graph, now time.Time) (*Team, walk.Step, error) {
	t := New(g, now)
	if g.ChoosesStart() {
		return t, walk.Step{}, nil
	}
	step, err := walk.First(g, t)
	if err != nil {
		return nil, walk.Step{}, err
	}
	t.Apply(step)
	return t, step, nil
}

// Place puts a new team directly on node, as if it had just arrived
// there with no history. The designer's preview uses it to show one
// step on its own.
func Place(g *graph.Graph, node string, now time.Time) (*Team, error) {
	if _, ok := g.Node(node); !ok {
		return nil, fmt.Errorf("no node %q", node)
	}
	t := New(g, now)
	t.Current = node
	t.Row(node)
	return t, nil
}

// Row returns the team's record for a node, creating it — which is what
// marks the node visited.
func (t *Team) Row(slug string) *NodeState {
	r := t.Nodes[slug]
	if r == nil {
		r = &NodeState{FirstSeen: t.Now}
		t.Nodes[slug] = r
	}
	return r
}

// MarkEntered records that the team has gone into a hub branch.
func (t *Team) MarkEntered(hub, port string) {
	if t.Entered[hub] == nil {
		t.Entered[hub] = map[string]bool{}
	}
	t.Entered[hub][port] = true
}

// Apply folds a walk step into the team: every node passed — which
// includes the one it lands on — is visited,
// every branch entered is recorded, and the cursor moves. Effects are not
// here — the walk applied them through ApplyEffect as it passed them, so
// a gate later in the same step could see them.
func (t *Team) Apply(step walk.Step) {
	for _, slug := range step.Passed {
		t.Row(slug)
	}
	for _, b := range step.Entered {
		t.MarkEntered(b.Hub, b.Port)
	}
	if step.Finished {
		if !t.Completed {
			// The quiz walk is marked as the team finishes, never before:
			// a score that moved with each pick would give the answers away.
			// So is the accusation, which would give the saboteur away.
			t.Points += t.Quiz().Points + t.accusePoints()
		}
		t.Completed = true
		t.Ending = step.Outcome
	}
	t.Current = step.To
	t.NodeEnteredAt = t.Now
	t.WalkedMetres = 0
}

// --- walk.State ----------------------------------------------------------

func (t *Team) Solved(n string) bool {
	r := t.Nodes[n]
	return r != nil && r.Solved
}

func (t *Team) Visited(n string) bool { return t.Nodes[n] != nil }

func (t *Team) Outcome(n string) (string, bool) {
	r := t.Nodes[n]
	if r == nil || !r.HasOutcome {
		return "", false
	}
	return r.Outcome, true
}

func (t *Team) Attempts(n string) int {
	if r := t.Nodes[n]; r != nil {
		return r.Attempts
	}
	return 0
}

func (t *Team) ItemQty(i string) int { return t.Items[i] }

func (t *Team) AnswerTo(n string) (string, bool) {
	r := t.Nodes[n]
	if r == nil || r.Answer == "" {
		return "", false
	}
	return r.Answer, true
}

func (t *Team) MatchedAnswer(n string) string {
	if r := t.Nodes[n]; r != nil {
		return r.MatchID
	}
	return ""
}

func (t *Team) LastAnswer() (string, bool) { return t.LatestAnswer, t.LatestAnswer != "" }

func (t *Team) CounterValue(c string) float64 {
	if v, ok := t.Counters[c]; ok {
		return v
	}
	// A counter no team has touched yet reads as its declared initial
	// value, not zero.
	for _, def := range t.G.Counters {
		if def.Slug == c {
			return def.Initial
		}
	}
	return 0
}

// Score is the net score — points earned minus hint and penalty costs —
// because that is the figure a "score ≥ 100" gate means.
func (t *Team) Score() float64 { return t.Points - t.PenaltyScore }

func (t *Team) ElapsedSeconds() float64 { return t.Now.Sub(t.StartedAt).Seconds() }

func (t *Team) NodeElapsedSeconds() float64 {
	if t.NodeEnteredAt.IsZero() {
		return 0
	}
	return t.Now.Sub(t.NodeEnteredAt).Seconds()
}

func (t *Team) TeamSize() int { return t.Size }

// InZone reports whether one of the team's players is inside the zone.
// While the team is together that is where it is; once it splits up, a
// player who reached the zone has brought the team there, however far
// the others are.
func (t *Team) InZone(slug string) bool { return t.PlayersInZone(slug) > 0 }

// PlayersInZone counts the team's players inside the zone.
func (t *Team) PlayersInZone(slug string) int {
	z := t.zoneBySlug(slug)
	if z == nil {
		return 0
	}
	n := 0
	for _, p := range t.players() {
		if ZoneContains(*z, p.Lat, p.Lng) {
			n++
		}
	}
	return n
}

// ZonesHeld reports whether every one of the zones has a player of its
// own inside it, at the same time. Zones may overlap, and a player
// standing in two of them holds only one, so this is a matching of
// players to zones rather than a count per zone. Teams are small and so
// are the lists (see matching).
func (t *Team) ZonesHeld(slugs []string) bool {
	slugs = slices.Compact(slices.Sorted(slices.Values(slugs)))
	players := t.players()
	if len(slugs) > len(players) {
		return false
	}
	// in[i][j]: player j stands in zone i.
	in := make([][]bool, len(slugs))
	for i, slug := range slugs {
		z := t.zoneBySlug(slug)
		if z == nil {
			return false
		}
		in[i] = make([]bool, len(players))
		for j, p := range players {
			in[i][j] = ZoneContains(*z, p.Lat, p.Lng)
		}
	}
	return matching(in, len(players)) == len(slugs)
}

// Viewer is one phone of the team.
type Viewer struct {
	// Slot is the player's.
	Slot int64
	// Position is where the phone is; nil when it has not reported
	// lately, and then it is nowhere.
	Position *Position
	// Roles are the roles its player holds: claimed, or dealt to them.
	Roles []string
}

// Here reports whether the player the view is for is inside the zone.
func (t *Team) Here(slug string) bool {
	if t.Viewer == nil {
		return t.InZone(slug)
	}
	z := t.zoneBySlug(slug)
	return z != nil && t.Viewer.Position != nil && ZoneContains(*z, t.Viewer.Position.Lat, t.Viewer.Position.Lng)
}

// HasRole reports whether the player the view is for holds the role. The
// team as a whole holds every role: the organisers see what any of its
// players may, and a simulated team is all of its players at once.
func (t *Team) HasRole(role string) bool {
	return t.Viewer == nil || slices.Contains(t.Viewer.Roles, role)
}

// players are where the team's players stand: see Phones.
func (t *Team) players() []Position {
	switch {
	case len(t.Phones) > 0:
		return t.Phones
	case t.Position == nil:
		return nil
	case t.Phones != nil:
		return []Position{*t.Position}
	}
	out := make([]Position, max(t.Size, 1))
	for i := range out {
		out[i] = *t.Position
	}
	return out
}

// ZoneContains reports whether a point is inside a zone: within the
// circle, or inside the polygon.
//
// The polygon test is the even-odd ray cast on raw latitude and
// longitude. Treating degrees as a flat plane is exact enough at the
// scale of a zone — a park, a square, a block — and the answer does not
// depend on which corner the author drew first.
func ZoneContains(z graph.Zone, lat, lng float64) bool {
	if !z.IsPolygon() {
		return HaversineMeters(lat, lng, z.Lat, z.Lng) <= z.RadiusM
	}
	in := false
	p := z.Polygon
	for i, j := 0, len(p)-1; i < len(p); j, i = i, i+1 {
		yi, xi, yj, xj := p[i][0], p[i][1], p[j][0], p[j][1]
		if (yi > lat) != (yj > lat) && lng < (xj-xi)*(lat-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

func (t *Team) LocalTime() time.Time { return t.Now }

func (t *Team) Walked() float64 { return t.WalkedMetres }

func (t *Team) Picked(node, block string) string { return t.Picks[PickKey(node, block)] }

func (t *Team) Slot() int { return t.SlotIndex }

func (t *Team) BranchDone(hub, port string) bool { return t.Entered[hub][port] }

func (t *Team) CompletedBranches(hub string) int { return len(t.Entered[hub]) }

func (t *Team) Occupancy(hub, port string) int { return t.Occ[hub+"\x00"+port] }

// ApplyEffect folds an effect into memory as the walk passes it.
// Persisting it is the caller's business.
func (t *Team) ApplyEffect(_ string, e graph.EffectConfig) {
	switch e.Kind {
	case graph.GrantItem:
		t.Items[e.Item] += QtyOf(e)
	case graph.AdjustScore:
		t.Points += float64(e.Points)
	case graph.SetCounter:
		t.Counters[e.Counter] = t.nextCounterValue(e)
	case graph.UnlockJournal:
		if !t.Journal[e.JournalEntry] {
			t.Journal[e.JournalEntry] = true
			t.JournalOrder = append(t.JournalOrder, e.JournalEntry)
		}
	case graph.SetTheme:
		t.Theme = e.Theme
	}
}

// nextCounterValue computes a counter's value after an effect, clamped
// to its declared bounds.
func (t *Team) nextCounterValue(e graph.EffectConfig) float64 {
	v := e.Value
	if e.Op != graph.CounterSet {
		v = t.CounterValue(e.Counter) + e.Value
	}
	for _, def := range t.G.Counters {
		if def.Slug != e.Counter {
			continue
		}
		if def.Min != nil && v < *def.Min {
			v = *def.Min
		}
		if def.Max != nil && v > *def.Max {
			v = *def.Max
		}
	}
	return v
}

// QtyOf is an item effect's quantity; zero means one.
func QtyOf(e graph.EffectConfig) int {
	if e.Qty == 0 {
		return 1
	}
	return e.Qty
}

// --- actions -----------------------------------------------------------

// Result is what a player action produced.
type Result struct {
	// Accepted is true when an answer matched an output.
	Accepted bool `json:"accepted"`
	// Port is the output the team left by, when it moved.
	Port string `json:"port,omitempty"`
	// Points awarded by this action, after any partial-credit multiplier.
	Points float64 `json:"points,omitempty"`
	// Response is the wrong-answer ladder text for a rejected guess.
	Response string `json:"response,omitempty"`
	// BlockedUntil is set when a wrong guess triggered a lockout.
	BlockedUntil *time.Time `json:"blocked_until,omitempty"`
	// Hint is the text revealed by RevealHint.
	Hint string `json:"hint,omitempty"`
	// TimeUp is set when the adventure's time limit moved the team
	// instead of the action it asked for.
	TimeUp bool `json:"time_up,omitempty"`
	// Waiting is set when a step done together took the player's part
	// but waits for the others: keys still to turn, players to agree.
	Waiting *Together `json:"waiting,omitempty"`

	// View is the team's state after the action.
	View View `json:"view"`
}

// current checks that the client is acting on the node the team is at,
// and returns it.
func (t *Team) current(node string) (graph.Node, error) {
	if t.Completed {
		return graph.Node{}, ErrFinished
	}
	if t.Current != node {
		return graph.Node{}, ErrStale
	}
	n, ok := t.G.Node(node)
	if !ok {
		return graph.Node{}, fmt.Errorf("current node %q is not in the graph", node)
	}
	return n, nil
}

// Answer submits a guess at the puzzle the team is on.
//
// node is the puzzle the client believes the team is at. If the team has
// moved since — a teammate answered first — the guess is refused with
// ErrStale rather than applied to whatever the team is on now, which
// would be answering a question the player has never seen.
//
// A nil step with a nil error means the team stayed put but its record
// at node changed (a wrong guess was counted); the caller persists that.
func (t *Team) Answer(node, guess string) (Result, *walk.Step, error) {
	n, err := t.current(node)
	if err != nil {
		return Result{}, nil, err
	}
	if n.Type != graph.Puzzle || n.PuzzleCfg == nil {
		return Result{}, nil, ErrWrongNode
	}
	cfg := n.PuzzleCfg
	r := t.Row(n.Slug)
	if res, err := t.mayAnswer(n); err != nil {
		return res, nil, err
	}
	if cfg.Kind() == graph.AnswerKeys {
		// Turned, not answered: see Turn.
		return Result{}, nil, ErrWrongNode
	}
	if cfg.Kind() == graph.AnswerQuestions && t.unanswered(n) {
		return Result{}, nil, ErrUnanswered
	}
	if cfg.Kind() == graph.AnswerItems && t.usesOthersKit(guess) {
		return Result{}, nil, ErrNotYours
	}

	// First answer output that accepts the guess and whose zone
	// constraint holds. Order is the author's: it is how they express
	// precedence between overlapping answers.
	for _, o := range answerOrder(cfg) {
		m := t.judge(n, o, guess)
		if !m.OK || !t.outputZoneOK(o) {
			continue
		}
		points := float64(o.Points) * m.Multiplier
		if cfg.Unanimous {
			// Right, but it counts only once everyone says so; until then
			// it is neither taken nor a wrong guess.
			if w, all := t.agree(o.Port); !all {
				return Result{Waiting: w}, nil, nil
			}
		}
		if cfg.Kind() == graph.AnswerItems && !cfg.Answer.Keep {
			// Used up before the walk, which may pass a gate asking
			// what the team still carries.
			for slug, q := range cfg.Answer.Needs() {
				if t.Items[slug] -= q; t.Items[slug] <= 0 {
					delete(t.Items, slug)
				}
			}
		}
		r.Solved = true
		// Recorded before the walk, which may pass a switch reading it.
		r.Answer, r.MatchID = strings.TrimSpace(guess), m.ID
		t.LatestAnswer = r.Answer
		step, err := t.leave(n, o.Port, points)
		if err != nil {
			return Result{}, nil, err
		}
		return Result{Accepted: true, Port: o.Port, Points: points}, step, nil
	}

	// Wrong. Count it, lock out if configured, and take a pity path if
	// the author set one and it is now due.
	if cfg.Unanimous {
		t.disagree()
	}
	r.Attempts++
	res := Result{}
	reply := cfg.ResponseIndex(r.Attempts)
	lockout := cfg.LockoutSeconds
	if reply >= 0 {
		lockout = max(lockout, cfg.Responses[reply].LockoutSeconds)
	}
	if lockout > 0 {
		until := t.Now.Add(time.Duration(lockout) * time.Second)
		r.BlockedUntil = &until
		res.BlockedUntil = &until
	}
	for _, o := range cfg.Outputs {
		if o.Trigger == graph.OnAttempts && o.AfterAttempts > 0 && r.Attempts >= o.AfterAttempts {
			step, err := t.leave(n, o.Port, 0)
			if err != nil {
				return Result{}, nil, err
			}
			res.Port = o.Port
			return res, step, nil
		}
	}
	if reply >= 0 {
		res.Response = t.G.Tr(t.Lang, graph.NodeKey(n.Slug, fmt.Sprintf("reply:%d", reply)), cfg.Responses[reply].Text)
	}
	return res, nil, nil
}

// mayAnswer refuses an answer the puzzle is not taking now: locked out
// after a wrong guess, the team not at its zone, its time run out.
func (t *Team) mayAnswer(n graph.Node) (Result, error) {
	cfg := n.PuzzleCfg
	r := t.Row(n.Slug)
	if r.BlockedUntil != nil && t.Now.Before(*r.BlockedUntil) {
		return Result{BlockedUntil: r.BlockedUntil}, ErrLocked
	}
	if cfg.Zone != "" && !t.InZone(cfg.Zone) {
		return Result{}, ErrNotHere
	}
	if cfg.TimeLimitSeconds > 0 && t.NodeElapsedSeconds() > float64(cfg.TimeLimitSeconds) {
		return Result{}, ErrTimeUp
	}
	if cfg.By != "" && !t.HasRole(cfg.By) {
		return Result{}, ErrNotYours
	}
	return Result{}, nil
}

// usesOthersKit reports whether an item puzzle's guess places an item
// another player is to use (CanUse): theirs, not this player's. Not a
// wrong guess — the right items may be in it.
func (t *Team) usesOthersKit(guess string) bool {
	for _, slug := range strings.Split(guess, "|") {
		if !t.CanUse(strings.TrimSpace(slug)) {
			return true
		}
	}
	return false
}

// CanUse reports whether the player asking may use the item: one handed
// to a player is theirs alone, one from a role's kit its holder's, any
// other everyone's. The team as a whole may use them all.
func (t *Team) CanUse(item string) bool {
	if slot, ok := t.Holders[item]; ok {
		return t.Viewer == nil || t.Viewer.Slot == slot
	}
	if role, ok := t.G.ItemRole(item); ok {
		return t.HasRole(role)
	}
	return true
}

// Giveable reports whether the player asking may hand the item to a
// teammate: a kit item, carried, that is theirs to use.
func (t *Team) Giveable(item string) bool {
	if t.Viewer == nil || t.Items[item] <= 0 {
		return false
	}
	_, kit := t.G.ItemRole(item)
	_, given := t.Holders[item]
	return (kit || given) && t.CanUse(item)
}

// outputZoneOK applies an output's zone constraint, including the
// inverted form: "correct only if you are NOT here".
func (t *Team) outputZoneOK(o graph.Output) bool {
	if o.Zone == "" {
		return true
	}
	in := t.InZone(o.Zone)
	if o.Inverted {
		return !in
	}
	return in
}

// ChooseStart begins the adventure at one of its starts. The team must be
// waiting to begin, and inside the start's zone if it has one. The clock
// for the whole adventure starts here, not when the event did: a team
// that walks twenty minutes to its start is not behind for it.
func (t *Team) ChooseStart(start string) (Result, *walk.Step, error) {
	if t.Completed {
		return Result{}, nil, ErrFinished
	}
	if t.Current != "" {
		return Result{}, nil, ErrStale
	}
	n, ok := t.G.Node(start)
	if !ok || n.Type != graph.Start {
		return Result{}, nil, ErrWrongNode
	}
	if n.StartCfg != nil && n.StartCfg.Zone != "" && !t.InZone(n.StartCfg.Zone) {
		return Result{}, nil, ErrNotHere
	}
	t.StartedAt = t.Now
	r := t.Row(start)
	r.Solved, r.Outcome, r.HasOutcome = true, graph.PortOut, true
	step, err := walk.FirstFrom(t.G, t, start)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Port: graph.PortOut}, &step, nil
}

// Deadline is when the adventure's time limit falls for this team: a
// limit's length after it started. None before it has begun.
func (t *Team) Deadline() (time.Time, bool) {
	tl := t.G.TimeLimit
	if tl == nil || tl.Seconds <= 0 || t.StartedAt.IsZero() || t.Current == "" {
		return time.Time{}, false
	}
	return t.StartedAt.Add(time.Duration(tl.Seconds) * time.Second), true
}

// TimeUpDue reports whether the time limit has passed and not yet moved
// the team.
func (t *Team) TimeUpDue() bool {
	if t.TimedOut || t.Completed {
		return false
	}
	d, ok := t.Deadline()
	return ok && !t.Now.Before(d)
}

// Act runs one of the team's actions — Answer, Continue, Choose and the
// rest — after applying the adventure's time limit if it has passed: then
// the limit moves the team instead, and the action is not run. Every
// caller goes through here, so a phone that was asleep at the deadline
// cannot slip past it, and a new action cannot forget to check.
func (t *Team) Act(action func() (Result, *walk.Step, error)) (Result, *walk.Step, error) {
	if t.TimeUpDue() {
		return t.fireTimeUp()
	}
	return action()
}

// TimeUp moves the team as the time limit says, if it is due — the
// client calls it when its countdown ends, and the engine re-checks the
// time rather than trusting it. Through Act, a due limit has already
// fired; alone, it refuses when there is nothing to do.
func (t *Team) TimeUp() (Result, *walk.Step, error) {
	if !t.TimeUpDue() {
		return Result{}, nil, ErrNotAllowed
	}
	return t.fireTimeUp()
}

func (t *Team) fireTimeUp() (Result, *walk.Step, error) {
	t.TimedOut = true
	step, err := walk.Jump(t.G, t, t.Current, t.G.TimeLimit.To)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{TimeUp: true}, &step, nil
}

// Back returns the team from a step inside a home hub's branch to that
// hub — legacy's "leave route", which is how a team walks away from a
// suspect mid-interview and comes back later. The step it leaves keeps
// no outcome: an unanswered question is still there to answer.
func (t *Team) Back(node string) (Result, *walk.Step, error) {
	if _, err := t.current(node); err != nil {
		return Result{}, nil, err
	}
	hub, ok := t.backTo()
	if !ok {
		return Result{}, nil, ErrNotAllowed
	}
	step, err := walk.Back(t.G, t, node, hub)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{}, &step, nil
}

// backTo is the home hub the team can go back to from where it stands.
func (t *Team) backTo() (string, bool) {
	if t.Current == "" {
		return "", false
	}
	hub, ok := t.G.OwnerHub(t.Current)
	if !ok {
		return "", false
	}
	h, _ := t.G.Node(hub)
	return hub, h.HubCfg != nil && h.HubCfg.Home
}

// unanswered reports whether a step shows a quiz question or the
// tie-breaker the team has not picked. A hidden one does not count: the
// team cannot answer what it cannot see.
func (t *Team) unanswered(n graph.Node) bool {
	for _, b := range n.Blocks() {
		if (b.Kind == graph.BlockQuiz || b.Kind == graph.BlockTiebreak) && t.blockShown(n.Slug, b) && t.Picks[PickKey(n.Slug, b.ID)] == "" {
			return true
		}
	}
	return false
}

// Continue moves the team past a story node.
func (t *Team) Continue(node string) (Result, *walk.Step, error) {
	n, err := t.current(node)
	if err != nil {
		return Result{}, nil, err
	}
	if n.Type != graph.Story {
		return Result{}, nil, ErrWrongNode
	}
	if n.StoryCfg != nil && n.StoryCfg.Zone != "" && !t.InZone(n.StoryCfg.Zone) {
		return Result{}, nil, ErrNotHere
	}
	return t.continueStory(n)
}

// continueStory moves on from a story step whose place, if any, has
// been dealt with: by being there, or by a cheat.
func (t *Team) continueStory(n graph.Node) (Result, *walk.Step, error) {
	if t.unanswered(n) {
		return Result{}, nil, ErrUnanswered
	}
	t.Row(n.Slug).Solved = true
	step, err := t.leave(n, graph.PortOut, 0)
	return Result{Port: graph.PortOut}, step, err
}

// Choose enters a hub branch, or moves on from a satisfied hub when port
// is graph.PortDone.
func (t *Team) Choose(hub, port string) (Result, *walk.Step, error) {
	n, err := t.current(hub)
	if err != nil {
		return Result{}, nil, err
	}
	if n.Type != graph.Hub {
		return Result{}, nil, ErrWrongNode
	}
	step, err := walk.EnterBranch(t.G, t, hub, port)
	if err != nil {
		return Result{}, nil, fmt.Errorf("%w: %v", ErrNotAllowed, err)
	}
	return Result{Port: port}, &step, nil
}

// Skip takes a puzzle's skip output. Both thresholds must be met — the
// anti-abuse rule carried over from legacy: spamming guesses alone must
// never unlock a skip.
func (t *Team) Skip(node string) (Result, *walk.Step, error) {
	n, err := t.current(node)
	if err != nil {
		return Result{}, nil, err
	}
	if n.Type != graph.Puzzle || n.PuzzleCfg == nil {
		return Result{}, nil, ErrWrongNode
	}
	if !t.canSkip(n) {
		return Result{}, nil, ErrNotAllowed
	}
	o, _ := outputFor(n, graph.OnSkip)
	step, err := t.leave(n, o.Port, 0)
	return Result{Port: o.Port}, step, err
}

// Leave takes an item puzzle's OnMissing route, for a team that does
// not carry what the slots need — off to find it. Nothing is recorded
// for the puzzle, so it is presented again when the team comes back,
// rather than passed through by the way it left, as an answered
// puzzle is (see walk). A team carrying everything has no need to go.
func (t *Team) Leave(node string) (Result, *walk.Step, error) {
	n, err := t.current(node)
	if err != nil {
		return Result{}, nil, err
	}
	if n.Type != graph.Puzzle || n.PuzzleCfg == nil || n.PuzzleCfg.Kind() != graph.AnswerItems {
		return Result{}, nil, ErrWrongNode
	}
	o, ok := outputFor(n, graph.OnMissing)
	if !ok || t.carries(n.PuzzleCfg.Answer.Needs()) {
		return Result{}, nil, ErrNotAllowed
	}
	step, err := walk.Advance(t.G, t, n.Slug, o.Port)
	if err != nil {
		return Result{}, nil, fmt.Errorf("advancing from %s.%s: %w", n.Slug, o.Port, err)
	}
	return Result{Port: o.Port}, &step, nil
}

// ForceSolve takes a puzzle output as though the team had earned it,
// for an author testing a route without the answer to hand — a QR code
// at a signpost they are not standing by, say. It skips the zone,
// lockout and time-limit checks; that is the point of it. An answer
// output counts as solved with full points and records its first answer,
// so a crossroads further on routes as it would after a real one.
//
// Only the simulator calls it. A live force-solve must also be audited,
// which is the caller's job, not this package's.
func (t *Team) ForceSolve(node, port string) (Result, *walk.Step, error) {
	n, err := t.current(node)
	if err != nil {
		return Result{}, nil, err
	}
	if n.Type != graph.Puzzle || n.PuzzleCfg == nil {
		return Result{}, nil, ErrWrongNode
	}
	for _, o := range n.PuzzleCfg.Outputs {
		if o.Port != port {
			continue
		}
		points := 0.0
		if o.Trigger == graph.OnAnswer {
			r := t.Row(n.Slug)
			r.Solved = true
			if len(o.Matchers) > 0 {
				m := o.Matchers[0]
				r.Answer, r.MatchID = m.Value, m.ID
				t.LatestAnswer = r.Answer
			}
			points = float64(o.Points)
		}
		step, err := t.leave(n, o.Port, points)
		if err != nil {
			return Result{}, nil, err
		}
		return Result{Accepted: o.Trigger == graph.OnAnswer, Port: o.Port, Points: points}, step, nil
	}
	return Result{}, nil, ErrNotAllowed
}

// Rescue moves a stuck team past a puzzle for the arranger: the skip
// route when the author gave one, otherwise the first answer route. It
// ignores zone, lockout and skip thresholds — a GPS that won't settle
// or a lost prop is what it is for — and awards no points. Auditing is
// the caller's job.
func (t *Team) Rescue(node string) (Result, *walk.Step, error) {
	n, err := t.current(node)
	if err != nil {
		return Result{}, nil, err
	}
	if n.Type != graph.Puzzle || n.PuzzleCfg == nil {
		return Result{}, nil, ErrWrongNode
	}
	o, ok := outputFor(n, graph.OnSkip)
	if !ok {
		if o, ok = outputFor(n, graph.OnAnswer); !ok {
			return Result{}, nil, ErrNotAllowed
		}
		t.Row(n.Slug).Solved = true
	}
	step, err := t.leave(n, o.Port, 0)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Port: o.Port}, step, nil
}

// Cheat solves a puzzle for a tester who has the adventure's cheat
// password: the first answer route, as if answered right, with no
// points. It works away from the puzzle's place, so a route can be
// tested from a desk; a lockout and the time limit still hold. On a
// story step it continues without being at the step's place.
// Checking the password is the caller's job.
func (t *Team) Cheat(node string) (Result, *walk.Step, error) {
	n, err := t.current(node)
	if err != nil {
		return Result{}, nil, err
	}
	if n.Type == graph.Story {
		res, step, err := t.continueStory(n)
		if err == nil {
			t.Cheated = true
		}
		return res, step, err
	}
	if n.Type != graph.Puzzle || n.PuzzleCfg == nil {
		return Result{}, nil, ErrWrongNode
	}
	cfg := n.PuzzleCfg
	r := t.Row(n.Slug)
	if r.BlockedUntil != nil && t.Now.Before(*r.BlockedUntil) {
		return Result{BlockedUntil: r.BlockedUntil}, nil, ErrLocked
	}
	if cfg.TimeLimitSeconds > 0 && t.NodeElapsedSeconds() > float64(cfg.TimeLimitSeconds) {
		return Result{}, nil, ErrTimeUp
	}
	for _, o := range answerOrder(cfg) {
		r.Solved = true
		t.Cheated = true
		step, err := t.leave(n, o.Port, 0)
		if err != nil {
			return Result{}, nil, err
		}
		return Result{Accepted: true, Port: o.Port}, step, nil
	}
	return Result{}, nil, ErrNotAllowed
}

// Timeout takes a puzzle's timeout output once its limit has passed.
// The client calls it when its countdown reaches zero; this re-checks
// the time rather than trusting the client.
func (t *Team) Timeout(node string) (Result, *walk.Step, error) {
	n, err := t.current(node)
	if err != nil {
		return Result{}, nil, err
	}
	if n.Type != graph.Puzzle || n.PuzzleCfg == nil {
		return Result{}, nil, ErrWrongNode
	}
	limit := n.PuzzleCfg.TimeLimitSeconds
	if limit <= 0 || t.NodeElapsedSeconds() < float64(limit) {
		return Result{}, nil, ErrNotAllowed
	}
	o, ok := outputFor(n, graph.OnTimeout)
	if !ok {
		return Result{}, nil, ErrNotAllowed
	}
	step, err := t.leave(n, o.Port, 0)
	return Result{Port: o.Port}, step, err
}

// RevealHint reveals the next hint on the current puzzle.
//
// Hints reveal in order: index must be the next unrevealed one, so a
// client cannot skip to the most useful hint. The delay counts from when
// the team reached the puzzle, and the hint's cost is charged to the
// team's penalty score. Like a wrong answer, it never moves the team.
func (t *Team) RevealHint(node string, index int) (Result, *walk.Step, error) {
	n, err := t.current(node)
	if err != nil {
		return Result{}, nil, err
	}
	if n.Type != graph.Puzzle || n.PuzzleCfg == nil {
		return Result{}, nil, ErrWrongNode
	}
	r := t.Row(n.Slug)
	hints := n.PuzzleCfg.Hints
	if index != r.HintsRevealed || index < 0 || index >= len(hints) {
		return Result{}, nil, ErrNotAllowed
	}
	h := hints[index]
	if t.NodeElapsedSeconds() < float64(h.DelaySeconds) {
		return Result{}, nil, ErrNotAllowed
	}
	r.HintsRevealed++
	t.PenaltyScore += float64(h.Cost)
	return Result{Hint: t.G.Tr(t.Lang, graph.NodeKey(n.Slug, fmt.Sprintf("hint:%d", index)), h.Text)}, nil, nil
}

// SetLanguage switches the language the team plays in: one the adventure
// is written or translated in, or "" for its main language.
func (t *Team) SetLanguage(lang string) error {
	if lang != "" && lang != t.G.Language && !slices.Contains(t.G.Languages(), lang) {
		return ErrNotAllowed
	}
	if lang == t.G.Language {
		lang = ""
	}
	t.Lang = lang
	return nil
}

// leave records the team leaving node n by port — the outcome that
// history-based gates read — and walks on. The step is not applied yet:
// the live engine persists before it moves the cursor.
func (t *Team) leave(n graph.Node, port string, points float64) (*walk.Step, error) {
	r := t.Row(n.Slug)
	r.Outcome, r.HasOutcome = port, true
	t.Points += points
	step, err := walk.Advance(t.G, t, n.Slug, port)
	if err != nil {
		return nil, fmt.Errorf("advancing from %s.%s: %w", n.Slug, port, err)
	}
	return &step, nil
}

func outputFor(n graph.Node, trig graph.Trigger) (graph.Output, bool) {
	for _, o := range n.PuzzleCfg.Outputs {
		if o.Trigger == trig {
			return o, true
		}
	}
	return graph.Output{}, false
}

func (t *Team) canSkip(n graph.Node) bool {
	cfg := n.PuzzleCfg
	if _, ok := outputFor(n, graph.OnSkip); !ok {
		return false
	}
	return t.Attempts(n.Slug) >= cfg.SkipAfterGuesses &&
		t.NodeElapsedSeconds() >= float64(cfg.SkipAfterSeconds)
}

// sortedKeys returns a map's keys in order, so a View is deterministic
// and a test or a diff never flickers on map iteration order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// branchOpen reports whether a hub branch's condition holds.
func (t *Team) branchOpen(b graph.Branch) bool { return cond.Eval(b.Condition, t) }

// Code is the stable, machine-readable name of a refusal, for an API
// body. Clients branch on it; the error text is English prose and is not
// a contract. An error that is not a refusal has no code.
func Code(err error) string {
	switch {
	case errors.Is(err, ErrStale):
		return "stale"
	case errors.Is(err, ErrNotHere):
		return "not_here"
	case errors.Is(err, ErrNoFix):
		return "no_fix"
	case errors.Is(err, ErrNotYours):
		return "not_yours"
	case errors.Is(err, ErrLocked):
		return "locked"
	case errors.Is(err, ErrTimeUp):
		return "time_up"
	case errors.Is(err, ErrNotAllowed):
		return "not_allowed"
	case errors.Is(err, ErrFinished):
		return "finished"
	case errors.Is(err, ErrWrongNode):
		return "wrong_node"
	case errors.Is(err, ErrUnanswered):
		return "unanswered"
	}
	return ""
}
