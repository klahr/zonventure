package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	glook "github.com/klahr/zonventure/internal/graph/look"
	"github.com/klahr/zonventure/internal/httpx"
)

// The organisation's theme library: looks made once and picked in any of
// its adventures, where the theme editor copies one into the graph. Any
// member may use them; an org admin — who may author — adds and edits.
// A look in the library has no picture or sounds: those are an
// adventure's own media, which another adventure may not use.

type orgThemeDTO struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Look      glook.Look `json:"look"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// withoutMedia drops a look's picture and sounds, reporting whether it had any.
func withoutMedia(l *glook.Look) bool {
	dropped := false
	if b := l.Backdrop; b != nil && b.Kind == "picture" {
		l.Backdrop = nil
		dropped = true
	}
	if s := l.Style; s != nil && (s.SoundRight != 0 || s.SoundWrong != 0) {
		s.SoundRight, s.SoundWrong = 0, 0
		dropped = true
	}
	return dropped
}

// ListOrgThemes serves GET /api/admin/org-themes: the caller's organisation's themes, by name.
func (h *Handlers) ListOrgThemes(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	out := []orgThemeDTO{}
	if me.OrgID == nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"themes": out})
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id, name, look, updated_at FROM org_themes WHERE org_id = $1 ORDER BY lower(name)`, *me.OrgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading themes failed", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var (
			d   orgThemeDTO
			doc []byte
		)
		if err := rows.Scan(&d.ID, &d.Name, &doc, &d.UpdatedAt); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "loading themes failed", err)
			return
		}
		if json.Unmarshal(doc, &d.Look) == nil {
			out = append(out, d)
		}
	}
	if err := rows.Err(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading themes failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"themes": out})
}

type orgThemeRequest struct {
	Name string      `json:"name"`
	Look *glook.Look `json:"look"`
}

// orgThemeAuthor checks the caller may change the organisation's library.
func orgThemeAuthor(w http.ResponseWriter, r *http.Request) (int64, bool) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil || !canAuthor(me, sql.NullInt64{Int64: *me.OrgID, Valid: true}) {
		httpx.Error(w, http.StatusForbidden, "only an organisation admin can change its themes")
		return 0, false
	}
	return *me.OrgID, true
}

// readOrgTheme decodes and checks a theme, its picture and sounds dropped.
func readOrgTheme(w http.ResponseWriter, r *http.Request, needLook bool) (string, *glook.Look, bool, bool) {
	var req orgThemeRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return "", nil, false, false
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		httpx.Error(w, http.StatusBadRequest, "a theme needs a name of at most 80 characters")
		return "", nil, false, false
	}
	if req.Look == nil {
		if needLook {
			httpx.Error(w, http.StatusBadRequest, "a theme needs its look")
			return "", nil, false, false
		}
		return name, nil, false, true
	}
	dropped := withoutMedia(req.Look)
	if err := req.Look.Validate(); err != nil {
		httpx.Error(w, http.StatusBadRequest, "look "+err.Error())
		return "", nil, false, false
	}
	return name, req.Look, dropped, true
}

// CreateOrgTheme serves POST /api/admin/org-themes {name, look}.
func (h *Handlers) CreateOrgTheme(w http.ResponseWriter, r *http.Request) {
	org, ok := orgThemeAuthor(w, r)
	if !ok {
		return
	}
	name, l, dropped, ok := readOrgTheme(w, r, true)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	doc, _ := json.Marshal(l)
	d := orgThemeDTO{Name: name, Look: *l}
	err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO org_themes (org_id, name, look, created_by_user_id) VALUES ($1, $2, $3, $4)
		RETURNING id, updated_at
	`, org, name, doc, me.ID).Scan(&d.ID, &d.UpdatedAt)
	if isUniqueViolation(err) {
		httpx.Error(w, http.StatusConflict, "the organisation already has a theme of that name")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving the theme failed", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"theme": d, "dropped_media": dropped})
}

// UpdateOrgTheme serves PUT /api/admin/org-themes/{id} {name, look?}: a
// new name, and a new look when one is sent.
func (h *Handlers) UpdateOrgTheme(w http.ResponseWriter, r *http.Request) {
	org, ok := orgThemeAuthor(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad theme id")
		return
	}
	name, l, dropped, ok := readOrgTheme(w, r, false)
	if !ok {
		return
	}
	var doc any
	if l != nil {
		b, _ := json.Marshal(l)
		doc = string(b)
	}
	res, err := h.DB.ExecContext(r.Context(), `
		UPDATE org_themes SET name = $1, look = COALESCE($2::jsonb, look), updated_at = now()
		WHERE id = $3 AND org_id = $4
	`, name, doc, id, org)
	if isUniqueViolation(err) {
		httpx.Error(w, http.StatusConflict, "the organisation already has a theme of that name")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving the theme failed", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "no such theme")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"dropped_media": dropped})
}

// DeleteOrgTheme serves DELETE /api/admin/org-themes/{id}. Adventures that
// took the theme keep their copy.
func (h *Handlers) DeleteOrgTheme(w http.ResponseWriter, r *http.Request) {
	org, ok := orgThemeAuthor(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad theme id")
		return
	}
	res, err := h.DB.ExecContext(r.Context(), `DELETE FROM org_themes WHERE id = $1 AND org_id = $2`, id, org)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "removing the theme failed", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "no such theme")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
