package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/storage"
)

// Changes to an organisation's public page, while it has something for
// sale and no platform admin vouches for it (orgGated), apply at once
// and are reviewed afterwards (migration 0169): a reviewer approves one
// or puts the previous value back, and the organisation is told why.

// orgPageColumns are the text fields and their organizations columns.
var orgPageColumns = map[string]string{"name": "name", "tagline": "tagline", "public_website": "public_website"}

// noteOrgText records a change to a text field for review, keeping the
// value a reviewer last saw. Changed back to it, nothing is left to
// review.
func (h *Handlers) noteOrgText(ctx context.Context, orgID, userID int64, field, previous, next string) error {
	if previous == next {
		return nil
	}
	var by any
	if userID > 0 {
		by = userID
	}
	if _, err := h.DB.ExecContext(ctx, `
		INSERT INTO org_page_changes (org_id, field, previous_text, changed_by) VALUES ($1, $2, $3, $4)
		ON CONFLICT (org_id, field) WHERE reviewed_at IS NULL DO UPDATE SET changed_at = now(), changed_by = excluded.changed_by
	`, orgID, field, previous, by); err != nil {
		return err
	}
	_, err := h.DB.ExecContext(ctx, `
		DELETE FROM org_page_changes WHERE org_id = $1 AND field = $2 AND reviewed_at IS NULL AND previous_text = $3
	`, orgID, field, next)
	return err
}

// noteOrgPicture records a new logo or cover for review, keeping the
// picture a reviewer last saw, when the organisation's page is reviewed.
func (h *Handlers) noteOrgPicture(ctx context.Context, orgID, userID int64, field string) error {
	gated, err := orgGated(ctx, h.DB, orgID)
	if err != nil || !gated {
		return err
	}
	get := h.Storage.GetOrgLogo
	if field == "cover" {
		get = h.Storage.GetOrgCover
	}
	content, mime, err := get(ctx, orgID)
	if errors.Is(err, storage.ErrNotFound) {
		content, mime, err = nil, "", nil
	}
	if err != nil {
		return err
	}
	var by, m any
	if userID > 0 {
		by = userID
	}
	if mime != "" {
		m = mime
	}
	_, err = h.DB.ExecContext(ctx, `
		INSERT INTO org_page_changes (org_id, field, previous_mime, previous_content, changed_by) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (org_id, field) WHERE reviewed_at IS NULL DO UPDATE SET changed_at = now(), changed_by = excluded.changed_by
	`, orgID, field, m, content, by)
	return err
}

// orgPageChangeDTO is one change waiting for review.
type orgPageChangeDTO struct {
	ID        int64     `json:"id"`
	OrgID     int64     `json:"org_id"`
	OrgName   string    `json:"org_name"`
	OrgSlug   string    `json:"org_slug"`
	Field     string    `json:"field"`
	Previous  string    `json:"previous"`
	Current   string    `json:"current"`
	HadImage  bool      `json:"had_image"`
	ChangedAt time.Time `json:"changed_at"`
	ChangedBy string    `json:"changed_by,omitempty"`
}

