package admin

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/settings"
	"github.com/klahr/zonventure/internal/ws"
)

// Pay-later voucher codes (migration 0168): an organisation with invoice
// billing prints a batch of codes for a self-guided event, a buyer types
// one at /play, and that first use starts a team and puts one code's
// price on the organisation's next invoice.

// MaxVoucherBatch is the most codes one batch makes.
const MaxVoucherBatch = 2000

type voucherBatchDTO struct {
	ID        int64     `json:"id"`
	Label     string    `json:"label"`
	TeamSize  int       `json:"team_size"`
	Codes     int       `json:"codes"`
	Used      int       `json:"used"`
	CreatedAt time.Time `json:"created_at"`
	Voided    bool      `json:"voided"`
}

// voucherOfferDTO is GET /api/admin/events/{eventID}/voucher-batches:
// whether the organisation may make codes, what a used code costs, and
// the event's batches.
type voucherOfferDTO struct {
	// Allowed is invoice billing: only those organisations pay later.
	Allowed         bool              `json:"allowed"`
	PriceCents      int64             `json:"price_cents"`
	Currency        string            `json:"currency"`
	VATExcluded     bool              `json:"vat_excluded"`
	DefaultTeamSize int               `json:"default_team_size"`
	Batches         []voucherBatchDTO `json:"batches"`
}

// voucherEvent is the event a batch request names, checked to be the
// caller's organisation's self-guided event.
func (h *Handlers) voucherEvent(w http.ResponseWriter, r *http.Request) (eventID, orgID int64, teamSize int, ok bool) {
	eventID, ok = eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return 0, 0, 0, false
	}
	if orgID, ok = h.authorizeEvent(w, r, eventID); !ok {
		return 0, 0, 0, false
	}
	var forSale bool
	if err := h.DB.QueryRowContext(r.Context(), `SELECT public_for_sale, COALESCE(ticket_team_size, 4) FROM events WHERE id = $1`, eventID).
		Scan(&forSale, &teamSize); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return 0, 0, 0, false
	}
	if !forSale {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "voucher codes are for self-guided events", "reason": "not_self_guided"})
		return 0, 0, 0, false
	}
	return eventID, orgID, teamSize, true
}

// ListVoucherBatches is an event's batches and the offer.
//
// GET /api/admin/events/{eventID}/voucher-batches
func (h *Handlers) ListVoucherBatches(w http.ResponseWriter, r *http.Request) {
	eventID, orgID, teamSize, ok := h.voucherEvent(w, r)
	if !ok {
		return
	}
	h.writeVoucherOffer(w, r, eventID, orgID, teamSize)
}

func (h *Handlers) writeVoucherOffer(w http.ResponseWriter, r *http.Request, eventID, orgID int64, teamSize int) {
	p := settings.CurrentPricing()
	out := voucherOfferDTO{PriceCents: p.VoucherCents, Currency: settings.Currency, VATExcluded: p.VATExcluded, DefaultTeamSize: teamSize, Batches: []voucherBatchDTO{}}
	if err := h.DB.QueryRowContext(r.Context(), `SELECT trusted <> 0 FROM organizations WHERE id = $1`, orgID).Scan(&out.Allowed); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT b.id, b.label, b.team_size, b.created_at, b.voided_at IS NOT NULL,
		       (SELECT COUNT(*) FROM voucher_codes c WHERE c.batch_id = b.id),
		       (SELECT COUNT(*) FROM voucher_codes c WHERE c.batch_id = b.id AND c.activated_at IS NOT NULL)
		FROM voucher_batches b WHERE b.event_id = $1 ORDER BY b.id DESC
	`, eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var b voucherBatchDTO
		if err := rows.Scan(&b.ID, &b.Label, &b.TeamSize, &b.CreatedAt, &b.Voided, &b.Codes, &b.Used); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		out.Batches = append(out.Batches, b)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// CreateVoucherBatch makes a batch of codes: {"count": 200, "team_size":
// 4, "label": "Bookstores, spring print run"}. Org admins of an
// organisation with invoice billing.
//
// POST /api/admin/events/{eventID}/voucher-batches
func (h *Handlers) CreateVoucherBatch(w http.ResponseWriter, r *http.Request) {
	eventID, orgID, defaultSize, ok := h.voucherEvent(w, r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleOrgAdmin && !me.IsAdmin {
		httpx.Error(w, http.StatusForbidden, "only an organisation's admins make voucher codes")
		return
	}
	var trusted bool
	if err := h.DB.QueryRowContext(r.Context(), `SELECT trusted <> 0 FROM organizations WHERE id = $1`, orgID).Scan(&trusted); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if !trusted {
		httpx.JSON(w, http.StatusForbidden, map[string]string{"error": "voucher codes you pay later need invoice billing", "reason": "not_invoice"})
		return
	}
	var req struct {
		Count    int    `json:"count"`
		TeamSize int    `json:"team_size"`
		Label    string `json:"label"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Count < 1 || req.Count > MaxVoucherBatch {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("make between 1 and %d codes at a time", MaxVoucherBatch), "reason": "invalid_count"})
		return
	}
	if req.TeamSize == 0 {
		req.TeamSize = defaultSize
	}
	if req.TeamSize < 1 || req.TeamSize > 20 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "a team is 1 to 20 players", "reason": "invalid_team_size"})
		return
	}
	req.Label = strings.TrimSpace(req.Label)
	if utf8.RuneCountInString(req.Label) > 100 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "the label is too long", "reason": "invalid_label"})
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "create failed", err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var batchID int64
	var by any
	if me.ID > 0 {
		by = me.ID
	}
	if err := tx.QueryRowContext(r.Context(), `
		INSERT INTO voucher_batches (org_id, event_id, label, team_size, created_by) VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, orgID, eventID, req.Label, req.TeamSize, by).Scan(&batchID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "create failed", err)
		return
	}
	for made := 0; made < req.Count; {
		code, err := uniqueVoucherCode(r.Context(), tx)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "create failed", err)
			return
		}
		res, err := tx.ExecContext(r.Context(), `INSERT INTO voucher_codes (code, batch_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, code, batchID)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "create failed", err)
			return
		}
		if n, _ := res.RowsAffected(); n == 1 {
			made++
		}
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "create failed", err)
		return
	}
	noteAction(r, strconv.Itoa(req.Count)+" codes")
	logx.Info("admin.voucher_batch.created", "batch_id", batchID, "event_id", eventID, "codes", req.Count, "by", me.Username)
	h.writeVoucherOffer(w, r, eventID, orgID, defaultSize)
}

