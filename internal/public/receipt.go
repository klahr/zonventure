package public

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/results"
)

// receiptCode is a receipt as results hands it out: SHA-256, hex.
var receiptCode = regexp.MustCompile(`^[0-9a-f]{64}$`)

// CheckReceipt serves /api/public/results/{code}: a receipt a team
// kept, looked up in the results records. The code is the secret — only
// the team's recap shows it — so the answer names the team and the
// adventure it finished. A code no record holds is 404: mistyped, its
// event deleted, or the record rewritten since the team got it.
func (h *Handlers) CheckReceipt(w http.ResponseWriter, r *http.Request) {
	code := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "code")))
	if !receiptCode.MatchString(code) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	held, err := results.Check(r.Context(), h.DB, code)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "receipt check failed", err)
		return
	}
	var slug string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT adventure_slug FROM events WHERE id = $1`, held.EventID).Scan(&slug)
	title := ""
	if adv := h.Lib.Get(slug); adv != nil {
		title = adv.Meta.Title
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	httpx.JSON(w, http.StatusOK, map[string]any{
		"team_name":       held.TeamName,
		"adventure_title": title,
		"finished_at":     held.FinishedAt,
		"matches":         held.Matches,
	})
}
