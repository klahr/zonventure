// Package server wires the HTTP router, middleware, and per-package
// handlers into one http.Handler.
package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/components"
	"github.com/klahr/zonventure/internal/domains"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/mailer"
	"github.com/klahr/zonventure/internal/mcp"
	"github.com/klahr/zonventure/internal/metrics"
	"github.com/klahr/zonventure/internal/payments"
	simulatedprovider "github.com/klahr/zonventure/internal/payments/simulated"
	stripeprovider "github.com/klahr/zonventure/internal/payments/stripe"
	"github.com/klahr/zonventure/internal/periodic"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/player"
	"github.com/klahr/zonventure/internal/public"
	"github.com/klahr/zonventure/internal/settings"
	"github.com/klahr/zonventure/internal/storage"
	"github.com/klahr/zonventure/internal/storage/dbblob"
	"github.com/klahr/zonventure/internal/teamimport"
	teamimportcsv "github.com/klahr/zonventure/internal/teamimport/csv"
	teamimportiofxml "github.com/klahr/zonventure/internal/teamimport/iofxml"
	teamimportods "github.com/klahr/zonventure/internal/teamimport/ods"
	teamimportxlsx "github.com/klahr/zonventure/internal/teamimport/xlsx"
	"github.com/klahr/zonventure/internal/updates"
	"github.com/klahr/zonventure/internal/ws"
)

//go:embed web/play-sw.js web/play.html web/live.html web/recap.html web/i18n.js web/favicon.svg web/og-image.png web/privacy.html web/terms.html web/manual.html web/landing.html web/pricing.html web/store.html web/store-event.html web/redeem.html web/topbar.css web/topbar.js web/page-tokens.css web/fsmap.css web/jsqr.js web/404.html web/robots.txt web/sitemap.xml web/locales/web/*.json web/locales/landing/*.json web/assets/maplibre/*.mjs web/assets/maplibre/*.css web/assets/zv-maplibre.js web/assets/illustrations/not-found.webp web/assets/email/*.jpg web/assets/email/covers/*.jpg
var webFS embed.FS

