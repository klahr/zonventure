package graph

import (
	"slices"
)

// Paths is the shape of one split: what each of its paths covers, and
// the joins they lead to.
type Paths struct {
	// Steps are the nodes each path reaches, by port, before its join:
	// the steps its group walks alone. The join is not in it, nor the
	// split.
	Steps map[string][]string
	// Joins are the joins each path reaches, by port, in the order found.
	Joins map[string][]string
	// Nested are the other splits each path reaches, by port: a group
	// splitting again, which splits do not do (valid refuses it). The
	// path stops there.
	Nested map[string][]string
	// Loops are the ports whose path leads back into this split.
	Loops []string
}

// Splits works out the graph's splits' paths, each once. It is a
// snapshot of the graph as it was when made.
type Splits struct {
	g     *Graph
	nodes map[string]Node
	// next are where each node's edges lead, in order of slug, and edges
	// the edges from each, as drawn.
	next  map[string][]string
	edges map[string][]Edge
	paths map[string]Paths
	// slugs are the splits, in order of slug.
	slugs []string
	// on and steps are onPaths, and owners OwnerHubs, once worked out.
	on     map[string]pathKey
	steps  map[pathKey]map[string]bool
	owners map[string]string
}

// Splits starts working out the graph's splits; each is worked out when
// first asked for.
func (g *Graph) Splits() *Splits {
	s := &Splits{
		g:     g,
		nodes: make(map[string]Node, len(g.Nodes)),
		next:  make(map[string][]string, len(g.Edges)),
		edges: make(map[string][]Edge, len(g.Edges)),
		paths: map[string]Paths{},
	}
	for _, n := range g.Nodes {
		s.nodes[n.Slug] = n
		if n.Type == Split && n.SplitCfg != nil {
			s.slugs = append(s.slugs, n.Slug)
		}
	}
	slices.Sort(s.slugs)
	for _, e := range g.Edges {
		s.next[e.From] = append(s.next[e.From], e.To)
		s.edges[e.From] = append(s.edges[e.From], e)
	}
	// In order of slug, not of edge: what a path covers, and which join
	// it finds first, do not hang on the order the edges were drawn in.
	for _, to := range s.next {
		slices.Sort(to)
	}
	return s
}

// Paths is where a split's paths go. It reads only the edges, so it
// serves a graph mid-edit as well as a valid one.
func (s *Splits) Paths(split string) Paths {
	if p, ok := s.paths[split]; ok {
		return p
	}
	out := Paths{Steps: map[string][]string{}, Joins: map[string][]string{}, Nested: map[string][]string{}}
	n, ok := s.nodes[split]
	if !ok || n.Type != Split || n.SplitCfg == nil {
		return out
	}
	for _, p := range n.SplitCfg.Paths {
		if start, ok := s.g.Target(split, p.Port); ok {
			s.walk(&out, split, p.Port, start)
		}
	}
	s.paths[split] = out
	return out
}

// walk follows one path from its first step to the joins it reaches:
// all it can reach without passing a join or a split, so it does not
// hang on which way round, or how far, the edges go.
func (s *Splits) walk(out *Paths, split, port, start string) {
	s.Reach([]string{start}, func(cur string) bool {
		switch s.nodes[cur].Type {
		case Join:
			out.Joins[port] = append(out.Joins[port], cur)
		case Split:
			if cur == split {
				out.Loops = append(out.Loops, port)
			} else {
				out.Nested[port] = append(out.Nested[port], cur)
			}
		default:
			out.Steps[port] = append(out.Steps[port], cur)
			return true
		}
		return false
	})
}

// Reach walks the edges breadth first from the nodes from, nearest
// first and in order of slug, visiting each node reached once: visit
// says whether to go on past it.
func (s *Splits) Reach(from []string, visit func(slug string) bool) {
	seen := map[string]bool{}
	queue := slices.Clone(from)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		if visit(cur) {
			queue = append(queue, s.next[cur]...)
		}
	}
}

// Slugs are the graph's splits, in order of slug.
func (s *Splits) Slugs() []string { return s.slugs }

// Graph is the graph the splits are of.
func (s *Splits) Graph() *Graph { return s.g }

// Warm works out everything Splits works out on demand, so that from
// then on it only reads: one Splits may then serve any number of
// readers at once, for a graph no one changes.
func (s *Splits) Warm() *Splits {
	for _, slug := range s.slugs {
		s.Paths(slug)
	}
	s.onPaths()
	s.OwnerHubs()
	return s
}

// Node is the node slug, found without a scan of the graph's nodes.
func (s *Splits) Node(slug string) (Node, bool) {
	n, ok := s.nodes[slug]
	return n, ok
}

// OnAnyPath reports whether node is a step of any split's path.
func (s *Splits) OnAnyPath(node string) bool {
	_, _, ok := s.OnPath(node)
	return ok
}

// OnPath is the split path whose steps include node — the path of the
// group that walks it — as the split and its port.
func (s *Splits) OnPath(node string) (split, port string, ok bool) {
	on, _ := s.onPaths()
	k, ok := on[node]
	return k.split, k.port, ok
}

// pathKey names one path of one split.
type pathKey struct{ split, port string }

// onPaths are each node's path, leaving out nodes on none, and the steps
// of each path, worked out once. A node on two paths, which valid
// refuses, is the first split's in order of slug, and its first port.
func (s *Splits) onPaths() (map[string]pathKey, map[pathKey]map[string]bool) {
	if s.on != nil {
		return s.on, s.steps
	}
	s.on, s.steps = map[string]pathKey{}, map[pathKey]map[string]bool{}
	for _, split := range s.slugs {
		paths := s.Paths(split)
		for _, port := range s.nodes[split].Ports() {
			k := pathKey{split, port}
			s.steps[k] = make(map[string]bool, len(paths.Steps[port]))
			for _, x := range paths.Steps[port] {
				s.steps[k][x] = true
				if _, ok := s.on[x]; !ok {
					s.on[x] = k
				}
			}
		}
	}
	return s.on, s.steps
}

// JoinOf is the join a split's paths meet at: the first any path reaches.
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
