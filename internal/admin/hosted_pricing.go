package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/payments"
	"github.com/klahr/zonventure/internal/settings"
	"github.com/klahr/zonventure/internal/ws"
)

// Paying for hosted events (PLATFORM_PRICING.md, pay per team). An
// organisation's own hosted event ("Host it") costs nothing to make:
// each team is charged to the organisation as it joins a game
// (internal/billing). A family's party adventure, made in the public
// creator, holds the teams it was bought for, its first PartyFreeTeams
// free and each one after at the team price, for PartyGames games;
// "Add teams" buys it more. Those are charged on the platform's own
// account (the platform is the seller), as ticket purchases of kind
// 'hosted' and 'hosted_teams' that the checkout webhook fulfils
// (fulfilHostedCheckout). A party an org sells is the third way in: see
// BookParty.

// hostedPriceDTO is what the pages show of a party's price.
type hostedPriceDTO struct {
	TeamCents int64  `json:"team_cents"`
	Currency  string `json:"currency"`
	// FreeTeams are a family's party's free teams a game, for Games games.
	FreeTeams int `json:"free_teams"`
	Games     int `json:"games"`
	// VATExcluded: the price is shown "excl. VAT", else "incl. VAT".
	VATExcluded bool `json:"vat_excluded"`
}

func (h *Handlers) hostedPrice() hostedPriceDTO {
	p := h.pricing()
	return hostedPriceDTO{TeamCents: p.TeamCents, Currency: strings.ToUpper(settings.Currency),
		FreeTeams: int(p.PartyFreeTeams), Games: int(p.PartyGames), VATExcluded: p.VATExcluded}
}

// partyPrice is what a family's party of teams teams costs: its free
// teams cost nothing.
func (h *Handlers) partyPrice(teams int) int64 {
	p := h.pricing()
	if paid := int64(teams) - p.PartyFreeTeams; paid > 0 {
		return paid * p.TeamCents
	}
	return 0
}

// GetHostedPrice is the party price, for the console.
//
// GET /api/admin/hosted-price
func (h *Handlers) GetHostedPrice(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, h.hostedPrice())
}

type hostAdventureRequest struct {
	Name string `json:"name"`
}

// hostedCheckout is what a purchase answers: the checkout to pay at, or,
// where hosting is free, the event as it is now.
type hostedCheckout struct {
	CheckoutURL string `json:"checkout_url,omitempty"`
	EventID     int64  `json:"event_id,omitempty"`
	// HostURL is the admin page the purchase opens, once paid: given to
	// the browser that pays, so it can go there without the mail.
	HostURL string `json:"host_url,omitempty"`
}

