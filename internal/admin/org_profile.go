package admin

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/imageutil"
	"git.sr.ht/~klahr/zv/internal/logx"
	"git.sr.ht/~klahr/zv/internal/storage"
)

// orgProfileDTO is the GET / PUT body for the org-admin "Public
// profile" surface (Settings → General). Pairs the slug-derived
// store URL with the editable copy fields (tagline, website) and
// boolean indicators for the binary asset endpoints (logo + cover)
// so the GUI can render upload/preview without a speculative fetch.
type orgProfileDTO struct {
	Slug          string `json:"slug"`
	StoreURL      string `json:"store_url"`
	LandingURL    string `json:"landing_url"`
	Tagline       string `json:"tagline"`
	PublicWebsite string `json:"public_website"`
	HasLogo       bool   `json:"has_logo"`
	HasCover      bool   `json:"has_cover"`
}

// orgProfileMaxTagline caps the tagline length. Long enough for a
// useful one-liner ("Real-world team adventures around Stockholm")
// without giving an org room to dump a paragraph that breaks the
// hero layout. Mirrors Twitter's old 140 — culturally-anchored at
// "short by design".
const orgProfileMaxTagline = 140

// orgProfileMaxWebsite is the URL length cap. Plenty of room for
// any reasonable URL; the actual gate is format (http/https only)
// applied below.
const orgProfileMaxWebsite = 500

// GetMyOrgProfile returns the current org's full public profile.
// Available to any authenticated org user; the edit endpoints
// (SetMyOrgProfile, SetMyOrgCover, DeleteMyOrgCover) are
// org_admin-only. Read-only access lets org_member render the
// "this is what your store URL looks like" preview without giving
// them the rename button.
func (h *Handlers) GetMyOrgProfile(w http.ResponseWriter, r *http.Request) {
	me, ok := auth.FromContext(r.Context())
	if !ok || me.OrgID == nil || *me.OrgID == 0 {
		httpx.Error(w, http.StatusForbidden, "not in an organization")
		return
	}
	orgID := *me.OrgID
	var (
		slug     string
		tagline  sql.NullString
		website  sql.NullString
		hasLogo  bool
		hasCover bool
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT slug,
		       tagline,
		       public_website,
		       (logo_content IS NOT NULL AND length(logo_content) > 0),
		       (cover_image_content IS NOT NULL AND length(cover_image_content) > 0)
		FROM organizations
		WHERE id = $1
	`, orgID).Scan(&slug, &tagline, &website, &hasLogo, &hasCover)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "organization not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	httpx.JSON(w, http.StatusOK, orgProfileDTO{
		Slug:          slug,
		StoreURL:      storeURLFromSlug(h.PublicURL, slug),
		LandingURL:    landingURLFromSlug(h.PublicURL, slug),
		Tagline:       tagline.String,
		PublicWebsite: website.String,
		HasLogo:       hasLogo,
		HasCover:      hasCover,
	})
}

// SetMyOrgProfile updates the editable copy fields. Slug rename
// stays on its own endpoint so a quick tagline tweak doesn't risk
// fat-fingering the URL. RoleOrgAdmin only.
func (h *Handlers) SetMyOrgProfile(w http.ResponseWriter, r *http.Request) {
	me, ok := auth.FromContext(r.Context())
	if !ok || me.OrgID == nil || *me.OrgID == 0 {
		httpx.Error(w, http.StatusForbidden, "not in an organization")
		return
	}
	if me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "org admins only")
		return
	}
	orgID := *me.OrgID
	var req orgProfileDTO
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	tagline := strings.TrimSpace(req.Tagline)
	if len(tagline) > orgProfileMaxTagline {
		httpx.Error(w, http.StatusBadRequest, "tagline too long")
		return
	}
	website := strings.TrimSpace(req.PublicWebsite)
	if len(website) > orgProfileMaxWebsite {
		httpx.Error(w, http.StatusBadRequest, "website URL too long")
		return
	}
	if website != "" {
		// Restrict to http/https; "javascript:" or "data:" in the link
		// would render as a clickable XSS vector on the public landing.
		// Empty stays empty (clears the field).
		u, err := url.Parse(website)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			httpx.Error(w, http.StatusBadRequest, "website must be a full http or https URL")
			return
		}
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE organizations
		   SET tagline = NULLIF($1, ''),
		       public_website = NULLIF($2, '')
		 WHERE id = $3
	`, tagline, website, orgID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	logx.Info("admin.org.profile.updated", "org_id", orgID, "by", me.Username)
	// Re-render the GET payload so the frontend doesn't need a second
	// round-trip to refresh has_cover / has_logo derived booleans.
	h.GetMyOrgProfile(w, r)
}

// SetMyOrgCover stores the uploaded cover image. Mirrors SetOrgLogo
// down to the imageutil normalisation pass — re-encoding kills any
// polyglot upload and clamps dimensions to imageutil.DefaultMaxDim.
// RoleOrgAdmin only.
func (h *Handlers) SetMyOrgCover(w http.ResponseWriter, r *http.Request) {
	me, ok := auth.FromContext(r.Context())
	if !ok || me.OrgID == nil || *me.OrgID == 0 {
		httpx.Error(w, http.StatusForbidden, "not in an organization")
		return
	}
	if me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "org admins only")
		return
	}
	orgID := *me.OrgID
	if err := r.ParseMultipartForm(maxImageBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad multipart form")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field required")
		return
	}
	defer f.Close()
	if hdr.Size > maxImageBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "image too large")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read failed")
		return
	}
	if int64(len(data)) > maxImageBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "image too large")
		return
	}
	mime := hdr.Header.Get("Content-Type")
	normalized, newMIME, err := imageutil.NormalizeUpload(data, mime, imageutil.DefaultMaxDim)
	if errors.Is(err, imageutil.ErrUnsupportedMIME) {
		httpx.Error(w, http.StatusBadRequest, "only PNG, JPEG, or WebP cover images are accepted")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "unreadable image: "+err.Error())
		return
	}
	data, mime = normalized, newMIME
	if err := h.Storage.PutOrgCover(r.Context(), orgID, data, mime); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "organisation not found")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "store failed", err)
		return
	}
	logx.Info("admin.org.cover.uploaded", "org_id", orgID, "size", len(data), "by", me.Username)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"url":  orgCoverURL(orgID),
		"mime": mime,
		"size": int64(len(data)),
	})
}

