package server_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// fakeMP3 is a tiny byte payload that's not a real MP3 but is enough
// for the upload + storage round-trip. The handler doesn't decode
// audio — it just stores the bytes verbatim — so the "format" only
// matters insofar as the MIME type test demands `audio/*`.
var fakeMP3 = []byte{0xFF, 0xFB, 0x90, 0x44, 0x00, 0x00}

// TestAudio_PageUploadRoundTrip uploads an audio clip to a puzzle,
// fetches the served bytes, and confirms they match. Pins the new
// /api/admin/pages/{id}/audio + /adventure-assets/audio/{id} pair.
func TestAudio_PageUploadRoundTrip(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	pageID := demoPageID(t, admin)

	status, raw := uploadFile(t, admin,
		"/api/admin/pages/"+itoa(pageID)+"/audio",
		"clue.mp3", "audio/mpeg", fakeMP3)
	if status != http.StatusCreated {
		t.Fatalf("upload = %d, body=%s", status, raw)
	}
	var resp struct {
		ID          int64  `json:"id"`
		MimeType    string `json:"mime_type"`
		SizeBytes   int64  `json:"size_bytes"`
		URL         string `json:"url"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode upload resp: %v (body=%s)", err, raw)
	}
	if resp.ID == 0 || resp.URL == "" {
		t.Fatalf("missing id/url in upload response: %+v", resp)
	}
	if resp.MimeType != "audio/mpeg" {
		t.Errorf("mime_type = %q, want audio/mpeg", resp.MimeType)
	}
	if resp.SizeBytes != int64(len(fakeMP3)) {
		t.Errorf("size_bytes = %d, want %d", resp.SizeBytes, len(fakeMP3))
	}

	// Fetch back via the asset URL and confirm the bytes match.
	s, body := getRaw(t, admin, resp.URL)
	if s != 200 {
		t.Fatalf("fetch served audio = %d", s)
	}
	if body != string(fakeMP3) {
		t.Errorf("served bytes mismatch (len got=%d, want=%d)", len(body), len(fakeMP3))
	}
}

// TestAudio_PageRejectsNonAudioBytes is the load-bearing security
// pin on the upload handler. The check sniffs the actual bytes
// (http.DetectContentType) rather than the client-supplied header,
// so an attacker can't stash arbitrary blobs (HTML, JS, executables)
// under a forged "audio/mpeg" header. We exercise that by uploading
// HTML bytes WITH `audio/mpeg` Content-Type — header is friendly,
// bytes betray it, server still rejects.
func TestAudio_PageRejectsNonAudioBytes(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	pageID := demoPageID(t, admin)

	// HTML / JS / generic-binary blobs that lie about being audio.
	cases := []struct {
		name  string
		bytes []byte
	}{
		{"html", []byte("<!doctype html><html><body>hi</body></html>")},
		{"js", []byte("alert('xss');\n")},
		{"png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}},
	}
	for _, tc := range cases {
		// Lying header: claim audio/mpeg.
		status, _ := uploadFile(t, admin,
			"/api/admin/pages/"+itoa(pageID)+"/audio",
			"x", "audio/mpeg", tc.bytes)
		if status != http.StatusBadRequest {
			t.Errorf("upload bytes=%s with audio/mpeg header = %d, want 400 (server should sniff bytes)", tc.name, status)
		}
	}
}

// TestAudio_PageVisibleAcrossPagesInSameAdventure is the visibility
// regression for the canViewPageImage relaxation in 0.1.6: a player
// on puzzle 1 can fetch puzzle 0's (or puzzle 2's) audio so the
// service worker can prefetch the whole adventure for offline play.
//
// Before the relaxation the rule was "team is on this exact page",
// which made proactive prefetch impossible. The new rule is "team
// is in an event for this adventure". A regression to the old rule
// would silently break offline play; this test pins the new shape.
func TestAudio_PageVisibleAcrossPagesInSameAdventure(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	// Attach audio to puzzle 2 (the LAST page in the demo adventure).
	// The player will start on puzzle 0 — without the relaxation, the
	// fetch would 403.
	page2ID := demoPageIDByIndex(t, admin, 2)
	status, raw := uploadFile(t, admin,
		"/api/admin/pages/"+itoa(page2ID)+"/audio",
		"clue.mp3", "audio/mpeg", fakeMP3)
	if status != http.StatusCreated {
		t.Fatalf("upload = %d, body=%s", status, raw)
	}
	var up struct{ URL string `json:"url"` }
	_ = json.Unmarshal(raw, &up)

	arr, _, slotToken, eventID := setupRun(t, ts, assignmentID)
	if s := arr.do("POST", "/api/admin/events/"+itoa(eventID)+"/start",
		map[string]string{}, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	alice := loginPlayer(t, ts, slotToken)

	// Alice is on page 0; the audio is on page 2. Pre-relaxation
	// this would 403; post-relaxation it must serve.
	s, _ := getRaw(t, alice, up.URL)
	if s != http.StatusOK {
		t.Fatalf("cross-page audio fetch = %d, want 200 (visibility relaxation regression)", s)
	}
}

// TestAudio_HintGatedByReveal is the inverse pin: hint audio uses
// the same reveal-gated rule as hint images. A team on the hint's
// page but with the hint NOT yet revealed must 403; once the team
// taps Reveal, the same URL serves. Pinning so a future refactor
// of the hint visibility helpers doesn't accidentally relax this
// the way page audio was (correctly) relaxed.
func TestAudio_HintGatedByReveal(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	pageID := demoPageID(t, admin)
	if s := admin.do("PATCH", "/api/admin/pages/"+itoa(pageID), map[string]any{
		"title":  "Welcome",
		"prompt": "What word do you say when you meet someone for the first time?",
		"accept": acceptLits("hello", "hi"),
		"lat":    59.3308,
		"lng":    18.0731,
		"hints":  []map[string]any{{"text": "Now", "delay_seconds": 0}},
	}, nil); s != 204 {
		t.Fatalf("PATCH = %d", s)
	}
	hintID := firstHintID(t, admin, pageID)

	status, raw := uploadFile(t, admin,
		"/api/admin/hints/"+itoa(hintID)+"/audio",
		"hint.mp3", "audio/mpeg", fakeMP3)
	if status != http.StatusCreated {
		t.Fatalf("upload hint audio = %d, body=%s", status, raw)
	}
	var up struct{ URL string `json:"url"` }
	_ = json.Unmarshal(raw, &up)

	arr, _, slotToken, eventID := setupRun(t, ts, assignmentID)
	if s := arr.do("POST", "/api/admin/events/"+itoa(eventID)+"/start",
		map[string]string{}, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	alice := loginPlayer(t, ts, slotToken)

	// Pre-reveal: 403. The team is on the right page, but the hint
	// reveal record doesn't exist yet.
	if s, _ := getRaw(t, alice, up.URL); s != http.StatusForbidden {
		t.Fatalf("pre-reveal hint audio = %d, want 403", s)
	}

	// Reveal the hint, then re-fetch — must succeed.
	if s := alice.do("POST", "/api/player/hint",
		map[string]any{"hint_index": 0}, nil); s != 200 {
		t.Fatalf("reveal = %d", s)
	}
	if s, _ := getRaw(t, alice, up.URL); s != http.StatusOK {
		t.Fatalf("post-reveal hint audio = %d, want 200", s)
	}

	// Staff sessions skip the reveal gate entirely.
	if s, _ := getRaw(t, arr, up.URL); s != http.StatusOK {
		t.Fatalf("staff hint audio = %d, want 200", s)
	}
}

// demoPageIDByIndex is the multi-page sibling of demoPageID — finds
// the page id at a given index in the seeded demo adventure. Used
// by tests that need cross-page assertions.
func demoPageIDByIndex(t *testing.T, admin *client, idx int) int64 {
	t.Helper()
	var resp struct {
		Pages []struct {
			ID    int64 `json:"id"`
			Index int   `json:"index"`
		} `json:"pages"`
	}
	if s := admin.do("GET", "/api/admin/adventures/demo/edit", nil, &resp); s != 200 {
		t.Fatalf("GET /edit = %d", s)
	}
	for _, p := range resp.Pages {
		if p.Index == idx {
			return p.ID
		}
	}
	t.Fatalf("no page at index %d", idx)
	return 0
}

// firstHintID returns the id of the first hint on a page — the
// audio + reveal-gate tests only attach one hint and then need its
// id to address the upload route.
func firstHintID(t *testing.T, admin *client, pageID int64) int64 {
	t.Helper()
	var resp struct {
		Pages []struct {
			ID    int64 `json:"id"`
			Hints []struct {
				ID int64 `json:"id"`
			} `json:"hints"`
		} `json:"pages"`
	}
	if s := admin.do("GET", "/api/admin/adventures/demo/edit", nil, &resp); s != 200 {
		t.Fatalf("GET /edit = %d", s)
	}
	for _, p := range resp.Pages {
		if p.ID == pageID && len(p.Hints) > 0 {
			return p.Hints[0].ID
		}
	}
	t.Fatalf("no hint on page %d", pageID)
	return 0
}
