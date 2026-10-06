package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/httpx"
)

// The platform catalog is the adventures and library puzzles of the
// publisher org — at most one organisation, flagged
// organizations.is_publisher. Its published adventures are visible to
// every org; everything else it owns stays private to it like any
// org's content.

// CatalogEnabled opens the catalog to every org. It is off for now: a
// published adventure, and the publisher's puzzle library, stay with
// the publisher, and other orgs reach its adventures only through a
// commission or an event pass the operator grants. Curation still
// works, so turning this back on restores the listing as it was. The
// console hides the catalog under the same switch (CATALOG_ENABLED in
// web/apps/arrange/src/router.ts).
var CatalogEnabled = false

// PuzzleCountSQL counts the puzzles of `adventures a` in its graph: the
// published version, else the draft, else none.
func PuzzleCountSQL() string {
	return `(SELECT COUNT(*) FROM jsonb_array_elements(COALESCE(
	           (SELECT g.graph FROM adventure_graphs g WHERE g.id = a.published_graph_id),
	           (SELECT d.graph FROM adventure_graph_drafts d WHERE d.adventure_id = a.id)) -> 'nodes') n
	         WHERE n ->> 'type' = 'puzzle')`
}

// CatalogFilter restricts a query over `adventures a` to the published
// catalog, or matches nothing while the catalog is off.
func CatalogFilter() string {
	if !CatalogEnabled {
		return `FALSE`
	}
	return `(a.published = 1 AND NOT a.trail_hidden AND a.org_id = (SELECT id FROM organizations WHERE is_publisher))`
}

// publisherOrgID returns the publisher org's id; ok is false when no
// org is flagged.
func publisherOrgID(ctx context.Context, db *sql.DB) (id int64, ok bool, err error) {
	err = db.QueryRowContext(ctx, `SELECT id FROM organizations WHERE is_publisher`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

// isCatalogOwner reports whether owner is the publisher org. A lookup
// error counts as "no", failing closed like the other visibility gates.
func (h *Handlers) isCatalogOwner(ctx context.Context, owner sql.NullInt64) bool {
	if !owner.Valid {
		return false
	}
	id, ok, err := publisherOrgID(ctx, h.DB)
	return err == nil && ok && id == owner.Int64
}

// isCatalogSlug reports whether slug is a catalog adventure — the only
// kind the operator may assign or commission to an org.
func (h *Handlers) isCatalogSlug(ctx context.Context, slug string) bool {
	owner, err := h.adventureOwner(ctx, slug)
	return err == nil && h.isCatalogOwner(ctx, owner)
}

/* ---------------- Catalog curation (platform operator) ---------------- */

type catalogAdventureDTO struct {
	Slug      string      `json:"slug"`
	Title     string      `json:"title"`
	Language  string      `json:"language"`
	Published bool        `json:"published"`
	Playable  bool        `json:"playable"`
	AvatarURL string      `json:"avatar_url,omitempty"`
	Shares    []shareView `json:"shares"`
}

// ListCatalog returns every adventure the publisher org owns, drafts
// included, each with the orgs it is commissioned for. The operator
// curates from this list; authoring stays with the publisher org.
//
// GET /api/admin/catalog
func (h *Handlers) ListCatalog(w http.ResponseWriter, r *http.Request) {
	pid, ok, err := publisherOrgID(r.Context(), h.DB)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	out := []catalogAdventureDTO{}
	if !ok {
		httpx.JSON(w, http.StatusOK, map[string]any{"publisher_org_id": nil, "adventures": out})
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT a.slug, a.title, a.language, a.published <> 0,
		       a.published_graph_id IS NOT NULL,
		       a.avatar_mime_type IS NOT NULL
		FROM adventures a
		WHERE a.org_id = $1 AND NOT a.trail_hidden
		ORDER BY LOWER(a.title), a.slug
	`, pid)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var (
			d         catalogAdventureDTO
			hasAvatar bool
		)
		if err := rows.Scan(&d.Slug, &d.Title, &d.Language, &d.Published, &d.Playable, &hasAvatar); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if hasAvatar {
			d.AvatarURL = adventureAvatarURL(d.Slug)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	for i := range out {
		shares, err := loadAdventureShares(r.Context(), h.DB, out[i].Slug)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "load failed")
			return
		}
		out[i].Shares = shares
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"publisher_org_id": pid, "adventures": out})
}

// requireCatalogAdventure answers 404 unless slug is owned by the
// publisher org — the operator curates the catalog, not other orgs'
// private work.
func (h *Handlers) requireCatalogAdventure(w http.ResponseWriter, r *http.Request, slug string) bool {
	if !h.isCatalogSlug(r.Context(), slug) {
		httpx.Error(w, http.StatusNotFound, "not a catalog adventure")
		return false
	}
	return true
}

type setPublishedRequest struct {
	Published bool `json:"published"`
}

// SetCatalogPublished lists or unlists a catalog adventure. Listed
// adventures are visible to every org; unlisted ones only to orgs
// they are commissioned for.
//
// PUT /api/admin/catalog/{slug}/published
func (h *Handlers) SetCatalogPublished(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if !h.requireCatalogAdventure(w, r, slug) {
		return
	}
	var req setPublishedRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	v := 0
	if req.Published {
		v = 1
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE adventures SET published = $1 WHERE slug = $2`, v, slug); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

type setPublisherRequest struct {
	IsPublisher bool `json:"is_publisher"`
}

// SetOrgPublisher makes an org the catalog publisher, or stops it
// being one. At most one org publishes (409 while another does).
// Unflagging hides the whole catalog from every other org, so it is
// password-gated like the other destructive operator actions.
//
// PUT /api/admin/organizations/{orgID}/publisher
func (h *Handlers) SetOrgPublisher(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad org id")
		return
	}
	var req setPublisherRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if !req.IsPublisher && !h.requirePasswordConfirm(w, r) {
		return
	}
	if req.IsPublisher {
		pid, ok, err := publisherOrgID(r.Context(), h.DB)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if ok && pid != orgID {
			httpx.Error(w, http.StatusConflict, "another organization is already the catalog publisher")
			return
		}
	}
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE organizations SET is_publisher = $1 WHERE id = $2`, req.IsPublisher, orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "update failed", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "organization not found")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}
