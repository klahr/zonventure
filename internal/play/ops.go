package play

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	effectq "github.com/klahr/zonventure/internal/effects"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graph/valid"
	"github.com/klahr/zonventure/internal/graph/walk"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/results"
)

// StartEvent pins the adventure's published graph to the event and places
// every team on its first node.
//
// Idempotent: the pin is written only if the event has none, and a team
// that already has state is left alone. That makes a retry after a
// partial failure safe, and it means a team added after the start can be
// placed by calling this again.
//
// The graph is validated before it is pinned. Save already refuses an
// invalid graph, but an event must never start on one — this is the
// last point at which a problem can be stopped before players see it.
func (e *Engine) StartEvent(ctx context.Context, eventID int64) error {
	tx, err := e.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		slug      string
		pinned    sql.NullInt64
		graphID   int64
		paused    int
		pausedAt  sql.NullString
		party     bool
		partyMode string
		base      sql.NullInt64
	)
	if err := tx.QueryRowContext(ctx,
		`SELECT adventure_slug, graph_id, paused_seconds, paused_at, party, party_mode, base_graph_id FROM events WHERE id = $1 FOR UPDATE`, eventID,
	).Scan(&slug, &pinned, &paused, &pausedAt, &party, &partyMode, &base); err != nil {
		return fmt.Errorf("loading event %d: %w", eventID, err)
	}

	if pinned.Valid {
		graphID = pinned.Int64
	} else {
		var adventureID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM adventures WHERE slug = $1`, slug).Scan(&adventureID); err != nil {
			return fmt.Errorf("loading adventure %q: %w", slug, err)
		}
		stored, err := baseGraph(ctx, tx, adventureID, base)
		if errors.Is(err, graphstore.ErrNotFound) {
			return ErrNoGraph
		}
		if err != nil {
			return err
		}
		if r := valid.Check(stored.Graph); r.HasErrors() {
			return &graphstore.InvalidError{Report: r}
		}
		graphID = stored.ID
		if party {
			// A party plays its own copy, adapted to where it is held.
			if graphID, err = pinParty(ctx, tx, adventureID, eventID, partyMode, stored.Graph); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE events SET graph_id = $1 WHERE id = $2`, graphID, eventID); err != nil {
			return err
		}
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT t.id, t.lang FROM teams t
		LEFT JOIN team_graph_state s ON s.team_id = t.id
		WHERE t.event_id = $1 AND s.team_id IS NULL AND t.withdrawn_at IS NULL
		ORDER BY t.id
	`, eventID)
	if err != nil {
		return err
	}
	type placing struct {
		id   int64
		lang string
	}
	var teams []placing
	for rows.Next() {
		var p placing
		if err := rows.Scan(&p.id, &p.lang); err != nil {
			rows.Close()
			return err
		}
		teams = append(teams, p)
	}
	if err := closeRows(rows); err != nil {
		return err
	}

	now := e.now()
	// A team placed after the event has been paused counts from the
	// clock as it reads now, not the pauses before it joined.
	clock := pauseClock(paused, pausedAt, now)
	for _, p := range teams {
		teamID := p.id
		g, err := e.graph(ctx, tx, graphID)
		if err != nil {
			return err
		}
		// The team starts in the language it was given, if the adventure
		// is translated into it; the main language is stored as "".
		lang := p.lang
		if lang == g.Language || !slices.Contains(g.Languages(), lang) {
			lang = ""
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO team_graph_state (team_id, event_id, graph_id, started_at, node_entered_at, updated_at, start_paused, node_paused, lang)
			VALUES ($1, $2, $3, $4, $4, $4, $5, $5, $6)
		`, teamID, eventID, graphID, now, clock, lang); err != nil {
			return fmt.Errorf("placing team %d: %w", teamID, err)
		}
		// The roles' kits, in the backpack before the first step, which
		// may already ask what the team carries.
		for item, q := range g.Kit() {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO team_items (team_id, item, qty) VALUES ($1, $2, $3)
				ON CONFLICT (team_id, item) DO UPDATE SET qty = team_items.qty + EXCLUDED.qty
			`, teamID, item, q); err != nil {
				return fmt.Errorf("packing team %d's kit: %w", teamID, err)
			}
		}
		s, err := e.loadTeam(ctx, tx, teamID)
		if err != nil {
			return err
		}
		// A team that chooses its start — or must enter a start code, or
		// stand at the starting point — waits at the start screen until it
		// does, so the clock only starts when it actually begins. Everyone
		// else is placed at the first step now.
		if s.HoldsAtStart() {
			continue
		}
		step, err := walk.First(s.G, s)
		if err != nil {
			return fmt.Errorf("placing team %d: %w", teamID, err)
		}
		if err := e.commit(ctx, tx, s, step); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Answer submits a guess at the puzzle the team is on. See
// session.Team.Answer for the rules; this makes the outcome durable.
func (e *Engine) Answer(ctx context.Context, teamID int64, node, guess string) (Result, error) {
	return e.act(ctx, teamID, action{"answer", node, guess}, func(t *session.Team) (Result, *walk.Step, error) { return t.Answer(node, guess) })
}

// ChooseStart begins the team's adventure at one of its starts. code is
// the start-code the player typed (empty when the adventure has no gate);
// session.ChooseStart validates it before the clock starts.
func (e *Engine) ChooseStart(ctx context.Context, teamID int64, start, code string) (Result, error) {
	return e.act(ctx, teamID, action{"choose_start", start, ""}, func(t *session.Team) (Result, *walk.Step, error) { return t.ChooseStart(start, code) })
}

// Continue moves the team past a story node.
func (e *Engine) Continue(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.act(ctx, teamID, action{"continue", node, ""}, func(t *session.Team) (Result, *walk.Step, error) { return t.Continue(node) })
}

// Walk is a phone's choice of which of its groups it walks, by the
// group's key (session.GroupView.Port). See session.Team.Walk.
func (e *Engine) Walk(ctx context.Context, teamID int64, group string) (Result, error) {
	return e.act(ctx, teamID, action{"walk", "", group}, func(t *session.Team) (Result, *walk.Step, error) { return t.Walk(group) })
}

// SetLanguage switches the language the team plays in, or, asked by a
// phone, the language its own player reads: teammates keep theirs.
func (e *Engine) SetLanguage(ctx context.Context, teamID int64, lang string) (Result, error) {
	me, forPhone := phoneOf(ctx)
	if !forPhone {
		return e.act(ctx, teamID, action{"language", "", lang}, func(t *session.Team) (Result, *walk.Step, error) { return Result{}, nil, t.SetLanguage(lang) })
	}
	var graphID int64
	if err := e.DB.QueryRowContext(ctx, `SELECT graph_id FROM team_graph_state WHERE team_id = $1`, teamID).Scan(&graphID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Result{}, ErrNoGraph
		}
		return Result{}, err
	}
	g, err := e.graph(ctx, e.DB, graphID)
	if err != nil {
		return Result{}, err
	}
	if lang != g.Language && !slices.Contains(g.Languages(), lang) {
		return Result{}, ErrNotAllowed
	}
	if _, err := e.DB.ExecContext(ctx, `UPDATE player_slots SET lang = $1 WHERE id = $2 AND team_id = $3`, lang, me, teamID); err != nil {
		return Result{}, err
	}
	v, err := e.Snapshot(ctx, teamID)
	return Result{View: v}, err
}

// TimeUp applies the adventure's time limit once it has passed. The
// client calls it when its countdown ends; the engine re-checks.
func (e *Engine) TimeUp(ctx context.Context, teamID int64) (Result, error) {
	return e.act(ctx, teamID, action{"time_up", "", ""}, func(t *session.Team) (Result, *walk.Step, error) { return t.TimeUp() })
}

// Back returns the team from inside a home screen's branch to that home
// screen. See session.Team.Back.
func (e *Engine) Back(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.act(ctx, teamID, action{"back", node, ""}, func(t *session.Team) (Result, *walk.Step, error) { return t.Back(node) })
}

// Choose enters a hub branch, or moves on from a satisfied hub when port
// is graph.PortDone.
func (e *Engine) Choose(ctx context.Context, teamID int64, hub, port string) (Result, error) {
	return e.act(ctx, teamID, action{"choose", hub, port}, func(t *session.Team) (Result, *walk.Step, error) { return t.Choose(hub, port) })
}

// Skip takes a puzzle's skip output once both thresholds are met.
func (e *Engine) Skip(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.act(ctx, teamID, action{"skip", node, ""}, func(t *session.Team) (Result, *walk.Step, error) { return t.Skip(node) })
}

// Cheat solves a puzzle for a tester. See session.Team.Cheat; the
// caller has checked the adventure's cheat password.
func (e *Engine) Cheat(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.act(ctx, teamID, action{"cheat", node, ""}, func(t *session.Team) (Result, *walk.Step, error) { return t.Cheat(node) })
}

// Leave takes an item puzzle's route for a team without the items. See
// session.Team.Leave.
func (e *Engine) Leave(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.act(ctx, teamID, action{"leave", node, ""}, func(t *session.Team) (Result, *walk.Step, error) { return t.Leave(node) })
}

// Timeout takes a puzzle's timeout output once its limit has passed.
// The client calls it when its countdown reaches zero; the engine
// re-checks the time rather than trusting the client.
func (e *Engine) Timeout(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.act(ctx, teamID, action{"timeout", node, ""}, func(t *session.Team) (Result, *walk.Step, error) { return t.Timeout(node) })
}

// Pick answers a quiz question or the tie-breaker on the team's step.
func (e *Engine) Pick(ctx context.Context, teamID int64, node, block, choice string) (Result, error) {
	return e.act(ctx, teamID, action{"pick", node, block + "=" + choice}, func(t *session.Team) (Result, *walk.Step, error) { return t.Pick(node, block, choice) })
}

// savePicks writes the team's quiz answers. Session.Pick refuses a change
// once the step is left, so an answer written here is always one the
// team may still hold.
func savePicks(ctx context.Context, tx *sql.Tx, s *teamState) error {
	for key, choice := range s.Picks {
		node, block, _ := strings.Cut(key, "\x00")
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO team_block_picks (team_id, node, block_id, choice, picked_at) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (team_id, node, block_id) DO UPDATE SET choice = EXCLUDED.choice, picked_at = EXCLUDED.picked_at
			WHERE team_block_picks.choice <> EXCLUDED.choice
		`, s.teamID, node, block, choice, s.Now); err != nil {
			return err
		}
	}
	return nil
}

