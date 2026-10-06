package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/payments"
	"github.com/klahr/zonventure/internal/ws"
)

// Paying for hosted events: a pass of h.Hosted's price per started
// block of TeamsPerBlock teams a game may hold. "Host it" buys an
// arranger a hosted event of their own adventure; "Add teams" buys an
// event more blocks. Both are charged on the platform's own account
// (no connected account: the platform is the seller), as ticket
// purchases of kind 'hosted' and 'hosted_blocks' that the checkout
// webhook fulfils (fulfilHostedCheckout). Where hosting is free (a
// self-host without a pass price) both happen at once. A party an org
// sells is the third way in: see BookParty, whose platform fee is the
// pass price. See migration 0110.

// maxBlocks caps the blocks one purchase buys: 150 teams in one game.
const maxBlocks = 10

// hostedPriceDTO is what the pages show of the pass price.
type hostedPriceDTO struct {
	BlockCents int64  `json:"block_cents"`
	Currency   string `json:"currency"`
	TeamsPer   int    `json:"teams_per_block"`
}

func (h *Handlers) hostedPrice() hostedPriceDTO {
	return hostedPriceDTO{BlockCents: h.Hosted.BlockCents, Currency: h.Hosted.Currency, TeamsPer: TeamsPerBlock}
}

// GetHostedPrice is the pass price, for the console's Host it dialog.
//
// GET /api/admin/hosted-price
func (h *Handlers) GetHostedPrice(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, h.hostedPrice())
}

type hostAdventureRequest struct {
	Name   string `json:"name"`
	Blocks int    `json:"blocks"`
}

// hostedCheckout is what a purchase answers: the checkout to pay at, or,
// where hosting is free, the event as it is now.
type hostedCheckout struct {
	CheckoutURL string `json:"checkout_url,omitempty"`
	EventID     int64  `json:"event_id,omitempty"`
}

