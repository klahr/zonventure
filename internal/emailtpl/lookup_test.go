package emailtpl_test

import (
	"context"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/emailtpl"
)

// A new logo gets a new address: mail clients keep an image they have
// seen under a URL, so the URL must change with the picture.
func TestLookupLogoURLFollowsTheLogo(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()

	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if got := emailtpl.Lookup(ctx, db, "https://zv.example", orgID, 0).Brand.LogoURL; got != "" {
		t.Errorf("no logo, no address: %q", got)
	}
	setLogo := func(b []byte) string {
		t.Helper()
		if _, err := db.Exec(`UPDATE organizations SET logo_content = $1, logo_size_bytes = $2, logo_mime_type = 'image/png' WHERE id = $3`, b, len(b), orgID); err != nil {
			t.Fatal(err)
		}
		return emailtpl.Lookup(ctx, db, "https://zv.example", orgID, 0).Brand.LogoURL
	}
	first := setLogo([]byte("old logo"))
	if !strings.HasPrefix(first, "https://zv.example/org-logo/") || !strings.Contains(first, "?v=") {
		t.Fatalf("logo address = %q", first)
	}
	if second := setLogo([]byte("new logo")); second == first {
		t.Errorf("a new logo keeps the old address %q", second)
	}
}
