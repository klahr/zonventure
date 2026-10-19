package server

import (
	"regexp"
	"strings"
	"testing"
)

// A page that draws a map must let it fetch its style and tiles and
// decode them in a worker. Its own <meta> policy otherwise blocks what
// the server's header allows: the ticket page's starting-point map once
// stayed blank on an organisation's own domain for that.
func TestMapPages_MetaCSPAllowsMap(t *testing.T) {
	meta := regexp.MustCompile(`http-equiv="Content-Security-Policy" content="([^"]*)"`)
	for _, name := range []string{"web/store-event.html"} {
		b, err := webFS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		m := meta.FindSubmatch(b)
		if m == nil {
			continue
		}
		csp := string(m[1])
		for _, want := range []string{"connect-src 'self' wss: ws: https:", "worker-src 'self' blob:"} {
			if !strings.Contains(csp, want) {
				t.Errorf("%s: meta CSP lacks %q: %s", name, want, csp)
			}
		}
	}
}
