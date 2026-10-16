// Static asset routes — every URL whose response is "bytes from the
// embed.FS, plus a content-type and ETag." Split from server.go to
// keep the route registrations findable. Wired by registerStaticAssetRoutes
// (favicon / fonts / scripts / maplibre / robots / sitemap) and
// registerAdventureAssetRoutes (the slash-prefixed media routes that
// run through admin handlers for permission checks).
package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/emailtpl"
)

// registerStaticAssetRoutes wires every URL whose response is a
// fixed (or version-stamped) blob baked into the binary: favicon,
// the SPA's runtime scripts, /assets/* CSS/JS, the self-hosted
// fonts + Leaflet, robots.txt, and the templated sitemap.xml.
func registerStaticAssetRoutes(r chi.Router, env *routesEnv) {
	cfg := env.cfg

	// The classic player's root service worker, retired: what is served
	// now unregisters it from phones that still have it.
	r.Get("/play-sw.js", func(w http.ResponseWriter, req *http.Request) {
		body, err := readWebFile("web/play-sw.js")
		if err != nil {
			http.Error(w, "embed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Service-Worker-Allowed", "/")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write(body)
	})

	// Shared i18n dictionary + helpers, loaded by all three entry points.
	r.Get("/i18n.js", serveRenderedI18n())
	// Favicon: single SVG served at both the conventional /favicon.ico
	// path and the explicit /favicon.svg so old browsers that sniff
	// the extension don't 404. SVG is tiny, scales for any device.
	r.Get("/favicon.svg", serveAsset("web/favicon.svg", "image/svg+xml"))
	r.Get("/favicon.ico", serveAsset("web/favicon.svg", "image/svg+xml"))
	// Link-preview image for og:image / twitter:image. Unfurlers want a
	// 1200x630 raster; an override dir can shadow it with its own.
	r.Get("/og-image.png", serveAsset("web/og-image.png", "image/png"))

	// Shared topbar CSS + JS, served as long-cached static assets.
	r.Get("/assets/topbar.css", serveAsset("web/topbar.css", "text/css; charset=utf-8"))
	r.Get("/assets/topbar.js", serveAsset("web/topbar.js", "application/javascript; charset=utf-8"))
	r.Get("/assets/page-tokens.css", serveAsset("web/page-tokens.css", "text/css; charset=utf-8"))
	registerFontRoutes(r)
	// Self-hosted MapLibre for the server-rendered pages' vector maps
	// (store, recap, live view), copied from web/node_modules by `make
	// web` so it matches the arrange app's. Vendored so the player
	// surface never reaches out to a CDN mid-event, and so a hostile CDN
	// swap can't inject script. The worker imports the shared chunk
	// from beside itself, hence one directory.
	for _, f := range []string{"maplibre-gl.mjs", "maplibre-gl-shared.mjs", "maplibre-gl-worker.mjs"} {
		r.Get("/assets/maplibre/"+f, serveAsset("web/assets/maplibre/"+f, "text/javascript; charset=utf-8"))
	}
	r.Get("/assets/maplibre/maplibre-gl.css", serveAsset("web/assets/maplibre/maplibre-gl.css", "text/css; charset=utf-8"))
	r.Get("/assets/zv-maplibre.js", serveAsset("web/assets/zv-maplibre.js", "text/javascript; charset=utf-8"))
	// Map tile source for every browser-side map (ZV_MAP_TILE_URL /
	// ZV_MAP_TILE_ATTRIBUTION). no-cache, so a provider switch reaches
	// open pages on their next load.
	mapConfig := mapConfigJS(cfg.Legal)
	r.Get("/assets/map-config.js", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(mapConfig)
	})
	// jsQR — vendored fallback QR decoder for browsers that don't
	// expose the native BarcodeDetector (Firefox today, older
	// Safari/iOS). Both /play and the puzzle editor in /arrange
	// load it lazily; the canvas-based decode runs entirely
	// client-side, no network beyond fetching this asset.
	r.Get("/assets/jsqr.js", serveAsset("web/jsqr.js", "application/javascript; charset=utf-8"))
	r.Get("/assets/illustrations/not-found.webp", serveAsset("web/assets/illustrations/not-found.webp", "image/webp"))
	// Emails' pictures (internal/emailtpl/pictures.go): JPEG, which
	// every mail client shows, at the instance's public URL.
	for _, f := range emailtpl.HouseFiles() {
		r.Get("/assets/email/"+f, serveAsset("web/assets/email/"+f, "image/jpeg"))
	}
	for _, id := range emailtpl.CoverThemes {
		r.Get("/assets/email/covers/"+id+".jpg", serveAsset("web/assets/email/covers/"+id+".jpg", "image/jpeg"))
	}

	// Crawler hygiene: robots.txt and a small sitemap of public URLs.
	// {{HOST}} in the sitemap is rewritten with the operator-configured
	// PublicURL — without it the URLs render origin-relative, which
	// most crawlers handle but a few (Bing especially) prefer absolute.
	r.Get("/robots.txt", serveAsset("web/robots.txt", "text/plain; charset=utf-8"))
	sitemapBody := env.sitemapBody
	r.Get("/sitemap.xml", func(w http.ResponseWriter, req *http.Request) {
		host := strings.TrimRight(cfg.PublicURL, "/")
		if host == "" {
			scheme := "https"
			if req.TLS == nil && req.Header.Get("X-Forwarded-Proto") != "https" {
				scheme = "http"
			}
			host = scheme + "://" + req.Host
		}
		// Per-org store entries: list the slug of every org with at
		// least one for-sale event. The query is cheap (count + slug
		// per org) and rebuilds on every fetch — cache-control still
		// pins the response for an hour, so crawler load stays low.
		orgURLs := buildOrgSitemapURLs(req.Context(), cfg.DB, host)
		out := strings.ReplaceAll(string(sitemapBody), "{{HOST}}", host)
		out = strings.ReplaceAll(out, "{{ORG_URLS}}", orgURLs)
		out = dropSitemapPages(out, host, unavailablePages(legalNow(cfg.Legal), cfg.HomeOrg))
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write([]byte(out))
	})
}

