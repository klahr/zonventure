package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/logx"
	"git.sr.ht/~klahr/zv/internal/mailer"
	"git.sr.ht/~klahr/zv/internal/metrics"
)

// Handlers wraps the dependencies of the auth HTTP endpoints.
type Handlers struct {
	DB           *sql.DB
	SecureCookie bool // set to true when behind TLS
	// Pricing carries the operator's catalog price per slot, for client
	// totals. Zero values render as "$0.00" — operator should always set
	// this; the server's actual charge is enforced separately.
	Pricing Pricing
	// Limiter throttles failed login attempts. Optional: nil disables.
	Limiter *LoginLimiter
	// ForgotLimiter is a separate per-IP bucket for /forgot. We keep it
	// distinct from Limiter so a user who has just tripped the login
	// per-IP cap (because they forgot their password and tried a few
	// guesses first) can still reach the password-reset flow. The
	// per-(ip, email) pair throttle is still enforced inside this
	// limiter, so an attacker enumerating addresses is still slowed.
	// Optional: nil disables.
	ForgotLimiter *LoginLimiter
	// TOTPLimiter throttles per-(ip, challenge-token) brute force on
	// the /api/auth/login/totp endpoint. The 6-digit TOTP space is
	// 10^6, trivially enumerable without a rate limit. Sibling of
	// Limiter rather than a shared bucket so TOTP misses don't lock
	// out password attempts and vice versa. Optional: nil disables.
	TOTPLimiter *LoginLimiter
	// TOTPKey is the AES-GCM key used to wrap users.totp_secret_encrypted.
	// Loaded once at boot via auth.LoadTOTPKey. Nil disables the TOTP
	// surface entirely (enrol endpoints 500 with a clear error,
	// existing enrolments behave as if disabled — the login flow
	// short-circuits before any decrypt is attempted).
	TOTPKey []byte
	// Mailer delivers signup verification + future forgot-password
	// emails. Always set in production; tests can leave nil and the
	// handler skips the send.
	Mailer mailer.Mailer
	// PublicURL is the externally-reachable origin (no trailing slash)
	// used to build links in outbound mail. e.g. "https://zv.example".
	PublicURL string
	// OIDC is the configured single-sign-on client. nil disables SSO
	// entirely — the /auth/oidc/* endpoints 404 and the login UI
	// hides the SSO button. Built once at boot via auth.NewOIDC.
	OIDC *OIDC
}

