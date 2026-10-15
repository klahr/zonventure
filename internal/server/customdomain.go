package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/domains"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/sitename"
)

// An organisation on its own domain (internal/domains): requests for its
// hostname are answered as its store, in its name alone, and its pages
// on the instance's own host move there once it is live.

// whiteLabelFrom is the organisation whose domain r came in on.
func whiteLabelFrom(ctx context.Context) (domains.Entry, bool) {
	return domains.FromContext(ctx)
}

// readWebFileFor is readWebFile, except that on an organisation's own
// domain the embedded, brand-neutral page is used: an operator's
// override pages carry the operator's brand.
func readWebFileFor(r *http.Request, name string) ([]byte, error) {
	if e, ok := whiteLabelFrom(r.Context()); ok {
		b, err := webFS.ReadFile(name)
		if err != nil || !strings.HasSuffix(name, ".html") {
			return b, err
		}
		n := html.EscapeString(e.Name)
		return []byte(strings.NewReplacer(sitename.Placeholder, n, sitename.WordmarkPlaceholder, n).Replace(string(b))), nil
	}
	return readWebFile(name)
}

// staffPrefixes are what an organisation's own domain sends back to the
// instance's host: the console, signing in, and what only staff use.
var staffPrefixes = []string{
	"/arrange", "/app/arrange", "/api/admin", "/api/auth", "/login", "/signup", "/admin",
	"/mcp", "/oauth", "/.well-known/oauth", "/api/mcp", "/dev", "/metrics",
}

// platformPages are the instance's own marketing and help, which an
// organisation's domain doesn't show.
var platformPages = []string{"/manual", "/pricing", "/create", "/shop"}

func hasPathPrefix(p, prefix string) bool {
	return p == prefix || strings.HasPrefix(p, prefix+"/") || strings.HasPrefix(p, prefix+"?")
}

// customDomains routes requests by Host. mainBase is ZV_PUBLIC_URL.
func customDomains(mainBase string) func(http.Handler) http.Handler {
	mainBase = strings.TrimRight(strings.TrimSpace(mainBase), "/")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The TLS proxy asks before fetching a certificate; any host.
			if r.URL.Path == "/internal/tls-allowed" {
				tlsAllowed(w, r)
				return
			}
			e, ok := domains.ByHost(r.Host)
			if !ok {
				if moveToOwnDomain(w, r) {
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			now := time.Now()
			p := r.URL.Path
			if !e.Active(now) {
				// Past the grace, every link goes back to the store on
				// the instance's own host, so nothing sent breaks.
				target := mainBase + p
				if p == "/" {
					target = mainBase + "/o/" + e.Slug
				}
				if r.URL.RawQuery != "" {
					target += "?" + r.URL.RawQuery
				}
				http.Redirect(w, r, target, http.StatusMovedPermanently)
				return
			}
			if p == domains.ProbePath {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				_, _ = w.Write([]byte(e.Host))
				return
			}
			for _, s := range staffPrefixes {
				if hasPathPrefix(p, s) {
					target := mainBase + r.URL.RequestURI()
					http.Redirect(w, r, target, http.StatusFound)
					return
				}
			}
			for _, s := range platformPages {
				if hasPathPrefix(p, s) {
					http.Redirect(w, r, "/", http.StatusFound)
					return
				}
			}
			own := "/o/" + e.Slug
			switch {
			case p == "/" || p == "":
				r = rewritePath(r, own)
			case p == "/store" || p == own || p == own+"/" || p == own+"/store":
				redirectKeepingQuery(w, r, "/")
				return
			case p == "/redeem":
				r = rewritePath(r, own+"/redeem")
			case p == "/request":
				r = rewritePath(r, own+"/request")
			case p == "/favicon.ico" || p == "/favicon.svg":
				if e.HasLogo {
					r = rewritePath(r, "/org-logo/"+strconv.FormatInt(e.OrgID, 10))
				}
			case strings.HasPrefix(p, "/o/") && !hasPathPrefix(p, own),
				strings.HasPrefix(p, "/api/o/") && !hasPathPrefix(p, "/api"+own):
				// Another organisation's pages aren't on this one's domain.
				http.NotFound(w, r)
				return
			}
			r = r.WithContext(domains.WithEntry(r.Context(), e))
			if p == "/privacy" || p == "/terms" {
				// The platform's terms, as written: they name who
				// actually sells and processes.
				next.ServeHTTP(w, r)
				return
			}
			wl := &whiteLabelWriter{ResponseWriter: w, entry: e, lang: emailtpl.LangFromRequest(r)}
			next.ServeHTTP(wl, r)
			wl.finish()
		})
	}
}

func rewritePath(r *http.Request, p string) *http.Request {
	r2 := r.Clone(r.Context())
	r2.URL.Path = p
	r2.URL.RawPath = ""
	r2.RequestURI = p
	if r.URL.RawQuery != "" {
		r2.RequestURI += "?" + r.URL.RawQuery
	}
	return r2
}

var orgPagePath = regexp.MustCompile(`^/o/([a-z0-9-]+)(/.*)?$`)

