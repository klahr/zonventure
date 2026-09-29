package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/klahr/zonventure/internal/settings"
)

// unavailablePages lists the public pages that would answer with an
// error right now: /privacy and /terms until the operator sets the
// legal identity, /pricing while the marketing landing is off. Links to
// them are hidden rather than left to dead-end on a 503 or 404.
func unavailablePages(ctx context.Context, db *sql.DB, legal LegalConfig) []string {
	var out []string
	if len(legal.missingRequired()) > 0 {
		out = append(out, "/privacy", "/terms")
	}
	if !settings.LandingEnabled(ctx, db) {
		out = append(out, "/pricing")
	}
	return out
}

// hideLinksScript hides every link to an unavailable page, including
// links scripts add later. A link standing alone (a footer entry) goes
// with its "·" separator; one inside a sentence (a consent line) keeps
// its words as plain text.
const hideLinksScript = `<script>(()=>{const u=%s;const q=u.map(p=>'a[href="'+p+'"]').join(',');` +
	`const sep=n=>n&&n.nodeType===3&&/^\s*·?\s*$/.test(n.textContent);` +
	`const f=()=>document.querySelectorAll(q).forEach(a=>{const p=a.parentNode;if(!p)return;` +
	`const words=[...p.childNodes].some(n=>n!==a&&n.nodeType===3&&!sep(n));` +
	`if(words){a.replaceWith(document.createTextNode(a.textContent));return;}` +
	`if(sep(a.nextSibling))a.nextSibling.textContent=' ';else if(sep(a.previousSibling))a.previousSibling.textContent=' ';a.remove();});` +
	`f();new MutationObserver(f).observe(document.documentElement,{childList:true,subtree:true});})()</script>`

// hideUnavailableLinks injects hideLinksScript into HTML pages while
// any public page is unavailable. Only HTML is held back, and only an
// HTML response pays for the settings lookup; other responses stream
// straight through.
func hideUnavailableLinks(db *sql.DB, legal LegalConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
				next.ServeHTTP(w, r)
				return
			}
			bw := &htmlBuffer{ResponseWriter: w}
			next.ServeHTTP(bw, r)
			if !bw.html {
				return
			}
			body := bw.buf.Bytes()
			if gone := unavailablePages(r.Context(), db, legal); len(gone) > 0 {
				list, _ := json.Marshal(gone)
				script := []byte(strings.Replace(hideLinksScript, "%s", string(list), 1))
				// Early in <head> — the first "<head" is the real tag, whereas
				// a "</head>" can hide in a script's string — but after the
				// charset meta when it comes first, which must stay within
				// the document's first 1024 bytes.
				if i := bytes.Index(body, []byte("<head")); i >= 0 {
					if j := bytes.IndexByte(body[i:], '>'); j >= 0 {
						i += j + 1
						if k := bytes.Index(body[i:], []byte("<meta charset")); k >= 0 && k < 200 {
							if e := bytes.IndexByte(body[i+k:], '>'); e >= 0 {
								i += k + e + 1
							}
						}
						body = append(body[:i:i], append(script, body[i:]...)...)
					}
				} else {
					body = append(script, body...)
				}
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(bw.status)
			_, _ = w.Write(body)
		})
	}
}

// htmlBuffer holds back an HTML response so a script can be spliced
// in; anything else streams through untouched.
type htmlBuffer struct {
	http.ResponseWriter
	buf     bytes.Buffer
	status  int
	decided bool
	html    bool
}

func (b *htmlBuffer) WriteHeader(status int) {
	if b.decided {
		return
	}
	b.decided = true
	b.status = status
	ct := b.Header().Get("Content-Type")
	b.html = strings.HasPrefix(ct, "text/html") && b.Header().Get("Content-Encoding") == ""
	if !b.html {
		b.ResponseWriter.WriteHeader(status)
	}
}

func (b *htmlBuffer) Write(p []byte) (int, error) {
	if !b.decided {
		if b.Header().Get("Content-Type") == "" {
			b.Header().Set("Content-Type", http.DetectContentType(p))
		}
		b.WriteHeader(http.StatusOK)
	}
	if b.html {
		return b.buf.Write(p)
	}
	return b.ResponseWriter.Write(p)
}

func (b *htmlBuffer) Flush() {
	if f, ok := b.ResponseWriter.(http.Flusher); ok && !b.html {
		f.Flush()
	}
}

// sitemapURL matches one <url> entry, for dropping unavailable pages.
var sitemapURL = regexp.MustCompile(`(?s)\s*<url>\s*<loc>([^<]*)</loc>.*?</url>`)

func dropSitemapPages(xml, host string, gone []string) string {
	if len(gone) == 0 {
		return xml
	}
	return sitemapURL.ReplaceAllStringFunc(xml, func(m string) string {
		loc := sitemapURL.FindStringSubmatch(m)[1]
		for _, p := range gone {
			if loc == host+p {
				return ""
			}
		}
		return m
	})
}
