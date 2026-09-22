// Package convert turns a lane-and-page adventure into a graph.
//
// Every adventure authored so far is in the lane model: an ordered list
// of lanes, each linear or shuffled, each holding ordered pages. The
// conversion is mechanical, and it has to be faithful — a converted
// adventure must play exactly as it did, or converting it silently
// changes where teams walk. The rules:
//
//   - Lanes run in index order, each one finishing before the next.
//   - A linear lane becomes a chain of nodes.
//   - A shuffled lane becomes a Hub with the shuffle policy and every
//     branch required. walk.PolicyShuffle reproduces
//     adventure.LanePermutation's order exactly, including as branches
//     complete; convert_test.go checks that against the real function.
//   - A page with accepted answers becomes a Puzzle with one "solved"
//     output. A page with none becomes a Story: zv shows those as a
//     Continue button, which is precisely a narrative beat.
//   - A page with a radius becomes a zone, and the node is gated on it.
//   - Disabled pages are dropped, as the runtime already skips them.
//
// What does not carry over, because zv never had it: scoring (points
// are zero) and hint costs (zero). Images and audio are referenced by
// page index in the v1 archive and are handled by the importer, not
// here.
package convert

import (
	"fmt"
	"sort"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
)

// The v1 shapes, mirroring the fields of internal/admin/portability.go's
// exportLane and exportPage that bear on structure. Declared here rather
// than imported so this package stays free of the admin layer.

// Lane is one v1 lane.
type Lane struct {
	Idx  int    `json:"idx"`
	Mode string `json:"mode"`
	Name string `json:"name,omitempty"`
}

// Accept is one v1 accepted answer.
type Accept struct {
	Text  string `json:"text"`
	Regex bool   `json:"regex,omitempty"`
}

// Hint is one v1 hint.
type Hint struct {
	Idx          int    `json:"idx"`
	Text         string `json:"text"`
	DelaySeconds int    `json:"delay_seconds"`
}

// Page is one v1 page (puzzle).
type Page struct {
	Idx      int      `json:"idx"`
	LaneIdx  int      `json:"lane_idx"`
	Title    string   `json:"title"`
	Prompt   string   `json:"prompt,omitempty"`
	Accept   []Accept `json:"accept,omitempty"`
	Lat      float64  `json:"lat"`
	Lng      float64  `json:"lng"`
	RadiusM  *float64 `json:"radius_m,omitempty"`
	Disabled bool     `json:"disabled,omitempty"`
	Hints    []Hint   `json:"hints,omitempty"`
}

// Adventure is the structural part of a v1 envelope.
type Adventure struct {
	Lanes []Lane `json:"lanes"`
	Pages []Page `json:"pages"`
}

const (
	shuffled = "shuffled"

	// Canvas spacing, so a converted graph opens laid out rather than
	// piled at the origin. Cosmetic only.
	rowGap    = 140.0
	columnGap = 280.0
)

// PageSlug is the node slug a converted page gets. Stable, so that a
// page's history and any MCP tool addressing it by index still line up
// after conversion.
func PageSlug(idx int) string { return fmt.Sprintf("page-%d", idx) }

// LaneSlug is the hub slug a converted shuffled lane gets.
func LaneSlug(idx int) string { return fmt.Sprintf("lane-%d", idx) }

func zoneSlug(pageIdx int) string { return fmt.Sprintf("zone-page-%d", pageIdx) }

// ToGraph converts a v1 adventure. The result always validates when the
// input has at least one enabled page; an adventure with none converts
// to Start → End, which is valid but plays as an immediate finish.
func ToGraph(a Adventure) *graph.Graph {
	g := &graph.Graph{}
	g.Nodes = append(g.Nodes, graph.Node{Slug: "start", Type: graph.Start, Title: "Start"})

	lanes := groupPages(a)

	// prev is the port the next node attaches to: the chain's open end.
	prevNode, prevPort := "start", graph.PortOut
	row := 1

	attach := func(to string) {
		g.Edges = append(g.Edges, graph.Edge{From: prevNode, FromPort: prevPort, To: to})
	}

	for _, l := range lanes {
		if len(l.pages) == 0 {
			continue
		}

		if l.mode != shuffled {
			for _, p := range l.pages {
				n := pageNode(g, p, 0, float64(row)*rowGap)
				attach(n.Slug)
				prevNode, prevPort = n.Slug, portOut(n)
				row++
			}
			continue
		}

		// Shuffled lane → hub. Every page is a branch that loops back.
		hub := graph.Node{
			Slug:  LaneSlug(l.idx),
			Type:  graph.Hub,
			Title: l.name,
			Y:     float64(row) * rowGap,
			HubCfg: &graph.HubConfig{
				Policy:   graph.PolicyShuffle,
				Required: 0, // all
			},
		}
		row++
		for i, p := range l.pages {
			port := PageSlug(p.Idx)
			hub.HubCfg.Branches = append(hub.HubCfg.Branches, graph.Branch{Port: port, Label: p.Title})
			n := pageNode(g, p, float64(i+1)*columnGap, float64(row)*rowGap)
			g.Edges = append(g.Edges,
				graph.Edge{From: hub.Slug, FromPort: port, To: n.Slug},
				graph.Edge{From: n.Slug, FromPort: portOut(n), To: hub.Slug},
			)
		}
		row++
		g.Nodes = append(g.Nodes, hub)
		attach(hub.Slug)
		prevNode, prevPort = hub.Slug, graph.PortDone
	}

	end := graph.Node{Slug: "end", Type: graph.End, Title: "End", Y: float64(row) * rowGap}
	g.Nodes = append(g.Nodes, end)
	attach(end.Slug)
	return g
}