// moveToOwnDomain sends a visitor of an organisation's pages on the
// instance's own host to its live domain. Pages only: the API, play and
// join links stay where they were sent, so a running game never jumps.
func moveToOwnDomain(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	m := orgPagePath.FindStringSubmatch(r.URL.Path)
	if m == nil {
		return false
	}
	e, ok := domains.BySlug(m[1])
	if !ok || !e.Live(time.Now()) {
		return false
	}
	rest := m[2]
	switch rest {
	case "", "/", "/store":
		rest = "/"
	default:
		rest = r.URL.Path
	}
	target := domains.Origin(e.Host) + rest
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
	return true
}

// tlsAllowed answers the TLS proxy's on-demand question (Caddy's
// on_demand_tls ask): 200 for a hostname an entitled organisation
// claimed and whose record was found right, 404 otherwise.
func tlsAllowed(w http.ResponseWriter, r *http.Request) {
	e, ok := domains.ByHost(r.URL.Query().Get("domain"))
	if ok && e.Serving(time.Now()) {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.NotFound(w, r)
}

// whiteLabelWriter rewrites the HTML pages it carries into the
// organisation's name: the instance's name becomes the organisation's,
// the instance's own links (marked <!--platform-->…<!--/platform--> in
// the templates) go, and the icon is its logo. Anything else passes
// through untouched.
type whiteLabelWriter struct {
	http.ResponseWriter
	entry   domains.Entry
	lang    string
	decided bool
	buffer  bool
	status  int
	buf     bytes.Buffer
}

func (w *whiteLabelWriter) decide() {
	if w.decided {
		return
	}
	w.decided = true
	ct := w.Header().Get("Content-Type")
	w.buffer = strings.HasPrefix(ct, "text/html") && w.Header().Get("Content-Encoding") == ""
}

func (w *whiteLabelWriter) WriteHeader(status int) {
	w.decide()
	if w.buffer {
		w.status = status
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *whiteLabelWriter) Write(b []byte) (int, error) {
	w.decide()
	if w.buffer {
		return w.buf.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

func (w *whiteLabelWriter) Flush() {
	if w.decided && w.buffer {
		return
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *whiteLabelWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("hijack not supported")
}

func (w *whiteLabelWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *whiteLabelWriter) finish() {
	if !w.decided || !w.buffer {
		return
	}
	out := whiteLabelHTML(w.buf.String(), w.entry, w.lang)
	w.Header().Del("Content-Length")
	w.Header().Del("ETag")
	if w.status != 0 {
		w.ResponseWriter.WriteHeader(w.status)
	}
	_, _ = w.ResponseWriter.Write([]byte(out))
}

var titleTag = regexp.MustCompile(`<title>([^<]*)</title>`)

// titleOnce drops a trailing " — name" (or " · ", " | ") from the page
// title when name is already in it: the site's name and the
// organisation's are one here.
func titleOnce(s, name string) string {
	return titleTag.ReplaceAllStringFunc(s, func(tag string) string {
		t := titleTag.FindStringSubmatch(tag)[1]
		for _, sep := range []string{" — ", " · ", " | "} {
			if rest, ok := strings.CutSuffix(t, sep+name); ok && strings.Contains(rest, name) {
				return "<title>" + rest + "</title>"
			}
		}
		return tag
	})
}

var platformBlock = regexp.MustCompile(`(?s)<!--platform-->.*?<!--/platform-->`)

// orgLinkTexts label an organisation's own policies in a page's footer.
var orgLinkTexts = map[string][2]string{
	"en": {"%s terms", "%s privacy"},
	"sv": {"%s villkor", "%s integritet"},
}

// whiteLabelHTML is page s in e's name: its title typeface, and its own
// terms and privacy links where a template marks <!--org-links-->.
func whiteLabelHTML(s string, e domains.Entry, lang string) string {
	// "Verified creator" is the platform vouching for an organiser; on
	// the organiser's own domain it would read as vouching for itself.
	// And the organisation's name above each adventure only repeats itself.
	s = strings.Replace(s, "</head>", "<style>.verified-badge, .store-org { display: none !important; }</style></head>", 1)
	// The store's pages carry it already (orgFontBlock); the rest of
	// its domain's pages get it here.
	if font := orgFontBlock(e.Font); font != "" && !strings.Contains(s, lookFontURL(e.Font)) {
		s = strings.Replace(s, "</head>", font+"</head>", 1)
	}
	if strings.Contains(s, "<!--org-links-->") {
		texts, ok := orgLinkTexts[lang]
		if !ok {
			texts = orgLinkTexts["en"]
		}
		var links strings.Builder
		for i, u := range []string{e.TermsURL, e.PrivacyURL} {
			if u != "" {
				links.WriteString(` · <a href="` + html.EscapeString(u) + `" rel="noopener">` + html.EscapeString(fmt.Sprintf(texts[i], e.Name)) + `</a>`)
			}
		}
		s = strings.ReplaceAll(s, "<!--org-links-->", links.String())
	}
	s = platformBlock.ReplaceAllString(s, "")
	name := html.EscapeString(e.Name)
	if site := html.EscapeString(sitename.Name()); site != "" && site != name {
		s = strings.ReplaceAll(s, site, name)
	}
	if own := html.EscapeString(e.OrgName); own != "" && own != name {
		s = strings.ReplaceAll(s, own, name)
	}
	s = titleOnce(s, name)
	if e.HasLogo {
		s = strings.ReplaceAll(s, `href="/favicon.svg" type="image/svg+xml"`, `href="/favicon.ico"`)
	}
	return s
}
