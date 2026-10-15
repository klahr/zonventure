package stripe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/payments"

	stripego "github.com/stripe/stripe-go/v87"
	checkoutsession "github.com/stripe/stripe-go/v87/checkout/session"
	"github.com/stripe/stripe-go/v87/customer"
	"github.com/stripe/stripe-go/v87/paymentintent"
)

var _ payments.CardBiller = (*Provider)(nil)

// CreateCardSetup opens a Checkout session in setup mode, saving a card
// on the customer (made now from the email when there is none) for
// charging later, off session.
func (p *Provider) CreateCardSetup(ctx context.Context, params payments.CardSetupParams) (payments.CheckoutSession, error) {
	if params.SuccessURL == "" || params.CancelURL == "" {
		return payments.CheckoutSession{}, errors.New("stripe: missing success/cancel URL")
	}
	stripego.Key = p.SecretKey
	cust := params.CustomerID
	if cust == "" {
		cp := &stripego.CustomerParams{Email: stripego.String(params.Email)}
		cp.Context = ctx
		for k, v := range params.Metadata {
			cp.AddMetadata(k, v)
		}
		c, err := customer.New(cp)
		if err != nil {
			return payments.CheckoutSession{}, err
		}
		cust = c.ID
	}
	sp := &stripego.CheckoutSessionParams{
		Mode:       stripego.String(string(stripego.CheckoutSessionModeSetup)),
		Customer:   stripego.String(cust),
		Currency:   stripego.String(strings.ToLower(params.Currency)),
		SuccessURL: stripego.String(params.SuccessURL),
		CancelURL:  stripego.String(params.CancelURL),
	}
	for k, v := range params.Metadata {
		sp.AddMetadata(k, v)
	}
	sp.Context = ctx
	s, err := checkoutsession.New(sp)
	if err != nil {
		return payments.CheckoutSession{}, err
	}
	return payments.CheckoutSession{URL: s.URL, SessionID: s.ID, CustomerID: cust}, nil
}

// savedCard reads the card a completed setup session saved.
func (p *Provider) savedCard(sessionID string) (*payments.SavedCard, error) {
	stripego.Key = p.SecretKey
	ctx, cancel := context.WithTimeout(context.Background(), webhookAPITimeout)
	defer cancel()
	params := &stripego.CheckoutSessionParams{}
	params.Context = ctx
	params.AddExpand("setup_intent.payment_method")
	s, err := checkoutsession.Get(sessionID, params)
	if err != nil {
		return nil, err
	}
	if s.SetupIntent == nil || s.SetupIntent.PaymentMethod == nil {
		return nil, fmt.Errorf("setup session %s saved no payment method", sessionID)
	}
	pm := s.SetupIntent.PaymentMethod
	card := &payments.SavedCard{PaymentMethodID: pm.ID, Label: "Card"}
	if s.Customer != nil {
		card.CustomerID = s.Customer.ID
	}
	if pm.Card != nil {
		brand := string(pm.Card.Brand)
		if brand != "" {
			brand = strings.ToUpper(brand[:1]) + brand[1:]
		}
		card.Label = strings.TrimSpace(brand + " •••• " + pm.Card.Last4)
	}
	return card, nil
}

// ChargeCard charges a saved card off session. A card the bank declines
// comes back as not succeeded, with Stripe's message.
func (p *Provider) ChargeCard(ctx context.Context, params payments.CardChargeParams) (payments.CardCharge, error) {
	stripego.Key = p.SecretKey
	pp := &stripego.PaymentIntentParams{
		Amount:        stripego.Int64(params.AmountCents),
		Currency:      stripego.String(strings.ToLower(params.Currency)),
		Customer:      stripego.String(params.CustomerID),
		PaymentMethod: stripego.String(params.PaymentMethodID),
		OffSession:    stripego.Bool(true),
		Confirm:       stripego.Bool(true),
		Description:   stripego.String(params.Description),
	}
	for k, v := range params.Metadata {
		pp.AddMetadata(k, v)
	}
	if params.IdempotencyKey != "" {
		pp.IdempotencyKey = stripego.String(params.IdempotencyKey)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pp.Context = ctx
	pi, err := paymentintent.New(pp)
	var se *stripego.Error
	if errors.As(err, &se) && se.Type == stripego.ErrorTypeCard {
		return payments.CardCharge{Failure: se.Msg}, nil
	}
	if err != nil {
		return payments.CardCharge{}, err
	}
	out := payments.CardCharge{ID: pi.ID, Succeeded: pi.Status == stripego.PaymentIntentStatusSucceeded}
	if !out.Succeeded {
		out.Failure = "the payment is " + string(pi.Status)
	}
	return out, nil
}
