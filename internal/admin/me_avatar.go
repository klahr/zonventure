package admin

// A person's own picture:
//
//	POST   /api/admin/me/avatar              multipart "file": set the caller's
//	DELETE /api/admin/me/avatar              clear it: initials again
//	GET    /api/admin/users/{userID}/avatar  serve someone's, to anyone signed in

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/imageutil"
	"github.com/klahr/zonventure/internal/storage"
)

// avatarDim is the largest side a person's picture is kept at: it is
// shown small, so a phone photo need not be kept whole.
const avatarDim = 256

// SetMyAvatar stores the caller's picture, scaled down and re-encoded.
func (h *Handlers) SetMyAvatar(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes+(1<<20))
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
	data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read failed")
		return
	}
	if int64(len(data)) > maxImageBytes {
		httpx.JSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "the picture is too large", "code": "image_too_large"})
		return
	}
	data, mime, err := imageutil.NormalizeUpload(data, hdr.Header.Get("Content-Type"), avatarDim)
	if errors.Is(err, imageutil.ErrUnsupportedMIME) {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "use a PNG, JPEG, WebP or SVG picture", "code": "image_type"})
		return
	}
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "the picture could not be read", "code": "image_unreadable"})
		return
	}
	if err := h.Storage.PutUserAvatar(r.Context(), me.ID, data, mime); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "store failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"avatar_url": auth.AvatarURLs(r.Context(), h.DB, []int64{me.ID})[me.ID]})
}

// DeleteMyAvatar clears the caller's picture.
func (h *Handlers) DeleteMyAvatar(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if err := h.Storage.DeleteUserAvatar(r.Context(), me.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "clear failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ServeUserAvatar writes someone's picture. Anyone signed in may see it:
// people see each other's in the organisations they share. Its address
// is versioned, so it may be cached for long.
func (h *Handlers) ServeUserAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	content, mime, err := h.Storage.GetUserAvatar(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(content)
}
