package play

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/valid"
	"github.com/klahr/zonventure/internal/graph/walk"
	"github.com/klahr/zonventure/internal/graphstore"
)

// StartEvent pins the adventure's latest graph to the event and places
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
		stored, err := graphstore.Latest(ctx, tx, adventureID)
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
		step, err := walk.First(s.g, s)
		if err != nil {
			return fmt.Errorf("placing team %d: %w", teamID, err)
		}
		if err := e.commit(ctx, tx, s, step); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Answer submits a guess at the puzzle the team is on.
//
// node is the puzzle the client believes the team is at. If the team has
// moved since — a teammate answered first — the guess is refused with
// ErrStale rather than applied to whatever the team is on now, which
// would be answering a question the player has never seen.
func (e *Engine) Answer(ctx context.Context, teamID int64, node, guess string) (Result, error) {
	return e.act(ctx, teamID, node, func(tx *sql.Tx, s *teamState, n graph.Node) (Result, *walk.Step, error) {
		if n.Type != graph.Puzzle || n.PuzzleCfg == nil {
			return Result{}, nil, ErrWrongNode
		}
		cfg := n.PuzzleCfg
		r := s.row(n.Slug)

		if r.blockedUntil != nil && s.now.Before(*r.blockedUntil) {
			return Result{BlockedUntil: r.blockedUntil}, nil, ErrLocked
		}
		if cfg.Zone != "" && !s.InZone(cfg.Zone) {
			return Result{}, nil, ErrNotHere
		}
		if cfg.TimeLimitSeconds > 0 && s.NodeElapsedSeconds() > float64(cfg.TimeLimitSeconds) {
			return Result{}, nil, ErrTimeUp
		}

		// First answer output whose matchers and zone constraint both
		// hold. Order is the author's: it is how they express precedence
		// between overlapping answers.
		for _, o := range cfg.Outputs {
			if o.Trigger != graph.OnAnswer {
				continue
			}
			m := match.Any(o.Matchers, guess)
			if !m.OK || !outputZoneOK(s, o) {
				continue
			}
			points := float64(o.Points) * m.Multiplier
			r.solved = true
			step, err := e.leave(s, n, o.Port, points)
			if err != nil {
				return Result{}, nil, err
			}
			return Result{Accepted: true, Port: o.Port, Points: points}, step, nil
		}

		// Wrong. Count it, lock out if configured, and take a pity path
		// if the author set one and it is now due.
		r.attempts++
		res := Result{}
		if cfg.LockoutSeconds > 0 {
			until := s.now.Add(time.Duration(cfg.LockoutSeconds) * time.Second)
			r.blockedUntil = &until
			res.BlockedUntil = &until
		}
		for _, o := range cfg.Outputs {
			if o.Trigger == graph.OnAttempts && o.AfterAttempts > 0 && r.attempts >= o.AfterAttempts {
				step, err := e.leave(s, n, o.Port, 0)
				if err != nil {
					return Result{}, nil, err
				}
				res.Port = o.Port
				return res, step, nil
			}
		}
		if resp, ok := cfg.ResponseFor(r.attempts); ok {
			res.Response = resp.Text
		}
		if err := saveNodeRow(ctx, tx, s, n.Slug); err != nil {
			return Result{}, nil, err
		}
		return res, nil, nil
	})
}

// outputZoneOK applies an output's zone constraint, including the
// inverted form: "correct only if you are NOT here".
func outputZoneOK(s *teamState, o graph.Output) bool {
	if o.Zone == "" {
		return true
	}
	in := s.InZone(o.Zone)
	if o.Inverted {
		return !in
	}
	return in
}

