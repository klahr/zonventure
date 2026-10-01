// Package cond evaluates the conditions on Gate nodes and Hub branches.
//
// This is deliberately NOT an expression language.
//
// The tempting design is a small DSL — `has("red") and not solved("vault")`
// — and it is a trap. It starts as a grammar you can read in an
// afternoon, then someone needs arithmetic, then string concatenation,
// then a loop, and you have shipped a badly-designed programming
// language with no debugger, embedded in a Go binary, authored by people
// who are not programmers, running a paid event on a Saturday morning.
//
// So a condition is a *tree of plain data*. The editor builds it from
// dropdowns and renders it back as a sentence. That buys three things a
// DSL cannot:
//
//   - It is statically checkable. Refs walks the tree and hands the
//     validator every node, item, counter and zone slug it mentions, so
//     "you referenced a puzzle that no longer exists" is a save-time
//     400, not a team stuck in a park.
//   - It always terminates. There is no recursion an author can write.
//   - It round-trips through JSON with no parser on either side.
//
// Anything genuinely unanticipated goes out through a Webhook effect —
// a real escape hatch, outside the process, rather than a VM inside it.
//
// The package is pure: no database, no clock of its own. Everything it
// reads comes from the State interface.
package cond

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Op discriminates a node in the condition tree.
type Op string

const (
	// Composite operators. Of holds the operands.
	OpAll Op = "all" // every operand true; vacuously true when empty
	OpAny Op = "any" // at least one operand true; vacuously false when empty
	OpNot Op = "not" // negates the first operand

	// History. These are the reason keys are gone: the team's own
	// record answers them, so nothing has to be granted in advance.
	OpSolved   Op = "solved"   // Node was solved
	OpVisited  Op = "visited"  // Node was reached, solved or not
	OpOutcome  Op = "outcome"  // Node was left by output port Is
	OpAttempts Op = "attempts" // wrong-answer count at Node, compared Cmp N

	// Inventory and shared state.
	OpItem    Op = "item"    // quantity of Item, compared Cmp N (default: at least 1)
	OpCounter Op = "counter" // value of Counter, compared Cmp N

	// Progress and context.
	OpScore       Op = "score"        // current score, compared Cmp N
	OpElapsed     Op = "elapsed"      // seconds since the event started
	OpNodeElapsed Op = "node_elapsed" // seconds on the current node
	OpTeamSize    Op = "team_size"    // number of player slots
	OpInZone      Op = "in_zone"      // one of the team's players is inside Zone
	// The team spread out: these read each phone's position, not the team's.
	OpPlayersIn Op = "players_in_zone" // players inside Zone, compared Cmp N (default: at least 1)
	OpZonesHeld Op = "zones_held"      // every zone in Members has a player of its own inside it
	// OpHere asks about the phone the view is for: a block shown only to
	// the player standing in Zone. Only a block's show condition may use
	// it — a gate or a branch is the team's, and has no one phone.
	OpHere   Op = "here"   // this player is inside Zone
	OpWalked Op = "walked" // metres walked on the current node, compared Cmp N
	OpDrawn  Op = "drawn"  // the card a draw Block on Node dealt is option Is (its index)

	// Wall clock, for venue constraints like "the museum closes at 17:00".
	OpTimeOfDay Op = "time_of_day" // local HH:MM compared Cmp Is
	OpWeekday   Op = "weekday"     // one of Members ("mon", "tue", …)

	// OpAlways is the identity. An empty Predicate evaluates as this, so
	// "no condition" means "open" rather than an error.
	OpAlways Op = ""
)

// Cmp is a numeric or lexical comparison.
type Cmp string

const (
	Eq  Cmp = "eq"
	Ne  Cmp = "ne"
	Lt  Cmp = "lt"
	Lte Cmp = "lte"
	Gt  Cmp = "gt"
	Gte Cmp = "gte"
)

