// Command server is the zv adventure backend.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/components"
	"git.sr.ht/~klahr/zv/internal/db"
	"git.sr.ht/~klahr/zv/internal/logx"
	"git.sr.ht/~klahr/zv/internal/mailer"
	"git.sr.ht/~klahr/zv/internal/metrics"
	"git.sr.ht/~klahr/zv/internal/retention"
	"git.sr.ht/~klahr/zv/internal/scheduler"
	"git.sr.ht/~klahr/zv/internal/server"
	"git.sr.ht/~klahr/zv/internal/webhooks"
	"git.sr.ht/~klahr/zv/internal/ws"
)

// version is the build identifier surfaced on /api/version. Default
// "dev"; production builds override it via
//   go build -ldflags="-X main.version=$(git describe --always --dirty)"
// The accompanying commit / build time / dirty flag come from
// runtime/debug.ReadBuildInfo and don't need ldflags.
var version = "dev"

// envIntDefault returns the integer value of the named env var, or the
// fallback when the var is unset or unparseable. Lets us share env-vs-flag
// configuration logic for retention thresholds the same way ZV_PUBLIC_URL
// already does.
func envIntDefault(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// envTruthy reads a boolean-flavoured env var. Anything in the standard
// "yes/true/on" set (case-insensitive, trimmed) flips it on; everything
// else — empty, "0", "false", garbage — stays off. Matches the
// docker-compose / systemd convention where ZV_FLAG=1 is the obvious
// way to switch a behaviour on.
func envTruthy(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func main() {
	var (
		addr              = flag.String("addr", ":8080", "listen address")
		dsn               = flag.String("dsn", "", "postgres DSN (libpq URL or key=val); falls back to $DATABASE_URL")
		secureCookie      = flag.Bool("secure-cookie", false, "force Secure flag on cookies even when the request looks plain HTTP; auto-detects from r.TLS / X-Forwarded-Proto otherwise")
		publicURL         = flag.String("public-url", os.Getenv("ZV_PUBLIC_URL"), "externally-reachable origin (e.g. https://zv.example) used to build links in outbound emails")
		sessionTTL        = flag.Duration("session-ttl", auth.SessionTTL, "how long a staff login stays valid (e.g. 24h, 168h, 720h); newly-set sessions only")
		logLevel          = flag.String("log-level", "INFO", "initial log level (DEBUG|INFO|WARN|ERROR)")
		seedAdmin         = flag.String("seed-admin", "", "if set, create/update a global admin as username:password")
		seedArranger      = flag.String("seed-arranger", "", "if set, create/update an org_admin arranger as username:password in the default Arrangers org")
		seedDemoAdvFlag   = flag.Bool("seed-demo-adventure", false, "install the demo adventure (puzzles + placeholder images)")
		seedDemoFlag      = flag.Bool("seed-demo", false, "install a full demo dataset and exit")
		revokeFlag        = flag.String("revoke-sessions", "", `if set, drop staff sessions and exit. Pass a username to scope, or "all" to nuke every session+slot session`)
		backupFlag        = flag.Bool("backup", false, "stream a pg_dump of the configured database to stdout and exit; redirect to a file (e.g. `zv -backup > zv.sql`)")
		restoreFlag       = flag.Bool("restore", false, "read a pg_dump from stdin and replay it into the configured database; pair with -backup for round-trips")
		retentionLocation = flag.Int("retention-location-days", envIntDefault("ZV_LOCATION_RETENTION_DAYS", 30), "delete player location pings + answer attempts this many days after the event completes (0 disables)")
		retentionAudit    = flag.Int("retention-audit-days", envIntDefault("ZV_AUDIT_RETENTION_DAYS", 90), "delete auth_events rows this many days after they were emitted (0 disables)")
		retentionInterval = flag.Duration("retention-interval", time.Hour, "how often the retention sweeper runs")
		dbMaxOpenConns    = flag.Int("db-max-open-conns", envIntDefault("ZV_DB_MAX_OPEN_CONNS", 0), "Postgres pool: max open connections (0 = default 25). Bound by the cluster's max_connections")
		dbMaxIdleConns    = flag.Int("db-max-idle-conns", envIntDefault("ZV_DB_MAX_IDLE_CONNS", 0), "Postgres pool: max idle connections kept warm (0 = default 5)")
	)
	flag.Parse()
	logx.SetLevel(logx.ParseLevel(*logLevel))
	auth.SetSessionTTL(*sessionTTL)

	connStr := *dsn
	if connStr == "" {
		connStr = os.Getenv("DATABASE_URL")
	}
	if connStr == "" {
		log.Fatalf("no postgres DSN: pass -dsn or set DATABASE_URL")
	}

	// Backup / restore run before db.Open so the app's connection pool
	// and migration logic don't fight with the dump or replay. Both are
	// terminal — the process exits as soon as the operation finishes.
	if *backupFlag {
		if err := runBackup(connStr, os.Stdout); err != nil {
			log.Fatalf("%v", err)
		}
		return
	}
	if *restoreFlag {
		if err := runRestore(connStr, os.Stdin); err != nil {
			log.Fatalf("%v", err)
		}
		return
	}

	sqlDB, err := db.OpenWithOpts(connStr, db.Opts{
		MaxOpenConns: *dbMaxOpenConns,
		MaxIdleConns: *dbMaxIdleConns,
	})
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	// Seeds run in a fixed order (admin → arranger → adventure → full demo)
	// so `make seed` can combine flags in one invocation. If any seed
	// flag is set the server exits without starting the listener.
	seeded := false
	if *seedAdmin != "" {
		if err := seedAdminUser(sqlDB, *seedAdmin); err != nil {
			log.Fatalf("seed admin: %v", err)
		}
		log.Printf("admin user ready")
		seeded = true
	}
	if *seedArranger != "" {
		if err := seedArrangerUser(sqlDB, *seedArranger); err != nil {
			log.Fatalf("seed arranger: %v", err)
		}
		log.Printf("arranger ready in org Arrangers")
		seeded = true
	}
	if *seedDemoAdvFlag {
		if err := seedDemoAdventure(sqlDB); err != nil {
			log.Fatalf("seed demo adventure: %v", err)
		}
		log.Printf("demo adventure ready")
		seeded = true
	}
	if *seedDemoFlag {
		if err := seedDemo(context.Background(), sqlDB); err != nil {
			log.Fatalf("seed demo: %v", err)
		}
		log.Printf(`demo data installed. Log in at /arrange as admin/admin, boss/s3cret (org_admin) or jane/jane (org_member). Players use per-slot magic links — copy them from Runs → Run #1 → Edit team.`)
		seeded = true
	}
	if *revokeFlag != "" {
		n, err := revokeSessions(sqlDB, *revokeFlag)
		if err != nil {
			log.Fatalf("revoke sessions: %v", err)
		}
		log.Printf("revoked %d session row(s) for target %q", n, *revokeFlag)
		seeded = true
	}
	if seeded {
		return
	}

	// Optional first-run admin seed driven by an env var. Lets the
	// canonical Docker setup boot a usable system from `docker compose
	// up -d` without a follow-up `-seed-admin` command. Idempotent: if
	// any global admin already exists this is a no-op, so the env var
	// is safe to leave in compose.yaml across restarts.
	if v := os.Getenv("ZV_SEED_ADMIN"); v != "" {
		if err := seedAdminIfMissing(sqlDB, v); err != nil {
			log.Fatalf("seed admin (env): %v", err)
		}
	}

	lib := adventure.New(sqlDB)
	logx.Info("adventure.library.loaded", "count", len(lib.List()))

	hub := ws.NewHub()
	mail, err := mailer.FromEnv()
	if err != nil {
		log.Fatalf("mailer config: %v", err)
	}
	h := server.New(server.Config{
		DB:           sqlDB,
		Library:      lib,
		Hub:          hub,
		SecureCookie: *secureCookie,
		Mailer:       mail,
		PublicURL:    *publicURL,
		Components:   describeComponents(mail, os.Getenv("ZV_STRIPE_SECRET_KEY"), os.Getenv("ZV_STRIPE_WEBHOOK_SECRET"), os.Getenv("ZV_METRICS_ADDR")),
		Legal: server.LegalConfig{
			CompanyName:           os.Getenv("ZV_COMPANY_NAME"),
			ContactEmail:          os.Getenv("ZV_CONTACT_EMAIL"),
			DPOEmail:              os.Getenv("ZV_DPO_EMAIL"),
			Jurisdiction:          os.Getenv("ZV_JURISDICTION"),
			LocationRetentionDays: os.Getenv("ZV_LOCATION_RETENTION_DAYS"),
			AuditRetentionDays:    os.Getenv("ZV_AUDIT_RETENTION_DAYS"),
			LastUpdated:           os.Getenv("ZV_LEGAL_LAST_UPDATED"),
			HostingProvider:       os.Getenv("ZV_HOSTING_PROVIDER"),
			EmailProvider:         os.Getenv("ZV_EMAIL_PROVIDER"),
			Reviewed:              envTruthy("ZV_LEGAL_REVIEWED"),
		},
		StripeSecretKey:     os.Getenv("ZV_STRIPE_SECRET_KEY"),
		StripeWebhookSecret: os.Getenv("ZV_STRIPE_WEBHOOK_SECRET"),
		Version:             version,
		WebOverrideDir:      os.Getenv("ZV_WEB_OVERRIDE_DIR"),
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           h,
		// Read/write/idle timeouts cap how long the server holds a
		// connection in any one state, so a misbehaving client (slow
		// loris-style attack, hung upload, idle-and-forgotten socket)
		// can't sit on a goroutine indefinitely.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		// 1 MB header cap — well above what any sane client sends,
		// and rejects oversized/malformed headers at the front door.
		MaxHeaderBytes: 1 << 20,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Optional metrics listener. Off by default — self-hosters who don't
	// configure it never expose /metrics, so a misconfigured reverse
	// proxy can't accidentally surface ops data. Bind to 127.0.0.1
	// in production unless you front it with auth.
	if mAddr := os.Getenv("ZV_METRICS_ADDR"); mAddr != "" {
		go metrics.ServeAddr(ctx, mAddr)
	}

	// Sweep for scheduled events every second and auto-start any that
	// are due. A 1s tick is what makes the player + live-view countdowns
	// transition from "scheduled" to "running" within a second of
	// hitting zero — a 15s sweep made the digits sit at "Go!" / 00:00
	// for an awkward up-to-15-second pause. The query is "SELECT … WHERE
	// state = 'scheduled' AND scheduled_at <= now()" against an indexed
	// column, so 60 queries/min on an idle system is trivial DB load.
	schedDone := scheduler.Start(ctx, sqlDB, hub, time.Second)

	// Privacy-retention sweeper: prunes location pings + answer attempts
	// from completed runs and ages out auth_events. Operator-tunable via
	// flags (or ZV_*_RETENTION_DAYS env vars); 0 disables.
	retDone := retention.Start(ctx, sqlDB, retention.Config{
		LocationDays: *retentionLocation,
		AuditDays:    *retentionAudit,
		Interval:     *retentionInterval,
	})

	// Outbound webhook delivery worker. Drains webhook_deliveries
	// rows enqueued by the various Publish call sites and POSTs them
	// to customer endpoints with HMAC signatures + exponential
	// backoff retry.
	webhookDone := webhooks.Start(ctx, sqlDB)

	go func() {
		logx.Info("http.listening", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	}()

	<-ctx.Done()
	logx.Info("http.shutdown")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logx.Error("http.shutdown.failed", "err", err)
	}
	// Wait for the scheduler tick goroutine to fully exit before
	// returning, so an in-flight sweep finishes its UPDATE rather than
	// being cut mid-statement when the process dies. Bounded by the
	// signal context plus a small grace window: if the scheduler is
	// somehow stuck we still exit instead of hanging the operator.
	select {
	case <-schedDone:
	case <-time.After(5 * time.Second):
		logx.Warn("scheduler.shutdown.timeout")
	}
	select {
	case <-retDone:
	case <-time.After(5 * time.Second):
		logx.Warn("retention.shutdown.timeout")
	}
	// Webhooks worker: a 5s grace beyond the scheduler since an
	// in-flight POST can take up to the http.Client.Timeout (30s).
	// Worst case: we drop in-flight deliveries; they remain in
	// status='pending' and the next process restart picks them up.
	select {
	case <-webhookDone:
	case <-time.After(35 * time.Second):
		logx.Warn("webhooks.shutdown.timeout")
	}
}

// describeComponents builds the snapshot of pluggable seams the
// admin "Installed components" panel renders. The values come from
// the same configuration mailer.FromEnv etc. read, so the panel
// reflects what actually got wired — not what the operator might
// have intended to wire. Add an entry whenever a new seam is
// introduced (e.g. a payments interface, a blob-storage interface);
// keep entries pure descriptive — no side effects, no DB calls.
func describeComponents(m mailer.Mailer, stripeKey, stripeWebhookSecret, metricsAddr string) []components.Component {
	mailerImpl, mailerEnabled, mailerNotes := "log", false, "set $ZV_SMTP_HOST to send real mail"
	if _, ok := m.(*mailer.SMTPMailer); ok {
		mailerImpl, mailerEnabled, mailerNotes = "smtp", true, ""
	}
	stripeOK := stripeKey != "" && stripeWebhookSecret != ""
	paymentsImpl, paymentsEnabled, paymentsNotes := "simulated", true, "set $ZV_STRIPE_SECRET_KEY and $ZV_STRIPE_WEBHOOK_SECRET to enable Stripe"
	if stripeOK {
		paymentsImpl, paymentsEnabled, paymentsNotes = "stripe", true, ""
	}
	metricsEnabled := metricsAddr != ""
	metricsNotes := "set $ZV_METRICS_ADDR to expose /metrics"
	if metricsEnabled {
		metricsNotes = "exposed at " + metricsAddr
	}
	return []components.Component{
		{Name: "mailer", Impl: mailerImpl, Enabled: mailerEnabled, Notes: mailerNotes},
		{Name: "payments", Impl: paymentsImpl, Enabled: paymentsEnabled, Notes: paymentsNotes},
		{Name: "metrics", Impl: "prometheus", Enabled: metricsEnabled, Notes: metricsNotes},
		{Name: "storage", Impl: "db-blob", Enabled: true, Notes: "team avatars and page images live in Postgres"},
		{Name: "auth", Impl: "local-password", Enabled: true},
		{Name: "webhooks", Impl: "signed-post", Enabled: true},
		{Name: "roster-importers", Impl: "json, csv", Enabled: true},
	}
}

// seedAdminIfMissing is the env-var-friendly twin of seedAdminUser:
// instead of upserting unconditionally, it only creates the admin when
// no global admin exists yet. Lets ZV_SEED_ADMIN sit in a Docker
// compose file forever without ever clobbering a real password an
// operator set after the first login.
func seedAdminIfMissing(d *sql.DB, creds string) error {
	var n int
	if err := d.QueryRow(
		`SELECT count(*) FROM users WHERE role='admin' AND org_id IS NULL`,
	).Scan(&n); err != nil {
		return fmt.Errorf("count admins: %w", err)
	}
	if n > 0 {
		logx.Info("seed-admin.skipped", "reason", "admin exists")
		return nil
	}
	if err := seedAdminUser(d, creds); err != nil {
		return err
	}
	username, _, _ := strings.Cut(creds, ":")
	logx.Info("seed-admin.created", "username", username)
	return nil
}

// seedAdminUser upserts a global-admin user from a "username:password" flag
// value. Used to bootstrap the very first admin on a fresh database.
func seedAdminUser(d *sql.DB, creds string) error {
	i := strings.IndexByte(creds, ':')
	if i <= 0 || i == len(creds)-1 {
		return errors.New(`expected "username:password"`)
	}
	username, password := creds[:i], creds[i+1:]
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	_, err = d.Exec(`
		INSERT INTO users(username, password_hash, role, org_id) VALUES ($1, $2, 'admin', NULL)
		ON CONFLICT(username) DO UPDATE SET password_hash = excluded.password_hash, role = 'admin', org_id = NULL
	`, username, hash)
	return err
}

// revokeSessions clears server-side session rows so cookie holders are
// forced through the login flow on their next request. target may be a
// username (drops just that user's staff sessions) or the literal "all"
// (drops every staff session AND every slot session — the emergency
// "log everyone out" lever).
//
// Returns the total number of rows deleted across both tables. An
// unknown username yields a non-nil error so the operator notices a
// typo rather than thinking it succeeded silently.
func revokeSessions(d *sql.DB, target string) (int64, error) {
	if target == "all" {
		var total int64
		for _, table := range []string{"sessions", "slot_sessions"} {
			res, err := d.Exec("DELETE FROM " + table)
			if err != nil {
				return total, fmt.Errorf("delete %s: %w", table, err)
			}
			n, _ := res.RowsAffected()
			total += n
		}
		return total, nil
	}
	var userID int64
	if err := d.QueryRow(`SELECT id FROM users WHERE username = $1`, target).Scan(&userID); err != nil {
		return 0, fmt.Errorf("user %q not found: %w", target, err)
	}
	res, err := d.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
