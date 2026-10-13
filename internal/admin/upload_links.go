package admin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/tokenx"
)

// Upload links: an assistant asks for one (the MCP tool get_upload_link)
// and the file goes to the server without passing through the model as
// base64 — sent with curl where the assistant has a shell, or picked on
// the page the link opens, where it has not: on the author's phone.
// A link uploads a step's picture, sound or video to the adventure's
// media, sets the adventure's icon, or sets an event's cover.
//
//	GET  /api/media-upload/{ticket}   the page to pick the file on
//	POST /api/media-upload/{ticket}   multipart "file"
const (
	uploadTicketTTL  = 30 * time.Minute
	uploadTicketUses = 20
)

// Upload link purposes.
const (
	UploadMedia      = "media"
	UploadIcon       = "icon"
	UploadEventCover = "event_cover"
)

// CreateUploadLink mints an upload link for the caller, returning its
// secret. Media and the icon need the caller to be able to edit the
// adventure; an event's cover, the event to be the caller's
// organisation's.
func CreateUploadLink(ctx context.Context, db *sql.DB, me auth.User, purpose, slug string, eventID int64) (string, time.Duration, error) {
	var advID int64
	switch purpose {
	case UploadMedia, UploadIcon:
		id, _, err := AuthorizeAdventureEdit(ctx, db, slug, me)
		if err != nil {
			return "", 0, err
		}
		advID, eventID = id, 0
	case UploadEventCover:
		var org sql.NullInt64
		err := db.QueryRowContext(ctx, `
			SELECT ev.org_id, a.id FROM events ev JOIN adventures a ON a.slug = ev.adventure_slug WHERE ev.id = $1
		`, eventID).Scan(&org, &advID)
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, fmt.Errorf("no event %d", eventID)
		}
		if err != nil {
			return "", 0, err
		}
		if me.OrgID == nil || !org.Valid || org.Int64 != *me.OrgID || !runsEvents(me) {
			return "", 0, ErrForbidden
		}
	default:
		return "", 0, fmt.Errorf("unknown upload purpose %q", purpose)
	}
	secret, err := tokenx.New(32)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256([]byte(secret))
	if _, err := db.ExecContext(ctx, `DELETE FROM media_upload_tickets WHERE expires_at < now()`); err != nil {
		return "", 0, err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO media_upload_tickets (token_hash, adventure_id, user_id, org_id, expires_at, purpose, event_id)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, 0))
	`, sum[:], advID, me.ID, me.OrgID, time.Now().Add(uploadTicketTTL), purpose, eventID)
	return secret, uploadTicketTTL, err
}

type uploadTicket struct {
	userID  int64
	orgID   sql.NullInt64
	slug    string
	title   string
	purpose string
	eventID sql.NullInt64
}

// lookupUploadTicket finds a live link; use counts one upload against it.
func (h *Handlers) lookupUploadTicket(ctx context.Context, secret string, use bool) (uploadTicket, error) {
	sum := sha256.Sum256([]byte(secret))
	var t uploadTicket
	q := `
		SELECT t.user_id, t.org_id, a.slug, a.title, t.purpose, t.event_id
		FROM media_upload_tickets t JOIN adventures a ON a.id = t.adventure_id
		WHERE t.token_hash = $1 AND t.expires_at > now() AND t.uses < $2`
	if use {
		q = `
		UPDATE media_upload_tickets t SET uses = uses + 1
		FROM adventures a
		WHERE t.token_hash = $1 AND a.id = t.adventure_id AND t.expires_at > now() AND t.uses < $2
		RETURNING t.user_id, t.org_id, a.slug, a.title, t.purpose, t.event_id`
	}
	err := h.DB.QueryRowContext(ctx, q, sum[:], uploadTicketUses).Scan(&t.userID, &t.orgID, &t.slug, &t.title, &t.purpose, &t.eventID)
	return t, err
}

const uploadLinkGone = "This upload link is unknown, used up or expired. Ask for a new one."

// UploadPage serves GET /api/media-upload/{ticket}: a page to pick the
// file on, for a phone. It works without scripts.
func (h *Handlers) UploadPage(w http.ResponseWriter, r *http.Request) {
	t, err := h.lookupUploadTicket(r.Context(), chi.URLParam(r, "ticket"), false)
	if errors.Is(err, sql.ErrNoRows) {
		renderUploadPage(w, http.StatusNotFound, uploadView{Error: uploadLinkGone})
		return
	}
	if err != nil {
		httpx500(w, err)
		return
	}
	renderUploadPage(w, http.StatusOK, viewFor(t))
}

// UploadMediaByTicket serves POST /api/media-upload/{ticket}: the file
// sent to an upload link, stored as its user would upload it. A browser
// gets the page back, with how it went; curl gets the JSON.
func (h *Handlers) UploadMediaByTicket(w http.ResponseWriter, r *http.Request) {
	page := strings.Contains(r.Header.Get("Accept"), "text/html")
	t, err := h.lookupUploadTicket(r.Context(), chi.URLParam(r, "ticket"), true)
	if errors.Is(err, sql.ErrNoRows) {
		if page {
			renderUploadPage(w, http.StatusNotFound, uploadView{Error: uploadLinkGone})
			return
		}
		httpx.Error(w, http.StatusNotFound, "this upload link is unknown, used up or expired; ask for a new one")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "upload link lookup failed", err)
		return
	}
	u, err := tokenUser(r.Context(), h.DB, t.userID, t.orgID)
	if err != nil {
		httpx.Error(w, http.StatusForbidden, "the user of this upload link can no longer upload")
		return
	}
	rctx := chi.NewRouteContext()
	handler := h.UploadAdventureMedia
	switch t.purpose {
	case UploadIcon:
		rctx.URLParams.Add("slug", t.slug)
		handler = h.SetAdventureAvatar
	case UploadEventCover:
		rctx.URLParams.Add("eventID", strconv.FormatInt(t.eventID.Int64, 10))
		handler = h.SetEventCover
	default:
		rctx.URLParams.Add("slug", t.slug)
	}
	ctx := context.WithValue(auth.WithAPIToken(auth.WithUser(r.Context(), u)), chi.RouteCtxKey, rctx)
	if !page {
		handler(w, r.WithContext(ctx))
		return
	}
	rec := httptest.NewRecorder()
	handler(rec, r.WithContext(ctx))
	v := viewFor(t)
	if rec.Code >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		v.Error = "That did not work: " + strings.TrimSpace(e.Error)
		if e.Error == "" {
			v.Error = "That did not work. Try another file."
		}
		renderUploadPage(w, rec.Code, v)
		return
	}
	v.Done = true
	renderUploadPage(w, http.StatusOK, v)
}

type uploadView struct {
	Heading, Lead, Accept, Error string
	Done, Again                  bool
}

func viewFor(t uploadTicket) uploadView {
	switch t.purpose {
	case UploadIcon:
		return uploadView{Heading: "The icon for " + t.title, Lead: "Pick a picture: a square one looks best.", Accept: "image/*"}
	case UploadEventCover:
		return uploadView{Heading: "The cover for an event of " + t.title, Lead: "Pick a picture for the event.", Accept: "image/*"}
	}
	return uploadView{Heading: "Add to " + t.title, Lead: "Pick a picture, sound or video for the adventure.", Accept: "image/*,audio/*,video/*", Again: true}
}

var uploadPageTmpl = template.Must(template.New("upload").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Upload · Zonventure</title>
<style>
body{margin:0;font:17px/1.45 system-ui,sans-serif;background:#f6f5f2;color:#1d1d1f}
main{max-width:28rem;margin:0 auto;padding:2rem 1rem}
h1{font-size:1.35rem;margin:0 0 .4rem}
p{margin:.4rem 0 1.2rem;color:#555}
form{display:flex;flex-direction:column;gap:1rem;padding:1.2rem;border-radius:14px;background:#fff;box-shadow:0 1px 3px rgba(0,0,0,.08)}
input[type=file]{font-size:1rem}
button{padding:.85rem;border:0;border-radius:999px;background:#1f6feb;color:#fff;font:600 1rem system-ui,sans-serif}
.ok{padding:1rem;border-radius:12px;background:#e7f6ec;color:#14532d;font-weight:600}
.err{padding:1rem;border-radius:12px;background:#fdecec;color:#7f1d1d}
</style></head><body><main>
{{if .Heading}}<h1>{{.Heading}}</h1>{{end}}
{{if .Done}}<p class="ok">Uploaded. Go back to the chat and tell Claude it is done.</p>{{end}}
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
{{if and .Heading (or (not .Done) .Again)}}
{{if not .Done}}<p>{{.Lead}}</p>{{end}}
<form method="post" enctype="multipart/form-data">
<input type="file" name="file" accept="{{.Accept}}" required>
<button type="submit">{{if .Done}}Upload another{{else}}Upload{{end}}</button>
</form>{{end}}
</main></body></html>`))

func renderUploadPage(w http.ResponseWriter, status int, v uploadView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = uploadPageTmpl.Execute(w, v)
}

func httpx500(w http.ResponseWriter, err error) {
	httpx.ServerErr(w, http.StatusInternalServerError, "upload link lookup failed", err)
}
