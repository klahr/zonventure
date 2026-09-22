package server_test

import (
	"database/sql"
	"net/http/httptest"
	"testing"

	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/server"
)

// seedPurchaseStack seeds the minimum schema for purchase tests:
//   - one adventure ("demo") visible to the org
//   - one organization ("TestOrg")
//   - one verified org_admin ("boss" / "s3cret") so purchases pass the
//     email_verified gate
//   - one global admin ("root" / "rootpw") for cross-org assertions
//   - one second org with its own org_admin so we can prove cross-org
//     confirms are rejected.
//
// Returns the test server, the *sql.DB (so tests can mutate state
// directly), the boss user's org id, and a cleanup.
func seedPurchaseStack(t *testing.T, cfg server.Config) (*httptest.Server, *sql.DB, int64, func()) {
	t.Helper()
	sqlDB, dbCleanup := dbtest.Open(t)
	lib := adventure.New(sqlDB)

	if _, err := sqlDB.Exec(
		`INSERT INTO adventures(slug, title, description, author) VALUES ('demo', 'Demo', '', 'zv')`,
	); err != nil {
		dbCleanup()
		t.Fatalf("seed adventure: %v", err)
	}

	var orgID int64
	if err := sqlDB.QueryRow(
		`INSERT INTO organizations(name) VALUES ('TestOrg') RETURNING id`,
	).Scan(&orgID); err != nil {
		dbCleanup()
		t.Fatalf("seed org: %v", err)
	}
	// boss: org_admin of TestOrg. Post-migration 0020, the role
	// column was dropped from users; org-scoped role lives on
	// org_memberships and admin status on users.is_admin.
	hash, _ := auth.HashPassword("s3cret")
	var bossID int64
	if err := sqlDB.QueryRow(
		`INSERT INTO users(username, password_hash, email_verified) VALUES ('boss', $1, 1) RETURNING id`,
		hash,
	).Scan(&bossID); err != nil {
		dbCleanup()
		t.Fatalf("seed boss: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`,
		bossID, orgID,
	); err != nil {
		dbCleanup()
		t.Fatalf("seed boss membership: %v", err)
	}
	// root: global admin. is_admin=TRUE; no org membership.
	rootHash, _ := auth.HashPassword("rootpw")
	if _, err := sqlDB.Exec(
		`INSERT INTO users(username, password_hash, email_verified, is_admin) VALUES ('root', $1, 1, TRUE)`,
		rootHash,
	); err != nil {
		dbCleanup()
		t.Fatalf("seed root: %v", err)
	}

	// Second org + member so we can assert cross-org confirms are
	// rejected by the simulated path.
	var otherOrgID int64
	if err := sqlDB.QueryRow(
		`INSERT INTO organizations(name) VALUES ('OtherOrg') RETURNING id`,
	).Scan(&otherOrgID); err != nil {
		dbCleanup()
		t.Fatalf("seed other org: %v", err)
	}
	otherHash, _ := auth.HashPassword("otherpw")
	var rivalID int64
	if err := sqlDB.QueryRow(
		`INSERT INTO users(username, password_hash, email_verified) VALUES ('rival', $1, 1) RETURNING id`,
		otherHash,
	).Scan(&rivalID); err != nil {
		dbCleanup()
		t.Fatalf("seed rival: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`,
		rivalID, otherOrgID,
	); err != nil {
		dbCleanup()
		t.Fatalf("seed rival membership: %v", err)
	}

	cfg.DB = sqlDB
	cfg.Library = lib
	h := server.New(cfg)
	ts := httptest.NewServer(h)
	cleanup := func() {
		ts.Close()
		dbCleanup()
	}
	return ts, sqlDB, orgID, cleanup
}

func TestCreatePurchase_Simulated(t *testing.T) {
	ts, _, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	c := newClient(t, ts)
	if s := c.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	var resp struct {
		ID          int64  `json:"id"`
		AmountCents int    `json:"amount_cents"`
		State       string `json:"state"`
		Provider    string `json:"provider"`
		CheckoutURL string `json:"checkout_url"`
	}
	// 20 players: 15 free + 5 chargeable @ 500c each = 2500c.
	body := map[string]any{"adventure_slug": "demo", "max_players": 20}
	if s := c.do("POST", "/api/admin/purchases", body, &resp); s != 201 {
		t.Fatalf("create purchase = %d", s)
	}
	if resp.ID == 0 {
		t.Fatalf("expected purchase id, got %+v", resp)
	}
	if resp.AmountCents != 5*500 {
		t.Fatalf("amount_cents = %d, want 2500", resp.AmountCents)
	}
	if resp.Provider != "simulated" {
		t.Fatalf("provider = %q, want simulated (no Stripe creds set)", resp.Provider)
	}
	if resp.State != "pending" {
		t.Fatalf("state = %q, want pending", resp.State)
	}
	if resp.CheckoutURL == "" {
		t.Fatalf("missing checkout_url")
	}
}

// TestCreatePurchase_FreeTier asserts that buys at or below the free
// threshold skip Stripe, mark paid immediately, and return the
// assignment id directly.
func TestCreatePurchase_FreeTier(t *testing.T) {
	ts, _, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	c := newClient(t, ts)
	if s := c.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	var resp struct {
		ID           int64  `json:"id"`
		AmountCents  int    `json:"amount_cents"`
		State        string `json:"state"`
		Provider     string `json:"provider"`
		AssignmentID int64  `json:"assignment_id"`
	}
	body := map[string]any{"adventure_slug": "demo", "max_players": 15}
	if s := c.do("POST", "/api/admin/purchases", body, &resp); s != 201 {
		t.Fatalf("create free purchase = %d", s)
	}
	if resp.AmountCents != 0 {
		t.Fatalf("amount_cents = %d, want 0", resp.AmountCents)
	}
	if resp.Provider != "free" {
		t.Fatalf("provider = %q, want free", resp.Provider)
	}
	if resp.State != "paid" {
		t.Fatalf("state = %q, want paid", resp.State)
	}
	if resp.AssignmentID == 0 {
		t.Fatalf("expected assignment_id on free response")
	}
}

func TestCreatePurchase_RequiresEmailVerified(t *testing.T) {
	ts, db, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	// Demote boss to unverified so we can assert the gate fires.
	if _, err := db.Exec(
		`UPDATE users SET email_verified = 0 WHERE username = 'boss'`,
	); err != nil {
		t.Fatalf("demote boss: %v", err)
	}

	c := newClient(t, ts)
	if s := c.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	body := map[string]any{"adventure_slug": "demo", "max_players": 1}
	if s := c.do("POST", "/api/admin/purchases", body, nil); s != 403 {
		t.Fatalf("unverified create = %d, want 403", s)
	}
}

func TestCreatePurchase_RejectsBadInput(t *testing.T) {
	ts, _, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	c := newClient(t, ts)
	if s := c.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"zero players", map[string]any{"adventure_slug": "demo", "max_players": 0}, 400},
		{"too many", map[string]any{"adventure_slug": "demo", "max_players": 501}, 400},
		{"unknown adventure", map[string]any{"adventure_slug": "nope", "max_players": 20}, 404},
		{"unsupported provider", map[string]any{"adventure_slug": "demo", "max_players": 20, "provider": "paypal"}, 400},
		{"stripe without creds", map[string]any{"adventure_slug": "demo", "max_players": 20, "provider": "stripe"}, 503},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if s := c.do("POST", "/api/admin/purchases", tc.body, nil); s != tc.want {
				t.Fatalf("status = %d, want %d", s, tc.want)
			}
		})
	}
}

