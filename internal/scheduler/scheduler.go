// Package scheduler runs a periodic sweep that auto-starts any adventure
// run whose scheduled_at time has arrived. The intent is pragmatic: an
// arranger books an event to flip to "running" at, say, 14:00 on Saturday, and
// the sweep promotes it within the sweep interval of the target time.
package scheduler

import (
	"context"
	"database/sql"
	"time"

	"github.com/klahr/zonventure/internal/activity"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/periodic"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/ws"
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
// scheduled_at is stored as UTC "YYYY-MM-DD HH:MM:SS" with no offset, so
// it is read as UTC explicitly rather than in the session's TimeZone.
//
// An event whose adventure has nothing published stays scheduled, as the
// admin start refuses it; the arranger gets one graph.start.failed row.
func tick(ctx context.Context, db *sql.DB, hub *ws.Hub) {
	rows, err := db.QueryContext(ctx, `
		SELECT e.id, COALESCE(e.created_by_user_id, 0),
		       e.graph_id IS NOT NULL OR a.published_graph_id IS NOT NULL
		FROM events e
		LEFT JOIN adventures a ON a.slug = e.adventure_slug
		WHERE e.state = 'scheduled'
		  AND e.scheduled_at IS NOT NULL
		  AND (e.scheduled_at::timestamp AT TIME ZONE 'UTC') <= now()
	`)
	if err != nil {
		logx.Error("scheduler.query", "err", err)
		return
	}
	type due struct {
		id, actor int64
		published bool
	}
	var runs []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.actor, &d.published); err != nil {
			rows.Close()
			logx.Error("scheduler.scan", "err", err)
			return
		}
		runs = append(runs, d)
	}
	rows.Close()
	for _, d := range runs {
		if !d.published {
			unstartable(ctx, db, d.id)
			continue
		}
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
		if err := play.Start(ctx, db, d.id); err != nil {
			logx.Error("scheduler.graph_start", "event_id", d.id, "err", err)
			activity.Record(ctx, db, d.id, nil, activity.Actor{},
				"graph.start.failed", map[string]any{"error": err.Error(), "auto": true})
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

// unstartable records, once per event, that a due event could not start
// because its adventure has no published graph. The sweep sees the event
// again every tick until it is published or rescheduled.
func unstartable(ctx context.Context, db *sql.DB, eventID int64) {
	var seen bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM event_activity WHERE event_id = $1 AND kind = 'graph.start.failed')
	`, eventID).Scan(&seen); err != nil || seen {
		return
	}
	activity.Record(ctx, db, eventID, nil, activity.Actor{},
		"graph.start.failed", map[string]any{"error": play.ErrNoGraph.Error(), "auto": true})
	logx.Error("scheduler.graph_start", "event_id", eventID, "err", play.ErrNoGraph)
}
