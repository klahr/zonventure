package admin

import (
	"database/sql"
	"errors"
	"net/http"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"github.com/go-chi/chi/v5"
)

type arrangerDTO struct {
	ID       int64    `json:"id"`
	Username string   `json:"username"`
	Grants   []string `json:"grants"`
}

// ListArrangers returns every arranger together with their adventure grants.
// Root-only.
func (h *Handlers) ListArrangers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, username FROM users WHERE role = 'arranger' ORDER BY username COLLATE NOCASE
	`)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []arrangerDTO{}
	for rows.Next() {
		a := arrangerDTO{Grants: []string{}}
		if err := rows.Scan(&a.ID, &a.Username); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		out = append(out, a)
	}
	// Second pass: fill grants.
	for i := range out {
		grows, err := h.DB.QueryContext(r.Context(), `
			SELECT adventure_slug FROM arranger_adventures WHERE arranger_user_id = ? ORDER BY adventure_slug
		`, out[i].ID)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "grants failed")
			return
		}
		for grows.Next() {
			var s string
			if err := grows.Scan(&s); err != nil {
				grows.Close()
				httpx.Error(w, http.StatusInternalServerError, "scan failed")
				return
			}
			out[i].Grants = append(out[i].Grants, s)
		}
		grows.Close()
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"arrangers": out})
}

type grantRequest struct {
	AdventureSlug string `json:"adventure_slug"`
}

// GrantAdventure gives an arranger permission to schedule runs of one
// adventure. Root-only.
func (h *Handlers) GrantAdventure(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	var req grantRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if h.Lib.Get(req.AdventureSlug) == nil {
		httpx.Error(w, http.StatusBadRequest, "unknown adventure")
		return
	}
	var role string
	err := h.DB.QueryRowContext(r.Context(), `SELECT role FROM users WHERE id = ?`, id).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if role != auth.RoleArranger {
		httpx.Error(w, http.StatusBadRequest, "target user is not an arranger")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		INSERT OR IGNORE INTO arranger_adventures(arranger_user_id, adventure_slug) VALUES (?, ?)
	`, id, req.AdventureSlug); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "grant failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// RevokeAdventure removes a grant. Root-only.
func (h *Handlers) RevokeAdventure(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		httpx.Error(w, http.StatusBadRequest, "missing slug")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		DELETE FROM arranger_adventures WHERE arranger_user_id = ? AND adventure_slug = ?
	`, id, slug); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "revoke failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}
