package admin

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/imageutil"
	"git.sr.ht/~klahr/zv/internal/storage"
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
	normalized, newMIME, err := imageutil.NormalizeUpload(data, mime, imageutil.DefaultMaxDim)
	if errors.Is(err, imageutil.ErrUnsupportedMIME) {
		httpx.Error(w, http.StatusBadRequest,
			"only PNG, JPEG, WebP, or SVG covers are accepted")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "unreadable image: "+err.Error())
		return
	}
	data, mime = normalized, newMIME
	if err := h.Storage.PutAdventureAvatar(r.Context(), slug, data, mime); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "adventure not found")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "store failed", err)
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
	if err := h.Storage.DeleteAdventureAvatar(r.Context(), slug); err != nil {
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
	content, mime, err := h.Storage.GetAdventureAvatar(r.Context(), slug)
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
