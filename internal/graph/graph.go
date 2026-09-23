// Package graph is the authored shape of an adventure.
//
// An adventure is a flat directed graph. One token per team moves
// through it, carrying the team's whole state, and every node is a
// function from state to (new state, one output port). Edges say where
// each port leads.
//
// There is no nesting. Legacy modelled adventures as a tree of blocks
// containing routes containing waypoints containing puzzles — three
// container types and three matching runtime tables — and needed "keys"
// as a side channel to carry history past that structure. A flat graph
// with looping Hub nodes expresses everything that tree did, and because
// the token carries history, gating needs no keys at all.
//
// The package is pure data plus lookups: no database, no clock, no I/O.
// internal/graph/walk interprets it; internal/graph/valid checks it;
// internal/play persists it.
package graph

import (
	"fmt"
	"sort"

	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
)

// Type discriminates a node. Seven types replace fourteen legacy
// entities; resist adding an eighth without deleting something.
type Type string

const (
	Start  Type = "start"  // entry; exactly one per graph
	Puzzle Type = "puzzle" // a challenge; one output per outcome
	Story  Type = "story"  // content with no challenge
	Hub    Type = "hub"    // a set of branches with a selection policy
	Gate   Type = "gate"   // routes on a condition
	Effect Type = "effect" // changes state, then continues
	Switch Type = "switch" // routes on an answer the team gave
	End    Type = "end"    // finishes the run
)

// Well-known port names. A Puzzle's and a Hub's ports are author-defined
// and therefore not listed here.
const (
	PortOut  = "out"  // Start, Story, Effect
	PortPass = "pass" // Gate, condition held
	PortFail = "fail" // Gate, condition did not hold
	PortDone = "done" // Hub, enough branches completed
	PortElse = "else" // Switch, no case matched
)

// Graph is one authored version of an adventure.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`

	// Zones, Items, Counters and Journal are adventure-level resources
	// that nodes reference by slug. They are not nodes: putting a
	// geofence on the canvas would show the world rather than the flow,
	// and the flow is what an author is reasoning about.
	Zones    []Zone         `json:"zones,omitempty"`
	Items    []Item         `json:"items,omitempty"`
	Counters []Counter      `json:"counters,omitempty"`
	Journal  []JournalEntry `json:"journal,omitempty"`
}

