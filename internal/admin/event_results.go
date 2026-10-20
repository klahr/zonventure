package admin

// The event's tamper-evident results record (internal/results), checked.
//
//	GET /api/admin/events/{eventID}/results/verify   the event's organisers, platform admins

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/results"
)

// VerifyEventResults recomputes an event's results record: every
// entry's hash and link, and each team's result now against the one
// recorded. Its fingerprint is what the event's teams' receipts lead to.
func (h *Handlers) VerifyEventResults(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if me, _ := auth.FromContext(r.Context()); !me.IsAdmin {
		if _, ok := h.authorizeEvent(w, r, eventID); !ok {
			return
		}
	}
	rep, err := results.Verify(r.Context(), h.DB, eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "results check failed", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, rep)
}

// resultsAnchorWindow is how long after an event ends its fingerprint
// is still mailed: enough to ride out an outage, short of mailing every
// event that ended before there were fingerprints to mail.
const resultsAnchorWindow = 7 * 24 * time.Hour

// AnchorEventResults mails each event's results fingerprint to its
// organisation's admins once the event is over, with the check of its
// record then: run it periodically. A copy outside the database makes
// even a record rewritten whole show, when its fingerprint no longer
// matches the mail. An event no team played in has nothing to keep.
func (h *Handlers) AnchorEventResults(ctx context.Context) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT DISTINCT l.event_id, e.org_id, e.adventure_slug, COALESCE(e.name, '')
		FROM event_result_log l JOIN events e ON e.id = l.event_id
		WHERE l.kind IN ($1, $2) AND l.server_time > $3
		  AND NOT EXISTS (SELECT 1 FROM event_result_anchors a WHERE a.event_id = l.event_id)
		  AND EXISTS (SELECT 1 FROM event_result_log t WHERE t.event_id = l.event_id AND t.team_id IS NOT NULL)`,
		results.EventCompleted, results.EventCancelled, time.Now().Add(-resultsAnchorWindow))
	if err != nil {
		logx.Warn("admin.results.anchor", "err", err.Error())
		return
	}
	type sealed struct {
		eventID, orgID int64
		slug, name     string
	}
	var due []sealed
	for rows.Next() {
		var s sealed
		if rows.Scan(&s.eventID, &s.orgID, &s.slug, &s.name) == nil {
			due = append(due, s)
		}
	}
	rows.Close()
	for _, s := range due {
		if s.name == "" {
			s.name = h.adventureTitle(s.slug)
		}
		h.anchorEventResults(ctx, s.eventID, s.orgID, s.name)
	}
}

func (h *Handlers) anchorEventResults(ctx context.Context, eventID, orgID int64, name string) {
	rep, err := results.Verify(ctx, h.DB, eventID)
	if err != nil || !rep.Sealed {
		if err != nil {
			logx.Warn("admin.results.anchor", "event_id", eventID, "err", err.Error())
		}
		return
	}
	var ended time.Time
	_ = h.DB.QueryRowContext(ctx, `SELECT server_time FROM event_result_log WHERE entry_hash = $1`, rep.Fingerprint).Scan(&ended)
	to, err := h.emailsOf(ctx, `
		SELECT u.email FROM org_memberships m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND m.role = 'org_admin' AND COALESCE(u.email, '') <> ''`, orgID)
	if err != nil {
		logx.Warn("admin.results.anchor", "event_id", eventID, "err", err.Error())
		return
	}
	if len(to) > 0 {
		l := h.orgLook(ctx, orgID)
		subject, c := emailtpl.EventResultsSealed(l.Lang, name, emailtpl.When(l.Lang, ended, l.TimeZone),
			rep.Fingerprint, rep.Entries, len(rep.Problems), h.arrangeLink(fmt.Sprintf("#/events/%d", eventID)))
		if err := h.send(ctx, "event_results_sealed", l.message(to, subject, c)); err != nil {
			logx.Warn("admin.results.anchor", "event_id", eventID, "err", err.Error())
			return
		}
	}
	if _, err := h.DB.ExecContext(ctx, `
		INSERT INTO event_result_anchors (event_id, fingerprint, entries, problems) VALUES ($1, $2, $3, $4)
		ON CONFLICT (event_id) DO NOTHING`, eventID, rep.Fingerprint, rep.Entries, len(rep.Problems)); err != nil {
		logx.Warn("admin.results.anchor", "event_id", eventID, "err", err.Error())
	}
}
