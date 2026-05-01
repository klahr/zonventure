package server_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestManifest_IncludesAllAdventureAssets pins the contract the
// player-side service worker depends on: /api/player/manifest must
// return every puzzle + hint asset URL for the team's adventure, so
// the SW can cache.addAll() them on team join. A regression that
// silently drops hint assets would break offline-tolerant play
// without any visible error — the player would still see the
// current puzzle's media (cached lazily), but a hint asset would
// 404 once they reach a dead spot.
func TestManifest_IncludesAllAdventureAssets(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	page0ID := demoPageIDByIndex(t, admin, 0)
	page2ID := demoPageIDByIndex(t, admin, 2)

	// Attach one audio clip to puzzle 0 and one to puzzle 2 so the
	// manifest must walk every page in author order.
	status, body0 := uploadFile(t, admin,
		"/api/admin/pages/"+itoa(page0ID)+"/audio",
		"a.mp3", "audio/mpeg", fakeMP3)
	if status != 201 {
		t.Fatalf("upload page0 audio = %d", status)
	}
	var a0 struct{ URL string `json:"url"` }
	_ = json.Unmarshal(body0, &a0)
	status, body2 := uploadFile(t, admin,
		"/api/admin/pages/"+itoa(page2ID)+"/audio",
		"b.mp3", "audio/mpeg", fakeMP3)
	if status != 201 {
		t.Fatalf("upload page2 audio = %d", status)
	}
	var a2 struct{ URL string `json:"url"` }
	_ = json.Unmarshal(body2, &a2)

	// Hint with audio attached so the manifest includes the
	// reveal-gated asset URL too. The SW will get a 403 on those
	// at prefetch time, which is fine — it skips them silently and
	// re-tries on cache miss after the player reveals.
	if s := admin.do("PATCH", "/api/admin/pages/"+itoa(page0ID), map[string]any{
		"title":  "Welcome",
		"prompt": "What word do you say when you meet someone for the first time?",
		"accept": acceptLits("hello", "hi"),
		"lat":    59.3308,
		"lng":    18.0731,
		"hints":  []map[string]any{{"text": "Now", "delay_seconds": 0}},
	}, nil); s != 204 {
		t.Fatalf("PATCH hints = %d", s)
	}
	hintID := firstHintID(t, admin, page0ID)
	status, bodyH := uploadFile(t, admin,
		"/api/admin/hints/"+itoa(hintID)+"/audio",
		"h.mp3", "audio/mpeg", fakeMP3)
	if status != 201 {
		t.Fatalf("upload hint audio = %d", status)
	}
	var ah struct{ URL string `json:"url"` }
	_ = json.Unmarshal(bodyH, &ah)

	// Now spin up an event, log in as alice, fetch the manifest.
	arr, _, slotToken, eventID := setupRun(t, ts, assignmentID)
	if s := arr.do("POST", "/api/admin/events/"+itoa(eventID)+"/start",
		map[string]string{}, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	alice := loginPlayer(t, ts, slotToken)

	var m struct {
		AdventureSlug string   `json:"adventure_slug"`
		URLs          []string `json:"urls"`
	}
	if s := alice.do("GET", "/api/player/manifest", nil, &m); s != 200 {
		t.Fatalf("manifest = %d", s)
	}
	if m.AdventureSlug != "demo" {
		t.Errorf("adventure_slug = %q, want demo", m.AdventureSlug)
	}
	want := []string{a0.URL, a2.URL, ah.URL}
	for _, w := range want {
		if !contains(m.URLs, w) {
			t.Errorf("manifest missing %q\nfull manifest: %v", w, m.URLs)
		}
	}
	// The hint URL must be the reveal-gated /hint-audio/ path so the
	// SW prefetch fails closed there, not the looser /audio/ path
	// which any team member can fetch.
	if !strings.Contains(ah.URL, "/hint-audio/") {
		t.Errorf("hint audio URL %q should route through /hint-audio/", ah.URL)
	}
}

// TestManifest_RequiresSlot pins the auth boundary on the manifest
// endpoint. The SW prefetch is privileged data — knowing the asset
// IDs in an adventure is enough for someone to probe per-asset
// visibility. Anonymous + arranger-only sessions must 401, only
// slot-authenticated players see their team's manifest.
func TestManifest_RequiresSlot(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	anon := newClient(t, ts)
	if s := anon.do("GET", "/api/player/manifest", nil, nil); s != 401 {
		t.Errorf("anonymous manifest = %d, want 401", s)
	}

	// An arranger has a staff cookie but no slot — same 401 as
	// the rest of /api/player/* would issue.
	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("arr login = %d", s)
	}
	if s := arr.do("GET", "/api/player/manifest", nil, nil); s != 401 {
		t.Errorf("arranger manifest = %d, want 401", s)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
