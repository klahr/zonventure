package admin

// Media for graph adventures: pictures and sounds a step or hint shows.
//
//	POST /api/admin/adventures/{slug}/media   upload one file (multipart "file")
//	GET  /media/{mediaID}                     fetch it
//
// Images are re-encoded by imageutil.Shrink, which caps their size and kills any
// polyglot; audio is stored verbatim, so its type is sniffed from the
// bytes and the client's header is ignored.
//
// Fetching is gated. Staff see media of adventures they can see. A player sees media on a step their team has reached or in
// a hint it has revealed — the graph decides, through the same session
// rules the engine plays by.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/imageutil"
	"github.com/klahr/zonventure/internal/tokenx"
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

type mediaView struct {
	ID       int64           `json:"id"`
	Kind     graph.MediaKind `json:"kind"`
	MimeType string          `json:"mime_type"`
	Size     int64           `json:"size_bytes"`
}

// maxVideoBytes is the per-upload size cap for a video block's film.
// Defaults to 30 MiB: a minute or two of phone video, which is all a
// step should ask a team to watch standing in the street. Override with
// ZV_MAX_VIDEO_BYTES; non-positive falls back.
var maxVideoBytes int64 = func() int64 {
	const def int64 = 30 * 1024 * 1024
	if s := os.Getenv("ZV_MAX_VIDEO_BYTES"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return def
}()

// UploadAdventureMedia stores one image, audio or video file for the
// adventure. Videos are for video blocks; the validator keeps them off
// hints.
func (h *Handlers) UploadAdventureMedia(w http.ResponseWriter, r *http.Request) {
	advID, ok := h.graphTarget(w, r)
	if !ok {
		return
	}
	limit := max(maxImageBytes, maxAudioBytes, maxVideoBytes)
	if err := r.ParseMultipartForm(limit); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad multipart form")
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read failed")
		return
	}

	var kind graph.MediaKind
	mime := http.DetectContentType(data)
	switch {
	case strings.HasPrefix(mime, "image/"):
		if int64(len(data)) > maxImageBytes {
			httpx.Error(w, http.StatusRequestEntityTooLarge, "image too large")
			return
		}
		shrunk, newMIME, err := imageutil.ShrinkKeepAlpha(data, imageutil.DefaultMaxDim)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "unreadable image: "+err.Error())
			return
		}
		kind, data, mime = graph.MediaImage, shrunk, newMIME
	case strings.HasPrefix(mime, "audio/"):
		if int64(len(data)) > maxAudioBytes {
			httpx.Error(w, http.StatusRequestEntityTooLarge, "audio too large")
			return
		}
		kind = graph.MediaAudio
	case strings.HasPrefix(mime, "video/"):
		if int64(len(data)) > maxVideoBytes {
			httpx.Error(w, http.StatusRequestEntityTooLarge, "video too large")
			return
		}
		kind = graph.MediaVideo
	default:
		httpx.Error(w, http.StatusBadRequest, "only image, audio and video uploads are accepted")
		return
	}

	var userID int64
	if me, ok := auth.FromContext(r.Context()); ok {
		userID = me.ID
	}
	v, err := putMedia(r.Context(), h.DB, advID, userID, kind, mime, data)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving media failed", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, v)
}

// putMedia stores content once per adventure: the same bytes again return
// the existing row.
func putMedia(ctx context.Context, db *sql.DB, advID, userID int64, kind graph.MediaKind, mime string, content []byte) (mediaView, error) {
	sum := sha256.Sum256(content)
	var user any
	if userID > 0 {
		user = userID
	}
	v := mediaView{Kind: kind, MimeType: mime, Size: int64(len(content))}
	err := db.QueryRowContext(ctx, `
		INSERT INTO adventure_media (adventure_id, kind, mime_type, size_bytes, sha256, content, created_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (adventure_id, sha256) DO UPDATE SET adventure_id = EXCLUDED.adventure_id
		RETURNING id
	`, advID, string(kind), mime, v.Size, hex.EncodeToString(sum[:]), content, user).Scan(&v.ID)
	return v, err
}

