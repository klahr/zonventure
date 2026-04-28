package admin

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/imageutil"
	"github.com/go-chi/chi/v5"
)

/* ---------------- Adventure avatar ---------------- */

// adventureAvatarURL is the canonical serve path. Keyed by slug so
// callers in DTOs don't need the numeric id.
func adventureAvatarURL(slug string) string {
	return "/adventure-assets/avatar/" + url.PathEscape(slug)
}

// SetAdventureAvatar stores the uploaded cover picture inline on the
// adventures row. Same contract as SetTeamAvatar — multipart `file`
// field, 5 MB cap, image/* only. Authorised via requireEditAdventure.
func (h *Handlers) SetAdventureAvatar(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
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
	res, err := h.DB.ExecContext(r.Context(), `
		UPDATE adventures
		   SET avatar_mime_type = $1, avatar_size_bytes = $2, avatar_content = $3
		 WHERE slug = $4
	`, mime, int64(len(data)), data, slug)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "store failed: "+err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "adventure not found")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"url":  adventureAvatarURL(slug),
		"mime": mime,
		"size": int64(len(data)),
	})
}

// DeleteAdventureAvatar clears the cover picture so the client falls
// back to the initials-on-accent tile.
func (h *Handlers) DeleteAdventureAvatar(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE adventures
		   SET avatar_mime_type = NULL, avatar_size_bytes = NULL, avatar_content = NULL
		 WHERE slug = $1
	`, slug); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "clear failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// ServeAdventureAvatar writes the stored blob. No auth: cover art is
// safe public read, same reasoning as team avatars — the slug is the
// identifier that's already in the URL.
func (h *Handlers) ServeAdventureAvatar(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	var (
		mime    sql.NullString
		size    sql.NullInt64
		content []byte
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT avatar_mime_type, avatar_size_bytes, avatar_content
		FROM adventures WHERE slug = $1
	`, slug).Scan(&mime, &size, &content)
	if errors.Is(err, sql.ErrNoRows) || !mime.Valid || len(content) == 0 {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	w.Header().Set("Content-Type", mime.String)
	w.Header().Set("Content-Length", strconv.FormatInt(size.Int64, 10))
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write(content)
}
