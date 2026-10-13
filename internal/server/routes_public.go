// Public token routes — every URL where the token IS the auth.
// /live, /recap, /team, /qr/team, /store, /o/{slug} and their
// supporting JSON APIs. Wired by registerPublicTokenRoutes; called
// from New() in server.go.
package server

import (
	"net/http"
	"net/url"
	"strconv"
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
		// The organisers' files, by the team's token: the download links
		// in the ticket and invitation emails.
		r.Get("/team/{token}/files/{fileID}", publicH.TeamFile)
		// Print materials the event emails to its buyers, likewise.
		r.Get("/team/{token}/print/{material}", publicH.TeamPrint)
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
		// /store is the site's own shop: its home organisation's page,
		// or not found on a server that has none. There is no store of
		// every organisation's adventures.
		r.Get("/store", func(w http.ResponseWriter, req *http.Request) {
			if cfg.HomeOrg == "" {
				render404(w, req)
				return
			}
			redirectKeepingQuery(w, req, "/o/"+url.PathEscape(cfg.HomeOrg))
		})
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
		r.Get("/api/store/items/{event_id}", publicH.StoreEvent)
		r.Post("/api/store/buy", adminH.BuyTicket)
		// Post-purchase handoff: the /store?paid=N&session_id=...
		// landing polls this every couple of seconds until the
		// webhook fulfills the purchase, then redirects the buyer
		// to /team/{token} so they never have to leave the page
		// to find their team link. session_id is the capability
		// check; mismatch returns 404.
		r.Get("/api/store/ticket/{purchase_id}/team-token", publicH.TicketTeamToken)

		// Per-org public surfaces. /o/{slug} is the organisation's page —
		// who it is and all its adventures for sale — and
		// /o/{slug}/store/event/{id} each one's page. The matching API
		// endpoints scope every query to the slug's org_id, refusing
		// cross-org event ids with a 404. /o/{slug}/store, the shop's
		// old address, leads to the organisation's page.
		r.Get("/o/{slug}/store", func(w http.ResponseWriter, req *http.Request) {
			redirectKeepingQuery(w, req, "/o/"+url.PathEscape(chi.URLParam(req, "slug")))
		})
		r.Get("/o/{slug}", func(w http.ResponseWriter, req *http.Request) {
			slug := chi.URLParam(req, "slug")
			// Confirm the org exists before painting the chrome —
			// otherwise a typo'd link renders a fully-formed page
			// with an empty grid, which is worse UX than 404.
			if !orgSlugExists(req.Context(), cfg.DB, slug) {
				render404(w, req)
				return
			}
			org, err := loadOrgProfile(req.Context(), cfg.DB, strings.TrimSpace(slug))
			if err != nil {
				http.Error(w, "lookup failed", http.StatusInternalServerError)
				return
			}
			renderOrgPage(w, req, org)
		})
		r.Get("/o/{slug}/store/event/{event_id}", func(w http.ResponseWriter, req *http.Request) {
			// A listing that can't be bought (no payment provider, or no
			// Stripe account under a platform fee) isn't in the store.
			if id, err := strconv.ParseInt(chi.URLParam(req, "event_id"), 10, 64); err == nil && !publicH.ForSale(req.Context(), id) {
				render404(w, req)
				return
			}
			renderStoreEventPage(w, req, cfg.DB, cfg.PublicURL, env.simulatedProv != nil)
		})
		r.Get("/api/o/{slug}/store/items", publicH.StoreListByOrg)
		r.Get("/api/o/{slug}/store/items/{event_id}", publicH.StoreEventByOrg)
		r.Post("/api/o/{slug}/store/buy", adminH.BuyTicketByOrg)

		// Parties: an org's party offers, booked with no account, and
		// the host page the booker runs the party from. The host link's
		// secret is the auth, like a team link's; the pages are the
		// party app's, at the short URLs a mail or a QR code carries.
		r.Get("/api/o/{slug}/parties", adminH.ListPartiesByOrg)
		r.Get("/api/o/{slug}/parties/{offerID}", adminH.GetPartyByOrg)
		r.Post("/api/o/{slug}/parties/{offerID}/book", adminH.BookParty)
		r.Get("/o/{slug}/party/{offerID}", func(w http.ResponseWriter, req *http.Request) {
			redirectKeepingQuery(w, req, "/app/party/book/"+url.PathEscape(chi.URLParam(req, "slug"))+"/"+url.PathEscape(chi.URLParam(req, "offerID")))
		})
		r.Get("/party/{token}", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
			w.Header().Set("Referrer-Policy", "no-referrer")
			http.Redirect(w, req, "/app/party/host/"+url.PathEscape(chi.URLParam(req, "token")), http.StatusFound)
		})
		r.Post("/api/party/resend", adminH.ResendPartyLinks)
		r.Get("/api/party/{token}", adminH.GetHostParty)
		r.Delete("/api/party/{token}/teams/{teamID}", adminH.RemoveHostTeam)
		r.Post("/api/party/{token}/finish-game", adminH.FinishHostedGame)
		r.Put("/api/party/{token}/hold-results", adminH.PutHoldResults)
		r.Post("/api/party/{token}/send-player-link", adminH.SendHostedPlayerLink)
		r.Post("/api/party/{token}/replace-link", adminH.ReplaceHostLink)
		r.Post("/api/party/{token}/blocks", adminH.AddHostedBlocks)
		r.Get("/api/party/{token}/cards.pdf", adminH.HostedTrailPDF)
		r.Get("/api/party/{token}/cards", adminH.GetHostTrail)
		r.Put("/api/party/{token}/cards", adminH.PutHostTrail)
		r.Put("/api/party/{token}/cards/theme", adminH.PutTrailDraftTheme)
		r.Post("/api/party/{token}/keep", adminH.HostTrailKeep)
		r.Get("/api/party/{token}/qr", adminH.HostPartyQR)
		r.Put("/api/party/{token}/personalise", adminH.PutHostPersonalise)
		r.Put("/api/party/{token}/mode", adminH.PutHostMode)
		r.Put("/api/party/{token}/places/{slug}", adminH.PutHostPlace)
		r.Post("/api/party/{token}/places/shift", adminH.PostHostShift)
		r.Post("/api/party/{token}/{action}", adminH.HostPartyAction)

		// The player link of a hosted event: teams join from it, naming
		// themselves. Its secret is printed as a QR code on the cards.
		r.Get("/j/{token}", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
			w.Header().Set("Referrer-Policy", "no-referrer")
			http.Redirect(w, req, "/app/party/join/"+url.PathEscape(chi.URLParam(req, "token")), http.StatusFound)
		})
		r.Get("/api/join/{token}", adminH.GetJoin)
		r.Get("/api/join/{token}/cover", adminH.JoinCover)
		r.Post("/api/join/{token}", adminH.Join)

		// Quiz trails at the public door (admin/trails_public.go): made
		// with no account, opened by the draft's secret (a header, never
		// the URL) or an email session. The pages are the party app's;
		// the copied link to a draft carries its secret in the fragment,
		// which a redirect keeps.
		r.Get("/o/{slug}/trail", func(w http.ResponseWriter, req *http.Request) {
			redirectKeepingQuery(w, req, "/app/party/trail/start/"+url.PathEscape(chi.URLParam(req, "slug")))
		})
		r.Get("/o/{slug}/trail/edit", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
			http.Redirect(w, req, "/app/party/trail/edit", http.StatusFound)
		})
		r.Get("/trail/mine", func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "/app/party/trail/mine", http.StatusFound)
		})
		r.Get("/api/o/{slug}/trail", adminH.GetTrailStart)
		r.Post("/api/o/{slug}/trail/drafts", adminH.CreateTrailDraft)
		r.Get("/api/trail/drafts/{id}", adminH.GetTrailDraft)
		r.Put("/api/trail/drafts/{id}", adminH.PutTrailDraft)
		r.Put("/api/trail/drafts/{id}/theme", adminH.PutTrailDraftTheme)
		r.Delete("/api/trail/drafts/{id}", adminH.DeleteTrailDraft)
		r.Post("/api/trail/drafts/{id}/duplicate", adminH.DuplicateTrailDraft)
		r.Post("/api/trail/drafts/{id}/checkout", adminH.CheckoutTrailDraft)
		r.Post("/api/trail/drafts/{id}/host-link", adminH.SendTrailHostLink)
		r.Post("/api/trail/email/save", adminH.SaveTrailsToEmail)
		r.Post("/api/trail/email/signin", adminH.SignInToTrails)
		r.Post("/api/trail/email/verify", adminH.VerifyTrailEmail)
		r.Post("/api/trail/mine", adminH.ListMyTrails)
		r.Get("/api/trail/keep/{token}", adminH.GetKeepTrail)

		// Gift box redemption (Live it, TrueStory). The bare /redeem
		// offers every organisation's products; /o/{slug}/redeem only
		// that organisation's. A redemption waits for an organiser to
		// check the code; see admin/redemptions.go.
		r.Get("/redeem", func(w http.ResponseWriter, req *http.Request) {
			renderRedeemPage(w, req, "", "")
		})
		r.Get("/o/{slug}/redeem", func(w http.ResponseWriter, req *http.Request) {
			slug := chi.URLParam(req, "slug")
			if !orgSlugExists(req.Context(), cfg.DB, slug) {
				render404(w, req)
				return
			}
			org, err := loadOrgProfile(req.Context(), cfg.DB, strings.TrimSpace(slug))
			if err != nil {
				http.Error(w, "lookup failed", http.StatusInternalServerError)
				return
			}
			renderRedeemPage(w, req, slug, org.Name)
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

// redirectKeepingQuery sends a permanent redirect to path, keeping the
// query string: a buyer back from checkout (?paid=…) must not lose it.
func redirectKeepingQuery(w http.ResponseWriter, r *http.Request, path string) {
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, path, http.StatusMovedPermanently)
}