// Predicate is one node of the condition tree.
//
// A single struct with a discriminator, rather than an interface or a
// union type, because it has to survive a JSON round-trip and be
// rendered by a schema-driven form. Fields irrelevant to Op are ignored.
type Predicate struct {
	Op Op `json:"op"`

	// Of holds the operands of All, Any and Not.
	Of []Predicate `json:"of,omitempty"`

	// Node, Item, Counter and Zone name what is being asked about, and
	// Block the block on Node, for Drawn.
	Node    string `json:"node,omitempty"`
	Block   string `json:"block,omitempty"`
	Item    string `json:"item,omitempty"`
	Counter string `json:"counter,omitempty"`
	Zone    string `json:"zone,omitempty"`

	// Cmp and N form a numeric comparison. Cmp defaults to Gte, so
	// {op: item, item: crowbar} reads as "has at least one crowbar"
	// with no further ceremony — the overwhelmingly common case.
	Cmp Cmp     `json:"cmp,omitempty"`
	N   float64 `json:"n,omitempty"`

	// Is is the string operand: the output port for Outcome, or an
	// HH:MM for TimeOfDay.
	Is string `json:"is,omitempty"`

	// Members enumerates accepted values for Weekday, and the zones of
	// ZonesHeld.
	Members []string `json:"members,omitempty"`
}

// State is everything a condition may read. The runtime satisfies it;
// tests use a fake. Keeping this an interface is what stops the package
// acquiring a database import by accident.
type State interface {
	Solved(node string) bool
	Visited(node string) bool
	// Outcome returns the output port the team left node by, and
	// whether it has left that node at all.
	Outcome(node string) (string, bool)
	Attempts(node string) int
	ItemQty(item string) int
	CounterValue(counter string) float64
	Score() float64
	ElapsedSeconds() float64
	NodeElapsedSeconds() float64
	TeamSize() int
	InZone(zone string) bool
	// PlayersInZone counts the team's players inside zone.
	PlayersInZone(zone string) int
	// ZonesHeld reports whether each zone has a different player inside
	// it at once: one player standing where two zones overlap holds one.
	ZonesHeld(zones []string) bool
	// Here reports whether the player the view is for is inside zone.
	Here(zone string) bool
	// Walked is how far the team has walked since it reached its current
	// node, in metres.
	Walked() float64
	// Picked is what the team picked or was dealt on a block, or "".
	Picked(node, block string) string
	// LocalTime is the team's wall clock, used only by the wall-clock
	// operators. Passed in rather than read from the process so a
	// simulated or replayed event evaluates identically.
	LocalTime() time.Time
}

// Eval reports whether the predicate holds.
//
// An empty predicate is true: "no condition" means "open". That matters
// because it is the default shape of a freshly-dragged Gate, and an
// author should not have to satisfy a condition they have not written.
func Eval(p Predicate, s State) bool {
	switch p.Op {
	case OpAlways:
		return true

	case OpAll:
		for _, sub := range p.Of {
			if !Eval(sub, s) {
				return false
			}
		}
		return true

	case OpAny:
		for _, sub := range p.Of {
			if Eval(sub, s) {
				return true
			}
		}
		return false

	case OpNot:
		if len(p.Of) == 0 {
			// "not nothing". Treated as false rather than true: an
			// author who left a Not empty meant to restrict something,
			// so failing closed is the safer reading.
			return false
		}
		return !Eval(p.Of[0], s)

	case OpSolved:
		return s.Solved(p.Node)

	case OpVisited:
		return s.Visited(p.Node)

	case OpOutcome:
		got, left := s.Outcome(p.Node)
		return left && got == p.Is

	case OpAttempts:
		return compare(float64(s.Attempts(p.Node)), p.Cmp, p.N)

	case OpItem:
		// Default N of 0 with the default Gte would be vacuously true,
		// which is never what "requires an item" means. So a bare item
		// test reads as "at least one".
		n := p.N
		if p.Cmp == "" && n == 0 {
			n = 1
		}
		return compare(float64(s.ItemQty(p.Item)), p.Cmp, n)

	case OpCounter:
		return compare(s.CounterValue(p.Counter), p.Cmp, p.N)

	case OpScore:
		return compare(s.Score(), p.Cmp, p.N)

	case OpElapsed:
		return compare(s.ElapsedSeconds(), p.Cmp, p.N)

	case OpNodeElapsed:
		return compare(s.NodeElapsedSeconds(), p.Cmp, p.N)

	case OpTeamSize:
		return compare(float64(s.TeamSize()), p.Cmp, p.N)

	case OpInZone:
		return s.InZone(p.Zone)

	case OpPlayersIn:
		// A bare count reads as "at least one", as for an item.
		n := p.N
		if p.Cmp == "" && n == 0 {
			n = 1
		}
		return compare(float64(s.PlayersInZone(p.Zone)), p.Cmp, n)

	case OpZonesHeld:
		// No zones is not a place anyone can stand: fail closed, as an
		// unfinished condition must not open a gate.
		return len(p.Members) > 0 && s.ZonesHeld(p.Members)

	case OpHere:
		return s.Here(p.Zone)

	case OpWalked:
		return compare(s.Walked(), p.Cmp, p.N)

	case OpDrawn:
		return p.Is != "" && s.Picked(p.Node, p.Block) == p.Is

	case OpTimeOfDay:
		mins, ok := parseHHMM(p.Is)
		if !ok {
			return false
		}
		now := s.LocalTime()
		return compare(float64(now.Hour()*60+now.Minute()), p.Cmp, float64(mins))

	case OpWeekday:
		today := shortDay(s.LocalTime().Weekday().String())
		for _, d := range p.Members {
			if shortDay(d) == today {
				return true
			}
		}
		return false
	}

	// An operator this build does not know. Fail closed: a graph
	// authored by a newer version must not silently open a gate.
	return false
}

