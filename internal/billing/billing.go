// Package billing charges organisations for the teams and phones that
// play their events (PLATFORM_PRICING.md, "pay per team").
//
// Every organisation has a balance: one running sum, in öre, of what it
// put in (top-ups, their bonus, monthly bills paid) and what its teams
// and phones cost (billing_entries). A team is charged the first time
// it joins a game, a phone past the team's included ones the first time
// it opens its seat. Charges are refused when they would take the
// balance below the organisation's floor: the overdraft for a prepaid
// organisation, its credit limit for a monthly one.
//
// Not charged: dev events, events paid at checkout (billing =
// 'prepaid': a family's party, a party offer bought in the store), and
// a team bought in the store up to the phones its ticket paid for.
package billing

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/klahr/zonventure/internal/settings"
)

// ErrCannotPay is a charge the organisation's balance can't take: the
// team can't start, or the phone can't join, until it tops up or pays.
var ErrCannotPay = errors.New("billing: the organisation can't pay for this")

// Querier is a *sql.DB or a *sql.Tx.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Account is an organisation's standing: its balance, the lowest it may
// go, and how it pays.
type Account struct {
	Mode         string `json:"mode"` // prepaid | monthly
	BalanceCents int64  `json:"balance_cents"`
	// FloorCents is the lowest the balance may go: minus the overdraft,
	// or minus the credit limit for a monthly organisation that can be
	// billed.
	FloorCents int64 `json:"floor_cents"`
	// CreditLimitCents is the monthly credit limit in force, 0 for a
	// prepaid organisation.
	CreditLimitCents int64 `json:"credit_limit_cents"`
	// Card is the saved card's label, "" for none; Invoice says the
	// organisation is billed by invoice (the trusted flag).
	Card    string `json:"card"`
	Invoice bool   `json:"invoice"`
	// Overdue says a monthly bill failed and is unpaid: no credit until
	// it is.
	Overdue bool `json:"overdue"`
}

// Billable reports whether a monthly organisation can actually be
// billed: by card, or by invoice.
func (a Account) Billable() bool { return a.Card != "" || a.Invoice }

// Available is how much more the organisation's teams may cost now.
func (a Account) Available() int64 {
	if n := a.BalanceCents - a.FloorCents; n > 0 {
		return n
	}
	return 0
}

// Load reads an organisation's account under the prices p.
func Load(ctx context.Context, q Querier, orgID int64, p settings.Pricing) (Account, error) {
	var (
		a        Account
		override sql.NullInt64
		paid     int64
	)
	if err := q.QueryRowContext(ctx, `
		SELECT o.billing_mode, o.credit_limit_cents, o.billing_card_label, o.trusted <> 0,
		       COALESCE((SELECT SUM(amount_cents) FROM billing_entries WHERE org_id = o.id), 0),
		       (SELECT COUNT(*) FROM billing_bills WHERE org_id = o.id AND state = 'paid'),
		       EXISTS (SELECT 1 FROM billing_bills WHERE org_id = o.id AND state = 'failed')
		FROM organizations o WHERE o.id = $1
	`, orgID).Scan(&a.Mode, &override, &a.Card, &a.Invoice, &a.BalanceCents, &paid, &a.Overdue); err != nil {
		return a, err
	}
	a.FloorCents = -p.OverdraftCents
	if a.Mode == "monthly" && a.Billable() {
		a.CreditLimitCents = p.CreditNewCents
		if paid >= p.CreditGrownAfter {
			a.CreditLimitCents = p.CreditGrownCents
		}
		if override.Valid {
			a.CreditLimitCents = override.Int64
		}
		a.FloorCents = -a.CreditLimitCents
		if a.Overdue {
			a.FloorCents = 0
		}
	}
	return a, nil
}

// charge adds a cost entry for orgID after locking the organisation, so
// two charges at once can't both take the last of its balance. It
// reports ErrCannotPay, adding nothing, when the balance can't take it.
func charge(ctx context.Context, tx *sql.Tx, orgID int64, p settings.Pricing, cost int64, insert func() (sql.Result, error)) error {
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM organizations WHERE id = $1 FOR UPDATE`, orgID); err != nil {
		return err
	}
	a, err := Load(ctx, tx, orgID, p)
	if err != nil {
		return err
	}
	if a.BalanceCents-cost < a.FloorCents {
		return ErrCannotPay
	}
	_, err = insert()
	return err
}

// billable is a team's event as charging sees it.
type billable struct {
	orgID, eventID int64
	round          int
	paidPhones     sql.NullInt64
	skip           bool
}

func loadTeam(ctx context.Context, tx *sql.Tx, teamID int64) (billable, error) {
	var (
		b       billable
		round   sql.NullInt64
		dev     bool
		billing string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT e.org_id, e.id, t.round, t.paid_phones, e.dev, e.billing
		FROM teams t JOIN events e ON e.id = t.event_id WHERE t.id = $1
	`, teamID).Scan(&b.orgID, &b.eventID, &round, &b.paidPhones, &dev, &billing); err != nil {
		return b, err
	}
	b.round = int(round.Int64)
	b.skip = dev || billing != "teams"
	return b, nil
}

