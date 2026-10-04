// Public token routes — every URL where the token IS the auth.
// /live, /recap, /team, /qr/team, /store, /o/{slug} and their
// supporting JSON APIs. Wired by registerPublicTokenRoutes; called
// from New() in server.go.
package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// registerPublicTokenRoutes wires the public-by-unguessable-token
// surface inside one chi.Group so a single per-IP rate limiter
// covers every endpoint. The limiter cap is generous — a real
// viewer with reconnect storms / tab refreshes / sharing the page
// to a chat group should never trip it; the cap is there for
// scripts brute-forcing tokens or DoSing the public endpoints.
func registerPublicTokenRoutes(r chi.Router, env *routesEnv) {
	cfg := env.cfg
	publicH := env.publicH
	adminH := env.adminH

	// Public, no-auth read-only endpoints. Tokens are unguessable but
	// the routes still hit the database; rate-limit per IP so a script
	// can't brute-force or DoS them. Caps generously so a real viewer
	// (with reconnect storms, tab refreshes, etc.) never trips it.
	r.Group(func(r chi.Router) {
		r.Use(publicRLMiddleware(env.publicRL))
		// Token URLs must not be crawled. The page itself carries
		// `<meta name="robots" content="noindex, nofollow">`; the
		// header is the belt to that suspenders, blocking indexers
		// that don't render JS but do honour the HTTP header.
		liveHandler := servePage("web/live.html")
		r.Get("/live/{token}", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
			liveHandler(w, req)
		})
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
		r.Get("/api/public/recap/{token}/photos/{photoID}", publicH.RecapPhoto)
		// Team-token-authed avatar upload/delete so the team can change
		// their own profile picture from the share page. The token IS the
		// auth; no staff session required.
		r.Post("/api/public/team/{token}/avatar", publicH.SetTeamAvatarByToken)
		r.Delete("/api/public/team/{token}/avatar", publicH.DeleteTeamAvatarByToken)
		// Scan-to-join: a player takes the next free seat on the team and
		// is signed in, so the self-guided page needs only one "Start"
		// button rather than a seat picker.
		r.Post("/api/public/team/{token}/join", publicH.JoinTeamByToken)

		// Public store. Listing + per-event detail are read-only and
		// served by publicH; the buy endpoint initiates a Stripe
		// Checkout session and lives on adminH because that's where
		// the Stripe credentials are wired (mirrors the StripeWebhook
		// admin/public split below).
		r.Get("/store", renderStoreListingPage)
		// Per-event detail page. Server-rendered (rather than SPA
		// in JS) so social-media previewers, search crawlers, and
		// AI agents see real OpenGraph metadata + the description
		// + the price as text in the response body. The template
		// is override-friendly: drop store-event.html into
		// ZV_WEB_OVERRIDE_DIR for a branded variant.
		//
		// Legacy / global path: 301 to the org-scoped canonical
		// (/o/{slug}/store/event/{id}) after looking up the event's
		// org. Old QR codes / emailed links / search-engine
		// snapshots all keep working through the redirect.
		r.Get("/store/event/{event_id}", func(w http.ResponseWriter, req *http.Request) {
			redirectLegacyStoreEvent(w, req, cfg.DB)
		})
		r.Get("/api/store/items", publicH.StoreList)
		r.Get("/api/store/items/{event_id}", publicH.StoreEvent)
		r.Post("/api/store/buy", adminH.BuyTicket)
		// Post-purchase handoff: the /store?paid=N&session_id=...
		// landing polls this every couple of seconds until the
		// webhook fulfills the purchase, then redirects the buyer
		// to /team/{token} so they never have to leave the page
		// to find their team link. session_id is the capability
		// check; mismatch returns 404.
		r.Get("/api/store/ticket/{purchase_id}/team-token", publicH.TicketTeamToken)

		// Per-org public surfaces. /o/{slug}/store + /o/{slug}/store/event/{id}
		// are the canonical buyer-facing URLs; the bare /store stays
		// as a global discovery page. The matching API endpoints
		// scope every query to the slug's org_id, refusing
		// cross-org event ids with a 404.
		r.Get("/o/{slug}", func(w http.ResponseWriter, req *http.Request) {
			renderOrgLandingPage(w, req, cfg.DB, cfg.PublicURL)
		})
		r.Get("/o/{slug}/store", func(w http.ResponseWriter, req *http.Request) {
			slug := chi.URLParam(req, "slug")
			// Confirm the org exists before painting the chrome —
			// otherwise a typo'd link renders a fully-formed page
			// with an empty grid, which is worse UX than 404.
			if !orgSlugExists(req.Context(), cfg.DB, slug) {
				servePublicPage("web/404.html", http.StatusNotFound)(w, req)
				return
			}
			org, err := loadOrgProfile(req.Context(), cfg.DB, strings.TrimSpace(slug))
			if err != nil {
				http.Error(w, "lookup failed", http.StatusInternalServerError)
				return
			}
			renderStoreListingPageScoped(w, req, &org)
		})
		r.Get("/o/{slug}/store/event/{event_id}", func(w http.ResponseWriter, req *http.Request) {
			renderStoreEventPage(w, req, cfg.DB, cfg.PublicURL)
		})
		r.Get("/api/o/{slug}/store/items", publicH.StoreListByOrg)
		r.Get("/api/o/{slug}/store/items/{event_id}", publicH.StoreEventByOrg)
		r.Post("/api/o/{slug}/store/buy", adminH.BuyTicketByOrg)

		// Gift box redemption (Live it, TrueStory). The bare /redeem
		// offers every organisation's products; /o/{slug}/redeem only
		// that organisation's. A redemption waits for an organiser to
		// check the code; see admin/redemptions.go.
		r.Get("/redeem", func(w http.ResponseWriter, req *http.Request) {
			renderRedeemPage(w, req, "")
		})
		r.Get("/o/{slug}/redeem", func(w http.ResponseWriter, req *http.Request) {
			slug := chi.URLParam(req, "slug")
			if !orgSlugExists(req.Context(), cfg.DB, slug) {
				servePublicPage("web/404.html", http.StatusNotFound)(w, req)
				return
			}
			renderRedeemPage(w, req, slug)
		})
		// Voucher redemption: the /play code box posts a code here and
		// gets back the team page to go to. Public + rate-limited (the
		// handler also shares the magic-link per-IP limiter), like the
		// other capability endpoints in this group.
		r.Post("/api/play/voucher", env.playerH.VoucherRedeem)

		r.Get("/api/redeem/options", adminH.RedeemOptions)
		r.Post("/api/redeem", adminH.Redeem)
		r.Get("/api/o/{slug}/redeem/options", adminH.RedeemOptionsByOrg)
		r.Post("/api/o/{slug}/redeem", adminH.RedeemByOrg)
	})
}
