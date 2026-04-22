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
	Username string `json:"username"`
	Role     string `json:"role"`
}

// Login validates credentials and sets the session cookie.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	u, token, err := Login(r.Context(), h.DB, req.Username, req.Password)
	if errors.Is(err, ErrInvalidCredentials) {
		httpx.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "login failed")
		return
	}
	SetCookie(w, token, h.SecureCookie)
	httpx.JSON(w, http.StatusOK, whoamiResponse{Username: u.Username, Role: u.Role})
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
	httpx.JSON(w, http.StatusOK, whoamiResponse{Username: u.Username, Role: u.Role})
}
