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
	if s := pub.doAny("PATCH", "/api/admin/adventures/demo", map[string]any{"corners": "sharp", "cards": "flat", "card_outline": true, "heading_font": "fredoka", "accent_color": "#123456"}, nil); s != 200 && s != 204 {
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
	if o := state.OrgBrand; o.Accent != "#00aa00" || o.Background != "#fafafa" {
		t.Errorf("org brand = %+v, want the short #0a0 spelled out", o)
	}
}