// RevealHint reveals the next hint on the current puzzle.
func (e *Engine) RevealHint(ctx context.Context, teamID int64, node string, index int) (Result, error) {
	return e.act(ctx, teamID, action{"hint", node, strconv.Itoa(index)}, func(t *session.Team) (Result, *walk.Step, error) { return t.RevealHint(node, index) })
}

// Snapshot returns the team's current view without changing anything.
func (e *Engine) Snapshot(ctx context.Context, teamID int64) (View, error) {
	tx, err := e.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return View{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// A read-only transaction cannot take FOR UPDATE, so load without
	// it. Occupancy is irrelevant to a read; position is not — the view
	// says whether the team is inside its zone.
	s, err := e.loadCore(ctx, tx, teamID, false)
	if err != nil {
		return View{}, err
	}
	if err := e.locate(ctx, tx, s); err != nil {
		return View{}, err
	}
	if _, err := e.divide(ctx, tx, s); err != nil {
		return View{}, err
	}
	v := s.View()
	if err := e.decorate(ctx, tx, s.G, s.eventID, teamID, &v); err != nil {
		return View{}, err
	}
	return v, nil
}

// --- the shared action skeleton ---------------------------------------

// action names a player action for the event's result record
// (internal/results): what was done, on which step, with what input.
type action struct {
	name, node, input string
}

// act runs one player action: lock and load the team, run the action's
// rules, persist whatever changed, record it in the event's result
// chain, and commit — or roll everything back.
func (e *Engine) act(ctx context.Context, teamID int64, a action, fn func(*session.Team) (Result, *walk.Step, error)) (Result, error) {
	tx, err := e.DB.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback() }()

	s, err := e.loadTeam(ctx, tx, teamID)
	if err != nil {
		return Result{}, err
	}
	// The saboteur is chosen as the team first moves, among those playing.
	if dealt, err := e.dealSecrets(ctx, tx, s); err != nil {
		return Result{}, err
	} else if dealt {
		// The phone asking may be the saboteur now.
		if err := e.locate(ctx, tx, s); err != nil {
			return Result{}, err
		}
	}

	// A split team's players are dealt into its groups as they come.
	dealt, err := e.divide(ctx, tx, s)
	if err != nil {
		return Result{}, err
	}

	score := s.Score()
	res, step, err := s.Act(func() (Result, *walk.Step, error) { return fn(s.Team) })
	if err != nil {
		// Some refusals (a lockout) still carry information worth
		// returning, but nothing is persisted.
		res.View = s.View()
		if derr := e.decorate(ctx, tx, s.G, s.eventID, teamID, &res.View); derr != nil {
			return Result{}, derr
		}
		return res, err
	}
	// A player's part in a step done together, kept until the team
	// leaves it — which commit then clears.
	if err := savePress(ctx, tx, s); err != nil {
		return Result{}, err
	}
	if step != nil {
		err = e.commit(ctx, tx, s, *step)
	} else {
		err = e.stay(ctx, tx, s)
	}
	if err == nil && step == nil && (dealt || s.GroupsChanged || s.afresh) {
		err = saveSplit(ctx, tx, s)
	}
	if err != nil {
		return Result{}, err
	}
	if err := recordAction(ctx, tx, s, a, step); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(); err != nil {
		return Result{}, err
	}
	res.Scored = s.Score() != score
	res.Shared = shared(s.G, step, res.Scored)
	res.View = s.View()
	if err := e.decorate(ctx, e.DB, s.G, s.eventID, teamID, &res.View); err != nil {
		return Result{}, err
	}
	if res.View.ResultsHeld {
		res.Points = 0
	}
	return res, nil
}

