package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/imageutil"
	"github.com/go-chi/chi/v5"
)

/* ---------------- Page images (upload + edit + delete) ---------------- */

// AddPageImage reads the multipart "file" form field and stores the blob.
func (h *Handlers) AddPageImage(w http.ResponseWriter, r *http.Request) {
	pageID, ok := pageIDFromURL(r)
	if !ok {
		return
	}
	if !h.requireEditPage(w, r, pageID) {
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
	if !strings.HasPrefix(mime, "image/") {
		httpx.Error(w, http.StatusBadRequest, "only image uploads are accepted")
		return
	}
	// Cap both dimensions to 512 px before storing — the player client
	// renders images at phone-width so there's no point storing camera
	// originals; this also keeps the SQLite row small.
	shrunk, newMIME, err := imageutil.Shrink(data, imageutil.DefaultMaxDim)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "unreadable image: "+err.Error())
		return
	}
	data, mime = shrunk, newMIME

	var nextIdx int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(idx)+1, 0) FROM adventure_page_images WHERE page_id = $1`, pageID).
		Scan(&nextIdx); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "next index lookup failed")
		return
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO adventure_page_images(page_id, idx, mime_type, size_bytes, content)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, pageID, nextIdx, mime, int64(len(data)), data).Scan(&id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "insert failed: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, imageView{
		ID: id, Index: nextIdx, MimeType: mime, SizeBytes: int64(len(data)),
		URL: fmt.Sprintf("/adventure-assets/image/%d", id),
	})
}

// UpdatePageImage edits the metadata of an image — currently just the
// alt text. The blob itself is replaced by deleting and re-uploading.
type updatePageImageRequest struct {
	AltText *string `json:"alt_text,omitempty"`
}

func (h *Handlers) UpdatePageImage(w http.ResponseWriter, r *http.Request) {
	imageID, err := strconv.ParseInt(chi.URLParam(r, "imageID"), 10, 64)
	if err != nil || imageID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad image id")
		return
	}
	var pageID int64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT page_id FROM adventure_page_images WHERE id = $1`, imageID).Scan(&pageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "image not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !h.requireEditPage(w, r, pageID) {
		return
	}
	var req updatePageImageRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.AltText != nil {
		// Cap at 500 chars — the WCAG short-text-alternative ceiling
		// most authoring guides converge on. Longer descriptions
		// belong in the prompt body.
		alt := strings.TrimSpace(*req.AltText)
		if len(alt) > 500 {
			httpx.Error(w, http.StatusBadRequest, "alt text too long (max 500 chars)")
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE adventure_page_images SET alt_text = $1 WHERE id = $2`,
			alt, imageID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// DeletePageImage drops one image.
func (h *Handlers) DeletePageImage(w http.ResponseWriter, r *http.Request) {
	imageID, err := strconv.ParseInt(chi.URLParam(r, "imageID"), 10, 64)
	if err != nil || imageID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad image id")
		return
	}
	// Resolve the owning page so we can run the standard policy check.
	var pageID int64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT page_id FROM adventure_page_images WHERE id = $1`, imageID).Scan(&pageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "image not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !h.requireEditPage(w, r, pageID) {
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM adventure_page_images WHERE id = $1`, imageID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "image not found")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

/* ---------------- Image serving ---------------- */

// ServeImage writes the binary image to the response. Admins may fetch
// any image; slots may fetch images for their current page only.
func (h *Handlers) ServeImage(w http.ResponseWriter, r *http.Request) {
	imageID, err := strconv.ParseInt(chi.URLParam(r, "imageID"), 10, 64)
	if err != nil || imageID <= 0 {
		http.Error(w, "bad image id", http.StatusBadRequest)
		return
	}
	var (
		pageID  int64
		mime    string
		size    int64
		content []byte
	)
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT page_id, mime_type, size_bytes, content
		FROM adventure_page_images WHERE id = $1
	`, imageID).Scan(&pageID, &mime, &size, &content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if !h.canViewPageImage(r, pageID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write(content)
}

// canViewPageImage allows any staff user and players whose team is
// currently on the page owning the image.
func (h *Handlers) canViewPageImage(r *http.Request, pageID int64) bool {
	if u, ok := auth.FromContext(r.Context()); ok {
		_ = u
		return true
	}
	slot, ok := auth.SlotFromContext(r.Context())
	if !ok {
		return false
	}
	var (
		advID int64
		idx   int
	)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT adventure_id, idx FROM adventure_pages WHERE id = $1`, pageID).Scan(&advID, &idx); err != nil {
		return false
	}
	// Match up: slot's team is on an event whose adventure_slug → advID, and
	// team.current_page_index == idx.
	var onPage int
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM teams t
		JOIN events ar ON ar.id = t.event_id
		JOIN adventures a ON a.slug = ar.adventure_slug
		WHERE t.id = $1 AND a.id = $2 AND t.current_page_index = $3
	`, slot.TeamID, advID, idx).Scan(&onPage)
	if err != nil {
		return false
	}
	return onPage > 0
}

func loadPageImages(ctx context.Context, db *sql.DB, pageID int64) ([]imageView, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, idx, mime_type, size_bytes, COALESCE(alt_text, '') FROM adventure_page_images
		WHERE page_id = $1 ORDER BY idx
	`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []imageView{}
	for rows.Next() {
		var im imageView
		if err := rows.Scan(&im.ID, &im.Index, &im.MimeType, &im.SizeBytes, &im.AltText); err != nil {
			return nil, err
		}
		im.URL = fmt.Sprintf("/adventure-assets/image/%d", im.ID)
		out = append(out, im)
	}
	return out, nil
}
