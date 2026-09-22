package server_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// The demo adventure seeded by newTestServer starts with no colours set,
// so these tests drive the admin API to assign a Mind-Race palette and
// verify it flows through to (a) the /edit payload, (b) the player
// state, and (c) the live WS snapshot.

const (
	mindAccent    = "#34d399" // green
	mindSecondary = "#7c3aed" // purple
	mindBg        = "#ffffff" // white
	mindText      = "#000000" // black
)

func TestColors_PatchRoundTripsThroughEdit(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}

	// Set all four.
	payload := map[string]any{
		"accent_color":           mindAccent,
		"accent_secondary_color": mindSecondary,
		"background_color":       mindBg,
		"text_color":             mindText,
	}
	if s := admin.do("PATCH", "/api/admin/adventures/demo", payload, nil); s != 204 {
		t.Fatalf("PATCH colours = %d", s)
	}

	var edit struct {
		AccentColor          string `json:"accent_color"`
		AccentSecondaryColor string `json:"accent_secondary_color"`
		BackgroundColor      string `json:"background_color"`
		TextColor            string `json:"text_color"`
	}
	if s := admin.do("GET", "/api/admin/adventures/demo/edit", nil, &edit); s != 200 {
		t.Fatalf("edit GET = %d", s)
	}
	if edit.AccentColor != mindAccent || edit.AccentSecondaryColor != mindSecondary ||
		edit.BackgroundColor != mindBg || edit.TextColor != mindText {
		t.Fatalf("edit payload lost colours: %+v", edit)
	}

	// Clearing one: empty string stores NULL and round-trips as "".
	if s := admin.do("PATCH", "/api/admin/adventures/demo",
		map[string]any{"text_color": ""}, nil); s != 204 {
		t.Fatalf("clear text = %d", s)
	}
	edit.TextColor = "dirty"
	if s := admin.do("GET", "/api/admin/adventures/demo/edit", nil, &edit); s != 200 {
		t.Fatalf("edit GET = %d", s)
	}
	if edit.TextColor != "" {
		t.Fatalf("cleared text_color = %q, want \"\"", edit.TextColor)
	}
	if edit.AccentColor != mindAccent {
		t.Fatalf("clear bled over: accent_color=%q", edit.AccentColor)
	}
}

func TestColors_HexValidation(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}

	cases := []struct {
		val string
		ok  bool
	}{
		{"#ffffff", true},
		{"#ABC123", true},
		{"", true},      // empty = "use default"
		{"red", false},  // CSS keyword rejected
		{"#fff", false}, // 3-digit shorthand rejected (ambiguous wire format)
		{"#gggggg", false},
		{"ffffff", false}, // missing '#'
	}
	for _, c := range cases {
		s := admin.do("PATCH", "/api/admin/adventures/demo",
			map[string]any{"accent_color": c.val}, nil)
		if c.ok && s != 204 {
			t.Errorf("valid hex %q: status=%d, want 204", c.val, s)
		}
		if !c.ok && s != 400 {
			t.Errorf("invalid hex %q: status=%d, want 400", c.val, s)
		}
	}
}

func TestColors_OrgAdminCannotPatchColours(t *testing.T) {
	// Org-admins must not be able to rebrand adventures they don't own —
	// only the global admin tier can. This pins that authorization slice
	// so a future refactor can't quietly open up the surface.
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("boss login = %d", s)
	}
	s := boss.do("PATCH", "/api/admin/adventures/demo",
		map[string]any{"accent_color": "#112233"}, nil)
	if s == 204 {
		t.Fatalf("org_admin was able to patch colours — should be forbidden")
	}
}

func TestColors_SurfaceInPlayerState(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	if s := admin.do("PATCH", "/api/admin/adventures/demo",
		map[string]any{
			"accent_color":           mindAccent,
			"accent_secondary_color": mindSecondary,
			"background_color":       mindBg,
			"text_color":             mindText,
		}, nil); s != 204 {
		t.Fatalf("PATCH colours = %d", s)
	}

	_, _, slotToken, _ := setupRun(t, ts, assignmentID)
	alice := loginPlayer(t, ts, slotToken)

	// HTTP state endpoint carries colours.
	var st struct {
		Colors *struct {
			Accent          string `json:"accent"`
			AccentSecondary string `json:"accent_secondary"`
			Background      string `json:"background"`
			Text            string `json:"text"`
		} `json:"colors"`
	}
	if s := alice.do("GET", "/api/player/state", nil, &st); s != 200 {
		t.Fatalf("GET state = %d", s)
	}
	if st.Colors == nil {
		t.Fatalf("state missing colors")
	}
	if st.Colors.Accent != mindAccent || st.Colors.AccentSecondary != mindSecondary ||
		st.Colors.Background != mindBg || st.Colors.Text != mindText {
		t.Fatalf("state colours = %+v", *st.Colors)
	}

	// WS push carries the same payload shape.
	c := dialWS(t, ts, "/ws/play", alice.c)
	defer c.Close(websocket.StatusNormalClosure, "")
	m := recvKind(t, c, "state", 2*time.Second)
	var wsSt struct {
		Colors *struct {
			Accent string `json:"accent"`
		} `json:"colors"`
	}
	json.Unmarshal(m.Data, &wsSt)
	if wsSt.Colors == nil || wsSt.Colors.Accent != mindAccent {
		t.Fatalf("ws state colours = %+v", wsSt.Colors)
	}
}

func TestColors_SurfaceInLiveSnapshot(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	if s := admin.do("PATCH", "/api/admin/adventures/demo",
		map[string]any{"accent_color": mindAccent, "background_color": mindBg}, nil); s != 204 {
		t.Fatalf("PATCH colours = %d", s)
	}

	_, liveToken, _, _ := setupRun(t, ts, assignmentID)
	c := dialWS(t, ts, "/ws/live/"+liveToken, nil)
	defer c.Close(websocket.StatusNormalClosure, "")
	m := recvKind(t, c, "snapshot", 2*time.Second)
	var snap struct {
		Colors *struct {
			Accent     string `json:"accent"`
			Background string `json:"background"`
		} `json:"colors"`
	}
	json.Unmarshal(m.Data, &snap)
	if snap.Colors == nil {
		t.Fatalf("snapshot missing colors")
	}
	if snap.Colors.Accent != mindAccent || snap.Colors.Background != mindBg {
		t.Fatalf("snapshot colours = %+v", *snap.Colors)
	}
}

func TestColors_OmittedWhenUnset(t *testing.T) {
	// Default seed leaves all colour columns NULL. The JSON output should
	// omit the whole "colors" field rather than shipping an empty object —
	// so the client cleanly distinguishes "restore defaults" from "admin
	// set this to white".
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	_, liveToken, slotToken, _ := setupRun(t, ts, assignmentID)

	c := dialWS(t, ts, "/ws/live/"+liveToken, nil)
	defer c.Close(websocket.StatusNormalClosure, "")
	m := recvKind(t, c, "snapshot", 2*time.Second)
	if strings.Contains(string(m.Data), `"colors"`) {
		t.Errorf("unset snapshot still carries colors field: %s", m.Data)
	}

	alice := loginPlayer(t, ts, slotToken)
	c2 := dialWS(t, ts, "/ws/play", alice.c)
	defer c2.Close(websocket.StatusNormalClosure, "")
	ms := recvKind(t, c2, "state", 2*time.Second)
	if strings.Contains(string(ms.Data), `"colors"`) {
		t.Errorf("unset player state still carries colors field: %s", ms.Data)
	}
}
