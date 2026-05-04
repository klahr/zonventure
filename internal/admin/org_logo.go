package admin

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/imageutil"
	"git.sr.ht/~klahr/zv/internal/storage"
)

// orgLogoURL is the canonical serve path. Keyed by id rather than
// name so a rename doesn't break inflight references (the recap PDF
// generator embeds the bytes inline anyway, but the same URL is
// useful from the arranger console preview and any future surface
// that paints branding over WebSocket-driven UI).
func orgLogoURL(orgID int64) string {
	return "/org-logo/" + strconv.FormatInt(orgID, 10)
}

// SetOrgLogo stores the uploaded logo inline on the organisation row.
// PNG/JPEG only — SVG is intentionally rejected because every
// SaaS that allows it eventually has a stored XSS via embedded
// `<script>`. The accepted bytes go through imageutil.Shrink so a
// polyglot upload dies on re-encoding, the same defence images
// attached to puzzles get.
//
// Mounted under the global-admin route group, mirroring the
// organisations CRUD; the multi-tenant story for org_admin self-
// service waits until a workspace abstraction exists.
func (h *Handlers) SetOrgLogo(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
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
		httpx.Error(w, http.StatusBadRequest,
			"only PNG, JPEG, WebP, or SVG logos are accepted")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "unreadable image: "+err.Error())
		return
	}
	data, mime = normalized, newMIME
	if err := h.Storage.PutOrgLogo(r.Context(), orgID, data, mime); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "organisation not found")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "store failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"url":  orgLogoURL(orgID),
		"mime": mime,
		"size": int64(len(data)),
	})
}

// DeleteOrgLogo clears the logo so future renders fall back to the
// text-only org name on the recap PDF cover.
func (h *Handlers) DeleteOrgLogo(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	if err := h.Storage.DeleteOrgLogo(r.Context(), orgID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "clear failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// ServeOrgLogo writes the stored blob. Public read — same reasoning
// as adventure cover art: the org id in the URL is already known to
// every member, and a logo is the kind of asset companies put on
// their public website. Rate-limit at the proxy if needed.
func (h *Handlers) ServeOrgLogo(w http.ResponseWriter, r *http.Request) {
	idRaw := strings.TrimPrefix(r.URL.Path, "/org-logo/")
	orgID, err := strconv.ParseInt(idRaw, 10, 64)
	if err != nil || orgID <= 0 {
		http.NotFound(w, r)
		return
	}
	content, mime, err := h.Storage.GetOrgLogo(r.Context(), orgID)
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
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write(content)
}
