// Package simulator drives a fake race inside the server so an operator
// can see the live view react to motion, reorders and answers without
// needing real players on the ground. It writes through the same
// tables real runs use — location_pings, event_activity, teams — so every
// subscriber (live WS, leaderboard, map tracks) picks up the activity
// with no special casing.
//
// One simulation runs at a time. Start creates a throwaway run, Pause
// freezes the tick loop without tearing anything down, Resume thaws it,
// and Reset deletes everything the simulator created.
package simulator

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	mrand "math/rand"
	"sync"
	"sync/atomic"
	"time"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/logx"
	"git.sr.ht/~klahr/zv/internal/activity"
	"git.sr.ht/~klahr/zv/internal/ws"
)

// tickInterval is the simulator's logical tick. At the default speedMul
// each real second advances one simulated second; bumping speedMul just
// scales movement distance per tick — the tick cadence stays constant
// so ping density looks natural on the map.
const tickInterval = 750 * time.Millisecond

// arrivalRadiusM is how close (metres) a team has to get before it's
// considered "at" a puzzle and starts thinking about the answer.
const arrivalRadiusM = 15.0

// dwellBase and dwellJitter control how long a team pauses at a puzzle
// before submitting an answer. Each arrival picks a dwell in
// [dwellBase, dwellBase+dwellJitter).
const (
	dwellBase   = 3 * time.Second
	dwellJitter = 8 * time.Second
)

// teamNames seeds the simulated team roster. Small & whimsical — keep
// the live view legible on a TV. Index is wrapped with a suffix when
// TeamCount > len(teamNames).
var teamNames = []string{
	"Comet", "Nebula", "Orion", "Pulsar", "Quasar",
	"Sirius", "Titan", "Vega", "Atlas", "Bolt",
	"Falcon", "Gryphon", "Hydra", "Ibis", "Jackal",
}

// State is the external snapshot exposed over the admin status
// endpoint. Intentionally flat so the JSON shape is stable.
type State struct {
	Stage         string  `json:"stage"` // "idle" | "running" | "paused"
	EventID         int64   `json:"event_id,omitempty"`
	LiveToken     string  `json:"live_token,omitempty"`
	AdventureSlug string  `json:"adventure_slug,omitempty"`
	TeamCount     int     `json:"team_count,omitempty"`
	SpeedMul      float64 `json:"speed_mul,omitempty"`
	// StartedAt is a string so omitempty can actually drop it when idle —
	// time.Time's zero value still serialises to "0001-01-01…" which the
	// frontend would happily render as "Running for 17 million hours".
	StartedAt string `json:"started_at,omitempty"`
}

// Simulator holds one optional simulated run per organization. Each
// org gets its own slot — arrangers can preview their adventures in
// parallel without stepping on each other — but a given org is still
// limited to a single sim at a time so two staff in the same org can't
// fight over the controls.
type Simulator struct {
	db  *sql.DB
	lib *adventure.Library
	hub *ws.Hub

	mu      sync.Mutex
	current map[int64]*run
}

// run bundles the per-simulation state. Lives only while a simulation
// is Running or Paused; nilled out on Reset.
type run struct {
	id            int64
	liveToken     string
	adventureSlug string
	orgID         int64

	enabled  []adventure.Page
	teams    []*simTeam
	speedMul float64

	paused    atomic.Bool
	cancel    context.CancelFunc
	done      chan struct{}
	startedAt time.Time

	rng *mrand.Rand
}

// simTeam carries the per-team virtual walker. Fields that the tick
// loop mutates (position, dwell) are not shared across goroutines —
// only the driver goroutine touches them.
type simTeam struct {
	id         int64
	slotID     int64
	name       string
	lat, lng   float64
	rawPageIdx int // adventure_pages.idx of the next puzzle to solve

	speedMps  float64 // metres per simulated second
	wrongProb float64 // 0..1 probability of a wrong answer per attempt
	dwellLeft time.Duration
	finished  bool
}

// New wires the simulator to its dependencies. The hub lets the tick
// loop push run-topic updates so live viewers see movement frame by
// frame.
func New(db *sql.DB, lib *adventure.Library, hub *ws.Hub) *Simulator {
	return &Simulator{db: db, lib: lib, hub: hub, current: map[int64]*run{}}
}

