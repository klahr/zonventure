package server_test

import (
	"net/http"
	"testing"

	"git.sr.ht/~klahr/zv/internal/server"
)

// (getRaw lives in team_share_test.go — reused here for hint-image
// fetches where the response body is binary, not JSON.)

// TestHints_UpsertPreservesIDs is the load-bearing test for hint
// images: the upsert in pages.go must UPDATE existing rows by id
// rather than wiping and reinserting, otherwise every page save
// would orphan every attached hint image. This test pins the
// invariant directly — IDs round-trip across an edit, only deleted
// hints lose their ID.
func TestHints_UpsertPreservesIDs(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	admin := newClient(t, ts)
	if s := admin.do("POST", "/api/auth/login",
		map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("admin login = %d", s)
	}
	pageID := demoPageID(t, admin)

	patch := func(hints []map[string]any) {
		if s := admin.do("PATCH", "/api/admin/pages/"+itoa(pageID), map[string]any{
			"title":  "Welcome",
			"prompt": "What word do you say when you meet someone for the first time?",
			"accept": acceptLits("hello", "hi"),
			"lat":    59.3308,
			"lng":    18.0731,
			"hints":  hints,
		}, nil); s != 204 {
			t.Fatalf("PATCH = %d", s)
		}
	}
	type hintRow struct {
		ID           int64  `json:"id"`
		Text         string `json:"text"`
		DelaySeconds int    `json:"delay_seconds"`
	}
	read := func() []hintRow {
		var resp struct {
			Pages []struct {
				ID    int64     `json:"id"`
				Hints []hintRow `json:"hints"`
			} `json:"pages"`
		}
		if s := admin.do("GET", "/api/admin/adventures/demo/edit", nil, &resp); s != 200 {
			t.Fatalf("GET = %d", s)
		}
		for _, p := range resp.Pages {
			if p.ID == pageID {
				return p.Hints
			}
		}
		t.Fatalf("page %d not in /edit response", pageID)
		return nil
	}

	// First save: two hints, no client-side IDs (the editor's first
	// encounter with this page).
	patch([]map[string]any{
		{"text": "First", "delay_seconds": 0},
		{"text": "Second", "delay_seconds": 60},
	})
	initial := read()
	if len(initial) != 2 {
		t.Fatalf("after first patch: %d hints, want 2", len(initial))
	}
	h1, h2 := initial[0].ID, initial[1].ID
	if h1 == 0 || h2 == 0 || h1 == h2 {
		t.Fatalf("bad ids on first read: %v / %v", h1, h2)
	}

	// Second save: keep h1 (with edited text + delay), drop h2,
	// insert a new third hint. Upsert must reuse h1's row, delete
	// h2's row, and mint a fresh id for the new entry.
	patch([]map[string]any{
		{"id": h1, "text": "First — edited", "delay_seconds": 30},
		{"text": "Third", "delay_seconds": 120},
	})
	after := read()
	if len(after) != 2 {
		t.Fatalf("after second patch: %d hints, want 2", len(after))
	}
	if after[0].ID != h1 {
		t.Errorf("first hint id changed: was %d, now %d (UPDATE was lost to a DELETE+INSERT)", h1, after[0].ID)
	}
	if after[0].Text != "First — edited" || after[0].DelaySeconds != 30 {
		t.Errorf("first hint update lost: %+v", after[0])
	}
	if after[1].ID == h1 || after[1].ID == h2 {
		t.Errorf("third hint should have a fresh id, got %d (h1=%d, h2=%d)", after[1].ID, h1, h2)
	}
	if after[1].Text != "Third" || after[1].DelaySeconds != 120 {
		t.Errorf("third hint = %+v", after[1])
	}
}

