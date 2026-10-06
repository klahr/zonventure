package server_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// The demo adventure seeded by newTestServer starts with no colours set,
// so TestColors drives the admin API to assign a Mind-Race palette and
// verify it flows through to (a) the /edit payload, (b) the player
// state, and (c) the live WS snapshot.

const (
	mindAccent    = "#34d399" // green
	mindSecondary = "#7c3aed" // purple
	mindBg        = "#ffffff" // white
	mindText      = "#000000" // black
)

// TestColors runs its steps in order on one server: the unset check
// comes first, while the seeded demo still has no colours, and later
// steps read the palette earlier ones set.
func TestColors(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	_, liveToken, slotToken, _ := setupRun(t, ts, assignmentID)
	alice := loginPlayer(t, ts, slotToken)

	t.Run("omitted when unset", func(t *testing.T) {
		alice := alice.in(t)
		// Default seed leaves all colour columns NULL. The JSON output
		// should omit the whole "colors" field rather than shipping an
		// empty object — so the client cleanly distinguishes "restore
		// defaults" from "admin set this to white".
		c := dialWS(t, ts, "/ws/live/"+liveToken, nil)
		defer c.Close(websocket.StatusNormalClosure, "")
		m := recvKind(t, c, "snapshot", 2*time.Second)
		if strings.Contains(string(m.Data), `"colors"`) {
			t.Errorf("unset snapshot still carries colors field: %s", m.Data)
		}

		var raw json.RawMessage
		if s := alice.do("GET", "/api/player/play", nil, &raw); s != 200 {
			t.Fatalf("GET play = %d", s)
		}
		if strings.Contains(string(raw), `"brand"`) {
			t.Errorf("unset play state still carries brand field: %s", raw)
		}
	})

	t.Run("hex validation", func(t *testing.T) {
		admin := admin.in(t)
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
	})

	t.Run("patch round-trips through edit", func(t *testing.T) {
		admin := admin.in(t)
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
	})

	// The full palette again, for the player and the live board.
	if s := admin.do("PATCH", "/api/admin/adventures/demo",
		map[string]any{
			"accent_color":           mindAccent,
			"accent_secondary_color": mindSecondary,
			"background_color":       mindBg,
			"text_color":             mindText,
		}, nil); s != 204 {
		t.Fatalf("PATCH colours = %d", s)
	}

	t.Run("surface in play state", func(t *testing.T) {
		alice := alice.in(t)
		// The player's view carries the adventure's colours.
		var st struct {
			Brand *struct {
				Accent          string `json:"accent"`
				AccentSecondary string `json:"accent_secondary"`
				Background      string `json:"background"`
				Text            string `json:"text"`
			} `json:"brand"`
		}
		if s := alice.do("GET", "/api/player/play", nil, &st); s != 200 {
			t.Fatalf("GET play = %d", s)
		}
		if st.Brand == nil {
			t.Fatalf("play state missing brand")
		}
		if st.Brand.Accent != mindAccent || st.Brand.AccentSecondary != mindSecondary ||
			st.Brand.Background != mindBg || st.Brand.Text != mindText {
			t.Fatalf("play brand = %+v", *st.Brand)
		}
	})

	t.Run("surface in live snapshot", func(t *testing.T) {
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
	})
}
