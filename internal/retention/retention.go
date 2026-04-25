// Package retention implements the periodic privacy-driven prune of
// player location pings, answer attempts, and auth audit events. The
// thresholds match what /privacy promises (location/answers: 30 days
// post-run-completion; auth events: 90 days from emission); both are
// operator-tunable via flags.
//
// Runs as a single ticking goroutine like internal/scheduler — same
// shutdown contract: cancel the ctx and the loop drains, returning a
// "done" channel main can wait on.
package retention

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"git.sr.ht/~klahr/zv/internal/logx"
)

// Config controls thresholds + cadence. Zero values are treated as
// "feature disabled" so a misconfigured deployment errs on the side
// of keeping data rather than nuking it.
type Config struct {
	// LocationDays is how long after a run completes we keep its
	// player location pings + answer attempts. Default 30.
	LocationDays int
	// AuditDays is how long auth_events rows stick around from their
	// timestamp. Default 90.
	AuditDays int
	// Interval between sweeps. The work is cheap and incremental;
	// once an hour is plenty.
	Interval time.Duration
}

// Start launches the sweep loop and returns a channel closed when the
// goroutine exits. Mirror of scheduler.Start so main can wait on
// shutdown the same way.
func Start(ctx context.Context, db *sql.DB, cfg Config) <-chan struct{} {
	if cfg.Interval <= 0 {
		cfg.Interval = time.Hour
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(cfg.Interval)
		defer t.Stop()
		// Run an initial sweep at startup so a long-restarted server
		// catches up immediately rather than waiting an hour.
		sweep(ctx, db, cfg)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				sweep(ctx, db, cfg)
			}
		}
	}()
	return done
}

// sweep runs one pass of all configured retention deletes. Each
// statement is bounded to deleting at most batchSize rows so a backlog
// from a long downtime doesn't take a giant write lock. The loop
// re-runs each statement in tight succession until it deletes zero
// rows, then moves to the next.
const batchSize = 5000

func sweep(ctx context.Context, db *sql.DB, cfg Config) {
	if cfg.LocationDays > 0 {
		pruneCompletedRunPings(ctx, db, cfg.LocationDays)
		pruneCompletedRunAnswers(ctx, db, cfg.LocationDays)
	}
	if cfg.AuditDays > 0 {
		pruneAuthEvents(ctx, db, cfg.AuditDays)
	}
}

func pruneCompletedRunPings(ctx context.Context, db *sql.DB, days int) {
	stmt := `
		DELETE FROM location_pings
		WHERE id IN (
		  SELECT lp.id
		  FROM location_pings lp
		  JOIN adventure_runs ar ON ar.id = lp.run_id
		  WHERE ar.state = 'completed'
		    AND ar.completed_at IS NOT NULL
		    AND ar.completed_at::timestamptz < now() - ($1 || ' days')::interval
		  LIMIT ` + strconv.Itoa(batchSize) + `
		)
	`
	runBoundedDelete(ctx, db, "retention.location_pings", stmt, days)
}

func pruneCompletedRunAnswers(ctx context.Context, db *sql.DB, days int) {
	stmt := `
		DELETE FROM answer_attempts
		WHERE id IN (
		  SELECT aa.id
		  FROM answer_attempts aa
		  JOIN teams t ON t.id = aa.team_id
		  JOIN adventure_runs ar ON ar.id = t.run_id
		  WHERE ar.state = 'completed'
		    AND ar.completed_at IS NOT NULL
		    AND ar.completed_at::timestamptz < now() - ($1 || ' days')::interval
		  LIMIT ` + strconv.Itoa(batchSize) + `
		)
	`
	runBoundedDelete(ctx, db, "retention.answer_attempts", stmt, days)
}

func pruneAuthEvents(ctx context.Context, db *sql.DB, days int) {
	// auth_events.ts is also a TEXT timestamp produced by now_text();
	// cast to timestamptz so the comparison works.
	stmt := `
		DELETE FROM auth_events
		WHERE id IN (
		  SELECT id FROM auth_events
		  WHERE ts::timestamptz < now() - ($1 || ' days')::interval
		  LIMIT ` + strconv.Itoa(batchSize) + `
		)
	`
	runBoundedDelete(ctx, db, "retention.auth_events", stmt, days)
}

// runBoundedDelete executes the bounded delete repeatedly until a pass
// removes zero rows, so a sweep clears the whole backlog without any
// single statement being unbounded. Each pass is independent — a tick
// failure doesn't roll back the previous batch's deletes, by design:
// privacy retention is an "always making forward progress" job.
func runBoundedDelete(ctx context.Context, db *sql.DB, label, stmt string, days int) {
	total := int64(0)
	for {
		if ctx.Err() != nil {
			return
		}
		res, err := db.ExecContext(ctx, stmt, days)
		if err != nil {
			logx.Error(label+".failed", "err", err.Error())
			return
		}
		n, _ := res.RowsAffected()
		total += n
		if n < batchSize {
			break
		}
	}
	if total > 0 {
		logx.Info(label+".pruned", "rows", total, "older_than_days", days)
	}
}
