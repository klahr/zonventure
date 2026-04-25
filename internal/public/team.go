package public

import (
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/imageutil"
	"git.sr.ht/~klahr/zv/internal/ws"
	"github.com/go-chi/chi/v5"
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
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT t.id, t.name, a.title, (t.avatar_mime_type IS NOT NULL)::int AS has_avatar
		FROM teams t
		JOIN adventure_runs ar ON ar.id = t.run_id
		LEFT JOIN adventures a ON a.slug = ar.adventure_slug
		WHERE t.token = $1
	`, token).Scan(&teamID, &view.TeamName, &title, &hasAvatar)
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
	_ = httpx.Error // keep import useful if we later shift to JSON
}

// teamByToken resolves a share token to (teamID, runID). Empty/missing
// tokens return (0, 0, false) with the HTTP error already written.
// Used by the share page upload/delete handlers — the token IS the
// authentication so there's no Role guard around them.
func (h *Handlers) teamByToken(w http.ResponseWriter, r *http.Request, token string) (int64, int64, bool) {
	if token == "" {
		httpx.Error(w, http.StatusBadRequest, "bad token")
		return 0, 0, false
	}
	var (
		teamID int64
		runID  int64
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT id, run_id FROM teams WHERE token = $1
	`, token).Scan(&teamID, &runID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "team not found")
		return 0, 0, false
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return 0, 0, false
	}
	return teamID, runID, true
}

// SetTeamAvatarByToken lets the team itself upload a profile picture
// via its share URL. Multipart POST with a `file` field. Same size and
// MIME constraints as the admin-side endpoint.
func (h *Handlers) SetTeamAvatarByToken(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	teamID, runID, ok := h.teamByToken(w, r, token)
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
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE teams
		   SET avatar_mime_type = $1, avatar_size_bytes = $2, avatar_content = $3
		 WHERE id = $4
	`, mime, int64(len(data)), data, teamID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "store failed: "+err.Error())
		return
	}
	// Tell any listening run detail / live board that this team's
	// picture changed so they re-fetch and re-render.
	h.Hub.Publish(ws.RunTopic(runID), "changed", nil)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"url":  fmt.Sprintf("/adventure-assets/team-avatar/%d", teamID),
		"mime": mime,
		"size": int64(len(data)),
	})
}

// DeleteTeamAvatarByToken clears the team's profile picture via the
// share URL.
func (h *Handlers) DeleteTeamAvatarByToken(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	teamID, runID, ok := h.teamByToken(w, r, token)
	if !ok {
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		UPDATE teams
		   SET avatar_mime_type = NULL, avatar_size_bytes = NULL, avatar_content = NULL
		 WHERE id = $1
	`, teamID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "clear failed")
		return
	}
	h.Hub.Publish(ws.RunTopic(runID), "changed", nil)
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
  overflow: hidden;
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
.avatar-btn img { width: 100%; height: 100%; object-fit: cover; display: block; }
.avatar-btn .hint-pill {
  position: absolute; bottom: -2px; right: -2px;
  background: var(--surface); color: var(--ink);
  border: 2px solid var(--accent);
  border-radius: 999px; padding: .15rem .45rem;
  font-family: 'Open Sans', sans-serif; font-size: .7rem; font-weight: 700;
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
</style></head>
<body>
<div class="wrap">
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

  <p class="hint">Pick your name to open your personal link. Keep it — only you should use it.</p>
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
  <p class="footer">If your name isn't listed, ask the arranger to add you.</p>
</div>

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
})();
</script>
</body></html>`))