// ChargeTeam charges the team's organisation for the team playing its
// game: once a game, and not at all for a store-bought team or an event
// that isn't billed per team. Call it in the transaction that starts
// the team (its first phone opening a seat, a hosted team joining).
func ChargeTeam(ctx context.Context, tx *sql.Tx, teamID int64, p settings.Pricing) error {
	b, err := loadTeam(ctx, tx, teamID)
	if err != nil || b.skip || b.paidPhones.Valid || p.TeamCents <= 0 {
		return err
	}
	var done bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM billing_entries WHERE kind = 'team' AND team_id = $1 AND round = $2)
	`, teamID, b.round).Scan(&done); err != nil || done {
		return err
	}
	return charge(ctx, tx, b.orgID, p, p.TeamCents, func() (sql.Result, error) {
		return tx.ExecContext(ctx, `
			INSERT INTO billing_entries(org_id, kind, amount_cents, currency, event_id, team_id, round)
			VALUES ($1, 'team', $2, $3, $4, $5, $6)
			ON CONFLICT DO NOTHING
		`, b.orgID, -p.TeamCents, strings.ToUpper(settings.Currency), b.eventID, teamID, b.round)
	})
}

// ChargePhone charges for a phone that has just opened its seat, when
// it is past the phones the team includes (or its ticket paid for).
// Call it after stamping the slot's claimed_at, in the same transaction.
func ChargePhone(ctx context.Context, tx *sql.Tx, slotID int64, p settings.Pricing) error {
	var teamID int64
	if err := tx.QueryRowContext(ctx, `SELECT team_id FROM player_slots WHERE id = $1`, slotID).Scan(&teamID); err != nil {
		return err
	}
	b, err := loadTeam(ctx, tx, teamID)
	if err != nil || b.skip || p.PhoneCents <= 0 {
		return err
	}
	included := p.TeamPhones
	if b.paidPhones.Valid && b.paidPhones.Int64 > included {
		included = b.paidPhones.Int64
	}
	// This phone's place among the team's: the seats opened before it,
	// and itself.
	var place int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM player_slots
		WHERE team_id = $1 AND claimed_at IS NOT NULL
		  AND (claimed_at, id) <= (SELECT claimed_at, id FROM player_slots WHERE id = $2)
	`, teamID, slotID).Scan(&place); err != nil {
		return err
	}
	if place <= included {
		return nil
	}
	var done bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM billing_entries WHERE kind = 'phone' AND slot_id = $1)
	`, slotID).Scan(&done); err != nil || done {
		return err
	}
	return charge(ctx, tx, b.orgID, p, p.PhoneCents, func() (sql.Result, error) {
		return tx.ExecContext(ctx, `
			INSERT INTO billing_entries(org_id, kind, amount_cents, currency, event_id, team_id, slot_id, round)
			VALUES ($1, 'phone', $2, $3, $4, $5, $6, $7)
			ON CONFLICT DO NOTHING
		`, b.orgID, -p.PhoneCents, strings.ToUpper(settings.Currency), b.eventID, teamID, slotID, b.round)
	})
}

// ClaimSeat is a phone opening its seat for the first time, in tx:
// the team is charged if this starts it, and the phone if it is past
// the ones included. ErrCannotPay leaves the seat to be rolled back.
func ClaimSeat(ctx context.Context, tx *sql.Tx, slotID int64, p settings.Pricing) error {
	var teamID int64
	if err := tx.QueryRowContext(ctx, `SELECT team_id FROM player_slots WHERE id = $1`, slotID).Scan(&teamID); err != nil {
		return err
	}
	if err := ChargeTeam(ctx, tx, teamID, p); err != nil {
		return err
	}
	return ChargePhone(ctx, tx, slotID, p)
}

// CanStart reports whether orgID's balance could pay for n more teams
// now: the check before a live event starts and before a team joins.
func CanStart(ctx context.Context, q Querier, orgID int64, n int64, p settings.Pricing) (bool, error) {
	a, err := Load(ctx, q, orgID, p)
	if err != nil {
		return false, err
	}
	return a.BalanceCents-n*p.TeamCents >= a.FloorCents, nil
}
