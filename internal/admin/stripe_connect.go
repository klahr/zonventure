package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/payments"
	"github.com/klahr/zonventure/internal/settings"
)

// Stripe Connect onboarding for the /store marketplace flow.
//
// Each org that wants to sell quest tickets onboards an Express
// connected account via Stripe-hosted KYC. Every ticket is charged
// on the platform account (which carries refunds and disputes) and
// destination-transferred to the org, minus a per-charge
// application fee configured via ZV_PLATFORM_FEE_PERCENT_BPS /
// _FIXED_CENTS / _MIN_CENTS. Payouts to the org run monthly. The org sees its balance and payouts in Stripe's
// Express dashboard, reached through the dashboard endpoint below.
// Orgs onboarded as Standard accounts before the switch keep
// selling; they use their own Stripe dashboard instead.
//
// Endpoint surface:
//
//	POST /api/admin/orgs/{orgID}/stripe-connect/dashboard
//	     Returns a single-use login link into the Express dashboard.
//
//	POST /api/admin/orgs/{orgID}/stripe-connect/start
//	     Idempotent. Creates the connected account on first call,
//	     returns a fresh hosted-onboarding URL on every call.
//
//	GET  /api/admin/orgs/{orgID}/stripe-connect/status
//	     Returns the cached status flags + the optional refresh-link
//	     URL when onboarding was started but never completed.
//
//	POST /api/admin/orgs/{orgID}/stripe-connect/refresh
//	     Force-syncs status from Stripe (live API call) into the DB.
//	     The account.updated webhook does this automatically; this
//	     endpoint is the manual "I just finished onboarding, refresh
//	     now" affordance for the Settings page.
//
// All three are gated to org_admins of the named org, plus global
// admins (cross-org troubleshooting). The ZV_PLATFORM_FEE_*
// configuration must be set for the start endpoint to make sense
// at all — without a platform fee there's no marketplace flow,
// so we refuse with a clear error rather than silently creating
// a connected account that goes unused.

// stripeConnectStatusDTO is the response shape for the status
// endpoint. Mirrors the columns added in 0049_stripe_connect.sql.
type stripeConnectStatusDTO struct {
	AccountID      string `json:"account_id,omitempty"`
	ChargesEnabled bool   `json:"charges_enabled"`
	PayoutsEnabled bool   `json:"payouts_enabled"`
	SyncedAt       string `json:"synced_at,omitempty"`
	// PlatformFeeEnabled is the marketplace (Handlers.marketplace):
	// orgs sell into Stripe accounts of their own. Off (a self-host),
	// the UI shows no Connect panel: there is nothing to set up.
	PlatformFeeEnabled bool `json:"platform_fee_enabled"`
}

type startStripeConnectResp struct {
	// URL is the single-use Stripe-hosted onboarding link. The
	// frontend redirects the browser straight to it. A fresh URL
	// is minted on each call so a stale one (from a previous
	// session) doesn't 404 the user when they click.
	URL string `json:"url"`
	// AccountID is the underlying acct_xxx id, surfaced for the
	// UI's "connected to acct_xxxx" banner. Same value the
	// status endpoint returns.
	AccountID string `json:"account_id"`
}

