package play

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graph/valid"
	"github.com/klahr/zonventure/internal/graph/walk"
	"github.com/klahr/zonventure/internal/graphstore"
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
		slug    string
		pinned  sql.NullInt64
		graphID int64
	)
	if err := tx.QueryRowContext(ctx,
		`SELECT adventure_slug, graph_id FROM events WHERE id = $1 FOR UPDATE`, eventID,
	).Scan(&slug, &pinned); err != nil {
		return fmt.Errorf("loading event %d: %w", eventID, err)
	}

	if pinned.Valid {
		graphID = pinned.Int64
	} else {
		var adventureID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM adventures WHERE slug = $1`, slug).Scan(&adventureID); err != nil {
			return fmt.Errorf("loading adventure %q: %w", slug, err)
		}
		stored, err := graphstore.Published(ctx, tx, adventureID)
		if errors.Is(err, graphstore.ErrNotFound) {
			return ErrNoGraph
		}
		if err != nil {
			return err
		}
		if r := valid.Check(stored.Graph); r.HasErrors() {
			return &graphstore.InvalidError{Report: r}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE events SET graph_id = $1 WHERE id = $2`, stored.ID, eventID); err != nil {
			return err
		}
		graphID = stored.ID
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT t.id FROM teams t
		LEFT JOIN team_graph_state s ON s.team_id = t.id
		WHERE t.event_id = $1 AND s.team_id IS NULL AND t.withdrawn_at IS NULL
		ORDER BY t.id
	`, eventID)
	if err != nil {
		return err
	}
	var teams []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		teams = append(teams, id)
	}
	if err := closeRows(rows); err != nil {
		return err
	}

	now := e.now()
	for _, teamID := range teams {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO team_graph_state (team_id, event_id, graph_id, started_at, node_entered_at, updated_at)
			VALUES ($1, $2, $3, $4, $4, $4)
		`, teamID, eventID, graphID, now); err != nil {
			return fmt.Errorf("placing team %d: %w", teamID, err)
		}
		s, err := e.loadTeam(ctx, tx, teamID)
		if err != nil {
			return err
		}
		// A team that chooses its start waits, unplaced, until it does.
		if s.G.ChoosesStart() {
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
	return e.act(ctx, teamID, func(t *session.Team) (Result, *walk.Step, error) { return t.Answer(node, guess) })
}

// ChooseStart begins the team's adventure at one of its starts.
func (e *Engine) ChooseStart(ctx context.Context, teamID int64, start string) (Result, error) {
	return e.act(ctx, teamID, func(t *session.Team) (Result, *walk.Step, error) { return t.ChooseStart(start) })
}

// Continue moves the team past a story node.
func (e *Engine) Continue(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.act(ctx, teamID, func(t *session.Team) (Result, *walk.Step, error) { return t.Continue(node) })
}

// SetLanguage switches the language the team plays in.
func (e *Engine) SetLanguage(ctx context.Context, teamID int64, lang string) (Result, error) {
	return e.act(ctx, teamID, func(t *session.Team) (Result, *walk.Step, error) { return Result{}, nil, t.SetLanguage(lang) })
}

// TimeUp applies the adventure's time limit once it has passed. The
// client calls it when its countdown ends; the engine re-checks.
func (e *Engine) TimeUp(ctx context.Context, teamID int64) (Result, error) {
	return e.act(ctx, teamID, func(t *session.Team) (Result, *walk.Step, error) { return t.TimeUp() })
}

// Back returns the team from inside a home screen's branch to that home
// screen. See session.Team.Back.
func (e *Engine) Back(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.act(ctx, teamID, func(t *session.Team) (Result, *walk.Step, error) { return t.Back(node) })
}

// Choose enters a hub branch, or moves on from a satisfied hub when port
// is graph.PortDone.
func (e *Engine) Choose(ctx context.Context, teamID int64, hub, port string) (Result, error) {
	return e.act(ctx, teamID, func(t *session.Team) (Result, *walk.Step, error) { return t.Choose(hub, port) })
}

// Skip takes a puzzle's skip output once both thresholds are met.
func (e *Engine) Skip(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.act(ctx, teamID, func(t *session.Team) (Result, *walk.Step, error) { return t.Skip(node) })
}

