package settings

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"sync/atomic"
)

// Currency is the one currency the platform and its orgs sell in.
const Currency = "sek"

// Pricing is what the platform charges, edited by the platform admin
// under Settings → Pricing. Amounts are öre, VAT included.
type Pricing struct {
	// PlayerCents is the event pass: each player slot beyond FreePlayers.
	PlayerCents int64 `json:"player_cents"`
	FreePlayers int64 `json:"free_players"`
	// HostedBlockCents is the hosted pass per started block of teams; 0 is free.
	HostedBlockCents int64 `json:"hosted_block_cents"`
	// The platform's cut of tickets and parties orgs sell; all 0 is none.
	FeePercentBPS int64 `json:"fee_percent_bps"`
	FeeFixedCents int64 `json:"fee_fixed_cents"`
	FeeMinCents   int64 `json:"fee_min_cents"`
}

// DefaultPricing applies to any price never saved.
var DefaultPricing = Pricing{PlayerCents: 4900, FreePlayers: 15, HostedBlockCents: 14900}

const (
	keyPlayerCents      = "price_player_cents"
	keyFreePlayers      = "price_free_players"
	keyHostedBlockCents = "price_hosted_block_cents"
	keyFeePercentBPS    = "price_fee_percent_bps"
	keyFeeFixedCents    = "price_fee_fixed_cents"
	keyFeeMinCents      = "price_fee_min_cents"
)

func (p *Pricing) fields() []struct {
	key string
	v   *int64
} {
	return []struct {
		key string
		v   *int64
	}{
		{keyPlayerCents, &p.PlayerCents},
		{keyFreePlayers, &p.FreePlayers},
		{keyHostedBlockCents, &p.HostedBlockCents},
		{keyFeePercentBPS, &p.FeePercentBPS},
		{keyFeeFixedCents, &p.FeeFixedCents},
		{keyFeeMinCents, &p.FeeMinCents},
	}
}

// pricing is the in-process copy every checkout reads, seeded by
// LoadPricing at startup and replaced by SetPricing.
var pricing atomic.Pointer[Pricing]

// CurrentPricing returns the prices in force: the saved ones, else the defaults.
func CurrentPricing() Pricing {
	if p := pricing.Load(); p != nil {
		return *p
	}
	return DefaultPricing
}

// LoadPricing seeds the in-memory prices from the settings table, a
// missing or unreadable row keeping its default. Tolerates a nil db.
func LoadPricing(ctx context.Context, db *sql.DB) {
	p := DefaultPricing
	if db != nil {
		for _, f := range p.fields() {
			v, err := getString(ctx, db, f.key)
			if err != nil {
				continue
			}
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
				*f.v = n
			}
		}
	}
	pricing.Store(&p)
}

// Validate checks every price is a whole, non-negative number and the
// percentage at most 100 %.
func (p Pricing) Validate() error {
	for _, f := range p.fields() {
		if *f.v < 0 {
			return errors.New(f.key + " must not be negative")
		}
	}
	if p.FeePercentBPS > 10000 {
		return errors.New("the platform fee's percentage must be at most 100 %")
	}
	return nil
}

// SetPricing saves every price in one transaction and puts them in force.
func SetPricing(ctx context.Context, db *sql.DB, p Pricing) error {
	if err := p.Validate(); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, f := range p.fields() {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings(key, value, updated_at)
			VALUES ($1, $2, now_text())
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
		`, f.key, strconv.FormatInt(*f.v, 10)); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	pricing.Store(&p)
	return nil
}