func TestConfirmPurchase_SimulatedFlow(t *testing.T) {
	ts, _, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	c := newClient(t, ts)
	if s := c.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	var created struct {
		ID int64 `json:"id"`
	}
	// 20 players triggers the simulated /confirm path; ≤15 would
	// short-circuit to the free path with no checkout step.
	body := map[string]any{"adventure_slug": "demo", "max_players": 20}
	if s := c.do("POST", "/api/admin/purchases", body, &created); s != 201 {
		t.Fatalf("create = %d", s)
	}

	var confirmed struct {
		ID           int64  `json:"id"`
		State        string `json:"state"`
		AssignmentID int64  `json:"assignment_id"`
	}
	if s := c.do("POST", "/api/admin/purchases/"+itoa(created.ID)+"/confirm", map[string]string{}, &confirmed); s != 200 {
		t.Fatalf("confirm = %d", s)
	}
	if confirmed.State != "paid" {
		t.Fatalf("state = %q, want paid", confirmed.State)
	}
	if confirmed.AssignmentID == 0 {
		t.Fatalf("expected assignment_id")
	}

	// Idempotent: a second confirm returns the same assignment id, no
	// duplicate row, still 200.
	var second struct {
		AssignmentID int64 `json:"assignment_id"`
	}
	if s := c.do("POST", "/api/admin/purchases/"+itoa(created.ID)+"/confirm", map[string]string{}, &second); s != 200 {
		t.Fatalf("repeat confirm = %d", s)
	}
	if second.AssignmentID != confirmed.AssignmentID {
		t.Fatalf("repeat confirm minted a new assignment: %d vs %d", second.AssignmentID, confirmed.AssignmentID)
	}

	// Listing for the buyer surfaces the paid row with the assignment.
	var list struct {
		Purchases []struct {
			ID           int64  `json:"id"`
			State        string `json:"state"`
			AssignmentID *int64 `json:"assignment_id"`
		} `json:"purchases"`
	}
	if s := c.do("GET", "/api/admin/purchases", nil, &list); s != 200 {
		t.Fatalf("list = %d", s)
	}
	var found bool
	for _, p := range list.Purchases {
		if p.ID == created.ID {
			found = true
			if p.State != "paid" || p.AssignmentID == nil || *p.AssignmentID != confirmed.AssignmentID {
				t.Fatalf("listed row = %+v", p)
			}
		}
	}
	if !found {
		t.Fatalf("purchase %d not in listing", created.ID)
	}
}

