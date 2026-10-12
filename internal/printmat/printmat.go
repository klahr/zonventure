// Package printmat resolves an event's print materials: those of the
// adventure version it plays, each delivered the way the event says or
// else the way the adventure does, and which file a team gets.
//
// Delivery is the event's to change because it is about operations, not
// content: an organiser may email the letter for self-printing in summer
// and post it in winter, without publishing a new version.
package printmat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
)

// Overrides maps a print material's id to the event's own delivery.
type Overrides map[string]graph.Delivery

// Parse reads an event's print_delivery column. Unknown deliveries are
// dropped, so a bad row reads as the adventure's.
func Parse(raw []byte) Overrides {
	o := Overrides{}
	if len(raw) == 0 || json.Unmarshal(raw, &o) != nil {
		return Overrides{}
	}
	for id, d := range o {
		if !Valid(d) {
			delete(o, id)
		}
	}
	return o
}

// Valid reports whether d is a delivery.
func Valid(d graph.Delivery) bool {
	switch d {
	case graph.DeliveryPosted, graph.DeliveryEmailed, graph.DeliveryOrganiser:
		return true
	}
	return false
}

// Effective returns g's print materials as the event delivers them.
func Effective(g *graph.Graph, o Overrides) []graph.PrintMaterial {
	if g == nil {
		return nil
	}
	out := make([]graph.PrintMaterial, len(g.PrintMaterials))
	for i, pm := range g.PrintMaterials {
		if d, ok := o[pm.ID]; ok {
			pm.Delivery = d
		}
		out[i] = pm
	}
	return out
}

// File picks pm's file for a team in lang: lang's, else the adventure's
// main language's, else the first it has. It returns the language picked
// and the media id, or "" and 0 when pm has no file.
func File(pm graph.PrintMaterial, lang, main string) (string, int64) {
	for _, l := range []string{lang, main} {
		if id := pm.Files[l]; l != "" && id > 0 {
			return l, id
		}
	}
	for _, l := range slices.Sorted(maps.Keys(pm.Files)) {
		if id := pm.Files[l]; id > 0 {
			return l, id
		}
	}
	return "", 0
}

// ForEvent loads the adventure version an event plays (the published one
// while the event has not pinned its own) and the event's deliveries.
// An adventure without a graph has no version: nil, without an error.
func ForEvent(ctx context.Context, q graphstore.Querier, eventID int64) (*graph.Graph, Overrides, error) {
	var (
		graphID, advID int64
		raw            []byte
	)
	if err := q.QueryRowContext(ctx, `
		SELECT COALESCE(e.graph_id, 0), COALESCE(a.id, 0), e.print_delivery
		FROM events e LEFT JOIN adventures a ON a.slug = e.adventure_slug
		WHERE e.id = $1
	`, eventID).Scan(&graphID, &advID, &raw); err != nil {
		return nil, nil, fmt.Errorf("reading event %d: %w", eventID, err)
	}
	var (
		s   graphstore.Stored
		err error
	)
	switch {
	case graphID > 0:
		s, err = graphstore.ByID(ctx, q, graphID)
	case advID > 0:
		s, err = graphstore.Published(ctx, q, advID)
	default:
		return nil, Parse(raw), nil
	}
	if errors.Is(err, graphstore.ErrNotFound) {
		return nil, Parse(raw), nil
	}
	if err != nil {
		return nil, nil, err
	}
	return s.Graph, Parse(raw), nil
}
