package auth

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/logx"
	"git.sr.ht/~klahr/zv/internal/mailer"
)

// Handlers wraps the dependencies of the auth HTTP endpoints.
type Handlers struct {
	DB           *sql.DB
	SecureCookie bool // set to true when behind TLS
	// Features ships in whoami so the client can hide UI surfaces
	// that aren't wired up on this server (e.g. the simulator).
	Features Features
	// Pricing carries the operator's catalog price per slot, for client
	// totals. Zero values render as "$0.00" — operator should always set
	// this; the server's actual charge is enforced separately.
	Pricing Pricing
	// Limiter throttles failed login attempts. Optional: nil disables.
	Limiter *LoginLimiter
	// Mailer delivers signup verification + future forgot-password
	// emails. Always set in production; tests can leave nil and the
	// handler skips the send.
	Mailer mailer.Mailer
	// PublicURL is the externally-reachable origin (no trailing slash)
	// used to build links in outbound mail. e.g. "https://zv.example".
	PublicURL string
}

// Features is the small bag of server-side flags surfaced via
// /whoami. Keeping it a named struct so adding another flag later
// doesn't re-shape the response envelope.
type Features struct {
	SimulatorEnabled bool `json:"simulator_enabled"`
}

// Pricing is the small slice of catalog data the frontend needs to
// render totals before submitting a purchase. Mirrors what the server
// will charge so the UI doesn't have to keep its own constant. Surfaced
// on /whoami so the bootstrap fetch already carries it.
type Pricing struct {
	SlotCents int    `json:"slot_cents"`
	Currency  string `json:"currency"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type whoamiResponse struct {
	ID            int64    `json:"id"`
	Username      string   `json:"username"`
	Role          string   `json:"role"`
	OrgID         *int64   `json:"org_id,omitempty"`
	Email         string   `json:"email,omitempty"`
	EmailVerified bool     `json:"email_verified"`
	Features      Features `json:"features"`
	Pricing       Pricing  `json:"pricing"`
}

func (h *Handlers) userResponse(u User) whoamiResponse {
	return whoamiResponse{
		ID: u.ID, Username: u.Username, Role: u.Role, OrgID: u.OrgID,
		Email: u.Email, EmailVerified: u.EmailVerified,
		Features: h.Features,
		Pricing:  h.Pricing,
	}
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
	u, token, err := Login(r.Context(), h.DB, req.Username, req.Password)
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
	h.Limiter.RecordSuccess(ip, req.Username)
	Record(r.Context(), h.DB, AuditEvent{
		Event: EventLogin, OK: true, UserID: &u.ID, Username: u.Username, IP: ip, UserAgent: ua,
		Reason: ReasonSuccess,
	})
	SetCookie(w, token, CookieSecure(r, h.SecureCookie))
	httpx.JSON(w, http.StatusOK, h.userResponse(u))
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
	SetCookie(w, token, CookieSecure(r, h.SecureCookie))
	httpx.JSON(w, http.StatusOK, h.userResponse(u))
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
	httpx.JSON(w, http.StatusOK, h.userResponse(u))
}
