package play

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/icscal"
)

// maxPingGapSeconds is the anti-teleport rule, identical to
// maxLookbackForSpeedSec in internal/player: if the two most recent
// pings are further apart than this, the latest one is not trusted. A
// player cannot pause location sharing, walk somewhere, and reappear
// inside a zone — they have to produce another plausible fix first.
const maxPingGapSeconds = 120.0

// PingLocator reads positions from location_pings.
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

// walkStep is how far a phone must get from where it last counted before
// the stretch counts as walked: GPS wanders a few metres standing still,
// and counting every wobble would walk a team that never moved.
const walkStep = 15.0

// walkedSince is how far the team has walked since a time, in metres:
// the furthest any one of its phones went, counted in straight stretches
// of at least walkStep. Phones are counted apart, as two teammates a
// street apart are not one person zig-zagging across it.
func walkedSince(ctx context.Context, q Querier, teamID int64, since time.Time) (float64, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT slot_id, lat, lng FROM location_pings
		WHERE team_id = $1 AND server_ts >= $2
		ORDER BY slot_id, server_ts, id`, teamID, since.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type path struct {
		lat, lng, walked float64
	}
	paths := map[int64]*path{}
	best := 0.0
	for rows.Next() {
		var slot int64
		var lat, lng float64
		if err := rows.Scan(&slot, &lat, &lng); err != nil {
			return 0, err
		}
		p := paths[slot]
		if p == nil {
			paths[slot] = &path{lat: lat, lng: lng}
			continue
		}
		if d := HaversineMeters(p.lat, p.lng, lat, lng); d >= walkStep {
			p.walked += d
			p.lat, p.lng = lat, lng
			best = max(best, p.walked)
		}
	}
	return best, rows.Err()
}

// walkedGraphs remembers which graphs ask how far a team walked, so the
// pings are read only for those.
var walkedGraphs sync.Map // *graph.Graph -> bool

// usesWalked reports whether any condition in g asks how far the team
// walked. Conditions sit in many places — gates, switches, hub branches,
// blocks — so the encoded graph is searched rather than each walked.
func usesWalked(g *graph.Graph) bool {
	if v, ok := walkedGraphs.Load(g); ok {
		return v.(bool)
	}
	raw, err := json.Marshal(g)
	uses := err == nil && strings.Contains(string(raw), `"op":"walked"`)
	walkedGraphs.Store(g, uses)
	return uses
}

// locate gives the team its position and, where the graph asks, how far
// it has walked on its current node.
func (e *Engine) locate(ctx context.Context, q Querier, s *teamState) error {
	lat, lng, ok, err := e.locator().Position(ctx, q, s.teamID)
	if err != nil {
		return fmt.Errorf("reading position: %w", err)
	}
	if ok {
		s.Position = &session.Position{Lat: lat, Lng: lng}
	}
	if s.G != nil && usesWalked(s.G) {
		if s.WalkedMetres, err = walkedSince(ctx, q, s.teamID, s.NodeEnteredAt); err != nil {
			return fmt.Errorf("reading walked distance: %w", err)
		}
	}
	return nil
}

// HaversineMeters is the great-circle distance used for zones. It lives
// in internal/graph/session with the rest of the rules.
var HaversineMeters = session.HaversineMeters

// teamState is a team's full state, loaded once per action and held in
// memory while the action runs. The rules live on the embedded
// session.Team; this adds the keys needed to write it back.
type teamState struct {
	*session.Team
	teamID, eventID, graphID int64
}

// newTeamState returns an empty state for teamID at now.
func newTeamState(teamID int64, now time.Time) *teamState {
	t := &session.Team{Now: now}
	t.Init()
	return &teamState{Team: t, teamID: teamID}
}

// loadTeam locks the team's state row and reads everything the walk may
// ask about. It must run inside the action's transaction: the row lock
// is what serialises two teammates acting at once.
func (e *Engine) loadTeam(ctx context.Context, tx *sql.Tx, teamID int64) (*teamState, error) {
	s, err := e.loadCore(ctx, tx, teamID, true)
	if err != nil {
		return nil, err
	}

	// Capacity occupancy is read last, under the advisory locks, so it
	// reflects every team that committed before us.
	if err := s.lockAndCountOccupancy(ctx, tx); err != nil {
		return nil, err
	}

	if err := e.locate(ctx, tx, s); err != nil {
		return nil, err
	}
	return s, nil
}

// loadCore reads the team's state row and everything hanging off it.
// forUpdate takes the row lock; a read-only snapshot cannot.
func (e *Engine) loadCore(ctx context.Context, q Querier, teamID int64, forUpdate bool) (*teamState, error) {
	s := newTeamState(teamID, e.now())

	var (
		current   sql.NullString
		entered   sql.NullTime
		completed sql.NullTime
		outcome   sql.NullString
	)
	query := `
		SELECT event_id, graph_id, current_node, node_entered_at, started_at,
		       completed_at, outcome, score, penalty_score, penalty_seconds, theme, last_answer, timed_out, lang, cheated
		FROM team_graph_state
		WHERE team_id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	err := q.QueryRowContext(ctx, query, teamID).Scan(&s.eventID, &s.graphID, &current, &entered, &s.StartedAt,
		&completed, &outcome, &s.Points, &s.PenaltyScore, &s.PenaltySeconds, &s.Theme, &s.LatestAnswer, &s.TimedOut, &s.Lang, &s.Cheated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoGraph
	}
	if err != nil {
		return nil, fmt.Errorf("loading team state: %w", err)
	}
	s.Current = current.String
	if entered.Valid {
		s.NodeEnteredAt = entered.Time
	}
	s.Completed = completed.Valid
	s.Ending = graph.Outcome(outcome.String)

	if s.G, err = e.graph(ctx, q, s.graphID); err != nil {
		return nil, err
	}
	return s, s.loadRest(ctx, q)
}

