package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/settings"
)

var prices = settings.Pricing{TeamCents: 4900, TeamPhones: 3, PhoneCents: 1500,
	OverdraftCents: 25000, CreditNewCents: 50000, CreditGrownCents: 500000, CreditGrownAfter: 3}

type fixture struct {
	t     *testing.T
	db    *sql.DB
	orgID int64
	n     int
}

func newFixture(t *testing.T) (*fixture, func()) {
	t.Helper()
	db, cleanup := dbtest.Open(t)
	f := &fixture{t: t, db: db}
	f.must(db.QueryRow(`INSERT INTO organizations(name, slug) VALUES ('Acme', 'acme') RETURNING id`).Scan(&f.orgID))
	f.exec(`INSERT INTO adventures(slug, title, org_id) VALUES ('walk', 'Walk', $1)`, f.orgID)
	return f, cleanup
}

func (f *fixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	_, err := f.db.Exec(q, args...)
	f.must(err)
}

// event makes an event of the org's: dev, or billed as billing says.
func (f *fixture) event(dev bool, billing string) int64 {
	f.t.Helper()
	f.n++
	var id int64
	f.must(f.db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name, dev, billing)
		VALUES ('walk', $1, 'running', $2, 'Event', $3, $4) RETURNING id
	`, f.orgID, fmt.Sprintf("lt-%d", f.n), dev, billing).Scan(&id))
	return id
}

// team makes a team of seats phones; paid is its ticket's phones, 0 for
// none.
func (f *fixture) team(eventID int64, seats, paid int) (int64, []int64) {
	f.t.Helper()
	f.n++
	var teamID int64
	var paidArg any
	if paid > 0 {
		paidArg = paid
	}
	f.must(f.db.QueryRow(`INSERT INTO teams(event_id, name, token, paid_phones) VALUES ($1, $2, $2, $3) RETURNING id`,
		eventID, fmt.Sprintf("tok-%d", f.n), paidArg).Scan(&teamID))
	var slots []int64
	for i := range seats {
		var id int64
		f.must(f.db.QueryRow(`INSERT INTO player_slots(team_id, name, token) VALUES ($1, $2, $3) RETURNING id`,
			teamID, fmt.Sprintf("P%d", i+1), fmt.Sprintf("slot-%d-%d", f.n, i)).Scan(&id))
		slots = append(slots, id)
	}
	return teamID, slots
}

// claim opens a seat as the login does, in one transaction.
func (f *fixture) claim(slotID int64) error {
	f.t.Helper()
	ctx := context.Background()
	tx, err := f.db.BeginTx(ctx, nil)
	f.must(err)
	defer tx.Rollback() //nolint:errcheck
	_, err = tx.ExecContext(ctx, `UPDATE player_slots SET claimed_at = COALESCE(claimed_at, now_text()) WHERE id = $1`, slotID)
	f.must(err)
	if err := ClaimSeat(ctx, tx, slotID, prices); err != nil {
		return err
	}
	return tx.Commit()
}

func (f *fixture) account() Account {
	f.t.Helper()
	a, err := Load(context.Background(), f.db, f.orgID, prices)
	f.must(err)
	return a
}

func (f *fixture) topUp(cents int64) {
	f.exec(`INSERT INTO billing_entries(org_id, kind, amount_cents, currency) VALUES ($1, 'top_up', $2, 'SEK')`, f.orgID, cents)
}

func TestTeamAndPhones(t *testing.T) {
	f, cleanup := newFixture(t)
	defer cleanup()
	f.topUp(100000)
	ev := f.event(false, "teams")
	_, slots := f.team(ev, 5, 0)
	for _, s := range slots {
		f.must(f.claim(s))
	}
	// Opening a seat again charges nothing more.
	f.must(f.claim(slots[0]))
	f.must(f.claim(slots[4]))
	if got := f.account().BalanceCents; got != 100000-4900-2*1500 {
		t.Errorf("balance after a team of 5 = %d", got)
	}
}

func TestNotCharged(t *testing.T) {
	f, cleanup := newFixture(t)
	defer cleanup()
	f.topUp(100000)
	_, devSlots := f.team(f.event(true, "teams"), 4, 0)
	_, partySlots := f.team(f.event(false, "prepaid"), 4, 0)
	_, storeSlots := f.team(f.event(false, "teams"), 6, 5)
	for _, s := range append(append(devSlots, partySlots...), storeSlots...) {
		f.must(f.claim(s))
	}
	// The store team's ticket paid for 5 phones: the 6th is charged.
	if got := f.account().BalanceCents; got != 100000-1500 {
		t.Errorf("balance = %d, want only the store team's 6th phone charged", got)
	}
}

func TestPrepaidOverdraft(t *testing.T) {
	f, cleanup := newFixture(t)
	defer cleanup()
	ev := f.event(false, "teams")
	// No balance: 5 teams fit in the 250 kr overdraft (5 × 49 = 245).
	for i := range 6 {
		_, slots := f.team(ev, 1, 0)
		err := f.claim(slots[0])
		if i < 5 && err != nil {
			t.Fatalf("team %d: %v", i+1, err)
		}
		if i == 5 && !errors.Is(err, ErrCannotPay) {
			t.Fatalf("team 6: %v, want ErrCannotPay", err)
		}
	}
	if got := f.account().BalanceCents; got != -5*4900 {
		t.Errorf("balance = %d", got)
	}
	var claimed int
	f.must(f.db.QueryRow(`SELECT COUNT(*) FROM player_slots WHERE claimed_at IS NOT NULL`).Scan(&claimed))
	if claimed != 5 {
		t.Errorf("%d seats claimed, the refused one rolled back with its charge", claimed)
	}
}

func TestMonthlyCredit(t *testing.T) {
	f, cleanup := newFixture(t)
	defer cleanup()
	f.exec(`UPDATE organizations SET billing_mode = 'monthly' WHERE id = $1`, f.orgID)
	// Monthly without a card or invoice billing: still the overdraft.
	if a := f.account(); a.FloorCents != -25000 || a.Billable() {
		t.Errorf("unbillable monthly: %+v", a)
	}
	f.exec(`UPDATE organizations SET billing_card_label = 'Visa •••• 4242', billing_payment_method = 'pm_1' WHERE id = $1`, f.orgID)
	if a := f.account(); a.FloorCents != -50000 || a.CreditLimitCents != 50000 {
		t.Errorf("new monthly: %+v", a)
	}
	for i, period := range []string{"2026-01", "2026-02", "2026-03"} {
		f.exec(`INSERT INTO billing_bills(org_id, period, method, amount_cents, currency, state) VALUES ($1, $2, 'card', 100, 'SEK', 'paid')`, f.orgID, period)
		want := int64(50000)
		if i == 2 {
			want = 500000
		}
		if a := f.account(); a.CreditLimitCents != want {
			t.Errorf("after %d paid bills: limit %d, want %d", i+1, a.CreditLimitCents, want)
		}
	}
	f.exec(`UPDATE organizations SET credit_limit_cents = 1000 WHERE id = $1`, f.orgID)
	if a := f.account(); a.CreditLimitCents != 1000 {
		t.Errorf("override: %+v", a)
	}
	f.exec(`INSERT INTO billing_bills(org_id, period, method, amount_cents, currency, state) VALUES ($1, '2026-04', 'card', 100, 'SEK', 'failed')`, f.orgID)
	if a := f.account(); a.FloorCents != 0 || !a.Overdue {
		t.Errorf("overdue: %+v", a)
	}
	ok, err := CanStart(context.Background(), f.db, f.orgID, 1, prices)
	if err != nil || ok {
		t.Errorf("overdue can start: %v %v", ok, err)
	}
}

func TestHostedTeamChargedPerGame(t *testing.T) {
	f, cleanup := newFixture(t)
	defer cleanup()
	f.topUp(100000)
	ev := f.event(false, "teams")
	teamID, slots := f.team(ev, 1, 0)
	f.must(f.claim(slots[0]))
	f.exec(`UPDATE teams SET round = 2 WHERE id = $1`, teamID)
	ctx := context.Background()
	tx, err := f.db.BeginTx(ctx, nil)
	f.must(err)
	f.must(ChargeTeam(ctx, tx, teamID, prices))
	f.must(ChargeTeam(ctx, tx, teamID, prices))
	f.must(tx.Commit())
	if got := f.account().BalanceCents; got != 100000-2*4900 {
		t.Errorf("balance = %d, want one charge per game", got)
	}
}
