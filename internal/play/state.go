package play

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/icscal"
)

// maxPingGapSeconds is the anti-teleport rule, identical to
// maxLookbackForSpeedSec in internal/player: if the two most recent
// pings are further apart than this, the latest one is not trusted. A
// player cannot pause location sharing, walk somewhere, and reappear
// inside a zone — they have to produce another plausible fix first.
const maxPingGapSeconds = 120.0

// PingLocator reads positions from location_pings with the same rule the
// lane runtime's gateReached uses.
type PingLocator struct{}

// Position implements Locator.
func (PingLocator) Position(ctx context.Context, q Querier, teamID int64) (float64, float64, bool, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT lat, lng, server_ts FROM location_pings
		WHERE team_id = $1
		ORDER BY server_ts DESC
		LIMIT 2
	`, teamID)
	if err != nil {
		return 0, 0, false, err
	}
	defer rows.Close()

	type ping struct {
		lat, lng float64
		ts       time.Time
		ok       bool
	}
	var pings []ping
	for rows.Next() {
		var p ping
		var ts string
		if err := rows.Scan(&p.lat, &p.lng, &ts); err != nil {
			return 0, 0, false, err
		}
		p.ts, p.ok = icscal.ParseTimestamp(ts)
		pings = append(pings, p)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, false, err
	}
	if len(pings) == 0 {
		return 0, 0, false, nil
	}
	if len(pings) == 2 && pings[0].ok && pings[1].ok &&
		pings[0].ts.Sub(pings[1].ts).Seconds() > maxPingGapSeconds {
		return 0, 0, false, nil
	}
	return pings[0].lat, pings[0].lng, true, nil
}

// HaversineMeters is the great-circle distance, R = 6371000, matching
// the lane runtime exactly so a zone admits the same positions.
func HaversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371000.0
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat, dLng := rad(lat2-lat1), rad(lng2-lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return r * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// nodeRow is one team_node_state row.
type nodeRow struct {
	firstSeen     time.Time
	solved        bool
	outcome       string
	hasOutcome    bool
	attempts      int
	blockedUntil  *time.Time
	hintsRevealed int
}

// teamState is a team's full state, loaded once per action and held in
// memory while the walk runs. It implements walk.State.
type teamState struct {
	teamID, eventID, graphID int64
	g                        *graph.Graph

	current       string
	nodeEnteredAt time.Time
	startedAt     time.Time
	completed     bool
	outcome       graph.Outcome

	score, penaltyScore float64
	penaltySeconds      int
	theme               string

	nodes    map[string]*nodeRow
	items    map[string]int
	journal  map[string]bool
	counters map[string]float64
	entered  map[string]map[string]bool // hub -> port -> ever entered
	occ      map[string]int             // "hub\x00port" -> other teams inside

	slot, teamSize int
	now            time.Time

	lat, lng float64
	hasPos   bool
}

// loadTeam locks the team's state row and reads everything the walk may
// ask about. It must run inside the action's transaction: the row lock
// is what serialises two teammates acting at once.
func (e *Engine) loadTeam(ctx context.Context, tx *sql.Tx, teamID int64) (*teamState, error) {
	s := &teamState{
		teamID:   teamID,
		now:      e.now(),
		nodes:    map[string]*nodeRow{},
		items:    map[string]int{},
		journal:  map[string]bool{},
		counters: map[string]float64{},
		entered:  map[string]map[string]bool{},
		occ:      map[string]int{},
	}

	var (
		current   sql.NullString
		entered   sql.NullTime
		completed sql.NullTime
		outcome   sql.NullString
	)
	err := tx.QueryRowContext(ctx, `
		SELECT event_id, graph_id, current_node, node_entered_at, started_at,
		       completed_at, outcome, score, penalty_score, penalty_seconds, theme
		FROM team_graph_state
		WHERE team_id = $1
		FOR UPDATE
	`, teamID).Scan(&s.eventID, &s.graphID, &current, &entered, &s.startedAt,
		&completed, &outcome, &s.score, &s.penaltyScore, &s.penaltySeconds, &s.theme)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoGraph
	}
	if err != nil {
		return nil, fmt.Errorf("loading team state: %w", err)
	}
	s.current = current.String
	if entered.Valid {
		s.nodeEnteredAt = entered.Time
	}
	s.completed = completed.Valid
	s.outcome = graph.Outcome(outcome.String)

	if s.g, err = e.graph(ctx, tx, s.graphID); err != nil {
		return nil, err
	}
	if err := s.loadRest(ctx, tx); err != nil {
		return nil, err
	}

	// Capacity occupancy is read last, under the advisory locks, so it
	// reflects every team that committed before us.
	if err := s.lockAndCountOccupancy(ctx, tx); err != nil {
		return nil, err
	}

	lat, lng, ok, err := e.locator().Position(ctx, tx, teamID)
	if err != nil {
		return nil, fmt.Errorf("reading position: %w", err)
	}
	s.lat, s.lng, s.hasPos = lat, lng, ok
	return s, nil
}

func (s *teamState) loadRest(ctx context.Context, q Querier) error {
	rows, err := q.QueryContext(ctx, `
		SELECT node, first_seen_at, solved_at IS NOT NULL, outcome, attempts, blocked_until, hints_revealed
		FROM team_node_state WHERE team_id = $1
	`, s.teamID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			slug    string
			r       nodeRow
			outcome sql.NullString
			blocked sql.NullTime
		)
		if err := rows.Scan(&slug, &r.firstSeen, &r.solved, &outcome, &r.attempts, &blocked, &r.hintsRevealed); err != nil {
			rows.Close()
			return err
		}
		r.outcome, r.hasOutcome = outcome.String, outcome.Valid
		if blocked.Valid {
			t := blocked.Time
			r.blockedUntil = &t
		}
		s.nodes[slug] = &r
	}
	if err := closeRows(rows); err != nil {
		return err
	}

	if err := scanPairs(ctx, q, `SELECT item, qty FROM team_items WHERE team_id = $1`, s.teamID,
		func(k string, v float64) { s.items[k] = int(v) }); err != nil {
		return err
	}
	if err := scanPairs(ctx, q, `SELECT entry, 1 FROM team_journal WHERE team_id = $1`, s.teamID,
		func(k string, _ float64) { s.journal[k] = true }); err != nil {
		return err
	}
	if err := scanPairs(ctx, q, `SELECT counter, value FROM event_counters WHERE event_id = $1`, s.eventID,
		func(k string, v float64) { s.counters[k] = v }); err != nil {
		return err
	}

	brows, err := q.QueryContext(ctx, `SELECT hub, port FROM team_hub_branches WHERE team_id = $1`, s.teamID)
	if err != nil {
		return err
	}
	for brows.Next() {
		var hub, port string
		if err := brows.Scan(&hub, &port); err != nil {
			brows.Close()
			return err
		}
		s.markEntered(hub, port)
	}
	if err := closeRows(brows); err != nil {
		return err
	}

	// The slot is the team's rank by id within its event — the same key
	// lanes.teamSlot uses, so the shuffle is identical.
	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM teams WHERE event_id = $1 AND id < $2
	`, s.eventID, s.teamID).Scan(&s.slot); err != nil {
		return err
	}
	return q.QueryRowContext(ctx, `SELECT COUNT(*) FROM player_slots WHERE team_id = $1`, s.teamID).Scan(&s.teamSize)
}

