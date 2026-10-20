package graph

import "slices"

// Paths is the shape of one split: what each of its paths covers, and
// the joins they lead to.
type Paths struct {
	// Steps are the nodes each path reaches, by port, without passing
	// through a join: the steps its group walks alone. A join reached is
	// not in it, nor the split itself.
	Steps map[string][]string
	// Joins are the joins each path reaches, by port, in the order found.
	Joins map[string][]string
}

// SplitPaths works out where a split's paths go. It reads only the
// edges, so it serves a graph mid-edit as well as a valid one.
func (g *Graph) SplitPaths(split string) Paths {
	out := Paths{Steps: map[string][]string{}, Joins: map[string][]string{}}
	n, ok := g.Node(split)
	if !ok || n.SplitCfg == nil {
		return out
	}
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
		seen := map[string]bool{split: true}
		queue := []string{start}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if seen[cur] {
				continue
			}
			seen[cur] = true
			if types[cur] == Join {
				out.Joins[p.Port] = append(out.Joins[p.Port], cur)
				continue
			}
			out.Steps[p.Port] = append(out.Steps[p.Port], cur)
			queue = append(queue, next[cur]...)
		}
	}
	return out
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
