package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"github.com/go-chi/chi/v5"
)

// maxAudioBytes is the per-upload size cap for audio. Defaults to 15
// MiB which fits a ~15 min voice memo at 128 kbps or a 5 min music clip
// at MP3 320 kbps — comfortably more than any single puzzle clue should
// need, but not so much that a runaway upload eats Postgres row pages.
// Override with ZV_MAX_AUDIO_BYTES; non-positive falls back.
var maxAudioBytes int64 = func() int64 {
	const def int64 = 15 * 1024 * 1024
	if s := os.Getenv("ZV_MAX_AUDIO_BYTES"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return def
}()

/* ---------------- Page audio (upload + edit + delete) ---------------- */

// AddPageAudio reads the multipart "file" form field and stores the
// blob alongside the page. Mirrors AddPageImage but skips the
// imageutil.Shrink step — audio is stored verbatim and the player
// negotiates the format via the <audio> element's MIME type.
func (h *Handlers) AddPageAudio(w http.ResponseWriter, r *http.Request) {
	pageID, ok := pageIDFromURL(r)
	if !ok {
		return
	}
	if !h.requireEditPage(w, r, pageID) {
		return
	}
	if err := r.ParseMultipartForm(maxAudioBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad multipart form")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field required")
		return
	}
	defer f.Close()
	if hdr.Size > maxAudioBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "audio too large")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxAudioBytes+1))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read failed")
		return
	}
	if int64(len(data)) > maxAudioBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "audio too large")
		return
	}
	mime := hdr.Header.Get("Content-Type")
	if !strings.HasPrefix(mime, "audio/") {
		httpx.Error(w, http.StatusBadRequest, "only audio uploads are accepted")
		return
	}

	var nextIdx int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(idx)+1, 0) FROM adventure_page_audio WHERE page_id = $1`, pageID).
		Scan(&nextIdx); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "next index lookup failed")
		return
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO adventure_page_audio(page_id, idx, mime_type, size_bytes, content)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, pageID, nextIdx, mime, int64(len(data)), data).Scan(&id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "insert failed: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, audioView{
		ID: id, Index: nextIdx, MimeType: mime, SizeBytes: int64(len(data)),
		URL: fmt.Sprintf("/adventure-assets/audio/%d", id),
	})
}

type updatePageAudioRequest struct {
	Description *string `json:"description,omitempty"`
}

// UpdatePageAudio edits an audio row's description. The blob itself is
// replaced by deleting and re-uploading.
func (h *Handlers) UpdatePageAudio(w http.ResponseWriter, r *http.Request) {
	audioID, err := strconv.ParseInt(chi.URLParam(r, "audioID"), 10, 64)
	if err != nil || audioID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad audio id")
		return
	}
	var pageID int64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT page_id FROM adventure_page_audio WHERE id = $1`, audioID).Scan(&pageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "audio not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !h.requireEditPage(w, r, pageID) {
		return
	}
	var req updatePageAudioRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Description != nil {
		desc := strings.TrimSpace(*req.Description)
		// 2000-char ceiling — long enough for a short transcript hint
		// but not so long that the row dwarfs the audio itself. Authors
		// who want a full transcript should put it in the prompt body.
		if len(desc) > 2000 {
			httpx.Error(w, http.StatusBadRequest, "description too long (max 2000 chars)")
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE adventure_page_audio SET description = $1 WHERE id = $2`,
			desc, audioID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// DeletePageAudio drops one audio row.
func (h *Handlers) DeletePageAudio(w http.ResponseWriter, r *http.Request) {
	audioID, err := strconv.ParseInt(chi.URLParam(r, "audioID"), 10, 64)
	if err != nil || audioID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad audio id")
		return
	}
	var pageID int64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT page_id FROM adventure_page_audio WHERE id = $1`, audioID).Scan(&pageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "audio not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !h.requireEditPage(w, r, pageID) {
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM adventure_page_audio WHERE id = $1`, audioID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "audio not found")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

/* ---------------- Audio serving ---------------- */

// ServeAudio writes the binary audio blob. Visibility uses the same
// page-bound rule as ServeImage (see canViewPageImage): admins always
// pass; players whose team is on the owning page pass; everyone else
// gets 403.
func (h *Handlers) ServeAudio(w http.ResponseWriter, r *http.Request) {
	audioID, err := strconv.ParseInt(chi.URLParam(r, "audioID"), 10, 64)
	if err != nil || audioID <= 0 {
		http.Error(w, "bad audio id", http.StatusBadRequest)
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
		FROM adventure_page_audio WHERE id = $1
	`, audioID).Scan(&pageID, &mime, &size, &content); err != nil {
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
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write(content)
}

func loadPageAudio(ctx context.Context, db *sql.DB, pageID int64) ([]audioView, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, idx, mime_type, size_bytes, COALESCE(description, '') FROM adventure_page_audio
		WHERE page_id = $1 ORDER BY idx
	`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []audioView{}
	for rows.Next() {
		var av audioView
		if err := rows.Scan(&av.ID, &av.Index, &av.MimeType, &av.SizeBytes, &av.Description); err != nil {
			return nil, err
		}
		av.URL = fmt.Sprintf("/adventure-assets/audio/%d", av.ID)
		out = append(out, av)
	}
	return out, nil
}

/* ---------------- Hint audio (upload + edit + delete) ---------------- */

// AddHintAudio attaches an audio blob to a hint. Same shape as
// AddPageAudio plus the requireEditHint policy hop.
func (h *Handlers) AddHintAudio(w http.ResponseWriter, r *http.Request) {
	hintID, err := strconv.ParseInt(chi.URLParam(r, "hintID"), 10, 64)
	if err != nil || hintID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad hint id")
		return
	}
	if !h.requireEditHint(w, r, hintID) {
		return
	}
	if err := r.ParseMultipartForm(maxAudioBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad multipart form")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field required")
		return
	}
	defer f.Close()
	if hdr.Size > maxAudioBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "audio too large")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxAudioBytes+1))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read failed")
		return
	}
	if int64(len(data)) > maxAudioBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "audio too large")
		return
	}
	mime := hdr.Header.Get("Content-Type")
	if !strings.HasPrefix(mime, "audio/") {
		httpx.Error(w, http.StatusBadRequest, "only audio uploads are accepted")
		return
	}

	var nextIdx int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(idx)+1, 0) FROM adventure_page_hint_audio WHERE hint_id = $1`, hintID).
		Scan(&nextIdx); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "next index lookup failed")
		return
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO adventure_page_hint_audio(hint_id, idx, mime_type, size_bytes, content)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, hintID, nextIdx, mime, int64(len(data)), data).Scan(&id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "insert failed: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, audioView{
		ID: id, Index: nextIdx, MimeType: mime, SizeBytes: int64(len(data)),
		URL: fmt.Sprintf("/adventure-assets/hint-audio/%d", id),
	})
}

