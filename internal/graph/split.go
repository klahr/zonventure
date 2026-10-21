package graph

import (
	"maps"
	"slices"
	"strings"
)

// MaxSplitDepth is how deep splits may nest: a group of a group of a
// group of a group. Deeper, no one could follow who is with whom.
const MaxSplitDepth = 4

// Paths is the shape of one split: what each of its paths covers, and
// the joins they lead to.
type Paths struct {
	// Steps are the nodes each path reaches, by port, before its join:
	// the steps its group walks alone. A split nested on the path counts
	// whole — itself, its paths' steps and its join — since its groups
	// are this group's. The split's own join is not in it, nor the split.
	Steps map[string][]string
	// Joins are the joins each path reaches, by port, in the order found.
	Joins map[string][]string
	// Inner are the splits nested on each path, by port, outermost first.
	Inner map[string][]string
	// Loops are the ports whose path leads back into this split, or into
	// one it is nested in: a group splitting into itself.
	Loops []string
}

// SplitPaths works out where a split's paths go. It reads only the
// edges, so it serves a graph mid-edit as well as a valid one.
func (g *Graph) SplitPaths(split string) Paths {
	return g.Splits().Paths(split)
}

// Splits works out every split's paths at once, sharing the work: a
// split nested on many paths, or in many splits, is worked out once.
// It is a snapshot of the graph as it was when made. Splits in a loop —
// a path leading back into its split — are cut where the loop closes, so
// what they cover hangs on which was asked for first; the validator
// refuses them all the same, and asks in an order of its own.
type Splits struct {
	g      *Graph
	nodes  map[string]Node
	next   map[string][]string
	paths  map[string]Paths
	depths map[string]int
	// direct are the joins each split's paths reach before any other
	// split or join, by port: the split's own, unless it is badly made.
	direct map[string]map[string][]string
	// open are the splits being worked out, each with the path being
	// walked: a path leading into one is a group splitting into itself.
	open map[string]string
}

// Splits starts working out the graph's splits; each is worked out when
// first asked for.
func (g *Graph) Splits() *Splits {
	s := &Splits{
		g:      g,
		nodes:  make(map[string]Node, len(g.Nodes)),
		next:   make(map[string][]string, len(g.Edges)),
		paths:  map[string]Paths{},
		depths: map[string]int{},
		direct: map[string]map[string][]string{},
		open:   map[string]string{},
	}
	for _, n := range g.Nodes {
		s.nodes[n.Slug] = n
	}
	for _, e := range g.Edges {
		s.next[e.From] = append(s.next[e.From], e.To)
	}
	// In order of slug, not of edge: what a path covers, and which join
	// it finds first, do not hang on the order the edges were drawn in.
	for _, to := range s.next {
		slices.Sort(to)
	}
	return s
}

// Paths is where a split's paths go: SplitPaths, sharing the work.
func (s *Splits) Paths(split string) Paths {
	if p, ok := s.paths[split]; ok {
		return p
	}
	out := Paths{Steps: map[string][]string{}, Joins: map[string][]string{}, Inner: map[string][]string{}}
	n, ok := s.nodes[split]
	if !ok || n.Type != Split || n.SplitCfg == nil {
		return out
	}
	defer delete(s.open, split)
	for _, p := range n.SplitCfg.Paths {
		s.open[split] = p.Port
		if start, ok := s.g.Target(split, p.Port); ok {
			s.walk(&out, p.Port, start)
		}
	}
	s.paths[split] = out
	return out
}

