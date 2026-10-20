package graph

import "slices"

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
	return g.splitPaths(split, map[string]bool{})
}

func (g *Graph) splitPaths(split string, enclosing map[string]bool) Paths {
	out := Paths{Steps: map[string][]string{}, Joins: map[string][]string{}, Inner: map[string][]string{}}
	n, ok := g.Node(split)
	if !ok || n.SplitCfg == nil {
		return out
	}
	enclosing[split] = true
	defer delete(enclosing, split)
	next := make(map[string][]string, len(g.Edges))
	for _, e := range g.Edges {
		next[e.From] = append(next[e.From], e.To)
	}
	types := make(map[string]Type, len(g.Nodes))
	for _, x := range g.Nodes {
		types[x.Slug] = x.Type
	}
	for _, p := range n.SplitCfg.Paths {
		start, ok := g.Target(split, p.Port)
		if !ok {
			continue
		}
		seen := map[string]bool{}
		queue := []string{start}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if types[cur] == Split && enclosing[cur] {
				if !slices.Contains(out.Loops, p.Port) {
					out.Loops = append(out.Loops, p.Port)
				}
				continue
			}
			if seen[cur] {
				continue
			}
			seen[cur] = true
			switch types[cur] {
			case Join:
				out.Joins[p.Port] = append(out.Joins[p.Port], cur)
			case Split:
				// A group of this path splitting again: the inner split's
				// paths and join are this path's, and it goes on from there.
				inner := g.splitPaths(cur, enclosing)
				out.Steps[p.Port] = append(out.Steps[p.Port], cur)
				out.Inner[p.Port] = append(out.Inner[p.Port], cur)
				if len(inner.Loops) > 0 && !slices.Contains(out.Loops, p.Port) {
					out.Loops = append(out.Loops, p.Port)
				}
				for _, q := range n2p(g, cur) {
					for _, s := range inner.Steps[q] {
						if !seen[s] {
							seen[s] = true
							out.Steps[p.Port] = append(out.Steps[p.Port], s)
						}
					}
					out.Inner[p.Port] = append(out.Inner[p.Port], inner.Inner[q]...)
					for _, j := range inner.Joins[q] {
						if !seen[j] {
							seen[j] = true
							out.Steps[p.Port] = append(out.Steps[p.Port], j)
							queue = append(queue, next[j]...)
						}
					}
				}
			default:
				out.Steps[p.Port] = append(out.Steps[p.Port], cur)
				queue = append(queue, next[cur]...)
			}
		}
	}
	return out
}

// n2p is a split's ports, in path order.
func n2p(g *Graph, split string) []string {
	n, _ := g.Node(split)
	return n.Ports()
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

// JoinOf is the join a split's paths meet at: the first any path reaches.
func (g *Graph) JoinOf(split string) (string, bool) {
	n, ok := g.Node(split)
	if !ok || n.SplitCfg == nil {
		return "", false
	}
	paths := g.SplitPaths(split)
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
	return g.splitDepth(split, map[string]bool{}, map[string]int{})
}

func (g *Graph) splitDepth(split string, seen map[string]bool, memo map[string]int) int {
	if d, ok := memo[split]; ok {
		return d
	}
	if seen[split] {
		return 0 // a loop, which the validator reports as such
	}
	seen[split] = true
	defer delete(seen, split)
	deepest := 0
	for _, inner := range g.SplitPaths(split).Inner {
		for _, s := range inner {
			deepest = max(deepest, g.splitDepth(s, seen, memo))
		}
	}
	memo[split] = 1 + deepest
	return 1 + deepest
}