// StartStripeConnect creates the connected account if missing and
// returns a fresh hosted-onboarding URL.
//
// POST /api/admin/orgs/{orgID}/stripe-connect/start
func (h *Handlers) StartStripeConnect(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if !canManageOrgConnect(me, orgID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	if !h.marketplace() {
		httpx.Error(w, http.StatusServiceUnavailable,
			"selling into organisations' own Stripe accounts is not set up on this server")
		return
	}
	mp, ok := h.Provider.(payments.MarketplaceProvider)
	if !ok {
		httpx.Error(w, http.StatusServiceUnavailable,
			"payment provider does not support marketplace flows")
		return
	}

	// Look up the existing acct_xxx if any. Create-on-miss so the
	// endpoint is idempotent at the org level: re-clicking "Connect
	// Stripe" never produces a second account.
	var (
		existing      sql.NullString
		orgName, ship string
		attempt       int
	)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT stripe_connect_account_id, name, array_to_string(ship_countries, ','), stripe_connect_attempt FROM organizations WHERE id = $1`,
		orgID).Scan(&existing, &orgName, &ship, &attempt); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "org lookup failed")
		return
	}

	accountID := existing.String
	if accountID == "" {
		// Stripe needs the account's country before it can sell: the
		// first one the org posts letters to, else the platform's.
		country := settings.OrgShipCountries(ship)[0]
		newID, err := mp.CreateConnectedAccount(r.Context(), payments.ConnectedAccountParams{
			Email:       strings.TrimSpace(me.Email),
			DisplayName: orgName,
			Country:     country,
			// Stable per-org: even if the local UPDATE below fails
			// after a successful CreateConnectedAccount, a retry
			// must hit Stripe with the same key and get the original
			// account id back rather than provisioning a second
			// acct_xxx (with its own KYC requirements).
			// The attempt is in it: after a disconnect the next
			// account is a new one.
			IdempotencyKey: fmt.Sprintf("connect-org-%d-%s-%d", orgID, country, attempt),
		})
		if err != nil {
			logx.Warn("admin.stripe_connect.create", "org", orgID, "err", err.Error())
			httpx.ServerErr(w, http.StatusBadGateway, "stripe account create failed", err)
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE organizations SET stripe_connect_account_id = $1 WHERE id = $2`,
			newID, orgID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "org save failed")
			return
		}
		accountID = newID
	}

	publicURL := strings.TrimRight(h.PublicURL, "/")
	if publicURL == "" {
		// Fall back to the request host so a self-host without
		// PUBLIC_URL still works for the local-dev case. Stripe
		// requires absolute URLs; building one off r.Host + scheme
		// matches the auth-handlers fallback.
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		publicURL = scheme + "://" + r.Host
	}
	// Both URLs return the user to the Settings → Billing tab. The
	// refresh URL is hit when Stripe wants to re-prompt the user
	// (e.g. session expired mid-flow); the return URL is hit on
	// completion or explicit cancel. The frontend distinguishes
	// the two via the ?stripe_connect query param. Query is in the
	// real querystring (not the hash) so location.search picks it
	// up; the hash carries the in-app route.
	link, err := mp.CreateAccountLink(r.Context(), payments.AccountLinkParams{
		AccountID:  accountID,
		RefreshURL: publicURL + "/arrange?stripe_connect=refresh#settings/billing",
		ReturnURL:  publicURL + "/arrange?stripe_connect=return#settings/billing",
	})
	if err != nil {
		logx.Warn("admin.stripe_connect.link", "org", orgID, "err", err.Error())
		httpx.ServerErr(w, http.StatusBadGateway, "stripe link create failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, startStripeConnectResp{
		URL:       link,
		AccountID: accountID,
	})
}