// UsesHere reports whether a condition asks where the viewing player is,
// anywhere in its tree.
func UsesHere(p Predicate) bool {
	if p.Op == OpHere {
		return true
	}
	for _, q := range p.Of {
		if UsesHere(q) {
			return true
		}
	}
	return false
}

// Spread is how many players a condition wants in each zone, for the
// conditions that ask where the players are: zones_held wants one in
// each of its zones, players_in_zone as many as its count. Within all,
// the most any part asks of a zone; within any, its first part that asks
// something. In_zone wants one player there, which a team together
// gives it. Not asks nobody anywhere, and nor does anything else.
func Spread(p Predicate) map[string]int {
	out := map[string]int{}
	switch p.Op {
	case OpInZone:
		if p.Zone != "" {
			out[p.Zone] = 1
		}
	case OpZonesHeld:
		for _, z := range p.Members {
			out[z] = 1
		}
	case OpPlayersIn:
		n := int(math.Ceil(p.N))
		switch p.Cmp {
		case Gt:
			n++
		case "":
			n = max(n, 1)
		case Gte, Eq:
		default:
			// Fewer than: met by players leaving, not by sending them.
			return out
		}
		if n > 0 {
			out[p.Zone] = n
		}
	case OpAll:
		for _, q := range p.Of {
			for z, n := range Spread(q) {
				out[z] = max(out[z], n)
			}
		}
	case OpAny:
		for _, q := range p.Of {
			if m := Spread(q); len(m) > 0 {
				return m
			}
		}
	}
	return out
}

// Waits reports whether a condition that does not hold could come true
// by the team's players moving — getting to the gate, or to the gate and
// the bridge — with nothing else changing: every part of it that does
// not hold asks where the players are, and the team has the players it
// asks for. A hub whose branches wait on one keeps the team, instead of
// taking the branch for permanently shut and moving it on.
//
// Only where the players are waits. A branch shut on the clock, on
// history or on what the team carries still counts as shut.
func Waits(p Predicate, s State) bool {
	switch p.Op {
	case OpInZone, OpZonesHeld, OpPlayersIn:
		total := 0
		for _, n := range Spread(p) {
			total += n
		}
		return total <= max(s.TeamSize(), 1)
	case OpAll:
		some := false
		for _, q := range p.Of {
			if Eval(q, s) {
				continue
			}
			if !Waits(q, s) {
				return false
			}
			some = true
		}
		total := 0
		for _, n := range Spread(p) {
			total += n
		}
		return some && total <= max(s.TeamSize(), 1)
	case OpAny:
		for _, q := range p.Of {
			if Waits(q, s) {
				return true
			}
		}
	}
	return false
}

func compare(got float64, c Cmp, want float64) bool {
	switch c {
	case Eq:
		return got == want
	case Ne:
		return got != want
	case Lt:
		return got < want
	case Lte:
		return got <= want
	case Gt:
		return got > want
	case Gte, "":
		// Gte is the default because almost every authored comparison
		// is a threshold: "at least this much".
		return got >= want
	}
	return false
}

func parseHHMM(s string) (int, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

// shortDay reduces a weekday name to a lowercase three-letter key, so
// an author may write "Mon", "mon" or "Monday" and mean the same day.
func shortDay(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) > 3 {
		s = s[:3]
	}
	return s
}

// Refs is every slug a predicate tree mentions, grouped by what kind of
// thing it names. The validator uses this to reject a condition that
// points at a puzzle, item, counter or zone which does not exist — the
// failure mode that would otherwise strand a team at a gate that can
// never open.
type Refs struct {
	Nodes    []string
	Items    []string
	Counters []string
	Zones    []string
}

