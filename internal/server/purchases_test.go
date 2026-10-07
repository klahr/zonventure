package server_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klahr/zonventure/internal/adventure"
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
	// Passes here are bought for the seeded demo, a catalog adventure.
	withCatalog(t)
	sqlDB, dbCleanup := dbtest.Open(t)
	lib := adventure.New(sqlDB)

	if _, err := sqlDB.Exec(
		`INSERT INTO adventures(slug, title, description, author, org_id, published) VALUES ('demo', 'Demo', '', 'zv', $1, 1)`,
		seedPublisher(t, sqlDB),
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
	hash := fixtureHash(t, "s3cret")
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
	rootHash := fixtureHash(t, "rootpw")
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
	otherHash := fixtureHash(t, "otherpw")
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

// simulatedPaymentsConfig wires the dev-only simulated provider, the
// only configuration under which a paid buy self-confirms via /confirm.
var simulatedPaymentsConfig = server.Config{PaymentsProvider: "simulated", Env: "dev"}

// TestPurchase_NoProvider covers buying with no payment provider
// configured, on one server. Each subtest makes its own purchases;
// "requires email verified" demotes boss, so it runs last.
func TestPurchase_NoProvider(t *testing.T) {
	ts, db, orgID, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	t.Run("rejects bad input", func(t *testing.T) {
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
			{"negative players", map[string]any{"adventure_slug": "demo", "max_players": -3}, 400},
			{"too many", map[string]any{"adventure_slug": "demo", "max_players": 501}, 400},
			{"far too many", map[string]any{"adventure_slug": "demo", "max_players": 10000}, 400},
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
	})

	t.Run("confirm unknown purchase", func(t *testing.T) {
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
	})

	// Without the simulated provider wired, a paid buy must not
	// self-fulfil: the default refuses, naming "simulated" is forbidden,
	// and /confirm on a pending row is closed to the buyer.
	t.Run("paid refused without provider", func(t *testing.T) {
		c := newClient(t, ts)
		if s := c.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
			t.Fatalf("login = %d", s)
		}
		paid := map[string]any{"adventure_slug": "demo", "max_players": 20}
		if s := c.do("POST", "/api/admin/purchases", paid, nil); s != 503 {
			t.Fatalf("paid default = %d, want 503", s)
		}
		paid["provider"] = "simulated"
		if s := c.do("POST", "/api/admin/purchases", paid, nil); s != 403 {
			t.Fatalf("paid simulated = %d, want 403", s)
		}
		// A buy at or below the free threshold skips the provider, is paid
		// at once and hands back its assignment.
		var free struct {
			AmountCents  int    `json:"amount_cents"`
			State        string `json:"state"`
			Provider     string `json:"provider"`
			AssignmentID int64  `json:"assignment_id"`
		}
		if s := c.do("POST", "/api/admin/purchases", map[string]any{"adventure_slug": "demo", "max_players": 15}, &free); s != 201 {
			t.Fatalf("free = %d, want 201", s)
		}
		if free.AmountCents != 0 || free.Provider != "free" || free.State != "paid" || free.AssignmentID == 0 {
			t.Fatalf("free purchase = %+v, want 0 cents, provider free, paid, with an assignment", free)
		}
		// MCP pins provider "simulated"; a free buy must still go through.
		if s := c.do("POST", "/api/admin/purchases", map[string]any{"adventure_slug": "demo", "max_players": 15, "provider": "simulated"}, nil); s != 201 {
			t.Fatalf("free via simulated = %d, want 201", s)
		}

		var id int64
		if err := db.QueryRow(`
			INSERT INTO purchases(org_id, adventure_slug, max_players, amount_cents, currency, provider, state)
			VALUES ($1, 'demo', 20, 2500, 'EUR', 'stripe', 'pending') RETURNING id
		`, orgID).Scan(&id); err != nil {
			t.Fatalf("seed pending: %v", err)
		}
		if s := c.do("POST", "/api/admin/purchases/"+itoa(id)+"/confirm", map[string]string{}, nil); s != 403 {
			t.Fatalf("buyer confirm = %d, want 403", s)
		}
	})

	t.Run("stripe webhook disabled without creds", func(t *testing.T) {
		// No Stripe creds → handler should refuse with 503 before even
		// attempting signature verification.
		if status, _ := postWebhook(t, ts, []byte(`{}`), ""); status != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", status)
		}
	})

	t.Run("requires email verified", func(t *testing.T) {
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
	})
}

// TestConfirmPurchase_Simulated runs the simulated provider's
// click-through on one server; each subtest buys its own purchase.
func TestConfirmPurchase_Simulated(t *testing.T) {
	ts, _, _, cleanup := seedPurchaseStack(t, simulatedPaymentsConfig)
	defer cleanup()

	t.Run("flow", func(t *testing.T) {
		c := newClient(t, ts)
		if s := c.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
			t.Fatalf("login = %d", s)
		}

		var created struct {
			ID          int64  `json:"id"`
			AmountCents int    `json:"amount_cents"`
			State       string `json:"state"`
			Provider    string `json:"provider"`
			CheckoutURL string `json:"checkout_url"`
		}
		// 20 players triggers the simulated /confirm path; ≤15 would
		// short-circuit to the free path with no checkout step.
		// 15 free + 5 chargeable @ 4900 öre each = 24500.
		body := map[string]any{"adventure_slug": "demo", "max_players": 20}
		if s := c.do("POST", "/api/admin/purchases", body, &created); s != 201 {
			t.Fatalf("create = %d", s)
		}
		if created.ID == 0 {
			t.Fatalf("expected purchase id, got %+v", created)
		}
		if created.AmountCents != 5*4900 {
			t.Fatalf("amount_cents = %d, want 24500", created.AmountCents)
		}
		if created.Provider != "simulated" {
			t.Fatalf("provider = %q, want simulated (no Stripe creds set)", created.Provider)
		}
		if created.State != "pending" {
			t.Fatalf("state = %q, want pending", created.State)
		}
		if created.CheckoutURL == "" {
			t.Fatalf("missing checkout_url")
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
			AssignmentID int64  `json:"assignment_id"`
			State        string `json:"state"`
		}
		if s := c.do("POST", "/api/admin/purchases/"+itoa(created.ID)+"/confirm", map[string]string{}, &second); s != 200 {
			t.Fatalf("repeat confirm = %d", s)
		}
		if second.AssignmentID != confirmed.AssignmentID {
			t.Fatalf("repeat confirm minted a new assignment: %d vs %d", second.AssignmentID, confirmed.AssignmentID)
		}
		if second.State != "paid" {
			t.Fatalf("state = %q after replay, want paid", second.State)
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

		// The assignment is visible via the existing assignments endpoint.
		var assigns struct {
			Assignments []struct {
				ID            int64  `json:"id"`
				AdventureSlug string `json:"adventure_slug"`
				MaxPlayers    int    `json:"max_players"`
			} `json:"assignments"`
		}
		if s := c.do("GET", "/api/admin/assignments", nil, &assigns); s != 200 {
			t.Fatalf("assignments = %d", s)
		}
		var hit bool
		for _, a := range assigns.Assignments {
			if a.ID == confirmed.AssignmentID {
				hit = true
				if a.AdventureSlug != "demo" || a.MaxPlayers != 20 {
					t.Errorf("assignment fields wrong: %+v", a)
				}
			}
		}
		if !hit {
			t.Fatalf("fresh assignment not listed")
		}
	})

	t.Run("cross-org forbidden", func(t *testing.T) {
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
	})
}
