package graph

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// StationsProblems lists what keeps g from being played as stations, a
// party's QR mode, in which reaching a zone means scanning its card. A
// card puts the whole team at one zone at a time, so anything that asks
// where each player stands, how far the team walked, or which of many
// zones it is in cannot be answered. Empty means g plays as stations.
func (g *Graph) StationsProblems() []string {
	var out []string
	for _, n := range g.Nodes {
		if n.PuzzleCfg == nil || n.PuzzleCfg.Answer == nil {
			continue
		}
		switch a := n.PuzzleCfg.Answer; {
		case a.Kind == AnswerSearch:
			out = append(out, fmt.Sprintf("step %q searches its zones", n.Slug))
		case a.Kind == AnswerKeys && len(a.Posts) > 0:
			out = append(out, fmt.Sprintf("step %q turns keys at posts", n.Slug))
		}
	}
	// Conditions sit in gates, switches, hubs and blocks alike, so the
	// encoded graph is searched rather than each walked.
	raw, err := json.Marshal(g)
	if err != nil {
		return append(out, "the graph cannot be read")
	}
	for _, op := range []string{"players_in_zone", "zones_held", "walked"} {
		if strings.Contains(string(raw), `"op":"`+op+`"`) {
			out = append(out, fmt.Sprintf("a condition asks %q", op))
		}
	}
	return out
}

// stationBlocks are the blocks that point a team at a zone's place on
// the map. A station is a card in someone's garden, so they would point
// at the adventure's original streets.
var stationBlocks = []BlockKind{BlockCompass, BlockMap, BlockRadar}

// AsStations returns a copy of g played as stations: Stations set, and
// the blocks that show a zone's place taken out. g is left as it was.
func (g *Graph) AsStations() (*Graph, error) {
	raw, err := json.Marshal(g)
	if err != nil {
		return nil, fmt.Errorf("copying graph: %w", err)
	}
	var c Graph
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("copying graph: %w", err)
	}
	c.Stations = true
	for i := range c.Nodes {
		n := &c.Nodes[i]
		if n.PuzzleCfg != nil {
			n.PuzzleCfg.Blocks = withoutStationBlocks(n.PuzzleCfg.Blocks)
		}
		if n.StoryCfg != nil {
			n.StoryCfg.Blocks = withoutStationBlocks(n.StoryCfg.Blocks)
		}
		if n.EndCfg != nil {
			n.EndCfg.Blocks = withoutStationBlocks(n.EndCfg.Blocks)
		}
	}
	return &c, nil
}

func withoutStationBlocks(bs []Block) []Block {
	return slices.DeleteFunc(bs, func(b Block) bool { return slices.Contains(stationBlocks, b.Kind) })
}
