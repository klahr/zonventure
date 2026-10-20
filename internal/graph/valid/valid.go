// Package valid checks an authored graph before it is saved.
//
// This is new capability, not a port. The classic importer's dry run ran
// the real importer inside a transaction and rolled it back, so it
// answered exactly one question — "would this import?" — and checked no
// structure at all. It did not even re-verify that an accept pattern
// compiles, so a broken regex imports cleanly and then silently rejects
// every answer a player types.
//
// A graph makes the real questions answerable statically. Every check
// here runs without executing anything and without a database, so a
// mistake is a 400 on the author's save button rather than a team
// standing in a park in front of a gate that can never open.
package valid

import (
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
)

// Severity separates "this cannot run" from "this is probably not what
// you meant". Only errors block a save; warnings are shown and ignored,
// because an author mid-edit should not be nagged into a corner.
type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
)

// Issue is one finding, addressed to a place on the canvas so the editor
// can highlight it.
type Issue struct {
	Severity Severity `json:"severity"`
	// Code is stable and machine-readable; the editor localises from it
	// rather than parsing Message.
	Code    string `json:"code"`
	Node    string `json:"node,omitempty"`
	Port    string `json:"port,omitempty"`
	Message string `json:"message"`
}

func (i Issue) String() string {
	where := i.Node
	if i.Port != "" {
		where += "." + i.Port
	}
	if where == "" {
		return fmt.Sprintf("[%s] %s", i.Code, i.Message)
	}
	return fmt.Sprintf("[%s] %s: %s", i.Code, where, i.Message)
}

// Report is the outcome of validating a graph.
type Report struct {
	Issues []Issue `json:"issues"`
}

// OK reports whether the graph may be saved and run.
func (r Report) OK() bool { return !r.HasErrors() }

// HasErrors reports whether any issue blocks the graph.
func (r Report) HasErrors() bool {
	for _, i := range r.Issues {
		if i.Severity == Error {
			return true
		}
	}
	return false
}

// Errors returns only the blocking issues.
func (r Report) Errors() []Issue {
	var out []Issue
	for _, i := range r.Issues {
		if i.Severity == Error {
			out = append(out, i)
		}
	}
	return out
}

type reporter struct{ issues []Issue }

func (r *reporter) err(code, node, port, format string, a ...any) {
	r.issues = append(r.issues, Issue{Error, code, node, port, fmt.Sprintf(format, a...)})
}

func (r *reporter) warn(code, node, port, format string, a ...any) {
	r.issues = append(r.issues, Issue{Warning, code, node, port, fmt.Sprintf(format, a...)})
}

// Check validates a graph completely. It always runs every check rather
// than stopping at the first failure: an author fixing one problem at a
// time, with a round trip each, is a miserable way to write an
// adventure.
func Check(g *graph.Graph) Report {
	r := &reporter{}

	checkSlugs(r, g)
	checkNodeConfigs(r, g)
	checkEdges(r, g)
	checkPortsConnected(r, g)
	checkReferences(r, g)
	checkMatchers(r, g)
	checkReachability(r, g)
	checkTermination(r, g)
	checkProgressFreeCycles(r, g)
	checkTimeoutKeys(r, g)
	checkResources(r, g)
	checkBlocks(r, g)
	checkMedia(r, g)
	checkTimeLimit(r, g)
	checkPlayers(r, g)
	checkTranslations(r, g)
	checkHostEdits(r, g)
	checkPrintMaterials(r, g)

	sort.SliceStable(r.issues, func(i, j int) bool {
		if r.issues[i].Severity != r.issues[j].Severity {
			return r.issues[i].Severity == Error
		}
		return r.issues[i].Node < r.issues[j].Node
	})
	return Report{Issues: r.issues}
}

func checkSlugs(r *reporter, g *graph.Graph) {
	seen := map[string]bool{}
	starts, ends := 0, 0
	for _, n := range g.Nodes {
		if n.Slug == "" {
			r.err("node.slug.empty", "", "", "a %s node has no slug", n.Type)
			continue
		}
		if seen[n.Slug] {
			r.err("node.slug.duplicate", n.Slug, "", "two nodes share the slug %q", n.Slug)
		}
		seen[n.Slug] = true

		switch n.Type {
		case graph.Start:
			starts++
		case graph.End:
			ends++
			if n.EndCfg == nil || n.EndCfg.Outcome == "" {
				r.warn("end.outcome.unset", n.Slug, "", "choose whether this ending is a success or a failure; until then players are told they made it")
			}
		}
	}

	if starts == 0 {
		r.err("graph.start.missing", "", "", "the adventure has no start node")
	}
	if ends == 0 {
		r.err("graph.end.missing", "", "", "the adventure has no end node, so no team can finish it")
	}
}

// checkNodeConfigs catches a node whose typed configuration is missing
// or belongs to a different type — the shape a hand-edited import can
// produce, and one the runtime cannot act on.
func checkNodeConfigs(r *reporter, g *graph.Graph) {
	var owners map[string]string // g.OwnerHubs(), once a puzzle needs it
	for _, n := range g.Nodes {
		switch n.Type {
		case graph.Puzzle:
			if n.PuzzleCfg == nil {
				r.err("node.config.missing", n.Slug, "", "puzzle has no configuration")
				continue
			}
			if len(n.PuzzleCfg.Outputs) == 0 {
				r.err("puzzle.outputs.none", n.Slug, "", "the puzzle has no ways out, so a team that solves it has nowhere to go")
			}
			if n.PuzzleCfg.Bar != nil {
				checkBar(r, n)
			}
			if n.PuzzleCfg.Answer != nil {
				if owners == nil {
					owners = g.OwnerHubs()
				}
				checkAnswer(r, g, n, owners)
			}
			ports := map[string]bool{}
			answers, missing := 0, 0
			for _, o := range n.PuzzleCfg.Outputs {
				if o.Port == "" {
					r.err("output.port.empty", n.Slug, "", "an output has no port name")
					continue
				}
				if ports[o.Port] {
					r.err("output.port.duplicate", n.Slug, o.Port, "two outputs share the port %q", o.Port)
				}
				ports[o.Port] = true

				switch o.Trigger {
				case graph.OnAnswer:
					answers++
					if len(o.Matchers) == 0 && n.PuzzleCfg.UsesMatchers() {
						r.err("output.matchers.none", n.Slug, o.Port,
							"no answer is set, so no team can take this route")
					}
				case graph.OnAttempts:
					if o.AfterAttempts <= 0 {
						r.err("output.attempts.threshold", n.Slug, o.Port,
							"an attempts output needs a threshold above zero")
					}
				case graph.OnTimeout:
					if n.PuzzleCfg.TimeLimitSeconds <= 0 {
						r.err("output.timeout.nolimit", n.Slug, o.Port,
							"output fires on timeout but the puzzle has no time limit")
					}
				case graph.OnSkip:
					if n.PuzzleCfg.SkipAfterGuesses <= 0 && n.PuzzleCfg.SkipAfterSeconds <= 0 {
						r.err("output.skip.nothreshold", n.Slug, o.Port,
							"output fires on skip but the puzzle sets no skip thresholds")
					}
				case graph.OnMissing:
					if n.PuzzleCfg.Kind() != graph.AnswerItems {
						r.err("output.missing.kind", n.Slug, o.Port,
							"only a puzzle answered with items can be left for lack of them")
					} else if missing++; missing == 2 {
						r.warn("output.missing.second", n.Slug, o.Port,
							"the puzzle already has a route for a team without the items; this one is never taken")
					}
				default:
					r.err("output.trigger.unknown", n.Slug, o.Port, "unknown trigger %q", o.Trigger)
				}
			}
			if answers == 0 && len(n.PuzzleCfg.Outputs) > 0 {
				// Legal — a pure timeout/skip puzzle — but almost always
				// an author who forgot to set the trigger.
				r.warn("puzzle.noanswer", n.Slug, "", "puzzle has no answer output; players cannot solve it, only time out or skip")
			}

		case graph.Hub:
			if n.HubCfg == nil {
				r.err("node.config.missing", n.Slug, "", "hub has no configuration")
				continue
			}
			if len(n.HubCfg.Branches) == 0 {
				r.err("hub.branches.none", n.Slug, "", "hub has no branches")
			}
			ports := map[string]bool{}
			for _, b := range n.HubCfg.Branches {
				if b.Port == "" {
					r.err("branch.port.empty", n.Slug, "", "a branch has no port name")
					continue
				}
				if b.Port == graph.PortDone {
					r.err("branch.port.reserved", n.Slug, b.Port, "%q is reserved for the hub's own exit", graph.PortDone)
				}
				if ports[b.Port] {
					r.err("branch.port.duplicate", n.Slug, b.Port, "two branches share the port %q", b.Port)
				}
				ports[b.Port] = true
				if b.Capacity < 0 {
					r.err("branch.capacity.negative", n.Slug, b.Port, "capacity cannot be negative")
				}
			}
			if n.HubCfg.Home && n.HubCfg.Policy != graph.PolicyChoice {
				r.err("hub.home.policy", n.Slug, "", "a home screen lets the team choose; its policy must be choice")
			}
			if !n.HubCfg.Home && n.HubCfg.Required > len(n.HubCfg.Branches) {
				r.err("hub.required.exceeds", n.Slug, "",
					"hub requires %d branches but only has %d, so it can never complete",
					n.HubCfg.Required, len(n.HubCfg.Branches))
			}
			if n.HubCfg.Policy == graph.PolicyCapacity {
				any := false
				for _, b := range n.HubCfg.Branches {
					if b.Capacity > 0 {
						any = true
					}
				}
				if !any {
					r.warn("hub.capacity.unset", n.Slug, "",
						"hub uses the capacity policy but no branch sets one, so it behaves like sequence")
				}
			}

		case graph.Gate:
			if n.GateCfg == nil {
				r.err("node.config.missing", n.Slug, "", "gate has no configuration")
			}

		case graph.Effect:
			if n.EffectCfg == nil {
				r.err("node.config.missing", n.Slug, "", "effect has no configuration")
				continue
			}
			checkEffect(r, g, n)

		case graph.Switch:
			checkSwitch(r, g, n)

		case graph.Start, graph.Story, graph.End:
			// No required configuration.

		default:
			r.err("node.type.unknown", n.Slug, "", "unknown node type %q", n.Type)
		}
	}
}