// Timeout takes a puzzle's timeout output once its limit has passed.
// The client calls it when its countdown reaches zero; the engine
// re-checks the time rather than trusting the client.
func (e *Engine) Timeout(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.act(ctx, teamID, func(t *session.Team) (Result, *walk.Step, error) { return t.Timeout(node) })
}

// RevealHint reveals the next hint on the current puzzle.
func (e *Engine) RevealHint(ctx context.Context, teamID int64, node string, index int) (Result, error) {
	return e.act(ctx, teamID, func(t *session.Team) (Result, *walk.Step, error) { return t.RevealHint(node, index) })
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
	lat, lng, ok, err := e.locator().Position(ctx, tx, teamID)
	if err != nil {
		return View{}, fmt.Errorf("reading position: %w", err)
	}
	if ok {
		s.Position = &session.Position{Lat: lat, Lng: lng}
	}
	return s.View(), nil
}

// --- the shared action skeleton ---------------------------------------

// act runs one player action: lock and load the team, run the action's
// rules, persist whatever changed, and commit — or roll everything back.
func (e *Engine) act(ctx context.Context, teamID int64, fn func(*session.Team) (Result, *walk.Step, error)) (Result, error) {
	tx, err := e.DB.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback() }()

	s, err := e.loadTeam(ctx, tx, teamID)
	if err != nil {
		return Result{}, err
	}

	res, step, err := fn(s.Team)
	if err != nil {
		// Some refusals (a lockout) still carry information worth
		// returning, but nothing is persisted.
		res.View = s.View()
		return res, err
	}
	if step != nil {
		err = e.commit(ctx, tx, s, *step)
	} else {
		err = e.stay(ctx, tx, s)
	}
	if err != nil {
		return Result{}, err
	}
	if err := tx.Commit(); err != nil {
		return Result{}, err
	}
	res.View = s.View()
	return res, nil
}

// stay persists an action that left the team where it was: a wrong
// guess counted, a lockout set, a hint bought.
func (e *Engine) stay(ctx context.Context, tx *sql.Tx, s *teamState) error {
	if err := saveNodeRow(ctx, tx, s, s.Current); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE team_graph_state SET penalty_score = $1, lang = $4, updated_at = $2 WHERE team_id = $3
	`, s.PenaltyScore, s.Now, s.teamID, s.Lang)
	return err
}

// commit persists a step: the node left, every node passed, hub branch
// bookkeeping, effects, and finally the cursor.
func (e *Engine) commit(ctx context.Context, tx *sql.Tx, s *teamState, step walk.Step) error {
	now := s.Now
	old := s.Current
	s.Apply(step)

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
		// event auto-completion read, for lanes and graphs alike. Written
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
		    outcome = COALESCE(outcome, $8), last_answer = $11, started_at = $12, timed_out = $13, lang = $14, updated_at = $2
		WHERE team_id = $9 AND current_node IS NOT DISTINCT FROM $10
	`, nullIfEmpty(step.To), now, s.Points, s.PenaltyScore, s.PenaltySeconds, s.Theme,
		completedAt, outcome, s.teamID, nullIfEmpty(old), s.LatestAnswer, s.StartedAt, s.TimedOut, s.Lang)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrStale
	}
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
			// The value was computed in memory under this transaction's
			// view; write it as-is. Counters are shared across teams, so
			// the upsert is what makes concurrent writers converge.
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
			kind, payload := "email", map[string]string{"template": fx.Template}
			if fx.Kind == graph.CallWebhook {
				kind, payload = "webhook", map[string]string{"url": fx.URL}
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

// Media returns the media ids a team may fetch now (see
// session.Team.VisibleMedia), honouring the adventure's strict-assets
// setting. A team with no graph state sees nothing.
func (e *Engine) Media(ctx context.Context, teamID int64) ([]int64, error) {
	s, err := e.loadCore(ctx, e.DB, teamID, false)
	if errors.Is(err, ErrNoGraph) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var strict bool
	if err := e.DB.QueryRowContext(ctx, `
		SELECT a.strict_assets FROM events ev JOIN adventures a ON a.slug = ev.adventure_slug
		WHERE ev.id = $1
	`, s.eventID).Scan(&strict); err != nil {
		return nil, err
	}
	return s.VisibleMedia(strict), nil
}

// CanSeeMedia reports whether a team may fetch one media file.
func (e *Engine) CanSeeMedia(ctx context.Context, teamID, mediaID int64) (bool, error) {
	ids, err := e.Media(ctx, teamID)
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
