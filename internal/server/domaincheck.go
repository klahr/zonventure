package server

import (
	"context"
	"html/template"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/admin"
)

// /domain-check/{host}: the page an organisation's admin sends whoever
// runs its domain. It shows the one record to add and, live, whether it
// works; no account needed. It tells nothing a DNS lookup wouldn't.

var domainCheckTexts = map[string]map[string]string{
	"en": {
		"title":       "Point %s at its adventures",
		"lead":        "%s wants its adventures at this address. Add this one record where the domain's DNS is managed:",
		"type":        "Type",
		"name":        "Name",
		"value":       "Value",
		"copy":        "Copy",
		"copied":      "Copied",
		"tips":        "Remove any other record with the same name first. On Cloudflare, leave the proxy off (grey cloud). Most panels add the domain to the name themselves: enter only the name above.",
		"waiting":     "Waiting for the record. Changes usually show within minutes, at most a few hours.",
		"wrong":       "We found a record, but it isn't right yet.",
		"certificate": "The record is right. Getting a security certificate…",
		"live":        "It works. %s is live.",
		"seen":        "We see",
		"checking":    "Checking…",
		"check":       "Check now",
		"auto":        "This page checks again by itself every 15 seconds.",
		"doubled":     "The name was entered as the full address. Enter only the name above.",
		"address":     "There's an A or AAAA record for this name. Remove it and add the CNAME instead.",
		"cloudflare":  "Cloudflare's proxy is on. Switch the record to DNS only (grey cloud).",
		"other":       "The record points somewhere else. Its value should be the one above.",
		"cert":        "The address reaches us, but HTTPS isn't answering yet. This usually sorts itself out within minutes.",
		"open":        "Open it",
	},
	"sv": {
		"title":       "Peka %s mot äventyren",
		"lead":        "%s vill ha sina äventyr på den här adressen. Lägg till den här posten där domänens DNS sköts:",
		"type":        "Typ",
		"name":        "Namn",
		"value":       "Värde",
		"copy":        "Kopiera",
		"copied":      "Kopierat",
		"tips":        "Ta först bort andra poster med samma namn. Hos Cloudflare, låt proxyn vara avslagen (grått moln). De flesta lägger till domänen i namnet själva: skriv bara namnet ovan.",
		"waiting":     "Väntar på posten. Ändringar syns oftast inom några minuter, som mest några timmar.",
		"wrong":       "Vi hittade en post, men den stämmer inte än.",
		"certificate": "Posten stämmer. Hämtar ett säkerhetscertifikat…",
		"live":        "Det fungerar. %s är igång.",
		"seen":        "Vi ser",
		"checking":    "Kollar…",
		"check":       "Kolla nu",
		"auto":        "Sidan kollar igen av sig själv var 15:e sekund.",
		"doubled":     "Namnet skrevs som hela adressen. Skriv bara namnet ovan.",
		"address":     "Det finns en A- eller AAAA-post för namnet. Ta bort den och lägg till CNAME-posten i stället.",
		"cloudflare":  "Cloudflares proxy är på. Ställ posten på DNS only (grått moln).",
		"other":       "Posten pekar någon annanstans. Värdet ska vara det ovan.",
		"cert":        "Adressen når oss, men HTTPS svarar inte än. Det brukar lösa sig inom några minuter.",
		"open":        "Öppna den",
	},
}

