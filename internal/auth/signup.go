package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"git.sr.ht/~klahr/zv/internal/tokenx"
)

// VerificationTTL is how long a "verify your email" token stays valid
// after issuance. Short enough that a leaked link is harmless within
// hours, long enough that a busy user doesn't have to scramble.
const VerificationTTL = time.Hour

// VerificationPurpose is the value stamped into email_verifications.purpose.
// Reserving the column for forgot-password reuse later — we don't want
// a verify-purpose token accidentally being accepted as a reset, so the
// table is shared but the purpose is checked at consumption time.
const (
	PurposeVerify         = "verify"
	PurposeForgotPassword = "forgot_password"
	PurposeChangeEmail    = "change_email"
)

// Signup-related errors.
var (
	ErrSignupEmailTaken    = errors.New("email already registered")
	ErrSignupOrgNameTaken  = errors.New("organization name already in use")
	ErrSignupBadEmail      = errors.New("email address is not valid")
	ErrSignupBadOrgName    = errors.New("organization name is empty")
	ErrVerificationUnknown = errors.New("verification token unknown")
	ErrVerificationExpired = errors.New("verification token expired")
)

// SignupResult is what a successful Signup returns: the newly-minted
// org_admin user, the staff session token (so the caller can drop the
// cookie and put the user straight into the app), and the
// email-verification token the caller must email out.
type SignupResult struct {
	User              User
	SessionToken      string
	VerificationToken string
}

// Signup creates an organization + an org_admin user + an unverified
// email + a one-shot verification token, all in one transaction so a
// crash mid-way leaves no half-rows. The username column doubles as the
// login identifier and is set to the (lower-cased, normalised) email
// — keeps the form simple (no username field for the new arranger to
// invent).
func Signup(ctx context.Context, db *sql.DB, email, password, orgName string) (SignupResult, error) {
	email = strings.TrimSpace(email)
	orgName = strings.TrimSpace(orgName)
	if email == "" || !strings.Contains(email, "@") {
		return SignupResult{}, ErrSignupBadEmail
	}
	if orgName == "" {
		return SignupResult{}, ErrSignupBadOrgName
	}
	if err := ValidatePassword(password); err != nil {
		return SignupResult{}, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return SignupResult{}, err
	}
	defer tx.Rollback() //nolint:errcheck — Commit takes over on success

	var orgID int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO organizations(name) VALUES ($1) RETURNING id`, orgName).Scan(&orgID); err != nil {
		// Postgres reports a unique-violation error for both unique
		// columns we touch (organizations.name and users.email);
		// since we know the table here, attribute it correctly.
		return SignupResult{}, fmt.Errorf("%w: %v", ErrSignupOrgNameTaken, err)
	}

	hash, err := HashPassword(password)
	if err != nil {
		return SignupResult{}, err
	}

	var userID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO users(username, email, password_hash, email_verified)
		VALUES ($1, $2, $3, 0)
		RETURNING id
	`, email, email, hash).Scan(&userID); err != nil {
		return SignupResult{}, fmt.Errorf("%w: %v", ErrSignupEmailTaken, err)
	}

	// Self-signup always creates an org_admin membership in the
	// just-minted org — the user is the only person in there, so
	// they get the keys.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')
	`, userID, orgID); err != nil {
		return SignupResult{}, err
	}

	verifyTok, err := newVerificationToken()
	if err != nil {
		return SignupResult{}, err
	}
	expiresAt := time.Now().Add(VerificationTTL).UTC().Format("2006-01-02 15:04:05")
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO email_verifications(token, user_id, purpose, expires_at)
		VALUES ($1, $2, $3, $4)
	`, verifyTok, userID, PurposeVerify, expiresAt); err != nil {
		return SignupResult{}, err
	}

	// Issue the staff session inside the same tx so login is "free"
	// after a successful signup. The cookie is set by the handler.
	// active_org_id is the just-created org — signup always lands the
	// caller into their new org, no picker needed.
	sessionTok, err := newSession(ctx, txAsExecutor{tx}, userID, "", "", sql.NullInt64{Int64: orgID, Valid: true})
	if err != nil {
		return SignupResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return SignupResult{}, err
	}
	return SignupResult{
		User: User{
			ID: userID, Username: email, IsAdmin: false, Role: RoleOrgAdmin,
			OrgID: &orgID, Email: email, EmailVerified: false,
		},
		SessionToken:      sessionTok,
		VerificationToken: verifyTok,
	}, nil
}

