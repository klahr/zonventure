// Package simulator drives a fake race inside the server so an operator
// can see the live view react to motion, reorders and answers without
// needing real players on the ground. It writes through the same
// tables real runs use — location_pings, run_events, teams — so every
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
	"git.sr.ht/~klahr/zv/internal/runevents"
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
	RunID         int64   `json:"run_id,omitempty"`
	LiveToken     string  `json:"live_token,omitempty"`
	AdventureSlug string  `json:"adventure_slug,omitempty"`
	TeamCount     int     `json:"team_count,omitempty"`
	SpeedMul      float64 `json:"speed_mul,omitempty"`
	// StartedAt is a string so omitempty can actually drop it when idle —
	// time.Time's zero value still serialises to "0001-01-01…" which the
	// frontend would happily render as "Running for 17 million hours".
	StartedAt string `json:"started_at,omitempty"`
}

// Simulator is the global singleton — one run at a time. Callers hold
// a single instance (injected into admin.Handlers).
type Simulator struct {
	db  *sql.DB
	lib *adventure.Library
	hub *ws.Hub

	mu      sync.Mutex
	current *run
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
	return &Simulator{db: db, lib: lib, hub: hub}
}

// Status is safe to call at any time — returns "idle" when nothing is
// running, otherwise a snapshot of the active simulation.
func (s *Simulator) Status() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return State{Stage: "idle"}
	}
	stage := "running"
	if s.current.paused.Load() {
		stage = "paused"
	}
	return State{
		Stage:         stage,
		RunID:         s.current.id,
		LiveToken:     s.current.liveToken,
		AdventureSlug: s.current.adventureSlug,
		TeamCount:     len(s.current.teams),
		SpeedMul:      s.current.speedMul,
		StartedAt:     s.current.startedAt.Format(time.RFC3339),
	}
}

// Start creates a fresh simulated run and begins driving it. Returns an
// error if one is already active — callers must Reset first.
func (s *Simulator) Start(ctx context.Context, adventureSlug string, teamCount int, speedMul float64, createdByUserID int64) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil {
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
	// Need an org to anchor the run — picking the first one keeps the
	// sim a one-click operation. Operators running this multi-tenant
	// in prod would wire it to their own org; this is a dev tool.
	var orgID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM organizations ORDER BY id ASC LIMIT 1`).Scan(&orgID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return State{}, errors.New("no organizations exist; create one before simulating")
		}
		return State{}, fmt.Errorf("lookup org: %w", err)
	}

	liveToken, err := newLiveToken()
	if err != nil {
		return State{}, fmt.Errorf("token: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO adventure_runs(adventure_slug, org_id, created_by_user_id, state, started_at, live_token, name, show_map)
		VALUES (?, ?, ?, 'running', datetime('now'), ?, ?, 1)
	`, adventureSlug, orgID, nullIfZero(createdByUserID), liveToken, "Simulated race")
	if err != nil {
		return State{}, fmt.Errorf("create run: %w", err)
	}
	runID, _ := res.LastInsertId()

	// Scatter team start positions around the first puzzle. ~300 m
	// spread is small enough to keep the TV map framed, large enough
	// that motion lines are visible.
	rng := mrand.New(mrand.NewSource(time.Now().UnixNano()))
	startPuzzle := enabled[0]
	teams := make([]*simTeam, 0, teamCount)
	for i := 0; i < teamCount; i++ {
		name := namedTeam(i)
		// Generate a token inline so each simulated team satisfies the
		// teams.token UNIQUE index. Using SQLite's randomblob keeps the
		// simulator free of extra crypto plumbing and matches the style
		// of the migration's own backfill.
		tres, err := s.db.ExecContext(ctx,
			`INSERT INTO teams(run_id, name, token) VALUES (?, ?, lower(hex(randomblob(18))))`,
			runID, name)
		if err != nil {
			// Roll back on best-effort — nothing hands out partial state.
			_, _ = s.db.ExecContext(ctx, `DELETE FROM adventure_runs WHERE id = ?`, runID)
			return State{}, fmt.Errorf("create team %q: %w", name, err)
		}
		tid, _ := tres.LastInsertId()
		// location_pings requires slot_id NOT NULL, so give each team a
		// single placeholder slot. The token is never used by a real
		// client, but must be unique per the schema.
		slotTok, _ := newLiveToken()
		sres, err := s.db.ExecContext(ctx,
			`INSERT INTO player_slots(team_id, name, token) VALUES (?, ?, ?)`,
			tid, "simrunner", slotTok)
		if err != nil {
			_, _ = s.db.ExecContext(ctx, `DELETE FROM adventure_runs WHERE id = ?`, runID)
			return State{}, fmt.Errorf("create slot: %w", err)
		}
		sid, _ := sres.LastInsertId()
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
		runevents.Record(ctx, s.db, runID, &tid, runevents.Actor{}, runevents.TeamCreated,
			map[string]any{"source": "simulator", "name": name})
	}

	ctxRun, cancel := context.WithCancel(context.Background())
	r := &run{
		id:            runID,
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
	s.current = r
	go s.loop(ctxRun, r)
	// Kick the live topic so anyone already watching picks up the
	// freshly-created run via a snapshot rebuild.
	s.hub.Publish(ws.RunTopic(runID), "changed", nil)
	logx.Info("simulator.started",
		"run_id", runID, "adventure", adventureSlug,
		"teams", teamCount, "speed_mul", speedMul)
	return s.statusLocked(), nil
}