// registerAdventureAssetRoutes wires the /adventure-assets/* media
// routes plus /org-logo, /org-cover, /store-cover. Permission is
// resolved inside each admin-handler — staff sessions always pass,
// slot sessions only pass when the rule for the asset kind says so.
// The routes themselves stay open (no middleware) so a service
// worker can pre-fetch them without an auth round-trip.
func registerAdventureAssetRoutes(r chi.Router, env *routesEnv) {
	adminH := env.adminH

	// Adventure media: one route for images and audio, gated in the
	// handler by what the team's graph has shown it.
	r.Get("/media/{mediaID}", adminH.ServeMedia)
	// Team avatars are intentionally open — they surface on the public
	// live board and on player views without a staff session. The team
	// id is opaque enough to be acceptable public read-only data.
	r.Get("/adventure-assets/team-avatar/{teamID}", adminH.ServeTeamAvatar)
	// Adventure cover pictures — same public-read rationale: the slug is
	// already in the URL, no extra sensitive metadata is leaked.
	r.Get("/adventure-assets/avatar/{slug}", adminH.ServeAdventureAvatar)
	// The adventure's own cover picture (its look's), for the pages that
	// wear the look outside the player: store, recap, live board.
	r.Get("/adventure-assets/cover/{slug}", adminH.ServeAdventureCover)
	// Org brand logo — public-read for the same reasons; companies put
	// their logo on their website, it's not sensitive material. Path
	// is short ("/org-logo/{id}") so the bytes embed cleanly into the
	// recap PDF and any future server-rendered surfaces.
	r.Get("/org-logo/{orgID}", adminH.ServeOrgLogo)
	// Org cover image — public-read for the same reasons as the
	// logo: the bytes are intentionally on the org's public
	// landing page, not sensitive material. Mirrors /org-logo's
	// shape so a single download path covers either asset.
	r.Get("/org-cover/{orgID}", adminH.ServeOrgCover)
	// Event store-cover image. Public so the /store page can render
	// the hero on every card without authentication; mounted at a
	// short path because it's the most-fetched public-image route on
	// a busy hosted instance.
	r.Get("/store-cover/event/{eventID}", adminH.ServeEventCover)
}
