package payments

import "context"

// MarketplaceProvider is the optional Connect-style extension to
// Provider. Impls that support marketplace flows — where the
// platform charges a buyer and routes most of the money to a
// connected sub-account, keeping a per-charge application fee —
// satisfy this interface in addition to Provider. The split keeps
// non-marketplace providers (the simulated dev impl, future
// Mollie / Paddle if/when they land) unburdened by methods they
// don't implement.
//
// Callers detect support with a type assertion:
//
//	mp, ok := h.Provider.(payments.MarketplaceProvider)
//	if !ok || !platformFee.Enabled() {
//	    // self-host / non-marketplace path
//	}
//
// The Provider methods on this interface accept the same args /
// return shapes as the bare Provider; the marketplace bits are
// the three extra methods plus the marketplace fields on
// CheckoutParams / RefundParams (kept on the base types so a
// MarketplaceProvider impl can act on them without the caller
// type-switching just to populate the request).
type MarketplaceProvider interface {
	Provider

	// CreateConnectedAccount creates a fresh connected account
	// suitable for receiving destination-charged transfers.
	// Standard / Express / Custom is up to the impl; today the
	// Stripe impl creates Standard accounts (the cheapest tier
	// and the one that doesn't burden the platform with KYC).
	// Returns the provider-specific account id (Stripe: acct_xxx).
	CreateConnectedAccount(ctx context.Context, p ConnectedAccountParams) (string, error)

	// CreateAccountLink mints a hosted onboarding URL the buyer
	// is redirected to. Used both for first-time onboarding and
	// for the "I dropped out, let me resume" refresh-link flow.
	// The URL is single-use and expires quickly (Stripe's default
	// is a few minutes); generate a fresh one each time.
	CreateAccountLink(ctx context.Context, p AccountLinkParams) (string, error)

	// GetConnectedAccount fetches the live status of a connected
	// account so the caller can sync the charges_enabled /
	// payouts_enabled flags into the local DB cache. Called from
	// the account.updated webhook handler and from the manual
	// "refresh" affordance on the Connect settings page.
	GetConnectedAccount(ctx context.Context, accountID string) (ConnectedAccount, error)
}

// ConnectedAccountParams describes a fresh connected account.
// Country is ISO 3166-1 alpha-2 (e.g. "SE", "US"); empty falls
// through to the provider's default for the platform's country.
// DisplayName seeds the business profile so the onboarding form
// doesn't show a blank "Untitled" header.
type ConnectedAccountParams struct {
	Email       string
	Country     string
	DisplayName string

	// IdempotencyKey deduplicates retried CreateConnectedAccount
	// calls. A retried "Connect Stripe" handler must not provision
	// two acct_xxx accounts (each requiring KYC, each costing the
	// arranger separate onboarding steps). Callers derive from the
	// local org id, e.g. "connect-org-42".
	IdempotencyKey string
}

// AccountLinkParams describes a hosted onboarding URL request.
// RefreshURL is where the user goes if the link expires mid-
// flow; ReturnURL is where they land after completing (or
// abandoning) onboarding. Both must be absolute https URLs in
// production.
type AccountLinkParams struct {
	AccountID  string
	RefreshURL string
	ReturnURL  string
}

// ConnectedAccount is the cached status snapshot. Provider impls
// fill ChargesEnabled / PayoutsEnabled from whatever the underlying
// API surfaces; Stripe's `charges_enabled` and `payouts_enabled`
// translate one-to-one. Other providers may approximate (e.g.
// "verified" maps to ChargesEnabled).
type ConnectedAccount struct {
	ID             string
	ChargesEnabled bool
	PayoutsEnabled bool
}
