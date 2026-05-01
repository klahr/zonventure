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

// canViewPageImage allows any staff user and any player whose team
// is in an event for the same adventure as the page. The previous
// rule pinned visibility to the team's CURRENT page only — that
// blocked proactive offline asset prefetch (the player can't cache
// puzzle 5's image while they're on puzzle 1) without a real
// security upside, since any team member sees every page in author
// order eventually. The reveal-gated path for hint assets still
// uses the stricter canViewHintImage check; that one matters
// because hints are a game mechanic, not just per-page progress.
func (h *Handlers) canViewPageImage(r *http.Request, pageID int64) bool {
	if u, ok := auth.FromContext(r.Context()); ok {
		_ = u
		return true
	}
	slot, ok := auth.SlotFromContext(r.Context())
	if !ok {
		return false
	}
	var advID int64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT adventure_id FROM adventure_pages WHERE id = $1`, pageID).Scan(&advID); err != nil {
		return false
	}
	var inAdventure int
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM teams t
		JOIN events ar ON ar.id = t.event_id
		JOIN adventures a ON a.slug = ar.adventure_slug
		WHERE t.id = $1 AND a.id = $2
	`, slot.TeamID, advID).Scan(&inAdventure)
	if err != nil {
		return false
	}
	return inAdventure > 0
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

/* ---------------- Hint images (upload + edit + delete) ---------------- */
//
// Same shape as the puzzle-image handlers above, hung off the per-
// hint adventure_page_hint_images table. Authoring permission is
// derived from the hint's owning page (same requireEditPage check).
//
// The serve route is deliberately separate from /adventure-assets/
// image/ so the player-side visibility rule can be stricter:
// canViewHintImage requires the team to have actively revealed the
// owning hint, not just to be on the page. Using the same route
// would risk players probing IDs to peek at unrevealed clues.

// requireEditHint resolves the page owning the hint and runs the
// standard edit-permission check on it.
func (h *Handlers) requireEditHint(w http.ResponseWriter, r *http.Request, hintID int64) bool {
	var pageID int64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT page_id FROM adventure_page_hints WHERE id = $1`, hintID).Scan(&pageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "hint not found")
			return false
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return false
	}
	return h.requireEditPage(w, r, pageID)
}

// AddHintImage reads the multipart "file" form field and stores the
// blob alongside the hint.
func (h *Handlers) AddHintImage(w http.ResponseWriter, r *http.Request) {
	hintID, err := strconv.ParseInt(chi.URLParam(r, "hintID"), 10, 64)
	if err != nil || hintID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad hint id")
		return
	}
	if !h.requireEditHint(w, r, hintID) {
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
	shrunk, newMIME, err := imageutil.Shrink(data, imageutil.DefaultMaxDim)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "unreadable image: "+err.Error())
		return
	}
	data, mime = shrunk, newMIME

	var nextIdx int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(idx)+1, 0) FROM adventure_page_hint_images WHERE hint_id = $1`, hintID).
		Scan(&nextIdx); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "next index lookup failed")
		return
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO adventure_page_hint_images(hint_id, idx, mime_type, size_bytes, content)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, hintID, nextIdx, mime, int64(len(data)), data).Scan(&id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "insert failed: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, imageView{
		ID: id, Index: nextIdx, MimeType: mime, SizeBytes: int64(len(data)),
		URL: fmt.Sprintf("/adventure-assets/hint-image/%d", id),
	})
}

