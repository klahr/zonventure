package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"github.com/go-chi/chi/v5"
)

/* ---------------- User typeahead search ---------------- */

type userSearchMatch struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// SearchUsers powers the typeahead in the "Add user to org" modal.
// Returns up to 10 username matches for a prefix query, excluding
// global admins (they don't fit the per-org role model) and
// optionally users who are already members of a target org. Auth:
// admin or org_admin, mirroring CreateOrgMember's envelope.
//
// Username is a CITEXT column so LIKE is case-insensitive without
// extra ILIKE / LOWER() decoration. The pattern is ESCAPE-quoted so
// a query like "10%_off" doesn't accidentally turn into a wildcard
// fishing expedition.
func (h *Handlers) SearchUsers(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleAdmin && me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	// Minimum length 2 — nudges callers toward intentional lookups
	// rather than slow-typing a wildcard scan of the user table.
	if len(q) < 2 {
		httpx.JSON(w, http.StatusOK, map[string]any{"users": []userSearchMatch{}})
		return
	}
	escape := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)
	pattern := escape.Replace(q) + "%"

	var orgID int64
	if v := r.URL.Query().Get("org_id"); v != "" {
		orgID, _ = strconv.ParseInt(v, 10, 64)
	}

	var (
		rows *sql.Rows
		err  error
	)
	if orgID > 0 {
		rows, err = h.DB.QueryContext(r.Context(), `
			SELECT u.id, u.username FROM users u
			WHERE u.is_admin = FALSE
			  AND u.username LIKE $1 ESCAPE '\'
			  AND NOT EXISTS (
			    SELECT 1 FROM org_memberships m WHERE m.user_id = u.id AND m.org_id = $2
			  )
			ORDER BY LOWER(u.username) LIMIT 10
		`, pattern, orgID)
	} else {
		rows, err = h.DB.QueryContext(r.Context(), `
			SELECT id, username FROM users
			WHERE is_admin = FALSE AND username LIKE $1 ESCAPE '\'
			ORDER BY LOWER(username) LIMIT 10
		`, pattern)
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "search failed")
		return
	}
	defer rows.Close()
	out := []userSearchMatch{}
	for rows.Next() {
		var u userSearchMatch
		if err := rows.Scan(&u.ID, &u.Username); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		out = append(out, u)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"users": out})
}

/* ---------------- Confirm-password gating ---------------- */

const confirmHeader = "X-Confirm-Password"

func requirePasswordConfirm(w http.ResponseWriter, r *http.Request, db *sql.DB) bool {
	me, _ := auth.FromContext(r.Context())
	pw := r.Header.Get(confirmHeader)
	if pw == "" {
		httpx.Error(w, http.StatusUnauthorized, "password confirmation required")
		return false
	}
	if err := auth.VerifyUserPassword(r.Context(), db, me.ID, pw); err != nil {
		httpx.Error(w, http.StatusUnauthorized, "wrong password")
		return false
	}
	return true
}

/* ---------------- Adventure & run detail ---------------- */