// shared reports whether a step changed what other teams' phones show:
// an event counter, a finish (the event may now be over), or the score
// where a standings block shows it. Occupancy is not shown to players.
func shared(g *graph.Graph, step *walk.Step, scored bool) bool {
	if step != nil {
		if step.Finished {
			return true
		}
		for _, n := range step.Effects {
			if n.EffectCfg != nil && n.EffectCfg.Kind == graph.SetCounter {
				return true
			}
		}
	}
	if !scored {
		return false
	}
	for _, n := range g.Nodes {
		if slices.ContainsFunc(n.Blocks(), func(b graph.Block) bool { return b.Kind == graph.BlockStandings }) {
			return true
		}
	}
	return false
}

// savePress persists the press an action recorded or withdrew, any
// note it unlocked by itself (a sabotage's false clue), and a player's
// pick at a mission.
func savePress(ctx context.Context, tx *sql.Tx, s *teamState) error {
	for _, entry := range s.NewJournal {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO team_journal (team_id, entry, unlocked_at) VALUES ($1, $2, $3)
			ON CONFLICT (team_id, entry) DO NOTHING`, s.teamID, entry, s.Now); err != nil {
			return err
		}
	}
	if c := s.SealChanged; c != nil {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO team_block_votes (team_id, node, block_id, slot_id, choice) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (team_id, node, block_id, slot_id) DO NOTHING`, s.teamID, c.Node, c.Block, c.Slot, c.Choice); err != nil {
			return err
		}
	}
	c := s.PressChanged
	if c == nil {
		return nil
	}
	if c.Press == nil {
		_, err := tx.ExecContext(ctx, `DELETE FROM team_presses WHERE team_id = $1 AND node = $2 AND slot_id = $3`, s.teamID, s.Current, c.Slot)
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO team_presses(team_id, node, slot_id, value, pressed_at) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (team_id, node, slot_id) DO UPDATE SET value = EXCLUDED.value, pressed_at = EXCLUDED.pressed_at
	`, s.teamID, s.Current, c.Slot, c.Press.Value, c.Press.At)
	return err
}

// Sabotage presses a sabotage block for the player asking (see
// session.Team.Sabotage); the request must be ForPhone.
func (e *Engine) Sabotage(ctx context.Context, teamID int64, node, block string) (Result, error) {
	return e.act(ctx, teamID, action{"sabotage", node, block}, func(t *session.Team) (Result, *walk.Step, error) { return t.Sabotage(node, block) })
}

// Seal records the asking player's pick at a mission (see
// session.Team.Seal); the request must be ForPhone.
func (e *Engine) Seal(ctx context.Context, teamID int64, node, block, choice string) (Result, error) {
	return e.act(ctx, teamID, action{"seal", node, block}, func(t *session.Team) (Result, *walk.Step, error) { return t.Seal(node, block, choice) })
}

// Task answers a task for the player asking (see session.Team.Task); the
// request must be ForPhone.
func (e *Engine) Task(ctx context.Context, teamID int64, node, block, guess string) (Result, error) {
	return e.act(ctx, teamID, action{"task", node, guess}, func(t *session.Team) (Result, *walk.Step, error) { return t.Task(node, block, guess) })
}

// Turn turns the asking player's key in a two-key launch (see
// session.Team.Turn); the request must be ForPhone.
func (e *Engine) Turn(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.act(ctx, teamID, action{"turn", node, ""}, func(t *session.Team) (Result, *walk.Step, error) { return t.Turn(node) })
}

// stay persists an action that left the team where it was: a wrong
// guess counted, a lockout set, a hint bought.
func (e *Engine) stay(ctx context.Context, tx *sql.Tx, s *teamState) error {
	if err := saveNodeRow(ctx, tx, s, s.Current); err != nil {
		return err
	}
	if err := savePicks(ctx, tx, s); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE team_graph_state SET penalty_score = $1, lang = $4, updated_at = $2 WHERE team_id = $3
	`, s.PenaltyScore, s.Now, s.teamID, s.Lang)
	return err
}

