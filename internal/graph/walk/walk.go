// Package walk moves a team through an authored graph.
//
// This is the interpreter, and it is pure: it takes a graph, a state and
// an event, and returns where the team ends up and what changed. It
// touches no database, so every progression rule — including the
// selection policies and the hub-completion arithmetic — is table-driven
// testable without a Postgres anywhere.
//
// That purity is the point. The legacy engine's UpdateGameState was ~150
// lines of mutually-recursive manager calls interleaved with writes, so
// the only way to find out what it did was to run an event.
//
// The caller (internal/play) is responsible for persistence, for the
// cursor compare-and-swap that makes concurrent submits safe, and for
// the occupancy row lock that makes a capacity branch honest. Those are
// inherently database concerns and deliberately live outside here.
package walk

import (
	"errors"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
)

// State is what the interpreter reads about a team. It extends the
// condition State with the bits progression needs.
type State interface {
	cond.State

	// CompletedBranches is how many branches of a hub the team has
	// finished, which is what Done compares against Required.
	CompletedBranches(hubSlug string) int
	// BranchDone reports whether one specific branch is finished, so a
	// hub never offers the same branch twice.
	//
	// "Finished" means the team was sent into the branch and has since
	// come back through the hub. Whenever the walk is evaluating a hub,
	// every branch the team entered there has necessarily been left, so
	// an implementation may simply report whether the branch was ever
	// entered — which is what internal/play does. Step.Entered is how
	// the caller learns which branches to record.
	BranchDone(hubSlug, port string) bool
	// Slot is the team's 0-based rank within its event. It seeds the
	// per-team shuffle.
	Slot() int
	// Occupancy is how many teams are currently inside a branch,
	// for PolicyCapacity. The caller reads this under a row lock.
	Occupancy(hubSlug, port string) int
	// ApplyEffect folds an effect into the in-memory state as the walk
	// passes it, so a Gate later in the same step sees the result.
	// Without this, Effect(+50 score) → Gate(score ≥ 100) would test the
	// score from before the effect. Persisting the effect is still the
	// caller's job, from Step.Effects; this only updates what the rest
	// of the walk reads.
	ApplyEffect(node string, e graph.EffectConfig)
	// AnswerTo is the answer the team gave that took it out of a
	// puzzle, and LastAnswer the most recent such answer anywhere. A
	// switch routes on one of them.
	AnswerTo(node string) (string, bool)
	LastAnswer() (string, bool)
	// MatchedAnswer is the ID of the accepted answer that took the team
	// out of a puzzle, or "" — what a crossroads reference compares.
	MatchedAnswer(node string) string
}

// Step is one movement through the graph.
type Step struct {
	// From is where the team was; To where it ends up. To is empty when
	// the run is over.
	From string
	To   string
	// Port is the output the team left From by.
	Port string
	// Effects are the effect nodes passed through on the way, in order.
	// The caller applies them; collecting them here keeps the walk pure.
	Effects []graph.Node
	// Passed is every node traversed after From, in order, including
	// gates, effects and hubs the player never sees and the final To.
	// The caller uses it to mark nodes visited and to close out any hub
	// branch the team has just returned from.
	Passed []string
	// Entered lists the hub branches this step sent the team into.
	// Under an automatic policy the walk picks the branch itself, so
	// this is the only way the caller learns it has to record the
	// entry — which is what both BranchDone and capacity occupancy are
	// computed from.
	Entered []BranchRef
	// Offer is set when the team has arrived at a Hub under
	// PolicyChoice and must pick. To is the hub itself in that case.
	Offer []graph.Branch
	// CanFinish accompanies an Offer and means the hub's Required is
	// already met: the team may take any remaining branch, or move on.
	// "Do at least three of the eight stations" needs both to be
	// possible, so the choice belongs to the player.
	CanFinish bool
	// GateFailed is set when the walk left a gate by PortFail.
	GateFailed bool
	// Finished is set when the team reached an End.
	Finished bool
	Outcome  graph.Outcome
}

// BranchRef names one branch of one hub.
type BranchRef struct {
	Hub  string
	Port string
}