// Continue moves the team past a story node.
func (e *Engine) Continue(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.act(ctx, teamID, node, func(_ *sql.Tx, s *teamState, n graph.Node) (Result, *walk.Step, error) {
		if n.Type != graph.Story {
			return Result{}, nil, ErrWrongNode
		}
		if n.StoryCfg != nil && n.StoryCfg.Zone != "" && !s.InZone(n.StoryCfg.Zone) {
			return Result{}, nil, ErrNotHere
		}
		s.row(n.Slug).solved = true
		step, err := e.leave(s, n, graph.PortOut, 0)
		return Result{Port: graph.PortOut}, step, err
	})
}

// Choose enters a hub branch, or moves on from a satisfied hub when port
// is graph.PortDone.
func (e *Engine) Choose(ctx context.Context, teamID int64, hub, port string) (Result, error) {
	return e.act(ctx, teamID, hub, func(_ *sql.Tx, s *teamState, n graph.Node) (Result, *walk.Step, error) {
		if n.Type != graph.Hub {
			return Result{}, nil, ErrWrongNode
		}
		step, err := walk.EnterBranch(s.g, s, hub, port)
		if err != nil {
			return Result{}, nil, fmt.Errorf("%w: %v", ErrNotAllowed, err)
		}
		return Result{Port: port}, &step, nil
	})
}

// Skip takes a puzzle's skip output. Both thresholds must be met — the
// anti-abuse rule carried over from legacy: spamming guesses alone must
// never unlock a skip.
func (e *Engine) Skip(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.act(ctx, teamID, node, func(_ *sql.Tx, s *teamState, n graph.Node) (Result, *walk.Step, error) {
		if n.Type != graph.Puzzle || n.PuzzleCfg == nil {
			return Result{}, nil, ErrWrongNode
		}
		if !canSkip(s, n) {
			return Result{}, nil, ErrNotAllowed
		}
		o, ok := outputFor(n, graph.OnSkip)
		if !ok {
			return Result{}, nil, ErrNotAllowed
		}
		step, err := e.leave(s, n, o.Port, 0)
		return Result{Port: o.Port}, step, err
	})
}

// Timeout takes a puzzle's timeout output once its limit has passed.
// The client calls it when its countdown reaches zero; the server
// re-checks the time rather than trusting the client.
func (e *Engine) Timeout(ctx context.Context, teamID int64, node string) (Result, error) {
	return e.act(ctx, teamID, node, func(_ *sql.Tx, s *teamState, n graph.Node) (Result, *walk.Step, error) {
		if n.Type != graph.Puzzle || n.PuzzleCfg == nil {
			return Result{}, nil, ErrWrongNode
		}
		limit := n.PuzzleCfg.TimeLimitSeconds
		if limit <= 0 || s.NodeElapsedSeconds() < float64(limit) {
			return Result{}, nil, ErrNotAllowed
		}
		o, ok := outputFor(n, graph.OnTimeout)
		if !ok {
			return Result{}, nil, ErrNotAllowed
		}
		step, err := e.leave(s, n, o.Port, 0)
		return Result{Port: o.Port}, step, err
	})
}

