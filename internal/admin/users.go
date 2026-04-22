package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"github.com/go-chi/chi/v5"
)

type userDTO struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
	TeamCount int    `json:"team_count"`
}

// ListUsers returns the users the caller is allowed to see. Root sees
// everyone. Arrangers see only the player accounts they themselves created,
// so one arranger's players are invisible to another arranger.
func (h *Handlers) ListUsers(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	var (
		query string
		args  []any
	)
	if me.Role == auth.RoleRoot {
		query = `
			SELECT u.id, u.username, u.role, u.created_at,
			       (SELECT COUNT(*) FROM team_members tm WHERE tm.user_id = u.id)
			FROM users u
			ORDER BY u.role, u.username COLLATE NOCASE
		`
	} else {
		query = `
			SELECT u.id, u.username, u.role, u.created_at,
			       (SELECT COUNT(*) FROM team_members tm WHERE tm.user_id = u.id)
			FROM users u
			WHERE u.role = 'player' AND u.created_by_user_id = ?
			ORDER BY u.username COLLATE NOCASE
		`
		args = []any{me.ID}
	}
	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []userDTO{}
	for rows.Next() {
		var u userDTO
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &u.TeamCount); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		out = append(out, u)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"users": out})
}

// confirmHeader is the request header destructive endpoints read the
// arranger's password from. Kept as a constant so JS and Go stay in sync.
const confirmHeader = "X-Confirm-Password"

// requirePasswordConfirm checks the X-Confirm-Password header against the
// caller's stored hash, writes the appropriate error if it doesn't match
// and returns false. Handlers should return early if this returns false.
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

// canManageTarget returns true if `me` is allowed to mutate a user row with
// the given target role and owner. Hierarchy: root > arranger > player.
// Arrangers can only manage player accounts they themselves created.
func canManageTarget(me auth.User, targetRole string, targetOwner sql.NullInt64) bool {
	switch me.Role {
	case auth.RoleRoot:
		return targetRole != auth.RoleRoot
	case auth.RoleArranger:
		if targetRole != auth.RolePlayer {
			return false
		}
		return targetOwner.Valid && targetOwner.Int64 == me.ID
	default:
		return false
	}
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// CreateUser provisions a new user account. Arrangers may only create
// players; root may create players or arrangers. Creating another root must
// be done out-of-band (the -seed-root flag) — the web UI never grants the
// superuser role.
//
// Player accounts are created without a password — the player sets their
// own when they log in for the first time. Arrangers (and anyone else with
// a web-create surface) still need an initial password supplied here.
func (h *Handlers) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		httpx.Error(w, http.StatusBadRequest, "username required")
		return
	}
	me, _ := auth.FromContext(r.Context())
	var storedHash any // NULL for players until they set their own
	switch req.Role {
	case auth.RolePlayer:
		// Any admin may create players. Ignore any password supplied — the
		// player sets their own on first login.
	case auth.RoleArranger:
		if me.Role != auth.RoleRoot {
			httpx.Error(w, http.StatusForbidden, "only root may create arrangers")
			return
		}
		if req.Password == "" {
			httpx.Error(w, http.StatusBadRequest, "password required for arrangers")
			return
		}
		h, err := auth.HashPassword(req.Password)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "hash failed")
			return
		}
		storedHash = h
	default:
		httpx.Error(w, http.StatusBadRequest, "role must be 'player' or 'arranger'")
		return
	}
	// Players are owned by the arranger that created them; arrangers created
	// by root carry the root's id.
	res, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO users(username, password_hash, role, created_by_user_id) VALUES (?, ?, ?, ?)
	`, req.Username, storedHash, req.Role, me.ID)
	if err != nil {
		httpx.Error(w, http.StatusConflict, "username not available")
		return
	}
	id, _ := res.LastInsertId()
	httpx.JSON(w, http.StatusCreated, userDTO{ID: id, Username: req.Username, Role: req.Role})
}

// DeleteUser removes a user if the caller's role dominates the target's,
// and never allows self-deletion. Requires the arranger's own password in
// the X-Confirm-Password header so a stolen session alone can't delete
// accounts without the password.
func (h *Handlers) DeleteUser(w http.ResponseWriter, r *http.Request) {
	if !requirePasswordConfirm(w, r, h.DB) {
		return
	}
	id, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.ID == id {
		httpx.Error(w, http.StatusConflict, "cannot delete yourself")
		return
	}
	var (
		targetRole  string
		targetOwner sql.NullInt64
	)
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT role, created_by_user_id FROM users WHERE id = ?`, id).Scan(&targetRole, &targetOwner)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !canManageTarget(me, targetRole, targetOwner) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM users WHERE id = ?`, id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

type resetPasswordRequest struct {
	Password string `json:"password"`
}

// ResetPassword resets a user's password. For players the stored hash is
// cleared so they choose a new one on next login — arrangers never see or
// set a player's password directly. For arrangers the caller must supply a
// new password in the request body. Active sessions for the affected user
// are always invalidated.
func (h *Handlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	// Body is optional: only parsed for non-player targets.
	var req resetPasswordRequest
	if r.ContentLength > 0 {
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
	}
	me, _ := auth.FromContext(r.Context())
	var (
		targetRole  string
		targetOwner sql.NullInt64
	)
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT role, created_by_user_id FROM users WHERE id = ?`, id).Scan(&targetRole, &targetOwner)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if me.ID != id && !canManageTarget(me, targetRole, targetOwner) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	if targetRole == auth.RolePlayer {
		// Clear the password so the player sets a fresh one on first login.
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE users SET password_hash = NULL WHERE id = ?`, id); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	} else {
		if req.Password == "" {
			httpx.Error(w, http.StatusBadRequest, "password required")
			return
		}
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "hash failed")
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE users SET password_hash = ? WHERE id = ?`, hash, id); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "session cleanup failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// AdventureDetail returns the full page list (including accepted answers and
// GPS gates) for a given adventure — arrangers need this to coach teams or
// troubleshoot answers.
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
		Hint     string   `json:"hint,omitempty"`
		Accept   []string `json:"accept"`
		Location locDTO   `json:"location"`
		Gated    bool     `json:"gated"`
	}
	pages := make([]pageDTO, len(adv.Pages))
	for i, p := range adv.Pages {
		pd := pageDTO{
			Index:  p.Index,
			Title:  p.Title,
			Prompt: p.Prompt,
			Hint:   p.Hint,
			Accept: p.Accept,
			Location: locDTO{
				Lat:     p.Location.Lat,
				Lng:     p.Location.Lng,
				RadiusM: p.Location.RadiusM,
			},
			Gated: p.Location.Gated(),
		}
		pages[i] = pd
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"slug":        adv.Slug,
		"title":       adv.Meta.Title,
		"description": adv.Meta.Description,
		"author":      adv.Meta.Author,
		"page_count":  len(adv.Pages),
		"pages":       pages,
	})
}