// checkSwitch checks a switch's cases and its source.
func checkSwitch(r *reporter, g *graph.Graph, n graph.Node) {
	cfg := n.SwitchCfg
	if cfg == nil || len(cfg.Cases) == 0 {
		r.err("switch.cases.none", n.Slug, "", "crossroads has no paths, so every team takes \"any other answer\"")
		return
	}
	ports := map[string]bool{}
	for _, c := range cfg.Cases {
		switch {
		case c.Port == "":
			r.err("case.port.empty", n.Slug, "", "a path has no name")
		case c.Port == graph.PortElse:
			r.err("case.port.reserved", n.Slug, c.Port, "%q is reserved for \"any other answer\"", graph.PortElse)
		case ports[c.Port]:
			r.err("case.port.duplicate", n.Slug, c.Port, "two paths share the name %q", c.Port)
		}
		ports[c.Port] = true
		if len(c.Matchers) == 0 && len(c.Answers) == 0 {
			r.err("case.matchers.none", n.Slug, c.Port, "path has no answers, so no team can take it")
		}
		for _, ref := range c.Answers {
			if !answerExists(g, ref) {
				r.err("case.answer.unknown", n.Slug, c.Port, "path refers to an answer that no longer exists; pick it again")
			}
		}
	}
	if cfg.From != "" {
		src, ok := g.Node(cfg.From)
		if !ok {
			r.err("switch.from.unknown", n.Slug, "", "crossroads reads the answer to %q, which is not a step", cfg.From)
		} else if src.Type != graph.Puzzle {
			r.err("switch.from.notpuzzle", n.Slug, "", "crossroads reads the answer to %q, which is a %s, not a puzzle", cfg.From, src.Type)
		}
	}
}

// answerExists reports whether a "node/matcher-id" reference names an
// accepted answer on a puzzle in the graph.
func answerExists(g *graph.Graph, ref string) bool {
	node, id, _ := strings.Cut(ref, "/")
	n, ok := g.Node(node)
	if !ok || n.PuzzleCfg == nil || id == "" {
		return false
	}
	for _, o := range n.PuzzleCfg.Outputs {
		for _, m := range o.Matchers {
			if m.ID == id {
				return true
			}
		}
	}
	return false
}

func checkEffect(r *reporter, g *graph.Graph, n graph.Node) {
	e := n.EffectCfg
	switch e.Kind {
	case graph.GrantItem:
		if e.Item == "" {
			r.err("effect.item.missing", n.Slug, "", "grant effect names no item")
		}
	case graph.AdjustScore:
		if e.Points == 0 {
			r.warn("effect.score.zero", n.Slug, "", "score effect awards nothing")
		}
	case graph.SetCounter:
		if e.Counter == "" {
			r.err("effect.counter.missing", n.Slug, "", "counter effect names no counter")
		}
	case graph.UnlockJournal:
		if e.JournalEntry == "" {
			r.err("effect.journal.missing", n.Slug, "", "journal effect names no entry")
		}
	case graph.SendEmail:
		if strings.TrimSpace(e.Subject) == "" || strings.TrimSpace(e.Body) == "" {
			r.err("effect.email.missing", n.Slug, "", "email effect needs a subject and a body")
		}
		for _, m := range placeholderRe.FindAllStringSubmatch(e.Subject+"\n"+e.Body, -1) {
			if !slices.Contains(graph.EmailPlaceholders, m[1]) {
				r.warn("effect.email.placeholder", n.Slug, "", "email uses unknown placeholder {{%s}}; it is sent as written", m[1])
			}
		}
	case graph.CallWebhook:
		checkWebhook(r, n.Slug, *e)
	case graph.SetTheme:
		if e.Theme == "" {
			r.err("effect.theme.missing", n.Slug, "", "theme effect names no theme")
		} else if !slices.Contains(graph.Themes, e.Theme) && g.Theme(e.Theme) == nil {
			r.err("effect.theme.unknown", n.Slug, "", "theme %q must be one of %s, or one of the adventure's saved themes", e.Theme, strings.Join(graph.Themes, ", "))
		}
	default:
		r.err("effect.kind.unknown", n.Slug, "", "unknown effect kind %q", e.Kind)
	}
}

func checkEdges(r *reporter, g *graph.Graph) {
	nodes := map[string]graph.Node{}
	for _, n := range g.Nodes {
		nodes[n.Slug] = n
	}

	used := map[string]bool{}
	for _, e := range g.Edges {
		from, ok := nodes[e.From]
		if !ok {
			r.err("edge.from.unknown", e.From, e.FromPort, "edge starts at a node that does not exist")
			continue
		}
		if _, ok := nodes[e.To]; !ok {
			r.err("edge.to.unknown", e.From, e.FromPort, "edge leads to %q, which does not exist", e.To)
		}

		valid := false
		for _, p := range from.Ports() {
			if p == e.FromPort {
				valid = true
				break
			}
		}
		if !valid {
			r.err("edge.port.unknown", e.From, e.FromPort, "%s has no output port %q", from.Type, e.FromPort)
		}

		// One edge per port. Two would make the destination ambiguous,
		// and the interpreter would silently take whichever came first.
		key := e.From + "\x00" + e.FromPort
		if used[key] {
			r.err("edge.port.duplicate", e.From, e.FromPort, "port has more than one outgoing edge")
		}
		used[key] = true

		if nodes[e.To].Type == graph.Start {
			r.err("edge.to.start", e.From, e.FromPort, "an edge leads back to the start node")
		}
	}
}

// checkPortsConnected is the check that most directly prevents a team
// being stranded: an output with no edge is a dead end the player can
// reach but not leave.
func checkPortsConnected(r *reporter, g *graph.Graph) {
	connected := map[string]bool{}
	for _, e := range g.Edges {
		connected[e.From+"\x00"+e.FromPort] = true
	}
	for _, n := range g.Nodes {
		for _, p := range n.Ports() {
			if !connected[n.Slug+"\x00"+p] {
				r.err("port.unconnected", n.Slug, p, "leads nowhere yet — connect it to the next step")
			}
		}
	}
}

