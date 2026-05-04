package admin

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
)

// auditEventDTO is one row of the auth_events table as the UI sees it.
// Booleans replace the schema's INTEGER 0/1 so the JSON shape is
// idiomatic; the column stays INTEGER to match the rest of the schema.
type auditEventDTO struct {
	ID        int64  `json:"id"`
	TS        string `json:"ts"`
	Event     string `json:"event"`
	OK        bool   `json:"ok"`
	UserID    *int64 `json:"user_id,omitempty"`
	Username  string `json:"username,omitempty"`
	IP        string `json:"ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// ListAuthEvents returns the most recent auth_events rows the caller
// is allowed to see, newest first.
//
//   - Global admin: every row.
//   - Org admin: rows whose user is in the caller's org. Rows with no
//     user (failed logins for unknown usernames) stay admin-only since
//     they have no org affinity.
//   - Anything else: 403.
//
// Pagination uses an opaque cursor on the descending id stream:
// pass ?before_id=<last_seen_id> to fetch the next page.
func (h *Handlers) ListAuthEvents(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleAdmin && me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	limit := 100
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	var beforeID int64
	if q := r.URL.Query().Get("before_id"); q != "" {
		if n, err := strconv.ParseInt(q, 10, 64); err == nil && n > 0 {
			beforeID = n
		}
	}

	query := `
		SELECT e.id, e.ts, e.event, e.ok, e.user_id,
		       COALESCE(e.username,''), COALESCE(e.ip,''),
		       COALESCE(e.user_agent,''), COALESCE(e.reason,'')
		FROM auth_events e
	`
	args := []any{}
	idx := 1
	where := ""
	add := func(clause string, vals ...any) {
		if where == "" {
			where = " WHERE "
		} else {
			where += " AND "
		}
		where += clause
		args = append(args, vals...)
	}
	if me.Role == auth.RoleOrgAdmin && me.OrgID != nil {
		// Org-scoped view: caller sees only events for users who
		// have a membership in the caller's currently-active org.
		add("e.user_id IS NOT NULL AND EXISTS (SELECT 1 FROM org_memberships m WHERE m.user_id = e.user_id AND m.org_id = $"+strconv.Itoa(idx)+")", *me.OrgID)
		idx++
	}
	if beforeID > 0 {
		add("e.id < $"+strconv.Itoa(idx), beforeID)
		idx++
	}
	// Optional filters. Bind through $-placeholders so anything in the
	// query string lands as a SQL value, never as SQL.
	if ev := strings.TrimSpace(r.URL.Query().Get("event")); ev != "" {
		add("e.event = $"+strconv.Itoa(idx), ev)
		idx++
	}
	if okq := r.URL.Query().Get("ok"); okq == "1" || okq == "true" {
		add("e.ok = 1")
	} else if okq == "0" || okq == "false" {
		add("e.ok = 0")
	}
	if u := strings.TrimSpace(r.URL.Query().Get("username")); u != "" {
		add("e.username ILIKE $"+strconv.Itoa(idx), "%"+u+"%")
		idx++
	}
	if ip := strings.TrimSpace(r.URL.Query().Get("ip")); ip != "" {
		add("e.ip ILIKE $"+strconv.Itoa(idx), "%"+ip+"%")
		idx++
	}
	query += where + " ORDER BY e.id DESC LIMIT $" + strconv.Itoa(idx)
	args = append(args, limit)

	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "list failed", err)
		return
	}
	defer rows.Close()
	out := []auditEventDTO{}
	for rows.Next() {
		var (
			e      auditEventDTO
			userID sql.NullInt64
			ok     int
		)
		if err := rows.Scan(&e.ID, &e.TS, &e.Event, &ok, &userID,
			&e.Username, &e.IP, &e.UserAgent, &e.Reason); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "scan failed", err)
			return
		}
		e.OK = ok != 0
		if userID.Valid {
			v := userID.Int64
			e.UserID = &v
		}
		out = append(out, e)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"events": out})
}

// ListMyAuthEvents returns just the caller's own auth_events. Plain
// users (org_member, not just admins) get to see when their account
// was logged into, password changed, email changed — basic security
// transparency that doesn't require a global Audit gate. No
// pagination; cap at the most recent 25 events to keep the settings
// card glanceable.
func (h *Handlers) ListMyAuthEvents(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, ts, event, ok, user_id,
		       COALESCE(username,''), COALESCE(ip,''),
		       COALESCE(user_agent,''), COALESCE(reason,'')
		FROM auth_events
		WHERE user_id = $1
		ORDER BY id DESC
		LIMIT 25
	`, me.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []auditEventDTO{}
	for rows.Next() {
		var (
			e      auditEventDTO
			userID sql.NullInt64
			ok     int
		)
		if err := rows.Scan(&e.ID, &e.TS, &e.Event, &ok, &userID,
			&e.Username, &e.IP, &e.UserAgent, &e.Reason); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		e.OK = ok != 0
		if userID.Valid {
			v := userID.Int64
			e.UserID = &v
		}
		out = append(out, e)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"events": out})
}
