package public

import (
	"database/sql"
	"errors"
	"html/template"
	"net/http"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"github.com/go-chi/chi/v5"
)

// teamShareView is the data passed to the share-page template.
type teamShareView struct {
	TeamName       string
	AdventureTitle string
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
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT t.name, a.title
		FROM teams t
		JOIN adventure_runs ar ON ar.id = t.run_id
		LEFT JOIN adventures a ON a.slug = ar.adventure_slug
		WHERE t.token = ?
	`, token).Scan(&view.TeamName, &title)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "team not found — ask your arranger for a fresh link", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	view.AdventureTitle = title.String

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT ps.name, ps.token
		FROM player_slots ps
		JOIN teams t ON t.id = ps.team_id
		WHERE t.token = ?
		ORDER BY ps.name COLLATE NOCASE
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
</style></head>
<body>
<div class="wrap">
  <h1 class="brand">zonventure</h1>
  <p class="sub">Team</p>
  <p class="team">{{.TeamName}}</p>
  {{if .AdventureTitle}}<p class="adv">{{.AdventureTitle}}</p>{{end}}
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
</body></html>`))