// voucherBatch loads a batch the caller may see.
func (h *Handlers) voucherBatch(w http.ResponseWriter, r *http.Request) (batchID, eventID, orgID int64, ok bool) {
	batchID, err := strconv.ParseInt(chi.URLParam(r, "batchID"), 10, 64)
	if err != nil || batchID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad batch id")
		return 0, 0, 0, false
	}
	if err := h.DB.QueryRowContext(r.Context(), `SELECT event_id, org_id FROM voucher_batches WHERE id = $1`, batchID).Scan(&eventID, &orgID); err != nil {
		httpx.Error(w, http.StatusNotFound, "batch not found")
		return 0, 0, 0, false
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return 0, 0, 0, false
	}
	return batchID, eventID, orgID, true
}

// VoucherBatchCSV is a batch's codes, for printing: code, used, when,
// and the team it started.
//
// GET /api/admin/voucher-batches/{batchID}/codes.csv
func (h *Handlers) VoucherBatchCSV(w http.ResponseWriter, r *http.Request) {
	batchID, _, _, ok := h.voucherBatch(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT c.code, c.activated_at, COALESCE(t.name, '')
		FROM voucher_codes c LEFT JOIN teams t ON t.id = c.team_id
		WHERE c.batch_id = $1 ORDER BY c.code
	`, batchID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	defer rows.Close()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="voucher-codes-%d.csv"`, batchID))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"code", "used", "used_at", "team"})
	for rows.Next() {
		var (
			code, team string
			at         sql.NullTime
		)
		if err := rows.Scan(&code, &at, &team); err != nil {
			return
		}
		used, when := "no", ""
		if at.Valid {
			used, when = "yes", at.Time.UTC().Format(time.RFC3339)
		}
		_ = cw.Write([]string{code, used, when, team})
	}
	cw.Flush()
}

// VoidVoucherBatch stops a batch's unused codes from starting teams.
// Teams already started keep playing; what was used stays charged.
//
// POST /api/admin/voucher-batches/{batchID}/void
func (h *Handlers) VoidVoucherBatch(w http.ResponseWriter, r *http.Request) {
	batchID, eventID, orgID, ok := h.voucherBatch(w, r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleOrgAdmin && !me.IsAdmin {
		httpx.Error(w, http.StatusForbidden, "only an organisation's admins void voucher codes")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `UPDATE voucher_batches SET voided_at = COALESCE(voided_at, now()) WHERE id = $1`, batchID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "void failed", err)
		return
	}
	noteAction(r, strconv.FormatInt(batchID, 10))
	var teamSize int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT COALESCE(ticket_team_size, 4) FROM events WHERE id = $1`, eventID).Scan(&teamSize)
	h.writeVoucherOffer(w, r, eventID, orgID, teamSize)
}

// errVoucherClosed is a batch code that exists but can't start a team now.
var errVoucherClosed = errors.New("voucher closed")

// ActivateVoucherCode turns an unused batch code into a team, the code
// becoming the team's voucher code, and charges it once. It returns the
// team's page, or ok false when code is no unused batch code (a used one
// resolves as an ordinary team voucher); closed says it exists but its
// batch is voided or its event takes no teams now.
func (h *Handlers) ActivateVoucherCode(ctx context.Context, code string) (teamURL string, ok, closed bool) {
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", false, false
	}
	defer func() { _ = tx.Rollback() }()
	var (
		eventID  int64
		teamSize int
		voided   bool
		state    string
		party    bool
	)
	err = tx.QueryRowContext(ctx, `
		SELECT b.event_id, b.team_size, b.voided_at IS NOT NULL, e.state, e.party
		FROM voucher_codes c JOIN voucher_batches b ON b.id = c.batch_id JOIN events e ON e.id = b.event_id
		WHERE c.code = $1 AND c.activated_at IS NULL
		FOR UPDATE OF c
	`, code).Scan(&eventID, &teamSize, &voided, &state, &party)
	if err != nil {
		return "", false, false
	}
	if voided || party || !allowedForState(state, OpCreateTeam) {
		return "", false, true
	}
	teamID, teamTok, _, err := createTicketTeam(ctx, tx, eventID, "", teamSize)
	if err != nil {
		logx.Warn("admin.voucher.activate", "err", err.Error())
		return "", false, false
	}
	if _, err := tx.ExecContext(ctx, `UPDATE teams SET voucher_code = $2 WHERE id = $1`, teamID, code); err != nil {
		logx.Warn("admin.voucher.activate", "err", err.Error())
		return "", false, false
	}
	p := settings.CurrentPricing()
	if _, err := tx.ExecContext(ctx, `
		UPDATE voucher_codes SET team_id = $2, activated_at = now(), price_cents = $3, currency = $4 WHERE code = $1
	`, code, teamID, p.VoucherCents, settings.Currency); err != nil {
		logx.Warn("admin.voucher.activate", "err", err.Error())
		return "", false, false
	}
	if err := tx.Commit(); err != nil {
		return "", false, false
	}
	logx.Info("admin.voucher.activated", "event_id", eventID, "team_id", teamID)
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	return "/team/" + teamTok, true, false
}

// voucherUseDTO is what an organisation owes for used codes: not yet on
// an invoice, and the invoices so far.
type voucherUseDTO struct {
	OrgID       int64  `json:"org_id"`
	OrgName     string `json:"org_name,omitempty"`
	Codes       int    `json:"codes"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
	// Since is the oldest use not yet invoiced.
	Since *time.Time `json:"since,omitempty"`
}

