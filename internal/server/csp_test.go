package server

import (
	"strings"
	"testing"
)

// The MapLibre maps fetch raster tiles and a vector style, so
// connect-src must name both hosts — and never anything but an https
// origin.
func TestCSPFor_TileOrigin(t *testing.T) {
	for _, c := range []struct{ tileURL, styleURL, want string }{
		{"", "", "https://tiles.openfreemap.org;"},
		{"https://tiles.example.com/{z}/{x}/{y}.png?key=k", "", "https://tiles.openfreemap.org https://tiles.example.com;"},
		{"http://tiles.example.com/{z}/{x}/{y}.png", "", "https://tiles.openfreemap.org;"},
		{"https://evil.example; script-src *", "https://evil.example; script-src *", "https://tiles.openfreemap.org;"},
		{"https://maps.example.com/{z}/{x}/{y}.png", "https://maps.example.com/styles/bright.json", "https://maps.example.com;"},
	} {
		csp := cspFor(c.tileURL, c.styleURL)
		if !strings.Contains(csp, "connect-src 'self' wss: ws: https://unpkg.com "+c.want) {
			t.Errorf("cspFor(%q) connect-src wrong: %s", c.tileURL, csp)
		}
		if strings.Contains(csp, "{tiles}") {
			t.Errorf("cspFor(%q) left the placeholder: %s", c.tileURL, csp)
		}
	}
}