// RunDetail returns a run with its teams and members — everything the
// arranger needs for one run on a single screen.
func (h *Handlers) RunDetail(w http.ResponseWriter, r *http.Request) {
	runID, ok := runIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad run id")
		return
	}
	arr, _ := auth.FromContext(r.Context())
	var d runDTO
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT id, adventure_slug, state,
		       COALESCE(scheduled_at,''), COALESCE(started_at,''), arranger_user_id
		FROM adventure_runs WHERE id = ? AND arranger_user_id = ?
	`, runID, arr.ID).Scan(&d.ID, &d.AdventureSlug, &d.State, &d.ScheduledAt, &d.StartedAt, &d.ArrangerUserID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT t.id, t.name, t.current_page_index, COALESCE(t.completed_at, '')
		FROM teams t WHERE t.run_id = ? ORDER BY t.name
	`, runID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "teams failed")
		return
	}
	defer rows.Close()

	type teamMember struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	type teamWithMembers struct {
		ID          int64        `json:"id"`
		Name        string       `json:"name"`
		PageIndex   int          `json:"page_index"`
		CompletedAt string       `json:"completed_at,omitempty"`
		Members     []teamMember `json:"members"`
	}
	teams := []teamWithMembers{}
	for rows.Next() {
		t := teamWithMembers{Members: []teamMember{}}
		if err := rows.Scan(&t.ID, &t.Name, &t.PageIndex, &t.CompletedAt); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "team scan failed")
			return
		}
		teams = append(teams, t)
	}
	// Pull members in a second query to keep the scan simple.
	for i := range teams {
		mrows, err := h.DB.QueryContext(r.Context(), `
			SELECT u.id, u.username FROM team_members tm
			JOIN users u ON u.id = tm.user_id
			WHERE tm.team_id = ? ORDER BY u.username COLLATE NOCASE
		`, teams[i].ID)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "members failed")
			return
		}
		for mrows.Next() {
			var m teamMember
			if err := mrows.Scan(&m.ID, &m.Username); err != nil {
				mrows.Close()
				httpx.Error(w, http.StatusInternalServerError, "member scan failed")
				return
			}
			teams[i].Members = append(teams[i].Members, m)
		}
		mrows.Close()
	}

	httpx.JSON(w, http.StatusOK, map[string]any{"run": d, "teams": teams})
}

func userIDFrom(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