// Node is one step. Exactly one of the typed config pointers is set,
// matching Type; the others are nil.
type Node struct {
	// Slug is the stable address. Everything — edges, conditions,
	// the import envelope, the MCP tools — refers to a node by slug and
	// never by database id, so a graph survives export and re-import.
	Slug  string `json:"slug"`
	Type  Type   `json:"type"`
	Title string `json:"title,omitempty"`

	// X and Y are canvas coordinates. Cosmetic: they never affect
	// evaluation, and the structural diff ignores them.
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`

	PuzzleCfg *PuzzleConfig `json:"puzzle,omitempty"`
	StoryCfg  *StoryConfig  `json:"story,omitempty"`
	HubCfg    *HubConfig    `json:"hub,omitempty"`
	GateCfg   *GateConfig   `json:"gate,omitempty"`
	EffectCfg *EffectConfig `json:"effect,omitempty"`
	SwitchCfg *SwitchConfig `json:"switch,omitempty"`
	EndCfg    *EndConfig    `json:"end,omitempty"`
	StartCfg  *StartConfig  `json:"start,omitempty"`
}

// Edge connects one output port to one node. A port may have at most
// one edge; a node may be the target of many, which is how paths
// reconverge after a branch.
type Edge struct {
	From     string `json:"from"`      // node slug
	FromPort string `json:"from_port"` // port on that node
	To       string `json:"to"`        // node slug
}

// Trigger says what causes an output to fire.
type Trigger string

const (
	// OnAnswer fires when the guess satisfies the output's matchers.
	// This is the branching mechanism: several OnAnswer outputs on one
	// puzzle send different answers different ways.
	OnAnswer Trigger = "answer"
	// OnTimeout fires when TimeLimitSeconds elapses.
	OnTimeout Trigger = "timeout"
	// OnSkip fires when the player skips, which requires both
	// SkipAfterGuesses and SkipAfterSeconds to be satisfied.
	OnSkip Trigger = "skipped"
	// OnAttempts fires once AfterAttempts wrong guesses have been made,
	// for a pity path that moves a stuck team along.
	OnAttempts Trigger = "attempts"
)

// Output is one way out of a puzzle.
type Output struct {
	Port    string  `json:"port"`
	Label   string  `json:"label,omitempty"`
	Trigger Trigger `json:"trigger"`

	// Matchers accept a guess. Evaluated in order, with Anything
	// matchers considered only after the specific ones — see match.Any.
	Matchers []match.Matcher `json:"matchers,omitempty"`

	// Zone optionally constrains where the answer counts. Inverted
	// turns it into "only if you are NOT here", which legacy supported
	// and is genuinely used: leave the courtyard, then answer.
	Zone     string `json:"zone,omitempty"`
	Inverted bool   `json:"inverted,omitempty"`

	// AfterAttempts is the threshold for OnAttempts.
	AfterAttempts int `json:"after_attempts,omitempty"`

	// Points awarded when this output fires, scaled by the matcher's
	// multiplier so a Near answer earns partial credit.
	Points int `json:"points,omitempty"`
}

// Response is one rung of the wrong-answer ladder.
//
// Responses are feedback, not routing: they never move the team. That
// separation is the thing legacy conflated — its AnswerResponses both
// spoke to the player *and* granted keys that unlocked routes, which is
// why branching needed keys in the first place.
// MediaKind is what a Media reference points at.
type MediaKind string

const (
	MediaImage MediaKind = "image"
	MediaAudio MediaKind = "audio"
)

// Media shows a picture or plays a sound on a step or a hint. ID is an
// adventure_media row of the same adventure; graphstore.Save refuses an
// id that belongs to another adventure, because a player may fetch any
// media their current step names.
type Media struct {
	ID   int64     `json:"id"`
	Kind MediaKind `json:"kind"`
	// Alt describes an image for someone who cannot see it, or an audio
	// clip for someone who cannot hear it.
	Alt string `json:"alt,omitempty"`
}

type Response struct {
	// AfterAttempts is the wrong-guess count at which this text starts
	// being shown. The highest matching rung wins, and the last one
	// repeats, so the 1st wrong guess can read differently from the 5th.
	AfterAttempts int    `json:"after_attempts,omitempty"`
	Text          string `json:"text"`
}

// Hint is a time-gated nudge.
type Hint struct {
	Text string `json:"text"`
	// DelaySeconds counts from when the team reached the puzzle.
	DelaySeconds int `json:"delay_seconds,omitempty"`
	// Cost is subtracted from the team's score when revealed.
	Cost int `json:"cost,omitempty"`

	Media []Media `json:"media,omitempty"`
}

// PuzzleConfig is the workhorse node.
type PuzzleConfig struct {
	Prompt string  `json:"prompt,omitempty"`
	Media  []Media `json:"media,omitempty"`

	// Zone gates arrival: the prompt is withheld and answers refused
	// until the team is inside it.
	Zone string `json:"zone,omitempty"`

	Outputs   []Output   `json:"outputs,omitempty"`
	Responses []Response `json:"responses,omitempty"`
	Hints     []Hint     `json:"hints,omitempty"`

	// TimeLimitSeconds, when set, fires an OnTimeout output. Legacy
	// only rejected late guesses, which could strand a team forever;
	// routing on timeout is the fix.
	TimeLimitSeconds int `json:"time_limit_seconds,omitempty"`

	// Skipping requires BOTH thresholds. That conjunction is
	// deliberate anti-abuse from legacy: guess-spamming alone must not
	// unlock a skip.
	SkipAfterGuesses int `json:"skip_after_guesses,omitempty"`
	SkipAfterSeconds int `json:"skip_after_seconds,omitempty"`

	// LockoutSeconds blocks answering for a while after a wrong guess.
	LockoutSeconds int `json:"lockout_seconds,omitempty"`

	// Repeatable allows re-solving on a later visit. Off by default:
	// that is what guarantees a Hub loop terminates.
	Repeatable bool `json:"repeatable,omitempty"`
}

// StoryConfig is content with no challenge — an intro, an interstitial,
// a reveal. Shares its implementation with a puzzle that has no answer
// outputs, but stays a distinct type because the canvas reads better
// when a narrative beat does not look like a challenge.
type StoryConfig struct {
	Text  string  `json:"text,omitempty"`
	Media []Media `json:"media,omitempty"`
	Zone  string  `json:"zone,omitempty"`
}

// Policy decides which branch of a Hub a team enters.
type Policy string

const (
	// PolicyChoice lets the team pick. Legacy PLAYER_CHOICE.
	PolicyChoice Policy = "choice"
	// PolicySequence takes branches in declared order. Legacy SEQUENCE.
	PolicySequence Policy = "sequence"
	// PolicyShuffle gives each team its own order, by the deterministic
	// cyclic shift in internal/adventure/permutation.go. Legacy had no
	// equivalent; zv's shuffled lane mode is exactly this.
	PolicyShuffle Policy = "shuffle"
	// PolicySpread sends teams to the least-occupied branch, to stop
	// everyone clustering at one station. Legacy SPREAD.
	PolicySpread Policy = "spread"
	// PolicyCapacity respects a per-branch limit on concurrent teams.
	// Legacy PRIORITY with MaxConcurrentVisitors — physical crowd
	// control at a real location, and the one policy whose failure is
	// visible in the field.
	PolicyCapacity Policy = "capacity"
	// PolicyRandom picks at random, seeded by (event, team, node) so a
	// run reproduces in the simulator and the recap.
	PolicyRandom Policy = "random"
)

// Branch is one way out of a Hub.
type Branch struct {
	Port  string `json:"port"`
	Label string `json:"label,omitempty"`

	// Condition decides whether this branch is offered at all. This is
	// about *offering*, which is intrinsically a hub concern; Gate
	// nodes remain the way to condition routing.
	// omitzero, not omitempty: omitempty is a no-op on a struct, so an
	// unconditional branch would serialise an empty condition object.
	Condition cond.Predicate `json:"condition,omitzero"`

	// Capacity is the concurrent-team limit under PolicyCapacity.
	Capacity int `json:"capacity,omitempty"`
}

// HubConfig is a set of branches the team works through, returning here
// after each. Hubs are why the graph may contain cycles.
type HubConfig struct {
	Policy   Policy   `json:"policy"`
	Branches []Branch `json:"branches,omitempty"`

	// Required is how many branches must complete before Done fires.
	// Zero means all. Legacy called this routes_required.
	Required int `json:"required,omitempty"`

	// HideLocked hides branches whose Condition is false, rather than
	// showing them greyed out.
	HideLocked bool `json:"hide_locked,omitempty"`
}

// GateConfig routes on a condition, to PortPass or PortFail.
type GateConfig struct {
	Condition cond.Predicate `json:"condition"`
}

// EffectKind is what an Effect node does.
// SwitchConfig routes on an answer, the way a hub routes on a choice.
//
// A puzzle's own outputs decide whether an answer is accepted at all — a
// wrong one keeps the team on the puzzle. A switch comes after: the
// puzzle accepts (often with an "anything" matcher), carries the answer
// down its output, and the switch sends the team one way or another by
// what it was. That separates "did they answer" from "what did they
// say", and lets a later switch route on an answer given much earlier.
type SwitchConfig struct {
	// From is the puzzle whose answer is tested. Empty means the answer
	// the team gave most recently — the one "on the wire".
	From string `json:"from,omitempty"`
	// Cases are tried in order; the first whose matchers accept the
	// answer is taken. None matching, or no answer yet, takes PortElse.
	Cases []Case `json:"cases,omitempty"`
}

// Case is one route out of a switch. It is taken when the answer is one
// of Answers, or failing that, is accepted by Matchers.
//
// Answers is the usual form: references to accepted answers of a puzzle,
// as "node/matcher-id". Following a reference rather than comparing text
// means an author who corrects "Nroth" to "North" in the puzzle does not
// also have to find and fix every crossroads that routes on it. Matchers
// are for answers that have nothing to refer to — an open question whose
// puzzle accepts anything.
type Case struct {
	Port     string          `json:"port"`
	Label    string          `json:"label,omitempty"`
	Answers  []string        `json:"answers,omitempty"`
	Matchers []match.Matcher `json:"matchers,omitempty"`
}

// AnswerRef builds a Case.Answers entry.
func AnswerRef(node, matcherID string) string { return node + "/" + matcherID }

type EffectKind string

const (
	GrantItem     EffectKind = "grant_item"
	AdjustScore   EffectKind = "score"
	SetCounter    EffectKind = "counter"
	UnlockJournal EffectKind = "journal"
	SendEmail     EffectKind = "email"
	SetTheme      EffectKind = "theme"
	// CallWebhook is the real escape hatch. Anything the model did not
	// anticipate becomes an HTTP call to the arranger's own code,
	// rather than a scripting VM inside this process.
	CallWebhook EffectKind = "webhook"
)

// CounterOp is how SetCounter changes a counter.
type CounterOp string

const (
	CounterSet CounterOp = "set"
	CounterAdd CounterOp = "add"
)

// EffectConfig changes state and continues. Effects are nodes rather
// than properties so a consequence is visible on the canvas: you can
// see where an item is granted without opening a panel.
type EffectConfig struct {
	Kind EffectKind `json:"kind"`

	Item string `json:"item,omitempty"`
	Qty  int    `json:"qty,omitempty"`

	Points int `json:"points,omitempty"`

	Counter string    `json:"counter,omitempty"`
	Op      CounterOp `json:"op,omitempty"`
	Value   float64   `json:"value,omitempty"`

	JournalEntry string `json:"journal_entry,omitempty"`
	Template     string `json:"template,omitempty"`
	URL          string `json:"url,omitempty"`
	Theme        string `json:"theme,omitempty"`
}

// Outcome is how a run ended.
type Outcome string

const (
	Complete Outcome = "complete"
	Failed   Outcome = "fail"
)

// StartConfig is where a start is. An adventure may have several starts;
// with more than one, or with a start that has a zone, each team chooses
// its start before playing, and may only choose a zoned start from inside
// it. A single start without a zone places every team at once, as before.
type StartConfig struct {
	Zone string `json:"zone,omitempty"`
}

// EndConfig terminates the run.
type EndConfig struct {
	Outcome Outcome `json:"outcome,omitempty"`
	Text    string  `json:"text,omitempty"`
	Media   []Media `json:"media,omitempty"`
}

// Zone is a named geofence, referenced by slug from starts, puzzles,
// outputs and conditions. It is a circle — Lat, Lng and RadiusM — unless
// Polygon has three or more corners, in which case the polygon is the
// shape and Lat/Lng is only where maps centre it.
type Zone struct {
	Slug    string  `json:"slug"`
	Name    string  `json:"name,omitempty"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	RadiusM float64 `json:"radius_m"`
	// Polygon corners as [lat, lng], in order; the last joins the first.
	Polygon [][2]float64 `json:"polygon,omitempty"`
}

