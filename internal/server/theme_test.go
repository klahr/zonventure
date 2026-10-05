package server_test

import (
	"testing"
)

// TestThemePresetsAndOrgBrandReachThePlayer: the adventure's style
// presets are validated and stored, and the player receives both the
// adventure's palette and the running organisation's colours.
func TestThemePresetsAndOrgBrandReachThePlayer(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("publisher login = %d", s)
	}
	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]string{"corners": "wobbly"}, nil); s != 400 {
		t.Errorf("unknown corners = %d, want 400", s)
	}
	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]string{"cards": "outlined"}, nil); s != 400 {
		t.Errorf("cards outlined (now a separate outline flag) = %d, want 400", s)
	}
	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"backdrop": map[string]any{"kind": "plaid", "opacity": 0.1, "scale": 20}}, nil); s != 400 {
		t.Errorf("unknown backdrop kind = %d, want 400", s)
	}
	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"style": map[string]any{"card_opacity": 0.05}}, nil); s != 400 {
		t.Errorf("card_opacity below the floor = %d, want 400", s)
	}
	backdrop := map[string]any{"kind": "dots", "color": "#1f8036", "opacity": 0.12, "scale": 24, "fade": true}
	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"corners": "sharp", "cards": "flat", "card_outline": true, "heading_font": "fredoka", "accent_color": "#123456", "backdrop": backdrop, "style": map[string]any{"card_opacity": 0.7, "card_blur": true}}, nil); s != 200 && s != 204 {
		t.Fatalf("set presets = %d", s)
	}

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("arranger login = %d", s)
	}
	if s := arr.doAny("PUT", "/api/admin/orgs/me/profile", map[string]string{"accent_color": "#0a0", "background_color": "#fafafa"}, nil); s != 200 && s != 204 {
		t.Fatalf("org colours = %d", s)
	}
	var run struct {
		ID int64 `json:"id"`
	}
	if s := arr.do("POST", "/api/admin/events", map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &run); s != 201 {
		t.Fatalf("create event = %d", s)
	}
	var team struct {
		Members []struct {
			Token string `json:"token"`
		} `json:"members"`
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/teams", map[string]any{"name": "Team Alpha", "member_names": []string{"alice"}}, &team); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	p := newClient(t, ts)
	if s := p.do("GET", "/play?t="+team.Members[0].Token, nil, nil); s != 200 {
		t.Fatalf("magic-link login = %d", s)
	}
	var state struct {
		Brand struct {
			Accent      string `json:"accent"`
			Corners     string `json:"corners"`
			Cards       string `json:"cards"`
			CardOutline bool   `json:"card_outline"`
			HeadingFont string `json:"heading_font"`
			Backdrop    *struct {
				Kind    string  `json:"kind"`
				Color   string  `json:"color"`
				Opacity float64 `json:"opacity"`
				Fade    bool    `json:"fade"`
			} `json:"backdrop"`
			Style *struct {
				CardOpacity float64 `json:"card_opacity"`
				CardBlur    bool    `json:"card_blur"`
			} `json:"style"`
		} `json:"brand"`
		OrgBrand struct {
			Accent     string `json:"accent"`
			Background string `json:"background"`
		} `json:"org_brand"`
	}
	if s := p.do("GET", "/api/player/play", nil, &state); s != 200 {
		t.Fatalf("play state = %d", s)
	}
	if b := state.Brand; b.Accent != "#123456" || b.Corners != "sharp" || b.Cards != "flat" || !b.CardOutline || b.HeadingFont != "fredoka" {
		t.Errorf("brand = %+v", b)
	}
	if d := state.Brand.Backdrop; d == nil || d.Kind != "dots" || d.Color != "#1f8036" || d.Opacity != 0.12 || !d.Fade {
		t.Errorf("backdrop = %+v", d)
	}

	if s := state.Brand.Style; s == nil || s.CardOpacity != 0.7 || !s.CardBlur {
		t.Errorf("style = %+v", s)
	}

	// A picture must be the adventure's own image; once set, every team
	// may fetch it, though no step shows it.
	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"backdrop": map[string]any{"kind": "picture", "media": 999999, "opacity": 1, "scale": 100}}, nil); s != 400 {
		t.Errorf("picture from elsewhere = %d, want 400", s)
	}
	up, img := pub.upload("/api/admin/adventures/demo/media", tinyPNG(t, 90))
	if up != 200 && up != 201 {
		t.Fatalf("upload = %d", up)
	}
	pic := int64(img["id"].(float64))
	if p.status("/media/"+itoa(pic)) == 200 {
		t.Fatalf("an unused image was served to a team")
	}
	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"backdrop": map[string]any{"kind": "picture", "media": pic, "opacity": 0.8, "scale": 100, "blur": 6}}, nil); s != 200 && s != 204 {
		t.Fatalf("set picture = %d", s)
	}
	if s := p.status("/media/" + itoa(pic)); s != 200 {
		t.Errorf("the look's picture = %d for a team, want 200", s)
	}

	// A sound must be the adventure's own audio; once set, every team may fetch it.
	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"style": map[string]any{"sound_right": pic}}, nil); s != 400 {
		t.Errorf("a picture as a sound = %d, want 400", s)
	}
	up, snd := pub.uploadNamed("/api/admin/adventures/demo/media", "ding.wav", tinyWAV())
	if up != 200 && up != 201 {
		t.Fatalf("sound upload = %d", up)
	}
	ding := int64(snd["id"].(float64))
	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"style": map[string]any{"sound_right": ding, "confetti_emoji": "🐘", "cheer_text": "Snyggt!"}}, nil); s != 200 && s != 204 {
		t.Fatalf("set sound = %d", s)
	}
	if s := p.status("/media/" + itoa(ding)); s != 200 {
		t.Errorf("the look's sound = %d for a team, want 200", s)
	}

	// An empty kind takes the pattern away again.
	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"backdrop": map[string]any{"kind": ""}}, nil); s != 200 && s != 204 {
		t.Fatalf("clear backdrop = %d", s)
	}
	state.Brand.Backdrop = nil
	if s := p.do("GET", "/api/player/play", nil, &state); s != 200 {
		t.Fatalf("play state = %d", s)
	}
	if state.Brand.Backdrop != nil {
		t.Errorf("backdrop after clearing = %+v, want none", state.Brand.Backdrop)
	}
	if o := state.OrgBrand; o.Accent != "#00aa00" || o.Background != "#fafafa" {
		t.Errorf("org brand = %+v, want the short #0a0 spelled out", o)
	}
}
