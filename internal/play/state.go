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
// maxLookbackForSpeedSec in internal/player: if a phone's two most recent
// pings are further apart than this, its latest one is not trusted. A
// player cannot pause location sharing, walk somewhere, and reappear
// inside a zone — they have to produce another plausible fix first.
//
// The rule is per phone. Teammates may be streets apart, and comparing
// one phone's ping with another's says nothing about either.
const maxPingGapSeconds = 120.0

// MaxPingSpeedMS is maxPlausibleSpeedMS in internal/player: a move between
// two pings faster than this is a GPS jump, not a walk, so it counts
// toward no distance walked.
const MaxPingSpeedMS = 60.0

// phoneFreshSeconds is how recently a phone must have reported to count
// as standing where it last was, for conditions on where the players
// are. Phones report every 15 seconds, and every 3 while a step waits on
// a zone; a phone silent for longer has been pocketed or closed, and
// must not hold a zone for its player while they walk away.
const phoneFreshSeconds = 60.0

// maxFixAccuracy is the widest fix, in metres, that counts for where a
// team is: a phone indoors may report a fix hundreds of metres wide,
// which could put the team in a zone it is nowhere near. Such pings are
// kept for the map, only not trusted. A ping without an accuracy (an
// older client, a simulated one) is trusted as before.
const maxFixAccuracy = 100.0

// PingLocator reads positions from location_pings.
type PingLocator struct{}

// fix is a phone's latest trustworthy ping.
type fix struct {
	slot     int64
	lat, lng float64
	at       time.Time
}

// fixes is the latest trustworthy ping of each of the team's phones, in
// slot order. A phone that turned location sharing off has none from
// before then.
func fixes(ctx context.Context, q Querier, teamID int64) ([]fix, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT s.id, p.lat, p.lng, p.server_ts
		FROM player_slots s
		CROSS JOIN LATERAL (
			SELECT lat, lng, server_ts, id FROM location_pings
			WHERE slot_id = s.id AND (accuracy IS NULL OR accuracy <= $2)
			  AND (s.location_off_at IS NULL OR server_ts > s.location_off_at)
			ORDER BY server_ts DESC, id DESC
			LIMIT 2
		) p
		WHERE s.team_id = $1
		ORDER BY s.id, p.server_ts DESC, p.id DESC
	`, teamID, maxFixAccuracy)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type ping struct {
		fix
		ok bool
	}
	bySlot := map[int64][]ping{}
	var order []int64
	for rows.Next() {
		var slot int64
		var p ping
		var ts string
		if err := rows.Scan(&slot, &p.lat, &p.lng, &ts); err != nil {
			return nil, err
		}
		p.slot = slot
		p.at, p.ok = icscal.ParseTimestamp(ts)
		if _, seen := bySlot[slot]; !seen {
			order = append(order, slot)
		}
		bySlot[slot] = append(bySlot[slot], p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []fix
	for _, slot := range order {
		pings := bySlot[slot]
		if len(pings) == 2 && pings[0].ok && pings[1].ok &&
			pings[0].at.Sub(pings[1].at).Seconds() > maxPingGapSeconds {
			continue
		}
		out = append(out, pings[0].fix)
	}
	return out, nil
}

// Position implements Locator: the most recent of the phones' trusted
// pings, however old.
func (PingLocator) Position(ctx context.Context, q Querier, teamID int64) (float64, float64, bool, error) {
	fs, err := fixes(ctx, q, teamID)
	if err != nil || len(fs) == 0 {
		return 0, 0, false, err
	}
	latest := fs[0]
	for _, f := range fs[1:] {
		if f.at.After(latest.at) {
			latest = f
		}
	}
	return latest.lat, latest.lng, true, nil
}

// Phones implements PhoneLocator: the trusted pings of the phones that
// have reported within phoneFreshSeconds of now.
func (PingLocator) Phones(ctx context.Context, q Querier, teamID int64, now time.Time) ([]Phone, error) {
	fs, err := fixes(ctx, q, teamID)
	if err != nil {
		return nil, err
	}
	out := []Phone{}
	for _, f := range fs {
		if now.Sub(f.at).Seconds() <= phoneFreshSeconds {
			out = append(out, Phone{Slot: f.slot, Position: session.Position{Lat: f.lat, Lng: f.lng}})
		}
	}
	return out, nil
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
		WHERE team_id = $1 AND server_ts >= $2 AND (accuracy IS NULL OR accuracy <= $3)
		ORDER BY slot_id, server_ts, id`, teamID, since.UTC().Format("2006-01-02 15:04:05"), maxFixAccuracy)
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

// awaySeconds is how long a phone may stay silent before its player
// counts as away: long enough to pocket it on the walk to a post, short
// enough that a dead battery does not hold the team for long.
const awaySeconds = 180.0