// Config is the bag of knobs New needs.
type Config struct {
	DB           *sql.DB
	Library      *adventure.Library
	Hub          *ws.Hub
	SecureCookie bool
	// Mailer is the transactional email backend. Optional: when nil
	// the server installs a LogMailer fallback so email-bound flows
	// don't crash in tests / dev clones with no SMTP configured.
	Mailer mailer.Mailer
	// PublicURL is the externally-reachable origin (no trailing slash)
	// used to build verification links in outbound mail. When empty
	// the auth handlers fall back to the request's Host header, which
	// works for dev but not when running behind a proxy that doesn't
	// forward Host correctly.
	PublicURL string
	// HomeOrg is the slug of the organisation the site is: /store sends
	// visitors to its page, and the landing page shows its adventures.
	// Empty, /store is not found and the landing page lists none.
	HomeOrg string
	// ShopHost is a hostname of its own for the shop (/shop), such as
	// shop.example.com: it serves the shop at its root. "" for none.
	ShopHost string
	// Legal is the operator-supplied content for /privacy and /terms.
	// Unset fields render "[set ZV_*]" so missing config is obvious
	// to an operator visiting the page in dev.
	Legal LegalConfig
	// StripeSecretKey + StripeWebhookSecret enable provider=stripe on
	// purchases. Both must be set for Stripe to be considered
	// available; either being empty leaves the simulated provider as
	// the only purchase flow.
	StripeSecretKey     string
	StripeWebhookSecret string
	// StripeConnectWebhookSecret signs the Connect endpoint's events:
	// those of organisations' own accounts (their store sales).
	StripeConnectWebhookSecret string
	// PaymentsProvider names which payments.Provider impl to wire.
	// Empty (default) auto-picks: Stripe when both Stripe credentials
	// are set, otherwise nil ("payments not configured"). Setting
	// this to "simulated" picks the dev-only no-money impl that
	// auto-confirms checkouts in-process — never deploy with this
	// in production. Future provider impls (mollie, paddle, …) get
	// added here as new accepted values.
	PaymentsProvider string
	// Env is the deployment-environment label ("dev", "staging",
	// "production"). The only behaviour keyed on it today is the
	// safety gate on PaymentsProvider="simulated", which refuses to
	// wire unless Env is "dev"/"development". An empty Env counts as
	// non-dev, so the gate trips on a default-config self-host.
	Env string
	// Version is the human-readable build identifier surfaced on
	// /api/version. cmd/server injects it via -ldflags at build time;
	// when empty, /api/version reports "dev". The VCS revision and
	// build time come from runtime/debug.ReadBuildInfo so an operator
	// upgrading via `docker compose up -d --build` can confirm exactly
	// which commit is serving, even without a tagged version string.
	Version string
	// Components is the snapshot of pluggable seams wired in at
	// startup. cmd/server builds the slice from its own knowledge of
	// what got configured (Mailer impl, Stripe enabled, metrics
	// listener address, etc.); the admin-only /api/admin/components
	// endpoint just echoes it. nil/empty is fine — the endpoint
	// returns an empty list rather than 500ing.
	Components []components.Component
	// Storage owns the binary asset side of the app (team avatars,
	// adventure cover art today). Optional: when nil the server
	// installs a dbblob.New(DB) fallback so the legacy inline-column
	// behaviour stays the default.
	Storage storage.Blobs
	// WebOverrideDir, when set, points at a directory of HTML
	// templates + a landing-locales.json that shadow the embedded
	// defaults. Lets a self-hosted deployment ship its own marketing
	// copy without forking the binary. Empty = use embedded only.
	WebOverrideDir string
	// TOTPKey is the AES-256-GCM key used to encrypt users.totp_secret_encrypted.
	// cmd/server loads it from ZV_TOTP_KEY at boot via auth.LoadTOTPKey.
	// nil disables the TOTP enrol surface (the login flow short-circuits
	// before any decrypt is attempted, so unenrolled users still log in).
	TOTPKey []byte
	// OIDC is the configured single-sign-on client, built once at boot
	// via auth.NewOIDC. nil disables SSO — /auth/oidc/* endpoints 404
	// and the login UI hides the SSO button. Required env vars on
	// ZV_OIDC_ENABLED=1 are validated in NewOIDC, not here.
	OIDC *auth.OIDC
	// TeamImporters is the registry of team-roster file-format
	// parsers. nil means "use the bundled default set" (CSV, ODS,
	// xlsx, IOF XML); set explicitly to subset, swap implementations,
	// or pass an empty registry to disable the import surface
	// entirely (the endpoint will then return 503 on every upload).
	TeamImporters *teamimport.Registry
	// ChangelogURL is the URL the update checker polls to detect
	// newer releases. Empty falls back to updates.DefaultChangelogURL
	// (the upstream CHANGELOG.md). Set explicitly to pin a fork, or to
	// updates.Off to disable checking (an empty ZV_CHANGELOG_URL).
	ChangelogURL string
	// UpdateContext bounds the lifetime of the changelog-polling
	// goroutine. Pass the same context the rest of the binary uses
	// for cancellation; nil falls back to context.Background() (the
	// loop runs until the process exits).
	UpdateContext context.Context
}

// newSweptLimiter constructs a LoginLimiter and spawns a background
// sweeper that drops entries past their lockout window. Without the
// sweep, a patient attacker rotating fake usernames or IPs would
// grow the in-memory maps unboundedly over the process's lifetime.
// ctx scopes the goroutine to the server's lifetime so a graceful
// shutdown can drain them cleanly; passing context.Background() is
// equivalent to "live until the process exits".
func newSweptLimiter(ctx context.Context) *auth.LoginLimiter {
	l := auth.NewLoginLimiter()
	go l.RunSweeper(ctx, 5*time.Minute)
	return l
}

