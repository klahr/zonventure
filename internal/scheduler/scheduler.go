// Package scheduler runs a periodic sweep that auto-starts any adventure
// run whose scheduled_at time has arrived. The intent is pragmatic: an
// arranger books an event to flip to "running" at, say, 14:00 on Saturday, and
// the sweep promotes it within the sweep interval of the target time.
package scheduler

import (
	"context"
	"database/sql"
	"time"

	"git.sr.ht/~klahr/zv/internal/activity"
	"git.sr.ht/~klahr/zv/internal/logx"
	"git.sr.ht/~klahr/zv/internal/periodic"
	"git.sr.ht/~klahr/zv/internal/ws"
)

// Start launches the sweep loop in its own goroutine. Stop it by cancelling
// the supplied context. The returned channel is closed once the loop has
// fully exited — main.go waits on it during shutdown so the process doesn't
// race the in-flight tick. hub may be nil (e.g. in tests).
func Start(ctx context.Context, db *sql.DB, hub *ws.Hub, interval time.Duration) <-chan struct{} {
	return periodic.Start(ctx, interval, func(ctx context.Context) {
		tick(ctx, db, hub)
	})
}

// tick finds and promotes all scheduled runs whose time has come.
//
// scheduled_at can be either RFC3339 (from the JS Date input) or our own
// "YYYY-MM-DD HH:MM:SS" wire format; casting through timestamptz parses
// both so we don't have to care what the writer chose.
func tick(ctx context.Context, db *sql.DB, hub *ws.Hub) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, COALESCE(created_by_user_id, 0)
		FROM events
		WHERE state = 'scheduled'
		  AND scheduled_at IS NOT NULL
		  AND scheduled_at::timestamptz <= now()
	`)
	if err != nil {
		logx.Error("scheduler.query", "err", err)
		return
	}
	type due struct{ id, actor int64 }
	var runs []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.actor); err != nil {
			rows.Close()
			logx.Error("scheduler.scan", "err", err)
			return
		}
		runs = append(runs, d)
	}
	rows.Close()
	for _, d := range runs {
		res, err := db.ExecContext(ctx, `
			UPDATE events
			SET state = 'running', started_at = now_text()
			WHERE id = $1 AND state = 'scheduled'
		`, d.id)
		if err != nil {
			logx.Error("scheduler.start", "event_id", d.id, "err", err)
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // lost the race to a manual start — that's fine
		}
		var actor activity.Actor
		if d.actor > 0 {
			actor = activity.UserActor(d.actor)
		}
		activity.Record(ctx, db, d.id, nil, actor, activity.EventStarted,
			map[string]any{"auto": true})
		hub.Publish(ws.EventTopic(d.id), "changed", nil)
		logx.Info("scheduler.auto_started", "event_id", d.id)
	}
}