// ServeMedia streams one media file to a caller allowed to see it.
func (h *Handlers) ServeMedia(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "mediaID"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "bad media id", http.StatusBadRequest)
		return
	}
	// What the file is and whose, without its bytes: those are read only
	// for a caller allowed them, and not at all when theirs is current.
	var (
		mime      string
		sum       string
		ownerOrg  sql.NullInt64
		published int
		slug      string
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT m.mime_type, m.sha256, a.org_id, a.published, a.slug
		FROM adventure_media m JOIN adventures a ON a.id = m.adventure_id
		WHERE m.id = $1
	`, id).Scan(&mime, &sum, &ownerOrg, &published, &slug)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}

	allowed := false
	if me, ok := auth.FromContext(r.Context()); ok {
		allowed = h.staffCanSeeAdventure(r.Context(), me, ownerOrg, published != 0, slug)
	} else if slot, ok := auth.SlotFromContext(r.Context()); ok {
		allowed, err = h.engine().CanSeeMedia(r.Context(), slot.TeamID, id)
		if err != nil {
			http.Error(w, "lookup failed", http.StatusInternalServerError)
			return
		}
	}
	if !allowed {
		// 404, not 403: a player probing ids ahead must not learn which
		// exist.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	// The content's hash names this version of it: a phone that has it
	// is told so without the file being read or sent again.
	etag := `"` + sum + `"`
	w.Header().Set("ETag", etag)
	if inm := r.Header.Get("If-None-Match"); inm != "" && (inm == etag || inm == "W/"+etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	var content []byte
	if err := h.DB.QueryRowContext(r.Context(), `SELECT content FROM adventure_media WHERE id = $1`, id).Scan(&content); err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	// ServeContent answers Range requests, which a video needs to seek —
	// and which Safari needs before it plays one at all.
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(content))
}

// Upload links: an assistant asks for one (the MCP tool
// get_media_upload_url) and uploads through it with curl, so a picture
// goes from the author's disk to the server without passing through the
// model as base64.
//
//	POST /api/media-upload/{ticket}   multipart "file", as UploadAdventureMedia
const (
	uploadTicketTTL  = 15 * time.Minute
	uploadTicketUses = 20
)

// CreateUploadTicket mints an upload link to the adventure for the
// caller, who must be able to edit it. It returns the link's secret.
func CreateUploadTicket(ctx context.Context, db *sql.DB, slug string, me auth.User) (string, time.Duration, error) {
	advID, _, err := AuthorizeAdventureEdit(ctx, db, slug, me)
	if err != nil {
		return "", 0, err
	}
	secret, err := tokenx.New(32)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256([]byte(secret))
	if _, err := db.ExecContext(ctx, `
		DELETE FROM media_upload_tickets WHERE expires_at < now()
	`); err != nil {
		return "", 0, err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO media_upload_tickets (token_hash, adventure_id, user_id, org_id, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, sum[:], advID, me.ID, me.OrgID, time.Now().Add(uploadTicketTTL))
	return secret, uploadTicketTTL, err
}

// UploadMediaByTicket stores a file sent to an upload link, as its user
// through the same checks as UploadAdventureMedia — which also re-checks
// that they may still edit the adventure.
func (h *Handlers) UploadMediaByTicket(w http.ResponseWriter, r *http.Request) {
	sum := sha256.Sum256([]byte(chi.URLParam(r, "ticket")))
	var (
		userID int64
		orgID  sql.NullInt64
		slug   string
	)
	err := h.DB.QueryRowContext(r.Context(), `
		UPDATE media_upload_tickets t SET uses = uses + 1
		FROM adventures a
		WHERE t.token_hash = $1 AND a.id = t.adventure_id AND t.expires_at > now() AND t.uses < $2
		RETURNING t.user_id, t.org_id, a.slug
	`, sum[:], uploadTicketUses).Scan(&userID, &orgID, &slug)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "this upload link is unknown, used up or expired; ask for a new one")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "upload link lookup failed", err)
		return
	}
	u, err := tokenUser(r.Context(), h.DB, userID, orgID)
	if err != nil {
		httpx.Error(w, http.StatusForbidden, "the user of this upload link can no longer upload")
		return
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("slug", slug)
	ctx := context.WithValue(auth.WithAPIToken(auth.WithUser(r.Context(), u)), chi.RouteCtxKey, rctx)
	h.UploadAdventureMedia(w, r.WithContext(ctx))
}
