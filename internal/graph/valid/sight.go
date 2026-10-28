package valid

import "github.com/klahr/zonventure/internal/graph"

// Sight lists what stands in the way of a player who cannot see and
// plays with a screen reader: a picture nobody described, a block or a
// puzzle that only works by looking, a place hidden from the phone. It
// is not part of Check, since most adventures are made for sighted
// players and these would only nag; the editor adds it for an adventure
// that says it can be played without sight. All are warnings.
func Sight(g *graph.Graph) []Issue {
	r := &reporter{g: g}
	hidden := map[string]bool{}
	for _, z := range g.Zones {
		if z.Hidden {
			hidden[z.Slug] = true
		}
	}
	for _, n := range g.Nodes {
		for _, b := range n.Blocks() {
			sightBlock(r, n.Slug, b)
		}
		if p := n.PuzzleCfg; p != nil {
			if hidden[p.Zone] {
				r.warn("sight.zone.hidden", n.Slug, "", "zone %q is hidden, so the phone gives no name, map or distance to walk to; a player who cannot see has nothing to go on", p.Zone)
			}
			for _, h := range p.Hints {
				for _, m := range h.Media {
					if m.Kind == graph.MediaImage && m.Alt == "" {
						r.warn("sight.alt", n.Slug, "", "a hint's picture has no description; a screen reader says nothing for it")
					}
				}
			}
			if a := p.Answer; a != nil {
				sightAnswer(r, n.Slug, a)
			}
		}
		if h := n.HubCfg; h != nil {
			for _, b := range h.Branches {
				// The phone reads a tile's label; its picture is decoration.
				if b.Image != nil && b.Label == "" {
					r.warn("sight.alt", n.Slug, "", "a branch is only a picture, with no label; a screen reader says nothing for it")
				}
			}
		}
	}
	return r.issues
}

func sightBlock(r *reporter, node string, b graph.Block) {
	switch b.Kind {
	case graph.BlockTorch, graph.BlockSpotDiff, graph.BlockOverlay:
		r.warn("sight.block", node, "", "a %s block is solved by looking at a picture; a player who cannot see cannot use it", b.Kind)
		return
	case graph.BlockPhoto, graph.BlockDraw:
		r.warn("sight.block", node, "", "a %s block asks the team to look and frame or draw; give a player who cannot see another way through", b.Kind)
		return
	case graph.BlockImage, graph.BlockHotspots:
		if b.Alt == "" {
			r.warn("sight.alt", node, "", "block %q shows a picture with no description; a screen reader says nothing for it", b.ID)
		}
	case graph.BlockSuspects:
		for _, c := range b.Cards {
			if c.Media != 0 && c.Alt == "" {
				r.warn("sight.alt", node, "", "suspect %q has a picture with no description", c.Name)
			}
		}
	}
}

func sightAnswer(r *reporter, node string, a *graph.Answer) {
	switch a.Kind {
	case graph.AnswerTiles:
		r.warn("sight.answer", node, "", "a tile puzzle is solved by looking at the picture; a player who cannot see cannot solve it")
	case graph.AnswerScan:
		r.warn("sight.answer", node, "", "a code to scan must be found and aimed at; let the team type its answer too, or have the code read out")
	case graph.AnswerOrder:
		for _, c := range a.Cards {
			if c.Text == "" && c.Image != nil && c.Image.Alt == "" {
				r.warn("sight.alt", node, "", "an order card is only a picture, with no text or description")
				break
			}
		}
	}
}
