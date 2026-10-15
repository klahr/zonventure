package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
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
	// Pay per team (PLATFORM_PRICING.md): every team that plays costs
	// TeamCents, its first TeamPhones phones included, PhoneCents each
	// phone after. Kids' parties from the public creator are free up to
	// PartyFreeTeams teams a game, for PartyGames games.
	TeamCents      int64 `json:"team_cents"`
	TeamPhones     int64 `json:"team_phones"`
	PhoneCents     int64 `json:"phone_cents"`
	PartyFreeTeams int64 `json:"party_free_teams"`
	PartyGames     int64 `json:"party_games"`
	// OverdraftCents is how far below zero a prepaid balance may go, so
	// a team at the start line isn't turned away. CreditNewCents is a
	// monthly-billed org's credit limit until it has paid
	// CreditGrownAfter bills, CreditGrownCents after.
	OverdraftCents   int64 `json:"overdraft_cents"`
	CreditNewCents   int64 `json:"credit_new_cents"`
	CreditGrownCents int64 `json:"credit_grown_cents"`
	CreditGrownAfter int64 `json:"credit_grown_after"`
	// TopUps are the prepaid amounts an org can buy, each with its bonus.
	TopUps []TopUp `json:"top_ups"`

	// VATExcluded says the prices are before VAT; every price shown
	// says "incl. VAT" or "excl. VAT" after it.
	VATExcluded bool `json:"vat_excluded"`
	// DomainYearCents is an organisation's own domain, per year; 0
	// shows "Contact us" with no price. DomainContactURL is where an
	// organisation asks for it (a mailto: or https: link); empty hides
	// the offer.
	DomainYearCents  int64  `json:"domain_year_cents"`
	DomainContactURL string `json:"domain_contact_url"`
}

// TopUp is a prepaid amount an org buys and the bonus it gets on top.
type TopUp struct {
	AmountCents int64 `json:"amount_cents"`
	BonusCents  int64 `json:"bonus_cents"`
}

// MaxTopUps is how many top-up amounts there may be.
const MaxTopUps = 4

// DefaultPricing applies to any price never saved.
var DefaultPricing = Pricing{TeamCents: 4900, TeamPhones: 3, PhoneCents: 1500, PartyFreeTeams: 5, PartyGames: 3,
	OverdraftCents: 25000, CreditNewCents: 50000, CreditGrownCents: 500000, CreditGrownAfter: 3,
	TopUps: []TopUp{{50000, 0}, {200000, 20000}, {500000, 100000}, {2000000, 500000}}}

const (
	keyVATExcluded      = "price_vat_excluded"
	keyDomainYearCents  = "price_domain_year_cents"
	keyDomainContactURL = "domain_contact_url"
	keyTeamCents        = "price_team_cents"
	keyTeamPhones       = "price_team_phones"
	keyPhoneCents       = "price_phone_cents"
	keyPartyFreeTeams   = "price_party_free_teams"
	keyPartyGames       = "price_party_games"
	keyOverdraftCents   = "billing_overdraft_cents"
	keyCreditNewCents   = "billing_credit_new_cents"
	keyCreditGrownCents = "billing_credit_grown_cents"
	keyCreditGrownAfter = "billing_credit_grown_after"
	keyTopUps           = "billing_top_ups"
)

func (p *Pricing) fields() []struct {
	key string
	v   *int64
} {
	return []struct {
		key string
		v   *int64
	}{
		{keyDomainYearCents, &p.DomainYearCents},
		{keyTeamCents, &p.TeamCents},
		{keyTeamPhones, &p.TeamPhones},
		{keyPhoneCents, &p.PhoneCents},
		{keyPartyFreeTeams, &p.PartyFreeTeams},
		{keyPartyGames, &p.PartyGames},
		{keyOverdraftCents, &p.OverdraftCents},
		{keyCreditNewCents, &p.CreditNewCents},
		{keyCreditGrownCents, &p.CreditGrownCents},
		{keyCreditGrownAfter, &p.CreditGrownAfter},
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
		if v, err := getString(ctx, db, keyDomainContactURL); err == nil {
			p.DomainContactURL = v
		}
		if v, err := getString(ctx, db, keyTopUps); err == nil {
			var t []TopUp
			if json.Unmarshal([]byte(v), &t) == nil && validTopUps(t) == nil {
				p.TopUps = t
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
	if p.TeamPhones < 1 {
		return errors.New("a team includes at least one phone")
	}
	if p.PartyGames < 1 {
		return errors.New("a party has at least one game")
	}
	if err := validTopUps(p.TopUps); err != nil {
		return err
	}
	if u := strings.TrimSpace(p.DomainContactURL); u != "" {
		low := strings.ToLower(u)
		if len(u) > 500 || !(strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "mailto:")) {
			return errors.New("the domain contact link must start with https:// or mailto:")
		}
	}
	return nil
}

// validTopUps checks the top-ups: one to MaxTopUps, each a positive
// amount with a bonus of zero or more, no amount twice.
func validTopUps(t []TopUp) error {
	if len(t) < 1 || len(t) > MaxTopUps {
		return fmt.Errorf("offer between 1 and %d top-up amounts", MaxTopUps)
	}
	seen := map[int64]bool{}
	for _, u := range t {
		if u.AmountCents <= 0 || u.BonusCents < 0 || seen[u.AmountCents] {
			return errors.New("each top-up needs its own positive amount, and a bonus of zero or more")
		}
		seen[u.AmountCents] = true
	}
	return nil
}

// SetPricing saves every price in one transaction and puts them in force.
func SetPricing(ctx context.Context, db *sql.DB, p Pricing) error {
	p.DomainContactURL = strings.TrimSpace(p.DomainContactURL)
	if err := p.Validate(); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	values := map[string]string{keyVATExcluded: "0", keyDomainContactURL: p.DomainContactURL}
	if p.VATExcluded {
		values[keyVATExcluded] = "1"
	}
	for _, f := range p.fields() {
		values[f.key] = strconv.FormatInt(*f.v, 10)
	}
	topUps, err := json.Marshal(p.TopUps)
	if err != nil {
		return err
	}
	values[keyTopUps] = string(topUps)
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