// lockAndCountOccupancy takes one transaction-scoped advisory lock per
// capacity hub, in a fixed order so two transactions can never deadlock,
// then counts the other teams currently inside each branch.
//
// The lock is what makes the count honest: without it, two teams
// arriving at the last free slot both read "one space left" and both
// go in. Only capacity hubs are locked — other policies do not read
// occupancy, and serialising them would slow every event for nothing.
func (s *teamState) lockAndCountOccupancy(ctx context.Context, tx *sql.Tx) error {
	var hubs []string
	for _, n := range s.g.Nodes {
		if n.Type == graph.Hub && n.HubCfg != nil &&
			(n.HubCfg.Policy == graph.PolicyCapacity || n.HubCfg.Policy == graph.PolicySpread) {
			hubs = append(hubs, n.Slug)
		}
	}
	if len(hubs) == 0 {
		return nil
	}
	sort.Strings(hubs)
	for _, hub := range hubs {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey(s.eventID, hub)); err != nil {
			return fmt.Errorf("locking hub %s: %w", hub, err)
		}
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT hub, port, COUNT(*) FROM team_hub_branches
		WHERE event_id = $1 AND returned_at IS NULL AND team_id <> $2
		GROUP BY hub, port
	`, s.eventID, s.teamID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var hub, port string
		var n int
		if err := rows.Scan(&hub, &port, &n); err != nil {
			rows.Close()
			return err
		}
		s.occ[hub+"\x00"+port] = n
	}
	return closeRows(rows)
}

// lockKey maps (event, hub) onto the 64-bit advisory lock space.
func lockKey(eventID int64, hub string) int64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "zv-hub:%d:%s", eventID, hub)
	return int64(h.Sum64())
}

func (s *teamState) markEntered(hub, port string) {
	if s.entered[hub] == nil {
		s.entered[hub] = map[string]bool{}
	}
	s.entered[hub][port] = true
}

func (s *teamState) row(slug string) *nodeRow {
	r := s.nodes[slug]
	if r == nil {
		r = &nodeRow{firstSeen: s.now}
		s.nodes[slug] = r
	}
	return r
}

// --- walk.State --------------------------------------------------------

func (s *teamState) Solved(n string) bool {
	r := s.nodes[n]
	return r != nil && r.solved
}

func (s *teamState) Visited(n string) bool { return s.nodes[n] != nil }

func (s *teamState) Outcome(n string) (string, bool) {
	r := s.nodes[n]
	if r == nil || !r.hasOutcome {
		return "", false
	}
	return r.outcome, true
}

func (s *teamState) Attempts(n string) int {
	if r := s.nodes[n]; r != nil {
		return r.attempts
	}
	return 0
}

func (s *teamState) ItemQty(i string) int { return s.items[i] }

func (s *teamState) CounterValue(c string) float64 {
	if v, ok := s.counters[c]; ok {
		return v
	}
	// A counter no team has touched yet reads as its declared initial
	// value, not zero.
	for _, def := range s.g.Counters {
		if def.Slug == c {
			return def.Initial
		}
	}
	return 0
}

// Score is the net score — points earned minus hint and penalty costs —
// because that is the figure a "score ≥ 100" gate means.
func (s *teamState) Score() float64 { return s.score - s.penaltyScore }

func (s *teamState) ElapsedSeconds() float64 {
	return s.now.Sub(s.startedAt).Seconds()
}

func (s *teamState) NodeElapsedSeconds() float64 {
	if s.nodeEnteredAt.IsZero() {
		return 0
	}
	return s.now.Sub(s.nodeEnteredAt).Seconds()
}

func (s *teamState) TeamSize() int { return s.teamSize }

func (s *teamState) InZone(slug string) bool {
	if !s.hasPos {
		return false
	}
	for _, z := range s.g.Zones {
		if z.Slug == slug {
			return HaversineMeters(s.lat, s.lng, z.Lat, z.Lng) <= z.RadiusM
		}
	}
	return false
}

func (s *teamState) LocalTime() time.Time { return s.now }

func (s *teamState) Slot() int { return s.slot }

func (s *teamState) BranchDone(hub, port string) bool { return s.entered[hub][port] }

func (s *teamState) CompletedBranches(hub string) int { return len(s.entered[hub]) }

func (s *teamState) Occupancy(hub, port string) int { return s.occ[hub+"\x00"+port] }

// ApplyEffect folds an effect into memory as the walk passes it, so a
// gate later in the same step sees it. Persistence happens afterwards,
// from Step.Effects, in applyEffects.
func (s *teamState) ApplyEffect(_ string, e graph.EffectConfig) {
	switch e.Kind {
	case graph.GrantItem:
		s.items[e.Item] += qtyOf(e)
	case graph.AdjustScore:
		s.score += float64(e.Points)
	case graph.SetCounter:
		s.counters[e.Counter] = s.nextCounterValue(e)
	case graph.UnlockJournal:
		s.journal[e.JournalEntry] = true
	case graph.SetTheme:
		s.theme = e.Theme
	}
}

// nextCounterValue computes a counter's value after an effect, clamped
// to its declared bounds.
func (s *teamState) nextCounterValue(e graph.EffectConfig) float64 {
	v := e.Value
	if e.Op != graph.CounterSet {
		v = s.CounterValue(e.Counter) + e.Value
	}
	for _, def := range s.g.Counters {
		if def.Slug != e.Counter {
			continue
		}
		if def.Min != nil && v < *def.Min {
			v = *def.Min
		}
		if def.Max != nil && v > *def.Max {
			v = *def.Max
		}
	}
	return v
}

func qtyOf(e graph.EffectConfig) int {
	if e.Qty == 0 {
		return 1
	}
	return e.Qty
}

// --- helpers -------------------------------------------------------------

func scanPairs(ctx context.Context, q Querier, query string, arg int64, put func(string, float64)) error {
	rows, err := q.QueryContext(ctx, query, arg)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k string
		var v float64
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return err
		}
		put(k, v)
	}
	return closeRows(rows)
}

func closeRows(rows *sql.Rows) error {
	err := rows.Err()
	if cerr := rows.Close(); err == nil {
		err = cerr
	}
	return err
}