// commit persists a step: the node left, every node passed, hub branch
// bookkeeping, effects, and finally the cursor. For a split team the
// cursor stored is the split's (session.Team.Whole); where each group is
// goes with the groups, in act.
func (e *Engine) commit(ctx context.Context, tx *sql.Tx, s *teamState, step walk.Step) error {
	now := s.Now
	old := s.Current
	s.Apply(step)
	if s.Current != old {
		// Done together or not, the step is behind the team: what each
		// player did there starts again on a later visit.
		if _, err := tx.ExecContext(ctx, `DELETE FROM team_presses WHERE team_id = $1 AND node = $2`, s.teamID, old); err != nil {
			return err
		}
		if err := lockRolesLeaving(ctx, tx, s, old); err != nil {
			return fmt.Errorf("locking roles: %w", err)
		}
	}

	if step.From != "" && s.Nodes[step.From] != nil {
		if err := saveNodeRow(ctx, tx, s, step.From); err != nil {
			return err
		}
	}

	for _, slug := range step.Passed {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO team_node_state (team_id, node, first_seen_at)
			VALUES ($1, $2, $3)
			ON CONFLICT (team_id, node) DO NOTHING
		`, s.teamID, slug, now); err != nil {
			return err
		}

		// Passing through a hub means the team has come back out of any
		// branch of it they were in.
		if n, ok := s.G.Node(slug); ok && n.Type == graph.Hub {
			if _, err := tx.ExecContext(ctx, `
				UPDATE team_hub_branches SET returned_at = $1
				WHERE team_id = $2 AND hub = $3 AND returned_at IS NULL
			`, now, s.teamID, slug); err != nil {
				return err
			}
		}
	}

	for _, b := range step.Entered {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO team_hub_branches (team_id, event_id, hub, port, entered_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (team_id, hub, port) DO NOTHING
		`, s.teamID, s.eventID, b.Hub, b.Port, now); err != nil {
			return err
		}
	}

	if err := e.applyEffects(ctx, tx, s, step.Effects); err != nil {
		return err
	}

	var completedAt any
	var outcome any
	if step.Finished {
		completedAt, outcome = now, string(step.Outcome)
		// A finished team is no longer inside any branch; free its places.
		if _, err := tx.ExecContext(ctx, `
			UPDATE team_hub_branches SET returned_at = $1 WHERE team_id = $2 AND returned_at IS NULL
		`, now, s.teamID); err != nil {
			return err
		}
		// teams.completed_at is what the leaderboard, the live view and
		// event auto-completion read. Written
		// in the same transaction so a finished team is never shown as
		// still playing.
		if _, err := tx.ExecContext(ctx, `
			UPDATE teams SET completed_at = COALESCE(completed_at, now_text()) WHERE id = $1
		`, s.teamID); err != nil {
			return err
		}
	}

	// The compare-and-swap. The row lock already serialises actions on
	// this team, so this cannot fail today — it is the guard that keeps
	// the invariant true if that lock is ever loosened.
	res, err := tx.ExecContext(ctx, `
		UPDATE team_graph_state
		SET current_node = $1, node_entered_at = $2, score = $3, penalty_score = $4,
		    penalty_seconds = $5, theme = $6, completed_at = COALESCE(completed_at, $7),
		    outcome = COALESCE(outcome, $8), last_answer = $11, started_at = $12, timed_out = $13, lang = $14, cheated = $15, updated_at = $2,
		    start_paused = $16, node_paused = $17
		WHERE team_id = $9 AND current_node IS NOT DISTINCT FROM $10
	`, nullIfEmpty(s.Whole()), now, s.Points, s.PenaltyScore, s.PenaltySeconds, s.Theme,
		completedAt, outcome, s.teamID, nullIfEmpty(s.whole), s.LatestAnswer, s.StartedAt, s.TimedOut, s.Lang, s.Cheated,
		s.StartPaused, s.Paused)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrStale
	}
	// Where each group is, or that the team is one again.
	if s.Split != "" || s.wasSplit {
		if err := saveSplit(ctx, tx, s); err != nil {
			return err
		}
	}
	s.whole, s.wasSplit = s.Whole(), s.Split != ""
	return nil
}

