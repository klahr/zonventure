package admin

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"github.com/go-chi/chi/v5"
)

/* ---------------- Adventure shares (commission an adventure for a specific org) ---------------- */

// ListAdventureShares returns the orgs that currently have a share for
// this adventure. Admin-only; the UI uses it to populate the manager
// section on the library edit page.
func (h *Handlers) ListAdventureShares(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	shares, err := loadAdventureShares(r.Context(), h.DB, slug)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "load failed")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"shares": shares})
}

type grantShareRequest struct {
	OrgID int64 `json:"org_id"`
}

// GrantAdventureShare adds (or keeps) a share row for the given org.
// Idempotent: re-granting to the same org is a no-op, so the UI can
// repost without checking first.
func (h *Handlers) GrantAdventureShare(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	var req grantShareRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.OrgID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "org_id required")
		return
	}
	var orgExists int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM organizations WHERE id = $1`, req.OrgID).Scan(&orgExists); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if orgExists == 0 {
		httpx.Error(w, http.StatusNotFound, "organization not found")
		return
	}
	me, _ := auth.FromContext(r.Context())
	if _, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO adventure_shares(adventure_slug, org_id, granted_by_user_id)
		VALUES ($1, $2, $3)
		ON CONFLICT(adventure_slug, org_id) DO NOTHING
	`, slug, req.OrgID, me.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "grant failed", err)
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// RevokeAdventureShare removes the share for a given org. Runs already
// created from the share are unaffected — revoking only stops new events.
func (h *Handlers) RevokeAdventureShare(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	orgID, err := strconv.ParseInt(chi.URLParam(r, "orgID"), 10, 64)
	if err != nil || orgID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad org id")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM adventure_shares WHERE adventure_slug = $1 AND org_id = $2`,
		slug, orgID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "revoke failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

func loadAdventureShares(ctx context.Context, db *sql.DB, slug string) ([]shareView, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.org_id, o.name, s.granted_at
		FROM adventure_shares s
		JOIN organizations o ON o.id = s.org_id
		WHERE s.adventure_slug = $1
		ORDER BY LOWER(o.name)
	`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []shareView{}
	for rows.Next() {
		var v shareView
		if err := rows.Scan(&v.OrgID, &v.OrgName, &v.GrantedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
