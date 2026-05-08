package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// pingTimeout caps the boot-time DB handshake. A misconfigured DSN
// that accepts TCP but never finishes the libpq handshake (firewall
// dropping the SSL negotiation, pgbouncer in a bad state) would
// otherwise hang Open() forever with no log output.
const pingTimeout = 5 * time.Second

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Opts tunes the connection pool. Zero / negative values fall back to
// the defaults below — chosen so a fresh self-host operator gets sane
// behaviour without reading the docs, while big deployments can dial
// the knobs from cmd/server flags.
//
// Defaults:
//   - MaxOpenConns:    25  (Postgres' default max_connections is 100;
//                          25 leaves ample headroom for psql sessions,
//                          backups, and `docker compose` peers)
//   - MaxIdleConns:    5   (database/sql defaults to 2, which is too
//                          stingy for the WS hub + scheduler + retention
//                          + handler concurrency we have)
//   - ConnMaxLifetime: 1h  (cycles connections so a long-running process
//                          eventually picks up DNS / TLS / Postgres
//                          rollovers without an operator restart)
//   - ConnMaxIdleTime: 30m (drops long-idle connections so pgbouncer or
//                          a reverse proxy in front of the cluster
//                          doesn't hold them open forever)
type Opts struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// Open opens a Postgres database with the default pool tuning and runs
// any embedded migrations that have not yet been applied. Equivalent to
// OpenWithOpts(dsn, Opts{}). Most callers (tests, backup CLI, default
// boot path) want this.
func Open(dsn string) (*sql.DB, error) {
	return OpenWithOpts(dsn, Opts{})
}

// OpenWithOpts is Open plus operator-tunable pool sizing. The dsn is
// either a libpq-style URL ("postgres://user:pass@host/db") or the kv
// form pgx accepts ("host=/var/run/postgresql dbname=zv").
func OpenWithOpts(dsn string, o Opts) (*sql.DB, error) {
	d, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if o.MaxOpenConns <= 0 {
		o.MaxOpenConns = 25
	}
	if o.MaxIdleConns <= 0 {
		o.MaxIdleConns = 5
	}
	if o.ConnMaxLifetime <= 0 {
		o.ConnMaxLifetime = time.Hour
	}
	if o.ConnMaxIdleTime <= 0 {
		o.ConnMaxIdleTime = 30 * time.Minute
	}
	d.SetMaxOpenConns(o.MaxOpenConns)
	d.SetMaxIdleConns(o.MaxIdleConns)
	d.SetConnMaxLifetime(o.ConnMaxLifetime)
	d.SetConnMaxIdleTime(o.ConnMaxIdleTime)
	pingCtx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	if err := d.PingContext(pingCtx); err != nil {
		d.Close()
		return nil, fmt.Errorf("postgres ping (timeout %s): %w", pingTimeout, err)
	}
	if err := migrate(d); err != nil {
		d.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

// allowedDuplicatePrefixes records pre-existing duplicate-prefix
// migrations that have already been applied to deployed databases.
// Renaming them now would force a replay on those environments, so
// the safer fix is to grandfather them in here and prevent NEW
// duplicates via checkUniquePrefixes.
var allowedDuplicatePrefixes = map[string]bool{
	// 0033_drop_adventure_template.sql + 0033_max_teams_to_max_players.sql
	// — both shipped in the same release; lex-sort puts drop first.
	"0033": true,
}

// checkUniquePrefixes refuses to boot if two migration files share the
// same 4-digit prefix that wasn't explicitly grandfathered. The
// migrations are sorted lexicographically, which means a future
// `0042_foo.sql` + `0042_bar.sql` pair would silently swap order if
// either name were renamed. Failing fast at startup is much cheaper
// than debugging a half-applied schema in production.
func checkUniquePrefixes(names []string) error {
	seen := make(map[string]string, len(names))
	for _, n := range names {
		if len(n) < 4 {
			continue
		}
		prefix := n[:4]
		if other, dup := seen[prefix]; dup && !allowedDuplicatePrefixes[prefix] {
			return fmt.Errorf("migrations: duplicate prefix %q on %s and %s — renumber one before adding to the embed",
				prefix, other, n)
		}
		seen[prefix] = n
	}
	return nil
}

// migrationLockKey is a stable arbitrary 64-bit value used with
// pg_advisory_lock so two booting processes never run the migration
// loop concurrently. Pick once and never change — changing it would
// allow two old + new processes to both acquire their own locks
// during a rolling upgrade and step on each other.
const migrationLockKey int64 = 0x7a76_6d69_6772_6174 // "zvmigrat"

func migrate(d *sql.DB) error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	if err := checkUniquePrefixes(names); err != nil {
		return err
	}

	// Take the cluster-wide migration lock so concurrent boots (rolling
	// deploy, accidental double-start, dbtest spinning up several
	// processes) serialise here. The lock is released when the
	// connection closes; we're holding the parent *sql.DB so it survives
	// the scope but goes away naturally on shutdown. pg_advisory_lock
	// blocks; the second process simply waits.
	if _, err := d.Exec(`SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		// Best-effort release. The connection may already be torn down
		// if migrate() failed in the middle; in that case the lock is
		// gone anyway because the session is gone.
		_, _ = d.Exec(`SELECT pg_advisory_unlock($1)`, migrationLockKey)
	}()

	// Load every applied migration in one round-trip rather than
	// SELECT-COUNT'ing per file. With 50+ migrations and a healthy
	// schema_migrations row, the per-file probe was 50+ sequential
	// round-trips on every boot.
	applied, err := loadAppliedMigrations(d)
	if err != nil {
		return err
	}

	for _, name := range names {
		if applied[name] {
			continue
		}
		content, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(name) VALUES ($1)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// loadAppliedMigrations returns the set of migration names already
// recorded in schema_migrations. One round trip; the set fits
// comfortably in memory (a few KB at most).
func loadAppliedMigrations(d *sql.DB) (map[string]bool, error) {
	rows, err := d.Query(`SELECT name FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool, 64)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}
