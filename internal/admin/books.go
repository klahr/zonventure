package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/payments"
	"github.com/klahr/zonventure/internal/settings"
)

// ShopProduct is a book in the operator's shop: shop/products.json in
// ZV_WEB_OVERRIDE_DIR, beside the shop's pages. Slug is the page's
// (/shop/{slug}); the price is read here, never from the buyer.
type ShopProduct struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	PriceCents int64  `json:"price_cents"`
}

// shopProduct finds slug in shop/products.json.
func (h *Handlers) shopProduct(slug string) (ShopProduct, bool, error) {
	if h.ShopDir == "" {
		return ShopProduct{}, false, nil
	}
	b, err := os.ReadFile(filepath.Join(h.ShopDir, "products.json"))
	if errors.Is(err, os.ErrNotExist) {
		return ShopProduct{}, false, nil
	}
	if err != nil {
		return ShopProduct{}, false, err
	}
	var file struct {
		Products []ShopProduct `json:"products"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return ShopProduct{}, false, fmt.Errorf("shop/products.json: %w", err)
	}
	for _, p := range file.Products {
		if p.Slug == slug && p.Name != "" && p.PriceCents > 0 {
			return p, true, nil
		}
	}
	return ShopProduct{}, false, nil
}

type buyBookRequest struct {
	Product    string `json:"product"`
	BuyerEmail string `json:"buyer_email"`
	Lang       string `json:"lang"`
}

// BuyBook opens a Checkout for one book from the shop, sold by the home
// organisation (ZV_HOME_ORG) on its own Stripe account with nothing
// taken by the platform: a book isn't a team. Checkout asks where to
// post it; the paid order is in the organisation's Orders to post.
// Public: the buyer has no account.
//
// POST /api/shop/checkout {"product": "…", "buyer_email": "…", "lang": "sv"}
func (h *Handlers) BuyBook(w http.ResponseWriter, r *http.Request) {
	if !h.paymentsEnabled() || h.HomeOrg == "" {
		httpx.Error(w, http.StatusServiceUnavailable, "the shop is not open on this server")
		return
	}
	var req buyBookRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.BuyerEmail))
	if _, err := mail.ParseAddress(email); err != nil || email == "" {
		httpx.Error(w, http.StatusBadRequest, "enter a valid email address")
		return
	}
	product, ok, err := h.shopProduct(strings.TrimSpace(req.Product))
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "shop lookup failed", err)
		return
	}
	if !ok {
		httpx.Error(w, http.StatusNotFound, "no such book")
		return
	}
	ctx := r.Context()
	var (
		orgID int64
		ship  string
	)
	if err := h.DB.QueryRowContext(ctx,
		`SELECT id, array_to_string(ship_countries, ',') FROM organizations WHERE slug = $1`, h.HomeOrg).Scan(&orgID, &ship); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "seller lookup failed", err)
		return
	}
	account := ""
	if h.marketplace() {
		account, err = h.stripeConnectAccountForOrgSlug(ctx, h.HomeOrg)
		if errors.Is(err, ErrConnectNotReady) {
			httpx.Error(w, http.StatusServiceUnavailable, "the shop is not taking payments right now")
			return
		}
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "seller lookup failed", err)
			return
		}
	}
	currency := strings.ToLower(settings.Currency)
	var purchaseID int64
	if err := h.DB.QueryRowContext(ctx, `
		INSERT INTO ticket_purchases(kind, buyer_email, team_size, amount_cents, currency, seller_org_id, product, product_name, buyer_lang)
		VALUES ('book', $1, 1, $2, $3, $4, $5, $6, $7) RETURNING id
	`, email, product.PriceCents, currency, orgID, product.Slug, product.Name, emailtpl.Lang(req.Lang)).Scan(&purchaseID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "order failed", err)
		return
	}
	// Back where the buyer came from: the shop's own host serves the
	// shop at its root.
	base := strings.TrimRight(h.PublicURL, "/") + "/shop/"
	host := r.Host
	if hp, _, err := net.SplitHostPort(host); err == nil {
		host = hp
	}
	if h.ShopHost != "" && strings.EqualFold(host, h.ShopHost) {
		base = "https://" + h.ShopHost + "/"
	}
	sess, err := h.Provider.CreateCheckout(ctx, payments.CheckoutParams{
		AmountCents:        product.PriceCents,
		Currency:           currency,
		ProductName:        product.Name,
		SuccessURL:         base + "success",
		CancelURL:          base + "canceled",
		CustomerEmail:      email,
		Metadata:           map[string]string{"zv_ticket_purchase_id": fmt.Sprintf("%d", purchaseID)},
		ConnectedAccountID: account,
		IdempotencyKey:     fmt.Sprintf("checkout-book-%d", purchaseID),
		ShipCountries:      settings.OrgShipCountries(ship),
	})
	if err != nil {
		logx.Warn("admin.shop.checkout", "purchase_id", purchaseID, "err", err.Error())
		httpx.ServerErr(w, http.StatusBadGateway, "checkout failed", err)
		return
	}
	if _, err := h.DB.ExecContext(ctx,
		`UPDATE ticket_purchases SET stripe_session_id = $1 WHERE id = $2`, sess.SessionID, purchaseID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "order failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"checkout_url": sess.URL})
}

// fulfilBookCheckout marks a paid book order paid with where to post
// it, inside the webhook's transaction, and mails the buyer a
// confirmation and the seller the order.
func (h *Handlers) fulfilBookCheckout(ctx context.Context, tx *sql.Tx, event payments.Event, purchaseID int64) error {
	var sh payments.Shipping
	if event.Checkout.Shipping != nil {
		sh = *event.Checkout.Shipping
	}
	var (
		email, name, lang string
		orgID             sql.NullInt64
	)
	if err := tx.QueryRowContext(ctx, `
		UPDATE ticket_purchases
		   SET state = 'paid', paid_at = now_text(), stripe_session_id = COALESCE(stripe_session_id, $2),
		       ship_name = $3, street_address = $4, address_2 = $5, postal_code = $6, city = $7, country = $8
		 WHERE id = $1
		RETURNING buyer_email, product_name, buyer_lang, seller_org_id
	`, purchaseID, event.Checkout.SessionID, sh.Name, sh.Line1, sh.Line2, sh.PostalCode, sh.City, sh.Country).Scan(&email, &name, &lang, &orgID); err != nil {
		return fmt.Errorf("book order: %w", err)
	}
	if err := updateTicketStripeMetadata(ctx, tx, purchaseID, *event.Checkout); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	logx.Info("admin.shop.book.paid", "purchase_id", purchaseID, "product", name)

	l := h.orgLook(ctx, orgID.Int64)
	if lang == "" {
		lang = l.Lang
	}
	address := addressLines(sh.Name, sh.Line1, sh.Line2, sh.PostalCode, sh.City)
	subject, c := emailtpl.BookOrdered(lang, l.Brand.Name, name, address, l.Contact)
	if err := h.send(ctx, "book_ordered", l.message([]string{email}, subject, c)); err != nil {
		logx.Warn("admin.shop.book.mail", "kind", "book_ordered", "purchase_id", purchaseID, "err", err.Error())
	}
	if l.Contact != "" {
		link := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/") + "/arrange#/redemptions"
		subject, c := emailtpl.BookOrderNew(l.Lang, name, address, link)
		msg := l.message([]string{l.Contact}, subject, c)
		msg.ReplyTo = email
		if err := h.send(ctx, "book_order_new", msg); err != nil {
			logx.Warn("admin.shop.book.mail", "kind", "book_order_new", "purchase_id", purchaseID, "err", err.Error())
		}
	}
	return nil
}