// checkReferences resolves every slug a condition or a node config
// mentions. A gate pointing at a deleted puzzle is the classic way to
// build an adventure that cannot be finished.
func checkReferences(r *reporter, g *graph.Graph) {
	nodes := map[string]bool{}
	for _, n := range g.Nodes {
		nodes[n.Slug] = true
	}
	zones := slugSet(len(g.Zones), func(i int) string { return g.Zones[i].Slug })
	items := slugSet(len(g.Items), func(i int) string { return g.Items[i].Slug })
	counters := slugSet(len(g.Counters), func(i int) string { return g.Counters[i].Slug })
	journal := slugSet(len(g.Journal), func(i int) string { return g.Journal[i].Slug })
	roles := slugSet(len(g.Roles), func(i int) string { return g.Roles[i].Slug })

	checkPredicate := func(slug string, p cond.Predicate) {
		refs := cond.Collect(p)
		for _, ref := range refs.Nodes {
			if !nodes[ref] {
				r.err("ref.node.unknown", slug, "", "condition refers to node %q, which does not exist", ref)
			}
		}
		for _, ref := range refs.Items {
			if !items[ref] {
				r.err("ref.item.unknown", slug, "", "condition refers to item %q, which is not declared", ref)
			}
		}
		for _, ref := range refs.Counters {
			if !counters[ref] {
				r.err("ref.counter.unknown", slug, "", "condition refers to counter %q, which is not declared", ref)
			}
		}
		for _, ref := range refs.Zones {
			if !zones[ref] {
				r.err("ref.zone.unknown", slug, "", "condition refers to zone %q, which is not declared", ref)
			}
		}
		for _, ref := range refs.Roles {
			if !roles[ref] {
				r.err("ref.role.unknown", slug, "", "condition refers to role %q, which is not declared", ref)
			}
		}
	}

	// A gate or a branch decides for the whole team, so it cannot ask
	// about "this player" — where they are, or their role: which player
	// would that be?
	checkTeamPredicate := func(slug, port string, p cond.Predicate) {
		if cond.ForPlayer(p) {
			r.err("cond.player.team", slug, port, "only a block's show condition can ask about this player, where they are or which role they hold; use players_in_zone for the team")
		}
	}

	kitOf := map[string]string{}
	defaults := 0
	for _, ro := range g.Roles {
		if ro.Default {
			defaults++
			switch {
			case ro.Secret:
				r.err("role.default.secret", "", "", "role %q cannot be both the saboteur's and everyone else's", ro.Slug)
			case len(ro.Kit) > 0:
				r.err("role.default.kit", "", "", "role %q is everyone else's, so its kit would have no one owner; give the items to a role one player holds", ro.Slug)
			}
			if defaults == 2 {
				r.err("role.default.many", "", "", "role %q is a second default role; only one role is everyone else's", ro.Slug)
			}
		}
		if ro.Secret && len(ro.Kit) > 0 {
			r.err("role.secret.kit", "", "", "secret role %q brings items, and everyone's backpack would say whose; give it none", ro.Slug)
		}
		if ro.Cover != "" && !ro.Secret {
			r.err("role.cover.public", "", "", "role %q has a cover, which only a secret role has: it is what the others are told in its place", ro.Slug)
		}
		for i, goal := range ro.Goals {
			if !ro.Secret {
				r.err("role.goals.public", "", "", "role %q has goals, which only a secret role has", ro.Slug)
				break
			}
			checkPredicate("", goal.When)
			if cond.ForPlayer(goal.When) {
				r.err("cond.player.team", "", "", "goal %d of %q asks about one player; a goal is checked against the whole team", i+1, ro.Slug)
			}
		}
		for _, it := range ro.Kit {
			if !items[it] {
				r.err("ref.item.unknown", "", "", "role %q brings item %q, which is not declared", ro.Slug, it)
			}
			if other, ok := kitOf[it]; ok {
				r.err("role.kit.shared", "", "", "item %q is in the kits of both %q and %q; one role's at most", it, other, ro.Slug)
			}
			kitOf[it] = ro.Slug
		}
	}

	for _, n := range g.Nodes {
		switch n.Type {
		case graph.Gate:
			if n.GateCfg != nil {
				checkPredicate(n.Slug, n.GateCfg.Condition)
				checkTeamPredicate(n.Slug, "", n.GateCfg.Condition)
			}
		case graph.Hub:
			if n.HubCfg != nil {
				for _, b := range n.HubCfg.Branches {
					checkPredicate(n.Slug, b.Condition)
					checkTeamPredicate(n.Slug, b.Port, b.Condition)
				}
			}
		case graph.Puzzle:
			if n.PuzzleCfg == nil {
				continue
			}
			if z := n.PuzzleCfg.Zone; z != "" && !zones[z] {
				r.err("ref.zone.unknown", n.Slug, "", "puzzle is gated on zone %q, which is not declared", z)
			}
			for _, o := range n.PuzzleCfg.Outputs {
				if o.Zone != "" && !zones[o.Zone] {
					r.err("ref.zone.unknown", n.Slug, o.Port, "output is constrained to zone %q, which is not declared", o.Zone)
				}
			}
			if by := n.PuzzleCfg.By; by != "" {
				if !roles[by] {
					r.err("ref.role.unknown", n.Slug, "", "puzzle is answered by role %q, which is not declared", by)
				}
				if ro, _ := g.Role(by); ro.Secret {
					r.err("puzzle.by.secret", n.Slug, "", "every phone would say only the %s can answer it, giving them away; use a sabotage block instead", g.RoleName(by))
				}
				if n.PuzzleCfg.Unanimous || n.PuzzleCfg.Kind() == graph.AnswerKeys {
					r.err("puzzle.by.together", n.Slug, "", "a puzzle every player answers together cannot also be one role's alone")
				}
			}
			if n.PuzzleCfg.Unanimous && !n.PuzzleCfg.UsesMatchers() {
				r.err("puzzle.unanimous.kind", n.Slug, "", "only an answer the routes' accepted answers decide can be unanimous: a typed answer, a scan, a lock or an unmarked choice")
			}
			if n.PuzzleCfg.Quiet && !n.PuzzleCfg.UsesMatchers() {
				r.err("puzzle.quiet.kind", n.Slug, "", "only an answer the routes' accepted answers decide can just move on: a typed answer, a scan, a lock or an unmarked choice")
			}
			if n.PuzzleCfg.Quiet && n.PuzzleCfg.Unanimous {
				r.err("puzzle.quiet.unanimous", n.Slug, "", "a step that moves on at any answer cannot also wait for every player to agree")
			}
			if a := n.PuzzleCfg.Answer; a != nil && a.Kind == graph.AnswerKeys {
				for _, z := range a.Posts {
					if !zones[z] {
						r.err("ref.zone.unknown", n.Slug, "", "a key is turned at zone %q, which is not declared", z)
					}
				}
				if len(a.PostRoles) > len(a.Posts) {
					r.err("keys.post_roles", n.Slug, "", "a two-key launch names a role for %d posts but has %d", len(a.PostRoles), len(a.Posts))
				}
				seen := map[string]bool{}
				for _, role := range a.PostRoles {
					switch ro, _ := g.Role(role); {
					case role == "":
					case !roles[role]:
						r.err("ref.role.unknown", n.Slug, "", "a key is turned by role %q, which is not declared", role)
					case ro.Default:
						r.err("keys.post_roles.default", n.Slug, "", "the %s is everyone else's, not one player to send to a post; leave the post open to anyone", g.RoleName(role))
					case ro.Secret:
						r.err("keys.post_roles.secret", n.Slug, "", "every phone would say where the %s turns their key, giving them away", g.RoleName(role))
					case seen[role]:
						r.err("keys.post_roles.twice", n.Slug, "", "the %s cannot turn keys at two posts at once", g.RoleName(role))
					}
					seen[role] = true
				}
			}
			if a := n.PuzzleCfg.Answer; a != nil && a.Kind == graph.AnswerItems {
				for i, s := range a.Slots {
					if s.Item != "" && !items[s.Item] {
						r.err("ref.item.unknown", n.Slug, "", "slot %d wants item %q, which is not declared", i+1, s.Item)
					}
				}
			}
		case graph.Story:
			if n.StoryCfg != nil && n.StoryCfg.Zone != "" && !zones[n.StoryCfg.Zone] {
				r.err("ref.zone.unknown", n.Slug, "", "story is gated on zone %q, which is not declared", n.StoryCfg.Zone)
			}
		case graph.Start:
			if n.StartCfg != nil && n.StartCfg.Zone != "" && !zones[n.StartCfg.Zone] {
				r.err("ref.zone.unknown", n.Slug, "", "start is at zone %q, which is not declared", n.StartCfg.Zone)
			}
		case graph.Effect:
			if n.EffectCfg == nil {
				continue
			}
			if s := n.EffectCfg.Item; s != "" && !items[s] {
				r.err("ref.item.unknown", n.Slug, "", "effect grants item %q, which is not declared", s)
			}
			if s := n.EffectCfg.Counter; s != "" && !counters[s] {
				r.err("ref.counter.unknown", n.Slug, "", "effect changes counter %q, which is not declared", s)
			}
			if s := n.EffectCfg.JournalEntry; s != "" && !journal[s] {
				r.err("ref.journal.unknown", n.Slug, "", "effect unlocks journal entry %q, which is not declared", s)
			}
		}
		for _, s := range n.Unlocks {
			if !journal[s] {
				r.err("ref.journal.unknown", n.Slug, "", "step unlocks journal entry %q, which is not declared", s)
			}
		}
		for _, b := range n.AllBlocks() {
			if b.Show != nil {
				checkPredicate(n.Slug, b.Show.When)
			}
		}
	}
}

// checkMatchers compiles every regex and sanity-checks the numeric
// matchers. Today's importer skips this entirely, so a pattern that
// cannot compile is accepted and then rejects every answer at match
// time, with nothing in any log to say why.
func checkMatchers(r *reporter, g *graph.Graph) {
	for _, n := range g.Nodes {
		// Puzzle outputs and switch cases carry the same matchers.
		type out struct {
			Port     string
			Matchers []match.Matcher
		}
		var outs []out
		if n.PuzzleCfg != nil {
			for _, o := range n.PuzzleCfg.Outputs {
				outs = append(outs, out{o.Port, o.Matchers})
			}
		}
		if n.SwitchCfg != nil {
			for _, c := range n.SwitchCfg.Cases {
				outs = append(outs, out{c.Port, c.Matchers})
			}
		}
		ids := map[string]bool{}
		for _, o := range outs {
			for _, m := range o.Matchers {
				if m.ID != "" {
					if ids[m.ID] {
						r.err("matcher.id.duplicate", n.Slug, o.Port, "two answers share the id %q; a crossroads could not tell them apart", m.ID)
					}
					ids[m.ID] = true
				}
				switch m.Kind {
				case match.Regex:
					if _, err := match.CompileRegex(m.Value); err != nil {
						r.err("matcher.regex.invalid", n.Slug, o.Port, "pattern does not compile: %v", err)
					}
				case match.Near:
					if m.Tolerance <= 0 {
						r.err("matcher.near.tolerance", n.Slug, o.Port, "a \"number near\" answer needs a tolerance above zero")
					}
				case match.Interval:
					if m.Low > m.High {
						r.warn("matcher.interval.reversed", n.Slug, o.Port, "interval bounds are reversed; they will be swapped")
					}
				case match.Set:
					if len(m.Members) == 0 {
						r.err("matcher.set.empty", n.Slug, o.Port, "a \"several of a list\" answer needs its list")
					}
					if m.Required > len(m.Members) {
						r.err("matcher.set.required", n.Slug, o.Port,
							"set requires %d of %d members, which can never be satisfied", m.Required, len(m.Members))
					}
				case match.Exact, match.Contains:
					if m.Value == "" {
						r.err("matcher.value.empty", n.Slug, o.Port, "an answer is empty — type what the team should give")
					}
				case match.Anything:
					// Legal, and evaluated last so it cannot shadow.
				default:
					r.err("matcher.kind.unknown", n.Slug, o.Port, "unknown kind of answer %q", m.Kind)
				}
			}
		}
	}
}

// checkReachability finds nodes no team can ever arrive at — usually a
// leftover from an edit, occasionally a branch the author forgot to wire.
func checkReachability(r *reporter, g *graph.Graph) {
	if _, ok := g.StartNode(); !ok {
		return // already reported
	}
	seen := reachableFromStarts(g)
	for _, n := range g.Nodes {
		if !seen[n.Slug] {
			r.warn("node.unreachable", n.Slug, "", "nothing leads here yet, so no team will reach this step")
		}
	}
}