var (
	// ErrDanglingPort is returned when an output has no edge. The
	// validator rejects this at save time, so reaching it at runtime
	// means a graph was written by something that skipped validation.
	ErrDanglingPort = errors.New("output port is not connected")
	// ErrNoStart means the graph has no entry node.
	ErrNoStart = errors.New("graph has no start node")
	// ErrStuck means no branch of a hub can be entered and the hub is
	// not yet done — a team would have nowhere to go.
	ErrStuck = errors.New("no branch available and hub not complete")
	// ErrLoop means the walk revisited a node within a single step,
	// which a progress-free cycle would cause.
	ErrLoop = errors.New("cycle with no progress")
)

// maxHops bounds a single Advance. A well-formed graph resolves in a
// handful of hops — the bound exists so a graph that slipped past the
// validator fails loudly instead of spinning a request forever.
const maxHops = 256

// First returns where a team begins: the node reached by walking out of
// Start, applying any effects on the way.
func First(g *graph.Graph, s State) (Step, error) {
	start, ok := g.StartNode()
	if !ok {
		return Step{}, ErrNoStart
	}
	return advanceFrom(g, s, start.Slug, graph.PortOut)
}

// FirstFrom is First for a chosen start, when an adventure has several.
func FirstFrom(g *graph.Graph, s State, start string) (Step, error) {
	n, ok := g.Node(start)
	if !ok || n.Type != graph.Start {
		return Step{}, fmt.Errorf("%w: %q is not a start", ErrNoStart, start)
	}
	return advanceFrom(g, s, start, graph.PortOut)
}

// Advance moves the team out of node `from` via output port `port` and
// settles on the next node that needs the player's attention.
//
// "Settles" is the important word: Gates, Effects and Hubs are resolved
// in this same call, so the team never sees them. A player is only ever
// parked on a Puzzle, a Story, a Hub awaiting a choice, or an End.
func Advance(g *graph.Graph, s State, from, port string) (Step, error) {
	return advanceFrom(g, s, from, port)
}

func advanceFrom(g *graph.Graph, s State, from, port string) (Step, error) {
	next, ok := g.Target(from, port)
	if !ok {
		return Step{From: from, Port: port}, fmt.Errorf("%w: %s.%s", ErrDanglingPort, from, port)
	}
	return walkTo(g, s, Step{From: from, Port: port}, next)
}

// Back takes a team from a step inside a home hub's branch back to that
// hub, without an outcome for the step it leaves: an unanswered question
// stays unanswered, to come back to.
func Back(g *graph.Graph, s State, from, hub string) (Step, error) {
	h, ok := g.Node(hub)
	if !ok || h.Type != graph.Hub || h.HubCfg == nil || !h.HubCfg.Home {
		return Step{From: from}, fmt.Errorf("%q is not a home hub", hub)
	}
	return walkTo(g, s, Step{From: from}, hub)
}

// Jump moves a team from wherever it is to `to`, as a time limit does,
// settling there like any arrival. The step it leaves keeps no outcome.
func Jump(g *graph.Graph, s State, from, to string) (Step, error) {
	if _, ok := g.Node(to); !ok {
		return Step{From: from}, fmt.Errorf("jump to unknown node %q", to)
	}
	return walkTo(g, s, Step{From: from}, to)
}