// TestHints_UpsertPreservesAttachedImages drives the actual scenario
// the upsert exists to protect: a hint with an image attached must
// keep that image after an edit to the hint's text.
func TestHints_UpsertPreservesAttachedImages(t *testing.T) {
	ts, db, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	var orgID int64
	if err := db.QueryRow(`SELECT org_id FROM users WHERE username = 'boss'`).Scan(&orgID); err != nil {
		t.Fatalf("read boss org: %v", err)
	}
	if _, err := db.Exec(`UPDATE adventures SET org_id = $1 WHERE slug = 'demo'`, orgID); err != nil {
		t.Fatalf("set demo org: %v", err)
	}
	var pageID int64
	if err := db.QueryRow(`
		INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept, lat, lng)
		SELECT id, 0, 'p', 'q', '[]', 0, 0 FROM adventures WHERE slug = 'demo'
		RETURNING id
	`).Scan(&pageID); err != nil {
		t.Fatalf("seed page: %v", err)
	}

	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("boss login = %d", s)
	}

	// Save one hint, read its id, then attach an image directly to
	// the hint via the DB (sidesteps the multipart upload + Shrink
	// pipeline; we're testing the upsert, not the upload).
	if s := boss.do("PATCH", "/api/admin/pages/"+itoa(pageID), map[string]any{
		"title": "p", "prompt": "q",
		"accept": acceptLits("x"),
		"lat":    0, "lng": 0,
		"hints": []map[string]any{{"text": "with image", "delay_seconds": 0}},
	}, nil); s != 204 {
		t.Fatalf("seed hint: %d", s)
	}
	var hintID int64
	if err := db.QueryRow(
		`SELECT id FROM adventure_page_hints WHERE page_id = $1`, pageID,
	).Scan(&hintID); err != nil {
		t.Fatalf("read hint id: %v", err)
	}
	var imageID int64
	if err := db.QueryRow(`
		INSERT INTO adventure_page_hint_images(hint_id, idx, mime_type, size_bytes, content)
		VALUES ($1, 0, 'image/png', 4, '\x89504e47'::bytea)
		RETURNING id
	`, hintID).Scan(&imageID); err != nil {
		t.Fatalf("seed hint image: %v", err)
	}

	// Edit the hint's text + delay, echoing its id. The image must
	// still be attached afterwards.
	if s := boss.do("PATCH", "/api/admin/pages/"+itoa(pageID), map[string]any{
		"title": "p", "prompt": "q",
		"accept": acceptLits("x"),
		"lat":    0, "lng": 0,
		"hints": []map[string]any{
			{"id": hintID, "text": "with image — edited", "delay_seconds": 90},
		},
	}, nil); s != 204 {
		t.Fatalf("re-patch = %d", s)
	}
	var stillThere int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM adventure_page_hint_images WHERE id = $1`, imageID,
	).Scan(&stillThere); err != nil {
		t.Fatalf("count: %v", err)
	}
	if stillThere != 1 {
		t.Fatalf("hint image was orphaned by a hint-text edit (count = %d)", stillThere)
	}

	// Conversely: removing the hint from the editor list must
	// cascade and drop its images. This pins the FK ON DELETE
	// CASCADE half of the contract.
	if s := boss.do("PATCH", "/api/admin/pages/"+itoa(pageID), map[string]any{
		"title": "p", "prompt": "q",
		"accept": acceptLits("x"),
		"lat":    0, "lng": 0,
		"hints":  []map[string]any{},
	}, nil); s != 204 {
		t.Fatalf("clear hints = %d", s)
	}
	var afterCascade int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM adventure_page_hint_images WHERE id = $1`, imageID,
	).Scan(&afterCascade); err != nil {
		t.Fatalf("count post-cascade: %v", err)
	}
	if afterCascade != 0 {
		t.Errorf("expected hint image to be cascade-deleted with its hint, got count=%d", afterCascade)
	}
}