// New builds the HTTP router.
func New(cfg Config) http.Handler {
	// Apply on-disk template overrides, if configured. Done before any
	// servePage/servePublicPage call below so the closure they capture
	// reflects the override-aware bytes. Errors here are surfaced as
	// startup panics — if the operator pointed us at a bad dir we'd
	// rather fail loudly than serve embedded copy and let them think
	// the override is in effect.
	if err := loadOverrides(cfg.WebOverrideDir); err != nil {
		panic("web overrides: " + err.Error())
	}
	// Render the /i18n.js bytes from the embedded JSON locales (or
	// the override copy of i18n.js, if loadOverrides above pointed
	// at one). landingLocales itself is populated in package
	// init() so the override-merge inside loadOverrides has
	// something to merge into.
	if err := initI18nFromJSON(); err != nil {
		panic("i18n init: " + err.Error())
	}
	// Seed the in-process redirect-mode + maintenance-mode pointers
	// from env + DB before the middleware chain reads them. Both
	// middlewares are DB-free on the hot path so a self-host with a
	// flaky DB during cutover still serves clean 301s and 503s.
	settings.LoadRedirectTarget(context.Background(), cfg.DB)
	settings.LoadMaintenanceMode(context.Background(), cfg.DB)
	if settings.MaintenanceMode() {
		logx.Warn("maintenance.on", "pinned_by_env", settings.MaintenancePinnedByEnv(), "msg", "maintenance mode is on: writes are refused until it is turned off")
	}
	// Prices are set under Settings → Pricing; the env vars that set
	// them before are ignored, so say so rather than silently charge
	// something else.
	settings.LoadPricing(context.Background(), cfg.DB)
	// Settings → Legal, once copied from the env vars it replaced;
	// after that they are ignored.
	copied := map[string]bool{}
	for _, name := range settings.LoadLegal(context.Background(), cfg.DB, os.Getenv) {
		copied[name] = true
		log.Printf("%s copied to Settings → Legal; remove it from the env", name)
	}
	for _, name := range settings.LegalEnvVars() {
		if !copied[name] && os.Getenv(name) != "" {
			log.Printf("%s is no longer read: set it under Settings → Legal", name)
		}
	}
	for _, name := range []string{"ZV_PRICE_PER_PLAYER_CENTS", "ZV_FREE_PLAYERS_PER_EVENT", "ZV_HOSTED_BLOCK_CENTS", "ZV_HOSTED_CURRENCY", "ZV_PLATFORM_FEE_PERCENT_BPS", "ZV_PLATFORM_FEE_FIXED_CENTS", "ZV_PLATFORM_FEE_MIN_CENTS"} {
		if os.Getenv(name) != "" {
			log.Printf("%s is no longer read: set prices under Settings → Pricing", name)
		}
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// chi's RealIP rewrites RemoteAddr from X-Forwarded-* unconditionally,
	// which on a directly-reachable deployment lets an attacker forge the
	// header and dodge the per-IP limiter. trustedProxyRealIP gates the
	// rewrite behind auth.IsTrustedPeer so XFF is only honoured when the
	// immediate peer is in the operator-configured trusted set.
	r.Use(trustedProxyRealIP)
	r.Use(middleware.Recoverer)
	// Baseline security headers wrap every response, including the
	// short-circuit 301 from redirectMode and 503 from maintenanceFreeze
	// below — without wrapping them first, those responses would lack
	// CSP / X-Frame / X-Content-Type-Options. Costs nothing on a 301
	// where the body is empty anyway; closes a real gap on the 503.
	r.Use(securityHeaders(cspFor(cfg.Legal.MapTileURL, cfg.Legal.MapStyleURL)))
	// An organisation's own domain answers as its store, in its name
	// (customdomain.go); its pages here move there once it is live.
	if cfg.DB != nil {
		if err := domains.Load(context.Background(), cfg.DB); err != nil {
			logx.Warn("domains.load", "err", err.Error())
		}
	}
	r.Use(customDomains(cfg.PublicURL, cfg.ShopHost))
	// Hide links to /privacy, /terms, /pricing and /store while they'd
	// answer 503 / 404 (legal identity unset, landing off, no home org).
	r.Use(hideUnavailableLinks(func() LegalConfig { return legalNow(cfg.Legal) }, cfg.HomeOrg))
	// Cutover redirect: when the operator has flipped redirect mode,
	// every non-allowlisted path 301s to the hosted instance so
	// printed QR codes / emailed magic links keep working.
	r.Use(redirectMode)
	// Maintenance-mode freeze: 503s mutations while the operator
	// runs an org-bundle export so concurrent writes don't tear
	// the snapshot. Reads pass through; the migrate toggles are
	// allowlisted so the operator can still flip the flag back off.
	r.Use(maintenanceFreeze)
	// Metrics middleware runs early so it records every request,
	// including those rejected by auth or CSRF below. The collectors
	// always increment; the /metrics scrape endpoint is only exposed
	// when ZV_METRICS_ADDR is set (see cmd/server/main.go).
	r.Use(metrics.HTTPMiddleware(func(req *http.Request) string {
		if rctx := chi.RouteContext(req.Context()); rctx != nil {
			return rctx.RoutePattern()
		}
		return ""
	}))
	// EnsureCSRF runs early so the zv_csrf cookie is on every response,
	// including the static /arrange and /play pages. The HTTP-only
	// session cookies (zv_session, zv_slot) live alongside it.
	r.Use(auth.EnsureCSRF(cfg.SecureCookie))
	r.Use(auth.Middleware(cfg.DB, lookupFailed("session lookup failed")))
	r.Use(auth.SlotMiddleware(cfg.DB, lookupFailed("slot lookup failed")))

	hub := cfg.Hub
	if hub == nil {
		hub = ws.NewHub()
	}

	// One server-lifetime context drives every long-lived background
	// goroutine spawned from here on (login-limiter sweepers, the
	// public-rate-limiter sweep loop, and updateSvc below). Falls back
	// to context.Background when the caller didn't supply one — that
	// matches the historic "live for the process" posture but lets a
	// future graceful-shutdown thread one in.
	bgCtx := cfg.UpdateContext
	if bgCtx == nil {
		bgCtx = context.Background()
	}
	authH := &auth.Handlers{
		DB:            cfg.DB,
		SecureCookie:  cfg.SecureCookie,
		Limiter:       newSweptLimiter(bgCtx),
		ForgotLimiter: newSweptLimiter(bgCtx),
		TOTPLimiter:   newSweptLimiter(bgCtx),
		TOTPKey:       cfg.TOTPKey,
		Mailer:        cfg.Mailer,
		PublicURL:     cfg.PublicURL,
		OIDC:          cfg.OIDC,
		MCPEnabled:    mcp.Enabled(),
	}
	playerH := &player.Handlers{
		DB: cfg.DB, Lib: cfg.Library, Hub: hub, SecureCookie: cfg.SecureCookie,
		PublicURL: cfg.PublicURL,
		// One engine for every request, so decoded graph versions are
		// cached once per process.
		Engine: play.New(cfg.DB),
		// Mailer is wired below the LogMailer fallback so dev/test
		// see the recovered link in stderr instead of failing closed.
	}
	mail := cfg.Mailer
	if mail == nil {
		mail = mailer.LogMailer{}
	}
	playerH.Mailer = mail
	store := cfg.Storage
	if store == nil {
		store = dbblob.New(cfg.DB)
	}
	playerH.Storage = store
	importers := cfg.TeamImporters
	if importers == nil {
		// Bundled default set. Each format is a separate subpackage
		// so an operator who wants to drop one (e.g. skip excelize
		// to shave the dep) can construct their own registry and
		// pass it via Config — the routes don't need to change.
		importers = teamimport.New()
		importers.Register(teamimportcsv.New())
		importers.Register(teamimportods.New())
		importers.Register(teamimportxlsx.New())
		importers.Register(teamimportiofxml.New())
	}
	updateCtx := bgCtx
	updateSvc := updates.NewService(updates.Config{
		Version:      cfg.Version,
		ChangelogURL: cfg.ChangelogURL,
	})
	updateSvc.Start(updateCtx)

	// Pick a payments.Provider impl. The default is auto-discovery:
	// Stripe when both Stripe credentials are set, else nil. The
	// operator can override with PaymentsProvider — "simulated" is
	// the dev-only impl that auto-confirms checkouts without
	// touching real money.
	var (
		paymentProvider payments.Provider
		simulatedProv   *simulatedprovider.Provider
	)
	switch strings.ToLower(strings.TrimSpace(cfg.PaymentsProvider)) {
	case "simulated":
		// Refuse to wire fake payments anywhere that isn't explicitly
		// a dev environment. The simulated provider auto-confirms
		// every checkout without taking a real charge, so an operator
		// flipping ZV_PAYMENTS_PROVIDER=simulated in prod would hand
		// out free fulfilment on every buy. ZV_ENV is the operator-
		// visible knob — anything other than "dev"/"development"
		// (including unset) trips the panic.
		env := strings.ToLower(strings.TrimSpace(cfg.Env))
		if env != "dev" && env != "development" {
			panic("ZV_PAYMENTS_PROVIDER=simulated requires ZV_ENV=dev (refusing to wire fake payments outside a dev environment)")
		}
		// Auto-confirm posts the synthetic event back to the
		// public webhook endpoint on the same origin so the
		// existing dedupe + handler path runs end-to-end.
		base := strings.TrimRight(cfg.PublicURL, "/")
		if base == "" {
			base = "http://127.0.0.1:8080"
		}
		simulatedProv = simulatedprovider.New(base + "/api/webhooks/stripe")
		paymentProvider = simulatedProv
	case "stripe", "":
		if p := stripeprovider.New(cfg.StripeSecretKey, cfg.StripeWebhookSecret); p != nil {
			env := strings.ToLower(strings.TrimSpace(cfg.Env))
			if stripeprovider.IsTestKey(cfg.StripeSecretKey) && env != "dev" && env != "development" {
				panic("ZV_STRIPE_SECRET_KEY is a Stripe test-mode key but ZV_ENV is not dev (refusing to take test-mode payments outside a dev environment)")
			}
			if !strings.HasPrefix(cfg.StripeSecretKey, "sk_") && !strings.HasPrefix(cfg.StripeSecretKey, "rk_") {
				logx.Warn("payments.stripe.key", "msg", "ZV_STRIPE_SECRET_KEY does not look like a secret key (sk_… or rk_…): checkouts will fail at Stripe")
			}
			p.ConnectWebhookSecret = cfg.StripeConnectWebhookSecret
			paymentProvider = p
		} else if cfg.StripeSecretKey != "" || cfg.StripeWebhookSecret != "" || cfg.PaymentsProvider != "" {
			logx.Warn("payments.stripe.incomplete", "msg", "Stripe needs both ZV_STRIPE_SECRET_KEY and ZV_STRIPE_WEBHOOK_SECRET; with one missing, paid checkout stays unavailable",
				"secret_key_set", cfg.StripeSecretKey != "", "webhook_secret_set", cfg.StripeWebhookSecret != "")
		}
	default:
		// Unknown name — fail loud rather than silently falling
		// through to "payments disabled" and confusing the
		// operator about why their checkout flow doesn't work.
		panic("unknown ZV_PAYMENTS_PROVIDER: " + cfg.PaymentsProvider + " (valid: stripe, simulated)")
	}
	// MailOutbox is the durable retry layer for transactional sends
	// where a paid user must not be stranded by an SMTP blip
	// (post-Stripe ticket fulfilment magic link). The worker drains
	// rows enqueued by sendTicketTeamLink with exponential backoff;
	// see internal/mailer/outbox.go for the contract.
	mailOutbox := &mailer.Outbox{DB: cfg.DB, Mailer: mail}
	go mailOutbox.Run(bgCtx)
	adminH := &admin.Handlers{
		DB: cfg.DB, Lib: cfg.Library, Hub: hub, Engine: playerH.Engine,
		Mailer:         mail,
		MailOutbox:     mailOutbox,
		PublicURL:      cfg.PublicURL,
		HomeOrg:        cfg.HomeOrg,
		ShopDir:        shopDir(cfg.WebOverrideDir),
		ShopHost:       cfg.ShopHost,
		SellerIdentity: func(lang string) string { return legalNow(cfg.Legal).SellerIdentityText(lang) },
		Provider:       paymentProvider,
		SecureCookie:   cfg.SecureCookie,
		Components: func() []components.Component {
			return withSSOComponent(withLegalComponent(cfg.Components, legalNow(cfg.Legal)), cfg.OIDC)
		},
		Storage:        store,
		TOTPKey:        cfg.TOTPKey,
		TeamImporters:  importers,
		Updates:        updateSvc,
		ConfirmLimiter: newSweptLimiter(bgCtx),
	}
	// Hosted events: finish idle games, end lapsed events, retry game
	// summaries. See admin/hosted.go.
	periodic.Start(bgCtx, time.Minute, adminH.SweepHosted)
	// Public quiz trails: delete old drafts and ended trails, offer
	// Keep it a week into play. See admin/trails_public.go.
	periodic.Start(bgCtx, time.Hour, adminH.SweepTrails)
	// The listing review log's head, mailed to platform admins daily
	// when it moved: a copy outside the database.
	periodic.Start(bgCtx, time.Hour, adminH.AnchorListingLog)
	// The day-before reminder of a scheduled account deletion.
	periodic.Start(bgCtx, time.Hour, adminH.SweepDeletionReminders)
	// Organisations' own domains: DNS checks, going live, endings.
	periodic.Start(bgCtx, time.Minute, adminH.SweepDomains)
	// Paying for teams: monthly bills on the 1st, card retries, low
	// balance warnings. See admin/billing.go.
	periodic.Start(bgCtx, time.Hour, adminH.SweepBilling)
	playerH.ActivateVoucher = adminH.ActivateVoucherCode
	publicH := &public.Handlers{
		DB: cfg.DB, Lib: cfg.Library, Hub: hub, Storage: store, PublicURL: cfg.PublicURL, Engine: playerH.Engine,
		SecureCookie: cfg.SecureCookie,
		Looks:        public.LookAssets{ThemeCover: themeCoverURL, Font: lookFontURL},
		Payments:     paymentProvider != nil,
		Marketplace:  adminH.Marketplace,
	}

	// Read-only sitemap template + computed version metadata —
	// build once at startup so the per-request handlers in
	// registerStaticAssetRoutes / registerMetaRoutes can hand back
	// pre-rendered bytes. /sitemap.xml still does a per-request
	// substitution for the ORG_URLS slot since orgs come and go.
	sitemapBody, err := readWebFile("web/sitemap.xml")
	if err != nil {
		panic("embed sitemap: " + err.Error())
	}
	verResp := buildVersion(cfg.Version)
	publicRL := newPublicLimiter(bgCtx)
	env := &routesEnv{
		cfg:           cfg,
		authH:         authH,
		playerH:       playerH,
		adminH:        adminH,
		publicH:       publicH,
		simulatedProv: simulatedProv,
		storage:       store,
		verResp:       verResp,
		sitemapBody:   sitemapBody,
		publicRL:      publicRL,
	}

	// Route registrations live in routes_*.go so the route table is
	// findable by surface (pages / assets / public-token / auth /
	// player / admin). Routes don't overlap, so the call order
	// affects readability only — chi treats each pattern
	// independently.
	registerMetaRoutes(r, env)
	registerPageRoutes(r, env)
	// The React apps, served under /app/<name>/. Mounted alongside the
	// hand-written pages above rather than replacing them: surfaces
	// move over one route registration at a time. See spa.go.
	registerSPARoutes(r)
	registerStaticAssetRoutes(r, env)
	registerAdventureAssetRoutes(r, env)
	registerPublicTokenRoutes(r, env)
	registerAuthRoutes(r, env)
	registerPaymentWebhookRoutes(r, env)
	registerPlayerRoutes(r, env)
	registerAdminRoutes(r, env)
	warnShadowedOverridePages(r)

	return r
}

// routesEnv bundles the dependencies that route helpers in
// routes_*.go need. Built once in New() after all the per-handler
// constructors run; passed to each register*Routes helper. Keeping
// it private to the package means nothing outside server-internal
// code knows or cares about the split.
type routesEnv struct {
	cfg           Config
	authH         *auth.Handlers
	playerH       *player.Handlers
	adminH        *admin.Handlers
	publicH       *public.Handlers
	simulatedProv *simulatedprovider.Provider
	storage       storage.Blobs
	verResp       versionResp
	sitemapBody   []byte
	publicRL      *publicLimiter
}

// servePage returns a handler that writes a single embedded HTML page with
// no caching. Used for the SPA shells (/arrange, /play, /live) where a
// stale shell after deploy would surface as broken UI.
// securityHeaders is the global middleware that sets the baseline
// CSP + X-Frame-Options + Referrer-Policy + nosniff on every
// response. Wrapping the entire router (rather than per-route)
// keeps the policy consistent — a future contributor adding a
// new endpoint inherits the headers automatically.
//
// CSP design notes (kept minimal so the embedded HTML pages
// continue to work):
//
//   - default-src 'self' — same-origin only, no implicit allow.
//   - script-src 'self' 'unsafe-inline' https://unpkg.com —
//     every page has at least one inline <script> block (login
//     form handlers, page-load theme application). The
//     /arrange surface still pulls xterm + xterm-addon-fit from
//     unpkg for the embedded admin terminal.
//   - style-src 'self' 'unsafe-inline' https://unpkg.com — inline
//     `style="..."` from the DOM helpers + xterm's CSS from unpkg.
//   - img-src 'self' data: blob: https: — data: for QR / canvas
//     downscale, blob: for QR-decoder + audio clips, https: so
//     adventure assets / map tiles (OpenStreetMap) work.
//   - font-src 'self' — Aclonica / Open Sans woff2 served from the
//     binary (/assets/fonts, the same files the SPAs bundle), so no
//     page hands a visitor's IP address to Google Fonts. Emoji come
//     from the system font, as they did with Google's CSS.
//   - connect-src 'self' wss: ws: https://unpkg.com — XHR /
//     fetch / WebSocket. unpkg is whitelisted so devtools can
//     pull xterm's sourcemaps without the browser logging a
//     CSP-block warning on every /arrange page open.
//     The map style's host (OpenFreeMap by default) and any raster
//     tile host are where the MapLibre maps fetch their tiles.
//   - worker-src 'self' blob: — MapLibre decodes tiles in a
//     worker.
//   - media-src 'self' blob: — audio attachments are blob URLs.
//   - frame-ancestors 'none' — blocks the clickjacking surface on
//     /team/{token}, /recap/{token}, /play, /arrange.
//   - object-src 'none' — no Flash / Java / legacy plugin embed.
//   - base-uri 'self' — pin <base> so a stored-XSS can't redirect
//     relative URLs.
const baselineCSP = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline' https://unpkg.com; " +
	"style-src 'self' 'unsafe-inline' https://unpkg.com; " +
	"img-src 'self' data: blob: https:; " +
	"font-src 'self'; " +
	// {tiles}: the MapLibre maps fetch their tiles from the configured
	// raster tile host (ZV_MAP_TILE_URL) and their vector style, tiles,
	// glyphs and sprites from the style's host (ZV_MAP_STYLE_URL), see
	// cspFor; blob: workers are how MapLibre decodes them off the main
	// thread.
	"connect-src 'self' wss: ws: https://unpkg.com {tiles}; " +
	"worker-src 'self' blob:; " +
	"media-src 'self' blob:; " +
	"frame-ancestors 'none'; " +
	"object-src 'none'; " +
	"base-uri 'self'"

// cspFor fills baselineCSP's map origins from the operator's vector
// style URL, falling back to OpenFreeMap's, and raster tile URL
// template, if any.
func cspFor(tileURL, styleURL string) string {
	origin := func(raw string) string {
		if u, err := url.Parse(raw); err == nil && u.Scheme == "https" && u.Host != "" {
			return "https://" + u.Host
		}
		return ""
	}
	hosts := origin(styleURL)
	if hosts == "" {
		hosts = "https://tiles.openfreemap.org"
	}
	if tiles := origin(tileURL); tiles != "" && tiles != hosts {
		hosts += " " + tiles
	}
	return strings.Replace(baselineCSP, "{tiles}", hosts, 1)
}

// trustedProxyRealIP is a gated replacement for chi's middleware.RealIP.
// chi's version rewrites RemoteAddr from X-Forwarded-For / X-Real-IP
// regardless of who sent the request, which on a directly-reachable
// deployment lets an attacker spoof their source IP and bypass per-IP
// rate limits, audit logging, and lockouts. Here, the rewrite only
// happens when auth.IsTrustedPeer reports the immediate peer is in the
// operator-configured ZV_TRUSTED_PROXIES set; otherwise RemoteAddr is
// left intact and the limiter / audit see the real edge address.
func trustedProxyRealIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.IsTrustedPeer(r) {
			r = auth.MarkTrustedPeer(r)
			if real := firstForwardedAddr(r); real != "" {
				r.RemoteAddr = real
			}
		}
		next.ServeHTTP(w, r)
	})
}

