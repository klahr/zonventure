package player

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
)

// A voucher code is a short, typeable alias for a team's link: a player
// reads it off a gift card or an email and enters it at /play instead of
// clicking the magic link. Resolving one lands them on the team page
// (/team/{token}), exactly where the clickable link would — from there
// they claim a seat and start. The code grants no more than that link
// does, so the lookup is rate-limited (per IP, shared with the magic-link
// exchange) and nothing distinguishes "unknown code" from "bad code".

// teamURLForVoucher resolves a normalised voucher code to its team's
// share path, or ("", false) when no team carries it.
func teamURLForVoucher(r *http.Request, db *sql.DB, code string) (string, bool) {
	if code == "" {
		return "", false
	}
	var token string
	err := db.QueryRowContext(r.Context(),
		`SELECT token FROM teams WHERE voucher_code = $1`, code).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) || token == "" {
		return "", false
	}
	if err != nil {
		return "", false
	}
	return "/team/" + token, true
}

// resolveVoucher is teamURLForVoucher, falling back to an unused batch
// code (admin/voucher_batches.go), which its first use turns into a
// team. closed says the code exists but can't start a team now.
func (h *Handlers) resolveVoucher(r *http.Request, code string) (url string, ok, closed bool) {
	if url, ok := teamURLForVoucher(r, h.DB, code); ok {
		return url, true, false
	}
	if h.ActivateVoucher == nil || code == "" {
		return "", false, false
	}
	url, ok, closed = h.ActivateVoucher(r.Context(), code)
	if !ok && !closed {
		// A teammate's first use may just have made the team.
		if url, ok := teamURLForVoucher(r, h.DB, code); ok {
			return url, true, false
		}
	}
	return url, ok, closed
}

// VoucherRedeem is the JSON endpoint the /play code box posts to:
// POST /api/play/voucher {"code":"..."} → {"team_url":"/team/..."} on a
// hit, 404 otherwise. Public and rate-limited; the body carries only the
// code so there is nothing to leak on a miss.
func (h *Handlers) VoucherRedeem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	ip := auth.ClientIP(r)
	if ok, retry := h.rateLimiter().AllowMagicLink(ip); !ok {
		writeRateLimited(w, retry)
		return
	}
	code := auth.NormalizeVoucherCode(req.Code)
	url, ok, closed := h.resolveVoucher(r, code)
	if closed {
		httpx.JSON(w, http.StatusConflict, map[string]string{
			"error": "this code's adventure isn't open right now — ask whoever sold it",
			"code":  "voucher_closed",
		})
		return
	}
	if !ok {
		httpx.JSON(w, http.StatusNotFound, map[string]string{
			"error": "that code isn't valid — check it and try again",
			"code":  "unknown_voucher",
		})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"team_url": url})
}

// VoucherLink handles the emailed deep link /play?c=<code>: it resolves
// the code and redirects to the team page, so a code in a link works the
// same as one typed into the box. Returns true when it handled the
// request (a ?c= was present). A miss redirects back to /play with a flag
// the page reads to show the "unknown code" message.
func (h *Handlers) VoucherLink(w http.ResponseWriter, r *http.Request) bool {
	raw := r.URL.Query().Get("c")
	if raw == "" {
		return false
	}
	ip := auth.ClientIP(r)
	if ok, _ := h.rateLimiter().AllowMagicLink(ip); !ok {
		http.Redirect(w, r, "/play?voucher=busy", http.StatusFound)
		return true
	}
	code := auth.NormalizeVoucherCode(raw)
	url, ok, closed := h.resolveVoucher(r, code)
	if closed {
		http.Redirect(w, r, "/play?voucher=closed", http.StatusFound)
		return true
	}
	if !ok {
		http.Redirect(w, r, "/play?voucher=invalid", http.StatusFound)
		return true
	}
	http.Redirect(w, r, url, http.StatusFound)
	return true
}