// away are the team's players whose phones have not reported for
// awaySeconds — counted, for a phone that never has, from when its
// player opened their link, so one just joining is not away yet.
func away(ctx context.Context, q Querier, teamID int64, now time.Time) (map[int64]bool, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT s.id, COALESCE((SELECT max(p.server_ts) FROM location_pings p WHERE p.slot_id = s.id), s.last_login_at, '')
		FROM player_slots s WHERE s.team_id = $1`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var slot int64
		var ts string
		if err := rows.Scan(&slot, &ts); err != nil {
			return nil, err
		}
		if at, ok := icscal.ParseTimestamp(ts); !ok || now.Sub(at).Seconds() > awaySeconds {
			out[slot] = true
		}
	}
	return out, rows.Err()
}

// hasRolePosts reports whether g has a two-key launch with a role's post,
// the one thing that asks who is away.
func hasRolePosts(g *graph.Graph) bool {
	for _, n := range g.Nodes {
		if n.PuzzleCfg != nil && n.PuzzleCfg.Answer != nil && len(n.PuzzleCfg.Answer.PostRoles) > 0 {
			return true
		}
	}
	return false
}

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

// locate gives the team its position, each phone's where the locator
// knows them — and so the asking phone's, and its player's roles, for a
// request marked ForPhone — and, where the graph asks, how far it has
// walked on its current node.
func (e *Engine) locate(ctx context.Context, q Querier, s *teamState) error {
	lat, lng, ok, err := e.locator().Position(ctx, q, s.teamID)
	if err != nil {
		return fmt.Errorf("reading position: %w", err)
	}
	if ok {
		s.Position = &session.Position{Lat: lat, Lng: lng}
	}
	me, forPhone := phoneOf(ctx)
	var players []int64
	if forPhone {
		// A locator that does not tell phones apart places every one at
		// the team's position.
		s.Viewer = &session.Viewer{Slot: me, Position: s.Position}
		if s.G != nil && s.G.Languages() != nil {
			if err := q.QueryRowContext(ctx, `SELECT lang FROM player_slots WHERE id = $1 AND team_id = $2`, me, s.teamID).Scan(&s.Viewer.Lang); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("reading the player's language: %w", err)
			}
			if s.Viewer.Lang != "" && !s.Speaks(s.Viewer.Lang) {
				s.Viewer.Lang = ""
			}
		}
		// The players a step done together waits for.
		var err error
		if players, err = Players(ctx, q, s.teamID, me); err != nil {
			return fmt.Errorf("reading players: %w", err)
		}
		s.Players = players
	}
	if pl, ok := e.locator().(PhoneLocator); ok {
		phones, err := pl.Phones(ctx, q, s.teamID, s.Now)
		if err != nil {
			return fmt.Errorf("reading phone positions: %w", err)
		}
		if forPhone {
			s.Viewer.Position = nil
		}
		s.Phones = make([]session.Position, len(phones))
		s.PhoneAt = make(map[int64]session.Position, len(phones))
		for i, p := range phones {
			s.Phones[i] = p.Position
			s.PhoneAt[p.Slot] = p.Position
			if forPhone && p.Slot == me {
				s.Viewer.Position = &s.Phones[i]
			}
		}
	}
	if forPhone && s.G != nil && len(s.G.Roles) > 0 {
		roles := s.G.DealtRoles()
		holdings, err := dealRoles(ctx, q, s.teamID, players, roles)
		if err != nil {
			return fmt.Errorf("dealing roles: %w", err)
		}
		s.Viewer.Roles = RolesOf(holdings, roles, me)
		s.RoleHolders = make(map[string]int64, len(holdings))
		for role, h := range holdings {
			s.RoleHolders[role] = h.Slot
		}
		if hasRolePosts(s.G) {
			if s.Away, err = away(ctx, q, s.teamID, s.Now); err != nil {
				return fmt.Errorf("reading who is away: %w", err)
			}
		}
		// The default role is everyone else's — and a player alone's.
		if d := s.G.DefaultRole(); d != "" && (len(s.Viewer.Roles) == 0 || len(players) <= 1) {
			s.Viewer.Roles = append(s.Viewer.Roles, d)
		}
		// A secret role is its player's alone, dealt once (dealSecrets).
		for _, role := range s.G.SecretRoles() {
			if slot := s.Secrets[role]; slot != 0 && slot == me {
				s.Viewer.Roles = append(s.Viewer.Roles, role)
			}
		}
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
	if err := s.lockCounters(ctx, tx); err != nil {
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
		pausedAt  sql.NullString
		startLat  sql.NullFloat64
		startLng  sql.NullFloat64
		startRad  sql.NullFloat64
		startName string
		anywhere  bool
		station   sql.NullString
	)
	query := `
		SELECT tgs.event_id, tgs.graph_id, tgs.current_node, tgs.node_entered_at, tgs.started_at,
		       tgs.completed_at, tgs.outcome, tgs.score, tgs.penalty_score, tgs.penalty_seconds,
		       tgs.theme, tgs.last_answer, tgs.timed_out, tgs.lang, tgs.cheated,
		       COALESCE(adv.start_code, ''), tgs.start_paused, tgs.node_paused, ev.paused_seconds, ev.paused_at,
		       adv.start_lat, adv.start_lng, adv.start_radius_m, adv.start_label, tm.start_anywhere,
		       tm.station_zone
		FROM team_graph_state tgs
		JOIN teams tm ON tm.id = tgs.team_id
		JOIN adventure_graphs g ON g.id = tgs.graph_id
		JOIN adventures adv ON adv.id = g.adventure_id
		JOIN events ev ON ev.id = tgs.event_id
		WHERE tgs.team_id = $1`
	if forUpdate {
		query += ` FOR UPDATE OF tgs`
	}
	err := q.QueryRowContext(ctx, query, teamID).Scan(&s.eventID, &s.graphID, &current, &entered, &s.StartedAt,
		&completed, &outcome, &s.Points, &s.PenaltyScore, &s.PenaltySeconds, &s.Theme, &s.LatestAnswer, &s.TimedOut, &s.Lang, &s.Cheated,
		&s.StartCode, &s.StartPaused, &s.NodePaused, &s.Paused, &pausedAt,
		&startLat, &startLng, &startRad, &startName, &anywhere, &station)
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
	s.Paused = pauseClock(s.Paused, pausedAt, s.Now)
	s.StartPlace = session.NewStartPlace(nullFloat(startLat), nullFloat(startLng), nullFloat(startRad), startName)
	// An arranger may let the team begin wherever it stands: it still sees
	// the start screen, but its start is open.
	s.StartAnywhere = anywhere
	s.Station = station.String

	if s.G, err = e.graph(ctx, q, s.graphID); err != nil {
		return nil, err
	}
	// Only a dev team's graph changes under it: a step taken out while
	// the team stands on it leaves the team at the start screen, to begin
	// the edited adventure again.
	if s.Current != "" {
		if _, ok := s.G.Node(s.Current); !ok {
			s.Current = ""
		}
	}
	return s, s.loadRest(ctx, q)
}

// pauseClock is how long an event has been paused in all at now: its
// closed pauses and, counted up to now, the open one (see
// activity.ClosePause).
func pauseClock(seconds int, pausedAt sql.NullString, now time.Time) int {
	if at, err := time.ParseInLocation(time.DateTime, pausedAt.String, time.UTC); pausedAt.Valid && err == nil && now.After(at) {
		seconds += int(now.Sub(at).Round(time.Second) / time.Second)
	}
	return seconds
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

	if err := s.loadSecrets(ctx, q); err != nil {
		return err
	}
	if s.G != nil && len(s.G.Kit()) > 0 {
		s.Holders = map[string]int64{}
		if err := scanRows(ctx, q, `SELECT item, slot_id FROM team_item_holders WHERE team_id = $1`, s.teamID,
			func(rows *sql.Rows) error {
				var item string
				var slot int64
				if err := rows.Scan(&item, &slot); err != nil {
					return err
				}
				s.Holders[item] = slot
				return nil
			}); err != nil {
			return err
		}
	}

	// What each player did on the current step, for one done together.
	if s.Current != "" {
		rows, err := q.QueryContext(ctx, `SELECT slot_id, value, pressed_at FROM team_presses WHERE team_id = $1 AND node = $2`,
			s.teamID, s.Current)
		if err != nil {
			return err
		}
		for rows.Next() {
			var slot int64
			var p session.Press
			if err := rows.Scan(&slot, &p.Value, &p.At); err != nil {
				rows.Close()
				return err
			}
			if s.Presses == nil {
				s.Presses = map[int64]session.Press{}
			}
			s.Presses[slot] = p
		}
		if err := closeRows(rows); err != nil {
			return err
		}
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

// lockCounters serialises teams that may change the same event-wide
// counter, then re-reads the counters. Without it, two teams adding 1
// to 5 both compute 6 from the value loadRest read before the lock.
// Taken after the hub locks, in a fixed order, so it cannot deadlock.
func (s *teamState) lockCounters(ctx context.Context, tx *sql.Tx) error {
	seen := map[string]bool{}
	var counters []string
	for _, n := range s.G.Nodes {
		if n.EffectCfg != nil && n.EffectCfg.Kind == graph.SetCounter && !seen[n.EffectCfg.Counter] {
			seen[n.EffectCfg.Counter] = true
			counters = append(counters, n.EffectCfg.Counter)
		}
	}
	if len(counters) == 0 {
		return nil
	}
	sort.Strings(counters)
	for _, c := range counters {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, counterLockKey(s.eventID, c)); err != nil {
			return fmt.Errorf("locking counter %s: %w", c, err)
		}
	}
	return scanPairs(ctx, tx, `SELECT counter, value FROM event_counters WHERE event_id = $1`, s.eventID,
		func(k string, v float64) { s.Counters[k] = v })
}

func counterLockKey(eventID int64, counter string) int64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "zv-counter:%d:%s", eventID, counter)
	return int64(h.Sum64())
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

// nullFloat is a nullable column's value, or nil.
func nullFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}
