package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// Currency is the one currency the platform and its orgs sell in, ISO
// 4217 lower case. LoadCurrency sets it from ZV_CURRENCY at startup.
var Currency = "sek"

// zeroDecimal are currencies without a minor unit. Amounts are stored
// in hundredths, so they would be charged a hundredfold.
var zeroDecimal = map[string]bool{
	"bif": true, "clp": true, "djf": true, "gnf": true, "jpy": true, "kmf": true, "krw": true, "mga": true,
	"pyg": true, "rwf": true, "ugx": true, "vnd": true, "vuv": true, "xaf": true, "xof": true, "xpf": true,
}

// LoadCurrency sets Currency from ZV_CURRENCY; unset keeps sek.
func LoadCurrency() error {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("ZV_CURRENCY")))
	if v == "" {
		return nil
	}
	if len(v) != 3 || strings.Trim(v, "abcdefghijklmnopqrstuvwxyz") != "" {
		return fmt.Errorf("%q is not a three-letter ISO 4217 code", v)
	}
	if zeroDecimal[v] {
		return fmt.Errorf("%s has no minor unit; amounts are stored in hundredths", v)
	}
	Currency = v
	return nil
}

// Pricing is what the platform charges, edited by the platform admin
// under Settings → Pricing. Amounts are in Currency's minor unit, VAT included unless
// VATExcluded.
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
	// VATExcluded says the prices are before VAT; every price shown
	// says "incl. VAT" or "excl. VAT" after it.
	VATExcluded bool `json:"vat_excluded"`
	// PayoutTerms is when and how an org is paid for the tickets it
	// sells while there is no platform fee (the platform's account
	// takes the money), shown on the org's Billing page; empty shows
	// the built-in terms.
	PayoutTerms string `json:"payout_terms"`
	// DomainYearCents is an organisation's own domain, per year; 0
	// shows "Contact us" with no price. DomainContactURL is where an
	// organisation asks for it (a mailto: or https: link); empty hides
	// the offer.
	DomainYearCents  int64  `json:"domain_year_cents"`
	DomainContactURL string `json:"domain_contact_url"`
}

// MaxPayoutTerms is the longest PayoutTerms may be.
const MaxPayoutTerms = 1000

// DefaultPricing applies to any price never saved.
var DefaultPricing = Pricing{PlayerCents: 4900, FreePlayers: 15, HostedBlockCents: 14900}

const (
	keyPlayerCents      = "price_player_cents"
	keyFreePlayers      = "price_free_players"
	keyHostedBlockCents = "price_hosted_block_cents"
	keyFeePercentBPS    = "price_fee_percent_bps"
	keyFeeFixedCents    = "price_fee_fixed_cents"
	keyFeeMinCents      = "price_fee_min_cents"
	keyVATExcluded      = "price_vat_excluded"
	keyPayoutTerms      = "payout_terms"
	keyDomainYearCents  = "price_domain_year_cents"
	keyDomainContactURL = "domain_contact_url"
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
		{keyDomainYearCents, &p.DomainYearCents},
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
		if v, err := getString(ctx, db, keyVATExcluded); err == nil {
			p.VATExcluded = parseBool(v, false)
		}
		if v, err := getString(ctx, db, keyPayoutTerms); err == nil {
			p.PayoutTerms = v
		}
		if v, err := getString(ctx, db, keyDomainContactURL); err == nil {
			p.DomainContactURL = v
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
	if utf8.RuneCountInString(p.PayoutTerms) > MaxPayoutTerms {
		return errors.New("the payout terms are too long")
	}
	if u := strings.TrimSpace(p.DomainContactURL); u != "" {
		low := strings.ToLower(u)
		if len(u) > 500 || !(strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "mailto:")) {
			return errors.New("the domain contact link must start with https:// or mailto:")
		}
	}
	return nil
}

// SetPricing saves every price in one transaction and puts them in force.
func SetPricing(ctx context.Context, db *sql.DB, p Pricing) error {
	p.PayoutTerms = strings.TrimSpace(p.PayoutTerms)
	p.DomainContactURL = strings.TrimSpace(p.DomainContactURL)
	if err := p.Validate(); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	values := map[string]string{keyVATExcluded: "0", keyPayoutTerms: p.PayoutTerms, keyDomainContactURL: p.DomainContactURL}
	if p.VATExcluded {
		values[keyVATExcluded] = "1"
	}
	for _, f := range p.fields() {
		values[f.key] = strconv.FormatInt(*f.v, 10)
	}
	for k, v := range values {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings(key, value, updated_at)
			VALUES ($1, $2, now_text())
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
		`, k, v); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	pricing.Store(&p)
	return nil
}
