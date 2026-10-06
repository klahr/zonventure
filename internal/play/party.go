package play

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
)

// ErrUnknownStation means a scanned or typed code is none of the party's
// station cards. Map to 404.
var ErrUnknownStation = errors.New("no such station")

// pinParty stores the graph a game of a party event plays, made from
// its adventure's published version g, and returns its id: g with what
// the host changed (graph.PartyOverrides). In QR mode the zones become
// stations, cards the host hides, so it refuses a graph that asks where
// each player stands (graph.StationsProblems); in park mode the zones
// are where the host put them.
func pinParty(ctx context.Context, tx *sql.Tx, adventureID, eventID int64, mode string, g *graph.Graph) (int64, error) {
	var (
		raw []byte
		o   graph.PartyOverrides
	)
	if err := tx.QueryRowContext(ctx, `SELECT party_overrides FROM events WHERE id = $1`, eventID).Scan(&raw); err != nil {
		return 0, err
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return 0, fmt.Errorf("party overrides: %w", err)
	}
	// What the adventure no longer lets a host change since they changed
	// it is left as the author has it.
	g, err := g.WithOverrides(o)
	if err != nil {
		return 0, err
	}
	if mode == "qr" {
		if p := g.StationsProblems(); len(p) > 0 {
			return 0, fmt.Errorf("the adventure cannot be played with cards: %s", p[0])
		}
		if g, err = g.AsStations(); err != nil {
			return 0, err
		}
		if err := EnsureStations(ctx, tx, eventID, g.Zones); err != nil {
			return 0, err
		}
	}
	return graphstore.SaveRound(ctx, tx, adventureID, eventID, g)
}

// PinRound pins the graph the event's next game plays — its adventure's
// published version as the host has adapted it — and makes it the
// event's, so teams placed from now on play it. Teams of earlier games
// keep theirs. Call it in the transaction that opens the round.
func PinRound(ctx context.Context, tx *sql.Tx, eventID int64) (int64, error) {
	var (
		adventureID int64
		mode        string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT a.id, e.party_mode FROM events e JOIN adventures a ON a.slug = e.adventure_slug WHERE e.id = $1
	`, eventID).Scan(&adventureID, &mode); err != nil {
		return 0, fmt.Errorf("loading event %d: %w", eventID, err)
	}
	stored, err := graphstore.Published(ctx, tx, adventureID)
	if errors.Is(err, graphstore.ErrNotFound) {
		return 0, ErrNoGraph
	}
	if err != nil {
		return 0, err
	}
	graphID, err := pinParty(ctx, tx, adventureID, eventID, mode, stored.Graph)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE events SET graph_id = $1 WHERE id = $2`, graphID, eventID); err != nil {
		return 0, err
	}
	return graphID, nil
}

// Station records that the team scanned a station card, or typed its
// code: in a party played as stations, that puts the team at the card's
// zone until it scans another. It returns the team's view from there.
func (e *Engine) Station(ctx context.Context, teamID int64, code string) (Result, error) {
	code = auth.NormalizeVoucherCode(code)
	if code == "" {
		return Result{}, ErrUnknownStation
	}
	res, err := e.DB.ExecContext(ctx, `
		UPDATE teams t
		   SET station_zone = ps.zone_slug, station_at = now()
		  FROM party_stations ps
		 WHERE t.id = $1 AND ps.event_id = t.event_id AND ps.code = $2
	`, teamID, code)
	if err != nil {
		return Result{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Result{}, ErrUnknownStation
	}
	v, err := e.Snapshot(ctx, teamID)
	if err != nil {
		return Result{}, err
	}
	return Result{Accepted: true, View: v}, nil
}

// EnsureStations gives every zone a card code of its own for the party,
// keeping the codes it already has: the host may print the cards before
// the adventure's author adds a zone. Codes are unique within the party
// only, which is all a scan is checked against.
func EnsureStations(ctx context.Context, q Querier, eventID int64, zones []graph.Zone) error {
	for _, z := range zones {
		for tries := 0; ; tries++ {
			code, err := auth.NewVoucherCode()
			if err != nil {
				return err
			}
			if _, err := q.ExecContext(ctx, `
				INSERT INTO party_stations (event_id, zone_slug, code) VALUES ($1, $2, $3)
				ON CONFLICT DO NOTHING
			`, eventID, z.Slug, code); err != nil {
				return fmt.Errorf("station for zone %q: %w", z.Slug, err)
			}
			var has bool
			if err := q.QueryRowContext(ctx,
				`SELECT EXISTS (SELECT 1 FROM party_stations WHERE event_id = $1 AND zone_slug = $2)`,
				eventID, z.Slug).Scan(&has); err != nil {
				return err
			}
			if has {
				break
			}
			// The code was another zone's: draw again.
			if tries > 5 {
				return fmt.Errorf("station for zone %q: no free code", z.Slug)
			}
		}
	}
	return nil
}