// checkTermination finds nodes from which no end is reachable. A team
// that walks into one can never finish, and unlike an unconnected port
// nothing about the canvas makes it obvious.
func checkTermination(r *reporter, g *graph.Graph) {
	if _, ok := g.StartNode(); !ok {
		return
	}
	// Reverse edges, then flood from every End.
	rev := map[string][]string{}
	for _, e := range g.Edges {
		rev[e.To] = append(rev[e.To], e.From)
	}
	// Back is a way out too: a step inside a home screen's branch can
	// always return to it, with no edge drawn.
	for slug, hub := range g.OwnerHubs() {
		if h, _ := g.Node(hub); h.HubCfg != nil && h.HubCfg.Home {
			rev[hub] = append(rev[hub], slug)
		}
	}
	canEnd := map[string]bool{}
	var stack []string
	for _, n := range g.Nodes {
		if n.Type == graph.End {
			canEnd[n.Slug] = true
			stack = append(stack, n.Slug)
		}
	}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, prev := range rev[cur] {
			if !canEnd[prev] {
				canEnd[prev] = true
				stack = append(stack, prev)
			}
		}
	}

	reachable := reachableFromStarts(g)
	for _, n := range g.Nodes {
		if reachable[n.Slug] && !canEnd[n.Slug] {
			r.err("node.nodeadend", n.Slug, "", "no route from here reaches an end, so a team arriving here could never finish")
		}
	}
}

// checkProgressFreeCycles finds a loop a team would traverse forever.
//
// Hub loops are fine and expected — a branch returning to its hub is the
// whole point. What is not fine is a ring of nodes the player never acts
// on: effects and gates always traverse immediately, so a cycle made
// only of those spins until the request times out.
func checkProgressFreeCycles(r *reporter, g *graph.Graph) {
	passive := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Type == graph.Effect || n.Type == graph.Gate || n.Type == graph.Switch {
			passive[n.Slug] = true
		}
	}

	adj := map[string][]string{}
	for _, e := range g.Edges {
		if passive[e.From] && passive[e.To] {
			adj[e.From] = append(adj[e.From], e.To)
		}
	}

	const (
		white = 0 // unvisited
		grey  = 1 // on the current path
		black = 2 // finished
	)
	colour := map[string]int{}
	var reported bool

	var visit func(string)
	visit = func(cur string) {
		colour[cur] = grey
		for _, next := range adj[cur] {
			switch colour[next] {
			case grey:
				if !reported {
					r.err("graph.cycle.noprogress", next, "",
						"this node is part of a loop of gates and effects with nothing for a player to do, so a team would never leave it")
					reported = true
				}
			case white:
				visit(next)
			}
		}
		colour[cur] = black
	}

	for slug := range passive {
		if colour[slug] == white {
			visit(slug)
		}
	}
}

// checkTimeoutKeys finds a puzzle whose timeout leaves an item out of
// reach that a gate, a branch or an item puzzle needs. A puzzle left by
// its timeout is passed through that way every time the team comes back,
// so if every grant of the item lies behind its other outputs, a team
// that ran out of time can never get it, and goes round between the hub
// and the gate for good.
// MaxTeamPlayers is the most players a team may be bounded to.
const MaxTeamPlayers = 50

// checkPlayers checks the bounds on a team's players: in order, within
// reason, and enough to deal a secret role among.
func checkPlayers(r *reporter, g *graph.Graph) {
	lo, hi := g.MinPlayers, g.MaxPlayers
	if lo < 0 || hi < 0 || lo > MaxTeamPlayers || hi > MaxTeamPlayers {
		r.err("players.range", "", "", "a team is bounded to %d to %d players; each must be 0 (no bound) to %d", lo, hi, MaxTeamPlayers)
		return
	}
	if hi > 0 && lo > hi {
		r.err("players.order", "", "", "a team needs at least %d players but takes at most %d", lo, hi)
	}
	if len(g.SecretRoles()) > 0 && hi > 0 && hi < graph.MinSecretPlayers {
		r.warn("players.secret", "", "", "a secret role is dealt only to a team of %d or more, but a team here has at most %d", graph.MinSecretPlayers, hi)
	}
}

func checkTimeoutKeys(r *reporter, g *graph.Graph) {
	needed := map[string]bool{}
	grants := map[string][]string{}
	for _, n := range g.Nodes {
		var p []cond.Predicate
		switch {
		case n.GateCfg != nil:
			p = append(p, n.GateCfg.Condition)
		case n.HubCfg != nil:
			for _, b := range n.HubCfg.Branches {
				p = append(p, b.Condition)
			}
		case n.PuzzleCfg != nil && n.PuzzleCfg.Kind() == graph.AnswerItems:
			for it := range n.PuzzleCfg.Answer.Needs() {
				needed[it] = true
			}
		case n.EffectCfg != nil && n.EffectCfg.Kind == graph.GrantItem:
			grants[n.EffectCfg.Item] = append(grants[n.EffectCfg.Item], n.Slug)
		}
		for _, q := range p {
			for _, it := range cond.Collect(q).Items {
				needed[it] = true
			}
		}
	}
	kit := g.Kit()
	reachable := reachableFromStarts(g)
	for _, n := range g.Nodes {
		if n.PuzzleCfg == nil || !reachable[n.Slug] {
			continue
		}
		o, ok := outputFor(n, graph.OnTimeout)
		if !ok {
			continue
		}
		// The graph as it is for a team that ran out of time here: only
		// the timeout leads on.
		timedOut := *g
		timedOut.Edges = slices.DeleteFunc(slices.Clone(g.Edges), func(e graph.Edge) bool {
			return e.From == n.Slug && e.FromPort != o.Port
		})
		after := reachableFromStarts(&timedOut)
		for _, it := range slices.Sorted(maps.Keys(grants)) {
			if !needed[it] || kit[it] > 0 {
				continue
			}
			earnable, lost := false, false
			for _, at := range grants[it] {
				earnable = earnable || after[at]
				lost = lost || reachable[at]
			}
			if lost && !earnable {
				r.warn("output.timeout.unearnable", n.Slug, o.Port,
					"if time runs out here, the team can never get an item that is needed further on, and may go round in circles; send the timeout where the item can still be earned, or drop the time limit")
				break
			}
		}
	}
}

// outputFor is a puzzle's first output with the trigger.
func outputFor(n graph.Node, t graph.Trigger) (graph.Output, bool) {
	for _, o := range n.PuzzleCfg.Outputs {
		if o.Trigger == t {
			return o, true
		}
	}
	return graph.Output{}, false
}

func checkResources(r *reporter, g *graph.Graph) {
	checkUnique(r, "zone", len(g.Zones), func(i int) string { return g.Zones[i].Slug })
	checkUnique(r, "item", len(g.Items), func(i int) string { return g.Items[i].Slug })
	checkUnique(r, "counter", len(g.Counters), func(i int) string { return g.Counters[i].Slug })
	checkUnique(r, "journal entry", len(g.Journal), func(i int) string { return g.Journal[i].Slug })
	checkUnique(r, "theme", len(g.Themes), func(i int) string { return g.Themes[i].Slug })
	if g.MainTheme != "" && g.Theme(g.MainTheme) == nil {
		r.err("theme.main.unknown", "", "", "main theme %q is not one of the adventure's saved themes", g.MainTheme)
	}
	for _, th := range g.Themes {
		if slices.Contains(graph.Themes, th.Slug) {
			r.err("theme.slug.builtin", "", "", "saved theme %q has the name of a built-in theme; give it another slug", th.Slug)
		}
		if strings.TrimSpace(th.Name) == "" {
			r.err("theme.name.missing", "", "", "saved theme %q has no name", th.Slug)
		}
		if th.Starter != "" && !starterID.MatchString(th.Starter) {
			r.err("theme.starter.invalid", "", "", "saved theme %q: starter %q is not a starter theme's id", th.Slug, th.Starter)
		}
		if err := th.Look.Validate(); err != nil {
			r.err("theme.look.invalid", "", "", "saved theme %q: %s", th.Slug, err.Error())
		}
	}

	offGlobe := func(lat, lng float64) bool { return lat < -90 || lat > 90 || lng < -180 || lng > 180 }
	for _, z := range g.Zones {
		switch {
		case len(z.Polygon) > 0 && len(z.Polygon) < 3:
			r.err("zone.polygon", "", "", "zone %q has %d corners; an area needs at least three", z.Slug, len(z.Polygon))
		case !z.IsPolygon() && z.RadiusM <= 0:
			r.err("zone.radius", "", "", "zone %q has a radius of %g; a team could never be inside it", z.Slug, z.RadiusM)
		}
		if offGlobe(z.Lat, z.Lng) {
			r.err("zone.coords", "", "", "zone %q has coordinates off the globe (%g, %g)", z.Slug, z.Lat, z.Lng)
		}
		for _, p := range z.Polygon {
			if offGlobe(p[0], p[1]) {
				r.err("zone.coords", "", "", "zone %q has a corner off the globe (%g, %g)", z.Slug, p[0], p[1])
				break
			}
		}
	}
	for _, c := range g.Counters {
		if c.Min != nil && c.Max != nil && *c.Min > *c.Max {
			r.err("counter.bounds", "", "", "counter %q has a minimum above its maximum", c.Slug)
		}
	}
}

