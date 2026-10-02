package server

import (
	"strings"
	"testing"
)

// The designer's MapLibre map fetches tiles, so connect-src must name
// the operator's tile host — and never anything but an https origin.
func TestCSPFor_TileOrigin(t *testing.T) {
	for _, c := range []struct{ tileURL, want string }{
		{"", "https://tile.openstreetmap.org;"},
		{"https://tiles.example.com/{z}/{x}/{y}.png?key=k", "https://tiles.example.com;"},
		{"http://tiles.example.com/{z}/{x}/{y}.png", "https://tile.openstreetmap.org;"},
		{"https://evil.example; script-src *", "https://tile.openstreetmap.org;"},
	} {
		csp := cspFor(c.tileURL)
		if !strings.Contains(csp, "connect-src 'self' wss: ws: https://unpkg.com "+c.want) {
			t.Errorf("cspFor(%q) connect-src wrong: %s", c.tileURL, csp)
		}
		if strings.Contains(csp, "{tiles}") {
			t.Errorf("cspFor(%q) left the placeholder: %s", c.tileURL, csp)
		}
	}
}
