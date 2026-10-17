package auth_test

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/settings"
)

func TestOIDCConfigValidate_OpenRefusesHostedDomain(t *testing.T) {
	t.Parallel()
	err := auth.OIDCConfig{
		Enabled: true, Open: true,
		Issuer: "https://accounts.google.com", ClientID: "c", ClientSecret: "s",
		RedirectURL: "https://zv.example.com/auth/oidc/callback", HostedDomain: "example.com",
	}.Validate()
	if err == nil || !strings.Contains(err.Error(), "ZV_OIDC_HOSTED_DOMAIN") {
		t.Fatalf("Validate = %v, want an error naming ZV_OIDC_HOSTED_DOMAIN", err)
	}
}

func TestOIDCOpen_NewAccountWhenSignupOn(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	if err := settings.SetSignupEnabled(context.Background(), db, true); err != nil {
		t.Fatal(err)
	}
	h, fi := newOpenOIDCHandlers(t, db)

	resp := openSignIn(t, h, fi, "g-new", "new@gmail.com")
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/arrange" {
		t.Fatalf("got %d → %q, want 302 → /arrange", resp.StatusCode, resp.Header.Get("Location"))
	}
	if !hasCookie(resp, auth.CookieName) {
		t.Fatal("no session cookie")
	}
	var memberships int
	if err := db.QueryRow(`
		SELECT COUNT(m.org_id) FROM users u LEFT JOIN org_memberships m ON m.user_id = u.id
		WHERE u.email = $1 AND u.oidc_subject = 'g-new' AND u.email_verified = 1
	`, "new@gmail.com").Scan(&memberships); err != nil {
		t.Fatalf("user not provisioned: %v", err)
	}
	if memberships != 0 {
		t.Errorf("open mode put the new user in %d orgs, want none", memberships)
	}
	var orgs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM organizations`).Scan(&orgs); err != nil {
		t.Fatal(err)
	}
	if orgs != 0 {
		t.Errorf("open mode created %d organisations, want none", orgs)
	}
}

func TestOIDCOpen_NoAccountWhenSignupOff(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	if err := settings.SetSignupEnabled(context.Background(), db, false); err != nil {
		t.Fatal(err)
	}
	h, fi := newOpenOIDCHandlers(t, db)

	resp := openSignIn(t, h, fi, "g-stranger", "stranger@gmail.com")
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "sso-error=no_account") {
		t.Fatalf("location = %q, want sso-error=no_account", loc)
	}
	if hasCookie(resp, auth.CookieName) {
		t.Fatal("session cookie set without an account")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE email = $1`, "stranger@gmail.com").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("signup is off but a user was created")
	}
}

func TestOIDCOpen_LinksVerifiedPasswordAccount(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	userID, orgID := seedOpenUser(t, db, "alice@example.com", "fake-hash", 1)
	h, fi := newOpenOIDCHandlers(t, db)

	resp := openSignIn(t, h, fi, "g-alice", "Alice@example.com")
	if resp.Header.Get("Location") != "/arrange" || !hasCookie(resp, auth.CookieName) {
		t.Fatalf("got %q, want a session and /arrange", resp.Header.Get("Location"))
	}
	var (
		sub    string
		hash   sql.NullString
		member int64
	)
	if err := db.QueryRow(`
		SELECT u.oidc_subject, u.password_hash, m.org_id FROM users u
		JOIN org_memberships m ON m.user_id = u.id WHERE u.id = $1
	`, userID).Scan(&sub, &hash, &member); err != nil {
		t.Fatal(err)
	}
	if sub != "g-alice" || hash.String != "fake-hash" || member != orgID {
		t.Errorf("after link: sub=%q hash=%q org=%d, want g-alice, the password kept, org %d", sub, hash.String, member, orgID)
	}
}

func TestOIDCOpen_LinksInvitedAccount(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	if err := settings.SetSignupEnabled(context.Background(), db, false); err != nil {
		t.Fatal(err)
	}
	// An invitation to a new address makes an account with no password
	// and an unverified email.
	userID, _ := seedOpenUser(t, db, "bob@example.com", "", 0)
	h, fi := newOpenOIDCHandlers(t, db)

	resp := openSignIn(t, h, fi, "g-bob", "bob@example.com")
	if resp.Header.Get("Location") != "/arrange" || !hasCookie(resp, auth.CookieName) {
		t.Fatalf("got %q, want a session and /arrange", resp.Header.Get("Location"))
	}
	var sub string
	var verified int
	if err := db.QueryRow(`SELECT oidc_subject, email_verified FROM users WHERE id = $1`, userID).Scan(&sub, &verified); err != nil {
		t.Fatal(err)
	}
	if sub != "g-bob" || verified != 1 {
		t.Errorf("sub=%q verified=%d, want g-bob and 1", sub, verified)
	}
}

