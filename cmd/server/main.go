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

	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/components"
	"github.com/klahr/zonventure/internal/db"
	"github.com/klahr/zonventure/internal/effects"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/mailer"
	"github.com/klahr/zonventure/internal/metrics"
	"github.com/klahr/zonventure/internal/retention"
	"github.com/klahr/zonventure/internal/scheduler"
	"github.com/klahr/zonventure/internal/server"
	"github.com/klahr/zonventure/internal/webhooks"
	"github.com/klahr/zonventure/internal/ws"
)

// version is the build identifier surfaced on /api/version. Default
// "dev"; production builds override it via
//
//	go build -ldflags="-X main.version=$(git describe --always --dirty)"
//
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
		seedPublisher     = flag.String("seed-publisher", "", "if set, create/update an org_admin as username:password in the catalog publisher org")
		seedDemoAdvFlag   = flag.Bool("seed-demo-adventure", false, "install the demo adventure (puzzles + placeholder images)")
		seedDemoFlag      = flag.Bool("seed-demo", false, "install a full demo dataset and exit")
		revokeFlag        = flag.String("revoke-sessions", "", `if set, drop staff sessions and exit. Pass a username to scope, or "all" to nuke every session+slot session`)
		backupFlag        = flag.Bool("backup", false, "stream a pg_dump of the configured database to stdout and exit; redirect to a file (e.g. `zv -backup > zv.sql`)")
		restoreFlag       = flag.Bool("restore", false, "read a pg_dump from stdin and replay it into the configured database; pair with -backup for round-trips")
		exportOrgFlag     = flag.Int64("export-org", 0, "stream a self-host → hosted migration bundle for the given org id to stdout and exit (e.g. `zv -export-org=1 > bundle.zip`)")
		importOrgFlag     = flag.Bool("import-org", false, "read an org migration bundle from stdin and replay it into the configured database as a fresh org")
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
	if *exportOrgFlag > 0 {
		if err := runExportOrg(connStr, *exportOrgFlag, os.Stdout); err != nil {
			log.Fatalf("%v", err)
		}
		return
	}
	if *importOrgFlag {
		if err := runImportOrg(connStr, os.Stdin); err != nil {
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
	if *seedPublisher != "" {
		if err := seedPublisherUser(sqlDB, *seedPublisher); err != nil {
			log.Fatalf("seed publisher: %v", err)
		}
		log.Printf("publisher ready in the catalog publisher org")
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
		log.Printf(`demo data installed. Log in at /arrange as admin/admin, boss/s3cret (org_admin) or jane/jane (org_member). Players use per-slot magic links — copy them from Events → Event #1 → Edit team.`)
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
	// Optional ZV_TOTP_KEY: the AES-256-GCM key wrapping
	// users.totp_secret_encrypted. When set we surface MFA in the
	// components panel and accept TOTP enrolments; when missing the
	// enrol endpoints decline cleanly and the login flow stays
	// password-only. Lose-the-key recovery: every enrolled user has
	// to re-enrol, so back the value up alongside DB credentials.
	var totpKey []byte
	if k, kerr := auth.LoadTOTPKey(); kerr == nil {
		totpKey = k
	} else if !errors.Is(kerr, auth.ErrTOTPKeyMissing) {
		log.Fatalf("ZV_TOTP_KEY: %v", kerr)
	}
	// Optional ZV_OIDC_*: when ZV_OIDC_ENABLED=1 we attempt OpenID
	// Connect discovery against the configured issuer at boot. Failure
	// here is fatal so a misconfigured env file fails loudly instead of
	// presenting a half-broken sign-in flow to the first user. Disabled
	// (the default) leaves SSO endpoints 404'd and the login UI hides
	// the SSO button.
	oidcCfg := auth.OIDCConfig{
		Enabled:      envTruthy("ZV_OIDC_ENABLED"),
		Issuer:       os.Getenv("ZV_OIDC_ISSUER"),
		ClientID:     os.Getenv("ZV_OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("ZV_OIDC_CLIENT_SECRET"),
		HostedDomain: os.Getenv("ZV_OIDC_HOSTED_DOMAIN"),
		RedirectURL:  os.Getenv("ZV_OIDC_REDIRECT_URL"),
		OrgName:      os.Getenv("ZV_OIDC_ORG_NAME"),
		ButtonLabel:  os.Getenv("ZV_OIDC_BUTTON_LABEL"),
		ProviderName: os.Getenv("ZV_OIDC_PROVIDER_NAME"),
	}
	oidcClient, err := auth.NewOIDC(context.Background(), oidcCfg)
	if err != nil {
		log.Fatalf("ZV_OIDC_*: %v", err)
	}
	if oidcClient.Enabled() {
		logx.Info("auth.oidc.ready", "issuer", oidcCfg.Issuer, "hosted_domain", oidcCfg.HostedDomain)
	}
	// Pay the one-shot decoy-hash construction cost before traffic
	// lands so the very first /api/auth/login on an unknown user
	// already finds the sentinel ready. Otherwise that request pays
	// hashSecret + verify (~100ms) while subsequent ones pay only
	// verify (~50ms), re-opening the user-enumeration timing oracle
	// VerifyPassword's user-not-found branch is meant to close.
	auth.PrimeDecoyHash()
	// ZV_TRUSTED_PROXIES is a comma-separated CIDR/IP list naming the
	// reverse proxies whose X-Forwarded-* headers we honour. Empty
	// (the default) means "trust nothing" — auth.ClientIP and the
	// trustedProxyRealIP middleware ignore forwarded headers, which
	// is the correct posture for a directly-reachable self-host. An
	// operator behind nginx/Caddy/Cloudflare-Tunnel sets this to
	// their internal subnet (e.g. "127.0.0.1,::1" for sidecar mode,
	// "10.0.0.0/8" for a private VPC).
	if spec := os.Getenv("ZV_TRUSTED_PROXIES"); spec != "" {
		prefixes, err := auth.ParseTrustedProxies(spec)
		if err != nil {
			log.Fatalf("ZV_TRUSTED_PROXIES: %v", err)
		}
		auth.SetTrustedProxies(prefixes)
		logx.Info("auth.trusted_proxies", "count", len(prefixes))
	}
	h := server.New(server.Config{
		DB:           sqlDB,
		Library:      lib,
		Hub:          hub,
		SecureCookie: *secureCookie,
		Mailer:       mail,
		PublicURL:    *publicURL,
		Components:   describeComponents(mail, os.Getenv("ZV_STRIPE_SECRET_KEY"), os.Getenv("ZV_STRIPE_WEBHOOK_SECRET"), os.Getenv("ZV_METRICS_ADDR"), os.Getenv("ZV_PAYMENTS_PROVIDER"), totpKey != nil),
		TOTPKey:      totpKey,
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
		// PaymentsProvider lets the operator override which
		// payments.Provider impl gets wired. Empty = auto (stripe
		// when keys present, else nil); "simulated" picks the
		// dev-only no-money impl that auto-confirms checkouts in-
		// process. Useful for CI / local dev where you want to
		// walk a buy flow without real Stripe credentials.
		PaymentsProvider: os.Getenv("ZV_PAYMENTS_PROVIDER"),
		Env:              os.Getenv("ZV_ENV"),
		Version:          version,
		WebOverrideDir:   os.Getenv("ZV_WEB_OVERRIDE_DIR"),
		ChangelogURL:     os.Getenv("ZV_CHANGELOG_URL"),
		OIDC:             oidcClient,
	})

	srv := &http.Server{
		Addr:    *addr,
		Handler: h,
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

	// Graph "Call a webhook" and "Send an email" steps, queued with
	// the team's move in graph_effect_outbox and sent from here.
	effectsDone := effects.Start(ctx, sqlDB, mail, *publicURL)

	go func() {
		logx.Info("http.listening", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	}()

	<-ctx.Done()
	logx.Info("http.shutdown")
	// Notify live WebSocket clients before tearing the listener down.
	// srv.Shutdown only drains in-flight HTTP — hijacked WS upgrades
	// are invisible to it, so without this announce the client just
	// sees the TCP connection drop on process exit. The hub fans the
	// frame out to every topic on a best-effort basis (drop-oldest
	// backpressure if a subscriber is slow); we sleep briefly so the
	// per-connection write goroutine has time to flush before the
	// listener stops accepting.
	hub.PublishAll("server_closing", map[string]string{
		"reason": "shutdown",
	})
	time.Sleep(250 * time.Millisecond)
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
	// Stopped by the same cancel, so this rarely waits: a call cut off
	// mid-flight keeps its lease and is sent again after it lapses.
	select {
	case <-effectsDone:
	case <-time.After(35 * time.Second):
		logx.Warn("effects.shutdown.timeout")
	}
}

// describeComponents builds the snapshot of pluggable seams the
// admin "Installed components" panel renders. The values come from
// the same configuration mailer.FromEnv etc. read, so the panel
// reflects what actually got wired — not what the operator might
// have intended to wire. Add an entry whenever a new seam is
// introduced (e.g. a payments interface, a blob-storage interface);
// keep entries pure descriptive — no side effects, no DB calls.
func describeComponents(m mailer.Mailer, stripeKey, stripeWebhookSecret, metricsAddr, paymentsOverride string, totpEnabled bool) []components.Component {
	mailerImpl, mailerEnabled, mailerNotes := "log", false, "set $ZV_SMTP_HOST to send real mail"
	if _, ok := m.(*mailer.SMTPMailer); ok {
		mailerImpl, mailerEnabled, mailerNotes = "smtp", true, ""
	}
	// Payments component: the runtime decision tree mirrors what
	// server.New does — explicit ZV_PAYMENTS_PROVIDER wins, else
	// auto-discover stripe when both creds are present, else fall
	// back to the in-app simulated/free/invoice flow.
	stripeOK := stripeKey != "" && stripeWebhookSecret != ""
	paymentsImpl, paymentsEnabled, paymentsNotes := "simulated-app", true, "set $ZV_STRIPE_SECRET_KEY and $ZV_STRIPE_WEBHOOK_SECRET to enable Stripe"
	switch strings.ToLower(strings.TrimSpace(paymentsOverride)) {
	case "simulated":
		paymentsImpl, paymentsEnabled, paymentsNotes = "simulated", true, "DEV ONLY — auto-confirms checkouts in-process; no money moves"
	case "stripe", "":
		if stripeOK {
			paymentsImpl, paymentsEnabled, paymentsNotes = "stripe", true, ""
		}
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
		{Name: "mfa", Impl: "totp", Enabled: totpEnabled, Notes: mfaNotes(totpEnabled)},
		{Name: "webhooks", Impl: "signed-post", Enabled: true},
		{Name: "roster-importers", Impl: "json, csv", Enabled: true},
	}
}

// mfaNotes is the operator-facing hint for the MFA registry row —
// short, with the env-var name when missing so the fix is obvious
// from the Settings page.
func mfaNotes(enabled bool) string {
	if enabled {
		return "RFC 6238 TOTP, opt-in per user"
	}
	return "set $ZV_TOTP_KEY (32 bytes base64) to enable TOTP enrolment"
}

// seedAdminIfMissing is the env-var-friendly twin of seedAdminUser:
// instead of upserting unconditionally, it only creates the admin when
// no global admin exists yet. Lets ZV_SEED_ADMIN sit in a Docker
// compose file forever without ever clobbering a real password an
// operator set after the first login.
func seedAdminIfMissing(d *sql.DB, creds string) error {
	var n int
	if err := d.QueryRow(
		`SELECT count(*) FROM users WHERE is_admin = TRUE`,
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
//
// Stamps password_is_default=1 when the seeded password is one of the
// well-known docs/defaults ("admin", "rootpw"). Login + whoami surface
// the flag to the UI as a loud banner so an operator who skipped the
// "change me immediately" step in SELFHOSTING.md doesn't expose a
// takeover-trivial admin to the public internet.
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
	isDefault := 0
	if password == "admin" || password == "rootpw" {
		isDefault = 1
	}
	_, err = d.Exec(`
		INSERT INTO users(username, password_hash, is_admin, password_is_default)
		VALUES ($1, $2, TRUE, $3)
		ON CONFLICT(username) DO UPDATE SET
		  password_hash = excluded.password_hash,
		  is_admin = TRUE,
		  password_is_default = excluded.password_is_default
	`, username, hash, isDefault)
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