// Pause freezes the tick loop but keeps all DB state. Safe to call
// when already paused.
func (s *Simulator) Pause() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return errors.New("no active simulation")
	}
	s.current.paused.Store(true)
	logx.Info("simulator.paused", "run_id", s.current.id)
	return nil
}

// Resume thaws a paused simulation.
func (s *Simulator) Resume() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return errors.New("no active simulation")
	}
	s.current.paused.Store(false)
	logx.Info("simulator.resumed", "run_id", s.current.id)
	return nil
}

// Reset stops the tick loop and deletes every DB row the simulator
// created. Safe to call when idle (no-op).
func (s *Simulator) Reset(ctx context.Context) error {
	s.mu.Lock()
	r := s.current
	s.current = nil
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
	if _, err := s.db.ExecContext(ctx, `DELETE FROM adventure_runs WHERE id = ?`, r.id); err != nil {
		return fmt.Errorf("delete run: %w", err)
	}
	logx.Info("simulator.reset", "run_id", r.id)
	return nil
}

// statusLocked is Status's implementation assuming the mutex is held.
func (s *Simulator) statusLocked() State {
	if s.current == nil {
		return State{Stage: "idle"}
	}
	stage := "running"
	if s.current.paused.Load() {
		stage = "paused"
	}
	return State{
		Stage:         stage,
		RunID:         s.current.id,
		LiveToken:     s.current.liveToken,
		AdventureSlug: s.current.adventureSlug,
		TeamCount:     len(s.current.teams),
		SpeedMul:      s.current.speedMul,
		StartedAt:     s.current.startedAt.Format(time.RFC3339),
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
			runevents.Record(ctx, s.db, r.id, &team.id, runevents.Actor{}, runevents.GateReached,
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
	s.hub.Publish(ws.RunTopic(r.id), "changed", nil)

	if allDone {
		// Every team has finished — pause the loop so we don't churn
		// with nothing to do. The operator can Reset to start over.
		r.paused.Store(true)
		logx.Info("simulator.complete", "run_id", r.id)
	}
}

// recordPing inserts a location_ping and is the signal the map WS feed
// rebuilds from. Errors are swallowed — telemetry must not take the
// run down.
func (s *Simulator) recordPing(ctx context.Context, r *run, t *simTeam) {
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO location_pings(slot_id, team_id, run_id, lat, lng, accuracy, speed)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, t.slotID, t.id, r.id, t.lat, t.lng, 8.0, t.speedMps)
}

// attemptAnswer rolls a d100 for wrongness. Wrong → re-dwell, right →
// advance cursor (and maybe complete the team). Keeps the teams DB
// table in sync so the live leaderboard moves.
func (s *Simulator) attemptAnswer(ctx context.Context, r *run, t *simTeam, puzzle *adventure.Page) {
	wrong := r.rng.Float64() < t.wrongProb
	if wrong {
		t.dwellLeft = dwellBase + time.Duration(r.rng.Int63n(int64(dwellJitter)))
		runevents.Record(ctx, s.db, r.id, &t.id, runevents.Actor{}, runevents.AnswerWrong,
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
		`UPDATE teams SET current_page_index = ? WHERE id = ?`,
		newCursor, t.id)
	runevents.Record(ctx, s.db, r.id, &t.id, runevents.Actor{}, runevents.AnswerRight,
		map[string]any{"source": "simulator", "puzzle_idx": puzzle.Index})
	if next == nil {
		s.finishTeam(ctx, r, t)
	}
}

// finishTeam stamps completed_at, emits the usual event, and flags the
// team so subsequent ticks skip it.
func (s *Simulator) finishTeam(ctx context.Context, r *run, t *simTeam) {
	if t.finished {
		return
	}
	t.finished = true
	_, _ = s.db.ExecContext(ctx,
		`UPDATE teams SET completed_at = datetime('now') WHERE id = ?`, t.id)
	runevents.Record(ctx, s.db, r.id, &t.id, runevents.Actor{}, runevents.TeamCompleted,
		map[string]any{"source": "simulator"})
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