// Pricing is the small slice of catalog data the frontend needs to
// render totals before submitting a purchase. Mirrors what the server
// will charge so the UI doesn't have to keep its own constant. Surfaced
// on /whoami so the bootstrap fetch already carries it.
//
// FreePlayersPerEvent is subtracted from the player count before
// multiplying by PlayerCents — the buyer sees "first N free, then €X
// per player" without doing arithmetic in their head.
type Pricing struct {
	PlayerCents         int    `json:"player_cents"`
	FreePlayersPerEvent int    `json:"free_players_per_event"`
	Currency            string `json:"currency"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type membershipDTO struct {
	OrgID   int64  `json:"org_id"`
	OrgName string `json:"org_name"`
	Role    string `json:"role"`
	// OrgLogoURL is the canonical /org-logo/{id} path when the
	// organisation has an uploaded logo, empty otherwise. Stable URL
	// is fine to bake on the client (the byte-serving handler is
	// public-read and keyed by id, not name).
	OrgLogoURL string `json:"org_logo_url,omitempty"`
	// Trusted = the org is allowed to buy player slots on invoice
	// instead of paying upfront via Stripe. Drives the "Add to
	// invoice" affordance in the buy-player-slots modal.
	Trusted bool `json:"trusted,omitempty"`
}

type whoamiResponse struct {
	ID            int64           `json:"id"`
	Username      string          `json:"username"`
	Role          string          `json:"role"`
	OrgID         *int64          `json:"org_id,omitempty"`
	Email         string          `json:"email,omitempty"`
	EmailVerified bool            `json:"email_verified"`
	Pricing       Pricing         `json:"pricing"`
	// Memberships is the user's full org list with the role they
	// hold in each. Drives the login picker (count > 1) and the
	// hamburger org switcher. Empty for admins.
	Memberships []membershipDTO `json:"memberships"`
	// ScheduledDeletionAt is RFC3339 when the user has requested
	// account deletion; absent otherwise. The UI shows a banner with
	// the date and a "Cancel deletion" affordance whenever this is set.
	ScheduledDeletionAt string `json:"scheduled_deletion_at,omitempty"`
	// PasswordIsDefault flags accounts that still hold the seeded
	// admin/admin or root/rootpw credentials. The UI renders a
	// red "change your password now" banner — leaving the seeded
	// password on a publicly-reachable box is a takeover, and
	// self-host operators who skim the docs need a louder nudge
	// than a footnote in SELFHOSTING.md.
	PasswordIsDefault bool `json:"password_is_default,omitempty"`
}

func (h *Handlers) userResponse(ctx context.Context, u User) whoamiResponse {
	resp := whoamiResponse{
		ID: u.ID, Username: u.Username, Role: u.Role, OrgID: u.OrgID,
		Email: u.Email, EmailVerified: u.EmailVerified,
		Pricing:           h.Pricing,
		Memberships:       []membershipDTO{},
		PasswordIsDefault: u.PasswordIsDefault,
	}
	if u.ScheduledDeletionAt != nil {
		resp.ScheduledDeletionAt = u.ScheduledDeletionAt.UTC().Format(time.RFC3339)
	}
	// Admins (Role == RoleAdmin) have no memberships — skip the
	// query rather than running a guaranteed-empty SELECT every
	// whoami fetch.
	if u.Role != RoleAdmin {
		mems, err := ListMemberships(ctx, h.DB, u.ID)
		if err == nil {
			resp.Memberships = make([]membershipDTO, 0, len(mems))
			for _, m := range mems {
				dto := membershipDTO{
					OrgID: m.OrgID, OrgName: m.OrgName, Role: m.Role,
					Trusted: m.Trusted,
				}
				if m.HasLogo {
					dto.OrgLogoURL = "/org-logo/" + strconv.FormatInt(m.OrgID, 10)
				}
				resp.Memberships = append(resp.Memberships, dto)
			}
		}
	}
	return resp
}

// Login validates credentials and sets the session cookie. Players whose
// arranger hasn't yet set a password receive a 409 with code
// "password_not_set" so the client can switch to the set-password flow.
//
// Failed attempts feed h.Limiter so credential stuffing slows down to a
// crawl after a handful of misses (per (ip, user) and per ip). Locked
// callers get 429 with Retry-After before any DB lookup happens, so the
// lockout itself is also a cheap response. Every outcome is appended to
// auth_events for the admin Audit tab.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	ip := ClientIP(r)
	ua := UserAgent(r)
	if ok, retry := h.Limiter.Allow(ip, req.Username); !ok {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, Username: req.Username, IP: ip, UserAgent: ua,
			Reason: ReasonRateLimited,
		})
		writeRateLimited(w, retry)
		return
	}
	// SSO-only enforcement: when the username/email is in the
	// configured SSO domain, reject the password attempt and steer
	// the client to the SSO entry point. Avoids a workspace-managed
	// user falling back to a stale local password after IT enabled
	// SSO. Non-domain users (e.g. external collaborators invited as
	// arrangers) keep the password flow.
	if h.OIDC.MatchesHostedDomain(req.Username) {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, Username: req.Username, IP: ip, UserAgent: ua,
			Reason: "sso_required",
		})
		httpx.JSON(w, http.StatusConflict, map[string]string{
			"error":     "single sign-on required for this account",
			"code":      "sso_required",
			"login_url": "/auth/oidc/login",
		})
		return
	}
	u, err := VerifyPassword(r.Context(), h.DB, req.Username, req.Password)
	if errors.Is(err, ErrPasswordNotSet) {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, Username: req.Username, IP: ip, UserAgent: ua,
			Reason: ReasonPasswordNotSet,
		})
		httpx.JSON(w, http.StatusConflict, map[string]string{
			"error": "password not set — choose one now",
			"code":  "password_not_set",
		})
		return
	}
	if errors.Is(err, ErrInvalidCredentials) {
		locked, retry := h.Limiter.RecordFailure(ip, req.Username)
		logx.Warn("auth.login.fail", "ip", ip, "username", req.Username, "reason", "invalid_credentials")
		metrics.AuthLogins.WithLabelValues("fail").Inc()
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, Username: req.Username, IP: ip, UserAgent: ua,
			Reason: ReasonInvalidCreds,
		})
		if locked {
			writeRateLimited(w, retry)
			return
		}
		httpx.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "login failed")
		return
	}
	// Password OK. Clear the failed-attempts counter for the pair so
	// a user who got their TOTP wrong a few times before getting it
	// right doesn't stay locked on the password leg.
	h.Limiter.RecordSuccess(ip, req.Username)
	// TOTP fork — short-circuit before minting a session.
	if u.TOTPEnabled {
		tok, err := IssueTOTPChallenge(r.Context(), h.DB, u.ID, "login", ip, ua)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "challenge issue failed")
			return
		}
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogin, OK: false, UserID: &u.ID, Username: u.Username, IP: ip, UserAgent: ua,
			Reason: ReasonTOTPRequired,
		})
		// 200 (not 401) so the JS layer doesn't double up with its
		// global 401-handler that bounces to the login page —
		// "totp_required" is a continuation, not a rejection.
		httpx.JSON(w, http.StatusOK, map[string]string{
			"code":      "totp_required",
			"challenge": tok,
		})
		return
	}
	var active sql.NullInt64
	if u.OrgID != nil {
		active = sql.NullInt64{Int64: *u.OrgID, Valid: true}
	}
	token, err := CompleteLogin(r.Context(), h.DB, u, []Factor{FactorPassword}, ua, ip, active)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "session create failed")
		return
	}
	metrics.AuthLogins.WithLabelValues("success").Inc()
	Record(r.Context(), h.DB, AuditEvent{
		Event: EventLogin, OK: true, UserID: &u.ID, Username: u.Username, IP: ip, UserAgent: ua,
		Reason: ReasonSuccess,
	})
	SetCookie(w, token, CookieSecure(r, h.SecureCookie))
	httpx.JSON(w, http.StatusOK, h.userResponse(r.Context(), u))
}

// writeRateLimited writes a 429 with a generic body and an integer
// Retry-After seconds header. Body intentionally doesn't leak which
// bucket tripped — clients only need the wait time.
func writeRateLimited(w http.ResponseWriter, retry time.Duration) {
	secs := int(retry / time.Second)
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	httpx.Error(w, http.StatusTooManyRequests, "too many attempts, try again later")
}

// SetPassword handles the first-time password set for accounts created
// without one (typically players invited by an arranger, or a previously
// reset account). Succeeds exactly once per account.
//
// Throttled by the same limiter as Login so an attacker can't mine
// usernames-with-no-password by retrying ErrInvalidCredentials.
func (h *Handlers) SetPassword(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := ValidatePassword(req.Password); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	ip := ClientIP(r)
	ua := UserAgent(r)
	if ok, retry := h.Limiter.Allow(ip, req.Username); !ok {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventSetPassword, OK: false, Username: req.Username, IP: ip, UserAgent: ua,
			Reason: ReasonRateLimited,
		})
		writeRateLimited(w, retry)
		return
	}
	u, token, err := SetInitialPassword(r.Context(), h.DB, req.Username, req.Password)
	if errors.Is(err, ErrInvalidCredentials) {
		locked, retry := h.Limiter.RecordFailure(ip, req.Username)
		logx.Warn("auth.setpassword.fail", "ip", ip, "username", req.Username, "reason", "invalid_credentials")
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventSetPassword, OK: false, Username: req.Username, IP: ip, UserAgent: ua,
			Reason: ReasonInvalidCreds,
		})
		if locked {
			writeRateLimited(w, retry)
			return
		}
		// Username not found — be generic so we don't leak existence.
		httpx.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if errors.Is(err, ErrPasswordAlreadySet) {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventSetPassword, OK: false, Username: req.Username, IP: ip, UserAgent: ua,
			Reason: ReasonPasswordAlreadySet,
		})
		httpx.Error(w, http.StatusConflict, "password already set — log in with your existing password")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "set-password failed")
		return
	}
	h.Limiter.RecordSuccess(ip, req.Username)
	Record(r.Context(), h.DB, AuditEvent{
		Event: EventSetPassword, OK: true, UserID: &u.ID, Username: u.Username, IP: ip, UserAgent: ua,
		Reason: ReasonSuccess,
	})
	StampSessionMeta(r.Context(), h.DB, token, ua, ip)
	SetCookie(w, token, CookieSecure(r, h.SecureCookie))
	httpx.JSON(w, http.StatusOK, h.userResponse(r.Context(), u))
}

// Logout clears the cookie and removes the server-side session row.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		_ = Logout(r.Context(), h.DB, c.Value)
	}
	// Audit who's leaving — useful for "did the admin actually log out
	// before walking away from the laptop?" forensics.
	if u, ok := FromContext(r.Context()); ok {
		Record(r.Context(), h.DB, AuditEvent{
			Event: EventLogout, OK: true, UserID: &u.ID, Username: u.Username,
			IP: ClientIP(r), UserAgent: UserAgent(r), Reason: ReasonSuccess,
		})
	}
	ClearCookie(w, CookieSecure(r, h.SecureCookie))
	httpx.JSON(w, http.StatusNoContent, nil)
}

// Whoami returns the currently authenticated user, or 401 if none.
func (h *Handlers) Whoami(w http.ResponseWriter, r *http.Request) {
	u, ok := FromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "not logged in")
		return
	}
	httpx.JSON(w, http.StatusOK, h.userResponse(r.Context(), u))
}

type switchOrgRequest struct {
	OrgID int64 `json:"org_id"`
}

// SwitchOrg pins the calling session's active organisation. The body
// names an org the user has a membership in; otherwise 403. Used by
// both the login-time picker modal (when a user belongs to several
// orgs) and the hamburger switcher mid-session. The cookie itself is
// unchanged — only sessions.active_org_id moves, so other tabs of
// the same browser also flip on their next page load.
func (h *Handlers) SwitchOrg(w http.ResponseWriter, r *http.Request) {
	u, ok := FromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "login required")
		return
	}
	var req switchOrgRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.OrgID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "org_id required")
		return
	}
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		httpx.Error(w, http.StatusUnauthorized, "no session")
		return
	}
	if err := SwitchActiveOrg(r.Context(), h.DB, u.ID, req.OrgID, c.Value); err != nil {
		if errors.Is(err, ErrNotAMember) {
			httpx.Error(w, http.StatusForbidden, "not a member of that organization")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "switch failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
