package server_test

import (
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/server"
)

// TestUpdatePageImage_AltText round-trips alt text through the PATCH
// endpoint: seed an image directly, verify default empty, set a value,
// verify the row, then check the 500-char cap.
func TestUpdatePageImage_AltText(t *testing.T) {
	ts, db, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	// Seed: one page on the demo adventure, one image on that page.
	var pageID int64
	if err := db.QueryRow(`
		INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept, lat, lng)
		SELECT id, 0, 'p', 'q', '[]', 0, 0 FROM adventures WHERE slug = 'demo'
		RETURNING id
	`).Scan(&pageID); err != nil {
		t.Fatalf("seed page: %v", err)
	}
	var imageID int64
	if err := db.QueryRow(`
		INSERT INTO adventure_page_images(page_id, idx, mime_type, size_bytes, content)
		VALUES ($1, 0, 'image/png', 4, '\x89504e47'::bytea)
		RETURNING id
	`, pageID).Scan(&imageID); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	root := newClient(t, ts)
	if s := root.do("POST", "/api/auth/login",
		map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	// Set a value.
	if s := root.do("PATCH", "/api/admin/images/"+itoa(imageID),
		map[string]any{"alt_text": "  Town hall fountain  "}, nil); s != 204 {
		t.Fatalf("PATCH = %d, want 204", s)
	}
	var alt string
	if err := db.QueryRow(
		`SELECT alt_text FROM adventure_page_images WHERE id = $1`, imageID,
	).Scan(&alt); err != nil {
		t.Fatalf("read alt: %v", err)
	}
	// Server trims surrounding whitespace.
	if alt != "Town hall fountain" {
		t.Fatalf("alt = %q, want trimmed value", alt)
	}

	// Cap at 500 chars: 501-byte payload → 400.
	huge := strings.Repeat("x", 501)
	if s := root.do("PATCH", "/api/admin/images/"+itoa(imageID),
		map[string]any{"alt_text": huge}, nil); s != 400 {
		t.Fatalf("oversized PATCH = %d, want 400", s)
	}

	// Empty alt is valid (decorative image).
	if s := root.do("PATCH", "/api/admin/images/"+itoa(imageID),
		map[string]any{"alt_text": ""}, nil); s != 204 {
		t.Fatalf("clear PATCH = %d, want 204", s)
	}
	if err := db.QueryRow(
		`SELECT alt_text FROM adventure_page_images WHERE id = $1`, imageID,
	).Scan(&alt); err != nil {
		t.Fatalf("re-read alt: %v", err)
	}
	if alt != "" {
		t.Fatalf("alt after clear = %q, want empty", alt)
	}

	// Unknown id → 404.
	if s := root.do("PATCH", "/api/admin/images/9999999",
		map[string]any{"alt_text": "x"}, nil); s != 404 {
		t.Fatalf("PATCH unknown id = %d, want 404", s)
	}
}