// walk follows one path from its first step to the joins it reaches.
// What it covers is all a path can reach without passing a join, so it
// does not hang on which way round, or how far, the edges go: a join
// first met head on is still an inner split's when that split is met
// later, and the path goes on past it.
func (s *Splits) walk(out *Paths, port, start string) {
	seen, on, inner := map[string]bool{}, map[string]bool{}, map[string]bool{}
	step := func(slug string) {
		if !on[slug] {
			on[slug] = true
			out.Steps[port] = append(out.Steps[port], slug)
		}
	}
	loop := func() {
		if !slices.Contains(out.Loops, port) {
			out.Loops = append(out.Loops, port)
		}
	}
	// passed are the inner splits' joins: steps of this path, not its
	// own join, however early the walk first met them.
	passed := map[string]bool{}
	var met []string
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		t := s.nodes[cur].Type
		if _, open := s.open[cur]; t == Split && open {
			loop()
			continue
		}
		if seen[cur] {
			continue
		}
		seen[cur] = true
		switch t {
		case Join:
			if !slices.Contains(met, cur) {
				met = append(met, cur)
			}
		case Split:
			// A group of this path splitting again: the inner split's
			// paths and join are this path's, and it goes on from there.
			paths := s.Paths(cur)
			step(cur)
			if !inner[cur] {
				inner[cur] = true
				out.Inner[port] = append(out.Inner[port], cur)
			}
			if len(paths.Loops) > 0 {
				loop()
			}
			for _, q := range s.nodes[cur].Ports() {
				for _, x := range paths.Steps[q] {
					step(x)
				}
				for _, x := range paths.Inner[q] {
					if !inner[x] {
						inner[x] = true
						out.Inner[port] = append(out.Inner[port], x)
					}
				}
			}
			for _, j := range s.joinsOf(cur, paths) {
				if !passed[j] {
					passed[j] = true
					step(j)
					queue = append(queue, s.next[j]...)
				}
			}
			// An inner path reaching another join — one it is not its
			// split's own — meets it as this path would: the walk stops
			// there, rather than going on past an enclosing split's join.
			for _, q := range s.nodes[cur].Ports() {
				for _, j := range paths.Joins[q] {
					if !passed[j] && !slices.Contains(met, j) {
						met = append(met, j)
					}
				}
			}
		default:
			step(cur)
			queue = append(queue, s.next[cur]...)
		}
	}
	for _, j := range met {
		if !passed[j] {
			out.Joins[port] = append(out.Joins[port], j)
		}
	}
}

// joinsOf are the joins an inner split's groups meet at, as the path it
// is nested on sees it: the joins its paths reach, less any an enclosing
// split's other paths reach directly — that split's own — and, for a
// split whose paths reach several, less any reached from another of them.
func (s *Splits) joinsOf(split string, paths Paths) []string {
	var js []string
	for _, q := range s.nodes[split].Ports() {
		for _, j := range paths.Joins[q] {
			if !slices.Contains(js, j) && !s.enclosing(j) {
				js = append(js, j)
			}
		}
	}
	if len(js) < 2 {
		return js
	}
	return slices.DeleteFunc(slices.Clone(js), func(j string) bool {
		for _, from := range js {
			if from != j && s.reaches(from, j) {
				return true
			}
		}
		return false
	})
}

// enclosing reports whether join is one an open split's paths reach
// directly, other than the path being walked: an enclosing split's own.
func (s *Splits) enclosing(join string) bool {
	for split, port := range s.open {
		for q, js := range s.directJoins(split) {
			if q != port && slices.Contains(js, join) {
				return true
			}
		}
	}
	return false
}

// directJoins are the joins each of a split's paths reaches before any
// other split or join, by port, once per split.
func (s *Splits) directJoins(split string) map[string][]string {
	if js, ok := s.direct[split]; ok {
		return js
	}
	out := map[string][]string{}
	for _, q := range s.nodes[split].Ports() {
		start, ok := s.g.Target(split, q)
		if !ok {
			continue
		}
		seen := map[string]bool{}
		queue := []string{start}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if seen[cur] {
				continue
			}
			seen[cur] = true
			switch s.nodes[cur].Type {
			case Join:
				out[q] = append(out[q], cur)
			case Split:
			default:
				queue = append(queue, s.next[cur]...)
			}
		}
	}
	s.direct[split] = out
	return out
}