// applyEffects persists the effects a step passed. Their in-memory
// counterparts were already applied during the walk.
func (e *Engine) applyEffects(ctx context.Context, tx *sql.Tx, s *teamState, effects []graph.Node) error {
	for _, n := range effects {
		if n.EffectCfg == nil {
			continue
		}
		fx := *n.EffectCfg
		var err error
		switch fx.Kind {
		case graph.GrantItem:
			_, err = tx.ExecContext(ctx, `
				INSERT INTO team_items (team_id, item, qty) VALUES ($1, $2, $3)
				ON CONFLICT (team_id, item) DO UPDATE SET qty = team_items.qty + EXCLUDED.qty
			`, s.teamID, fx.Item, session.QtyOf(fx))
		case graph.SetCounter:
			// The value was computed in memory from a read taken under
			// the counter's advisory lock (lockCounters), so it is
			// current and can be written as-is.
			_, err = tx.ExecContext(ctx, `
				INSERT INTO event_counters (event_id, counter, value) VALUES ($1, $2, $3)
				ON CONFLICT (event_id, counter) DO UPDATE SET value = EXCLUDED.value
			`, s.eventID, fx.Counter, s.Counters[fx.Counter])
		case graph.UnlockJournal:
			_, err = tx.ExecContext(ctx, `
				INSERT INTO team_journal (team_id, entry, unlocked_at) VALUES ($1, $2, $3)
				ON CONFLICT (team_id, entry) DO NOTHING
			`, s.teamID, fx.JournalEntry, s.Now)
		case graph.SendEmail, graph.CallWebhook:
			// The request is queued as the step says it now, so editing
			// the adventure later does not change a call already owed.
			var kind string
			var payload any
			if fx.Kind == graph.CallWebhook {
				kind, payload = "webhook", effectq.Webhook{URL: fx.URL, Method: fx.Method, Headers: fx.Headers}
			} else {
				subject, body, title := s.G.Email(s.Lang, n)
				kind, payload = "email", effectq.Email{Subject: subject, Body: body, Step: title}
			}
			doc, _ := json.Marshal(payload)
			_, err = tx.ExecContext(ctx, `
				INSERT INTO graph_effect_outbox (event_id, team_id, node, kind, payload)
				VALUES ($1, $2, $3, $4, $5)
			`, s.eventID, s.teamID, n.Slug, kind, doc)
		case graph.AdjustScore, graph.SetTheme:
			// Persisted with the cursor update in commit.
		}
		if err != nil {
			return fmt.Errorf("applying effect %s: %w", n.Slug, err)
		}
	}
	return nil
}