// Status returns the simulator state for the given org. Idle when no
// run is active for that org. Safe to call at any time.
func (s *Simulator) Status(orgID int64) State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked(orgID)
}

// Start creates a fresh simulated run for the given org and begins
// driving it. Returns an error if one is already active for this org
// — callers must Reset that org's run first. orgID must be > 0; the
// simulator is org-scoped so each tenant runs independently.
func (s *Simulator) Start(ctx context.Context, orgID int64, adventureSlug string, teamCount int, speedMul float64, createdByUserID int64) (State, error) {
	if orgID <= 0 {
		return State{}, errors.New("orgID required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.current[orgID]; ok {
		return State{}, errors.New("a simulation is already active; reset first")
	}
	if teamCount < 2 {
		teamCount = 2
	}
	if teamCount > 20 {
		teamCount = 20
	}
	if speedMul <= 0 {
		speedMul = 1
	}
	if speedMul > 50 {
		speedMul = 50
	}
	adv, err := s.lib.GetContext(ctx, adventureSlug)
	if err != nil {
		return State{}, fmt.Errorf("load adventure: %w", err)
	}
	if adv == nil {
		return State{}, errors.New("unknown adventure")
	}
	// Keep only enabled, geolocated puzzles — teams have nowhere to
	// walk toward otherwise and the simulator would just sit still.
	enabled := make([]adventure.Page, 0, len(adv.Pages))
	for _, p := range adv.Pages {
		if p.Disabled {
			continue
		}
		if p.Location.Lat == 0 && p.Location.Lng == 0 {
			continue
		}
		enabled = append(enabled, p)
	}
	if len(enabled) == 0 {
		return State{}, errors.New("adventure has no geolocated puzzles")
	}
	liveToken, err := newLiveToken()
	if err != nil {
		return State{}, fmt.Errorf("token: %w", err)
	}
	var eventID int64
	if err := s.db.QueryRowContext(ctx, `
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, started_at, live_token, name, show_map)
		VALUES ($1, $2, $3, 'running', now_text(), $4, $5, 1)
		RETURNING id
	`, adventureSlug, orgID, nullIfZero(createdByUserID), liveToken, "Simulated race").Scan(&eventID); err != nil {
		return State{}, fmt.Errorf("create run: %w", err)
	}

	// Scatter team start positions around the first puzzle. ~300 m
	// spread is small enough to keep the TV map framed, large enough
	// that motion lines are visible.
	rng := mrand.New(mrand.NewSource(time.Now().UnixNano()))
	startPuzzle := enabled[0]
	teams := make([]*simTeam, 0, teamCount)
	for i := 0; i < teamCount; i++ {
		name := namedTeam(i)
		// Generate the team token in Go to satisfy the teams.token UNIQUE
		// index — keeps the simulator independent of any DB-side helpers.
		teamTok, err := newLiveToken()
		if err != nil {
			_, _ = s.db.ExecContext(ctx, `DELETE FROM events WHERE id = $1`, eventID)
			return State{}, fmt.Errorf("team token: %w", err)
		}
		var tid int64
		if err := s.db.QueryRowContext(ctx,
			`INSERT INTO teams(event_id, name, token) VALUES ($1, $2, $3) RETURNING id`,
			eventID, name, teamTok).Scan(&tid); err != nil {
			// Roll back on best-effort — nothing hands out partial state.
			_, _ = s.db.ExecContext(ctx, `DELETE FROM events WHERE id = $1`, eventID)
			return State{}, fmt.Errorf("create team %q: %w", name, err)
		}
		// location_pings requires slot_id NOT NULL, so give each team a
		// single placeholder slot. The token is never used by a real
		// client, but must be unique per the schema.
		slotTok, _ := newLiveToken()
		var sid int64
		if err := s.db.QueryRowContext(ctx,
			`INSERT INTO player_slots(team_id, name, token) VALUES ($1, $2, $3) RETURNING id`,
			tid, "simrunner", slotTok).Scan(&sid); err != nil {
			_, _ = s.db.ExecContext(ctx, `DELETE FROM events WHERE id = $1`, eventID)
			return State{}, fmt.Errorf("create slot: %w", err)
		}
		lat, lng := jitter(startPuzzle.Location.Lat, startPuzzle.Location.Lng, 300, rng)
		teams = append(teams, &simTeam{
			id:         tid,
			slotID:     sid,
			name:       name,
			lat:        lat,
			lng:        lng,
			rawPageIdx: enabled[0].Index,
			// Spread speeds so teams overtake each other — otherwise
			// the rank list is static and the FLIP animation never
			// fires. Range is ~0.8 – 1.6 m/s base (walking pace).
			speedMps:  0.8 + rng.Float64()*0.8,
			wrongProb: 0.15 + rng.Float64()*0.15, // 15–30%
		})
		activity.Record(ctx, s.db, eventID, &tid, activity.Actor{}, activity.TeamCreated,
			map[string]any{"source": "simulator", "name": name})
	}

	ctxRun, cancel := context.WithCancel(context.Background())
	r := &run{
		id:            eventID,
		liveToken:     liveToken,
		adventureSlug: adventureSlug,
		orgID:         orgID,
		enabled:       enabled,
		teams:         teams,
		speedMul:      speedMul,
		cancel:        cancel,
		done:          make(chan struct{}),
		startedAt:     time.Now().UTC(),
		rng:           rng,
	}
	s.current[orgID] = r
	go s.loop(ctxRun, r)
	// Kick the live topic so anyone already watching picks up the
	// freshly-created run via a snapshot rebuild.
	s.hub.Publish(ws.EventTopic(eventID), "changed", nil)
	logx.Info("simulator.started",
		"event_id", eventID, "org_id", orgID, "adventure", adventureSlug,
		"teams", teamCount, "speed_mul", speedMul)
	return s.statusLocked(orgID), nil
}

// Pause freezes the tick loop for the given org but keeps all DB
// state. Safe to call when already paused.
func (s *Simulator) Pause(orgID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.current[orgID]
	if r == nil {
		return errors.New("no active simulation")
	}
	r.paused.Store(true)
	logx.Info("simulator.paused", "event_id", r.id, "org_id", orgID)
	return nil
}

// Resume thaws a paused simulation for the given org.
func (s *Simulator) Resume(orgID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.current[orgID]
	if r == nil {
		return errors.New("no active simulation")
	}
	r.paused.Store(false)
	logx.Info("simulator.resumed", "event_id", r.id, "org_id", orgID)
	return nil
}

// Reset stops the tick loop for the given org and deletes every DB
// row the simulator created for it. Safe to call when idle (no-op).
func (s *Simulator) Reset(ctx context.Context, orgID int64) error {
	s.mu.Lock()
	r := s.current[orgID]
	delete(s.current, orgID)
	s.mu.Unlock()
	if r == nil {
		return nil
	}
	r.cancel()
	// Wait for the tick loop to observe the cancel and exit so we
	// don't race with it on the DELETE below.
	select {
	case <-r.done:
	case <-time.After(2 * time.Second):
		// Fall through — the goroutine sees ctx.Done() on the next
		// iteration at worst; the cascade delete will take its pings.
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE id = $1`, r.id); err != nil {
		return fmt.Errorf("delete run: %w", err)
	}
	logx.Info("simulator.reset", "event_id", r.id, "org_id", orgID)
	return nil
}

// statusLocked is Status's implementation assuming the mutex is held.
func (s *Simulator) statusLocked(orgID int64) State {
	r := s.current[orgID]
	if r == nil {
		return State{Stage: "idle"}
	}
	stage := "running"
	if r.paused.Load() {
		stage = "paused"
	}
	return State{
		Stage:         stage,
		EventID:       r.id,
		LiveToken:     r.liveToken,
		AdventureSlug: r.adventureSlug,
		TeamCount:     len(r.teams),
		SpeedMul:      r.speedMul,
		StartedAt:     r.startedAt.Format(time.RFC3339),
	}
}

// loop is the driver goroutine. One per active simulation.
func (s *Simulator) loop(ctx context.Context, r *run) {
	defer close(r.done)
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if r.paused.Load() {
				continue
			}
			s.tick(ctx, r)
		}
	}
}

// tick advances every team by one simulated step. Effective dt scales
// with speedMul so operators can fast-forward the race.
func (s *Simulator) tick(ctx context.Context, r *run) {
	dtSec := tickInterval.Seconds() * r.speedMul
	allDone := true
	for _, team := range r.teams {
		if team.finished {
			continue
		}
		allDone = false
		target := r.nextPuzzle(team)
		if target == nil {
			// No more puzzles — mark finished.
			s.finishTeam(ctx, r, team)
			continue
		}
		// Phase 1: dwelling at a puzzle → count down, then answer.
		if team.dwellLeft > 0 {
			team.dwellLeft -= time.Duration(dtSec * float64(time.Second))
			if team.dwellLeft > 0 {
				s.recordPing(ctx, r, team)
				continue
			}
			s.attemptAnswer(ctx, r, team, target)
			s.recordPing(ctx, r, team)
			continue
		}
		// Phase 2: walking. Step along the great-circle bearing.
		distM := haversineMeters(team.lat, team.lng, target.Location.Lat, target.Location.Lng)
		if distM <= arrivalRadiusM {
			// Arrived: snap to the puzzle and start dwelling.
			team.lat, team.lng = target.Location.Lat, target.Location.Lng
			team.dwellLeft = dwellBase + time.Duration(r.rng.Int63n(int64(dwellJitter)))
			activity.Record(ctx, s.db, r.id, &team.id, activity.Actor{}, activity.GateReached,
				map[string]any{"source": "simulator"})
			s.recordPing(ctx, r, team)
			continue
		}
		stepM := team.speedMps * dtSec
		if stepM >= distM {
			team.lat, team.lng = target.Location.Lat, target.Location.Lng
		} else {
			team.lat, team.lng = stepToward(team.lat, team.lng,
				target.Location.Lat, target.Location.Lng, stepM)
		}
		s.recordPing(ctx, r, team)
	}
	// One publish per tick keeps the fanout cost predictable regardless
	// of team count. Live view receivers re-pull the snapshot once.
	s.hub.Publish(ws.EventTopic(r.id), "changed", nil)

	if allDone {
		// Every team has finished — pause the loop so we don't churn
		// with nothing to do. The operator can Reset to start over.
		r.paused.Store(true)
		logx.Info("simulator.complete", "event_id", r.id)
	}
}

// recordPing inserts a location_ping and is the signal the map WS feed
// rebuilds from. Errors are swallowed — telemetry must not take the
// run down.
func (s *Simulator) recordPing(ctx context.Context, r *run, t *simTeam) {
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO location_pings(slot_id, team_id, event_id, lat, lng, accuracy, speed)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, t.slotID, t.id, r.id, t.lat, t.lng, 8.0, t.speedMps)
}

// attemptAnswer rolls a d100 for wrongness. Wrong → re-dwell, right →
// advance cursor (and maybe complete the team). Keeps the teams DB
// table in sync so the live leaderboard moves.
func (s *Simulator) attemptAnswer(ctx context.Context, r *run, t *simTeam, puzzle *adventure.Page) {
	wrong := r.rng.Float64() < t.wrongProb
	if wrong {
		t.dwellLeft = dwellBase + time.Duration(r.rng.Int63n(int64(dwellJitter)))
		activity.Record(ctx, s.db, r.id, &t.id, activity.Actor{}, activity.AnswerWrong,
			map[string]any{"source": "simulator", "puzzle_idx": puzzle.Index})
		return
	}
	// Correct — advance cursor to the puzzle after this one in the
	// enabled list. The raw idx (not the enabled position) is what the
	// schema stores.
	next := r.nextPuzzleAfter(puzzle.Index)
	newCursor := puzzle.Index + 1
	if next != nil {
		// Walk forward past any disabled pages by setting cursor to
		// the *next* enabled raw idx. Otherwise nextPuzzle would keep
		// handing back the same target.
		newCursor = next.Index
	} else {
		newCursor = puzzle.Index + 1 // past the end; finishTeam handles it
	}
	t.rawPageIdx = newCursor
	_, _ = s.db.ExecContext(ctx,
		`UPDATE teams SET current_page_index = $1 WHERE id = $2`,
		newCursor, t.id)
	activity.Record(ctx, s.db, r.id, &t.id, activity.Actor{}, activity.AnswerRight,
		map[string]any{"source": "simulator", "puzzle_idx": puzzle.Index})
	if next == nil {
		s.finishTeam(ctx, r, t)
	}
}

// finishTeam stamps completed_at, emits the usual event, and flags the
// team so subsequent ticks skip it. When this finish leaves zero
// teams still racing, the event itself transitions to 'completed' too,
// matching the real player flow.
func (s *Simulator) finishTeam(ctx context.Context, r *run, t *simTeam) {
	if t.finished {
		return
	}
	t.finished = true
	_, _ = s.db.ExecContext(ctx,
		`UPDATE teams SET completed_at = now_text() WHERE id = $1`, t.id)
	activity.Record(ctx, s.db, r.id, &t.id, activity.Actor{}, activity.TeamCompleted,
		map[string]any{"source": "simulator"})
	_, _ = activity.MaybeCompleteEvent(ctx, s.db, r.id)
}

// nextPuzzle returns the team's current target — the first enabled
// puzzle whose raw index is >= the team's cursor. Returns nil once the
// team has walked off the end.
func (r *run) nextPuzzle(t *simTeam) *adventure.Page {
	for i := range r.enabled {
		if r.enabled[i].Index >= t.rawPageIdx {
			return &r.enabled[i]
		}
	}
	return nil
}

// nextPuzzleAfter returns the enabled puzzle strictly after the given
// raw adventure_pages.idx, or nil if none.
func (r *run) nextPuzzleAfter(rawIdx int) *adventure.Page {
	for i := range r.enabled {
		if r.enabled[i].Index > rawIdx {
			return &r.enabled[i]
		}
	}
	return nil
}

// ---- geometry helpers ----

// stepToward returns (lat, lng) after moving `distM` metres from the
// origin along the great-circle bearing to the target. Accurate to
// better than a metre over walkable distances.
func stepToward(lat1, lng1, lat2, lng2, distM float64) (float64, float64) {
	const earthR = 6371000.0
	rad := math.Pi / 180
	brg := bearingRad(lat1, lng1, lat2, lng2)
	ang := distM / earthR
	lat1R, lng1R := lat1*rad, lng1*rad
	lat2R := math.Asin(math.Sin(lat1R)*math.Cos(ang) + math.Cos(lat1R)*math.Sin(ang)*math.Cos(brg))
	lng2R := lng1R + math.Atan2(math.Sin(brg)*math.Sin(ang)*math.Cos(lat1R),
		math.Cos(ang)-math.Sin(lat1R)*math.Sin(lat2R))
	return lat2R / rad, lng2R / rad
}

func bearingRad(lat1, lng1, lat2, lng2 float64) float64 {
	rad := math.Pi / 180
	φ1, φ2 := lat1*rad, lat2*rad
	λ1, λ2 := lng1*rad, lng2*rad
	y := math.Sin(λ2-λ1) * math.Cos(φ2)
	x := math.Cos(φ1)*math.Sin(φ2) - math.Sin(φ1)*math.Cos(φ2)*math.Cos(λ2-λ1)
	return math.Atan2(y, x)
}

// haversineMeters is kept local to the package so the simulator doesn't
// take a dep on public/handlers where an identical helper lives.
func haversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371000.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*
			math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * R * math.Asin(math.Min(1, math.Sqrt(a)))
}

// jitter offsets a lat/lng by up to ~maxM metres in a uniformly-random
// direction. Used for scattering team start positions around the first
// puzzle.
func jitter(lat, lng, maxM float64, rng *mrand.Rand) (float64, float64) {
	dist := rng.Float64() * maxM
	brg := rng.Float64() * 2 * math.Pi
	const earthR = 6371000.0
	rad := math.Pi / 180
	ang := dist / earthR
	lat1R, lng1R := lat*rad, lng*rad
	lat2R := math.Asin(math.Sin(lat1R)*math.Cos(ang) + math.Cos(lat1R)*math.Sin(ang)*math.Cos(brg))
	lng2R := lng1R + math.Atan2(math.Sin(brg)*math.Sin(ang)*math.Cos(lat1R),
		math.Cos(ang)-math.Sin(lat1R)*math.Sin(lat2R))
	return lat2R / rad, lng2R / rad
}

// ---- misc small helpers ----

func namedTeam(i int) string {
	if i < len(teamNames) {
		return teamNames[i]
	}
	return fmt.Sprintf("Team %d", i+1)
}

func nullIfZero(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func newLiveToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