// walkTo settles a step that has arrived at `next`: see Advance.
func walkTo(g *graph.Graph, s State, step Step, next string) (Step, error) {
	seen := map[string]int{}
	for range maxHops {
		node, ok := g.Node(next)
		if !ok {
			return step, fmt.Errorf("edge points at unknown node %q", next)
		}

		// A node visited twice inside one Advance means the walk is
		// going in circles without reaching anything the player acts
		// on. Hub loops are fine across separate Advance calls; within
		// one, they are a bug.
		seen[next]++
		if seen[next] > 2 {
			return step, fmt.Errorf("%w: revisited %s", ErrLoop, next)
		}
		step.Passed = append(step.Passed, next)
		// Journal entries noted on the step itself are unlocked as the
		// team reaches it — through the same effect a journal node
		// would have, so the caller persists them the same way.
		for _, entry := range node.Unlocks {
			fx := graph.Node{Slug: node.Slug, Type: graph.Effect, EffectCfg: &graph.EffectConfig{Kind: graph.UnlockJournal, JournalEntry: entry}}
			step.Effects = append(step.Effects, fx)
			s.ApplyEffect(fx.Slug, *fx.EffectCfg)
		}

		switch node.Type {
		case graph.Puzzle:
			// A puzzle the team has already left, and which is not
			// repeatable, is passed through by the way they left it last
			// time rather than presented again. That is what guarantees a
			// loop through a hub terminates, and it is the only sensible
			// reading of arriving somewhere you have already been: you
			// solved the riddle, so you do not solve it twice.
			if node.PuzzleCfg != nil && !node.PuzzleCfg.Repeatable {
				if port, left := s.Outcome(next); left {
					if to, ok := g.Target(next, port); ok {
						next = to
						continue
					}
				}
			}
			step.To = next
			return step, nil

		case graph.Story:
			// Stories are shown every time — a "you still need the code"
			// beat on a loop back to a hub has to be seen on every pass —
			// unless the author marked one to be seen once.
			if node.StoryCfg != nil && node.StoryCfg.Once {
				if port, left := s.Outcome(next); left {
					if to, ok := g.Target(next, port); ok {
						next = to
						continue
					}
				}
			}
			step.To = next
			return step, nil

		case graph.End:
			step.To = next
			step.Finished = true
			step.Outcome = graph.Complete
			if node.EndCfg != nil && node.EndCfg.Outcome != "" {
				step.Outcome = node.EndCfg.Outcome
			}
			return step, nil

		case graph.Effect:
			step.Effects = append(step.Effects, node)
			if node.EffectCfg != nil {
				s.ApplyEffect(node.Slug, *node.EffectCfg)
			}
			next, ok = g.Target(node.Slug, graph.PortOut)
			if !ok {
				return step, fmt.Errorf("%w: %s.%s", ErrDanglingPort, node.Slug, graph.PortOut)
			}

		case graph.Gate:
			p := graph.PortFail
			if node.GateCfg == nil || cond.Eval(node.GateCfg.Condition, s) {
				p = graph.PortPass
			}
			step.GateFailed = step.GateFailed || p == graph.PortFail
			next, ok = g.Target(node.Slug, p)
			if !ok {
				return step, fmt.Errorf("%w: %s.%s", ErrDanglingPort, node.Slug, p)
			}

		case graph.Switch:
			p := switchPort(s, node)
			next, ok = g.Target(node.Slug, p)
			if !ok {
				return step, fmt.Errorf("%w: %s.%s", ErrDanglingPort, node.Slug, p)
			}

		case graph.Hub:
			chosen, offer, satisfied, done, err := resolveHub(s, node)
			if err != nil {
				return step, err
			}
			switch {
			case done:
				next, ok = g.Target(node.Slug, graph.PortDone)
				if !ok {
					return step, fmt.Errorf("%w: %s.%s", ErrDanglingPort, node.Slug, graph.PortDone)
				}
			case offer != nil:
				// The team must pick. Park here and let the player act.
				step.To = node.Slug
				step.Offer = offer
				step.CanFinish = satisfied
				return step, nil
			default:
				step.Entered = append(step.Entered, BranchRef{Hub: node.Slug, Port: chosen})
				next, ok = g.Target(node.Slug, chosen)
				if !ok {
					return step, fmt.Errorf("%w: %s.%s", ErrDanglingPort, node.Slug, chosen)
				}
			}

		case graph.Start:
			// Reaching Start again would loop forever.
			return step, fmt.Errorf("%w: edge leads back to start", ErrLoop)

		default:
			return step, fmt.Errorf("unknown node type %q at %s", node.Type, node.Slug)
		}
	}
	return step, fmt.Errorf("%w: exceeded %d hops", ErrLoop, maxHops)
}