// RevealHint reveals the next hint on the current puzzle.
//
// Hints reveal in order: index must be the next unrevealed one, so a
// client cannot skip to the most useful hint. The delay counts from when
// the team reached the puzzle, and the hint's cost is charged to the
// team's penalty score.
func (e *Engine) RevealHint(ctx context.Context, teamID int64, node string, index int) (Result, error) {
	return e.act(ctx, teamID, node, func(tx *sql.Tx, s *teamState, n graph.Node) (Result, *walk.Step, error) {
		if n.Type != graph.Puzzle || n.PuzzleCfg == nil {
			return Result{}, nil, ErrWrongNode
		}
		r := s.row(n.Slug)
		hints := n.PuzzleCfg.Hints
		if index != r.hintsRevealed || index < 0 || index >= len(hints) {
			return Result{}, nil, ErrNotAllowed
		}
		h := hints[index]
		if s.NodeElapsedSeconds() < float64(h.DelaySeconds) {
			return Result{}, nil, ErrNotAllowed
		}
		r.hintsRevealed++
		s.penaltyScore += float64(h.Cost)
		if err := saveNodeRow(ctx, tx, s, n.Slug); err != nil {
			return Result{}, nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE team_graph_state SET penalty_score = $1, updated_at = $2 WHERE team_id = $3
		`, s.penaltyScore, s.now, s.teamID); err != nil {
			return Result{}, nil, err
		}
		return Result{Hint: h.Text}, nil, nil
	})
}

// Snapshot returns the team's current view without changing anything.
func (e *Engine) Snapshot(ctx context.Context, teamID int64) (View, error) {
	tx, err := e.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return View{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// A read-only transaction cannot take FOR UPDATE, so load without it.
	s, err := e.loadTeamReadOnly(ctx, tx, teamID)
	if err != nil {
		return View{}, err
	}
	return s.view(), nil
}

// --- the shared action skeleton ---------------------------------------

type actionFn func(tx *sql.Tx, s *teamState, n graph.Node) (Result, *walk.Step, error)

// act runs one player action: lock and load the team, check the client
// is acting on the current node, run the action, persist any move, and
// commit — or roll everything back.
func (e *Engine) act(ctx context.Context, teamID int64, node string, fn actionFn) (Result, error) {
	tx, err := e.DB.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback() }()

	s, err := e.loadTeam(ctx, tx, teamID)
	if err != nil {
		return Result{}, err
	}
	if s.completed {
		return Result{View: s.view()}, ErrFinished
	}
	if s.current != node {
		return Result{View: s.view()}, ErrStale
	}
	n, ok := s.g.Node(node)
	if !ok {
		return Result{}, fmt.Errorf("current node %q is not in the graph", node)
	}

	res, step, err := fn(tx, s, n)
	if err != nil {
		// Some refusals (a lockout) still carry information worth
		// returning, but nothing is persisted.
		res.View = s.view()
		return res, err
	}
	if step != nil {
		if err := e.commit(ctx, tx, s, *step); err != nil {
			return Result{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Result{}, err
	}
	res.View = s.view()
	return res, nil
}

// leave records the team leaving node n by port — the outcome that
// history-based gates read — and walks on.
func (e *Engine) leave(s *teamState, n graph.Node, port string, points float64) (*walk.Step, error) {
	r := s.row(n.Slug)
	r.outcome, r.hasOutcome = port, true
	s.score += points
	step, err := walk.Advance(s.g, s, n.Slug, port)
	if err != nil {
		return nil, fmt.Errorf("advancing from %s.%s: %w", n.Slug, port, err)
	}
	return &step, nil
}

// commit persists a step: the node left, every node passed, hub branch
// bookkeeping, effects, and finally the cursor.
func (e *Engine) commit(ctx context.Context, tx *sql.Tx, s *teamState, step walk.Step) error {
	now := s.now

	if step.From != "" && s.nodes[step.From] != nil {
		if err := saveNodeRow(ctx, tx, s, step.From); err != nil {
			return err
		}
	}

	for _, slug := range step.Passed {
		s.row(slug) // mark visited in memory
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO team_node_state (team_id, node, first_seen_at)
			VALUES ($1, $2, $3)
			ON CONFLICT (team_id, node) DO NOTHING
		`, s.teamID, slug, now); err != nil {
			return err
		}

		// Passing through a hub means the team has come back out of any
		// branch of it they were in.
		if n, ok := s.g.Node(slug); ok && n.Type == graph.Hub {
			if _, err := tx.ExecContext(ctx, `
				UPDATE team_hub_branches SET returned_at = $1
				WHERE team_id = $2 AND hub = $3 AND returned_at IS NULL
			`, now, s.teamID, slug); err != nil {
				return err
			}
		}
	}

	for _, b := range step.Entered {
		s.markEntered(b.Hub, b.Port)
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
		s.completed = true
		s.outcome = step.Outcome
		completedAt, outcome = now, string(step.Outcome)
		// A finished team is no longer inside any branch; free its places.
		if _, err := tx.ExecContext(ctx, `
			UPDATE team_hub_branches SET returned_at = $1 WHERE team_id = $2 AND returned_at IS NULL
		`, now, s.teamID); err != nil {
			return err
		}
	}

	// The compare-and-swap. The row lock already serialises actions on
	// this team, so this cannot fail today — it is the guard that keeps
	// the invariant true if that lock is ever loosened.
	old := s.current
	res, err := tx.ExecContext(ctx, `
		UPDATE team_graph_state
		SET current_node = $1, node_entered_at = $2, score = $3, penalty_score = $4,
		    penalty_seconds = $5, theme = $6, completed_at = COALESCE(completed_at, $7),
		    outcome = COALESCE(outcome, $8), updated_at = $2
		WHERE team_id = $9 AND current_node IS NOT DISTINCT FROM $10
	`, nullIfEmpty(step.To), now, s.score, s.penaltyScore, s.penaltySeconds, s.theme,
		completedAt, outcome, s.teamID, nullIfEmpty(old))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrStale
	}
	s.current = step.To
	s.nodeEnteredAt = now
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
			`, s.teamID, fx.Item, qtyOf(fx))
		case graph.SetCounter:
			// The value was computed in memory under this transaction's
			// view; write it as-is. Counters are shared across teams, so
			// the upsert is what makes concurrent writers converge.
			_, err = tx.ExecContext(ctx, `
				INSERT INTO event_counters (event_id, counter, value) VALUES ($1, $2, $3)
				ON CONFLICT (event_id, counter) DO UPDATE SET value = EXCLUDED.value
			`, s.eventID, fx.Counter, s.counters[fx.Counter])
		case graph.UnlockJournal:
			_, err = tx.ExecContext(ctx, `
				INSERT INTO team_journal (team_id, entry, unlocked_at) VALUES ($1, $2, $3)
				ON CONFLICT (team_id, entry) DO NOTHING
			`, s.teamID, fx.JournalEntry, s.now)
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
	r := s.row(slug)
	var solvedAt any
	if r.solved {
		solvedAt = s.now
	}
	var outcome any
	if r.hasOutcome {
		outcome = r.outcome
	}
	var blocked any
	if r.blockedUntil != nil {
		blocked = *r.blockedUntil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO team_node_state (team_id, node, first_seen_at, solved_at, outcome, attempts, blocked_until, hints_revealed)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (team_id, node) DO UPDATE SET
		    solved_at      = COALESCE(team_node_state.solved_at, EXCLUDED.solved_at),
		    outcome        = COALESCE(EXCLUDED.outcome, team_node_state.outcome),
		    attempts       = EXCLUDED.attempts,
		    blocked_until  = EXCLUDED.blocked_until,
		    hints_revealed = EXCLUDED.hints_revealed
	`, s.teamID, slug, r.firstSeen, solvedAt, outcome, r.attempts, blocked, r.hintsRevealed)
	return err
}

