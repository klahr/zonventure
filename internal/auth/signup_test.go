package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
)

// TestEmailChange_RoundTrip drives IssueEmailChangeToken +
// ConsumeEmailChangeToken end-to-end against a real database so the
// post-commit reload exercises the live `users` schema. Regression
// guard for the dropped users.role / users.org_id columns: the
// previous reload SELECTed those columns and 500'd on every confirm.
func TestEmailChange_RoundTrip(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	uid := mustSeedUser(t, db)

	const newEmail = "alice+new@example.com"
	tok, err := IssueEmailChangeToken(ctx, db, uid, newEmail)
	if err != nil {
		t.Fatalf("IssueEmailChangeToken: %v", err)
	}
	if tok == "" {
		t.Fatalf("empty token")
	}

	u, err := ConsumeEmailChangeToken(ctx, db, tok)
	if err != nil {
		t.Fatalf("ConsumeEmailChangeToken: %v", err)
	}
	if u.ID != uid {
		t.Fatalf("returned user id %d, want %d", u.ID, uid)
	}
	if u.Email != newEmail {
		t.Fatalf("email after change = %q, want %q", u.Email, newEmail)
	}
	if u.Username != newEmail {
		t.Fatalf("username after change = %q, want %q", u.Username, newEmail)
	}
	if !u.EmailVerified {
		t.Fatalf("email_verified should be true after a successful change")
	}

	// Token must be single-use: a second consume must fail closed.
	if _, err := ConsumeEmailChangeToken(ctx, db, tok); !errors.Is(err, ErrVerificationUnknown) {
		t.Fatalf("second consume err = %v, want ErrVerificationUnknown", err)
	}
}

// TestEmailChange_UnknownToken returns the typed sentinel rather than
// a SQL error.
func TestEmailChange_UnknownToken(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	if _, err := ConsumeEmailChangeToken(context.Background(), db, "nope"); !errors.Is(err, ErrVerificationUnknown) {
		t.Fatalf("err = %v, want ErrVerificationUnknown", err)
	}
}
