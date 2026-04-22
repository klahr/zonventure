package auth

import (
	"database/sql"
	"errors"
	"net/http"

	"git.sr.ht/~klahr/zv/internal/httpx"
)

// Handlers wraps the dependencies of the auth HTTP endpoints.
type Handlers struct {
	DB           *sql.DB
	SecureCookie bool // set to true when behind TLS
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type whoamiResponse struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	OrgID    *int64 `json:"org_id,omitempty"`
}

func userResponse(u User) whoamiResponse {
	return whoamiResponse{ID: u.ID, Username: u.Username, Role: u.Role, OrgID: u.OrgID}
}

// Login validates credentials and sets the session cookie. Players whose
// arranger hasn't yet set a password receive a 409 with code
// "password_not_set" so the client can switch to the set-password flow.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	u, token, err := Login(r.Context(), h.DB, req.Username, req.Password)
	if errors.Is(err, ErrPasswordNotSet) {
		httpx.JSON(w, http.StatusConflict, map[string]string{
			"error": "password not set — choose one now",
			"code":  "password_not_set",
		})
		return
	}
	if errors.Is(err, ErrInvalidCredentials) {
		httpx.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "login failed")
		return
	}
	SetCookie(w, token, h.SecureCookie)
	httpx.JSON(w, http.StatusOK, userResponse(u))
}

// SetPassword handles the first-time password set for accounts created
// without one (typically players invited by an arranger, or a previously
// reset account). Succeeds exactly once per account.
func (h *Handlers) SetPassword(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if len(req.Password) < 4 {
		httpx.Error(w, http.StatusBadRequest, "password too short")
		return
	}
	u, token, err := SetInitialPassword(r.Context(), h.DB, req.Username, req.Password)
	if errors.Is(err, ErrInvalidCredentials) {
		// Username not found — be generic so we don't leak existence.
		httpx.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if errors.Is(err, ErrPasswordAlreadySet) {
		httpx.Error(w, http.StatusConflict, "password already set — log in with your existing password")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "set-password failed")
		return
	}
	SetCookie(w, token, h.SecureCookie)
	httpx.JSON(w, http.StatusOK, userResponse(u))
}

// Logout clears the cookie and removes the server-side session row.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		_ = Logout(r.Context(), h.DB, c.Value)
	}
	ClearCookie(w, h.SecureCookie)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// Whoami returns the currently authenticated user, or 401 if none.
func (h *Handlers) Whoami(w http.ResponseWriter, r *http.Request) {
	u, ok := FromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "not logged in")
		return
	}
	httpx.JSON(w, http.StatusOK, userResponse(u))
}