// GetStripeConnectStatus returns the cached status snapshot. Cheap
// — no Stripe API call. The Settings page hits this on load to
// know whether to show "Connect Stripe", "Onboarding in progress"
// or "Connected".
//
// GET /api/admin/orgs/{orgID}/stripe-connect/status
func (h *Handlers) GetStripeConnectStatus(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if !canManageOrgConnect(me, orgID) && me.Role != auth.RoleSupport {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	out, err := h.loadStripeConnectStatus(r.Context(), orgID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "status lookup failed")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// RefreshStripeConnectStatus force-syncs from Stripe into the local
// DB. Called from the Settings page after the buyer returns from
// Stripe-hosted onboarding (where the account.updated webhook may
// not have caught up yet) and from a manual "Refresh status"
// button when the cached state looks stale.
//
// POST /api/admin/orgs/{orgID}/stripe-connect/refresh
func (h *Handlers) RefreshStripeConnectStatus(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if !canManageOrgConnect(me, orgID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	mp, ok := h.Provider.(payments.MarketplaceProvider)
	if !ok {
		httpx.Error(w, http.StatusServiceUnavailable,
			"payment provider does not support marketplace flows")
		return
	}
	var accountID sql.NullString
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT stripe_connect_account_id FROM organizations WHERE id = $1`,
		orgID).Scan(&accountID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "org lookup failed")
		return
	}
	if !accountID.Valid || accountID.String == "" {
		httpx.Error(w, http.StatusConflict, "no Stripe account to refresh — start onboarding first")
		return
	}
	if err := h.syncStripeConnectAccount(r.Context(), orgID, accountID.String, mp); err != nil {
		logx.Warn("admin.stripe_connect.refresh", "org", orgID, "err", err.Error())
		httpx.ServerErr(w, http.StatusBadGateway, "stripe status refresh failed", err)
		return
	}
	out, err := h.loadStripeConnectStatus(r.Context(), orgID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "status lookup failed")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

type stripeConnectDashboardResp struct {
	// URL is where the org signs in to its Stripe dashboard.
	URL string `json:"url"`
}

// OpenStripeConnectDashboard says where the org's Stripe dashboard is:
// the account is its own, with Stripe's full dashboard (balance,
// payouts, refunds, disputes), signed in to at Stripe.
//
// POST /api/admin/orgs/{orgID}/stripe-connect/dashboard
func (h *Handlers) OpenStripeConnectDashboard(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if !canManageOrgConnect(me, orgID) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	// The account's own dashboard: someone signed in to several Stripe
	// accounts would otherwise land on the one they used last.
	var account sql.NullString
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT stripe_connect_account_id FROM organizations WHERE id = $1`, orgID).Scan(&account); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "org lookup failed", err)
		return
	}
	url := "https://dashboard.stripe.com/"
	if account.String != "" {
		url += "b/" + account.String
	}
	httpx.JSON(w, http.StatusOK, stripeConnectDashboardResp{URL: url})
}

/* ---------------- helpers ---------------- */

// DisconnectStripeConnect forgets an organisation's Stripe account, so
// the next connect creates a new one: for an account onboarded with the
// wrong company. The account itself is closed in Stripe's dashboard.
// Platform admin only.
//
// POST /api/admin/organizations/{orgID}/stripe-connect/disconnect
func (h *Handlers) DisconnectStripeConnect(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleAdmin {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	var old sql.NullString
	if err := h.DB.QueryRowContext(r.Context(), `
		UPDATE organizations o
		   SET stripe_connect_account_id = NULL, stripe_connect_charges_enabled = FALSE,
		       stripe_connect_payouts_enabled = FALSE, stripe_connect_synced_at = NULL,
		       stripe_connect_attempt = o.stripe_connect_attempt + 1
		  FROM organizations prev
		 WHERE o.id = $1 AND prev.id = o.id
		RETURNING prev.stripe_connect_account_id`, orgID).Scan(&old); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "no such organisation")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "disconnect failed", err)
		return
	}
	logx.Info("admin.stripe_connect.disconnect", "org", orgID, "account", old.String, "by", me.Username)
	status, err := h.loadStripeConnectStatus(r.Context(), orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "status lookup failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, status)
}

// canManageOrgConnect gates the Connect endpoints. Org admins of
// the named org may manage their own org's Connect account; global
// admins may manage any org (cross-tenant troubleshooting). Org
// members and lobby users get a clean 403.
func canManageOrgConnect(u auth.User, orgID int64) bool {
	if u.Role == auth.RoleAdmin {
		return true
	}
	return u.Role == auth.RoleOrgAdmin && u.OrgID != nil && *u.OrgID == orgID
}

