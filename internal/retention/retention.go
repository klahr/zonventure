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

	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/periodic"
)

// Config controls thresholds + cadence. Zero values are treated as
// "feature disabled" so a misconfigured deployment errs on the side
// of keeping data rather than nuking it.
type Config struct {
	// LocationDays is how long after an event completes we keep its
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
// shutdown the same way; both packages share the periodic shell.
func Start(ctx context.Context, db *sql.DB, cfg Config) <-chan struct{} {
	if cfg.Interval <= 0 {
		cfg.Interval = time.Hour
	}
	return periodic.Start(ctx, cfg.Interval, func(ctx context.Context) {
		sweep(ctx, db, cfg)
	})
}

// sweep runs one pass of all configured retention deletes. Each
// statement is bounded to deleting at most batchSize rows so a backlog
// from a long downtime doesn't take a giant write lock. The loop
// re-runs each statement in tight succession until it deletes zero
// rows, then moves to the next.
const batchSize = 5000

func sweep(ctx context.Context, db *sql.DB, cfg Config) {
	if cfg.LocationDays > 0 {
		pruneCompletedEventPings(ctx, db, cfg.LocationDays)
		pruneCompletedEventAnswers(ctx, db, cfg.LocationDays)
		pruneCompletedEventPhotos(ctx, db, cfg.LocationDays)
	}
	if cfg.AuditDays > 0 {
		pruneAuthEvents(ctx, db, cfg.AuditDays)
	}
	// User-deletion grace period is fixed (7 days, set in admin.me),
	// not operator-tunable, so it has no Days field on Config — the
	// timestamp on the row already encodes the deadline.
	pruneScheduledDeletions(ctx, db)
	// Expired TOTP challenges. TTL is 5 minutes, set at IssueChallenge
	// time; ConsumeChallenge deletes on success. This sweep mops up
	// abandoned attempts so the table stays small.
	pruneExpiredTOTPChallenges(ctx, db)
	// Player-action dedup keys: the offline replay queue replays
	// within minutes of reconnecting, so anything older than a week
	// is past any plausible retry. The slot-cascade also clears
	// rows when a slot rotates between events, so this sweep is
	// just for the long-tail.
	prunePlayerActionDedup(ctx, db)
	// Stripe webhook dedupe keys: Stripe redrives for at most ~3
	// days. Anything older than 60 days is dead weight slowing the
	// SELECT-COUNT(*) probe on every redelivery; we keep a buffer
	// past Stripe's window so a clock skew between us and them
	// doesn't accidentally re-fulfil an old event.
	pruneProcessedWebhookEvents(ctx, db)
}

func prunePlayerActionDedup(ctx context.Context, db *sql.DB) {
	stmt := `
		DELETE FROM player_action_dedup
		WHERE created_at < now() - make_interval(days => 7)
		AND ctid IN (
		  SELECT ctid FROM player_action_dedup
		  WHERE created_at < now() - make_interval(days => 7)
		  LIMIT ` + strconv.Itoa(batchSize) + `
		)
	`
	total := int64(0)
	for {
		if ctx.Err() != nil {
			return
		}
		res, err := db.ExecContext(ctx, stmt)
		if err != nil {
			logx.Error("retention.player_action_dedup.failed", "err", err.Error())
			return
		}
		n, _ := res.RowsAffected()
		total += n
		if n < batchSize {
			break
		}
	}
	if total > 0 {
		logx.Info("retention.player_action_dedup.pruned", "rows", total)
	}
}

func pruneProcessedWebhookEvents(ctx context.Context, db *sql.DB) {
	stmt := `
		DELETE FROM processed_webhook_events
		WHERE ctid IN (
		  SELECT ctid FROM processed_webhook_events
		  WHERE received_at::timestamptz < now() - make_interval(days => 60)
		  LIMIT ` + strconv.Itoa(batchSize) + `
		)
	`
	total := int64(0)
	for {
		if ctx.Err() != nil {
			return
		}
		res, err := db.ExecContext(ctx, stmt)
		if err != nil {
			// Older self-host instances may not have a processed_at
			// column; the audit table predates the retention sweep.
			// Log once-per-sweep and move on rather than failing the
			// whole retention pass.
			logx.Warn("retention.processed_webhook_events.failed", "err", err.Error())
			return
		}
		n, _ := res.RowsAffected()
		total += n
		if n < batchSize {
			break
		}
	}
	if total > 0 {
		logx.Info("retention.processed_webhook_events.pruned", "rows", total)
	}
}

func pruneExpiredTOTPChallenges(ctx context.Context, db *sql.DB) {
	stmt := `
		DELETE FROM auth_totp_challenges
		WHERE token IN (
		  SELECT token FROM auth_totp_challenges
		  WHERE expires_at::timestamptz < now()
		  LIMIT ` + strconv.Itoa(batchSize) + `
		)
	`
	total := int64(0)
	for {
		if ctx.Err() != nil {
			return
		}
		res, err := db.ExecContext(ctx, stmt)
		if err != nil {
			logx.Error("retention.totp_challenges.failed", "err", err.Error())
			return
		}
		n, _ := res.RowsAffected()
		total += n
		if n < batchSize {
			break
		}
	}
	if total > 0 {
		logx.Info("retention.totp_challenges.pruned", "rows", total)
	}
}

// COALESCE picks the correct terminal timestamp: completed_at for
// finished events, cancelled_at for ones the arranger killed early.
// The privacy argument is the same in either case — once the event
// is over, raw location pings shouldn't linger past the configured
// window. Active states have NULL on both columns so they're skipped
// implicitly.
func pruneCompletedEventPings(ctx context.Context, db *sql.DB, days int) {
	stmt := `
		DELETE FROM location_pings
		WHERE id IN (
		  SELECT lp.id
		  FROM location_pings lp
		  JOIN events ar ON ar.id = lp.event_id
		  WHERE ar.state IN ('completed','cancelled')
		    AND COALESCE(ar.completed_at, ar.cancelled_at) IS NOT NULL
		    AND COALESCE(ar.completed_at, ar.cancelled_at)::timestamptz < now() - make_interval(days => $1)
		  LIMIT ` + strconv.Itoa(batchSize) + `
		)
	`
	runBoundedDelete(ctx, db, "retention.location_pings", stmt, days)
}

func pruneCompletedEventAnswers(ctx context.Context, db *sql.DB, days int) {
	stmt := `
		DELETE FROM answer_attempts
		WHERE id IN (
		  SELECT aa.id
		  FROM answer_attempts aa
		  JOIN teams t ON t.id = aa.team_id
		  JOIN events ar ON ar.id = t.event_id
		  WHERE ar.state IN ('completed','cancelled')
		    AND COALESCE(ar.completed_at, ar.cancelled_at) IS NOT NULL
		    AND COALESCE(ar.completed_at, ar.cancelled_at)::timestamptz < now() - make_interval(days => $1)
		  LIMIT ` + strconv.Itoa(batchSize) + `
		)
	`
	runBoundedDelete(ctx, db, "retention.answer_attempts", stmt, days)
}

// pruneCompletedEventPhotos deletes the pictures teams took for photo
// blocks, and players' votes, on the same clock as their positions:
// both show who was where, and with whom. The points an approved photo
// earned stay in the score.
func pruneCompletedEventPhotos(ctx context.Context, db *sql.DB, days int) {
	photos := `
		DELETE FROM team_photos
		WHERE id IN (
		  SELECT p.id
		  FROM team_photos p
		  JOIN events ar ON ar.id = p.event_id
		  WHERE ar.state IN ('completed','cancelled')
		    AND COALESCE(ar.completed_at, ar.cancelled_at) IS NOT NULL
		    AND COALESCE(ar.completed_at, ar.cancelled_at)::timestamptz < now() - make_interval(days => $1)
		  LIMIT ` + strconv.Itoa(photoBatchSize) + `
		)
	`
	runBoundedDelete(ctx, db, "retention.team_photos", photos, days)
	votes := `
		DELETE FROM team_block_votes
		WHERE ctid IN (
		  SELECT v.ctid
		  FROM team_block_votes v
		  JOIN teams t ON t.id = v.team_id
		  JOIN events ar ON ar.id = t.event_id
		  WHERE ar.state IN ('completed','cancelled')
		    AND COALESCE(ar.completed_at, ar.cancelled_at) IS NOT NULL
		    AND COALESCE(ar.completed_at, ar.cancelled_at)::timestamptz < now() - make_interval(days => $1)
		  LIMIT ` + strconv.Itoa(batchSize) + `
		)
	`
	runBoundedDelete(ctx, db, "retention.team_block_votes", votes, days)
}

// photoBatchSize is smaller than batchSize: each row is a picture, and a
// batch is held in one statement.
const photoBatchSize = 200

// pruneScheduledDeletions hard-deletes users whose grace window has
// passed. Each user is processed in its own transaction so a single
// org-cascade error doesn't poison the rest of the batch. After
// deleting a user we collapse any org they were the last member of —
// org_memberships cascades the membership rows away on user delete,
// so we read them up front to know which orgs to re-check.
func pruneScheduledDeletions(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, username
		FROM users
		WHERE scheduled_deletion_at IS NOT NULL
		  AND scheduled_deletion_at <= now()
		LIMIT `+strconv.Itoa(batchSize))
	if err != nil {
		logx.Error("retention.users.query_failed", "err", err.Error())
		return
	}
	type doomed struct {
		id       int64
		username string
	}
	var batch []doomed
	for rows.Next() {
		var d doomed
		if err := rows.Scan(&d.id, &d.username); err != nil {
			logx.Error("retention.users.scan_failed", "err", err.Error())
			rows.Close()
			return
		}
		batch = append(batch, d)
	}
	rows.Close()
	for _, d := range batch {
		if ctx.Err() != nil {
			return
		}
		hardDeleteUser(ctx, db, d.id, d.username)
	}
}

func hardDeleteUser(ctx context.Context, db *sql.DB, userID int64, username string) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		logx.Error("retention.users.tx_begin_failed", "user_id", userID, "err", err.Error())
		return
	}
	defer tx.Rollback() //nolint:errcheck
	orgRows, err := tx.QueryContext(ctx,
		`SELECT org_id FROM org_memberships WHERE user_id = $1`, userID)
	if err != nil {
		logx.Error("retention.users.org_lookup_failed", "user_id", userID, "err", err.Error())
		return
	}
	var orgIDs []int64
	for orgRows.Next() {
		var id int64
		if err := orgRows.Scan(&id); err != nil {
			logx.Error("retention.users.org_scan_failed", "user_id", userID, "err", err.Error())
			orgRows.Close()
			return
		}
		orgIDs = append(orgIDs, id)
	}
	orgRows.Close()
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		logx.Error("retention.users.delete_failed", "user_id", userID, "err", err.Error())
		return
	}
	for _, orgID := range orgIDs {
		var remaining int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM org_memberships WHERE org_id = $1`, orgID).Scan(&remaining); err != nil {
			logx.Error("retention.users.org_count_failed", "user_id", userID, "err", err.Error())
			return
		}
		if remaining == 0 {
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM organizations WHERE id = $1`, orgID); err != nil {
				logx.Error("retention.users.org_cascade_failed", "user_id", userID, "err", err.Error())
				return
			}
			logx.Info("retention.users.org_collapsed", "org_id", orgID)
		}
	}
	if err := tx.Commit(); err != nil {
		logx.Error("retention.users.commit_failed", "user_id", userID, "err", err.Error())
		return
	}
	logx.Info("retention.users.deleted", "user_id", userID, "username", username)
}

func pruneAuthEvents(ctx context.Context, db *sql.DB, days int) {
	// auth_events.ts is also a TEXT timestamp produced by now_text();
	// cast to timestamptz so the comparison works.
	stmt := `
		DELETE FROM auth_events
		WHERE id IN (
		  SELECT id FROM auth_events
		  WHERE ts::timestamptz < now() - make_interval(days => $1)
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
