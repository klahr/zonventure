package auth

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/dbtest"
)

// TestEmailChange_RoundTrip drives IssueEmailChangeToken +
// ConsumeEmailChangeToken end-to-end against a real database so the
// post-commit reload exercises the live `users` schema. Regression
// guard for the dropped users.role / users.org_id columns: the
// previous reload SELECTed those columns and 500'd on every confirm.
func TestEmailChange_RoundTrip(t *testing.T) {
	t.Parallel()
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

// TestEmailChange_KeepsChosenUsername: a username that isn't the email
// ("boss") survives an email change, so the user can still log in with it.
func TestEmailChange_KeepsChosenUsername(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	uid := mustSeedUser(t, db)
	if _, err := db.ExecContext(ctx, `UPDATE users SET username = 'boss' WHERE id = $1`, uid); err != nil {
		t.Fatal(err)
	}
	tok, err := IssueEmailChangeToken(ctx, db, uid, "boss@example.org")
	if err != nil {
		t.Fatalf("IssueEmailChangeToken: %v", err)
	}
	u, err := ConsumeEmailChangeToken(ctx, db, tok)
	if err != nil {
		t.Fatalf("ConsumeEmailChangeToken: %v", err)
	}
	if u.Username != "boss" || u.Email != "boss@example.org" {
		t.Fatalf("after change: username %q email %q, want boss / boss@example.org", u.Username, u.Email)
	}
}

// TestEmailChange_RemembersPreviousVerification: an unverified old
// address stays unverified if the change is reverted.
func TestEmailChange_RemembersPreviousVerification(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	uid := mustSeedUser(t, db)
	if _, err := db.ExecContext(ctx, `UPDATE users SET email_verified = 0 WHERE id = $1`, uid); err != nil {
		t.Fatal(err)
	}
	tok, err := IssueEmailChangeToken(ctx, db, uid, "alice+v@example.com")
	if err != nil {
		t.Fatalf("IssueEmailChangeToken: %v", err)
	}
	if _, err := ConsumeEmailChangeToken(ctx, db, tok); err != nil {
		t.Fatalf("ConsumeEmailChangeToken: %v", err)
	}
	var prev int
	if err := db.QueryRowContext(ctx, `SELECT previous_email_verified FROM users WHERE id = $1`, uid).Scan(&prev); err != nil {
		t.Fatal(err)
	}
	if prev != 0 {
		t.Fatalf("previous_email_verified = %d, want 0", prev)
	}
}

// TestEmailChange_UnknownToken returns the typed sentinel rather than
// a SQL error.
func TestEmailChange_UnknownToken(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	if _, err := ConsumeEmailChangeToken(context.Background(), db, "nope"); !errors.Is(err, ErrVerificationUnknown) {
		t.Fatalf("err = %v, want ErrVerificationUnknown", err)
	}
}

// TestEmailRevert_Link: the old address's link puts it back, verified,
// signs out every session, revokes API tokens, drops a pending change
// and works once; a
// later change doesn't stop the first notice's link.
func TestEmailRevert_Link(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	uid := mustSeedUser(t, db)

	change := func(to string) {
		t.Helper()
		tok, err := IssueEmailChangeToken(ctx, db, uid, to)
		if err != nil {
			t.Fatalf("IssueEmailChangeToken: %v", err)
		}
		if _, err := ConsumeEmailChangeToken(ctx, db, tok); err != nil {
			t.Fatalf("ConsumeEmailChangeToken: %v", err)
		}
	}
	change("mallory@example.com")
	revert, err := IssueEmailRevertToken(ctx, db, uid, "alice@example.com", time.Hour)
	if err != nil {
		t.Fatalf("IssueEmailRevertToken: %v", err)
	}
	change("mallory2@example.com")
	if _, err := IssueEmailChangeToken(ctx, db, uid, "mallory3@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := newSession(ctx, db, uid, "", "", sql.NullInt64{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO mcp_tokens(user_id, token_hash, name, token_prefix) VALUES ($1, 'x', 'agent', 'zvmcp_x')`, uid); err != nil {
		t.Fatal(err)
	}

	u, err := ConsumeEmailRevertToken(ctx, db, revert)
	if err != nil {
		t.Fatalf("ConsumeEmailRevertToken: %v", err)
	}
	if u.Email != "alice@example.com" || u.Username != "alice@example.com" || !u.EmailVerified {
		t.Fatalf("after revert: %q / %q verified %v, want alice@example.com twice, verified", u.Email, u.Username, u.EmailVerified)
	}
	var sessions, pending, tokens int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM sessions WHERE user_id = $1), (SELECT COUNT(*) FROM email_verifications WHERE user_id = $1 AND purpose = $2), (SELECT COUNT(*) FROM mcp_tokens WHERE user_id = $1)`, uid, PurposeChangeEmail).Scan(&sessions, &pending, &tokens); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 || pending != 0 || tokens != 0 {
		t.Fatalf("sessions %d, pending changes %d, API tokens %d after revert, want 0, 0, 0", sessions, pending, tokens)
	}
	if _, err := ConsumeEmailRevertToken(ctx, db, revert); !errors.Is(err, ErrVerificationUnknown) {
		t.Fatalf("second use err = %v, want ErrVerificationUnknown", err)
	}
	expired, err := IssueEmailRevertToken(ctx, db, uid, "alice@example.com", -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConsumeEmailRevertToken(ctx, db, expired); !errors.Is(err, ErrVerificationExpired) {
		t.Fatalf("expired err = %v, want ErrVerificationExpired", err)
	}
	if _, err := ConsumeEmailRevertToken(ctx, db, "nope"); !errors.Is(err, ErrVerificationUnknown) {
		t.Fatalf("unknown err = %v, want ErrVerificationUnknown", err)
	}
}

func TestPersonalOrgName(t *testing.T) {
	cases := map[string]string{
		"anna.svensson@example.com": "Anna Svensson",
		"bob_jones42@example.com":   "Bob Jones",
		"åsa-lind@example.se":       "Åsa Lind",
		"KLAHR@example.com":         "Klahr",
		"12345@example.com":         "My adventures",
	}
	for email, want := range cases {
		if got := personalOrgName(email); got != want {
			t.Errorf("personalOrgName(%q) = %q, want %q", email, got, want)
		}
	}
}

// TestSignup_DisplayName: the optional name given at sign-up is what
// whoami greets by; none leaves it absent.
func TestSignup_DisplayName(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	h := &Handlers{DB: db}
	named, err := Signup(ctx, db, "sara@example.test", "a long passphrase 9", "", "  Sara N  ") // gitleaks:allow (test fixture)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.userResponse(ctx, named.User).DisplayName; got != "Sara N" {
		t.Errorf("display name = %q, want Sara N", got)
	}
	anon, err := Signup(ctx, db, "anon@example.test", "a long passphrase 9", "", "") // gitleaks:allow (test fixture)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.userResponse(ctx, anon.User).DisplayName; got != "" {
		t.Errorf("display name = %q, want none", got)
	}
}
