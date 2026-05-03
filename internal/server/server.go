// Package server wires the HTTP router, middleware, and per-package
// handlers into one http.Handler.
package server

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"git.sr.ht/~klahr/zv/internal/admin"
	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/components"
	"git.sr.ht/~klahr/zv/internal/mailer"
	"git.sr.ht/~klahr/zv/internal/storage"
	"git.sr.ht/~klahr/zv/internal/storage/dbblob"
	"git.sr.ht/~klahr/zv/internal/metrics"
	"git.sr.ht/~klahr/zv/internal/player"
	"git.sr.ht/~klahr/zv/internal/public"
	"git.sr.ht/~klahr/zv/internal/settings"
	"git.sr.ht/~klahr/zv/internal/simulator"
	"git.sr.ht/~klahr/zv/internal/teamimport"
	teamimportcsv "git.sr.ht/~klahr/zv/internal/teamimport/csv"
	teamimportiofxml "git.sr.ht/~klahr/zv/internal/teamimport/iofxml"
	teamimportods "git.sr.ht/~klahr/zv/internal/teamimport/ods"
	teamimportxlsx "git.sr.ht/~klahr/zv/internal/teamimport/xlsx"
	"git.sr.ht/~klahr/zv/internal/updates"
	"git.sr.ht/~klahr/zv/internal/ws"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed web/play.html web/play-sw.js web/play-queue.js web/arrange.html web/live.html web/recap.html web/i18n.js web/favicon.svg web/privacy.html web/terms.html web/manual.html web/landing.html web/pricing.html web/topbar.css web/topbar.js web/fsmap.css web/jsqr.js web/404.html web/robots.txt web/sitemap.xml
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
	// (the upstream sourcehut repo's CHANGELOG.md). Set explicitly to
	// pin a fork or to disable the surface in air-gapped deployments
	// (the URL "off" is treated as "no checking" — see Updates.Service).
	ChangelogURL string
	// UpdateContext bounds the lifetime of the changelog-polling
	// goroutine. Pass the same context the rest of the binary uses
	// for cancellation; nil falls back to context.Background() (the
	// loop runs until the process exits).
	UpdateContext context.Context
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
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	// Baseline security headers on every response. Tight by default —
	// the app loads fonts from Google's CDN (the embedded HTML pages
	// reference fonts.googleapis.com / fonts.gstatic.com) and a few
	// inline styles + scripts; the CSP allows those explicitly. No
	// remote scripts, no remote XHR / fetch — every JS asset and
	// API endpoint is same-origin. frame-ancestors 'none' blocks
	// clickjacking by refusing to be iframed; X-Frame-Options is
	// the legacy fallback for older browsers. Referrer-Policy
	// strict-origin keeps team-share / recap / live tokens out of
	// the Referer header on outbound link clicks.
	r.Use(securityHeaders)
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
	r.Use(auth.Middleware(cfg.DB))
	r.Use(auth.SlotMiddleware(cfg.DB))

	hub := cfg.Hub
	if hub == nil {
		hub = ws.NewHub()
	}

	authH := &auth.Handlers{
		DB:            cfg.DB,
		SecureCookie:  cfg.SecureCookie,
		Pricing: auth.Pricing{
			PlayerCents:         admin.PricePerPlayerCents,
			FreePlayersPerEvent: admin.FreePlayersPerEvent,
			Currency:            "EUR",
		},
		Limiter:       auth.NewLoginLimiter(),
		ForgotLimiter: auth.NewLoginLimiter(),
		TOTPLimiter:   auth.NewLoginLimiter(),
		TOTPKey:       cfg.TOTPKey,
		Mailer:        cfg.Mailer,
		PublicURL:     cfg.PublicURL,
		OIDC:          cfg.OIDC,
	}
	playerH := &player.Handlers{
		DB: cfg.DB, Lib: cfg.Library, Hub: hub, SecureCookie: cfg.SecureCookie,
		PublicURL: cfg.PublicURL,
		// Mailer is wired below the LogMailer fallback so dev/test
		// see the recovered link in stderr instead of failing closed.
	}
	sim := simulator.New(cfg.DB, cfg.Library, hub)
	mail := cfg.Mailer
	if mail == nil {
		mail = mailer.LogMailer{}
	}
	playerH.Mailer = mail
	store := cfg.Storage
	if store == nil {
		store = dbblob.New(cfg.DB)
	}
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
	updateCtx := cfg.UpdateContext
	if updateCtx == nil {
		updateCtx = context.Background()
	}
	updateSvc := updates.NewService(updates.Config{
		Version:      cfg.Version,
		ChangelogURL: cfg.ChangelogURL,
	})
	updateSvc.Start(updateCtx)

	adminH := &admin.Handlers{
		DB: cfg.DB, Lib: cfg.Library, Hub: hub, Sim: sim,
		Mailer:              mail,
		PublicURL:           cfg.PublicURL,
		StripeSecretKey:     cfg.StripeSecretKey,
		StripeWebhookSecret: cfg.StripeWebhookSecret,
		SecureCookie:        cfg.SecureCookie,
		Components:          cfg.Components,
		Storage:             store,
		TOTPKey:             cfg.TOTPKey,
		TeamImporters:       importers,
		Updates:             updateSvc,
	}
	publicH := &public.Handlers{DB: cfg.DB, Lib: cfg.Library, Hub: hub, Storage: store, PublicURL: cfg.PublicURL}

	// Health probe used by load balancers and `depends_on: condition:
	// service_healthy` in compose. Pings the DB so we report unhealthy
	// when the connection is down — a stuck-but-listening server would
	// otherwise keep receiving traffic.
	r.Get("/api/health", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), time.Second)
		defer cancel()
		if err := cfg.DB.PingContext(ctx); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"db unavailable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Build info, computed once at startup (debug.ReadBuildInfo is
	// stable for the lifetime of the process). Lets operators confirm
	// which build is serving after an upgrade — `curl /api/version`
	// returns the tagged version, VCS revision, and commit time.
	verResp := buildVersion(cfg.Version)
	r.Get("/api/version", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(verResp)
	})

	// Root branches by who's looking:
	//   - players (slot session)        → /play
	//   - logged-in staff (user session) → /arrange
	//   - everyone else                  → marketing landing page
	//
	// Returning the landing page (rather than the login screen) for
	// fresh visitors gives crawlers / AI agents / unauthenticated
	// humans a real "what is this?" surface before they're asked to
	// log in. The landing's CTAs send them to /arrange when they
	// actually want the app.
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if _, ok := auth.SlotFromContext(req.Context()); ok {
			http.Redirect(w, req, "/play", http.StatusFound)
			return
		}
		if _, ok := auth.FromContext(req.Context()); ok {
			http.Redirect(w, req, "/arrange", http.StatusFound)
			return
		}
		// Self-host operators can hide the marketing landing via the
		// admin Settings page. With the landing off, an unauthenticated
		// visitor lands directly on /arrange — the login screen is the
		// front door for a closed deployment.
		if !settings.LandingEnabled(req.Context(), cfg.DB) {
			http.Redirect(w, req, "/arrange", http.StatusFound)
			return
		}
		renderLandingPage(w, req)
	})

	// Custom 404 with branded chrome and a "back to home / read manual"
	// fallback. chi serves a plain "404 page not found" otherwise,
	// which feels broken from a marketing-site perspective.
	r.NotFound(servePublicPage("web/404.html", http.StatusNotFound))

	// /play handles three things:
	//   - /play?t=<magic> — exchange the magic-link for a slot session and
	//     redirect back to /play (stripping the token)
	//   - /play (no query, no session) — renders the play page which will
	//     say "open your magic link"
	//   - /play (with session cookie) — the playing UI
	r.Get("/play", func(w http.ResponseWriter, req *http.Request) {
		if playerH.MagicLinkLogin(w, req) {
			return
		}
		servePage("web/play.html")(w, req)
	})
	r.Get("/arrange", servePage("web/arrange.html"))

	// Player-side offline action queue. Loaded by play.html before
	// the inline IIFE so its helpers are in scope as globals.
	// Tested in isolation by play-queue.test.js (Node's built-in
	// test runner). Same path-based asset-style serve as i18n.js.
	r.Get("/play-queue.js", serveAsset("web/play-queue.js", "application/javascript; charset=utf-8"))

	// Player-side service worker. Served at the site root so its
	// scope is the whole origin — that's what lets it intercept
	// /adventure-assets/* requests for offline asset playback. The
	// browser refetches the SW on every page load (no-cache below),
	// so a deploy reaches users without a hard refresh.
	r.Get("/play-sw.js", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Service-Worker-Allowed", "/")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		body, err := readWebFile("web/play-sw.js")
		if err != nil {
			http.Error(w, "embed", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	})

	// Shared i18n dictionary + helpers, loaded by all three entry points.
	r.Get("/i18n.js", serveAsset("web/i18n.js", "application/javascript; charset=utf-8"))
	// Favicon: single SVG served at both the conventional /favicon.ico
	// path and the explicit /favicon.svg so old browsers that sniff
	// the extension don't 404. SVG is tiny, scales for any device.
	r.Get("/favicon.svg", serveAsset("web/favicon.svg", "image/svg+xml"))
	r.Get("/favicon.ico", serveAsset("web/favicon.svg", "image/svg+xml"))

	// Public, no-auth read-only endpoints. Tokens are unguessable but
	// the routes still hit the database; rate-limit per IP so a script
	// can't brute-force or DoS them. Caps generously so a real viewer
	// (with reconnect storms, tab refreshes, etc.) never trips it.
	publicRL := newPublicLimiter()
	r.Group(func(r chi.Router) {
		r.Use(publicRLMiddleware(publicRL))
		r.Get("/live/{token}", servePage("web/live.html"))
		r.Get("/api/public/live/{token}", publicH.Live)
		r.Get("/api/public/live/{token}/tracks", publicH.Tracks)
		r.Get("/ws/live/{token}", publicH.WSLive)
		r.Get("/team/{token}", publicH.TeamShare)
		r.Get("/qr/team/{token}", publicH.TeamShareQR)
		// Post-event recap. Two halves: the SPA shell at /recap/{token}
		// and the JSON it fetches on load. Both gate on event state
		// in the handler — a token from a still-running event 404s
		// here, so a leaked URL doesn't expose live position data.
		r.Get("/recap/{token}", servePublicPage("web/recap.html", http.StatusOK))
		r.Get("/api/public/recap/{token}", publicH.Recap)
		// Team-token-authed avatar upload/delete so the team can change
		// their own profile picture from the share page. The token IS the
		// auth; no staff session required.
		r.Post("/api/public/team/{token}/avatar", publicH.SetTeamAvatarByToken)
		r.Delete("/api/public/team/{token}/avatar", publicH.DeleteTeamAvatarByToken)
	})

	// Image serving: permission is decided inside the handler (staff
	// sessions always pass; slot sessions only when they're on the page).
	r.Get("/adventure-assets/image/{imageID}", adminH.ServeImage)
	// Hint images: same shape, stricter visibility (player must have
	// revealed the owning hint, not merely be on the page). The
	// handler enforces it; this route is intentionally separate from
	// /adventure-assets/image/ so the rules don't get conflated.
	r.Get("/adventure-assets/hint-image/{imageID}", adminH.ServeHintImage)
	// Audio attachments: page-level audio mirrors page-image visibility
	// (any team on the page sees it); hint audio mirrors hint-image's
	// reveal-gated rule. Same handlers, audio binary served verbatim.
	r.Get("/adventure-assets/audio/{audioID}", adminH.ServeAudio)
	r.Get("/adventure-assets/hint-audio/{audioID}", adminH.ServeHintAudio)
	// Team avatars are intentionally open — they surface on the public
	// live board and on player views without a staff session. The team
	// id is opaque enough to be acceptable public read-only data.
	r.Get("/adventure-assets/team-avatar/{teamID}", adminH.ServeTeamAvatar)
	// Adventure cover pictures — same public-read rationale: the slug is
	// already in the URL, no extra sensitive metadata is leaked.
	r.Get("/adventure-assets/avatar/{slug}", adminH.ServeAdventureAvatar)
	// Org brand logo — public-read for the same reasons; companies put
	// their logo on their website, it's not sensitive material. Path
	// is short ("/org-logo/{id}") so the bytes embed cleanly into the
	// recap PDF and any future server-rendered surfaces.
	r.Get("/org-logo/{orgID}", adminH.ServeOrgLogo)

	// Staff authentication. CSRF-protected — the user gets a zv_csrf
	// cookie from the /arrange page GET that hosts the login form, so
	// the form's POST can echo it back. A cross-origin "log my victim
	// into my account" attempt has no way to read the cookie value.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireCSRF())
		r.Post("/api/auth/login", authH.Login)
		// Second leg of login when the user has TOTP enabled. The
		// challenge token issued by /api/auth/login is the only way
		// in; the endpoint mints the session cookie on success.
		r.Post("/api/auth/login/totp", authH.LoginTOTP)
		r.Post("/api/auth/signup", authH.Signup)
		r.Post("/api/auth/set-password", authH.SetPassword)
		r.Post("/api/auth/logout", authH.Logout)
		r.Post("/api/auth/resend-verification", authH.ResendVerification)
		// Forgot/reset are pre-auth: a user who can't log in is
		// exactly the audience here. Both still need CSRF (the
		// /arrange page seeds the cookie before the form submits).
		r.Post("/api/auth/forgot", authH.ForgotPassword)
		r.Post("/api/auth/reset", authH.ResetPassword)
		// Lost-magic-link recovery: pre-auth player flow that
		// re-mints magic-link tokens for any team whose contact
		// email matches the supplied address. Same CSRF + always-
		// 204 anti-enumeration shape as /api/auth/forgot.
		r.Post("/api/player/recover-link", playerH.RecoverLink)
		// Switch the calling session's active org. Caller must already
		// be authenticated AND a member of the target org — handler
		// returns 403 if not. CSRF-protected because it mutates the
		// authoritative session-state row.
		r.Post("/api/auth/switch-org", authH.SwitchOrg)
	})
	r.Get("/api/auth/whoami", authH.Whoami)
	// Public signup-config: unauthenticated GET so the login screen
	// can decide whether to render a "Sign up" link before any user
	// has a session. CSRF irrelevant — read-only, no cookies needed.
	r.Get("/api/auth/signup-config", authH.SignupConfig)
	// Email verification clicks land on a GET; not CSRF-relevant.
	r.Get("/verify", authH.VerifyEmail)

	// Single-sign-on (OIDC) flow. Both endpoints are GET because the
	// IdP redirects back via top-level navigation — there's no JS
	// fetch in the loop. CSRF for /login is bound by the state cookie
	// pinning the redirect to the issuing browser; CSRF for /callback
	// is the same state check, validated against what came back on
	// the URL. Routes 404 cleanly when SSO is not configured.
	r.Get("/auth/oidc/login", authH.OIDCLogin)
	r.Get("/auth/oidc/callback", authH.OIDCCallback)

	// Legal pages — public, cached at the proxy by Cache-Control.
	// Both pages are clearly-marked drafts until the operator sets
	// the ZV_* env vars and a lawyer signs off on the body.
	// Public arranger manual. Served as plain HTML so AI agents,
	// search crawlers and curl users can read the content without JS
	// or auth. The in-app /arrange#manual view is a translated mirror
	// of the same content for logged-in arrangers.
	// Manual + pricing are templated through renderTopbarPage so the
	// shared sticky-topbar / hamburger / language-picker reads the
	// caller's locale cookie and renders the picker active state
	// correctly. Page bodies are not (yet) translated for these
	// routes; only the topbar chrome.
	r.Get("/manual", func(w http.ResponseWriter, req *http.Request) {
		renderTopbarPage(w, req, "web/manual.html")
	})
	r.Get("/pricing", func(w http.ResponseWriter, req *http.Request) {
		// The pricing page is part of the public marketing surface.
		// Self-hosters with the landing off don't sell anything to
		// random visitors — fall through to the 404 handler so a
		// crawler hitting an old link gets the same shape as any
		// other unknown URL.
		if !settings.LandingEnabled(req.Context(), cfg.DB) {
			servePublicPage("web/404.html", http.StatusNotFound)(w, req)
			return
		}
		renderTopbarPage(w, req, "web/pricing.html")
	})

	// Shared topbar CSS + JS, served as long-cached static assets.
	r.Get("/assets/topbar.css", serveAsset("web/topbar.css", "text/css; charset=utf-8"))
	r.Get("/assets/topbar.js", serveAsset("web/topbar.js", "application/javascript; charset=utf-8"))
	// Shared fullscreen-map overlay CSS — used by both the player
	// preview-debug coord overlay and the arranger's "show on map"
	// links. Same theme tokens, same DOM contract; the JS that
	// drives each surface still lives inline in its own page.
	r.Get("/assets/fsmap.css", serveAsset("web/fsmap.css", "text/css; charset=utf-8"))
	// jsQR — vendored fallback QR decoder for browsers that don't
	// expose the native BarcodeDetector (Firefox today, older
	// Safari/iOS). Both /play and the puzzle editor in /arrange
	// load it lazily; the canvas-based decode runs entirely
	// client-side, no network beyond fetching this asset.
	r.Get("/assets/jsqr.js", serveAsset("web/jsqr.js", "application/javascript; charset=utf-8"))

	// Crawler hygiene: robots.txt and a small sitemap of public URLs.
	// {{HOST}} in the sitemap is rewritten with the operator-configured
	// PublicURL — without it the URLs render origin-relative, which
	// most crawlers handle but a few (Bing especially) prefer absolute.
	r.Get("/robots.txt", serveAsset("web/robots.txt", "text/plain; charset=utf-8"))
	sitemapBody, err := readWebFile("web/sitemap.xml")
	if err != nil {
		panic("embed sitemap: " + err.Error())
	}
	r.Get("/sitemap.xml", func(w http.ResponseWriter, req *http.Request) {
		host := strings.TrimRight(cfg.PublicURL, "/")
		if host == "" {
			scheme := "https"
			if req.TLS == nil && req.Header.Get("X-Forwarded-Proto") != "https" {
				scheme = "http"
			}
			host = scheme + "://" + req.Host
		}
		out := strings.ReplaceAll(string(sitemapBody), "{{HOST}}", host)
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write([]byte(out))
	})
	r.Get("/privacy", func(w http.ResponseWriter, _ *http.Request) {
		renderLegalPage(w, "privacy.html", cfg.Legal)
	})
	r.Get("/terms", func(w http.ResponseWriter, _ *http.Request) {
		renderLegalPage(w, "terms.html", cfg.Legal)
	})

	// Stripe webhook. Public POST: the auth IS the HMAC signature in
	// the Stripe-Signature header, verified by the handler. Sits OUTSIDE
	// the CSRF middleware because Stripe doesn't (and shouldn't) know
	// our CSRF cookie. Idempotency is handled by processed_webhook_events.
	r.Post("/api/webhooks/stripe", adminH.StripeWebhook)
	// Change-email confirmation: public POST (the token IS the auth),
	// CSRF-protected because the page that calls it is /arrange and
	// already has the cookie. JSON body: {"token": "..."}.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireCSRF())
		r.Post("/api/auth/confirm-email-change", adminH.ConfirmEmailChange)
	})

	// Player (slot-authenticated). Mutations under CSRF; the GET reads
	// stay open since they're idempotent and can't damage state.
	r.Route("/api/player", func(r chi.Router) {
		r.Use(auth.RequireSlot)
		r.Use(auth.RequireCSRF())
		r.Get("/state", playerH.GetState)
		// Asset manifest powers the player-side service-worker prefetch
		// for offline play: the SW cache.addAll()s these URLs on team
		// join so puzzles render even in a dead spot. Each URL is
		// still visibility-gated at fetch time.
		r.Get("/manifest", playerH.Manifest)
		r.Post("/answer", playerH.SubmitAnswer)
		r.Post("/location", playerH.PostLocation)
		r.Post("/hint", playerH.RevealHint)
		// Player-driven slot rename — the arranger's pre-set name is
		// often a placeholder ("Player 3"). Letting the player put
		// their own name on the leaderboard improves engagement.
		r.Post("/me/name", playerH.RenameMyself)
		// Player-driven team rename — the arranger's "Team 3"
		// placeholder is a common pre-event setup and the team
		// itself agrees on the real name as a group. Any team
		// member can update; UNIQUE(event_id, name) prevents
		// collisions across teams.
		r.Post("/team/name", playerH.RenameMyTeam)
		r.Get("/summary", playerH.Summary)
		r.Post("/logout", playerH.Logout)
		// GDPR right-to-erasure: wipes pings + answers + slot name +
		// session for the caller's slot. The slot row stays so the
		// team's run keeps working for everyone else.
		r.Post("/forget", playerH.Forget)
	})

	// Player WebSocket: replaces the 5s /state poll and 15s /location POST
	// with a single push-when-changed socket.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireSlot)
		r.Get("/ws/play", playerH.WSPlay)
	})

	// Arranger subscription stream: mutations still use the /api/admin
	// HTTP endpoints above; this socket exists only to push event-detail
	// updates (positions, tracks, events, run+teams) on change.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAnyRole(auth.RoleAdmin, auth.RoleOrgAdmin, auth.RoleOrgMember))
		r.Get("/ws/arrange", adminH.WSArrange)
	})

	// Admin / org-user surface (staff-authenticated). RequireCSRF runs
	// after RequireAnyRole so the 401 for "no session" still wins
	// (pre-auth callers don't care about the CSRF distinction).
	r.Route("/api/admin", func(r chi.Router) {
		r.Use(auth.RequireAnyRole(auth.RoleAdmin, auth.RoleOrgAdmin, auth.RoleOrgMember))
		r.Use(auth.RequireCSRF())

		r.Get("/adventures", adminH.ListAdventures)
		r.Get("/adventures/{slug}", adminH.AdventureDetail)
		// Preview: render any puzzle in the shape /play expects so an
		// arranger can QA the adventure without a real run.
		r.Get("/adventures/{slug}/preview", adminH.PreviewAdventure)
		// Live markdown → HTML for the arrange editor's preview panes.
		// Pure function; no DB access, no org scoping needed.
		r.Post("/markdown/preview", adminH.PreviewMarkdown)

		r.Get("/events", adminH.ListEvents)
		r.Post("/events", adminH.CreateEvent)
		r.Get("/events/{eventID}", adminH.EventDetail)
		r.Patch("/events/{eventID}", adminH.UpdateEvent)
		r.Post("/events/{eventID}/start", adminH.StartEvent)
		r.Post("/events/{eventID}/pause", adminH.PauseEvent)
		r.Post("/events/{eventID}/resume", adminH.ResumeEvent)
		r.Post("/events/{eventID}/cancel", adminH.CancelEvent)
		r.Post("/events/{eventID}/teams", adminH.CreateTeam)
		r.Post("/events/{eventID}/teams/bulk", adminH.BulkCreateTeams)
		// Team-roster file import — multipart upload, returns a
		// parsed preview the client then submits to /teams/bulk
		// after the arranger inspects it.
		r.Post("/events/{eventID}/teams/import", adminH.ImportTeams)
		r.Get("/events/{eventID}/positions", adminH.ListPositions)
		r.Get("/events/{eventID}/activity", adminH.ListActivity)
		r.Get("/events/{eventID}/tracks", adminH.ListTracks)
		r.Get("/events/{eventID}/results.csv", adminH.ExportEventResults)
		// Branded recap PDF — same data, different shape: a polished
		// post-event artifact arrangers can hand to their stakeholders.
		// Mounted alongside the CSV so the same authorize-event check
		// gates both.
		r.Get("/events/{eventID}/recap.pdf", adminH.ExportEventRecapPDF)
		// iCalendar invite — arrangers download once and forward to
		// participants so the event lands on everyone's calendar.
		r.Get("/events/{eventID}/invite.ics", adminH.ExportEventInviteICS)
		// Send-invitations: proactive variant of the recover-link
		// flow. Iterates teams with non-empty contact emails and
		// mails each one their own slot links + the .ics. Same
		// authorize-event gate as the export endpoints — Race directors
		// (org_member) can invoke it; this is "running the game",
		// not authoring.
		r.Post("/events/{eventID}/invitations/send", adminH.SendEventInvitations)

		r.Patch("/teams/{teamID}", adminH.UpdateTeam)
		r.Delete("/teams/{teamID}", adminH.DeleteTeam)
		r.Post("/teams/{teamID}/avatar", adminH.SetTeamAvatar)
		r.Delete("/teams/{teamID}/avatar", adminH.DeleteTeamAvatar)
		r.Post("/teams/{teamID}/slots", adminH.AddSlot)
		r.Delete("/teams/{teamID}/slots/{slotID}", adminH.RemoveSlot)
		r.Post("/teams/{teamID}/slots/{slotID}/rotate", adminH.RotateSlotToken)
		r.Post("/teams/{teamID}/unlock", adminH.UnlockTeam)
		// Withdraw / unwithdraw soft-flag a team that gave up mid-event
		// without deleting them — they stay in the recap as
		// "withdrew at puzzle 4" instead of vanishing from history.
		r.Post("/teams/{teamID}/withdraw", adminH.WithdrawTeam)
		r.Post("/teams/{teamID}/unwithdraw", adminH.UnwithdrawTeam)
		// Per-team resend of the invitation email. Re-uses the team's
		// existing slot tokens (no re-mint), so opening already-
		// claimed sessions stays valid. Players who lost their session
		// state should hit the public lost-magic-link recovery path
		// instead — that's the explicit re-mint flow.
		r.Post("/teams/{teamID}/invitations/send", adminH.SendTeamInvitation)

		// Staff password reset (admins & org users).
		r.Post("/users/{userID}/password", adminH.ResetPassword)
		// Admin TOTP factory-reset — recovery path for users who lost
		// both their authenticator and every recovery code. Same
		// authorization rules as the password reset above; requires
		// X-Confirm-Password from the acting admin.
		r.Post("/users/{userID}/totp/disable", adminH.AdminDisableUserTOTP)
		// Username typeahead for the "Add user to org" modal — admin
		// / org_admin only. Returns up to 10 prefix matches that
		// aren't admins and aren't already members of the target org.
		r.Get("/users/search", adminH.SearchUsers)

		// Caller's own assignments list.
		r.Get("/assignments", adminH.ListMyAssignments)

		// Org admin or global admin: manage an organization's members + a
		// detail endpoint both admins and in-org users can read.
		r.Get("/organizations/{orgID}", adminH.GetOrganization)
		r.Post("/organizations/{orgID}/members", adminH.CreateOrgMember)
		r.Delete("/organizations/{orgID}/members/{userID}", adminH.DeleteOrgMember)
		// Atomic ownership transfer — promotes a member to org_admin
		// and demotes the calling org_admin to org_member in one tx.
		r.Post("/organizations/{orgID}/transfer-ownership", adminH.TransferOwnership)

		// Adventure authoring. Available to every authoring tier —
		// handlers enforce ownership inline (admin edits the global
		// catalog; org users edit their own org's adventures).
		r.Get("/adventures/{slug}/edit", adminH.LoadAdventureForEdit)
		r.Post("/adventures", adminH.CreateAdventure)
		r.Patch("/adventures/{slug}", adminH.UpdateAdventure)
		r.Delete("/adventures/{slug}", adminH.DeleteAdventure)
		r.Post("/adventures/{slug}/avatar", adminH.SetAdventureAvatar)
		r.Delete("/adventures/{slug}/avatar", adminH.DeleteAdventureAvatar)
		// Clone a globally-visible adventure (catalog row or template)
		// into the caller's org. Authoring-tier action: org_admin or
		// admin only. See internal/admin/duplicate.go for the
		// transactional copy logic.
		r.Post("/adventures/{slug}/duplicate", adminH.DuplicateAdventure)
		r.Get("/adventures/{slug}/shares", adminH.ListAdventureShares)
		r.Post("/adventures/{slug}/shares", adminH.GrantAdventureShare)
		r.Delete("/adventures/{slug}/shares/{orgID}", adminH.RevokeAdventureShare)
		r.Post("/adventures/{slug}/pages", adminH.CreatePage)
		r.Patch("/adventures/{slug}/pages/reorder", adminH.ReorderPages)
		r.Post("/adventures/{slug}/pages/copy-from/{pageID}", adminH.CopyPuzzleToAdventure)
		r.Patch("/pages/{pageID}", adminH.UpdatePage)
		r.Delete("/pages/{pageID}", adminH.DeletePage)
		// Duplicate one puzzle in place — Google Slides shape. The
		// new row lands at sourceIdx + 1 with the rest of the
		// adventure shifted down. Auth via requireEditPage so the
		// org_member tightening carries through.
		r.Post("/pages/{pageID}/duplicate", adminH.DuplicatePage)
		r.Post("/pages/{pageID}/images", adminH.AddPageImage)
		r.Patch("/images/{imageID}", adminH.UpdatePageImage)
		r.Delete("/images/{imageID}", adminH.DeletePageImage)
		// Per-hint image attachments. Hint images are addressed by
		// their own image id under /hint-images/ so the puzzle-image
		// patch/delete routes don't have to disambiguate which kind
		// they're acting on.
		r.Post("/hints/{hintID}/images", adminH.AddHintImage)
		r.Patch("/hint-images/{imageID}", adminH.UpdateHintImage)
		r.Delete("/hint-images/{imageID}", adminH.DeleteHintImage)
		// Audio attachments — same shape as the image routes. PATCH
		// only carries `description` (the WCAG transcript fallback);
		// the blob itself is replaced by delete + re-upload.
		r.Post("/pages/{pageID}/audio", adminH.AddPageAudio)
		r.Patch("/audio/{audioID}", adminH.UpdatePageAudio)
		r.Delete("/audio/{audioID}", adminH.DeletePageAudio)
		r.Post("/hints/{hintID}/audio", adminH.AddHintAudio)
		r.Patch("/hint-audio/{audioID}", adminH.UpdateHintAudio)
		r.Delete("/hint-audio/{audioID}", adminH.DeleteHintAudio)

		// Puzzle library (cross-adventure pool).
		r.Get("/puzzles", adminH.ListPuzzles)
		r.Post("/puzzles", adminH.CreateLibraryPuzzle)
		r.Get("/puzzles/{pageID}", adminH.GetPuzzle)

		// Purchases: arrangers buy player-slot assignments here. The
		// simulated flow just needs the confirm endpoint; a future
		// Stripe integration will also hit /confirm from a webhook.
		r.Post("/purchases", adminH.CreatePurchase)
		r.Get("/purchases", adminH.ListPurchases)
		r.Get("/purchases/{purchaseID}", adminH.GetPurchase)
		r.Post("/purchases/{purchaseID}/confirm", adminH.ConfirmPurchase)

		// Auth audit feed. The handler scopes per role: admins see
		// every event, org_admins see only their org's users, anyone
		// else gets 403. Mounted here (not inside the admin-only
		// group below) so org_admins reach it.
		r.Get("/auth-events", adminH.ListAuthEvents)

		// Account settings — every logged-in staff user can change
		// their own password / email / delete themselves. No role
		// gate beyond the surrounding RequireAnyRole.
		r.Post("/me/password", adminH.ChangeMyPassword)
		r.Post("/me/email", adminH.ChangeMyEmail)
		// Revertable email change: ConfirmEmailChange stashes the old
		// address; this endpoint reads the status and reverts within
		// the grace window.
		r.Get("/me/email/revert-status", adminH.MyEmailChangeStatus)
		r.Post("/me/email/revert", adminH.RevertMyEmailChange)
		r.Delete("/me", adminH.DeleteMyAccount)
		// Account deletion is soft — DeleteMyAccount sets a 7-day
		// timer; this endpoint clears it before the sweeper runs.
		r.Post("/me/cancel-deletion", adminH.CancelMyDeletion)
		// Stripe Customer Portal: hosted UI for the org's payment
		// method, invoices, and cancellations. Returns a one-shot
		// session URL the client redirects to.
		r.Post("/me/portal-session", adminH.CreatePortalSession)
		// GDPR data export — JSON dump of personal + (when scoped)
		// org data, served as a downloadable file.
		r.Get("/me/export", adminH.ExportMyData)
		// Self audit feed — every logged-in user can see their own
		// recent login / password / email events for transparency.
		r.Get("/me/audit", adminH.ListMyAuthEvents)
		// Active sessions: list + per-token revoke + log-out-everywhere.
		r.Get("/me/sessions", adminH.ListMySessions)
		r.Delete("/me/sessions/{token}", adminH.RevokeMySession)
		r.Delete("/me/sessions", adminH.RevokeMyOtherSessions)
		// TOTP 2FA: status, enrol (setup → confirm), disable, and
		// recovery-code regenerate. Disable + regenerate require
		// X-Confirm-Password reauth.
		r.Get("/me/totp", adminH.TOTPStatus)
		r.Post("/me/totp/setup", adminH.SetupTOTP)
		r.Post("/me/totp/confirm", adminH.ConfirmTOTP)
		r.Post("/me/totp/disable", adminH.DisableTOTP)
		r.Post("/me/totp/recovery-codes/regenerate", adminH.RegenerateTOTPRecoveryCodes)

		// Outbound webhooks: per-org subscription CRUD. Org-scoped
		// (handlers reject admins without an org). The kinds catalog
		// is read-only for clients to drive their UI.
		r.Get("/webhooks", adminH.ListWebhooks)
		r.Post("/webhooks", adminH.CreateWebhook)
		r.Patch("/webhooks/{endpointID}", adminH.UpdateWebhook)
		r.Delete("/webhooks/{endpointID}", adminH.DeleteWebhook)
		r.Post("/webhooks/{endpointID}/rotate-secret", adminH.RotateWebhookSecret)
		r.Post("/webhooks/{endpointID}/test", adminH.SendTestWebhook)
		r.Get("/webhook-event-kinds", adminH.ListWebhookEventKinds)
		// Delivery log: every attempt, statuses, errors, the raw
		// payload bytes. Org-scoped via the join to webhook_endpoints
		// inside the handler.
		r.Get("/webhooks/deliveries", adminH.ListWebhookDeliveries)

		// Global admin only.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdmin))
			r.Get("/organizations", adminH.ListOrganizations)
			r.Post("/organizations", adminH.CreateOrganization)
			r.Patch("/organizations/{orgID}", adminH.UpdateOrganization)
			r.Post("/organizations/{orgID}/trusted", adminH.SetOrgTrusted)
			r.Delete("/organizations/{orgID}", adminH.DeleteOrganization)
			// Brand logo upload — surfaced on the recap PDF cover and
			// (later) on the play / arrange chrome. PNG/JPEG/WebP only,
			// re-encoded by imageutil.Shrink.
			r.Post("/organizations/{orgID}/logo", adminH.SetOrgLogo)
			r.Delete("/organizations/{orgID}/logo", adminH.DeleteOrgLogo)
			r.Post("/organizations/{orgID}/assignments", adminH.CreateAssignment)
			r.Patch("/organizations/{orgID}/assignments/{assignmentID}", adminH.UpdateAssignment)
			r.Delete("/organizations/{orgID}/assignments/{assignmentID}", adminH.DeleteAssignment)

			r.Get("/admins", adminH.ListAdmins)
			r.Post("/admins", adminH.CreateAdmin)
			r.Delete("/admins/{userID}", adminH.DeleteAdmin)

			// Operator-tunable runtime settings. Admin-only because
			// these knobs affect everyone hitting the deployment —
			// public signup, the marketing landing, etc.
			r.Get("/settings", adminH.GetSettings)
			r.Put("/settings/signup", adminH.SetSignupEnabled)
			r.Put("/settings/landing", adminH.SetLandingEnabled)

			// Read-only "what's wired in?" panel — surfaces the
			// build's pluggable seams (mailer/payments/etc.) and
			// whether each is enabled in this deployment. Drives the
			// Settings → Installed components card.
			r.Get("/components", adminH.ListComponents)

			// Software-update surface: cached snapshot of the upstream
			// CHANGELOG.md plus an on-demand "check now" trigger. The
			// background poll runs every 24h regardless of the GET; the
			// POST exists so an operator clicking Check now in the UI
			// gets a fresh answer in one round trip.
			r.Get("/system-update", adminH.SystemUpdate)
			r.Post("/system-update/check", adminH.SystemUpdateCheck)

			// Manual email verification — admin escape hatch when an
			// arranger never received the self-serve verification mail.
			// Idempotent + audit-logged in auth_events as
			// event=email_verify reason=admin_manual.
			r.Post("/users/{userID}/verify-email", adminH.VerifyMemberEmail)

			// Test-email knob: lets the operator verify SMTP config
			// before customers hit the broken signup flow. Body just
			// names a recipient — content is fixed server-side so
			// this can't be repurposed as a spam relay.
			r.Post("/send-test-email", adminH.SendTestEmail)

			// Refunds touch real money — admin-only. The handler
			// just calls Stripe; the actual state flip happens via
			// the charge.refunded webhook so the same code path
			// works whether the refund originated here or in the
			// Stripe Dashboard.
			r.Post("/purchases/{purchaseID}/refund", adminH.RefundPurchase)
			// Off-platform invoice tracking — admin marks an invoice
			// purchase as billed once the real invoice has been sent.
			// The Purchases page's "to bill" filter ignores billed
			// rows so the same row doesn't keep nagging.
			r.Post("/purchases/{purchaseID}/billed", adminH.SetPurchaseBilled)
			// "Makulera" — drop the row out of the to-bill filter
			// without unwinding the assignment grant. Idempotent.
			r.Post("/purchases/{purchaseID}/void", adminH.VoidPurchase)
			// Admin-on-behalf-of grant: add player slots to an org's
			// event. Free for non-trusted orgs (forced); for trusted
			// orgs the admin chooses zero (goodwill) or a positive
			// amount routed via the invoice provider.
			r.Post("/events/{eventID}/grant-slots", adminH.GrantSlotsToEvent)

			// Server logs — stream + level control. Admin-only so org
			// users can't snoop another org's messages.
			r.Get("/logs", adminH.ListLogs)
			r.Get("/logs/level", adminH.LogsLevel)
			r.Put("/logs/level", adminH.SetLogsLevel)

		})

		// Race simulator. Open to arrangers (org_admin/org_member) plus
		// global admins — each call is org-scoped via the caller's
		// session, so two arrangers in different orgs can run sims in
		// parallel without colliding.
		r.Get("/simulator", adminH.SimulatorStatus)
		r.Post("/simulator/start", adminH.SimulatorStart)
		r.Post("/simulator/pause", adminH.SimulatorPause)
		r.Post("/simulator/resume", adminH.SimulatorResume)
		r.Post("/simulator/reset", adminH.SimulatorReset)
	})

	// Log stream socket. Lives outside the /api/admin route tree so it
	// can use the WS auth path the other sockets do. Admin-only.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdmin))
		r.Get("/ws/logs", adminH.WSLogs)
	})

	// The simulated-checkout HTML page. Lives outside /api/admin so the
	// arranger's browser lands at a clean URL after clicking "Pay".
	// With Stripe this becomes a redirect target from Checkout Sessions.
	r.Get("/purchase/{purchaseID}", adminH.SimulatedCheckoutPage)

	return r
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
//     form handlers, page-load theme application). The maps on
//     /play, /arrange, /live, /recap pull Leaflet from unpkg
//     at runtime; whitelisting the host (rather than 'self'-
//     hosting the JS file) keeps the embed footprint small.
//     Tightening to nonces is a real refactor; leaving this as
//     a known-loose-but-bounded surface for now.
//   - style-src 'self' 'unsafe-inline' https://fonts.googleapis.com
//     https://unpkg.com — Google Fonts stylesheet + inline
//     `style="..."` from the existing DOM helpers + Leaflet's
//     CSS from the same unpkg URL.
//   - img-src 'self' data: blob: https: — data: for QR / canvas
//     downscale, blob: for QR-decoder + audio clips, https: so
//     adventure assets / map tiles (OpenStreetMap) work.
//   - font-src 'self' https://fonts.gstatic.com — Google Fonts
//     binary fonts.
//   - connect-src 'self' wss: ws: — XHR / fetch / WebSocket all
//     same-origin; wss/ws because the WebSocket scheme isn't
//     covered by 'self' alone in older browsers.
//   - media-src 'self' blob: — audio attachments are blob URLs.
//   - frame-ancestors 'none' — blocks the clickjacking surface on
//     /team/{token}, /recap/{token}, /play, /arrange.
//   - object-src 'none' — no Flash / Java / legacy plugin embed.
//   - base-uri 'self' — pin <base> so a stored-XSS can't redirect
//     relative URLs.
const baselineCSP = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline' https://unpkg.com; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://unpkg.com; " +
	"img-src 'self' data: blob: https:; " +
	"font-src 'self' https://fonts.gstatic.com; " +
	"connect-src 'self' wss: ws:; " +
	"media-src 'self' blob:; " +
	"frame-ancestors 'none'; " +
	"object-src 'none'; " +
	"base-uri 'self'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", baselineCSP)
		h.Set("X-Frame-Options", "DENY")
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

// serveAsset serves an embedded asset with an explicit Content-Type.
// Used for the shared i18n.js so all three HTML pages can load a single
// copy instead of duplicating the dictionary per file.
func serveAsset(path, contentType string) http.HandlerFunc {
	body, err := readWebFile(path)
	if err != nil {
		panic("embed: " + err.Error())
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	}
}

// versionResp is the /api/version payload. Stable shape so monitoring
// scripts can pin to specific fields; new fields are additive only.
type versionResp struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	Time      string `json:"time,omitempty"`
	Modified  bool   `json:"modified,omitempty"`
	Go        string `json:"go,omitempty"`
	OS        string `json:"os,omitempty"`
	Arch      string `json:"arch,omitempty"`
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
