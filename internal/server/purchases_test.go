package server_test

import (
	"database/sql"
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
