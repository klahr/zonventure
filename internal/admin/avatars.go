package admin

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/imageutil"
	"github.com/klahr/zonventure/internal/storage"
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

/* ---------------- Event cover (store hero) ---------------- */

// eventCoverURL is the canonical serve path for an event's store
// cover image. Mirrors adventureAvatarURL but keyed by numeric id —
// the public store doesn't have a slug equivalent for events, and
// we don't want to leak the live_token through this read path.
func eventCoverURL(eventID int64) string {
	return "/store-cover/event/" + strconv.FormatInt(eventID, 10)
}

// SetEventCover uploads a hero image for an event's store card. Same
// shape as SetAdventureAvatar — multipart `file`, 5 MB cap, image/*
// only, normalised to PNG/JPEG/WebP via imageutil. Auth-gated via
// authorizeEvent: org_admin and org_member of the owning org pass.
func (h *Handlers) SetEventCover(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
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
	if err := h.Storage.PutEventCover(r.Context(), eventID, data, mime); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "event not found")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "store failed", err)
		return
	}
	if err := h.submitEventListing(r.Context(), eventID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "listing review failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"url":  eventCoverURL(eventID) + "?draft=1",
		"mime": mime,
		"size": int64(len(data)),
	})
}

// DeleteEventCover clears the cover image. The store card falls
// back to a text-only layout.
func (h *Handlers) DeleteEventCover(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	if err := h.Storage.DeleteEventCover(r.Context(), eventID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "clear failed")
		return
	}
	if err := h.submitEventListing(r.Context(), eventID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "listing review failed", err)
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// ServeEventCover writes an event's store cover. No auth: the store
// is a public surface and the buyer needs to see the image before
// committing to a purchase. The numeric event id is a tiny leak (it
// reveals event-creation order) which is the same posture every
// other public-by-id route in this file already takes.
//
// The public gets the approved listing's cover (listing review). With
// ?draft=1 the event's own staff, and platform admins reviewing it,
// get the cover as uploaded.
func (h *Handlers) ServeEventCover(w http.ResponseWriter, r *http.Request) {
	eventID, err := strconv.ParseInt(chi.URLParam(r, "eventID"), 10, 64)
	if err != nil || eventID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if r.URL.Query().Get("draft") == "" {
		h.serveListedEventCover(w, r, eventID)
		return
	}
	me, ok := auth.FromContext(r.Context())
	orgID, oerr := h.eventOrg(r.Context(), eventID)
	if !ok || oerr != nil || !(me.IsAdmin || sameOrg(me, orgID)) {
		http.NotFound(w, r)
		return
	}
	content, mime, err := h.Storage.GetEventCover(r.Context(), eventID)
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
	// public, since /store hits this with no cookie. 5-minute window
	// matches how often arrangers tweak a cover during setup.
	w.Header().Set("Cache-Control", "private, no-cache")
	_, _ = w.Write(content)
}

// serveListedEventCover writes the cover of an event's approved store
// listing: what the public may see.
func (h *Handlers) serveListedEventCover(w http.ResponseWriter, r *http.Request, eventID int64) {
	var (
		mime    string
		content []byte
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT cover_mime_type, cover_content FROM event_listings
		WHERE event_id = $1 AND cover_mime_type IS NOT NULL AND cover_content IS NOT NULL`, eventID).Scan(&mime, &content)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	// public, since /store hits this with no cookie. 5-minute window
	// matches how often a cover changes once it is approved.
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(content)
}