// VerifyEmail consumes a one-shot verification token: marks the user's
// email_verified, deletes the token row (single-use), and returns the
// fully-loaded User so the caller can refresh the session whoami view
// with the new flag.
func VerifyEmail(ctx context.Context, db *sql.DB, token string) (User, error) {
	if token == "" {
		return User{}, ErrVerificationUnknown
	}
	var (
		userID    int64
		purpose   string
		expiresAt string
	)
	err := db.QueryRowContext(ctx, `
		SELECT user_id, purpose, expires_at FROM email_verifications WHERE token = $1
	`, token).Scan(&userID, &purpose, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrVerificationUnknown
	}
	if err != nil {
		return User{}, err
	}
	if purpose != PurposeVerify {
		return User{}, ErrVerificationUnknown
	}
	// Fail closed on parse: a corrupt expires_at value mustn't extend a
	// token's life. Treat unparseable rows as expired and delete them.
	if t, perr := parseSlotTimestamp(expiresAt); perr != nil || time.Now().After(t) {
		_, _ = db.ExecContext(ctx, `DELETE FROM email_verifications WHERE token = $1`, token)
		return User{}, ErrVerificationExpired
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET email_verified = 1 WHERE id = $1`, userID); err != nil {
		return User{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM email_verifications WHERE token = $1`, token); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	// Reload the user — caller may want to refresh whoami. Role +
	// OrgID are derived from is_admin and the only-membership-pin
	// (same logic as Login post-credentials).
	var u User
	if err := db.QueryRowContext(ctx, `
		SELECT id, username, is_admin, COALESCE(email,''), (email_verified <> 0)
		FROM users WHERE id = $1
	`, userID).Scan(&u.ID, &u.Username, &u.IsAdmin, &u.Email, &u.EmailVerified); err != nil {
		return User{}, err
	}
	active, role, err := resolveInitialActiveOrg(ctx, db, u.ID)
	if err != nil {
		return User{}, err
	}
	if active.Valid {
		v := active.Int64
		u.OrgID = &v
	}
	if u.IsAdmin {
		u.Role = RoleAdmin
	} else {
		u.Role = role
	}
	return u, nil
}

// IssueForgotPasswordToken mints a single-use reset token for the
// given user. Drops any prior reset-purpose tokens for the same user
// (so a stale email link can't outlive a fresh request) and returns
// the new token string for the caller to email.
func IssueForgotPasswordToken(ctx context.Context, db *sql.DB, userID int64) (string, error) {
	return issueOneShotToken(ctx, db, userID, PurposeForgotPassword)
}