func (s *teamState) loadRest(ctx context.Context, q Querier) error {
	rows, err := q.QueryContext(ctx, `
		SELECT node, first_seen_at, solved_at IS NOT NULL, outcome, attempts, blocked_until, hints_revealed, answer, match_id
		FROM team_node_state WHERE team_id = $1
	`, s.teamID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			slug    string
			r       session.NodeState
			outcome sql.NullString
			blocked sql.NullTime
		)
		if err := rows.Scan(&slug, &r.FirstSeen, &r.Solved, &outcome, &r.Attempts, &blocked, &r.HintsRevealed, &r.Answer, &r.MatchID); err != nil {
			rows.Close()
			return err
		}
		r.Outcome, r.HasOutcome = outcome.String, outcome.Valid
		if blocked.Valid {
			t := blocked.Time
			r.BlockedUntil = &t
		}
		s.Nodes[slug] = &r
	}
	if err := closeRows(rows); err != nil {
		return err
	}

	if err := scanPairs(ctx, q, `SELECT item, qty FROM team_items WHERE team_id = $1`, s.teamID,
		func(k string, v float64) { s.Items[k] = int(v) }); err != nil {
		return err
	}
	if err := scanPairs(ctx, q, `SELECT entry, 1 FROM team_journal WHERE team_id = $1 ORDER BY unlocked_at, entry`, s.teamID,
		func(k string, _ float64) {
			s.Journal[k] = true
			s.JournalOrder = append(s.JournalOrder, k)
		}); err != nil {
		return err
	}
	if err := scanPairs(ctx, q, `SELECT counter, value FROM event_counters WHERE event_id = $1`, s.eventID,
		func(k string, v float64) { s.Counters[k] = v }); err != nil {
		return err
	}

	prows, err := q.QueryContext(ctx, `SELECT node, block_id, choice FROM team_block_picks WHERE team_id = $1`, s.teamID)
	if err != nil {
		return err
	}
	for prows.Next() {
		var node, block, choice string
		if err := prows.Scan(&node, &block, &choice); err != nil {
			prows.Close()
			return err
		}
		if s.Picks == nil {
			s.Picks = map[string]string{}
		}
		s.Picks[session.PickKey(node, block)] = choice
	}
	if err := closeRows(prows); err != nil {
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
		s.MarkEntered(hub, port)
	}
	if err := closeRows(brows); err != nil {
		return err
	}

	// The slot is the team's rank by id within its event, the key the
	// shuffle rotates by.
	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM teams WHERE event_id = $1 AND id < $2
	`, s.eventID, s.teamID).Scan(&s.SlotIndex); err != nil {
		return err
	}
	return q.QueryRowContext(ctx, `SELECT COUNT(*) FROM player_slots WHERE team_id = $1`, s.teamID).Scan(&s.Size)
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
	for _, n := range s.G.Nodes {
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
		s.Occ[hub+"\x00"+port] = n
	}
	return closeRows(rows)
}

// lockKey maps (event, hub) onto the 64-bit advisory lock space.
func lockKey(eventID int64, hub string) int64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "zv-hub:%d:%s", eventID, hub)
	return int64(h.Sum64())
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
