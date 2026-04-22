// Package scheduler runs a periodic sweep that auto-starts any adventure
// run whose scheduled_at time has arrived. The intent is pragmatic: an
// arranger books a run to flip to "running" at, say, 14:00 on Saturday, and
// the sweep promotes it within the sweep interval of the target time.
package scheduler

import (
	"context"
	"database/sql"
	"log"
	"time"

	"git.sr.ht/~klahr/zv/internal/runevents"
	"git.sr.ht/~klahr/zv/internal/ws"
)

// Start launches the sweep loop in its own goroutine. Stop it by cancelling
// the supplied context. hub may be nil (e.g. in tests).
func Start(ctx context.Context, db *sql.DB, hub *ws.Hub, interval time.Duration) {
	go loop(ctx, db, hub, interval)
}

func loop(ctx context.Context, db *sql.DB, hub *ws.Hub, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	tick(ctx, db, hub) // run one pass immediately on startup
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick(ctx, db, hub)
		}
	}
}

// tick finds and promotes all scheduled runs whose time has come.
//
// Dates are compared through sqlite's datetime() normaliser, which parses
// both RFC3339 strings and sqlite's own "YYYY-MM-DD HH:MM:SS" format so we
// don't have to care what the writer chose.
func tick(ctx context.Context, db *sql.DB, hub *ws.Hub) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, COALESCE(created_by_user_id, 0)
		FROM adventure_runs
		WHERE state = 'scheduled'
		  AND scheduled_at IS NOT NULL
		  AND datetime(scheduled_at) <= datetime('now')
	`)
	if err != nil {
		log.Printf("scheduler: query: %v", err)
		return
	}
	type due struct{ id, actor int64 }
	var runs []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.actor); err != nil {
			rows.Close()
			log.Printf("scheduler: scan: %v", err)
			return
		}
		runs = append(runs, d)
	}
	rows.Close()
	for _, d := range runs {
		res, err := db.ExecContext(ctx, `
			UPDATE adventure_runs
			SET state = 'running', started_at = datetime('now')
			WHERE id = ? AND state = 'scheduled'
		`, d.id)
		if err != nil {
			log.Printf("scheduler: start run %d: %v", d.id, err)
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // lost the race to a manual start — that's fine
		}
		var actor runevents.Actor
		if d.actor > 0 {
			actor = runevents.UserActor(d.actor)
		}
		runevents.Record(ctx, db, d.id, nil, actor, runevents.RunStarted,
			map[string]any{"auto": true})
		hub.Publish(ws.RunTopic(d.id), "changed", nil)
		log.Printf("scheduler: auto-started run #%d", d.id)
	}
}