// firstForwardedAddr returns the leftmost X-Forwarded-For entry, or
// X-Real-IP, or empty when neither is present. Matches the shape chi's
// RealIP middleware produced (RemoteAddr is "<host>" with no port —
// callers like net.SplitHostPort handle either shape).
func firstForwardedAddr(r *http.Request) string {
	// Right-most non-trusted XFF entry — the real client past the trusted
	// proxy chain. Walking from the right defeats client-forged left-side
	// entries when a proxy appends rather than overwrites XFF. Only
	// reached when the immediate peer is already trusted (see caller).
	if real := auth.RightmostUntrustedXFF(r.Header.Get("X-Forwarded-For")); real != "" {
		return real
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	return ""
}

// embeddablePrefix is where pages may be framed by the site's own
// pages: the quiz trail creator, which the landing page shows in a
// section of its own. Same origin only, so the clickjacking surface
// frame-ancestors 'none' closes stays closed to every other site.
const embeddablePrefix = "/app/party/trail/"

func securityHeaders(csp string) func(http.Handler) http.Handler {
	framed := strings.Replace(csp, "frame-ancestors 'none'", "frame-ancestors 'self'", 1)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			if strings.HasPrefix(r.URL.Path, embeddablePrefix) {
				h.Set("Content-Security-Policy", framed)
				h.Set("X-Frame-Options", "SAMEORIGIN")
			} else {
				h.Set("Content-Security-Policy", csp)
				h.Set("X-Frame-Options", "DENY")
			}
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			// nosniff applies to every response. We already set it
			// individually on a few specific endpoints (/adventure-
			// assets/audio/*) where MIME confusion is the highest
			// risk; the baseline keeps the rest of the surface
			// covered.
			h.Set("X-Content-Type-Options", "nosniff")
			next.ServeHTTP(w, r)
		})
	}
}