var domainCheckPage = template.Must(template.New("dc").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{.Host}}</title>
<style>
:root { --ink:#1f2937; --muted:#6b7280; --line:#e5e7eb; --bg:#f7f5f2; --card:#fff; --ok:#15803d; --warn:#b45309; --bad:#b91c1c; --accent:#1f3349; }
* { box-sizing: border-box; }
body { margin:0; background:var(--bg); color:var(--ink); font:16px/1.55 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif; }
main { max-width:40rem; margin:0 auto; padding:3rem 1rem; }
.card { background:var(--card); border-radius:16px; box-shadow:0 1px 2px rgba(0,0,0,.04),0 8px 28px rgba(31,51,73,.08); padding:2rem; }
h1 { font-size:1.45rem; line-height:1.25; margin:0 0 .5rem; overflow-wrap:anywhere; }
p { margin:.5rem 0; }
.muted { color:var(--muted); font-size:.92rem; }
.record { margin:1.25rem 0; border:1px solid var(--line); border-radius:12px; overflow:hidden; }
.row { display:grid; grid-template-columns:6rem 1fr auto; gap:.75rem; align-items:center; padding:.7rem 1rem; border-top:1px solid var(--line); }
.row:first-child { border-top:0; }
.row b { font-size:.8rem; text-transform:uppercase; letter-spacing:.04em; color:var(--muted); font-weight:600; }
code { font:600 .98rem/1.3 ui-monospace,SFMono-Regular,Menlo,monospace; overflow-wrap:anywhere; }
code span { white-space:nowrap; }
button { font:inherit; cursor:pointer; border-radius:999px; border:1px solid var(--line); background:#fff; color:var(--ink); padding:.35rem .9rem; font-size:.85rem; }
button:hover { border-color:var(--accent); }
button.primary { background:var(--accent); color:#fff; border-color:var(--accent); padding:.55rem 1.2rem; font-size:.95rem; }
.status { display:flex; gap:.8rem; align-items:flex-start; padding:1rem; border-radius:12px; background:#f3f4f6; margin:1.25rem 0 .75rem; }
.dot { flex:none; width:.8rem; height:.8rem; border-radius:50%; margin-top:.4rem; background:var(--muted); }
.s-waiting .dot { background:var(--warn); animation:pulse 1.6s infinite; }
.s-certificate .dot { background:#2563eb; animation:pulse 1.2s infinite; }
.s-wrong { background:#fef2f2; } .s-wrong .dot { background:var(--bad); }
.s-live { background:#f0fdf4; } .s-live .dot { background:var(--ok); }
@keyframes pulse { 50% { opacity:.35; } }
.actions { display:flex; gap:.6rem; align-items:center; flex-wrap:wrap; }
a.open { color:var(--ok); font-weight:700; }
@media (max-width:30rem) { .card { padding:1.25rem; } .row { grid-template-columns:1fr auto; } .row b { grid-column:1 / -1; } }
</style>
</head>
<body>
<main>
<div class="card">
  <h1>{{.Title}}</h1>
  <p>{{.Lead}}</p>
  <div class="record" role="table">
    <div class="row" role="row"><b role="rowheader">{{.T.type}}</b><code>CNAME</code><span></span></div>
    <div class="row" role="row"><b role="rowheader">{{.T.name}}</b><code>{{.NameHTML}}</code><button type="button" data-copy="{{.Name}}">{{.T.copy}}</button></div>
    <div class="row" role="row"><b role="rowheader">{{.T.value}}</b><code>{{.ValueHTML}}</code><button type="button" data-copy="{{.Value}}">{{.T.copy}}</button></div>
  </div>
  <p class="muted">{{.T.tips}}</p>
  <div id="status" class="status s-{{.Status}}" role="status" aria-live="polite">
    <span class="dot" aria-hidden="true"></span>
    <div><div id="status-text"></div><div id="status-seen" class="muted"></div></div>
  </div>
  <div class="actions" id="actions">
    <button type="button" class="primary" id="check">{{.T.check}}</button>
    <span class="muted">{{.T.auto}}</span>
  </div>
</div>
</main>
<script id="dc-data" type="application/json">{{.JSON}}</script>
<script>
(function () {
  var d = JSON.parse(document.getElementById('dc-data').textContent);
  var T = d.t, host = d.host;
  function fmt(s, a) { return s.replace('%s', a); }
  function csrf() { var m = document.cookie.match(/(?:^|; )zv_csrf=([^;]+)/); return m ? decodeURIComponent(m[1]) : ''; }
  var problems = { doubled: T.doubled, address: T.address, cloudflare: T.cloudflare, other_target: T.other, certificate: T.cert };
  function show(s) {
    var box = document.getElementById('status');
    box.className = 'status s-' + s.status;
    var text = document.getElementById('status-text');
    text.textContent = '';
    if (s.status === 'live') {
      text.appendChild(document.createTextNode(fmt(T.live, host) + ' '));
      var a = document.createElement('a'); a.className = 'open'; a.href = 'https://' + host; a.textContent = T.open + ' →';
      text.appendChild(a);
      document.getElementById('actions').hidden = true;
    } else {
      text.textContent = (s.problem && problems[s.problem]) || T[s.status] || T.waiting;
    }
    document.getElementById('status-seen').textContent = s.seen && s.status !== 'live' ? T.seen + ': ' + s.seen : '';
  }
  var busy = false;
  function check() {
    if (busy) return; busy = true;
    var btn = document.getElementById('check'); btn.textContent = T.checking; btn.disabled = true;
    fetch('/api/domain-check/' + encodeURIComponent(host), { method: 'POST', headers: { 'X-CSRF-Token': csrf() }, credentials: 'same-origin' })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (s) { if (s) { show(s); if (s.status === 'live') clearInterval(timer); } })
      .catch(function () {})
      .then(function () { busy = false; btn.textContent = T.check; btn.disabled = false; });
  }
  document.querySelectorAll('[data-copy]').forEach(function (b) {
    b.addEventListener('click', function () {
      var v = b.getAttribute('data-copy');
      navigator.clipboard.writeText(v).then(function () { b.textContent = T.copied; setTimeout(function () { b.textContent = T.copy; }, 1500); });
    });
  });
  document.getElementById('check').addEventListener('click', check);
  show(d.state);
  var timer = d.state.status === 'live' ? null : setInterval(check, 15000);
})();
</script>
</body>
</html>`))

func registerDomainCheck(r chi.Router, adminH *admin.Handlers) {
	r.Get("/domain-check/{host}", func(w http.ResponseWriter, req *http.Request) {
		host := strings.ToLower(chi.URLParam(req, "host"))
		st, ok := adminH.PublicDomain(context.WithoutCancel(req.Context()), host, false)
		if !ok {
			render404(w, req)
			return
		}
		lang := resolveLang(w, req)
		t, ok := domainCheckTexts[lang]
		if !ok {
			t = domainCheckTexts["en"]
		}
		data := map[string]any{
			"host":  st.Host,
			"t":     t,
			"state": st,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		_ = domainCheckPage.Execute(w, map[string]any{
			"Lang":      lang,
			"Host":      st.Host,
			"Name":      st.Name,
			"Value":     st.Value,
			"Status":    st.Status,
			"T":         t,
			"Title":     strings.Replace(t["title"], "%s", st.Host, 1),
			"Lead":      strings.Replace(t["lead"], "%s", st.OrgName, 1),
			"JSON":      template.JS(marshalJSONForScript(data)),
			"NameHTML":  wrapAtDots(st.Name),
			"ValueHTML": wrapAtDots(st.Value),
		})
	})
	r.Post("/api/domain-check/{host}", adminH.PublicDomainStatus)
}

// wrapAtDots is a hostname that wraps only after its dots.
func wrapAtDots(host string) template.HTML {
	parts := strings.Split(host, ".")
	for i, p := range parts {
		parts[i] = "<span>" + template.HTMLEscapeString(p) + "</span>"
	}
	return template.HTML(strings.Join(parts, ".<wbr>"))
}
