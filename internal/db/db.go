package db

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

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
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, err
	}
	if err := migrate(d); err != nil {
		d.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

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

	for _, name := range names {
		var applied int
		if err := d.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = $1`, name).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
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