func servePage(path string) http.HandlerFunc {
	body, err := readWebFile(path)
	if err != nil {
		panic("embed: " + err.Error())
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	}
}

// servePublicPage is the cacheable variant for the marketing landing
// and the public manual — content that's safe to sit behind a CDN /
// browser cache for a few minutes. status lets the caller pin a
// non-200 (used by the 404 handler so crawlers see the right code).
func servePublicPage(path string, status int) http.HandlerFunc {
	body, err := readWebFile(path)
	if err != nil {
		panic("embed: " + err.Error())
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// 5-minute browser cache + must-revalidate so a content fix
		// reaches readers within the next page load. "public" lets a
		// shared cache (CDN, reverse proxy) serve hot copies.
		w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
		if status != 0 && status != 200 {
			w.WriteHeader(status)
		}
		_, _ = w.Write(body)
	}
}

// serveAsset serves an embedded asset with an explicit Content-Type
// and ETag-based revalidation.
//
// The body is read once at handler construction so the ETag is a
// stable hash of the served bytes for the process lifetime. Browsers
// repeat-visit with `If-None-Match` and get a 304 when nothing has
// changed; combined with a moderate `max-age`, that means a typical
// nav doesn't even hit our handler. A deploy mints new ETags
// (process restart → fresh hash if bytes changed) so users pick up
// updated assets the next time the cache entry expires.
//
// Earlier the handler emitted `Cache-Control: no-store` which forced
// every navigation to re-download topbar.css, page-tokens.css,
// fsmap.css, i18n.js, and the 130 KB jsqr.js — the worst single
// repeat-visit cost in the product.
//
// Text assets are also kept gzipped, made once here too, and served so
// to a browser that accepts it: MapLibre's 1.1 MB of script is 0.3 MB
// on the wire.
func serveAsset(path, contentType string) http.HandlerFunc {
	body, err := readWebFile(path)
	if err != nil {
		panic("embed: " + err.Error())
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	var zipped []byte
	if !strings.HasPrefix(contentType, "image/") || strings.HasPrefix(contentType, "image/svg") {
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		_, _ = zw.Write(body)
		_ = zw.Close()
		if buf.Len() < len(body) {
			zipped = buf.Bytes()
		}
	}
	zipTag := strings.TrimSuffix(etag, `"`) + `-gz"`
	return func(w http.ResponseWriter, r *http.Request) {
		gz := zipped != nil && acceptsGzip(r.Header.Get("Accept-Encoding"))
		tag := etag
		if gz {
			tag = zipTag
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("ETag", tag)
		w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")
		if zipped != nil {
			w.Header().Set("Vary", "Accept-Encoding")
		}
		if match := r.Header.Get("If-None-Match"); match != "" && match == tag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if gz {
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(zipped)
			return
		}
		_, _ = w.Write(body)
	}
}

// acceptsGzip reports whether an Accept-Encoding header takes gzip,
// not refused with q=0.
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.TrimSpace(name) != "gzip" {
			continue
		}
		q := strings.ReplaceAll(strings.TrimSpace(params), " ", "")
		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}
	return false
}

// versionResp is the /api/version payload. Stable shape so monitoring
// scripts can pin to specific fields; new fields are additive only.
type versionResp struct {
	Version  string `json:"version"`
	Commit   string `json:"commit,omitempty"`
	Time     string `json:"time,omitempty"`
	Modified bool   `json:"modified,omitempty"`
	Go       string `json:"go,omitempty"`
	OS       string `json:"os,omitempty"`
	Arch     string `json:"arch,omitempty"`
}

// buildVersion assembles the response for /api/version. The version
// string comes from the caller (typically -ldflags="-X main.version=…");
// commit / build time / dirty flag come from runtime/debug.ReadBuildInfo
// which the Go toolchain populates automatically when building inside a
// VCS checkout. -trimpath does not strip these settings; -buildvcs=false
// would, so we don't pass it.
func buildVersion(version string) versionResp {
	if version == "" {
		version = "dev"
	}
	resp := versionResp{
		Version: version,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				resp.Commit = s.Value
			case "vcs.time":
				resp.Time = s.Value
			case "vcs.modified":
				resp.Modified = s.Value == "true"
			}
		}
	}
	return resp
}