// checkMedia checks the shape of media references. Whether an id exists,
// and belongs to this adventure, needs the database: graphstore.Save
// checks that.
// checkAnswer checks a puzzle's way of answering, by its kind.
// minPostRadius is the smallest a two-key launch's post should be, in
// metres: what a phone's GPS in a street can be trusted to.
const minPostRadius = 40

// locksRoles reports whether some roles block locks the team's roles.
func locksRoles(g *graph.Graph) bool {
	for _, n := range g.Nodes {
		if slices.ContainsFunc(n.Blocks(), func(b graph.Block) bool { return b.Kind == graph.BlockRoles && b.Lock && !b.Disabled }) {
			return true
		}
	}
	return false
}

func checkAnswer(r *reporter, g *graph.Graph, n graph.Node, owners map[string]string) {
	a := n.PuzzleCfg.Answer
	switch a.Kind {
	case graph.AnswerTiles:
		if a.Rows < graph.MinTiles || a.Rows > graph.MaxTiles || a.Cols < graph.MinTiles || a.Cols > graph.MaxTiles {
			r.err("tiles.size", n.Slug, "", "a tile puzzle needs %d to %d rows and columns", graph.MinTiles, graph.MaxTiles)
		}
		if a.Image == nil || a.Image.Kind != graph.MediaImage {
			r.err("tiles.image", n.Slug, "", "a tile puzzle needs a picture to cut up")
		}
	case graph.AnswerSearch:
		if hub, ok := owners[n.Slug]; ok && !n.PuzzleCfg.Repeatable {
			if h, _ := g.Node(hub); h.HubCfg != nil && h.HubCfg.Home {
				r.warn("search.repeatable", n.Slug, "",
					"teams come back to this search from the home screen, but it is not repeatable, so they will only ever see their first find")
			}
		}
	case graph.AnswerChoice:
		checkChoices(r, n.Slug, a)
	case graph.AnswerLock:
		checkLock(r, n.Slug, a)
	case graph.AnswerOrder:
		checkOrder(r, n.Slug, a)
	case graph.AnswerItems:
		checkSlots(r, g, n.Slug, a)
		if !slices.Contains(graph.Missings, a.Missing) {
			r.err("slots.missing", n.Slug, "", "what to tell a team without the items is %q; it must be name, none or empty", a.Missing)
		}
		// A branch of an ordinary hub counts as done once entered, so a
		// team that leaves this one for its items cannot come back.
		if o, ok := missingOutput(n); ok {
			if hub, in := owners[n.Slug]; in {
				if h, _ := g.Node(hub); h.HubCfg != nil && !h.HubCfg.Home {
					r.warn("slots.leave.oneway", n.Slug, o.Port,
						"a team that leaves for its items cannot come back: the hub %q counts this branch done once entered. Make it a home screen so its branches stay open", hub)
				}
			}
		}
	case graph.AnswerKeys:
		if a.Keys < 0 || a.Window < 0 {
			r.err("keys.counts", n.Slug, "", "a two-key launch needs a key count and a window of zero or more")
		}
		if len(a.Posts) > 0 && a.Keys > len(a.Posts) {
			r.err("keys.posts", n.Slug, "", "a two-key launch with posts turns one key at each, so it cannot ask for %d keys at %d posts", a.Keys, len(a.Posts))
		}
		if a.Keys == 1 {
			r.warn("keys.one", n.Slug, "", "one key is a button, not a launch: ask for two or more, or leave it at every player")
		}
		for _, slug := range a.Posts {
			for _, z := range g.Zones {
				if z.Slug == slug && len(z.Polygon) == 0 && z.RadiusM < minPostRadius {
					r.warn("keys.post.small", n.Slug, "", "post %q is %.0f m across from its centre; a phone's GPS in a street is often off by more, so a player standing there is told to go there. Make it %d m or more", z.Name, z.RadiusM, minPostRadius)
				}
			}
		}
		if slices.ContainsFunc(a.PostRoles, func(role string) bool { return role != "" }) && !locksRoles(g) {
			r.warn("keys.post_roles.unlocked", n.Slug, "", "roles can change hands until they are locked, so a player may walk to a post and find it someone else's; lock them with a roles block (Lock) before this step")
		}
	case graph.AnswerScan:
	case graph.AnswerQuestions:
		if !slices.ContainsFunc(n.Blocks(), isQuestion) {
			r.warn("questions.none", n.Slug, "",
				"the puzzle is answered by its questions but has none, so a team continues at once; add a quiz question or the tie-breaker")
		}
		answers := 0
		for _, o := range n.PuzzleCfg.Outputs {
			if o.Trigger == graph.OnAnswer {
				if answers++; answers == 2 {
					r.warn("questions.outputs", n.Slug, o.Port,
						"a puzzle answered by its questions always takes its first answer route; this one is never taken")
				}
			}
		}
	case graph.AnswerTasks:
		if !slices.ContainsFunc(n.Blocks(), func(b graph.Block) bool { return b.Kind == graph.BlockTask }) {
			r.err("tasks.none", n.Slug, "", "the puzzle is answered by its tasks but has none; add a task block for each role's part")
		}
	default:
		r.err("puzzle.answer.kind", n.Slug, "", "unknown kind of answer %q", a.Kind)
	}
}

// checkBar catches an answer bar set on a puzzle that shows none, or
// asking for a keyboard there is none of.
func checkBar(r *reporter, n graph.Node) {
	b, kind := n.PuzzleCfg.Bar, n.PuzzleCfg.Kind()
	if !slices.Contains(graph.Keyboards, b.Keyboard) {
		r.err("bar.keyboard", n.Slug, "", "the answer bar asks for keyboard %q; it must be text or number", b.Keyboard)
	}
	if kind != "" && kind != graph.AnswerScan {
		r.warn("bar.unused", n.Slug, "", "the answer bar's settings are for typed or scanned answers; this puzzle is answered by %s, so they are never shown", kind)
	}
}

// checkLock catches a lock that cannot be turned, or whose symbols could
// not be told apart in the answer.
func checkLock(r *reporter, node string, l *graph.Answer) {
	if l.Wheels < 1 || l.Wheels > graph.MaxWheels {
		r.err("lock.wheels", node, "", "a lock has 1 to %d wheels", graph.MaxWheels)
	}
	if len(l.Symbols) < 2 {
		r.err("lock.symbols", node, "", "a lock's wheels need at least two symbols to turn through")
	}
	seen := map[string]bool{}
	for _, s := range l.Symbols {
		switch {
		case strings.TrimSpace(s.Value) == "":
			r.err("lock.value.empty", node, "", "a lock symbol has no value")
		case strings.Contains(s.Value, "|"):
			r.err("lock.value.pipe", node, "", "lock symbol %q contains \"|\", which separates the wheels", s.Value)
		case seen[s.Value]:
			r.err("lock.value.duplicate", node, "", "the lock has the symbol %q twice", s.Value)
		}
		seen[s.Value] = true
	}
	for _, i := range l.Start {
		if i < 0 || i >= len(l.Symbols) {
			r.err("lock.start", node, "", "a wheel starts at symbol %d, which the lock does not have", i+1)
		}
	}
}

// cardColor is a card's background colour: #rrggbb.
var cardColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// checkOrder catches an order puzzle with too few or too many cards, a
// card with nothing on it, or cards dealt in an order that is not one.
func checkOrder(r *reporter, node string, a *graph.Answer) {
	if len(a.Cards) < graph.MinCards || len(a.Cards) > graph.MaxCards {
		r.err("order.cards", node, "", "an order puzzle has %d to %d cards", graph.MinCards, graph.MaxCards)
	}
	for i, c := range a.Cards {
		if strings.TrimSpace(c.Text) == "" && c.Image == nil {
			r.err("order.card.empty", node, "", "card %d has neither text nor a picture", i+1)
		}
		if c.Image != nil && c.Image.Kind != graph.MediaImage {
			r.err("order.card.image", node, "", "card %d's background is not a picture", i+1)
		}
		if c.Color != "" && !cardColor.MatchString(c.Color) {
			r.err("order.card.color", node, "", "card %d's colour %q is not a colour like #1f6feb", i+1, c.Color)
		}
	}
	if len(a.Start) > 0 && !slices.Equal(a.Deal(), a.Start) {
		r.err("order.start", node, "", "the cards are dealt in an order that does not hold each card once")
	}
	if len(a.Cards) >= graph.MinCards && slices.Equal(a.Deal(), identity(len(a.Cards))) {
		r.warn("order.start.solved", node, "", "the cards are dealt in the right order, so there is nothing to do: shuffle them")
	}
}

