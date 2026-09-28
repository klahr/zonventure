package admin

// Media for graph adventures: pictures and sounds a step or hint shows.
//
//	POST /api/admin/adventures/{slug}/media   upload one file (multipart "file")
//	GET  /media/{mediaID}                     fetch it
//
// Uploads follow the page-image and page-audio rules exactly: images are
// re-encoded by imageutil.Shrink, which caps their size and kills any
// polyglot; audio is stored verbatim, so its type is sniffed from the
// bytes and the client's header is ignored.
//
// Fetching is gated. Staff see media of adventures they can see, as with
// page images. A player sees media on a step their team has reached or in
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
	"github.com/klahr/zonventure/internal/play"
)

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
	var (
		mime      string
		content   []byte
		ownerOrg  sql.NullInt64
		published int
		slug      string
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT m.mime_type, m.content, a.org_id, a.published, a.slug
		FROM adventure_media m JOIN adventures a ON a.id = m.adventure_id
		WHERE m.id = $1
	`, id).Scan(&mime, &content, &ownerOrg, &published, &slug)
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
		allowed, err = play.New(h.DB).CanSeeMedia(r.Context(), slot.TeamID, id)
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
	// ServeContent answers Range requests, which a video needs to seek —
	// and which Safari needs before it plays one at all.
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(content))
}