// reaches reports whether any route leads from one node to another.
func (s *Splits) reaches(from, to string) bool {
	seen := map[string]bool{from: true}
	stack := slices.Clone(s.next[from])
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == to {
			return true
		}
		if !seen[cur] {
			seen[cur] = true
			stack = append(stack, s.next[cur]...)
		}
	}
	return false
}

// PathOf is the path of the split whose steps include node, if one does:
// the group a step belongs to.
func (p Paths) PathOf(node string) (string, bool) {
	for port, steps := range p.Steps {
		if slices.Contains(steps, node) {
			return port, true
		}
	}
	return "", false
}

// InnerPath is the innermost split path whose steps include node — the
// path of the group that walks it — as the split and its port.
func (s *Splits) InnerPath(node string) (split, port string, ok bool) {
	inner, _ := s.innerPaths()
	k, ok := inner[node]
	if !ok {
		return "", "", false
	}
	split, port, _ = strings.Cut(k, "\x00")
	return split, port, true
}

// innerPaths are each node's innermost path, keyed split "\x00" port —
// the fewest steps of any path holding it, the smallest key among equals
// — leaving out nodes on none; and the steps of each path, by key.
func (s *Splits) innerPaths() (map[string]string, map[string]map[string]bool) {
	var splits []string
	for slug, n := range s.nodes {
		if n.Type == Split && n.SplitCfg != nil {
			splits = append(splits, slug)
		}
	}
	slices.Sort(splits)
	inner := map[string]string{}
	steps := map[string]map[string]bool{}
	for _, split := range splits {
		paths := s.Paths(split)
		for _, port := range slices.Sorted(maps.Keys(paths.Steps)) {
			k := split + "\x00" + port
			on := make(map[string]bool, len(paths.Steps[port]))
			for _, x := range paths.Steps[port] {
				on[x] = true
			}
			steps[k] = on
		}
	}
	for k, on := range steps {
		for x := range on {
			if o, ok := inner[x]; !ok || len(on) < len(steps[o]) || len(on) == len(steps[o]) && k < o {
				inner[x] = k
			}
		}
	}
	return inner, steps
}

// JoinOf is the join a split's paths meet at: the first any path reaches.
func (g *Graph) JoinOf(split string) (string, bool) {
	return g.Splits().JoinOf(split)
}

// JoinOf is Graph.JoinOf, sharing the work.
func (s *Splits) JoinOf(split string) (string, bool) {
	n, ok := s.nodes[split]
	if !ok || n.SplitCfg == nil {
		return "", false
	}
	paths := s.Paths(split)
	for _, p := range n.SplitCfg.Paths {
		if js := paths.Joins[p.Port]; len(js) > 0 {
			return js[0], true
		}
	}
	return "", false
}

// SplitDepth is how deep splits nest from this one down: 1 for a split
// with no split on its paths.
func (g *Graph) SplitDepth(split string) int {
	return g.Splits().Depth(split)
}

// Depth is Graph.SplitDepth, sharing the work.
func (s *Splits) Depth(split string) int {
	d, _ := s.depth(split, map[string]bool{})
	return d
}

// depth reports, too, whether the depth is whole: one cut short by a
// loop, which the validator reports as such, is not kept, so it never
// stands in for the split's depth seen from elsewhere.
func (s *Splits) depth(split string, seen map[string]bool) (int, bool) {
	if d, ok := s.depths[split]; ok {
		return d, true
	}
	if seen[split] {
		return 0, false
	}
	seen[split] = true
	defer delete(seen, split)
	deepest, whole := 0, true
	paths := s.Paths(split)
	for _, port := range s.nodes[split].Ports() {
		for _, in := range paths.Inner[port] {
			d, ok := s.depth(in, seen)
			deepest, whole = max(deepest, d), whole && ok
		}
	}
	if whole {
		s.depths[split] = 1 + deepest
	}
	return 1 + deepest, whole
}