// HostAdventure buys the caller's org a hosted event of one of its
// adventures — its own or one shared with it — to run privately: the
// same host page and player link as a party, opened in the console or
// by the host link mailed to the caller.
//
// POST /api/admin/adventures/{slug}/host
func (h *Handlers) HostAdventure(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	var req hostAdventureRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Blocks < 1 || req.Blocks > maxBlocks {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("blocks must be 1–%d", maxBlocks))
		return
	}
	name := strings.TrimSpace(req.Name)
	if utf8.RuneCountInString(name) > maxPartyNameLen {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("name is too long (%d characters max)", maxPartyNameLen))
		return
	}
	if me.Email == "" || !me.EmailVerified {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "add and verify your email address first", "code": "email_required"})
		return
	}
	ctx := r.Context()
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	var (
		advID int64
		title string
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT a.id, COALESCE(NULLIF(a.title, ''), a.slug) FROM adventures a
		WHERE a.slug = $1 AND (a.org_id = $2
		   OR EXISTS (SELECT 1 FROM adventure_shares s WHERE s.adventure_slug = a.slug AND s.org_id = $2))
	`, slug, *me.OrgID).Scan(&advID, &title); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "no such adventure for your organisation")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "adventure lookup failed", err)
		return
	}
	// A quiz trail is hosted as its cards are now.
	if _, err := publishTrail(ctx, h.DB, advID, me.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "publish failed", err)
		return
	}
	g, err := h.partyGraph(ctx, advID)
	if errors.Is(err, errNoPartyGraph) {
		httpx.Error(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "graph lookup failed", err)
		return
	}
	if p := g.StationsProblems(); len(p) > 0 && !parkOK(g) {
		httpx.JSON(w, http.StatusConflict, map[string]any{
			"error":    "this adventure cannot be hosted: " + p[0],
			"code":     "not_playable_as_party",
			"problems": p,
		})
		return
	}
	if name == "" {
		name = title
	}

	if h.Hosted.Free() {
		eventID, err := h.hostFree(ctx, *me.OrgID, slug, name, req.Blocks, me.Email)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "host failed", err)
			return
		}
		httpx.JSON(w, http.StatusCreated, hostedCheckout{EventID: eventID})
		return
	}
	if !h.paymentsEnabled() {
		httpx.Error(w, http.StatusConflict, "payments are not set up on this server")
		return
	}
	amount := h.Hosted.Price(req.Blocks)
	var purchaseID int64
	if err := h.DB.QueryRowContext(ctx, `
		INSERT INTO ticket_purchases (kind, buyer_org_id, adventure_slug, party_name, buyer_email,
		                              team_size, amount_cents, currency, blocks)
		VALUES ('hosted', $1, $2, $3, $4, 0, $5, $6, $7) RETURNING id
	`, *me.OrgID, slug, name, strings.ToLower(me.Email), amount, h.Hosted.Currency, req.Blocks).Scan(&purchaseID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "host failed", err)
		return
	}
	product := fmt.Sprintf("%s — hosted, %d teams per game", title, req.Blocks*TeamsPerBlock)
	h.startHostedCheckout(w, r, purchaseID, product, amount, me.Email, "/app/arrange/")
}

// hostFree makes a hosted event where hosting costs nothing, and mails
// its host link to the caller.
func (h *Handlers) hostFree(ctx context.Context, orgID int64, slug, name string, blocks int, email string) (int64, error) {
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	eventID, hostToken, err := createHostedEvent(ctx, tx, orgID, slug, name, 0, blocks)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if err := h.sendPartyHostLink(ctx, eventID, email, hostToken); err != nil {
		logx.Warn("admin.hosted.mail", "event_id", eventID, "err", err.Error())
	}
	return eventID, nil
}

// startHostedCheckout opens the checkout for a hosted purchase, charged
// on the platform's own account, and answers with its URL. back is
// where the buyer lands afterwards.
func (h *Handlers) startHostedCheckout(w http.ResponseWriter, r *http.Request, purchaseID int64, product string, amount int64, email, back string) {
	url, sessID, err := h.createTicketCheckoutSession(r.Context(), purchaseID, product, h.Hosted.Currency, amount,
		email, "", 0, back)
	if err != nil {
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE ticket_purchases SET state = 'failed' WHERE id = $1 AND state = 'pending'`, purchaseID)
		httpx.ServerErr(w, http.StatusBadGateway, "checkout create failed", err)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE ticket_purchases SET stripe_session_id = $1 WHERE id = $2`, sessID, purchaseID); err != nil {
		logx.Warn("admin.hosted.session_id_persist", "purchase_id", purchaseID, "err", err.Error())
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, hostedCheckout{CheckoutURL: url})
}

// AddHostedBlocks buys a hosted event more teams per game: Blocks more
// blocks. From the host page the buyer is the event's own buyer; from
// the console, the arranger.
//
// POST /api/party/{token}/blocks
// POST /api/admin/events/{eventID}/hosted/blocks
func (h *Handlers) AddHostedBlocks(w http.ResponseWriter, r *http.Request) {
	p, ok := h.hostedParty(w, r)
	if !ok {
		return
	}
	var req struct {
		Blocks int `json:"blocks"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Blocks < 1 || p.Blocks+req.Blocks > maxBlocks {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("a game holds at most %d teams", maxBlocks*TeamsPerBlock))
		return
	}
	if !h.partyOpen(w, p) {
		return
	}
	ctx := r.Context()
	if h.Hosted.Free() {
		if _, err := h.DB.ExecContext(ctx, `UPDATE events SET hosted_blocks = hosted_blocks + $2 WHERE id = $1`, p.EventID, req.Blocks); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "add teams failed", err)
			return
		}
		h.Hub.Publish(ws.EventTopic(p.EventID), "changed", nil)
		httpx.JSON(w, http.StatusOK, hostedCheckout{EventID: p.EventID})
		return
	}
	if !h.paymentsEnabled() {
		httpx.Error(w, http.StatusConflict, "payments are not set up on this server")
		return
	}
	// The host link's secret never goes to the payment provider: the
	// host page pays in a tab of its own and lands on a page that says
	// to go back.
	email, back := "", "/app/party/paid"
	if eventID, ok := eventIDFrom(r); ok {
		me, _ := auth.FromContext(ctx)
		email, back = me.Email, fmt.Sprintf("/app/arrange/#/events/%d", eventID)
	} else if err := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT buyer_email FROM ticket_purchases WHERE event_id = $1 AND kind IN ('party', 'hosted') ORDER BY id LIMIT 1), '')
	`, p.EventID).Scan(&email); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "add teams failed", err)
		return
	}
	amount := h.Hosted.Price(req.Blocks)
	var purchaseID int64
	if err := h.DB.QueryRowContext(ctx, `
		INSERT INTO ticket_purchases (kind, event_id, buyer_email, team_size, amount_cents, currency, blocks)
		VALUES ('hosted_blocks', $1, $2, 0, $3, $4, $5) RETURNING id
	`, p.EventID, strings.ToLower(email), amount, h.Hosted.Currency, req.Blocks).Scan(&purchaseID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "add teams failed", err)
		return
	}
	product := fmt.Sprintf("%d more teams per game", req.Blocks*TeamsPerBlock)
	h.startHostedCheckout(w, r, purchaseID, product, amount, email, back)
}

// fulfilHostedCheckout fulfils a paid 'hosted' or 'hosted_blocks'
// purchase inside the webhook's transaction tx, which it commits.
func (h *Handlers) fulfilHostedCheckout(ctx context.Context, tx *sql.Tx, event payments.Event, purchaseID int64, kind string) error {
	var (
		orgID     sql.NullInt64
		slug      string
		name      string
		email     string
		blocks    int
		eventID   int64
		hostToken string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT buyer_org_id, adventure_slug, party_name, buyer_email, blocks, COALESCE(event_id, 0)
		FROM ticket_purchases WHERE id = $1
	`, purchaseID).Scan(&orgID, &slug, &name, &email, &blocks, &eventID); err != nil {
		return err
	}
	switch kind {
	case "hosted":
		if !orgID.Valid {
			// The org was deleted while the payment was in flight; the row
			// is kept for bookkeeping and the platform refunds it.
			logx.Warn("payments.hosted_checkout.org_gone", "ticket_purchase_id", purchaseID)
			return nil
		}
		var err error
		if eventID, hostToken, err = createHostedEvent(ctx, tx, orgID.Int64, slug, name, 0, blocks); err != nil {
			return fmt.Errorf("hosted create: %w", err)
		}
	case "hosted_blocks":
		if eventID == 0 {
			logx.Warn("payments.hosted_checkout.event_gone", "ticket_purchase_id", purchaseID)
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE events SET hosted_blocks = hosted_blocks + $2 WHERE id = $1`, eventID, blocks); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_purchases
		   SET state = 'paid', paid_at = now_text(), event_id = $1, stripe_session_id = COALESCE(stripe_session_id, $2)
		 WHERE id = $3
	`, eventID, event.Checkout.SessionID, purchaseID); err != nil {
		return fmt.Errorf("purchase update: %w", err)
	}
	if err := updateTicketStripeMetadata(ctx, tx, purchaseID, *event.Checkout); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	if hostToken != "" {
		if err := h.sendPartyHostLink(ctx, eventID, email, hostToken); err != nil {
			logx.Warn("admin.hosted.mail", "purchase_id", purchaseID, "event_id", eventID, "err", err.Error())
		}
	}
	logx.Info("admin.hosted.paid", "purchase_id", purchaseID, "event_id", eventID, "kind", kind, "blocks", blocks)
	return nil
}