func outputFor(n graph.Node, t graph.Trigger) (graph.Output, bool) {
	for _, o := range n.PuzzleCfg.Outputs {
		if o.Trigger == t {
			return o, true
		}
	}
	return graph.Output{}, false
}

func canSkip(s *teamState, n graph.Node) bool {
	cfg := n.PuzzleCfg
	if _, ok := outputFor(n, graph.OnSkip); !ok {
		return false
	}
	return s.Attempts(n.Slug) >= cfg.SkipAfterGuesses &&
		s.NodeElapsedSeconds() >= float64(cfg.SkipAfterSeconds)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// loadTeamReadOnly is loadTeam without the row lock or the advisory
// locks, for Snapshot. Occupancy is irrelevant to a read.
func (e *Engine) loadTeamReadOnly(ctx context.Context, tx *sql.Tx, teamID int64) (*teamState, error) {
	s := &teamState{
		teamID: teamID, now: e.now(),
		nodes: map[string]*nodeRow{}, items: map[string]int{}, journal: map[string]bool{},
		counters: map[string]float64{}, entered: map[string]map[string]bool{}, occ: map[string]int{},
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
		FROM team_graph_state WHERE team_id = $1
	`, teamID).Scan(&s.eventID, &s.graphID, &current, &entered, &s.startedAt,
		&completed, &outcome, &s.score, &s.penaltyScore, &s.penaltySeconds, &s.theme)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoGraph
	}
	if err != nil {
		return nil, err
	}
	s.current = current.String
	if entered.Valid {
		s.nodeEnteredAt = entered.Time
	}
	s.completed, s.outcome = completed.Valid, graph.Outcome(outcome.String)
	if s.g, err = e.graph(ctx, tx, s.graphID); err != nil {
		return nil, err
	}
	return s, s.loadRest(ctx, tx)
}

// view renders the team's state for the player.
func (s *teamState) view() View {
	v := View{
		Node:           s.current,
		Score:          s.score,
		PenaltyScore:   s.penaltyScore,
		PenaltySeconds: s.penaltySeconds,
		Finished:       s.completed,
		Outcome:        s.outcome,
		Theme:          s.theme,
	}
	if len(s.items) > 0 {
		v.Items = map[string]int{}
		for k, q := range s.items {
			if q > 0 {
				v.Items[k] = q
			}
		}
	}
	for entry := range s.journal {
		v.Journal = append(v.Journal, entry)
	}

	n, ok := s.g.Node(s.current)
	if !ok {
		return v
	}
	v.Type, v.Title = n.Type, n.Title
	zone := func(slug string) *graph.Zone {
		for _, z := range s.g.Zones {
			if z.Slug == slug {
				zc := z
				return &zc
			}
		}
		return nil
	}

	switch n.Type {
	case graph.Puzzle:
		cfg := n.PuzzleCfg
		v.Text = cfg.Prompt
		if cfg.Zone != "" {
			v.Zone = zone(cfg.Zone)
		}
		r := s.nodes[n.Slug]
		revealed := 0
		if r != nil {
			revealed, v.Attempts = r.hintsRevealed, r.attempts
			if r.blockedUntil != nil && s.now.Before(*r.blockedUntil) {
				v.BlockedUntil = r.blockedUntil
			}
		}
		for i := 0; i < revealed && i < len(cfg.Hints); i++ {
			v.Hints = append(v.Hints, cfg.Hints[i].Text)
		}
		v.HintCount = len(cfg.Hints)
		if cfg.TimeLimitSeconds > 0 && !s.nodeEnteredAt.IsZero() {
			d := s.nodeEnteredAt.Add(time.Duration(cfg.TimeLimitSeconds) * time.Second)
			v.Deadline = &d
		}
		v.CanSkip = canSkip(s, n)
	case graph.Story:
		if n.StoryCfg != nil {
			v.Text = n.StoryCfg.Text
			if n.StoryCfg.Zone != "" {
				v.Zone = zone(n.StoryCfg.Zone)
			}
		}
	case graph.End:
		if n.EndCfg != nil {
			v.Text = n.EndCfg.Text
		}
	case graph.Hub:
		// Recompute the offer: it is a function of state, not stored.
		if n.HubCfg != nil {
			met := s.CompletedBranches(n.Slug) >= n.HubCfg.RequiredBranches()
			for _, b := range n.HubCfg.Branches {
				if s.BranchDone(n.Slug, b.Port) {
					continue
				}
				open := cond.Eval(b.Condition, s)
				if !open && n.HubCfg.HideLocked {
					continue
				}
				v.Offer = append(v.Offer, OfferEntry{Port: b.Port, Label: b.Label, Locked: !open})
			}
			v.CanFinish = met
		}
	}
	return v
}