func saveNodeRow(ctx context.Context, tx *sql.Tx, s *teamState, slug string) error {
	r := s.Row(slug)
	var solvedAt any
	if r.Solved {
		solvedAt = s.Now
	}
	var outcome any
	if r.HasOutcome {
		outcome = r.Outcome
	}
	var blocked any
	if r.BlockedUntil != nil {
		blocked = *r.BlockedUntil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO team_node_state (team_id, node, first_seen_at, solved_at, outcome, attempts, blocked_until, hints_revealed, answer, match_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (team_id, node) DO UPDATE SET
		    solved_at      = COALESCE(team_node_state.solved_at, EXCLUDED.solved_at),
		    outcome        = COALESCE(EXCLUDED.outcome, team_node_state.outcome),
		    attempts       = EXCLUDED.attempts,
		    blocked_until  = EXCLUDED.blocked_until,
		    hints_revealed = EXCLUDED.hints_revealed,
		    answer         = EXCLUDED.answer,
		    match_id       = EXCLUDED.match_id
	`, s.teamID, slug, r.FirstSeen, solvedAt, outcome, r.Attempts, blocked, r.HintsRevealed, r.Answer, r.MatchID)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Media returns the media ids a team's phone should download ahead (see
// session.Team.PrefetchMedia). A team with no graph state sees nothing.
func (e *Engine) Media(ctx context.Context, teamID int64) ([]int64, error) {
	return e.media(ctx, teamID, true)
}

// media returns the media ids a team may fetch now (see
// session.Team.VisibleMedia), honouring the adventure's strict-assets
// setting, or with prefetch only those worth downloading ahead.
func (e *Engine) media(ctx context.Context, teamID int64, prefetch bool) ([]int64, error) {
	var (
		strict              bool
		picture, right, bad int64
		cover               int64
	)
	// The adventure's own look may have a picture, sounds and a cover:
	// every team may fetch them, an adventure with a graph or without.
	if err := e.DB.QueryRowContext(ctx, `
		SELECT a.strict_assets,
		       COALESCE(CASE WHEN a.backdrop->>'kind' = 'picture' THEN (a.backdrop->>'media')::bigint END, 0),
		       COALESCE((a.look_style->>'sound_right')::bigint, 0),
		       COALESCE((a.look_style->>'sound_wrong')::bigint, 0),
		       COALESCE((a.look_style->>'cover')::bigint, 0)
		FROM teams t JOIN events ev ON ev.id = t.event_id JOIN adventures a ON a.slug = ev.adventure_slug
		WHERE t.id = $1
	`, teamID).Scan(&strict, &picture, &right, &bad, &cover); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var ids []int64
	for _, id := range []int64{picture, right, bad, cover} {
		if id > 0 && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	s, err := e.loadCore(ctx, e.DB, teamID, false)
	if errors.Is(err, ErrNoGraph) {
		return ids, nil
	}
	if err != nil {
		return nil, err
	}
	// A block shown inside a zone, or only to the player standing there,
	// brings its picture with it: where the team is decides what it may
	// fetch, as it decides what it sees.
	if err := e.locate(ctx, e.DB, s); err != nil {
		return nil, err
	}
	// A split team's phone sees its own group's step.
	if _, err := e.divide(ctx, e.DB, s); err != nil {
		return nil, err
	}
	s.Focus()
	seen := s.VisibleMedia(strict)
	if prefetch {
		seen = s.PrefetchMedia(strict)
	}
	for _, id := range seen {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// CanSeeMedia reports whether a team may fetch one media file.
func (e *Engine) CanSeeMedia(ctx context.Context, teamID, mediaID int64) (bool, error) {
	ids, err := e.media(ctx, teamID, false)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == mediaID {
			return true, nil
		}
	}
	return false, nil
}

// recordAction appends a player action to the event's result chain, in
// the action's transaction: the step acted on as it stands after (tries,
// solved, hints, the answer kept), where the team went, and its standing
// (results.Append adds it). A finish appends the team's finish entry,
// whose hash is its receipt.
func recordAction(ctx context.Context, tx *sql.Tx, s *teamState, a action, step *walk.Step) error {
	fields := map[string]any{"action": a.name}
	if a.node != "" {
		fields["node"] = a.node
		r := s.Row(a.node)
		fields["step"] = map[string]any{"attempts": r.Attempts, "solved": r.Solved, "hints": r.HintsRevealed, "answer": r.Answer}
	}
	if a.input != "" {
		fields["input"] = a.input
	}
	if step != nil {
		fields["to"] = step.To
		if step.Finished {
			fields["finished"] = string(step.Outcome)
		}
	}
	if _, err := results.Append(ctx, tx, s.eventID, s.teamID, "play."+a.name, fields); err != nil {
		return err
	}
	if step != nil && step.Finished {
		if _, err := results.Append(ctx, tx, s.eventID, s.teamID, results.TeamFinished, map[string]any{"outcome": string(step.Outcome)}); err != nil {
			return err
		}
	}
	return nil
}