// TestHints_CopyClonesImages ensures CopyPuzzleToAdventure carries
// per-hint image attachments across alongside the hint rows
// themselves — otherwise duplicating a library puzzle silently
// drops the author's annotations.
func TestHints_CopyClonesImages(t *testing.T) {
	ts, db, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	var orgID int64
	if err := db.QueryRow(`SELECT org_id FROM users WHERE username = 'boss'`).Scan(&orgID); err != nil {
		t.Fatalf("read boss org: %v", err)
	}
	if _, err := db.Exec(`UPDATE adventures SET org_id = $1 WHERE slug = 'demo'`, orgID); err != nil {
		t.Fatalf("set demo org: %v", err)
	}
	// Source: a library puzzle (no adventure_id) owned by boss's org,
	// carrying one hint with one image attached.
	var srcPageID int64
	if err := db.QueryRow(`
		INSERT INTO adventure_pages(idx, title, prompt, accept, lat, lng, org_id)
		VALUES (0, 'src', 'q', '[]', 0, 0, $1) RETURNING id
	`, orgID).Scan(&srcPageID); err != nil {
		t.Fatalf("seed source page: %v", err)
	}
	var srcHintID int64
	if err := db.QueryRow(`
		INSERT INTO adventure_page_hints(page_id, idx, text, delay_seconds)
		VALUES ($1, 0, 'clue', 0) RETURNING id
	`, srcPageID).Scan(&srcHintID); err != nil {
		t.Fatalf("seed source hint: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO adventure_page_hint_images(hint_id, idx, mime_type, size_bytes, content, alt_text)
		VALUES ($1, 0, 'image/png', 4, '\x89504e47'::bytea, 'tower')
	`, srcHintID); err != nil {
		t.Fatalf("seed source hint image: %v", err)
	}

	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login",
		map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("boss login = %d", s)
	}
	var copied struct {
		ID int64 `json:"id"`
	}
	if s := boss.do("POST",
		"/api/admin/adventures/demo/pages/copy-from/"+itoa(srcPageID),
		map[string]any{}, &copied); s != 201 {
		t.Fatalf("copy = %d", s)
	}

	// The clone's hint must carry an image with the same alt text.
	// We look up by joining through the hint to dodge any reliance
	// on image-id ordering.
	var altText string
	if err := db.QueryRow(`
		SELECT i.alt_text FROM adventure_page_hint_images i
		JOIN adventure_page_hints h ON h.id = i.hint_id
		WHERE h.page_id = $1
	`, copied.ID).Scan(&altText); err != nil {
		t.Fatalf("read clone hint image: %v", err)
	}
	if altText != "tower" {
		t.Errorf("clone hint image alt = %q, want \"tower\"", altText)
	}
}

// TestServeHintImage_VisibilityRevealGate is the security pin: a
// player with a slot session can only fetch hint images for hints
// they have actively revealed. Sitting on the page that owns the
// hint is not enough — guessing image IDs must fail until the
// player taps "Reveal hint".
func TestServeHintImage_VisibilityRevealGate(t *testing.T) {
	ts, db, orgID, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	// Seed: one page on demo, a hint (delay 0 so it's revealable
	// immediately), an image on that hint, and an assignment so
	// setupRun can mint a real event.
	var pageID int64
	if err := db.QueryRow(`
		INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept, lat, lng)
		SELECT id, 0, 'p', 'q', '[]', 0, 0 FROM adventures WHERE slug = 'demo'
		RETURNING id
	`).Scan(&pageID); err != nil {
		t.Fatalf("seed page: %v", err)
	}
	var hintID int64
	if err := db.QueryRow(`
		INSERT INTO adventure_page_hints(page_id, idx, text, delay_seconds)
		VALUES ($1, 0, 'clue', 0) RETURNING id
	`, pageID).Scan(&hintID); err != nil {
		t.Fatalf("seed hint: %v", err)
	}
	var imageID int64
	if err := db.QueryRow(`
		INSERT INTO adventure_page_hint_images(hint_id, idx, mime_type, size_bytes, content)
		VALUES ($1, 0, 'image/png', 4, '\x89504e47'::bytea)
		RETURNING id
	`, hintID).Scan(&imageID); err != nil {
		t.Fatalf("seed hint image: %v", err)
	}
	var assignmentID int64
	if err := db.QueryRow(`
		INSERT INTO adventure_assignments(org_id, adventure_slug, max_teams)
		VALUES ($1, 'demo', 3) RETURNING id
	`, orgID).Scan(&assignmentID); err != nil {
		t.Fatalf("seed assignment: %v", err)
	}

	arr, _, slotToken, eventID := setupRun(t, ts, assignmentID)
	if s := arr.do("POST", "/api/admin/events/"+itoa(eventID)+"/start",
		map[string]string{}, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	alice := loginPlayer(t, ts, slotToken)

	// Pre-reveal: alice is on page 0, hint is on page 0, but she
	// hasn't revealed it. The serve route must 403.
	s, _ := getRaw(t, alice, "/adventure-assets/hint-image/"+itoa(imageID))
	if s != http.StatusForbidden {
		t.Fatalf("pre-reveal player GET = %d, want 403", s)
	}

	// Reveal the hint, then re-fetch — must succeed.
	if rs := alice.do("POST", "/api/player/hint",
		map[string]any{"hint_index": 0}, nil); rs != 200 {
		t.Fatalf("reveal = %d", rs)
	}
	s, _ = getRaw(t, alice, "/adventure-assets/hint-image/"+itoa(imageID))
	if s != http.StatusOK {
		t.Fatalf("post-reveal player GET = %d, want 200", s)
	}

	// Staff sessions don't go through the reveal gate at all — they
	// can browse hint images straight from the editor.
	s, _ = getRaw(t, arr, "/adventure-assets/hint-image/"+itoa(imageID))
	if s != http.StatusOK {
		t.Fatalf("staff GET = %d, want 200", s)
	}
}
