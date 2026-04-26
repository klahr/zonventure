// Package server wires the HTTP router, middleware, and per-package
// handlers into one http.Handler.
package server

import (
	"context"
	"database/sql"
	"embed"
	"net/http"
	"strings"
	"time"

	"git.sr.ht/~klahr/zv/internal/admin"
	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/mailer"
	"git.sr.ht/~klahr/zv/internal/metrics"
	"git.sr.ht/~klahr/zv/internal/player"
	"git.sr.ht/~klahr/zv/internal/public"
	"git.sr.ht/~klahr/zv/internal/simulator"
	"git.sr.ht/~klahr/zv/internal/ws"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed web/play.html web/arrange.html web/live.html web/i18n.js web/favicon.svg web/privacy.html web/terms.html web/manual.html web/landing.html web/pricing.html web/404.html web/robots.txt web/sitemap.xml
var webFS embed.FS

// Config is the bag of knobs New needs.
type Config struct {
	DB           *sql.DB
	Library      *adventure.Library
	Hub          *ws.Hub
	SecureCookie bool
	// SimulatorEnabled toggles the race simulator. Off in production
	// so an admin can't flatten a real run by accident; on in dev by
	// setting ZV_SIMULATOR=1 on the server binary.
	SimulatorEnabled bool
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
}

// New builds the HTTP router.
func New(cfg Config) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
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
		Features:      auth.Features{SimulatorEnabled: cfg.SimulatorEnabled},
		Pricing:       auth.Pricing{SlotCents: admin.PricePerSlotCents, Currency: "EUR"},
		Limiter:       auth.NewLoginLimiter(),
		ForgotLimiter: auth.NewLoginLimiter(),
		Mailer:        cfg.Mailer,
		PublicURL:     cfg.PublicURL,
	}
	playerH := &player.Handlers{DB: cfg.DB, Lib: cfg.Library, Hub: hub, SecureCookie: cfg.SecureCookie}
	// Simulator instance always exists (package isn't huge, avoids
	// null-check scatter); the ROUTES are gated on SimulatorEnabled
	// below so disabled deployments simply 404 on /api/admin/simulator.
	sim := simulator.New(cfg.DB, cfg.Library, hub)
	mail := cfg.Mailer
	if mail == nil {
		mail = mailer.LogMailer{}
	}
	adminH := &admin.Handlers{
		DB: cfg.DB, Lib: cfg.Library, Hub: hub, Sim: sim,
		SimulatorEnabled:    cfg.SimulatorEnabled,
		Mailer:              mail,
		PublicURL:           cfg.PublicURL,
		StripeSecretKey:     cfg.StripeSecretKey,
		StripeWebhookSecret: cfg.StripeWebhookSecret,
		SecureCookie:        cfg.SecureCookie,
	}
	publicH := &public.Handlers{DB: cfg.DB, Lib: cfg.Library, Hub: hub}

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
		// Team-token-authed avatar upload/delete so the team can change
		// their own profile picture from the share page. The token IS the
		// auth; no staff session required.
		r.Post("/api/public/team/{token}/avatar", publicH.SetTeamAvatarByToken)
		r.Delete("/api/public/team/{token}/avatar", publicH.DeleteTeamAvatarByToken)
	})

	// Image serving: permission is decided inside the handler (staff
	// sessions always pass; slot sessions only when they're on the page).
	r.Get("/adventure-assets/image/{imageID}", adminH.ServeImage)
	// Team avatars are intentionally open — they surface on the public
	// live board and on player views without a staff session. The team
	// id is opaque enough to be acceptable public read-only data.
	r.Get("/adventure-assets/team-avatar/{teamID}", adminH.ServeTeamAvatar)
	// Adventure cover pictures — same public-read rationale: the slug is
	// already in the URL, no extra sensitive metadata is leaked.
	r.Get("/adventure-assets/avatar/{slug}", adminH.ServeAdventureAvatar)

	// Staff authentication. CSRF-protected — the user gets a zv_csrf
	// cookie from the /arrange page GET that hosts the login form, so
	// the form's POST can echo it back. A cross-origin "log my victim
	// into my account" attempt has no way to read the cookie value.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireCSRF())
		r.Post("/api/auth/login", authH.Login)
		r.Post("/api/auth/signup", authH.Signup)
		r.Post("/api/auth/set-password", authH.SetPassword)
		r.Post("/api/auth/logout", authH.Logout)
		r.Post("/api/auth/resend-verification", authH.ResendVerification)
		// Forgot/reset are pre-auth: a user who can't log in is
		// exactly the audience here. Both still need CSRF (the
		// /arrange page seeds the cookie before the form submits).
		r.Post("/api/auth/forgot", authH.ForgotPassword)
		r.Post("/api/auth/reset", authH.ResetPassword)
	})
	r.Get("/api/auth/whoami", authH.Whoami)
	// Email verification clicks land on a GET; not CSRF-relevant.
	r.Get("/verify", authH.VerifyEmail)

	// Legal pages — public, cached at the proxy by Cache-Control.
	// Both pages are clearly-marked drafts until the operator sets
	// the ZV_* env vars and a lawyer signs off on the body.
	// Public arranger manual. Served as plain HTML so AI agents,
	// search crawlers and curl users can read the content without JS
	// or auth. The in-app /arrange#manual view is a translated mirror
	// of the same content for logged-in arrangers.
	r.Get("/manual", servePublicPage("web/manual.html", http.StatusOK))
	r.Get("/pricing", servePublicPage("web/pricing.html", http.StatusOK))

	// Crawler hygiene: robots.txt and a small sitemap of public URLs.
	// {{HOST}} in the sitemap is rewritten with the operator-configured
	// PublicURL — without it the URLs render origin-relative, which
	// most crawlers handle but a few (Bing especially) prefer absolute.
	r.Get("/robots.txt", serveAsset("web/robots.txt", "text/plain; charset=utf-8"))
	sitemapBody, err := webFS.ReadFile("web/sitemap.xml")
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
		r.Post("/answer", playerH.SubmitAnswer)
		r.Post("/location", playerH.PostLocation)
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
		r.Post("/events/{eventID}/teams", adminH.CreateTeam)
		r.Get("/events/{eventID}/positions", adminH.ListPositions)
		r.Get("/events/{eventID}/activity", adminH.ListActivity)
		r.Get("/events/{eventID}/tracks", adminH.ListTracks)
		r.Get("/events/{eventID}/results.csv", adminH.ExportEventResults)

		r.Patch("/teams/{teamID}", adminH.RenameTeam)
		r.Delete("/teams/{teamID}", adminH.DeleteTeam)
		r.Post("/teams/{teamID}/avatar", adminH.SetTeamAvatar)
		r.Delete("/teams/{teamID}/avatar", adminH.DeleteTeamAvatar)
		r.Post("/teams/{teamID}/slots", adminH.AddSlot)
		r.Delete("/teams/{teamID}/slots/{slotID}", adminH.RemoveSlot)
		r.Post("/teams/{teamID}/slots/{slotID}/rotate", adminH.RotateSlotToken)
		r.Post("/teams/{teamID}/unlock", adminH.UnlockTeam)

		// Staff password reset (admins & org users).
		r.Post("/users/{userID}/password", adminH.ResetPassword)

		// Caller's own assignments list.
		r.Get("/assignments", adminH.ListMyAssignments)

		// Org admin or global admin: manage an organization's members + a
		// detail endpoint both admins and in-org users can read.
		r.Get("/organizations/{orgID}", adminH.GetOrganization)
		r.Post("/organizations/{orgID}/members", adminH.CreateOrgMember)
		r.Delete("/organizations/{orgID}/members/{userID}", adminH.DeleteOrgMember)

		// Adventure authoring. Available to every authoring tier —
		// handlers enforce ownership inline (admin edits the global
		// catalog; org users edit their own org's adventures).
		r.Get("/adventures/{slug}/edit", adminH.LoadAdventureForEdit)
		r.Post("/adventures", adminH.CreateAdventure)
		r.Patch("/adventures/{slug}", adminH.UpdateAdventure)
		r.Delete("/adventures/{slug}", adminH.DeleteAdventure)
		r.Post("/adventures/{slug}/avatar", adminH.SetAdventureAvatar)
		r.Delete("/adventures/{slug}/avatar", adminH.DeleteAdventureAvatar)
		r.Get("/adventures/{slug}/shares", adminH.ListAdventureShares)
		r.Post("/adventures/{slug}/shares", adminH.GrantAdventureShare)
		r.Delete("/adventures/{slug}/shares/{orgID}", adminH.RevokeAdventureShare)
		r.Post("/adventures/{slug}/pages", adminH.CreatePage)
		r.Patch("/adventures/{slug}/pages/reorder", adminH.ReorderPages)
		r.Post("/adventures/{slug}/pages/copy-from/{pageID}", adminH.CopyPuzzleToAdventure)
		r.Patch("/pages/{pageID}", adminH.UpdatePage)
		r.Delete("/pages/{pageID}", adminH.DeletePage)
		r.Post("/pages/{pageID}/images", adminH.AddPageImage)
		r.Delete("/images/{imageID}", adminH.DeletePageImage)

		// Puzzle library (cross-adventure pool).
		r.Get("/puzzles", adminH.ListPuzzles)
		r.Post("/puzzles", adminH.CreateLibraryPuzzle)
		r.Get("/puzzles/{pageID}", adminH.GetPuzzle)

		// Purchases: arrangers buy team-slot assignments here. The
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
			r.Delete("/organizations/{orgID}", adminH.DeleteOrganization)
			r.Post("/organizations/{orgID}/assignments", adminH.CreateAssignment)
			r.Patch("/organizations/{orgID}/assignments/{assignmentID}", adminH.UpdateAssignment)
			r.Delete("/organizations/{orgID}/assignments/{assignmentID}", adminH.DeleteAssignment)

			r.Get("/admins", adminH.ListAdmins)
			r.Post("/admins", adminH.CreateAdmin)
			r.Delete("/admins/{userID}", adminH.DeleteAdmin)

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

			// Server logs — stream + level control. Admin-only so org
			// users can't snoop another org's messages.
			r.Get("/logs", adminH.ListLogs)
			r.Get("/logs/level", adminH.LogsLevel)
			r.Put("/logs/level", adminH.SetLogsLevel)

			// Race simulator. Admin-only because it writes runs/teams
			// on behalf of the first org and can take the DB on a walk.
			// Routes only mount when the operator has explicitly opted
			// in via the ZV_SIMULATOR env var, so production admins
			// can't spin up a simulated race by accident.
			if cfg.SimulatorEnabled {
				r.Get("/simulator", adminH.SimulatorStatus)
				r.Post("/simulator/start", adminH.SimulatorStart)
				r.Post("/simulator/pause", adminH.SimulatorPause)
				r.Post("/simulator/resume", adminH.SimulatorResume)
				r.Post("/simulator/reset", adminH.SimulatorReset)
			}
		})
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
func servePage(path string) http.HandlerFunc {
	body, err := webFS.ReadFile(path)
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
	body, err := webFS.ReadFile(path)
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
	body, err := webFS.ReadFile(path)
	if err != nil {
		panic("embed: " + err.Error())
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	}
}
