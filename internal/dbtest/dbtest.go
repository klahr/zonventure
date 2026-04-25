// Package dbtest spins up isolated Postgres databases for tests. Each
// call to Open creates a fresh database off the admin DSN, runs the
// embedded migrations, and returns a *sql.DB plus a cleanup that drops
// the database when the test ends.
//
// The admin DSN is read from $ZV_TEST_DSN (or a dev default). Tests
// that touch the DB Skip when no DSN is reachable so a bare
// `go test ./...` on a workstation without Postgres still passes —
// the DB-bound tests just get marked as skipped.
package dbtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"git.sr.ht/~klahr/zv/internal/db"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// defaultAdminDSN points at a local socket-auth Postgres in the
// "postgres" maintenance database. Override via $ZV_TEST_DSN to use a
// different cluster (e.g. CI's `postgres://postgres:postgres@localhost`).
const defaultAdminDSN = "postgres:///postgres?host=/var/run/postgresql"

// Open returns a *sql.DB pointing at a brand-new test database. The
// caller MUST call the returned cleanup; otherwise the database lingers.
// Skips the test if no Postgres is reachable.
func Open(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	adminDSN := os.Getenv("ZV_TEST_DSN")
	if adminDSN == "" {
		adminDSN = defaultAdminDSN
	}

	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Skipf("dbtest: open %q: %v (set ZV_TEST_DSN to a postgres admin DSN with CREATEDB)", adminDSN, err)
	}
	if err := admin.Ping(); err != nil {
		admin.Close()
		t.Skipf("dbtest: ping %q: %v (set ZV_TEST_DSN to a postgres admin DSN with CREATEDB)", adminDSN, err)
	}

	name := "zv_test_" + randHex(8)
	if _, err := admin.ExecContext(context.Background(), "CREATE DATABASE "+name); err != nil {
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