// EnterBranch is the follow-up to a Step carrying an Offer: the team has
// picked, so walk into that branch.
//
// PortDone is a legal choice when the Step reported CanFinish — that is
// the player electing to move on rather than do another optional branch.
func EnterBranch(g *graph.Graph, s State, hubSlug, port string) (Step, error) {
	hub, ok := g.Node(hubSlug)
	if !ok || hub.Type != graph.Hub {
		return Step{}, fmt.Errorf("%q is not a hub", hubSlug)
	}

	home := hub.HubCfg != nil && hub.HubCfg.Home
	if port == graph.PortDone {
		if home {
			return Step{}, fmt.Errorf("hub %q is a home screen and never finishes", hubSlug)
		}
		if hub.HubCfg == nil || s.CompletedBranches(hubSlug) < hub.HubCfg.RequiredBranches() {
			return Step{}, fmt.Errorf("hub %q is not finished yet", hubSlug)
		}
		return advanceFrom(g, s, hubSlug, graph.PortDone)
	}

	b, ok := hub.Branch(port)
	if !ok {
		return Step{}, fmt.Errorf("hub %q has no branch %q", hubSlug, port)
	}
	// Re-check rather than trusting the client: the offer was computed
	// on a previous request and the team's state may have moved on.
	if s.BranchDone(hubSlug, port) && !home {
		return Step{}, fmt.Errorf("branch %q is already done", port)
	}
	if !branchOpen(s, hub, b) {
		return Step{}, fmt.Errorf("branch %q is not available", port)
	}
	step, err := advanceFrom(g, s, hubSlug, port)
	// Record the entry even on error, so the caller's view of what was
	// attempted matches what the walk did before it failed.
	step.Entered = append([]BranchRef{{Hub: hubSlug, Port: port}}, step.Entered...)
	return step, err
}

// resolveHub decides what happens at a hub: done, a single chosen
// branch, or an offer the player must answer.
//
// The subtle case is a hub whose Required is already met but which still
// has branches open. "Do at least three of the eight stations" means
// exactly that — three is enough to move on, and a team with time to
// spare may do more. So under PolicyChoice the player is offered both,
// and satisfied is returned alongside the offer. Under the automatic
// policies the player is not choosing anything, so a met Required goes
// straight out of Done.
func resolveHub(s State, hub graph.Node) (chosen string, offer []graph.Branch, satisfied, done bool, err error) {
	if hub.HubCfg == nil {
		return "", nil, false, false, fmt.Errorf("hub %q has no configuration", hub.Slug)
	}
	cfg := hub.HubCfg

	// A home screen offers every open branch, visited or not, and never
	// finishes: a team goes on through a branch that ends the adventure.
	if cfg.Home {
		var open []graph.Branch
		for _, b := range cfg.Branches {
			if branchOpen(s, hub, b) {
				open = append(open, b)
			}
		}
		if len(open) == 0 {
			if branchesWait(s, hub) {
				return "", []graph.Branch{}, false, false, nil
			}
			return "", nil, false, false, fmt.Errorf("%w: hub %s", ErrStuck, hub.Slug)
		}
		return "", open, false, false, nil
	}

	met := s.CompletedBranches(hub.Slug) >= cfg.RequiredBranches()

	open := make([]graph.Branch, 0, len(cfg.Branches))
	for _, b := range cfg.Branches {
		if s.BranchDone(hub.Slug, b.Port) {
			continue
		}
		if branchOpen(s, hub, b) {
			open = append(open, b)
		}
	}

	// Nothing left to offer: leave, whether because the requirement is
	// met or because every remaining branch is permanently shut. The
	// second case matters — a team whose last open branch became
	// unreachable must not be stranded in a park. A branch waiting on the
	// players to get in place is not shut: the team stays, with nothing
	// to choose yet, until they do.
	if len(open) == 0 {
		if !met && branchesWait(s, hub) {
			return "", []graph.Branch{}, false, false, nil
		}
		if met || allBranchesAccountedFor(s, hub) {
			return "", nil, met, true, nil
		}
		return "", nil, false, false, fmt.Errorf("%w: hub %s", ErrStuck, hub.Slug)
	}

	// Enough done, and the player is not choosing: move on.
	if met && cfg.Policy != graph.PolicyChoice {
		return "", nil, true, true, nil
	}

	switch cfg.Policy {
	case graph.PolicyChoice:
		// When met, the caller also offers "move on"; see Step.CanFinish.
		return "", open, met, false, nil

	case graph.PolicySequence, "":
		return open[0].Port, nil, false, false, nil

	case graph.PolicyShuffle:
		// The declared branches rotated by the team's slot, taking the
		// first one not yet done. Rotating the full declared list — not
		// just the open ones — keeps each team's order fixed as branches
		// complete.
		// With N teams and N branches every branch is someone's first,
		// which is what stops eight teams walking to the same station at
		// the same minute.
		n := len(cfg.Branches)
		off := shift(s.Slot(), n)
		for i := range n {
			b := cfg.Branches[(off+i)%n]
			for _, o := range open {
				if o.Port == b.Port {
					return o.Port, nil, false, false, nil
				}
			}
		}
		return open[0].Port, nil, false, false, nil

	case graph.PolicyRandom:
		// Seeded by (hub, team) rather than a real RNG so a run
		// reproduces exactly in the simulator and in the recap.
		return open[int(seed(hub.Slug, s.Slot())%uint64(len(open)))].Port, nil, false, false, nil

	case graph.PolicySpread:
		return leastOccupied(s, hub.Slug, open).Port, nil, false, false, nil

	case graph.PolicyCapacity:
		// First branch still under its limit. Legacy fell through to
		// the last branch on overflow rather than blocking, and that is
		// the right call: a team standing in a park must always have
		// somewhere to go, even if it means a queue.
		for _, b := range open {
			if b.Capacity <= 0 || s.Occupancy(hub.Slug, b.Port) < b.Capacity {
				return b.Port, nil, false, false, nil
			}
		}
		return open[len(open)-1].Port, nil, false, false, nil
	}

	return "", nil, false, false, fmt.Errorf("hub %s has unknown policy %q", hub.Slug, cfg.Policy)
}

