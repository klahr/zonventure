package payments

import (
	"os"
	"strconv"
	"strings"
)

// HostedPass is the platform's price for a hosted event: BlockCents for
// each started block of teams a game may hold. A quiz trail and "Host
// it" are charged it on the platform's own account; for a party an org
// sells, it is the platform's application fee instead of PlatformFee.
type HostedPass struct {
	BlockCents int64
	Currency   string
}

// DefaultHostedBlockCents is the pass price of a hosted instance (one
// that takes a platform fee): 149 kr.
const DefaultHostedBlockCents = 14900

// HostedPassFromEnv reads ZV_HOSTED_BLOCK_CENTS and ZV_HOSTED_CURRENCY.
// Unset, the price is DefaultHostedBlockCents where the platform takes a
// fee (feeEnabled) and 0 — free — on a self-host that doesn't.
func HostedPassFromEnv(feeEnabled bool) HostedPass {
	p := HostedPass{Currency: "sek"}
	if feeEnabled {
		p.BlockCents = DefaultHostedBlockCents
	}
	if v, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("ZV_HOSTED_BLOCK_CENTS")), 10, 64); err == nil && v >= 0 {
		p.BlockCents = v
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("ZV_HOSTED_CURRENCY"))); v != "" {
		p.Currency = v
	}
	return p
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
