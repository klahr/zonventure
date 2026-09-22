package admin

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
)

// VerifyMemberEmail flips a target user's email_verified flag to true.
// Admin-only escape hatch for the case where a real arranger never
// receives the verification mail (mail provider rejection, typo in the
// address with a follow-up out-of-band correction, etc.). The action
// is audit-logged so the trail shows who unblocked whom.
//
// Idempotent: re-running on an already-verified user returns 204
// without writing a new audit row, so a UI that re-issues the request
// after a stale render doesn't pollute the log.
func (h *Handlers) VerifyMemberEmail(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	userID, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}

	// Look up the target so we have a username for the audit row and
	// can short-circuit when already verified.
	var (
		username string
		current  bool
	)
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT username, (email_verified <> 0) FROM users WHERE id = $1`, userID,
	).Scan(&username, &current)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if current {
		// Already verified — no-op, no audit noise.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE users SET email_verified = 1 WHERE id = $1`, userID,
	); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}

	auth.Record(r.Context(), h.DB, auth.AuditEvent{
		Event:     auth.EventEmailVerify,
		OK:        true,
		UserID:    &userID,
		Username:  username,
		IP:        auth.ClientIP(r),
		UserAgent: auth.UserAgent(r),
		Reason:    auth.ReasonAdminManual,
	})
	logx.Info("admin.email_verify.manual",
		"target_user_id", userID, "target_username", username,
		"by_user_id", me.ID, "by_username", me.Username)

	w.WriteHeader(http.StatusNoContent)
}
