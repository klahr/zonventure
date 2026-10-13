package admin

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
)

// actionRecorder captures the status a handler wrote.
type actionRecorder struct {
	http.ResponseWriter
	status int
}

func (r *actionRecorder) WriteHeader(s int) {
	if r.status == 0 {
		r.status = s
	}
	r.ResponseWriter.WriteHeader(s)
}

func (r *actionRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

type actionNoteKey struct{}

// actionNote is what a handler adds to its logged action.
type actionNote struct{ note, target string }

// noteAction qualifies the logged action of a route that does more than
// one thing, so un-marking a purchase billed doesn't read as marking it.
func noteAction(r *http.Request, note string) {
	if p, ok := r.Context().Value(actionNoteKey{}).(*actionNote); ok {
		p.note = note
	}
}

// noteTarget names the logged action's target when the route's ids
// don't: a user or organisation named in the body.
func noteTarget(r *http.Request, name string) {
	if p, ok := r.Context().Value(actionNoteKey{}).(*actionNote); ok {
		p.target = name
	}
}

// onOff is a switch route's note: which way it was flipped.
func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// targetNames turns a route's ids into the names an auditor knows them
// by. Keyed by URL parameter; each query takes the parameter's value.
var targetNames = map[string]string{
	"orgID":   `SELECT name FROM organizations WHERE id = $1`,
	"userID":  `SELECT username FROM users WHERE id = $1`,
	"eventID": `SELECT COALESCE(NULLIF(name, ''), party_name, '') FROM events WHERE id = $1`,
	"teamID":  `SELECT name FROM teams WHERE id = $1`,
	"slug":    `SELECT COALESCE(NULLIF(title, ''), slug) FROM adventures WHERE slug = $1`,
	"purchaseID": `SELECT COALESCE(o.name || ' · ', '') || COALESCE(NULLIF(a.title, ''), p.adventure_slug)
		FROM purchases p LEFT JOIN organizations o ON o.id = p.org_id LEFT JOIN adventures a ON a.slug = p.adventure_slug
		WHERE p.id = $1`,
}

// listingNames name a listing under review, by its kind.
var listingNames = map[string]string{
	listingEvent: `SELECT name FROM events WHERE id = $1`,
	listingParty: `SELECT o.name || ' · ' || COALESCE(NULLIF(a.title, ''), po.adventure_slug)
		FROM party_offers po JOIN organizations o ON o.id = po.org_id LEFT JOIN adventures a ON a.slug = po.adventure_slug
		WHERE po.id = $1`,
	listingOrg: `SELECT g.name FROM presentation_proposals p JOIN organizations g ON g.id = p.org_id WHERE p.id = $1`,
	listingAdventure: `SELECT g.name || COALESCE(' · ' || COALESCE(NULLIF(a.title, ''), a.slug), '')
		FROM presentation_proposals p JOIN organizations g ON g.id = p.org_id LEFT JOIN adventures a ON a.id = p.target_id
		WHERE p.id = $1`,
}

// ticketPurchaseName names a ticket sale by its event.
const ticketPurchaseName = `SELECT e.name FROM ticket_purchases tp JOIN events e ON e.id = tp.event_id WHERE tp.id = $1`

// targetName names what the request acts on, from its route's ids in
// path order ("Acme · Summer quest"). Run before the handler, so a
// deletion still names what it deleted. Ids it can't name are left out.
func targetName(ctx context.Context, db *sql.DB, r *http.Request) string {
	rc := chi.RouteContext(ctx)
	if rc == nil || rc.Routes == nil {
		return ""
	}
	m := chi.NewRouteContext()
	if !rc.Routes.Match(m, r.Method, r.URL.Path) {
		return ""
	}
	var names []string
	for i, k := range m.URLParams.Keys {
		v := m.URLParams.Values[i]
		q := targetNames[k]
		switch {
		case k == "id":
			q = listingNames[m.URLParam("kind")]
		case k == "purchaseID" && strings.Contains(m.RoutePattern(), "/ticket-purchases/"):
			q = ticketPurchaseName
		}
		if q == "" {
			continue
		}
		var arg any = v
		if k != "slug" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				continue
			}
			arg = n
		}
		var name string
		if db.QueryRowContext(ctx, q, arg).Scan(&name) == nil && name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, " · ")
}

// LogAdminActions records every successful state-changing request in
// admin_actions: who, which route, the path with its ids and what those
// ids name (see targetName), plus a handler's note (on or off). Mounted
// on the whole /api/admin tree, so flags, refunds, voids, pricing,
// members and deletions show under Settings → Audit. A user's changes
// to their own account (/me) are in the auth log instead. Bodies are
// never stored. Only global admins' (full or Support) requests are
// recorded.
func LogAdminActions(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			var note actionNote
			r = r.WithContext(context.WithValue(r.Context(), actionNoteKey{}, &note))
			var name string
			if me, _ := auth.FromContext(r.Context()); readsAsAdmin(me) {
				name = targetName(r.Context(), db, r)
			}
			rec := &actionRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 || rec.status >= 400 {
				return
			}
			me, _ := auth.FromContext(r.Context())
			if !readsAsAdmin(me) {
				return
			}
			pattern := r.URL.Path
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				pattern = rc.RoutePattern()
			}
			if pattern == "/api/admin/me" || strings.HasPrefix(pattern, "/api/admin/me/") {
				return
			}
			action := r.Method + " " + pattern
			if note.note != "" {
				action += " (" + note.note + ")"
			}
			if note.target != "" {
				name = note.target
			}
			var userID any
			if me.ID != 0 {
				userID = me.ID
			}
			if _, err := db.ExecContext(r.Context(),
				`INSERT INTO admin_actions (user_id, username, action, target, target_name) VALUES ($1, $2, $3, $4, $5)`,
				userID, me.Username, action, r.URL.Path, name); err != nil {
				logx.Warn("admin.actions.insert", "action", action, "err", err.Error())
			}
		})
	}
}

type adminActionDTO struct {
	ID       int64  `json:"id"`
	TS       string `json:"ts"`
	Username string `json:"username,omitempty"`
	Action   string `json:"action"`
	Target   string `json:"target"`
	// TargetName is what the route's ids named when it ran ("Acme"),
	// empty on rows from before it was recorded.
	TargetName string `json:"target_name,omitempty"`
}

// ListAdminActions returns the newest admin_actions rows. Admin-only;
// ?before_id= pages back like ListAuthEvents, and ?username= keeps the
// rows of admins whose name contains it, as the auth events' filter.
func (h *Handlers) ListAdminActions(w http.ResponseWriter, r *http.Request) {
	var beforeID int64
	if q := r.URL.Query().Get("before_id"); q != "" {
		if n, err := strconv.ParseInt(q, 10, 64); err == nil && n > 0 {
			beforeID = n
		}
	}
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, ts, username, action, target, target_name
		FROM admin_actions
		WHERE ($1 = 0 OR id < $1) AND ($2 = '' OR username ILIKE '%' || $2 || '%')
		ORDER BY id DESC
		LIMIT 100
	`, beforeID, username)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "list failed", err)
		return
	}
	defer rows.Close()
	out := []adminActionDTO{}
	for rows.Next() {
		var a adminActionDTO
		if err := rows.Scan(&a.ID, &a.TS, &a.Username, &a.Action, &a.Target, &a.TargetName); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "scan failed", err)
			return
		}
		out = append(out, a)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"actions": out})
}