// AdventureDetail returns the full page list for an adventure. All
// authenticated admin-surface users can see it.
func (h *Handlers) AdventureDetail(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	adv := h.Lib.Get(slug)
	if adv == nil {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	type locDTO struct {
		Lat     float64  `json:"lat"`
		Lng     float64  `json:"lng"`
		RadiusM *float64 `json:"radius_m,omitempty"`
	}
	type pageDTO struct {
		Index    int                     `json:"index"`
		Title    string                  `json:"title"`
		Prompt   string                  `json:"prompt"`
		Accept   []adventure.AcceptEntry `json:"accept"`
		Location locDTO                  `json:"location"`
		Gated    bool                    `json:"gated"`
	}
	pages := make([]pageDTO, len(adv.Pages))
	for i, p := range adv.Pages {
		pages[i] = pageDTO{
			Index: p.Index, Title: p.Title, Prompt: p.Prompt, Accept: p.Accept,
			Location: locDTO{Lat: p.Location.Lat, Lng: p.Location.Lng, RadiusM: p.Location.RadiusM},
			Gated:    p.Location.Gated(),
		}
	}
	out := map[string]any{
		"slug":        adv.Slug,
		"title":       adv.Meta.Title,
		"description": adv.Meta.Description,
		"author":      adv.Meta.Author,
		"page_count":  len(adv.Pages),
		"pages":       pages,
	}
	if !adv.Colors.Empty() {
		out["colors"] = adv.Colors
	}
	// Tack on the cover picture URL if one is stored, and the publish
	// state. Lib.Get doesn't carry either, so hit the DB with a tiny probe.
	var (
		hasAvatar int
		published int
	)
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT avatar_mime_type IS NOT NULL, published FROM adventures WHERE slug = $1`,
		slug).Scan(&hasAvatar, &published)
	if hasAvatar != 0 {
		out["avatar_url"] = adventureAvatarURL(slug)
	}
	out["published"] = published != 0
	httpx.JSON(w, http.StatusOK, out)
}

// EventDetail returns an event with its teams and slots (players).
func (h *Handlers) EventDetail(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	var (
		d       eventDTO
		showMap int
	)
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT ar.id, ar.name, ar.adventure_slug, ar.state,
		       COALESCE(ar.scheduled_at,''), COALESCE(ar.started_at,''),
		       ar.org_id, COALESCE(ar.created_by_user_id, 0),
		       COALESCE(ar.assignment_id, 0), COALESCE(aa.max_teams, 0),
		       ar.live_token, ar.show_map
		FROM events ar
		LEFT JOIN adventure_assignments aa ON aa.id = ar.assignment_id
		WHERE ar.id = $1
	`, eventID).Scan(&d.ID, &d.Name, &d.AdventureSlug, &d.State, &d.ScheduledAt, &d.StartedAt,
		&d.OrgID, &d.CreatedByUserID, &d.AssignmentID, &d.MaxTeams,
		&d.LiveToken, &showMap); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	d.ShowMap = showMap != 0
	d.AllowedOps = allowedOpsForState(d.State)

	type slotOut struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Token       string `json:"token"`
		LastLoginAt string `json:"last_login_at,omitempty"`
	}
	type teamOut struct {
		ID          int64     `json:"id"`
		Name        string    `json:"name"`
		PageIndex   int       `json:"page_index"`
		CompletedAt string    `json:"completed_at,omitempty"`
		Token       string    `json:"token"`
		AvatarURL   string    `json:"avatar_url,omitempty"`
		Members     []slotOut `json:"members"`
	}
	teams := []teamOut{}
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT t.id, t.name, t.current_page_index, COALESCE(t.completed_at, ''), t.token,
		       (t.avatar_mime_type IS NOT NULL)::int AS has_avatar
		FROM teams t WHERE t.event_id = $1 ORDER BY t.name
	`, eventID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "teams failed")
		return
	}
	defer rows.Close()
	for rows.Next() {
		t := teamOut{Members: []slotOut{}}
		var hasAvatar int
		if err := rows.Scan(&t.ID, &t.Name, &t.PageIndex, &t.CompletedAt, &t.Token, &hasAvatar); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "team scan failed")
			return
		}
		if hasAvatar != 0 {
			t.AvatarURL = teamAvatarURL(t.ID)
		}
		teams = append(teams, t)
	}
	for i := range teams {
		mrows, err := h.DB.QueryContext(r.Context(), `
			SELECT id, name, token, COALESCE(last_login_at, '')
			FROM player_slots WHERE team_id = $1 ORDER BY LOWER(name)
		`, teams[i].ID)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "members failed")
			return
		}
		for mrows.Next() {
			var s slotOut
			if err := mrows.Scan(&s.ID, &s.Name, &s.Token, &s.LastLoginAt); err != nil {
				mrows.Close()
				httpx.Error(w, http.StatusInternalServerError, "member scan failed")
				return
			}
			teams[i].Members = append(teams[i].Members, s)
		}
		mrows.Close()
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"event": d, "teams": teams})
}

// userIDFrom remains for admin/org user URL params (admins list, delete).
func userIDFrom(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// ResetPassword clears or replaces a staff user's password. Players don't
// have passwords any more (they log in with magic-link tokens).
func (h *Handlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if r.ContentLength > 0 {
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
	}
	me, _ := auth.FromContext(r.Context())
	var targetIsAdmin bool
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT is_admin FROM users WHERE id = $1`, id).Scan(&targetIsAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	switch {
	case me.IsAdmin:
		if targetIsAdmin && me.ID != id {
			httpx.Error(w, http.StatusForbidden, "admin-on-admin reset is not supported")
			return
		}
	case me.ID == id:
		// anyone can reset their own
	default:
		// org_admin can reset anyone in their own active org (besides
		// admins). Caller must have an active org and be org_admin in
		// it; target must be a member of that same org.
		if me.OrgID == nil || me.Role != auth.RoleOrgAdmin {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		var found int
		mErr := h.DB.QueryRowContext(r.Context(),
			`SELECT 1 FROM org_memberships WHERE user_id = $1 AND org_id = $2`,
			id, *me.OrgID).Scan(&found)
		if errors.Is(mErr, sql.ErrNoRows) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		if mErr != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
	}
	if req.Password == "" {
		httpx.Error(w, http.StatusBadRequest, "password required")
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "hash failed")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE users SET password_hash = $1 WHERE id = $2`, hash, id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id = $1`, id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "session cleanup failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}