// loadStripeConnectStatus reads the cached status from the org row
// and wraps it in the response DTO. Includes the platform-fee
// enabled flag so the UI can suppress the panel on self-host.
func (h *Handlers) loadStripeConnectStatus(ctx context.Context, orgID int64) (stripeConnectStatusDTO, error) {
	var (
		out    stripeConnectStatusDTO
		acct   sql.NullString
		synced sql.NullString
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT stripe_connect_account_id, stripe_connect_charges_enabled,
		       stripe_connect_payouts_enabled, stripe_connect_synced_at
		FROM organizations WHERE id = $1
	`, orgID).Scan(&acct, &out.ChargesEnabled, &out.PayoutsEnabled, &synced)
	if err != nil {
		return stripeConnectStatusDTO{}, err
	}
	if acct.Valid {
		out.AccountID = acct.String
	}
	if synced.Valid {
		out.SyncedAt = synced.String
	}
	out.PlatformFeeEnabled = h.marketplace()
	return out, nil
}

// syncStripeConnectAccount fetches the live status from Stripe and
// writes it back into the org row. Shared by the manual refresh
// endpoint and (eventually) the account.updated webhook handler so
// both paths use exactly the same UPDATE.
func (h *Handlers) syncStripeConnectAccount(ctx context.Context, orgID int64, accountID string, mp payments.MarketplaceProvider) error {
	acct, err := mp.GetConnectedAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if _, err := h.DB.ExecContext(ctx, `
		UPDATE organizations
		SET stripe_connect_charges_enabled = $1,
		    stripe_connect_payouts_enabled = $2,
		    stripe_connect_synced_at = now_text()
		WHERE id = $3
	`, acct.ChargesEnabled, acct.PayoutsEnabled, orgID); err != nil {
		return err
	}
	return nil
}

// ErrConnectNotReady is returned by the helper that loads the
// connected-account id for a given event. Callers translate it
// into a "set up Stripe Connect first" 409 on /store/buy.
var ErrConnectNotReady = errors.New("connect not ready")

// handleConnectedAccountUpdated processes an account.updated event
// from Stripe. The event body's account id maps back to one of our
// orgs via the unique stripe_connect_account_id index added in
// 0049; we update the cached charges_enabled / payouts_enabled
// flags directly from the event payload (no extra Stripe API call
// needed — Stripe already populated them in the webhook body).
//
// An unrecognised account id (event for an account this server
// didn't create — happens if the same Stripe key is shared
// across deployments) is silently acked rather than erroring;
// looping the webhook on irrelevant traffic adds no value.
func (h *Handlers) handleConnectedAccountUpdated(ctx context.Context, event payments.Event) error {
	if event.ConnectedAccount == nil || event.ConnectedAccount.ID == "" {
		return nil
	}
	a := event.ConnectedAccount
	res, err := h.DB.ExecContext(ctx, `
		UPDATE organizations
		SET stripe_connect_charges_enabled = $1,
		    stripe_connect_payouts_enabled = $2,
		    stripe_connect_synced_at = now_text()
		WHERE stripe_connect_account_id = $3
	`, a.ChargesEnabled, a.PayoutsEnabled, a.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Account isn't ours; ack and move on. Logging at debug
		// because legitimate cases (cross-deployment shared key,
		// account onboarded against a different env) trigger
		// this and we don't want them in the warn feed.
		logx.Debug("admin.stripe_connect.webhook.unknown_account", "account_id", a.ID)
	}
	return nil
}

// stripeConnectAccountForEvent looks up the connected account that
// should receive funds for an event's ticket sales. Returns
// ErrConnectNotReady when the org hasn't completed Connect
// onboarding (no acct id, or charges_enabled=false). Callers use
// this in the /store/buy flow to refuse purchases on a
// not-yet-onboarded quest.
func (h *Handlers) stripeConnectAccountForEvent(ctx context.Context, eventID int64) (string, error) {
	var (
		acct           sql.NullString
		chargesEnabled bool
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT o.stripe_connect_account_id, o.stripe_connect_charges_enabled
		FROM events e
		JOIN organizations o ON o.id = e.org_id
		WHERE e.id = $1
	`, eventID).Scan(&acct, &chargesEnabled)
	if err != nil {
		return "", err
	}
	if !acct.Valid || acct.String == "" || !chargesEnabled {
		return "", ErrConnectNotReady
	}
	return acct.String, nil
}
