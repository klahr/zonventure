package retention

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/klahr/zonventure/internal/logx"
)

// partyBatchSize bounds one pass: deleting an event cascades through its
// teams, slots, pings, answers and photos, so a batch is many rows.
const partyBatchSize = 50

// prunePartyEvents deletes parties whose time is up, whole: the event and
// everything about its players, and the party's own graph. A party is
// kept days after it ended, for its results page; one never started goes
// when its valid_until passes; one started but never ended goes days
// after it started. The ticket purchase stays for bookkeeping, its email
// blanked by the ticket_purchases_anonymise trigger.
func prunePartyEvents(ctx context.Context, db *sql.DB, days int) {
	stmt := `
		WITH gone AS (
		  DELETE FROM events
		  WHERE id IN (
		    SELECT id FROM events
		    WHERE party AND (
		      (state IN ('completed','cancelled')
		        AND COALESCE(completed_at, cancelled_at)::timestamp AT TIME ZONE 'UTC' < now() - make_interval(days => $1))
		      OR (state = 'scheduled' AND valid_until < now())
		      OR (state IN ('running','paused')
		        AND started_at::timestamp AT TIME ZONE 'UTC' < now() - make_interval(days => $1))
		    )
		    LIMIT ` + strconv.Itoa(partyBatchSize) + `
		  )
		  RETURNING graph_id
		), graphs AS (
		  DELETE FROM adventure_graphs g USING gone
		  WHERE g.id = gone.graph_id AND g.version < 0
		)
		SELECT COUNT(*) FROM gone
	`
	total := 0
	for ctx.Err() == nil {
		var n int
		if err := db.QueryRowContext(ctx, stmt, days).Scan(&n); err != nil {
			logx.Error("retention.party_events.failed", "err", err.Error())
			return
		}
		total += n
		if n < partyBatchSize {
			break
		}
	}
	if total > 0 {
		logx.Info("retention.party_events.pruned", "events", total)
	}
}
