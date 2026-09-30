// Page + meta routes — the SPA shells, marketing pages, custom 404,
// and the small "tell me about this server" endpoints (health,
// version, MCP). Wired by registerPageRoutes / registerMetaRoutes,
// both invoked from New() in server.go.
//
// Order in the original New() is preserved at the call site so a
// future contributor reading the route table top-to-bottom sees
// the same shape they did before the split.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/mcp"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/settings"
)

// registerMetaRoutes wires /api/health, /api/version, and the
// optional /mcp endpoint. Each is a thin status / readiness probe;
// none of them touch the auth or CSRF middleware.
func registerMetaRoutes(r chi.Router, env *routesEnv) {
	cfg := env.cfg

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

	// MCP (Model Context Protocol) endpoint. Off by default — flips
	// on for self-host operators who set ZV_MCP_ENABLED=1 in their
	// .env. Auth is bearer-token, distinct from the cookie+CSRF
	// stack the rest of /api/admin uses, since MCP clients (Claude
	// desktop, claude.ai connectors) speak Authorization headers.
	if mcp.Enabled() {
		mcpSrv := mcp.New(cfg.DB, cfg.Version, env.storage, env.adminH)
		r.Method(http.MethodPost, "/mcp", mcpSrv.Handler())
	}

	// Build info, computed once at startup (debug.ReadBuildInfo is
	// stable for the lifetime of the process). Lets operators confirm
	// which build is serving after an upgrade — `curl /api/version`
	// returns the tagged version, VCS revision, and commit time.
	verResp := env.verResp
	r.Get("/api/version", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(verResp)
	})
}

// registerPageRoutes wires the SPA shells (/play, /arrange), the
// marketing surface (/, /manual, /pricing), the legal templates
// (/privacy, /terms), and the branded 404 fallback.
func registerPageRoutes(r chi.Router, env *routesEnv) {
	cfg := env.cfg
	playerH := env.playerH

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
		renderLandingPage(w, req, cfg.PublicURL)
	})

	// Custom 404 with branded chrome and a "back to home / read manual"
	// fallback. chi serves a plain "404 page not found" otherwise,
	// which feels broken from a marketing-site perspective.
	r.NotFound(render404)

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
		// A team whose event runs on the graph engine plays in the React
		// app. Deciding here keeps every magic link ever sent working:
		// the link still says /play, and lane events still get play.html.
		if slot, ok := auth.SlotFromContext(req.Context()); ok {
			if uses, err := play.UsesGraph(req.Context(), cfg.DB, slot.EventID); err == nil && uses {
				http.Redirect(w, req, "/app/play/", http.StatusFound)
				return
			}
		}
		servePage("web/play.html")(w, req)
	})
	// The console lives at /app/arrange/. /arrange redirects there with its
	// query (Stripe's ?stripe_connect=return) kept; the browser carries
	// the #hash across the redirect, and the new console reads the old
	// hashes, so every emailed or bookmarked link still lands.
	r.Get("/arrange", func(w http.ResponseWriter, req *http.Request) {
		target := "/app/arrange/"
		if req.URL.RawQuery != "" {
			target += "?" + req.URL.RawQuery
		}
		http.Redirect(w, req, target, http.StatusFound)
	})
	// The classic console, kept reachable while its list-based adventure
	// editor has no replacement decided.
	r.Get("/arrange/classic", servePage("web/arrange.html"))

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

	// Operator's static shop from ZV_WEB_OVERRIDE_DIR/shop; see shop.go.
	r.Get("/shop", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/shop/", http.StatusMovedPermanently)
	})
	r.Get("/shop/*", serveShop)

	r.Get("/privacy", func(w http.ResponseWriter, _ *http.Request) {
		renderLegalPage(w, "privacy.html", cfg.Legal)
	})
	r.Get("/terms", func(w http.ResponseWriter, _ *http.Request) {
		renderLegalPage(w, "terms.html", cfg.Legal)
	})
}
