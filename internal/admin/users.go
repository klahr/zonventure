package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"github.com/go-chi/chi/v5"
)

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
		Index    int      `json:"index"`
		Title    string   `json:"title"`
		Prompt   string   `json:"prompt"`
		Accept   []string `json:"accept"`
		Location locDTO   `json:"location"`
		Gated    bool     `json:"gated"`
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

// RunDetail returns a run with its teams and slots (players).
func (h *Handlers) RunDetail(w http.ResponseWriter, r *http.Request) {
	runID, ok := runIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad run id")
		return
	}
	if _, ok := h.authorizeRun(w, r, runID); !ok {
		return
	}
	var (
		d       runDTO
		showMap int
	)
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT ar.id, ar.name, ar.adventure_slug, ar.state,
		       COALESCE(ar.scheduled_at,''), COALESCE(ar.started_at,''),
		       ar.org_id, COALESCE(ar.created_by_user_id, 0),
		       COALESCE(ar.assignment_id, 0), COALESCE(aa.max_teams, 0),
		       ar.live_token, ar.show_map
		FROM adventure_runs ar
		LEFT JOIN adventure_assignments aa ON aa.id = ar.assignment_id
		WHERE ar.id = $1
	`, runID).Scan(&d.ID, &d.Name, &d.AdventureSlug, &d.State, &d.ScheduledAt, &d.StartedAt,
		&d.OrgID, &d.CreatedByUserID, &d.AssignmentID, &d.MaxTeams,
		&d.LiveToken, &showMap); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	d.ShowMap = showMap != 0

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
		FROM teams t WHERE t.run_id = $1 ORDER BY t.name
	`, runID)
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
	httpx.JSON(w, http.StatusOK, map[string]any{"run": d, "teams": teams})
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
	var (
		role    string
		orgID   sql.NullInt64
	)
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT role, org_id FROM users WHERE id = $1`, id).Scan(&role, &orgID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	switch {
	case me.Role == auth.RoleAdmin:
		if role == auth.RoleAdmin && me.ID != id {
			httpx.Error(w, http.StatusForbidden, "admin-on-admin reset is not supported")
			return
		}
	case me.ID == id:
		// anyone can reset their own
	default:
		// org_admin can reset anyone in their own org (besides admins).
		if me.OrgID == nil || !orgID.Valid || *me.OrgID != orgID.Int64 {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		if me.Role != auth.RoleOrgAdmin {
			httpx.Error(w, http.StatusForbidden, "forbidden")
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