const voucherUnbilledSQL = `
	SELECT b.org_id, o.name, COUNT(*), COALESCE(SUM(c.price_cents), 0), MIN(c.currency), MIN(c.activated_at)
	FROM voucher_codes c JOIN voucher_batches b ON b.id = c.batch_id JOIN organizations o ON o.id = b.org_id
	WHERE c.activated_at IS NOT NULL AND c.billed_at IS NULL AND c.price_cents > 0`

// MyVoucherUse is the caller's organisation's used codes not yet
// invoiced, for its Billing page.
//
// GET /api/admin/orgs/me/voucher-use
func (h *Handlers) MyVoucherUse(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	out := voucherUseDTO{OrgID: *me.OrgID, Currency: settings.Currency}
	var since sql.NullTime
	var cur sql.NullString
	err := h.DB.QueryRowContext(r.Context(), voucherUnbilledSQL+` AND b.org_id = $1 GROUP BY b.org_id, o.name`, *me.OrgID).
		Scan(&out.OrgID, &out.OrgName, &out.Codes, &out.AmountCents, &cur, &since)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if cur.Valid {
		out.Currency = cur.String
	}
	if since.Valid {
		out.Since = &since.Time
	}
	httpx.JSON(w, http.StatusOK, out)
}

// VoucherBilling is every organisation's used codes not yet invoiced,
// for the platform admin's monthly invoicing. Platform admins.
//
// GET /api/admin/voucher-billing
func (h *Handlers) VoucherBilling(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), voucherUnbilledSQL+` GROUP BY b.org_id, o.name ORDER BY o.name`)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	defer rows.Close()
	out := []voucherUseDTO{}
	for rows.Next() {
		var u voucherUseDTO
		var since sql.NullTime
		var cur sql.NullString
		if err := rows.Scan(&u.OrgID, &u.OrgName, &u.Codes, &u.AmountCents, &cur, &since); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		u.Currency = cur.String
		if since.Valid {
			u.Since = &since.Time
		}
		out = append(out, u)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"orgs": out})
}

// MarkVoucherBilled records the invoice that charged an organisation for
// every code it had used up to now: {"invoice_number": "2026-117"}.
// Platform admins.
//
// POST /api/admin/organizations/{orgID}/voucher-billing
func (h *Handlers) MarkVoucherBilled(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad organisation id")
		return
	}
	var req struct {
		InvoiceNumber string `json:"invoice_number"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.InvoiceNumber = strings.TrimSpace(req.InvoiceNumber)
	if req.InvoiceNumber == "" || len(req.InvoiceNumber) > 100 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "enter the invoice's number", "reason": "invalid_invoice"})
		return
	}
	res, err := h.DB.ExecContext(r.Context(), `
		UPDATE voucher_codes c SET billed_at = now(), invoice_number = $2
		FROM voucher_batches b
		WHERE b.id = c.batch_id AND b.org_id = $1 AND c.activated_at IS NOT NULL AND c.billed_at IS NULL AND c.price_cents > 0
	`, orgID, req.InvoiceNumber)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "update failed", err)
		return
	}
	n, _ := res.RowsAffected()
	noteAction(r, fmt.Sprintf("%s: %d codes", req.InvoiceNumber, n))
	httpx.JSON(w, http.StatusOK, map[string]int64{"billed": n})
}