func TestConfirmPurchase_CrossOrgForbidden(t *testing.T) {
	ts, _, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	// Boss creates the purchase.
	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login boss = %d", s)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	// 20 players forces the simulated/non-free path so a /confirm
	// call is required (free buys are auto-paid at create time).
	body := map[string]any{"adventure_slug": "demo", "max_players": 20}
	if s := boss.do("POST", "/api/admin/purchases", body, &created); s != 201 {
		t.Fatalf("create = %d", s)
	}

	// Rival from a different org can't confirm it.
	rival := newClient(t, ts)
	if s := rival.do("POST", "/api/auth/login", map[string]string{"username": "rival", "password": "otherpw"}, nil); s != 200 {
		t.Fatalf("login rival = %d", s)
	}
	if s := rival.do("POST", "/api/admin/purchases/"+itoa(created.ID)+"/confirm", map[string]string{}, nil); s != 403 {
		t.Fatalf("rival confirm = %d, want 403", s)
	}

	// Rival also can't see it in their listing.
	var list struct {
		Purchases []struct {
			ID int64 `json:"id"`
		} `json:"purchases"`
	}
	if s := rival.do("GET", "/api/admin/purchases", nil, &list); s != 200 {
		t.Fatalf("rival list = %d", s)
	}
	for _, p := range list.Purchases {
		if p.ID == created.ID {
			t.Fatalf("rival saw cross-org purchase %d", p.ID)
		}
	}
}

func TestConfirmPurchase_UnknownPurchase(t *testing.T) {
	ts, _, _, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	c := newClient(t, ts)
	if s := c.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	if s := c.do("POST", "/api/admin/purchases/999999/confirm", map[string]string{}, nil); s != 404 {
		t.Fatalf("status = %d, want 404", s)
	}
	if s := c.do("POST", "/api/admin/purchases/0/confirm", map[string]string{}, nil); s != 400 {
		t.Fatalf("zero id = %d, want 400", s)
	}
}
