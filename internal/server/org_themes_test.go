package server_test

import (
	"testing"
)

// TestOrgThemeLibrary: an org admin keeps themes for all the org's
// adventures — without pictures or sounds, which belong to one adventure
// — and another organisation neither sees nor changes them.
func TestOrgThemeLibrary(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("publisher login = %d", s)
	}
	look := map[string]any{
		"accent":   "#ff3399",
		"backdrop": map[string]any{"kind": "picture", "media": 1, "opacity": 1, "scale": 24},
		"style":    map[string]any{"card_opacity": 0.8, "sound_right": 2},
	}
	if s := pub.doAny("POST", "/api/admin/org-themes", map[string]any{"name": "  ", "look": look}, nil); s != 400 {
		t.Errorf("blank name = %d, want 400", s)
	}
	if s := pub.doAny("POST", "/api/admin/org-themes", map[string]any{"name": "Bad", "look": map[string]any{"corners": "wobbly"}}, nil); s != 400 {
		t.Errorf("bad look = %d, want 400", s)
	}
	var made struct {
		Theme struct {
			ID   int64 `json:"id"`
			Look struct {
				Backdrop *struct{} `json:"backdrop"`
				Style    struct {
					CardOpacity float64 `json:"card_opacity"`
					SoundRight  int64   `json:"sound_right"`
				} `json:"style"`
			} `json:"look"`
		} `json:"theme"`
		DroppedMedia bool `json:"dropped_media"`
	}
	if s := pub.do("POST", "/api/admin/org-themes", map[string]any{"name": "House pink", "look": look}, &made); s != 201 {
		t.Fatalf("create = %d", s)
	}
	if !made.DroppedMedia || made.Theme.Look.Backdrop != nil || made.Theme.Look.Style.SoundRight != 0 || made.Theme.Look.Style.CardOpacity != 0.8 {
		t.Errorf("picture and sounds not dropped, or the rest lost: %+v", made)
	}
	if s := pub.doAny("POST", "/api/admin/org-themes", map[string]any{"name": "House pink", "look": map[string]any{}}, nil); s != 409 {
		t.Errorf("duplicate name = %d, want 409", s)
	}
	id := itoa(made.Theme.ID)
	if s := pub.doAny("PUT", "/api/admin/org-themes/"+id, map[string]any{"name": "House rose"}, nil); s != 200 {
		t.Errorf("rename = %d", s)
	}

	var list struct {
		Themes []struct {
			Name string `json:"name"`
			Look struct {
				Accent string `json:"accent"`
			} `json:"look"`
		} `json:"themes"`
	}
	if s := pub.do("GET", "/api/admin/org-themes", nil, &list); s != 200 || len(list.Themes) != 1 || list.Themes[0].Name != "House rose" || list.Themes[0].Look.Accent != "#ff3399" {
		t.Fatalf("list = %d %+v", s, list)
	}

	other := newClient(t, ts)
	if s := other.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("other org login = %d", s)
	}
	list.Themes = nil
	if s := other.do("GET", "/api/admin/org-themes", nil, &list); s != 200 || len(list.Themes) != 0 {
		t.Errorf("another org sees = %d %+v", s, list)
	}
	if s := other.doAny("PUT", "/api/admin/org-themes/"+id, map[string]any{"name": "Mine"}, nil); s != 404 {
		t.Errorf("another org renames = %d, want 404", s)
	}
	if s := other.doAny("DELETE", "/api/admin/org-themes/"+id, nil, nil); s != 404 {
		t.Errorf("another org deletes = %d, want 404", s)
	}
	if s := pub.doAny("DELETE", "/api/admin/org-themes/"+id, nil, nil); s != 204 {
		t.Errorf("delete = %d", s)
	}
}