// UpdateHintImage edits the alt text on a single hint image. Same
// shape as UpdatePageImage; the blob itself is replaced by delete +
// re-upload.
func (h *Handlers) UpdateHintImage(w http.ResponseWriter, r *http.Request) {
	imageID, err := strconv.ParseInt(chi.URLParam(r, "imageID"), 10, 64)
	if err != nil || imageID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad image id")
		return
	}
	hintID, err := h.hintIDForHintImage(r.Context(), imageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "image not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !h.requireEditHint(w, r, hintID) {
		return
	}
	var req updatePageImageRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.AltText != nil {
		alt := strings.TrimSpace(*req.AltText)
		if len(alt) > 500 {
			httpx.Error(w, http.StatusBadRequest, "alt text too long (max 500 chars)")
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE adventure_page_hint_images SET alt_text = $1 WHERE id = $2`,
			alt, imageID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// DeleteHintImage drops one hint image.
func (h *Handlers) DeleteHintImage(w http.ResponseWriter, r *http.Request) {
	imageID, err := strconv.ParseInt(chi.URLParam(r, "imageID"), 10, 64)
	if err != nil || imageID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad image id")
		return
	}
	hintID, err := h.hintIDForHintImage(r.Context(), imageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "image not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !h.requireEditHint(w, r, hintID) {
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM adventure_page_hint_images WHERE id = $1`, imageID)
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

// ServeHintImage writes the blob. Staff sessions always pass; slot
// sessions only when the team is on the hint's page AND has actively
// revealed the hint. Probing unrevealed hints' image IDs is the
// attack we explicitly close here — see the doc comment on
// canViewHintImage.
func (h *Handlers) ServeHintImage(w http.ResponseWriter, r *http.Request) {
	imageID, err := strconv.ParseInt(chi.URLParam(r, "imageID"), 10, 64)
	if err != nil || imageID <= 0 {
		http.Error(w, "bad image id", http.StatusBadRequest)
		return
	}
	var (
		hintID  int64
		mime    string
		size    int64
		content []byte
	)
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT hint_id, mime_type, size_bytes, content
		FROM adventure_page_hint_images WHERE id = $1
	`, imageID).Scan(&hintID, &mime, &size, &content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if !h.canViewHintImage(r, hintID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write(content)
}

// canViewHintImage allows any staff user (read-through to the
// authoring side) and players whose team (a) is on the page that
// owns the hint AND (b) has revealed the hint at this index. The
// reveal join is the load-bearing check: without it a player could
// guess image IDs and read clues they haven't yet asked for.
func (h *Handlers) canViewHintImage(r *http.Request, hintID int64) bool {
	if _, ok := auth.FromContext(r.Context()); ok {
		return true
	}
	slot, ok := auth.SlotFromContext(r.Context())
	if !ok {
		return false
	}
	var (
		hintIdx int
		pageID  int64
		advID   int64
		pageIdx int
	)
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT h.idx, p.id, p.adventure_id, p.idx
		FROM adventure_page_hints h
		JOIN adventure_pages p ON p.id = h.page_id
		WHERE h.id = $1
	`, hintID).Scan(&hintIdx, &pageID, &advID, &pageIdx); err != nil {
		return false
	}
	var n int
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM teams t
		JOIN events e ON e.id = t.event_id
		JOIN adventures a ON a.slug = e.adventure_slug
		WHERE t.id = $1 AND a.id = $2 AND t.current_page_index = $3
		  AND EXISTS (
		    SELECT 1 FROM team_hint_reveals
		    WHERE team_id = t.id AND page_id = $4 AND hint_index = $5
		  )
	`, slot.TeamID, advID, pageIdx, pageID, hintIdx).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// hintIDForHintImage resolves the owning hint of an image — used by
// the patch/delete handlers above before the permission check.
func (h *Handlers) hintIDForHintImage(ctx context.Context, imageID int64) (int64, error) {
	var hintID int64
	err := h.DB.QueryRowContext(ctx,
		`SELECT hint_id FROM adventure_page_hint_images WHERE id = $1`, imageID).Scan(&hintID)
	return hintID, err
}

// loadHintImages returns one hint's images in author order. Mirrors
// loadPageImages so the front-end imageView shape is identical.
func loadHintImages(ctx context.Context, db *sql.DB, hintID int64) ([]imageView, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, idx, mime_type, size_bytes, COALESCE(alt_text, '') FROM adventure_page_hint_images
		WHERE hint_id = $1 ORDER BY idx
	`, hintID)
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
		im.URL = fmt.Sprintf("/adventure-assets/hint-image/%d", im.ID)
		out = append(out, im)
	}
	return out, nil
}