func identity(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func missingOutput(n graph.Node) (graph.Output, bool) {
	for _, o := range n.PuzzleCfg.Outputs {
		if o.Trigger == graph.OnMissing {
			return o, true
		}
	}
	return graph.Output{}, false
}

// checkSlots catches item slots a team could never fill: none, too
// many, one wanting no item, or an item neither a step nor a role's kit
// ever hands out. Whether
// each item is declared is checkReferences'.
func checkSlots(r *reporter, g *graph.Graph, node string, a *graph.Answer) {
	if len(a.Slots) == 0 || len(a.Slots) > graph.MaxSlots {
		r.err("slots.count", node, "", "an item puzzle has 1 to %d slots", graph.MaxSlots)
	}
	// A role's kit is in the backpack from the start.
	granted := map[string]bool{}
	for item := range g.Kit() {
		granted[item] = true
	}
	for _, n := range g.Nodes {
		if n.Type == graph.Effect && n.EffectCfg != nil && n.EffectCfg.Kind == graph.GrantItem {
			granted[n.EffectCfg.Item] = true
		}
	}
	warned := map[string]bool{}
	for i, s := range a.Slots {
		switch {
		case strings.TrimSpace(s.Item) == "":
			r.err("slots.item.missing", node, "", "slot %d wants no item", i+1)
		case !granted[s.Item] && !warned[s.Item]:
			warned[s.Item] = true
			r.warn("slots.item.never", node, "", "slot %d wants item %q, but no step gives it to the team", i+1, s.Item)
		}
	}
}

// checkChoices catches a choice question a team could not answer, or
// whose answers could not be told apart.
func checkChoices(r *reporter, node string, c *graph.Answer) {
	if len(c.Fields) == 0 {
		r.err("choices.none", node, "", "a choice puzzle has no questions")
	}
	answers := 0
	for i, f := range c.Fields {
		if len(f.Options) < 2 {
			r.err("choices.options", node, "", "question %d needs at least two options", i+1)
		}
		seen := map[string]bool{}
		for _, o := range f.Options {
			switch {
			case strings.TrimSpace(o.Value) == "":
				r.err("choices.value.empty", node, "", "question %d has an option with no value", i+1)
			case strings.Contains(o.Value, "|"):
				r.err("choices.value.pipe", node, "", "option %q of question %d contains \"|\", which separates the answers", o.Value, i+1)
			case seen[o.Value]:
				r.err("choices.value.duplicate", node, "", "question %d offers %q twice", i+1, o.Value)
			}
			seen[o.Value] = true
		}
		if f.Answer != "" {
			answers++
			if !seen[f.Answer] {
				r.err("choices.answer.unknown", node, "", "the answer to question %d, %q, is not one of its options", i+1, f.Answer)
			}
		}
	}
	if answers > 0 && answers < len(c.Fields) {
		r.err("choices.answer.partial", node, "", "either every question has a right answer, for a marked form, or none does")
	}
}

// checkTranslations warns about translations left behind by an edit — a
// step, zone or note deleted or renamed. They do no harm, but an author
// deserves to know their English for it is gone with it.
func checkTranslations(r *reporter, g *graph.Graph) {
	nodes := map[string]bool{}
	blocks := map[string]bool{}
	for _, n := range g.Nodes {
		nodes[n.Slug] = true
		for _, b := range n.AllBlocks() {
			blocks[graph.BlockKey(n.Slug, b.ID)] = true
		}
	}
	zones := slugSet(len(g.Zones), func(i int) string { return g.Zones[i].Slug })
	journal := slugSet(len(g.Journal), func(i int) string { return g.Journal[i].Slug })
	sections := slugSet(len(g.Journal), func(i int) string { return g.Journal[i].Section })
	items := slugSet(len(g.Items), func(i int) string { return g.Items[i].Slug })
	counters := slugSet(len(g.Counters), func(i int) string { return g.Counters[i].Slug })
	themes := slugSet(len(g.Themes), func(i int) string { return g.Themes[i].Slug })
	langs := make([]string, 0, len(g.Translations))
	for l := range g.Translations {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	for _, l := range langs {
		if l == g.Language && len(g.Translations[l]) > 0 {
			r.warn("translation.main", "", "", "the adventure is written in %s, so its %s translation is never shown", l, l)
		}
		stale := 0
		for key := range g.Translations[l] {
			kind, rest, _ := strings.Cut(key, ":")
			slug, _, _ := strings.Cut(rest, ":")
			// A block's key, or one of its fields' (BlockFieldKey).
			block, isBlock := strings.CutPrefix(rest, slug+":block:")
			id, _, _ := strings.Cut(block, ":")
			switch {
			case kind == "n" && !nodes[slug], kind == "z" && !zones[slug], kind == "j" && !journal[slug],
				kind == "js" && !sections[rest],
				kind == "i" && !items[slug], kind == "c" && !counters[slug], kind == "t" && !themes[slug],
				kind == "n" && isBlock && !blocks[graph.BlockKey(slug, id)]:
				stale++
			}
		}
		if stale > 0 {
			r.warn("translation.stale", "", "", "%d %s translations belong to steps, content blocks, zones, notes, journal sections, items, counters or themes that no longer exist", stale, l)
		}
	}
}

// checkTimeLimit catches a time limit with nowhere to send the team.
func checkTimeLimit(r *reporter, g *graph.Graph) {
	tl := g.TimeLimit
	if tl == nil {
		return
	}
	if tl.Seconds <= 0 {
		r.err("time_limit.seconds", "", "", "the adventure's time limit must be longer than zero")
	}
	n, ok := g.Node(tl.To)
	switch {
	case tl.To == "" || !ok:
		r.err("time_limit.to", "", "", "the adventure's time limit sends teams to %q, which is not a step", tl.To)
	case n.Type == graph.Start:
		r.err("time_limit.to", tl.To, "", "the time limit cannot send teams back to a start")
	}
}

// blockNames are the kinds of block in the designer's words, where the
// kind itself is not one an author would use.
var blockNames = map[graph.BlockKind]string{
	graph.BlockCall:     "incoming call",
	graph.BlockCipher:   "cipher sheet",
	graph.BlockRadar:    "hot-and-cold",
	graph.BlockOverlay:  "look-through",
	graph.BlockNoise:    "noise meter",
	graph.BlockBearing:  "bearing lock",
	graph.BlockShake:    "shake to reveal",
	graph.BlockFlasher:  "Morse flasher",
	graph.BlockSuspects: "suspect board",
	graph.BlockSpotDiff: "spot the difference",
	graph.BlockLetters:  "letter tiles",
	graph.BlockSplit:    "split secret",
	graph.BlockVote:     "team vote",
	graph.BlockPhoto:    "photo challenge",
	graph.BlockDraw:     "random draw",
	graph.BlockBulletin: "live bulletin",
	graph.BlockAccuse:   "accusation",
	graph.BlockQuiz:     "quiz question",
	graph.BlockTiebreak: "tie-breaker",
}

// checkBlocks checks the shape of a step's content: every block's
// required fields, per kind. A block's file and its show condition are
// checkMedia's and checkReferences'.
func checkBlocks(r *reporter, g *graph.Graph) {
	zones := slugSet(len(g.Zones), func(i int) string { return g.Zones[i].Slug })
	counters := slugSet(len(g.Counters), func(i int) string { return g.Counters[i].Slug })
	tiebreaks := 0
	for _, n := range g.Nodes {
		seen := map[string]bool{}
		fixed := map[graph.BlockKind]int{}
		for _, b := range n.AllBlocks() {
			switch {
			case b.ID == "":
				r.err("block.id.empty", n.Slug, "", "a content block on this step has no id")
			case seen[b.ID]:
				r.err("block.id.duplicate", n.Slug, "", "two content blocks on this step share the id %q", b.ID)
			}
			seen[b.ID] = true
			name := blockNames[b.Kind]
			if name == "" {
				name = string(b.Kind)
			}
			bad := func(code, format string, args ...any) {
				r.err(code, n.Slug, "", "the %s on this step "+format, append([]any{name}, args...)...)
			}
			if a := b.Animation; a != nil {
				switch {
				case a.Entrance != "" && !slices.Contains(graph.Entrances, a.Entrance):
					bad("block.animation.entrance", "enters as %q, which is not one of %s", a.Entrance, strings.Join(graph.Entrances, ", "))
				case a.Attention != "" && !slices.Contains(graph.Attentions, a.Attention):
					bad("block.animation.attention", "draws the eye with %q, which is not one of %s", a.Attention, strings.Join(graph.Attentions, ", "))
				case a.Duration != 0 && (a.Duration < graph.MinAnimationDuration || a.Duration > graph.MaxAnimationDuration):
					bad("block.animation.duration", "takes %g seconds; keep it %g–%d", a.Duration, graph.MinAnimationDuration, graph.MaxAnimationDuration)
				case a.Repeat != "" && !slices.Contains(graph.AnimationRepeats, a.Repeat):
					bad("block.animation.repeat", "repeats %q; use three, forever or leave it out", a.Repeat)
				case a.Delay < 0 || a.Delay > graph.MaxAnimationDelay:
					bad("block.animation.delay", "waits %g seconds; keep it 0–%d", a.Delay, graph.MaxAnimationDelay)
				}
			}
			switch b.Kind {
			case graph.BlockText, graph.BlockImage, graph.BlockAudio, graph.BlockVideo, graph.BlockOverlay,
				graph.BlockStandings, graph.BlockCall, graph.BlockBulletin:
			case graph.BlockQuiz:
				switch {
				case strings.TrimSpace(b.Text) == "":
					bad("block.quiz.question", "has no question")
				case len(b.Options) != len(graph.QuizChoices) || slices.ContainsFunc(b.Options, func(o string) bool { return strings.TrimSpace(o) == "" }):
					bad("block.quiz.options", "needs three answers, for 1, X and 2")
				case !slices.Contains(graph.QuizChoices, b.Answer):
					bad("block.quiz.answer", "has no right answer: choose 1, X or 2")
				case b.Worth < 0:
					bad("block.quiz.worth", "is worth %d points; it must be 0 or more", b.Worth)
				}
			case graph.BlockTiebreak:
				// A switched-off one is a spare: only the one shown ranks.
				if !b.Disabled {
					tiebreaks++
				}
				if !b.Disabled && tiebreaks == 2 {
					r.err("block.tiebreak.duplicate", n.Slug, "", "an adventure has one tie-breaker; this is a second")
				}
				switch {
				case strings.TrimSpace(b.Text) == "":
					bad("block.tiebreak.question", "has no question")
				case b.Target == nil:
					bad("block.tiebreak.target", "has no right number")
				}
			case graph.BlockTitle, graph.BlockPoints:
				fixed[b.Kind]++
				if !slices.Contains(graph.FixedKinds[n.Type], b.Kind) {
					r.err("block.fixed.misplaced", n.Slug, "", "a %s has no %s block", n.Type, b.Kind)
				}
			case graph.BlockCompass, graph.BlockMap, graph.BlockRadar:
				if b.Zone == "" {
					bad("block.zone.missing", "has no zone to show")
				} else if !zones[b.Zone] {
					r.err("ref.zone.unknown", n.Slug, "", "the %s on this step shows zone %q, which is not declared", b.Kind, b.Zone)
				}
				if b.Kind == graph.BlockMap && b.Zoom != 0 && (b.Zoom < graph.MinZoom || b.Zoom > graph.MaxZoom) {
					r.err("block.zoom", n.Slug, "", "the map on this step is at zoom %d; it must be %d to %d, or fit the zone", b.Zoom, graph.MinZoom, graph.MaxZoom)
				}
				for _, m := range b.Markers {
					if m.Lat < -85 || m.Lat > 85 || m.Lng < -180 || m.Lng > 180 {
						bad("block.marker", "has a marker at %g, %g, which is off the map", m.Lat, m.Lng)
					}
				}
			case graph.BlockNote:
				if b.Style != "" && !slices.Contains(graph.NoteStyles, b.Style) {
					bad("block.style", "has style %q; it must be one of %s", b.Style, strings.Join(graph.NoteStyles, ", "))
				}
			case graph.BlockDivider:
				if b.Style != "" && !slices.Contains(graph.DividerStyles, b.Style) {
					bad("block.style", "has style %q; it must be one of %s", b.Style, strings.Join(graph.DividerStyles, ", "))
				}
			case graph.BlockChat:
				if len(b.Lines) == 0 {
					bad("block.chat.empty", "has no messages")
				}
			case graph.BlockCipher:
				if b.Wheel && b.Cipher != "caesar" {
					bad("block.cipher.wheel", "is a wheel, which only a Caesar cipher can be")
				}
				switch {
				case !slices.Contains(graph.Ciphers, b.Cipher):
					bad("block.cipher", "is %q; it must be one of %s", b.Cipher, strings.Join(graph.Ciphers, ", "))
				case b.Cipher == "caesar" && (b.Shift < 1 || b.Shift > 25):
					bad("block.cipher.shift", "shifts by %d; a Caesar shift is 1 to 25", b.Shift)
				case b.Cipher == "custom" && strings.TrimSpace(b.Key) == "":
					bad("block.cipher.key", "has no key to show")
				}
			case graph.BlockHotspots:
				for _, sp := range b.Spots {
					if sp.X < 0 || sp.X > 1 || sp.Y < 0 || sp.Y > 1 {
						bad("block.spot", "has a spot outside the picture")
					}
				}
			case graph.BlockLevel:
				if b.Pitch < 0 || b.Pitch > 90 {
					bad("block.pitch", "wants %d°; it must be 0 (flat) to 90 (straight up)", b.Pitch)
				}
			case graph.BlockNoise:
				if b.Threshold < 0 || b.Threshold > 100 {
					bad("block.threshold", "wants loudness %d; it must be 1 to 100", b.Threshold)
				}
			case graph.BlockCountdown:
				if b.Seconds <= 0 {
					bad("block.seconds", "has no time to count down")
				}
				if !slices.Contains(graph.CountdownFroms, b.From) {
					bad("block.from", "counts from %q; it must be step or start", b.From)
				}
			case graph.BlockStatus:
				for _, c := range b.Counters {
					if !counters[c] {
						r.err("ref.counter.unknown", n.Slug, "", "the status on this step shows counter %q, which is not declared", c)
					}
				}
				if !b.Items && len(b.Counters) == 0 {
					bad("block.status.empty", "shows nothing: choose items or counters")
				}
			case graph.BlockTorch:
				if !slices.Contains(graph.TorchStyles, b.Style) {
					bad("block.style", "has style %q; it must be scratch, uv, red, or none for a torch", b.Style)
				}
			case graph.BlockSplit:
				if filled(b.Parts) < 2 {
					bad("block.split.parts", "has fewer than two parts to share out")
				}
			case graph.BlockBearing:
				if b.Heading < 0 || b.Heading > 359 {
					bad("block.heading", "wants heading %d°; it must be 0 to 359", b.Heading)
				}
				if strings.TrimSpace(b.Text) == "" {
					bad("block.text.empty", "reveals nothing: write what the team reads when it points the right way")
				}
			case graph.BlockShake:
				if b.Threshold < 0 || b.Threshold > 100 {
					bad("block.threshold", "wants a shake of %d; it must be 1 to 100", b.Threshold)
				}
				if strings.TrimSpace(b.Text) == "" {
					bad("block.text.empty", "reveals nothing: write what the team reads once it has shaken")
				}
			case graph.BlockSuspects:
				if len(b.Cards) == 0 {
					bad("block.cards.empty", "has no suspects")
				}
				for _, c := range b.Cards {
					if strings.TrimSpace(c.Name) == "" {
						bad("block.card.name", "has a suspect with no name")
						break
					}
				}
			case graph.BlockPhoto:
				if strings.TrimSpace(b.Text) == "" {
					bad("block.text.empty", "does not say what to take a picture of")
				}
				if b.Worth < 0 {
					bad("block.worth", "is worth %d points; it must be 0 or more", b.Worth)
				}
			case graph.BlockSpotDiff:
				if b.Other <= 0 {
					bad("block.other", "has no second picture to compare")
				}
				if len(b.Spots) == 0 {
					bad("block.spots.empty", "marks no differences to find")
				}
				for _, sp := range b.Spots {
					if sp.X < 0 || sp.X > 1 || sp.Y < 0 || sp.Y > 1 {
						bad("block.spot", "has a spot outside the picture")
					}
				}
			case graph.BlockFlasher:
				if strings.TrimSpace(b.Text) == "" {
					bad("block.text.empty", "has no message to flash")
				} else if c := unMorse(b.Text); c != "" {
					bad("block.flasher.char", "has %q, which Morse cannot flash; use letters, digits and simple punctuation", c)
				}
				if b.Speed < 0 || b.Speed > 30 {
					bad("block.speed", "flashes at %d words a minute; it must be 1 to 30", b.Speed)
				}
			case graph.BlockLetters:
				if strings.TrimSpace(b.Text) == "" {
					bad("block.text.empty", "has no letters to lay out")
				}
			case graph.BlockVote, graph.BlockDraw:
				if filled(b.Options) < 2 {
					bad("block.options", "has fewer than two options")
				} else if filled(b.Options) < len(b.Options) {
					bad("block.options.empty", "has a blank option: fill it in or remove it")
				}
			case graph.BlockRoles:
				if len(g.PublicRoles()) == 0 {
					bad("block.roles.none", "has no roles to show: add one under the adventure's roles")
				}
			case graph.BlockSabotage, graph.BlockAccuse:
				if ro, ok := g.Role(b.Role); !ok || !ro.Secret {
					bad("block.role.secret", "belongs to no secret role; pick the saboteur's")
				}
				if b.Worth < 0 || b.Seconds < 0 {
					bad("block.worth", "costs or is worth %d points over %d seconds; both must be 0 or more", b.Worth, b.Seconds)
				}
				if b.Kind == graph.BlockAccuse && strings.TrimSpace(b.Text) == "" {
					bad("block.text.empty", "does not ask anything: write the question, as “Who is the saboteur?”")
				}
				if b.Kind == graph.BlockSabotage {
					if b.Worth == 0 && b.Seconds == 0 && b.Journal == "" {
						bad("block.sabotage.empty", "does nothing: make it cost points, lock the answer for a while, or plant a note")
					}
					if b.Journal != "" && !slices.ContainsFunc(g.Journal, func(j graph.JournalEntry) bool { return j.Slug == b.Journal }) {
						r.err("ref.journal.unknown", n.Slug, "", "sabotage %q plants journal entry %q, which is not declared", b.ID, b.Journal)
					}
				}
			case graph.BlockTask:
				if n.PuzzleCfg == nil || n.PuzzleCfg.Kind() != graph.AnswerTasks {
					bad("block.task.step", "is a task on a step not answered by its tasks; set the puzzle's answer to “Tasks”")
				}
				if ro, ok := g.Role(b.Role); !ok || ro.Secret {
					bad("block.task.role", "belongs to no role a player is given openly; pick one")
				}
				if strings.TrimSpace(b.Answer) == "" {
					bad("block.task.answer", "has no answer to accept")
				}
				if b.Zone != "" && !zones[b.Zone] {
					r.err("ref.zone.unknown", n.Slug, "", "the task on this step is done at zone %q, which is not declared", b.Zone)
				}
			case graph.BlockMission:
				if ro, ok := g.Role(b.Role); !ok || !ro.Secret {
					bad("block.role.secret", "belongs to no secret role; pick the one whose betrayal counts")
				}
				if b.Zone == "" {
					bad("block.zone.missing", "has no post: pick the zone where players pick")
				} else if !zones[b.Zone] {
					r.err("ref.zone.unknown", n.Slug, "", "the mission on this step is picked at zone %q, which is not declared", b.Zone)
				}
				if b.Count < 0 {
					bad("block.count", "waits for %d players; it must be 1 or more", b.Count)
				}
				if len(b.Options) != 0 && (len(b.Options) != 2 || filled(b.Options) != 2) {
					bad("block.options", "has %d choices; name both, or leave them for Deliver and Tamper", filled(b.Options))
				}
			default:
				r.err("block.kind", n.Slug, "", "content block %q is %q, which is not a kind of block", b.ID, b.Kind)
			}
		}
		for k, c := range fixed {
			if c > 1 {
				r.err("block.fixed.duplicate", n.Slug, "", "this step has %d %s blocks; it has exactly one", c, k)
			}
		}
		if slices.ContainsFunc(n.Blocks(), isQuestion) {
			switch {
			case n.Type == graph.End:
				// The quiz is marked as the team arrives here, before
				// it could pick.
				r.err("block.quiz.end", n.Slug, "", "the end has quiz questions, but a team's quiz is marked as it arrives; move them to an earlier step")
			case n.PuzzleCfg != nil && n.PuzzleCfg.Kind() != graph.AnswerQuestions:
				r.warn("block.quiz.puzzle", n.Slug, "",
					"a team can solve this puzzle and move on without answering its quiz questions; let the questions be its answer, or move them to a story")
			}
		}
	}
}

// isQuestion reports whether a block is one a team answers with a pick.
func isQuestion(b graph.Block) bool {
	return b.Kind == graph.BlockQuiz || b.Kind == graph.BlockTiebreak
}

func checkMedia(r *reporter, g *graph.Graph) {
	for _, ro := range g.Roles {
		if m := ro.Image; m != nil && (m.ID <= 0 || m.Kind != graph.MediaImage) {
			r.err("media.role", "", "", "role %q has a picture with no file", ro.Slug)
		}
	}
	for _, je := range g.Journal {
		for _, m := range je.Media {
			if m.ID <= 0 || m.Kind != graph.MediaImage && m.Kind != graph.MediaAudio {
				r.err("media.journal", "", "", "journal note %q has a picture or sound with no file", je.Slug)
			}
		}
	}
	for _, n := range g.Nodes {
		for i, ms := range n.MediaLists() {
			for _, m := range ms {
				if i > 0 && m.Kind == graph.MediaVideo {
					r.err("media.kind", n.Slug, "", "a hint's media %d is a video; hints show pictures and sounds", m.ID)
				}
				if m.ID <= 0 {
					r.err("media.id", n.Slug, "", "a picture or sound on this step has no file")
				}
				if m.Kind != graph.MediaImage && m.Kind != graph.MediaAudio && m.Kind != graph.MediaVideo {
					r.err("media.kind", n.Slug, "", "media %d is %q; it must be an image, audio or video", m.ID, m.Kind)
				}
			}
		}
	}
}

// checkPrintMaterials keeps a posted material printable in every language
// a buyer can choose: a missing one blocks publishing, since nobody could
// print that buyer's letter.
func checkPrintMaterials(r *reporter, g *graph.Graph) {
	pms := g.PrintMaterials
	if len(pms) > graph.MaxPrintMaterials {
		r.err("print.count", "", "", "an adventure has at most %d print materials", graph.MaxPrintMaterials)
	}
	checkUnique(r, "print material", len(pms), func(i int) string { return pms[i].ID })
	langs := g.Languages()
	if langs == nil && g.Language != "" {
		langs = []string{g.Language}
	}
	for _, pm := range pms {
		name := pm.Name
		if strings.TrimSpace(name) == "" {
			r.err("print.name", "", "", "print material %q has no name", pm.ID)
			name = pm.ID
		}
		switch pm.Delivery {
		case graph.DeliveryPosted, graph.DeliveryEmailed, graph.DeliveryOrganiser:
		default:
			r.err("print.delivery", "", "", "print material %q is delivered %q; it must be posted, emailed or organiser", name, pm.Delivery)
		}
		for _, l := range slices.Sorted(maps.Keys(pm.Files)) {
			if id := pm.Files[l]; id <= 0 {
				r.err("print.file", "", "", "print material %q has no file for %s", name, l)
			} else if !slices.Contains(langs, l) {
				r.warn("print.language", "", "", "print material %q has a file for %s, which the adventure isn't played in", name, l)
			}
		}
		for _, l := range langs {
			if _, ok := pm.Files[l]; ok {
				continue
			}
			if pm.Delivery == graph.DeliveryPosted {
				r.err("print.missing", "", "", "print material %q is posted but has no file for %s", name, l)
			} else {
				r.warn("print.missing", "", "", "print material %q has no file for %s", name, l)
			}
		}
	}
}

func checkUnique(r *reporter, kind string, n int, slugAt func(int) string) {
	seen := map[string]bool{}
	for i := range n {
		s := slugAt(i)
		if s == "" {
			r.err("resource.slug.empty", "", "", "a %s has no slug", kind)
			continue
		}
		if seen[s] {
			r.err("resource.slug.duplicate", "", "", "two %ss share the slug %q", kind, s)
		}
		seen[s] = true
	}
}

func reachableFrom(g *graph.Graph, start string) map[string]bool {
	adj := map[string][]string{}
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	seen := map[string]bool{start: true}
	stack := []string{start}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, next := range adj[cur] {
			if !seen[next] {
				seen[next] = true
				stack = append(stack, next)
			}
		}
	}
	return seen
}

func slugSet(n int, at func(int) string) map[string]bool {
	out := make(map[string]bool, n)
	for i := range n {
		out[at(i)] = true
	}
	return out
}

// reachableFromStarts is every node some start leads to, starts included.
func reachableFromStarts(g *graph.Graph) map[string]bool {
	seen := map[string]bool{}
	for _, st := range g.Starts() {
		for k := range reachableFrom(g, st.Slug) {
			seen[k] = true
		}
	}
	return seen
}

// Headers a webhook may not set: the request's own framing, which the
// sender works out, and headers the delivery sets itself.
var reservedHeaders = []string{"host", "content-length", "transfer-encoding", "connection", "upgrade", "te", "trailer", "keep-alive", "proxy-connection", "user-agent", "x-zv-delivery-id"}

// maxWebhookHeaders caps a webhook's headers; a real API needs a few.
const maxWebhookHeaders = 20

// checkWebhook checks a webhook's URL, method and headers. Whether the
// host is one the server may call is the delivery worker's to decide, at
// send time, where DNS cannot be flipped after the check.
func checkWebhook(r *reporter, slug string, e graph.EffectConfig) {
	if e.URL == "" {
		r.err("effect.webhook.missing", slug, "", "webhook effect has no URL")
		return
	}
	if u, err := url.Parse(e.URL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		r.err("effect.webhook.url", slug, "", "webhook URL %q must be a full http(s) address", e.URL)
	}
	if e.Method != "" && !slices.Contains(graph.WebhookMethods, e.Method) {
		r.err("effect.webhook.method", slug, "", "webhook method %q must be one of %s", e.Method, strings.Join(graph.WebhookMethods, ", "))
	}
	if len(e.Headers) > maxWebhookHeaders {
		r.err("effect.webhook.headers", slug, "", "webhook has %d headers; at most %d", len(e.Headers), maxWebhookHeaders)
	}
	for _, h := range e.Headers {
		switch {
		case !validHeaderName(h.Name):
			r.err("effect.webhook.header", slug, "", "webhook header name %q is not a valid HTTP header name", h.Name)
		case slices.Contains(reservedHeaders, strings.ToLower(h.Name)):
			r.err("effect.webhook.header", slug, "", "webhook header %q is set by the delivery itself", h.Name)
		case strings.ContainsAny(h.Value, "\r\n\x00") || len(h.Value) > 4096:
			r.err("effect.webhook.header", slug, "", "webhook header %q has a value with line breaks or over 4096 bytes", h.Name)
		}
	}
}

// validHeaderName reports whether s is an HTTP token (RFC 9110 §5.6.2).
func validHeaderName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c > 127 || !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return true
}