// UpdateHintAudio edits the description on a single hint audio row.
func (h *Handlers) UpdateHintAudio(w http.ResponseWriter, r *http.Request) {
	audioID, err := strconv.ParseInt(chi.URLParam(r, "audioID"), 10, 64)
	if err != nil || audioID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad audio id")
		return
	}
	hintID, err := h.hintIDForHintAudio(r.Context(), audioID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "audio not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !h.requireEditHint(w, r, hintID) {
		return
	}
	var req updatePageAudioRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Description != nil {
		desc := strings.TrimSpace(*req.Description)
		if len(desc) > 2000 {
			httpx.Error(w, http.StatusBadRequest, "description too long (max 2000 chars)")
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE adventure_page_hint_audio SET description = $1 WHERE id = $2`,
			desc, audioID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// DeleteHintAudio drops one hint audio row.
func (h *Handlers) DeleteHintAudio(w http.ResponseWriter, r *http.Request) {
	audioID, err := strconv.ParseInt(chi.URLParam(r, "audioID"), 10, 64)
	if err != nil || audioID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad audio id")
		return
	}
	hintID, err := h.hintIDForHintAudio(r.Context(), audioID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "audio not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !h.requireEditHint(w, r, hintID) {
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM adventure_page_hint_audio WHERE id = $1`, audioID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "audio not found")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// ServeHintAudio writes the blob with the same reveal-gated rule as
// ServeHintImage — staff sessions always pass; slot sessions only
// when the team is on the hint's page AND has actively revealed the
// hint. canViewHintImage is name-misleading but role-pure (it gates
// any per-hint asset), so we reuse it.
func (h *Handlers) ServeHintAudio(w http.ResponseWriter, r *http.Request) {
	audioID, err := strconv.ParseInt(chi.URLParam(r, "audioID"), 10, 64)
	if err != nil || audioID <= 0 {
		http.Error(w, "bad audio id", http.StatusBadRequest)
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
		FROM adventure_page_hint_audio WHERE id = $1
	`, audioID).Scan(&hintID, &mime, &size, &content); err != nil {
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
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write(content)
}

func (h *Handlers) hintIDForHintAudio(ctx context.Context, audioID int64) (int64, error) {
	var hintID int64
	err := h.DB.QueryRowContext(ctx,
		`SELECT hint_id FROM adventure_page_hint_audio WHERE id = $1`, audioID).Scan(&hintID)
	return hintID, err
}

func loadHintAudio(ctx context.Context, db *sql.DB, hintID int64) ([]audioView, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, idx, mime_type, size_bytes, COALESCE(description, '') FROM adventure_page_hint_audio
		WHERE hint_id = $1 ORDER BY idx
	`, hintID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []audioView{}
	for rows.Next() {
		var av audioView
		if err := rows.Scan(&av.ID, &av.Index, &av.MimeType, &av.SizeBytes, &av.Description); err != nil {
			return nil, err
		}
		av.URL = fmt.Sprintf("/adventure-assets/hint-audio/%d", av.ID)
		out = append(out, av)
	}
	return out, nil
}

