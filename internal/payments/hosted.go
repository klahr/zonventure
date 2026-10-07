package payments

import "github.com/klahr/zonventure/internal/settings"

// HostedPass is the platform's price for a hosted event: BlockCents for
// each started block of teams a game may hold. A quiz trail and "Host
// it" are charged it on the platform's own account; for a party an org
// sells, it is the platform's application fee instead of PlatformFee.
type HostedPass struct {
	BlockCents int64
	Currency   string
}

// HostedFrom is the hosted pass in prices, in the platform's currency.
func HostedFrom(p settings.Pricing) HostedPass {
	return HostedPass{BlockCents: p.HostedBlockCents, Currency: settings.Currency}
}

// Price is what blocks blocks cost.
func (p HostedPass) Price(blocks int) int64 { return int64(blocks) * p.BlockCents }

// Free is whether hosting costs nothing here.
func (p HostedPass) Free() bool { return p.BlockCents <= 0 }

// Fee is the platform's cut of a party sold for amountCents holding
// blocks blocks: the pass price, kept strictly below the charge as
// Stripe requires (see PlatformFee.Compute).
func (p HostedPass) Fee(amountCents int64, blocks int) int64 {
	fee := p.Price(blocks)
	if fee >= amountCents {
		fee = amountCents - 1
	}
	return max(fee, 0)
}