// branchOpen reports whether a branch's condition currently holds.
func branchOpen(s cond.State, _ graph.Node, b graph.Branch) bool {
	return cond.Eval(b.Condition, s)
}

// branchesWait reports whether a branch not yet done is shut only until
// the team's players get in place: see cond.Waits.
func branchesWait(s State, hub graph.Node) bool {
	home := hub.HubCfg.Home
	for _, b := range hub.HubCfg.Branches {
		if (home || !s.BranchDone(hub.Slug, b.Port)) && !branchOpen(s, hub, b) && cond.Waits(b.Condition, s) {
			return true
		}
	}
	return false
}

// allBranchesAccountedFor reports whether every branch is either done or
// permanently shut, meaning there is nothing left for the team to do
// here even though Required was never reached.
func allBranchesAccountedFor(s State, hub graph.Node) bool {
	for _, b := range hub.HubCfg.Branches {
		if !s.BranchDone(hub.Slug, b.Port) && branchOpen(s, hub, b) {
			return false
		}
	}
	return true
}

func leastOccupied(s State, hubSlug string, open []graph.Branch) graph.Branch {
	best := open[0]
	bestN := s.Occupancy(hubSlug, best.Port)
	for _, b := range open[1:] {
		if n := s.Occupancy(hubSlug, b.Port); n < bestN {
			best, bestN = b, n
		}
	}
	return best
}

// shift is the cyclic offset for a team slot, normalised for negative
// slots the way permutation.go does.
func shift(slot, n int) int {
	if n <= 0 {
		return 0
	}
	return ((slot % n) + n) % n
}

func seed(slug string, slot int) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(slug))
	_, _ = fmt.Fprintf(h, ":%d", slot)
	return h.Sum64()
}

// switchPort picks a switch's exit: the first case whose matchers accept
// the answer, or PortElse.
func switchPort(s State, n graph.Node) string {
	if n.SwitchCfg == nil {
		return graph.PortElse
	}
	answer, ok := s.LastAnswer()
	if n.SwitchCfg.From != "" {
		answer, ok = s.AnswerTo(n.SwitchCfg.From)
	}
	for _, c := range n.SwitchCfg.Cases {
		for _, ref := range c.Answers {
			node, id, _ := strings.Cut(ref, "/")
			if id != "" && s.MatchedAnswer(node) == id {
				return c.Port
			}
		}
		if ok && len(c.Matchers) > 0 && match.Any(c.Matchers, answer).OK {
			return c.Port
		}
	}
	return graph.PortElse
}
