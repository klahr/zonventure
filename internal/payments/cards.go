package payments

import "context"

// CardBiller is the optional extension for monthly billing: saving an
// organisation's card through a hosted page, and charging it later,
// with nobody at the screen. Callers detect it with a type assertion.
type CardBiller interface {
	Provider

	// CreateCardSetup opens a hosted page that saves a card for the
	// customer: CustomerID, or one made now from Email (returned in the
	// session). Its completion arrives as EventCardSaved, carrying
	// Metadata.
	CreateCardSetup(ctx context.Context, p CardSetupParams) (CheckoutSession, error)

	// ChargeCard charges a saved card now, off session. A declined card
	// is not an error: Succeeded is false and Failure says why.
	ChargeCard(ctx context.Context, p CardChargeParams) (CardCharge, error)
}

// CardSetupParams describes a card to save.
type CardSetupParams struct {
	CustomerID string
	Email      string
	Currency   string
	SuccessURL string
	CancelURL  string
	Metadata   map[string]string
}

// CardChargeParams describes an off-session charge of a saved card.
type CardChargeParams struct {
	CustomerID      string
	PaymentMethodID string
	AmountCents     int64
	Currency        string
	Description     string
	IdempotencyKey  string
	Metadata        map[string]string
}

// CardCharge is how an off-session charge went.
type CardCharge struct {
	ID        string
	Succeeded bool
	Failure   string
}

// SavedCard is a card saved by CreateCardSetup: the customer it is
// saved on, its payment method, and how to show it ("Visa •••• 4242").
type SavedCard struct {
	CustomerID      string
	PaymentMethodID string
	Label           string
}
