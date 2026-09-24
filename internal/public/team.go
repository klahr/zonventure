package public

import (
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/imageutil"
	"github.com/klahr/zonventure/internal/storage"
	"github.com/klahr/zonventure/internal/ws"
	qrcode "github.com/skip2/go-qrcode"
)

// maxTeamAvatarBytes caps uploads from the share page. Matches the
// admin-side limit so both entry points behave the same.
const maxTeamAvatarBytes = 5 * 1024 * 1024

// teamShareView is the data passed to the share-page template.
type teamShareView struct {
	TeamName       string
	AdventureTitle string
	AvatarURL      string
	Token          string
	Members        []teamShareSlot
	// State, ScheduledAt and StartedAt feed the countdown card. State
	// is the event's lifecycle code; ScheduledAt is the server-side
	// "YYYY-MM-DD HH:MM:SS" UTC string, StartedAt the same. The inline
	// JS appends 'Z' before parsing so the browser interprets them as
	// UTC, then ticks down against Date.now() in local time.
	State       string
	ScheduledAt string
	StartedAt   string
}

type teamShareSlot struct {
	Name     string
	MagicURL string
}

// TeamShare serves the one-URL-per-team page. The arranger sends this
// single URL to the team captain; each member taps their own name to
// claim the slot. Unauthenticated (the token IS the auth): anyone with
// the URL can see every slot's magic link, which is exactly what team
// distribution needs.
func (h *Handlers) TeamShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		http.Error(w, "bad token", http.StatusBadRequest)
		return
	}
	var (
		view  teamShareView
		title sql.NullString
	)
	var (
		teamID    int64
		hasAvatar int
	)
	var (
		schedAt sql.NullString
		startAt sql.NullString
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT t.id, t.name, a.title, (t.avatar_mime_type IS NOT NULL)::int AS has_avatar,
		       ar.state, ar.scheduled_at, ar.started_at
		FROM teams t
		JOIN events ar ON ar.id = t.event_id
		LEFT JOIN adventures a ON a.slug = ar.adventure_slug
		WHERE t.token = $1
	`, token).Scan(&teamID, &view.TeamName, &title, &hasAvatar,
		&view.State, &schedAt, &startAt)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "team not found — ask your arranger for a fresh link", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	view.AdventureTitle = title.String
	view.Token = token
	view.ScheduledAt = schedAt.String
	view.StartedAt = startAt.String
	if hasAvatar != 0 {
		view.AvatarURL = fmt.Sprintf("/adventure-assets/team-avatar/%d", teamID)
	}

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT ps.name, ps.token
		FROM player_slots ps
		JOIN teams t ON t.id = ps.team_id
		WHERE t.token = $1
		ORDER BY LOWER(ps.name)
	`, token)
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var name, slotTok string
		if err := rows.Scan(&name, &slotTok); err != nil {
			http.Error(w, "scan failed", http.StatusInternalServerError)
			return
		}
		view.Members = append(view.Members, teamShareSlot{
			Name: name, MagicURL: "/play?t=" + slotTok,
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := teamShareTmpl.Execute(w, view); err != nil {
		// Already wrote some bytes most likely; nothing useful to report.
		_ = err
	}
}

// teamByToken resolves a share token to (teamID, eventID). Empty/missing
// tokens return (0, 0, false) with the HTTP error already written.
// Used by the share page upload/delete handlers — the token IS the
// authentication so there's no Role guard around them.
func (h *Handlers) teamByToken(w http.ResponseWriter, r *http.Request, token string) (int64, int64, bool) {
	if token == "" {
		httpx.Error(w, http.StatusBadRequest, "bad token")
		return 0, 0, false
	}
	var (
		teamID  int64
		eventID int64
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT id, event_id FROM teams WHERE token = $1
	`, token).Scan(&teamID, &eventID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "team not found")
		return 0, 0, false
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return 0, 0, false
	}
	return teamID, eventID, true
}

// SetTeamAvatarByToken lets the team itself upload a profile picture
// via its share URL. Multipart POST with a `file` field. Same size and
// MIME constraints as the admin-side endpoint.
func (h *Handlers) SetTeamAvatarByToken(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	teamID, eventID, ok := h.teamByToken(w, r, token)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(maxTeamAvatarBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad multipart form")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field required")
		return
	}
	defer f.Close()
	if hdr.Size > maxTeamAvatarBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "image too large")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxTeamAvatarBytes+1))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read failed")
		return
	}
	if int64(len(data)) > maxTeamAvatarBytes {
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
	if err := h.Storage.PutTeamAvatar(r.Context(), teamID, data, mime); err != nil {
		// teamByToken already proved the row exists; an ErrNotFound
		// here means the team was deleted mid-upload — surface as 404.
		if errors.Is(err, storage.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "team not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "store failed: "+err.Error())
		return
	}
	// Tell any listening run detail / live board that this team's
	// picture changed so they re-fetch and re-render.
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"url":  fmt.Sprintf("/adventure-assets/team-avatar/%d", teamID),
		"mime": mime,
		"size": int64(len(data)),
	})
}

// TeamShareQR renders the team-share URL as a PNG QR code. The token
// IS the auth (anyone with the URL can already see the share page
// itself), so we don't gate access here. Cached for a minute to spare
// the encoder work when the captain's phone re-fetches the image on
// rotation / wake.
func (h *Handlers) TeamShareQR(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		http.Error(w, "bad token", http.StatusBadRequest)
		return
	}
	// Build the absolute team-share URL the QR encodes. Read only
	// from h.PublicURL — request headers like X-Forwarded-Host
	// could otherwise be poisoned by a hostile reverse proxy and
	// the printed QR would point arrangers + players at a phishing
	// origin. No request-host fallback: a QR pointing at "localhost"
	// or whatever the operator's machine name happens to be is
	// useless to anyone scanning it on a different device, and the
	// 503 here pushes the operator to set ZV_PUBLIC_URL before the
	// QR is printed and handed out. Same posture as buildResetLink.
	base := strings.TrimRight(h.PublicURL, "/")
	if base == "" {
		http.Error(w, "operator must set ZV_PUBLIC_URL before generating share QR codes", http.StatusServiceUnavailable)
		return
	}
	target := base + "/team/" + token
	png, err := qrcode.Encode(target, qrcode.Medium, 360)
	if err != nil {
		http.Error(w, "qr encode failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=60")
	_, _ = w.Write(png)
}

// DeleteTeamAvatarByToken clears the team's profile picture via the
// share URL.
func (h *Handlers) DeleteTeamAvatarByToken(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	teamID, eventID, ok := h.teamByToken(w, r, token)
	if !ok {
		return
	}
	if err := h.Storage.DeleteTeamAvatar(r.Context(), teamID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "clear failed")
		return
	}
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// teamShareTmpl renders a tiny standalone page — no app-wide shell.
// The target audience is the team captain on a phone, so touch
// targets are generous and nothing requires JS. Escaping goes through
// html/template so team + member names can't break out of attributes.
var teamShareTmpl = template.Must(template.New("team").Parse(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<title>{{.TeamName}} · Zonventure</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Aclonica&family=Open+Sans:wght@400;600;700;800&display=swap" rel="stylesheet">
<style>
:root { color-scheme: light dark; --accent:#ad1357; --accent-hover:#951048; --accent-fg:#fff; --ink:#1f3349; --bg:#fbfbfd; --muted:#6b7f96; --surface:#ffffff; --border:#c6d2e166; }
@media (prefers-color-scheme: dark) { :root { --bg:#14202e; --ink:#e7ecf4; --muted:#9db1cc; --accent:#e24080; --accent-hover:#ad1357; --surface:#1f3349; --border:#3a517266; } }
* { box-sizing: border-box; }
body { font-family:'Open Sans',system-ui,sans-serif; color:var(--ink); background:var(--bg); margin:0; min-height:100vh; display:grid; place-items:center; padding:1rem; padding-bottom: calc(1rem + env(safe-area-inset-bottom)); }
.wrap { width:100%; max-width:440px; }
.brand { font-family:'Aclonica',Georgia,serif; color:var(--accent); text-align:center; font-size:1.6rem; margin:0 0 .25rem; }
.sub { color:var(--muted); text-align:center; text-transform:uppercase; letter-spacing:.12em; font-size:.75rem; font-weight:700; margin:0 0 1.5rem; }
.team { text-align:center; font-size: 1.35rem; font-weight: 800; margin:0 0 .1rem; }
.adv { text-align:center; color:var(--muted); margin: 0 0 1.5rem; }
.hint { color:var(--muted); font-size:.9rem; margin:0 0 1rem; }
.slot { display:block; padding: 1.1rem 1.25rem; margin: .6rem 0; background: var(--surface); border:1px solid var(--border); border-radius: 18px; text-decoration:none; color:var(--ink); font-weight:700; font-size:1.1rem; box-shadow: 0 2px 12px rgba(0,0,0,.07); transition: transform 120ms, box-shadow 120ms; }
.slot:hover { transform: translateY(-1px); box-shadow: 0 4px 18px rgba(0,0,0,.1); border-color: var(--accent); }
.slot:active { transform: translateY(1px); }
.slot .name { display:block; font-size: 1.2rem; color: var(--accent); }
.slot .go { display:block; color: var(--muted); font-size:.85rem; font-weight:400; margin-top:.2rem; }
.footer { text-align:center; color: var(--muted); font-size:.75rem; margin-top: 1.5rem; }
.empty { text-align:center; color: var(--muted); padding: 2rem 0; }
/* Team photo: big circular target at the top of the page. Tap = file
   picker. When a photo is set, a small ✕ overlays to remove it. */
.avatar-wrap { display: flex; justify-content: center; margin: 0 0 .9rem; }
.avatar-btn {
  position: relative;
  width: 96px; height: 96px; border-radius: 50%;
  background: var(--accent); color: var(--accent-fg);
  border: 3px solid var(--accent);
  cursor: pointer;
  display: grid; place-items: center;
  /* Body font — matches the initials on the arranger/live team avatars
     so the default rendering is consistent everywhere. */
  font-family: 'Open Sans', system-ui, sans-serif;
  font-size: 2rem; font-weight: 800;
  letter-spacing: .04em; text-transform: uppercase;
  box-shadow: 0 4px 16px rgba(0,0,0,.18);
  transition: transform 120ms, box-shadow 120ms;
  padding: 0;
}
.avatar-btn:hover { transform: translateY(-1px); box-shadow: 0 6px 22px rgba(0,0,0,.22); }
.avatar-btn:active { transform: translateY(1px); }
/* The photo is clipped to the circle, not the button: the pill sits
   over the button's edge and would be cut off with it. */
.avatar-btn img { width: 100%; height: 100%; object-fit: cover; display: block; border-radius: 50%; }
.avatar-btn .hint-pill {
  position: absolute; bottom: -2px; right: -2px;
  background: var(--surface); color: var(--ink);
  border: 2px solid var(--accent);
  border-radius: 999px; padding: .15rem .45rem;
  font-family: 'Open Sans', sans-serif; font-size: .7rem; font-weight: 700;
  letter-spacing: normal; text-transform: none; white-space: nowrap;
}
.avatar-remove {
  display: block; margin: .35rem auto 0;
  background: none; border: none;
  color: var(--muted); font: inherit; font-size: .8rem;
  cursor: pointer; padding: .25rem .5rem;
}
.avatar-remove:hover { color: var(--accent); }
.avatar-remove[hidden] { display: none; }
.avatar-err { color: #d00; font-size: .85rem; text-align: center; margin: .25rem 0 0; min-height: 1em; }
/* Accessibility utilities — skip link + universal focus ring. The
   share page is a one-shot static template, so styles live inline
   here rather than the shared arrange/play/live stylesheets. */
.visually-hidden {
  position: absolute !important;
  width: 1px; height: 1px;
  padding: 0; margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
.skip-link {
  position: fixed;
  /* Hidden by its own height, not a fixed offset, which a taller
     link peeked out from under. */
  top: 0; left: .5rem;
  transform: translateY(-150%);
  z-index: 10000;
  padding: .5rem 1rem;
  background: var(--accent); color: var(--accent-fg);
  border-radius: 8px;
  font-weight: 700; text-decoration: none;
  box-shadow: 0 2px 6px rgba(0,0,0,.15);
  transition: transform 120ms ease;
}
.skip-link:focus, .skip-link:focus-visible { transform: translateY(.5rem); }
:focus-visible {
  outline: 3px solid color-mix(in srgb, var(--accent) 50%, transparent);
  outline-offset: 2px;
}
/* Single-use warning callout: orange-tinted, prominent above the
   slots so a captain re-sharing the same magic link to multiple
   people sees the risk before they tap. */
.warn {
  margin: 0 0 1rem;
  padding: .65rem .8rem;
  background: color-mix(in srgb, #f0a000 18%, var(--surface));
  border: 1px solid color-mix(in srgb, #f0a000 50%, transparent);
  border-radius: 10px;
  color: var(--ink);
  font-size: .85rem;
}
.warn strong { color: #6b4500; }
@media (prefers-color-scheme: dark) {
  .warn { background: color-mix(in srgb, #f0a000 14%, var(--surface)); }
  .warn strong { color: #ffd58a; }
}
/* QR card: collapsed by default to keep the slot list as the
   primary CTA. Captain expands when they want to share the page
   in person. */
.qr-card { margin: 1rem 0 0; border: 1px solid var(--border); border-radius: 14px; padding: .8rem 1rem; background: var(--surface); }
.qr-card summary { cursor: pointer; font-weight: 700; list-style: none; }
.qr-card summary::-webkit-details-marker { display: none; }
.qr-card summary::before { content: '▸'; display: inline-block; margin-right: .35rem; transition: transform .15s ease; color: var(--muted); }
.qr-card[open] summary::before { transform: rotate(90deg); }
.qr-card .qr-body { margin-top: .8rem; text-align: center; }
.qr-card .qr-body img { width: 240px; height: 240px; max-width: 100%; image-rendering: pixelated; background: #fff; padding: .25rem; border-radius: 8px; }
.qr-card .qr-body p { color: var(--muted); font-size: .85rem; margin: .6rem 0 0; }
.copy-row { display: flex; gap: .5rem; margin: 0 0 1rem; }
.copy-row input { flex: 1; min-width: 0; padding: .55rem .7rem; border: 1px solid var(--border); border-radius: 10px; background: var(--surface); color: var(--ink); font: inherit; font-size: .85rem; }
.copy-row button { padding: .55rem .9rem; border-radius: 10px; border: 1px solid var(--accent); background: var(--accent); color: var(--accent-fg); font: inherit; font-weight: 700; cursor: pointer; }
.copy-row button:hover { background: var(--accent-hover); }
.copy-toast { position: fixed; left: 50%; bottom: 4rem; transform: translateX(-50%); background: var(--ink); color: var(--bg); padding: .55rem .9rem; border-radius: 999px; font-size: .85rem; opacity: 0; transition: opacity .2s ease; pointer-events: none; }
.copy-toast.show { opacity: 1; }
/* Countdown card. Sits between the warn callout and the slot list so a
   captain glances it on first load. The digits use a tabular monospace
   so the layout doesn't shift each second. */
.cd { margin: 0 0 1rem; padding: .8rem 1rem; background: var(--surface); border: 1px solid var(--border); border-radius: 12px; text-align: center; }
.cd .cd-label { color: var(--muted); text-transform: uppercase; letter-spacing: .12em; font-size: .7rem; font-weight: 700; margin: 0 0 .3rem; }
.cd .cd-value { font-family: ui-monospace, 'SF Mono', Menlo, monospace; font-variant-numeric: tabular-nums; font-size: 1.6rem; font-weight: 800; color: var(--ink); margin: 0; }
.cd.live .cd-value { color: var(--accent); }
.cd .cd-when { color: var(--muted); font-size: .8rem; margin: .25rem 0 0; }
</style></head>
<body>
<a class="skip-link" href="#main">Skip to your link</a>
<main id="main" class="wrap" tabindex="-1" aria-label="Team links">
  <h1 class="brand">zonventure</h1>
  <p class="sub">Team</p>
  <p class="team">{{.TeamName}}</p>
  {{if .AdventureTitle}}<p class="adv">{{.AdventureTitle}}</p>{{end}}

  <div class="avatar-wrap">
    <div style="text-align:center">
      <button type="button" id="avatar-btn" class="avatar-btn" aria-label="Upload team photo">
        {{if .AvatarURL}}<img id="avatar-img" src="{{.AvatarURL}}" alt="">{{else}}<span id="avatar-ini"></span>{{end}}
        <span class="hint-pill">{{if .AvatarURL}}change{{else}}+ photo{{end}}</span>
      </button>
      <button type="button" id="avatar-remove" class="avatar-remove"{{if not .AvatarURL}} hidden{{end}}>Remove photo</button>
      <p id="avatar-err" class="avatar-err"></p>
    </div>
    <input type="file" id="avatar-file" accept="image/*" style="display:none">
  </div>

  <p class="warn">
    <strong>Each link below is for one person only.</strong>
    Tap your own name; don't share your personal link with teammates — they have their own.
  </p>

  <div id="cd" class="cd" hidden>
    <p class="cd-label" id="cd-label">Starts in</p>
    <p class="cd-value" id="cd-value">—</p>
    <p class="cd-when" id="cd-when"></p>
  </div>
  <div class="copy-row">
    <input id="team-url" type="text" readonly aria-label="Team page URL">
    <button type="button" id="copy-team-url">Copy link</button>
  </div>
  {{if .Members}}
    {{range .Members}}
      <a class="slot" href="{{.MagicURL}}">
        <span class="name">{{.Name}}</span>
        <span class="go">Open my link →</span>
      </a>
    {{end}}
  {{else}}
    <p class="empty">No members on this team yet. Ask your arranger to add you.</p>
  {{end}}

  <details class="qr-card">
    <summary>Show QR code for nearby teammates</summary>
    <div class="qr-body">
      <img src="/qr/team/{{.Token}}" alt="QR code linking to this team page" loading="lazy">
      <p>Anyone who scans this lands on the same page and taps their own name. Don't share this QR with people not on your team.</p>
    </div>
  </details>

  <p class="footer">If your name isn't listed, ask the arranger to add you.</p>
</main>

<div id="copy-toast" class="copy-toast" role="status" aria-live="polite"></div>

<script>
// Tiny inline module — the share page is a standalone template, so no
// shared JS runtime. Handles the avatar upload + remove against the
// token-authed endpoints.
(function () {
  // html/template's <script> context auto-quotes string values as JS
  // literals, so pass .Token / .TeamName straight through. printf "%q"
  // would double-quote and produce strings that still carry the quotes
  // (then initials(" Alfa") reads "A" instead of "TA").
  var token = {{.Token}};
  var teamName = {{.TeamName}};
  var eventState = {{.State}};
  var scheduledAt = {{.ScheduledAt}};
  var startedAt = {{.StartedAt}};
  var btn = document.getElementById('avatar-btn');
  var file = document.getElementById('avatar-file');
  var removeBtn = document.getElementById('avatar-remove');
  var errEl = document.getElementById('avatar-err');
  var iniEl = document.getElementById('avatar-ini');
  if (iniEl) iniEl.textContent = initials(teamName);

  btn.addEventListener('click', function () { file.click(); });
  file.addEventListener('change', function () {
    var f = file.files && file.files[0];
    if (!f) return;
    errEl.textContent = '';
    var fd = new FormData();
    fd.append('file', f);
    fetch('/api/public/team/' + encodeURIComponent(token) + '/avatar', {
      method: 'POST', body: fd, credentials: 'same-origin',
    }).then(function (res) {
      if (!res.ok) return res.text().then(function (txt) { throw new Error(txt || ('error ' + res.status)); });
      return res.json();
    }).then(function (body) {
      // Cache-bust so the player + arranger views don't show the old
      // picture until the 60s cache TTL expires.
      var url = (body.url || ('/adventure-assets/team-avatar/' + Date.now())) + '?v=' + Date.now();
      paintPhoto(url);
      removeBtn.hidden = false;
    }).catch(function (e) {
      errEl.textContent = e.message || 'upload failed';
    }).then(function () { file.value = ''; });
  });
  removeBtn.addEventListener('click', function () {
    errEl.textContent = '';
    fetch('/api/public/team/' + encodeURIComponent(token) + '/avatar', {
      method: 'DELETE', credentials: 'same-origin',
    }).then(function (res) {
      if (res.status !== 204 && !res.ok) throw new Error('error ' + res.status);
      paintInitials();
      removeBtn.hidden = true;
    }).catch(function (e) { errEl.textContent = e.message || 'remove failed'; });
  });

  function paintPhoto(url) {
    btn.innerHTML = '';
    var img = new Image();
    img.id = 'avatar-img';
    img.alt = '';
    img.src = url;
    btn.appendChild(img);
    var pill = document.createElement('span');
    pill.className = 'hint-pill';
    pill.textContent = 'change';
    btn.appendChild(pill);
  }
  function paintInitials() {
    btn.innerHTML = '';
    var span = document.createElement('span');
    span.id = 'avatar-ini';
    span.textContent = initials(teamName);
    btn.appendChild(span);
    var pill = document.createElement('span');
    pill.className = 'hint-pill';
    pill.textContent = '+ photo';
    btn.appendChild(pill);
  }
  function initials(name) {
    var parts = (name || '').trim().split(/\s+/).filter(Boolean);
    if (parts.length === 0) return '?';
    if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
    return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
  }

  // Copy-team-link: prefill the read-only input with the current page
  // URL and wire the button to the clipboard API. Falls back to
  // selecting the input on copy failure (older Safari, locked-down
  // PWAs) so the user can copy by hand.
  var urlEl = document.getElementById('team-url');
  var copyBtn = document.getElementById('copy-team-url');
  var toast = document.getElementById('copy-toast');
  if (urlEl) urlEl.value = location.href;
  function flashToast(msg) {
    if (!toast) return;
    toast.textContent = msg;
    toast.classList.add('show');
    setTimeout(function () { toast.classList.remove('show'); }, 1800);
  }
  if (copyBtn && urlEl) {
    copyBtn.addEventListener('click', function () {
      var url = urlEl.value;
      var ok = function () { flashToast('Link copied'); };
      var fail = function () { urlEl.select(); flashToast('Press ⌘/Ctrl+C to copy'); };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(url).then(ok, fail);
      } else { fail(); }
    });
  }

  // Countdown card. Pre-start: ticks down to scheduled_at. Running:
  // shows "Live now" (no clock work). Completed: shows "Completed".
  // No event-state info → leave hidden.
  var cd = document.getElementById('cd');
  var cdLabel = document.getElementById('cd-label');
  var cdValue = document.getElementById('cd-value');
  var cdWhen = document.getElementById('cd-when');

  function parseUTC(s) {
    if (!s) return null;
    var iso = s.indexOf('T') >= 0 ? s : s.replace(' ', 'T') + 'Z';
    var d = new Date(iso);
    return isNaN(d.getTime()) ? null : d;
  }
  function pad(n) { return n < 10 ? '0' + n : '' + n; }
  function fmtCountdown(sec) {
    if (sec <= 0) return '00:00';
    var d = Math.floor(sec / 86400);
    var h = Math.floor((sec % 86400) / 3600);
    var m = Math.floor((sec % 3600) / 60);
    var s = sec % 60;
    if (d > 0) return d + 'd ' + pad(h) + ':' + pad(m) + ':' + pad(s);
    if (h > 0) return pad(h) + ':' + pad(m) + ':' + pad(s);
    return pad(m) + ':' + pad(s);
  }
  function fmtClock(d) {
    return d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short', hour12: false });
  }

  if (cd) {
    if (eventState === 'completed') {
      cd.hidden = false;
      cdLabel.textContent = 'Event';
      cdValue.textContent = 'Completed';
      cdWhen.textContent = '';
    } else if (eventState === 'running' || eventState === 'paused') {
      cd.hidden = false;
      cd.classList.add('live');
      cdLabel.textContent = 'Event';
      cdValue.textContent = eventState === 'paused' ? 'Paused' : 'Live now';
      var startD = parseUTC(startedAt);
      cdWhen.textContent = startD ? 'Started ' + fmtClock(startD) : '';
    } else {
      var when = parseUTC(scheduledAt);
      if (when) {
        cd.hidden = false;
        cdLabel.textContent = 'Starts in';
        cdWhen.textContent = fmtClock(when);
        var tick = function () {
          var sec = Math.round((when.getTime() - Date.now()) / 1000);
          if (sec <= 0) {
            cd.classList.add('live');
            cdLabel.textContent = 'Event';
            cdValue.textContent = 'Starting…';
            cdWhen.textContent = '';
            return false;
          }
          cdValue.textContent = fmtCountdown(sec);
          return true;
        };
        if (tick()) {
          var iv = setInterval(function () {
            if (!tick()) clearInterval(iv);
          }, 1000);
        }
      } else {
        // No scheduled_at and not yet running — arranger will start it
        // manually. Show a status line rather than a misleading clock.
        cd.hidden = false;
        cdLabel.textContent = 'Event';
        cdValue.textContent = 'Awaiting start';
        cdWhen.textContent = '';
      }
    }
  }
})();
</script>
</body></html>`))