// Collect walks the tree and returns everything it references.
func Collect(p Predicate) Refs {
	var r Refs
	seen := map[string]bool{}
	add := func(dst *[]string, kind, slug string) {
		if slug == "" {
			return
		}
		key := kind + "\x00" + slug
		if seen[key] {
			return
		}
		seen[key] = true
		*dst = append(*dst, slug)
	}

	var walk func(Predicate)
	walk = func(p Predicate) {
		add(&r.Nodes, "n", p.Node)
		add(&r.Items, "i", p.Item)
		add(&r.Counters, "c", p.Counter)
		add(&r.Zones, "z", p.Zone)
		if p.Op == OpZonesHeld {
			for _, z := range p.Members {
				add(&r.Zones, "z", z)
			}
		}
		for _, sub := range p.Of {
			walk(sub)
		}
	}
	walk(p)
	return r
}

// Describe renders a predicate as a readable sentence, for the editor
// and for validation messages. This is why a tree beats a DSL: the
// canonical form is data, so the human-readable form is generated and
// can never drift from what actually runs.
func Describe(p Predicate) string {
	switch p.Op {
	case OpAlways:
		return "always"
	case OpAll, OpAny:
		if len(p.Of) == 0 {
			if p.Op == OpAll {
				return "always"
			}
			return "never"
		}
		parts := make([]string, 0, len(p.Of))
		for _, sub := range p.Of {
			parts = append(parts, Describe(sub))
		}
		joiner := " and "
		if p.Op == OpAny {
			joiner = " or "
		}
		if len(parts) == 1 {
			return parts[0]
		}
		return "(" + strings.Join(parts, joiner) + ")"
	case OpNot:
		if len(p.Of) == 0 {
			return "never"
		}
		return "not " + Describe(p.Of[0])
	case OpSolved:
		return fmt.Sprintf("solved %q", p.Node)
	case OpVisited:
		return fmt.Sprintf("visited %q", p.Node)
	case OpOutcome:
		return fmt.Sprintf("left %q via %q", p.Node, p.Is)
	case OpAttempts:
		return fmt.Sprintf("attempts at %q %s %g", p.Node, cmpWord(p.Cmp), p.N)
	case OpItem:
		n := p.N
		if p.Cmp == "" && n == 0 {
			n = 1
		}
		return fmt.Sprintf("has %s %g × %q", cmpWord(p.Cmp), n, p.Item)
	case OpCounter:
		return fmt.Sprintf("counter %q %s %g", p.Counter, cmpWord(p.Cmp), p.N)
	case OpScore:
		return fmt.Sprintf("score %s %g", cmpWord(p.Cmp), p.N)
	case OpElapsed:
		return fmt.Sprintf("elapsed %s %gs", cmpWord(p.Cmp), p.N)
	case OpNodeElapsed:
		return fmt.Sprintf("time on this step %s %gs", cmpWord(p.Cmp), p.N)
	case OpTeamSize:
		return fmt.Sprintf("team size %s %g", cmpWord(p.Cmp), p.N)
	case OpInZone:
		return fmt.Sprintf("inside zone %q", p.Zone)
	case OpPlayersIn:
		n := p.N
		if p.Cmp == "" && n == 0 {
			n = 1
		}
		return fmt.Sprintf("players inside zone %q %s %g", p.Zone, cmpWord(p.Cmp), n)
	case OpHere:
		return fmt.Sprintf("this player inside zone %q", p.Zone)
	case OpZonesHeld:
		quoted := make([]string, len(p.Members))
		for i, z := range p.Members {
			quoted[i] = strconv.Quote(z)
		}
		return "a player each inside " + strings.Join(quoted, ", ")
	case OpWalked:
		return fmt.Sprintf("walked on this step %s %gm", cmpWord(p.Cmp), p.N)
	case OpDrawn:
		return fmt.Sprintf("drew card %s from %q on %q", p.Is, p.Block, p.Node)
	case OpTimeOfDay:
		return fmt.Sprintf("time of day %s %s", cmpWord(p.Cmp), p.Is)
	case OpWeekday:
		return "weekday is " + strings.Join(p.Members, "/")
	}
	return fmt.Sprintf("unknown condition %q", p.Op)
}

func cmpWord(c Cmp) string {
	switch c {
	case Eq:
		return "="
	case Ne:
		return "≠"
	case Lt:
		return "<"
	case Lte:
		return "≤"
	case Gt:
		return ">"
	default:
		return "≥"
	}
}
