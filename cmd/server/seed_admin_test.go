package main

import (
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
)

// Only the first global admin becomes a listing reviewer by default.
func TestSeedAdminUser_FirstAdminReviews(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	if _, err := db.Exec(`UPDATE users SET is_admin = FALSE`); err != nil {
		t.Fatal(err)
	}
	for _, creds := range []string{"first:pw-first-1", "second:pw-second-1", "first:pw-first-2"} {
		if err := seedAdminUser(db, creds); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]bool{"first": true, "second": false} {
		var got bool
		if err := db.QueryRow(`SELECT listing_reviewer FROM users WHERE username = $1`, name).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s listing_reviewer = %v, want %v", name, got, want)
		}
	}
}