func TestOIDCOpen_RefusesUnverifiedPasswordAccount(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	userID, _ := seedOpenUser(t, db, "carol@example.com", "fake-hash", 0)
	h, fi := newOpenOIDCHandlers(t, db)

	resp := openSignIn(t, h, fi, "g-carol", "carol@example.com")
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "sso-error=account_exists") {
		t.Fatalf("location = %q, want sso-error=account_exists", loc)
	}
	var sub sql.NullString
	if err := db.QueryRow(`SELECT oidc_subject FROM users WHERE id = $1`, userID).Scan(&sub); err != nil {
		t.Fatal(err)
	}
	if sub.Valid {
		t.Errorf("oidc_subject stamped despite refusal: %q", sub.String)
	}
}

func TestOIDCOpen_RefusesAccountBoundToAnotherIdentity(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	userID, _ := seedOpenUser(t, db, "dave@example.com", "fake-hash", 1)
	if _, err := db.Exec(`UPDATE users SET oidc_provider = 'google', oidc_subject = 'g-old' WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	h, fi := newOpenOIDCHandlers(t, db)

	resp := openSignIn(t, h, fi, "g-other", "dave@example.com")
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "sso-error=account_exists") {
		t.Fatalf("location = %q, want sso-error=account_exists", loc)
	}
}

func TestOIDCOpen_AsksForTOTP(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	userID, _ := seedOpenUser(t, db, "erin@example.com", "fake-hash", 1)
	if _, err := db.Exec(`UPDATE users SET totp_enabled = TRUE WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	h, fi := newOpenOIDCHandlers(t, db)

	resp := openSignIn(t, h, fi, "g-erin", "erin@example.com")
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/arrange#sso-totp=") {
		t.Fatalf("location = %q, want the TOTP step", loc)
	}
	if hasCookie(resp, auth.CookieName) {
		t.Fatal("session minted before the second factor")
	}
	var origin string
	if err := db.QueryRow(`SELECT origin FROM auth_totp_challenges WHERE user_id = $1`, userID).Scan(&origin); err != nil {
		t.Fatal(err)
	}
	if origin != auth.ChallengeOIDC {
		t.Errorf("challenge origin = %q, want %q", origin, auth.ChallengeOIDC)
	}
}

func TestOIDCOpen_PasswordLoginStillAllowed(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	h, _ := newOpenOIDCHandlers(t, db)
	if h.OIDC.MatchesHostedDomain("anyone@gmail.com") {
		t.Fatal("open mode must not steer password logins to SSO")
	}
}

// ----- helpers -------------------------------------------------------

func newOpenOIDCHandlers(t *testing.T, db *sql.DB) (*auth.Handlers, *fakeIssuer) {
	t.Helper()
	fi := newFakeIssuer(t, "test-client")
	t.Cleanup(fi.Close)
	o, err := auth.NewOIDC(context.Background(), auth.OIDCConfig{
		Enabled:      true,
		Open:         true,
		Issuer:       fi.URL,
		ClientID:     fi.clientID,
		ClientSecret: "test-secret",
		RedirectURL:  "http://server/auth/oidc/callback",
	})
	if err != nil {
		t.Fatalf("NewOIDC: %v", err)
	}
	return &auth.Handlers{DB: db, Limiter: auth.NewLoginLimiter(), OIDC: o}, fi
}

// openSignIn runs one Google sign-in for (sub, email), verified.
func openSignIn(t *testing.T, h *auth.Handlers, fi *fakeIssuer, sub, email string) *http.Response {
	t.Helper()
	state, nonce, cookie := startLogin(t, h)
	code := "code-" + randomToken(t)
	fi.queue(code, idClaims{Sub: sub, Email: email, EmailVerified: true, Nonce: nonce})
	return callCallback(t, h, code, state, cookie)
}

// seedOpenUser makes a user in a fresh org. An empty hash leaves the
// password unset.
func seedOpenUser(t *testing.T, db *sql.DB, email, hash string, verified int) (userID, orgID int64) {
	t.Helper()
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ($1) RETURNING id`, "Org of "+email).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO users(username, email, password_hash, email_verified)
		VALUES ($1, $1, NULLIF($2, ''), $3) RETURNING id
	`, email, hash, verified).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, userID, orgID); err != nil {
		t.Fatal(err)
	}
	return userID, orgID
}