// ListOrgPageChanges is every organisation page change waiting for
// review, oldest first. Listing reviewers and global admins.
//
// GET /api/admin/org-page-changes
func (h *Handlers) ListOrgPageChanges(w http.ResponseWriter, r *http.Request) {
	if _, ok := listingReader(w, r); !ok {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT c.id, c.org_id, o.name, o.slug, c.field, COALESCE(c.previous_text, ''),
		       CASE c.field WHEN 'name' THEN o.name WHEN 'tagline' THEN COALESCE(o.tagline, '') WHEN 'public_website' THEN COALESCE(o.public_website, '') ELSE '' END,
		       c.previous_content IS NOT NULL, c.changed_at, COALESCE(u.username, '')
		FROM org_page_changes c JOIN organizations o ON o.id = c.org_id LEFT JOIN users u ON u.id = c.changed_by
		WHERE c.reviewed_at IS NULL ORDER BY c.changed_at
	`)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	defer rows.Close()
	out := []orgPageChangeDTO{}
	for rows.Next() {
		var c orgPageChangeDTO
		if err := rows.Scan(&c.ID, &c.OrgID, &c.OrgName, &c.OrgSlug, &c.Field, &c.Previous, &c.Current, &c.HadImage, &c.ChangedAt, &c.ChangedBy); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		out = append(out, c)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"changes": out})
}

// OrgPageChangePrevious is the picture a logo or cover change replaced.
//
// GET /api/admin/org-page-changes/{id}/previous
func (h *Handlers) OrgPageChangePrevious(w http.ResponseWriter, r *http.Request) {
	if _, ok := listingReader(w, r); !ok {
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var (
		mime    sql.NullString
		content []byte
	)
	if err := h.DB.QueryRowContext(r.Context(), `SELECT previous_mime, previous_content FROM org_page_changes WHERE id = $1`, id).Scan(&mime, &content); err != nil || !mime.Valid || len(content) == 0 {
		httpx.Error(w, http.StatusNotFound, "no previous picture")
		return
	}
	w.Header().Set("Content-Type", mime.String)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	_, _ = w.Write(content)
}

// openOrgChange loads a change waiting for review that the caller, a
// listing reviewer outside its organisation, may decide.
func (h *Handlers) openOrgChange(w http.ResponseWriter, r *http.Request) (me auth.User, id, orgID int64, field string, ok bool) {
	me, ok = reviewer(w, r)
	if !ok {
		return
	}
	id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	err := h.DB.QueryRowContext(r.Context(), `SELECT org_id, field FROM org_page_changes WHERE id = $1 AND reviewed_at IS NULL`, id).Scan(&orgID, &field)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "no such change waiting for review")
		return me, 0, 0, "", false
	}
	own, err := h.ownOrg(r.Context(), me.ID, orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return me, 0, 0, "", false
	}
	if own {
		httpx.JSON(w, http.StatusForbidden, map[string]string{"error": "no one reviews their own organisation", "code": "own_org"})
		return me, 0, 0, "", false
	}
	return me, id, orgID, field, true
}

// ApproveOrgPageChange keeps a change.
//
// POST /api/admin/org-page-changes/{id}/approve
func (h *Handlers) ApproveOrgPageChange(w http.ResponseWriter, r *http.Request) {
	me, id, _, _, ok := h.openOrgChange(w, r)
	if !ok {
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE org_page_changes SET reviewed_at = now(), reviewed_by = $2, outcome = 'approved', previous_content = NULL WHERE id = $1
	`, id, me.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review failed", err)
		return
	}
	noteAction(r, strconv.FormatInt(id, 10))
	httpx.JSON(w, http.StatusOK, map[string]string{"outcome": "approved"})
}

// RollBackOrgPageChange puts the previous value back and tells the
// organisation's admins why: {"reason": "…"}.
//
// POST /api/admin/org-page-changes/{id}/roll-back
func (h *Handlers) RollBackOrgPageChange(w http.ResponseWriter, r *http.Request) {
	me, id, orgID, field, ok := h.openOrgChange(w, r)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		httpx.Error(w, http.StatusBadRequest, "say why it is rolled back")
		return
	}
	if len(reason) > MaxRejectReasonLen {
		httpx.Error(w, http.StatusBadRequest, "reason is too long")
		return
	}
	var (
		prevText sql.NullString
		prevMime sql.NullString
		prevData []byte
	)
	if err := h.DB.QueryRowContext(r.Context(), `SELECT previous_text, previous_mime, previous_content FROM org_page_changes WHERE id = $1`, id).
		Scan(&prevText, &prevMime, &prevData); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	ctx := r.Context()
	var err error
	switch field {
	case "logo", "cover":
		put, del := h.Storage.PutOrgLogo, h.Storage.DeleteOrgLogo
		if field == "cover" {
			put, del = h.Storage.PutOrgCover, h.Storage.DeleteOrgCover
		}
		if prevMime.Valid && len(prevData) > 0 {
			err = put(ctx, orgID, prevData, prevMime.String)
		} else {
			err = del(ctx, orgID)
			if errors.Is(err, storage.ErrNotFound) {
				err = nil
			}
		}
	default:
		col := orgPageColumns[field]
		if col == "name" {
			_, err = h.DB.ExecContext(ctx, `UPDATE organizations SET name = $2 WHERE id = $1`, orgID, prevText.String)
		} else {
			_, err = h.DB.ExecContext(ctx, `UPDATE organizations SET `+col+` = NULLIF($2, '') WHERE id = $1`, orgID, prevText.String)
		}
	}
	if err != nil {
		if isUniqueViolation(err) {
			httpx.JSON(w, http.StatusConflict, map[string]string{"error": "another organisation has the previous name now", "code": "org_name_taken"})
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "roll back failed", err)
		return
	}
	if _, err := h.DB.ExecContext(ctx, `
		UPDATE org_page_changes SET reviewed_at = now(), reviewed_by = $2, outcome = 'rolled_back', reason = $3, previous_content = NULL WHERE id = $1
	`, id, me.ID, reason); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review failed", err)
		return
	}
	noteAction(r, field+": "+reason)
	logx.Info("admin.org_page_change.rolled_back", "org_id", orgID, "field", field, "by", me.Username)
	h.mailDomainAdmins(ctx, orgID, func(lang string) (string, emailtpl.Content) {
		return emailtpl.OrgChangeRolledBack(lang, field, reason, h.arrangeLink("#/settings/organization"))
	})
	httpx.JSON(w, http.StatusOK, map[string]string{"outcome": "rolled_back"})
}