// ConsumeForgotPasswordToken validates a reset token and atomically:
// rewrites password_hash, deletes ALL of the user's existing sessions
// (kicks any logged-in clients on other devices), deletes the token,
// and issues a fresh session. The new session token is returned so
// the caller can drop it as a cookie immediately, completing the
// reset → logged-in transition in one round-trip.
func ConsumeForgotPasswordToken(ctx context.Context, db *sql.DB, token, newPassword string) (User, string, error) {
	if token == "" {
		return User{}, "", ErrVerificationUnknown
	}
	if err := ValidatePassword(newPassword); err != nil {
		return User{}, "", err
	}
	var (
		userID    int64
		purpose   string
		expiresAt string
	)
	err := db.QueryRowContext(ctx, `
		SELECT user_id, purpose, expires_at FROM email_verifications WHERE token = $1
	`, token).Scan(&userID, &purpose, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrVerificationUnknown
	}
	if err != nil {
		return User{}, "", err
	}
	if purpose != PurposeForgotPassword {
		return User{}, "", ErrVerificationUnknown
	}
	// Fail closed on parse: a corrupt expires_at value mustn't extend a
	// token's life. Treat unparseable rows as expired and delete them.
	if t, perr := parseSlotTimestamp(expiresAt); perr != nil || time.Now().After(t) {
		_, _ = db.ExecContext(ctx, `DELETE FROM email_verifications WHERE token = $1`, token)
		return User{}, "", ErrVerificationExpired
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return User{}, "", err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, "", err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET password_hash = $1 WHERE id = $2`, hash, userID); err != nil {
		return User{}, "", err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
		return User{}, "", err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM email_verifications WHERE token = $1`, token); err != nil {
		return User{}, "", err
	}
	// Pin the new session to whichever org the user has if it's the
	// only one — same logic as Login. Multi-org users get NULL and
	// land on the picker after the reset.
	var (
		onlyOrg sql.NullInt64
		memCnt  int
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT MIN(org_id), COUNT(*) FROM org_memberships WHERE user_id = $1
	`, userID).Scan(&onlyOrg, &memCnt); err != nil {
		return User{}, "", err
	}
	var initialActive sql.NullInt64
	if memCnt == 1 {
		initialActive = onlyOrg
	}
	sessionTok, err := newSession(ctx, txAsExecutor{tx}, userID, "", "", initialActive)
	if err != nil {
		return User{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return User{}, "", err
	}

	var u User
	if err := db.QueryRowContext(ctx, `
		SELECT id, username, is_admin, COALESCE(email,''), (email_verified <> 0)
		FROM users WHERE id = $1
	`, userID).Scan(&u.ID, &u.Username, &u.IsAdmin, &u.Email, &u.EmailVerified); err != nil {
		return User{}, "", err
	}
	if initialActive.Valid {
		v := initialActive.Int64
		u.OrgID = &v
	}
	if u.IsAdmin {
		u.Role = RoleAdmin
	} else if memCnt == 1 {
		// Single membership — fetch its role for the User struct.
		var memRole string
		_ = db.QueryRowContext(ctx,
			`SELECT role FROM org_memberships WHERE user_id = $1`, userID,
		).Scan(&memRole)
		u.Role = memRole
	}
	return u, sessionTok, nil
}

// IssueVerificationToken mints a fresh verify-purpose token for an
// existing user. Used by the resend-verification endpoint. Drops any
// existing verify-purpose tokens for the user so old links can't be
// used to leak that the account exists.
func IssueVerificationToken(ctx context.Context, db *sql.DB, userID int64) (string, error) {
	return issueOneShotToken(ctx, db, userID, PurposeVerify)
}

// IssueEmailChangeToken stores the pending new email in the
// email_verifications.data column and mints a token the caller emails
// to that new address. Replaces any prior change_email token for the
// same user so a regretted change doesn't leave a live old token.
func IssueEmailChangeToken(ctx context.Context, db *sql.DB, userID int64, newEmail string) (string, error) {
	tok, err := newVerificationToken()
	if err != nil {
		return "", err
	}
	expiresAt := time.Now().Add(VerificationTTL).UTC().Format("2006-01-02 15:04:05")
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM email_verifications WHERE user_id = $1 AND purpose = $2`,
		userID, PurposeChangeEmail); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO email_verifications(token, user_id, purpose, expires_at, data)
		VALUES ($1, $2, $3, $4, $5)
	`, tok, userID, PurposeChangeEmail, expiresAt, newEmail); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return tok, nil
}

// ConsumeEmailChangeToken validates a change_email token and atomically
// rewrites users.email + users.username (we use email as username) to
// the value previously stashed on the token row, deletes the token,
// and reloads the user. Username uniqueness is enforced by the
// existing CITEXT constraint, so a race between two pending change
// requests collapses cleanly into one winner + one error.
func ConsumeEmailChangeToken(ctx context.Context, db *sql.DB, token string) (User, error) {
	if token == "" {
		return User{}, ErrVerificationUnknown
	}
	var (
		userID    int64
		purpose   string
		expiresAt string
		newEmail  sql.NullString
	)
	err := db.QueryRowContext(ctx, `
		SELECT user_id, purpose, expires_at, data FROM email_verifications WHERE token = $1
	`, token).Scan(&userID, &purpose, &expiresAt, &newEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrVerificationUnknown
	}
	if err != nil {
		return User{}, err
	}
	if purpose != PurposeChangeEmail || !newEmail.Valid || newEmail.String == "" {
		return User{}, ErrVerificationUnknown
	}
	// Fail closed on parse: a corrupt expires_at value mustn't extend a
	// token's life. Treat unparseable rows as expired and delete them.
	if t, perr := parseSlotTimestamp(expiresAt); perr != nil || time.Now().After(t) {
		_, _ = db.ExecContext(ctx, `DELETE FROM email_verifications WHERE token = $1`, token)
		return User{}, ErrVerificationExpired
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	// Stash the previous email + timestamp so the user can revert
	// (within a grace window) if they realise they typed the new
	// address wrong. The grace period is enforced at revert time, not
	// here — keeping the revert decision out of the consume path so a
	// schema-only undo flow stays simple.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `
		UPDATE users SET
		  previous_email        = email,
		  previous_email_set_at = $1,
		  email                 = $2,
		  username              = $2,
		  email_verified        = 1
		WHERE id = $3
	`, now, newEmail.String, userID); err != nil {
		return User{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM email_verifications WHERE token = $1`, token); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}

	var (
		u     User
		orgID sql.NullInt64
	)
	if err := db.QueryRowContext(ctx, `
		SELECT id, username, role, org_id, COALESCE(email,''), (email_verified <> 0)
		FROM users WHERE id = $1
	`, userID).Scan(&u.ID, &u.Username, &u.Role, &orgID, &u.Email, &u.EmailVerified); err != nil {
		return User{}, err
	}
	if orgID.Valid {
		v := orgID.Int64
		u.OrgID = &v
	}
	return u, nil
}

// issueOneShotToken is the shared body of IssueVerificationToken and
// IssueForgotPasswordToken — same shape, different purpose, different
// row-cleanup scope. Worth one helper rather than two near-duplicates.
func issueOneShotToken(ctx context.Context, db *sql.DB, userID int64, purpose string) (string, error) {
	tok, err := newVerificationToken()
	if err != nil {
		return "", err
	}
	expiresAt := time.Now().Add(VerificationTTL).UTC().Format("2006-01-02 15:04:05")
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM email_verifications WHERE user_id = $1 AND purpose = $2`, userID, purpose); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO email_verifications(token, user_id, purpose, expires_at)
		VALUES ($1, $2, $3, $4)
	`, tok, userID, purpose, expiresAt); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return tok, nil
}

func newVerificationToken() (string, error) { return tokenx.New(24) }