// placeholderRe finds {{name}} in an email's text, as effects renders it.
var placeholderRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

// filled counts the non-blank strings.
func filled(ss []string) int {
	n := 0
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			n++
		}
	}
	return n
}

// morseable are the characters a flasher can send.
const morseable = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.,?'!/()&:;=+-_\"$@ "

// unMorse is the first character of s a flasher cannot send, or "".
func unMorse(s string) string {
	for _, r := range strings.ToUpper(s) {
		if r == '\n' || r == '\t' {
			continue
		}
		if !strings.ContainsRune(morseable, r) {
			return string(r)
		}
	}
	return ""
}

var hostSlug = regexp.MustCompile(`^[\w-]+$`)

// starterID bounds a theme's starter: the ids are play-ui's, as a party
// adventure's trail_theme.
var starterID = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// checkHostEdits covers what a party's host may change: host values
// need a slug a text can name, once each, and a question to ask; a
// HostEdit only means something on a text or a quiz question, and a
// HostAnswer on a puzzle whose typed answer the host can set.
func checkHostEdits(r *reporter, g *graph.Graph) {
	seen := map[string]bool{}
	for _, v := range g.HostValues {
		switch {
		case !hostSlug.MatchString(v.Slug):
			r.err("host_value.slug", "", "", "a host value's slug %q may hold only letters, digits, - and _", v.Slug)
		case seen[v.Slug]:
			r.err("host_value.duplicate", "", "", "two host values share the slug %q", v.Slug)
		case strings.TrimSpace(v.Label) == "":
			r.err("host_value.label", "", "", "the host value %q has no question for the host", v.Slug)
		}
		seen[v.Slug] = true
	}
	for _, n := range g.Nodes {
		for _, b := range n.AllBlocks() {
			if b.HostEdit && b.Kind != graph.BlockText && b.Kind != graph.BlockQuiz {
				r.warn("block.host_edit.kind", n.Slug, "", "a host can only rewrite a text or a quiz question; %q is a %s", b.ID, b.Kind)
			}
		}
		if p := n.PuzzleCfg; p != nil && p.HostAnswer && !p.UsesMatchers() {
			r.warn("puzzle.host_answer.kind", n.Slug, "", "a host can only set a typed answer, and this step is answered another way")
		}
	}
}
