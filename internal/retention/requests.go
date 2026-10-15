package retention

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/klahr/zonventure/internal/logx"
)

// requestDays is how long a request for a quote (org_requests) is kept
// after its last change: a year, whatever came of it.
const requestDays = 365

// pruneOrgRequests deletes requests for a quote untouched for a year.
func pruneOrgRequests(ctx context.Context, db *sql.DB) {
	stmt := `
		DELETE FROM org_requests
		WHERE id IN (
		  SELECT id FROM org_requests
		  WHERE updated_at < now() - make_interval(days => ` + strconv.Itoa(requestDays) + `)
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
			logx.Error("retention.org_requests.failed", "err", err.Error())
			return
		}
		n, _ := res.RowsAffected()
		total += n
		if n < batchSize {
			break
		}
	}
	if total > 0 {
		logx.Info("retention.org_requests.pruned", "rows", total)
	}
}