// DeleteMyOrgCover clears the cover image so the landing falls back
// to the no-image hero treatment. RoleOrgAdmin only.
func (h *Handlers) DeleteMyOrgCover(w http.ResponseWriter, r *http.Request) {
	me, ok := auth.FromContext(r.Context())
	if !ok || me.OrgID == nil || *me.OrgID == 0 {
		httpx.Error(w, http.StatusForbidden, "not in an organization")
		return
	}
	if me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "org admins only")
		return
	}
	orgID := *me.OrgID
	if err := h.Storage.DeleteOrgCover(r.Context(), orgID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "clear failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// ServeOrgCover writes the stored cover image bytes. Public read —
// same rationale as ServeOrgLogo: this is brand artwork the org is
// publishing on its own landing page; the org id in the URL is not
// sensitive. Cache-Control matches the logo at 60s — covers update
// rarely, but the operator-flipped cache-buster on the GET payload
// (we never set one) means a hard refresh is the way to invalidate.
func (h *Handlers) ServeOrgCover(w http.ResponseWriter, r *http.Request) {
	idRaw := strings.TrimPrefix(r.URL.Path, "/org-cover/")
	orgID, err := strconv.ParseInt(idRaw, 10, 64)
	if err != nil || orgID <= 0 {
		http.NotFound(w, r)
		return
	}
	content, mime, err := h.Storage.GetOrgCover(r.Context(), orgID)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("Cache-Control", "public, max-age=60")
	_, _ = w.Write(content)
}

// orgCoverURL is the canonical public read path. Keyed by id rather
// than slug so a slug rename doesn't break a server-side template
// that already substituted the bytes URL.
func orgCoverURL(orgID int64) string {
	return "/org-cover/" + strconv.FormatInt(orgID, 10)
}

// landingURLFromSlug returns the absolute URL of /o/{slug} for use
// in copy-button labels and shareable-link affordances. Sibling of
// storeURLFromSlug; same publicURL fallback logic.
func landingURLFromSlug(publicURL, slug string) string {
	base := strings.TrimRight(publicURL, "/")
	if base == "" {
		return "/o/" + slug
	}
	return base + "/o/" + slug
}