type laneGroup struct {
	idx   int
	mode  string
	name  string
	pages []Page
}

// groupPages buckets enabled pages by lane, in the order the runtime
// plays them. A page naming a lane that does not exist falls back to
// lane 0, which is what the v1 importer does; an adventure with no lanes
// at all gets one implicit linear lane, which is what zv synthesises.
func groupPages(a Adventure) []laneGroup {
	byIdx := map[int]*laneGroup{}
	for _, l := range a.Lanes {
		byIdx[l.Idx] = &laneGroup{idx: l.Idx, mode: l.Mode, name: l.Name}
	}
	if len(byIdx) == 0 {
		byIdx[0] = &laneGroup{idx: 0}
	}

	fallback := 0
	if _, ok := byIdx[0]; !ok {
		// No lane 0: use the lowest-numbered lane instead.
		fallback = minKey(byIdx)
	}

	for _, p := range a.Pages {
		if p.Disabled {
			continue
		}
		lg, ok := byIdx[p.LaneIdx]
		if !ok {
			lg = byIdx[fallback]
		}
		lg.pages = append(lg.pages, p)
	}

	out := make([]laneGroup, 0, len(byIdx))
	for _, lg := range byIdx {
		sort.SliceStable(lg.pages, func(i, j int) bool { return lg.pages[i].Idx < lg.pages[j].Idx })
		out = append(out, *lg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].idx < out[j].idx })
	return out
}

func minKey(m map[int]*laneGroup) int {
	first := true
	lowest := 0
	for k := range m {
		if first || k < lowest {
			lowest, first = k, false
		}
	}
	return lowest
}

// pageNode builds the node for one page, declaring its zone if it has
// one, and appends it to the graph.
func pageNode(g *graph.Graph, p Page, x, y float64) graph.Node {
	zone := ""
	if p.RadiusM != nil && *p.RadiusM > 0 {
		zone = zoneSlug(p.Idx)
		g.Zones = append(g.Zones, graph.Zone{
			Slug: zone, Name: p.Title, Lat: p.Lat, Lng: p.Lng, RadiusM: *p.RadiusM,
		})
	}

	n := graph.Node{Slug: PageSlug(p.Idx), Title: p.Title, X: x, Y: y}

	if len(p.Accept) == 0 {
		// No answer: zv shows a Continue button. That is a Story.
		n.Type = graph.Story
		n.StoryCfg = &graph.StoryConfig{Text: p.Prompt, Zone: zone}
		g.Nodes = append(g.Nodes, n)
		return n
	}

	matchers := make([]match.Matcher, 0, len(p.Accept))
	for _, a := range p.Accept {
		kind := match.Exact
		if a.Regex {
			kind = match.Regex
		}
		matchers = append(matchers, match.Matcher{Kind: kind, Value: a.Text})
	}

	hints := make([]graph.Hint, 0, len(p.Hints))
	sorted := append([]Hint(nil), p.Hints...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Idx < sorted[j].Idx })
	for _, h := range sorted {
		hints = append(hints, graph.Hint{Text: h.Text, DelaySeconds: h.DelaySeconds})
	}

	n.Type = graph.Puzzle
	n.PuzzleCfg = &graph.PuzzleConfig{
		Prompt: p.Prompt,
		Zone:   zone,
		Hints:  hints,
		Outputs: []graph.Output{{
			Port: "solved", Label: "Solved", Trigger: graph.OnAnswer, Matchers: matchers,
		}},
	}
	g.Nodes = append(g.Nodes, n)
	return n
}

func portOut(n graph.Node) string {
	if n.Type == graph.Story {
		return graph.PortOut
	}
	return "solved"
}