// IsPolygon reports whether the zone's shape is its polygon.
func (z Zone) IsPolygon() bool { return len(z.Polygon) >= 3 }

// Starts returns every start node, in graph order.
func (g *Graph) Starts() []Node {
	var out []Node
	for _, n := range g.Nodes {
		if n.Type == Start {
			out = append(out, n)
		}
	}
	return out
}

// ChoosesStart reports whether teams pick their start rather than being
// placed: there is more than one, or the only one has a zone.
func (g *Graph) ChoosesStart() bool {
	s := g.Starts()
	return len(s) > 1 || (len(s) == 1 && s[0].StartCfg != nil && s[0].StartCfg.Zone != "")
}

// Item is inventory. Flavour only: the player can see it and an Effect
// can grant it, but no gate ever requires one, because history already
// answers every gating question.
type Item struct {
	Slug        string `json:"slug"`
	Name        string `json:"name,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Description string `json:"description,omitempty"`
}

// Counter is event-scoped shared state — one value all teams can see
// and change. This is the one piece of state that is genuinely not
// per-team, which is why it is not derivable from history.
type Counter struct {
	Slug    string   `json:"slug"`
	Name    string   `json:"name,omitempty"`
	Initial float64  `json:"initial,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
}

// JournalEntry is unlockable lore.
type JournalEntry struct {
	Slug  string `json:"slug"`
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

// Node returns the node with the given slug.
func (g *Graph) Node(slug string) (Node, bool) {
	for _, n := range g.Nodes {
		if n.Slug == slug {
			return n, true
		}
	}
	return Node{}, false
}

// StartNode returns the single entry node.
func (g *Graph) StartNode() (Node, bool) {
	for _, n := range g.Nodes {
		if n.Type == Start {
			return n, true
		}
	}
	return Node{}, false
}

// Target follows one output port. Absent means the port is unconnected,
// which the validator rejects — a team reaching it would have nowhere
// to go.
func (g *Graph) Target(nodeSlug, port string) (string, bool) {
	for _, e := range g.Edges {
		if e.From == nodeSlug && e.FromPort == port {
			return e.To, true
		}
	}
	return "", false
}

// Ports lists a node's output ports in their declared order. The
// validator uses this to find unconnected ones, and the editor to draw
// the handles.
func (n Node) Ports() []string {
	switch n.Type {
	case Start, Story, Effect:
		return []string{PortOut}
	case Gate:
		return []string{PortPass, PortFail}
	case Switch:
		if n.SwitchCfg == nil {
			return []string{PortElse}
		}
		out := make([]string, 0, len(n.SwitchCfg.Cases)+1)
		for _, c := range n.SwitchCfg.Cases {
			out = append(out, c.Port)
		}
		return append(out, PortElse)
	case End:
		return nil
	case Puzzle:
		if n.PuzzleCfg == nil {
			return nil
		}
		out := make([]string, 0, len(n.PuzzleCfg.Outputs))
		for _, o := range n.PuzzleCfg.Outputs {
			out = append(out, o.Port)
		}
		return out
	case Hub:
		if n.HubCfg == nil {
			return nil
		}
		out := make([]string, 0, len(n.HubCfg.Branches)+1)
		for _, b := range n.HubCfg.Branches {
			out = append(out, b.Port)
		}
		return append(out, PortDone)
	}
	return nil
}

// Output finds a puzzle output by port.
func (n Node) Output(port string) (Output, bool) {
	if n.PuzzleCfg == nil {
		return Output{}, false
	}
	for _, o := range n.PuzzleCfg.Outputs {
		if o.Port == port {
			return o, true
		}
	}
	return Output{}, false
}

// Branch finds a hub branch by port.
func (n Node) Branch(port string) (Branch, bool) {
	if n.HubCfg == nil {
		return Branch{}, false
	}
	for _, b := range n.HubCfg.Branches {
		if b.Port == port {
			return b, true
		}
	}
	return Branch{}, false
}

// RequiredBranches is how many branches must complete before a hub is
// done, resolving the "zero means all" default and clamping a value
// larger than the branch count.
func (h *HubConfig) RequiredBranches() int {
	n := len(h.Branches)
	if h.Required <= 0 || h.Required > n {
		return n
	}
	return h.Required
}

// ResponseFor picks the ladder rung for a given wrong-guess count: the
// highest rung at or below attempts, with the last one repeating.
func (p *PuzzleConfig) ResponseFor(attempts int) (Response, bool) {
	best := -1
	for i, r := range p.Responses {
		if r.AfterAttempts <= attempts && (best < 0 || r.AfterAttempts >= p.Responses[best].AfterAttempts) {
			best = i
		}
	}
	if best < 0 {
		return Response{}, false
	}
	return p.Responses[best], true
}

// Slugs returns every node slug, sorted, for stable error messages and
// diffs.
// MediaIDs returns every media id the graph refers to, once each.
func (g *Graph) MediaIDs() []int64 {
	seen := map[int64]bool{}
	var out []int64
	add := func(ms []Media) {
		for _, m := range ms {
			if !seen[m.ID] {
				seen[m.ID] = true
				out = append(out, m.ID)
			}
		}
	}
	for _, n := range g.Nodes {
		for _, ms := range n.MediaLists() {
			add(ms)
		}
	}
	return out
}

// MediaLists returns the node's own media, then each hint's in order.
func (n Node) MediaLists() [][]Media {
	var out [][]Media
	switch {
	case n.PuzzleCfg != nil:
		out = append(out, n.PuzzleCfg.Media)
		for _, h := range n.PuzzleCfg.Hints {
			out = append(out, h.Media)
		}
	case n.StoryCfg != nil:
		out = append(out, n.StoryCfg.Media)
	case n.EndCfg != nil:
		out = append(out, n.EndCfg.Media)
	}
	return out
}

func (g *Graph) Slugs() []string {
	out := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		out = append(out, n.Slug)
	}
	sort.Strings(out)
	return out
}

// String renders a node for error messages.
func (n Node) String() string {
	if n.Title != "" {
		return fmt.Sprintf("%s %q (%s)", n.Type, n.Title, n.Slug)
	}
	return fmt.Sprintf("%s %q", n.Type, n.Slug)
}