// HostAdventure makes the caller's org a hosted event of one of its
// adventures — its own or one shared with it — to run privately: the
// same host page and player link as a party, opened in the console or
// by the host link mailed to the caller. Nothing is paid now: each team
// that joins is charged to the organisation.
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
		advID       int64
		title, lang string
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT a.id, COALESCE(NULLIF(a.title, ''), a.slug), a.language FROM adventures a
		WHERE a.slug = $1 AND NOT a.trail_hidden AND (a.org_id = $2
		   OR EXISTS (SELECT 1 FROM adventure_shares s WHERE s.adventure_slug = a.slug AND s.org_id = $2))
	`, slug, *me.OrgID).Scan(&advID, &title, &lang); err != nil {
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
		name = title + " " + trailDate(emailtpl.Lang(lang), time.Now())
	}

	eventID, err := h.hostFree(ctx, *me.OrgID, slug, name, hostedLimits{Billing: "teams"}, me.Email)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "host failed", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, hostedCheckout{EventID: eventID})
}

// hostFree makes a hosted event with nothing to pay now, and mails its
// host link to the caller.
func (h *Handlers) hostFree(ctx context.Context, orgID int64, slug, name string, lim hostedLimits, email string) (int64, error) {
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	eventID, hostToken, err := createHostedEvent(ctx, tx, orgID, slug, name, 0, lim, "")
	if err != nil {
		return 0, err
	}
	if err := h.keepHostToken(ctx, tx, eventID, hostToken); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if err := h.sendPartyHostLink(ctx, eventID, email, hostToken, false); err != nil {
		logx.Warn("admin.hosted.mail", "event_id", eventID, "err", err.Error())
	}
	return eventID, nil
}

// startHostedCheckout opens the checkout for a hosted purchase, charged
// on the platform's own account, and answers with its URL. back is
// where the buyer lands afterwards; hostURL, when set, the admin page
// the purchase opens once paid.
func (h *Handlers) startHostedCheckout(w http.ResponseWriter, r *http.Request, purchaseID int64, product string, amount int64, email, back, hostURL string) {
	url, sessID, err := h.createTicketCheckoutSession(r.Context(), purchaseID, product, settings.Currency, amount,
		email, "", 0, back, nil)
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
	httpx.JSON(w, http.StatusOK, hostedCheckout{CheckoutURL: url, HostURL: hostURL})
}

// AddHostedTeams buys a party more teams per game: Teams more, at the
// team price. From the host page the buyer is the event's own buyer;
// from the console, the arranger. An organisation's own hosted event
// has no limit to raise: each team is charged as it joins.
//
// POST /api/party/{token}/teams
// POST /api/admin/events/{eventID}/hosted/teams
func (h *Handlers) AddHostedTeams(w http.ResponseWriter, r *http.Request) {
	p, ok := h.hostedParty(w, r)
	if !ok {
		return
	}
	var req struct {
		Teams int `json:"teams"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if p.TeamLimit == 0 {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "this game takes any number of teams: each is charged as it joins", "code": "no_limit"})
		return
	}
	if req.Teams < 1 || p.TeamLimit+req.Teams > maxPartyTeams {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("a game holds at most %d teams", maxPartyTeams))
		return
	}
	if !h.partyOpen(w, p) {
		return
	}
	ctx := r.Context()
	amount := int64(req.Teams) * h.pricing().TeamCents
	if amount <= 0 {
		if _, err := h.DB.ExecContext(ctx, `UPDATE events SET team_limit = team_limit + $2 WHERE id = $1`, p.EventID, req.Teams); err != nil {
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
		SELECT COALESCE((SELECT buyer_email FROM ticket_purchases WHERE event_id = $1 AND kind IN ('party', 'hosted') ORDER BY id LIMIT 1),
		                (SELECT email FROM trails WHERE event_id = $1), '')
	`, p.EventID).Scan(&email); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "add teams failed", err)
		return
	}
	var purchaseID int64
	if err := h.DB.QueryRowContext(ctx, `
		INSERT INTO ticket_purchases (kind, event_id, buyer_email, team_size, amount_cents, currency, teams)
		VALUES ('hosted_teams', $1, $2, 0, $3, $4, $5) RETURNING id
	`, p.EventID, strings.ToLower(email), amount, strings.ToUpper(settings.Currency), req.Teams).Scan(&purchaseID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "add teams failed", err)
		return
	}
	var name string
	if err := h.DB.QueryRowContext(ctx, `SELECT party_name FROM events WHERE id = $1`, p.EventID).Scan(&name); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "add teams failed", err)
		return
	}
	product := emailtpl.T(h.eventLook(ctx, p.EventID).Lang, "%[1]s — more teams, up to %[2]d teams per game", name, p.TeamLimit+req.Teams)
	h.startHostedCheckout(w, r, purchaseID, product, amount, email, back, "")
}

// fulfilHostedCheckout fulfils a paid 'hosted' (a family's party) or
// 'hosted_teams' purchase inside the webhook's transaction tx, which it
// commits.
func (h *Handlers) fulfilHostedCheckout(ctx context.Context, tx *sql.Tx, event payments.Event, purchaseID int64, kind string) error {
	var (
		orgID     sql.NullInt64
		slug      string
		name      string
		email     string
		teams     int
		eventID   int64
		hostToken string
		minted    sql.NullString
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT buyer_org_id, adventure_slug, party_name, buyer_email, teams, COALESCE(event_id, 0), host_token
		FROM ticket_purchases WHERE id = $1
	`, purchaseID).Scan(&orgID, &slug, &name, &email, &teams, &eventID, &minted); err != nil {
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
		lim := hostedLimits{Teams: teams, Games: int(h.pricing().PartyGames), Billing: "prepaid"}
		if eventID, hostToken, err = createHostedEvent(ctx, tx, orgID.Int64, slug, name, 0, lim, minted.String); err != nil {
			return fmt.Errorf("hosted create: %w", err)
		}
		if err := h.keepHostToken(ctx, tx, eventID, hostToken); err != nil {
			return err
		}
		// A public quiz trail's: it is paid for now.
		if _, err := attachTrailEvent(ctx, tx, slug, eventID, email, true); err != nil {
			return fmt.Errorf("trail attach: %w", err)
		}
	case "hosted_teams":
		if eventID == 0 {
			logx.Warn("payments.hosted_checkout.event_gone", "ticket_purchase_id", purchaseID)
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE events SET team_limit = team_limit + $2 WHERE id = $1`, eventID, teams); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_purchases
		   SET state = 'paid', paid_at = now_text(), event_id = $1, stripe_session_id = COALESCE(stripe_session_id, $2),
		       host_token = NULL
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
		if err := h.sendPartyHostLink(ctx, eventID, email, hostToken, false); err != nil {
			logx.Warn("admin.hosted.mail", "purchase_id", purchaseID, "event_id", eventID, "err", err.Error())
		}
	}
	logx.Info("admin.hosted.paid", "purchase_id", purchaseID, "event_id", eventID, "kind", kind, "teams", teams)
	return nil
}
