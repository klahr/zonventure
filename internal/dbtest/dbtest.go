// Package dbtest spins up isolated Postgres databases for tests. Each
// call to Open clones a fresh database off a migrated template and
// returns a *sql.DB plus a cleanup that drops the database when the
// test ends.
//
// The template is built once per migration set (see template below).
// Running all 100+ migrations for every test used to dominate the
// suite's runtime; a template clone is a file copy.
//
// The admin DSN is read from $ZV_TEST_DSN (or a dev default). Tests
// that touch the DB Skip when no DSN is reachable so a bare
// `go test ./...` on a workstation without Postgres still passes —
// the DB-bound tests just get marked as skipped.
//
// That convenience is a liability in CI. Roughly a quarter of this
// suite is DB-bound (201 of 736 tests at the time of writing), so a
// misconfigured Postgres service container would produce a green build
// that tested almost nothing — silently, and exactly when it matters
// most. When $CI is set, an unreachable database is therefore a
// failure, not a skip. See unreachable below.
package dbtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/klahr/zonventure/internal/db"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// defaultAdminDSN points at a local socket-auth Postgres in the
// "postgres" maintenance database. Override via $ZV_TEST_DSN to use a
// different cluster (e.g. CI's `postgres://postgres:postgres@localhost`).
const defaultAdminDSN = "postgres:///postgres?host=/var/run/postgresql"

// unreachable reports a database we could not reach. On a developer
// workstation that is a skip, so `go test ./...` stays useful without
// Postgres installed. Under CI it is a fatal error: a green run that
// skipped its DB-bound tests is worse than a red one, because it looks
// like coverage it does not have.
func unreachable(t *testing.T, op, dsn string, err error) {
	t.Helper()
	const hint = "set ZV_TEST_DSN to a postgres admin DSN with CREATEDB"
	if os.Getenv("CI") != "" {
		t.Fatalf("dbtest: %s %q: %v (%s; skipping is disabled under CI)", op, dsn, err, hint)
	}
	t.Skipf("dbtest: %s %q: %v (%s)", op, dsn, err, hint)
}

// Open returns a *sql.DB pointing at a brand-new test database. The
// caller MUST call the returned cleanup; otherwise the database lingers.
// Skips the test if no Postgres is reachable — except under CI, where
// an unreachable database fails the run. See unreachable.
func Open(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	adminDSN := os.Getenv("ZV_TEST_DSN")
	if adminDSN == "" {
		adminDSN = defaultAdminDSN
	}

	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		unreachable(t, "open", adminDSN, err)
	}
	if err := admin.Ping(); err != nil {
		admin.Close()
		unreachable(t, "ping", adminDSN, err)
	}

	tmpl, err := template(admin, adminDSN)
	if err != nil {
		admin.Close()
		t.Fatalf("dbtest: template: %v", err)
	}

	name := "zv_test_" + randHex(8)
	if _, err := admin.ExecContext(context.Background(), "CREATE DATABASE "+name+" TEMPLATE "+tmpl); err != nil {
		admin.Close()
		t.Fatalf("dbtest: create %s: %v", name, err)
	}

	sqlDB, err := db.Open(withDatabase(adminDSN, name))
	if err != nil {
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE "+name)
		admin.Close()
		t.Fatalf("dbtest: open %s: %v", name, err)
	}

	return sqlDB, func() {
		sqlDB.Close()
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE "+name)
		admin.Close()
	}
}

// templateLockKey serialises template builds across the test processes
// `go test ./...` runs in parallel, one per package.
const templateLockKey int64 = 0x7a76_746d_706c_6174 // "zvtmplat"

var (
	templateOnce sync.Once
	templateName string
	templateErr  error
)

// template returns the name of a fully migrated database to clone test
// databases from, building it on first use. It is named after the
// migrations fingerprint, so it survives across packages and runs and
// is rebuilt only when a migration changes. Templates for other
// fingerprints are dropped as stale on a rebuild.
//
// The build goes to a scratch name and is renamed into place, so a run
// killed mid-migration never leaves a half-built template behind.
func template(admin *sql.DB, adminDSN string) (string, error) {
	templateOnce.Do(func() {
		templateName, templateErr = buildTemplate(admin, adminDSN)
	})
	return templateName, templateErr
}

func buildTemplate(admin *sql.DB, adminDSN string) (string, error) {
	fp, err := db.MigrationsFingerprint()
	if err != nil {
		return "", err
	}
	name := "zv_tmpl_" + fp

	ctx := context.Background()
	conn, err := admin.Conn(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, templateLockKey); err != nil {
		return "", fmt.Errorf("lock: %w", err)
	}
	defer conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, templateLockKey)

	var exists bool
	if err := conn.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`, name,
	).Scan(&exists); err != nil {
		return "", err
	}
	if exists {
		return name, nil
	}

	// Stale templates and scratch builds from earlier runs. Best effort:
	// one still in use by another checkout's run is left for next time.
	rows, err := conn.QueryContext(ctx,
		`SELECT datname FROM pg_database WHERE datname LIKE 'zv\_tmpl\_%'`)
	if err != nil {
		return "", err
	}
	var stale []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return "", err
		}
		stale = append(stale, s)
	}
	rows.Close()
	for _, s := range stale {
		_, _ = conn.ExecContext(ctx, "DROP DATABASE IF EXISTS "+s)
	}

	build := name + "_build"
	if _, err := conn.ExecContext(ctx, "CREATE DATABASE "+build); err != nil {
		return "", fmt.Errorf("create %s: %w", build, err)
	}
	built, err := db.Open(withDatabase(adminDSN, build))
	if err != nil {
		_, _ = conn.ExecContext(ctx, "DROP DATABASE "+build)
		return "", fmt.Errorf("migrate %s: %w", build, err)
	}
	built.Close()
	if _, err := conn.ExecContext(ctx, "ALTER DATABASE "+build+" RENAME TO "+name); err != nil {
		return "", fmt.Errorf("rename %s: %w", build, err)
	}
	return name, nil
}

// withDatabase rewrites the database name path segment of a libpq URL.
// Handles "postgres:///<db>?..." and "postgres://host/<db>" forms; falls
// back to appending "/<name>" for everything else.
func withDatabase(dsn, name string) string {
	q := ""
	if i := strings.Index(dsn, "?"); i >= 0 {
		q = dsn[i:]
		dsn = dsn[:i]
	}
	if j := strings.LastIndex(dsn, "/"); j >= 0 {
		return dsn[:j+1] + name + q
	}
	return dsn + "/" + name + q
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand really should not fail; fall back to a fixed-but-unique
		// fingerprint of the address so tests still produce unique names.
		return fmt.Sprintf("%x", &b)
	}
	return hex.EncodeToString(b)
}

// PublisherOrg returns the id of the catalog publisher org, creating
// one ("Catalog") on first call. Fixtures that stand in for platform
// catalog adventures give them this org as owner.
func PublisherOrg(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`
		INSERT INTO organizations(name, is_publisher) VALUES ('Catalog', TRUE)
		ON CONFLICT(name) DO UPDATE SET is_publisher = TRUE
		RETURNING id
	`).Scan(&id); err != nil {
		t.Fatalf("publisher org: %v", err)
	}
	return id
}
