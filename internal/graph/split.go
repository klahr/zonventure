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

// SplitPaths works out where a split's paths go. It reads only the
// edges, so it serves a graph mid-edit as well as a valid one.
func (g *Graph) SplitPaths(split string) Paths {
	return g.Splits().Paths(split)
}

// Splits works out the graph's splits' paths, each once. It is a
// snapshot of the graph as it was when made.
type Splits struct {
	g     *Graph
	nodes map[string]Node
	next  map[string][]string
	paths map[string]Paths
}

// Splits starts working out the graph's splits; each is worked out when
// first asked for.
func (g *Graph) Splits() *Splits {
	s := &Splits{
		g:     g,
		nodes: make(map[string]Node, len(g.Nodes)),
		next:  make(map[string][]string, len(g.Edges)),
		paths: map[string]Paths{},
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
			out.Joins[port] = append(out.Joins[port], cur)
		case Split:
			if cur == split {
				out.Loops = append(out.Loops, port)
			} else {
				out.Nested[port] = append(out.Nested[port], cur)
			}
		default:
			out.Steps[port] = append(out.Steps[port], cur)
			queue = append(queue, s.next[cur]...)
		}
	}
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
// of each path. A node on two paths, which valid refuses, is the first
// split's in order of slug, and its first port.
func (s *Splits) onPaths() (map[string]pathKey, map[pathKey]map[string]bool) {
	var splits []string
	for slug, n := range s.nodes {
		if n.Type == Split && n.SplitCfg != nil {
			splits = append(splits, slug)
		}
	}
	slices.Sort(splits)
	on := map[string]pathKey{}
	steps := map[pathKey]map[string]bool{}
	for _, split := range splits {
		paths := s.Paths(split)
		for _, port := range s.nodes[split].Ports() {
			k := pathKey{split, port}
			steps[k] = make(map[string]bool, len(paths.Steps[port]))
			for _, x := range paths.Steps[port] {
				steps[k][x] = true
				if _, ok := on[x]; !ok {
					on[x] = k
				}
			}
		}
	}
	return on, steps
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
