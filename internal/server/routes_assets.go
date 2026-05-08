// Static asset routes — every URL whose response is "bytes from the
// embed.FS, plus a content-type and ETag." Split from server.go to
// keep the route registrations findable. Wired by registerStaticAssetRoutes
// (favicon / fonts / scripts / leaflet / robots / sitemap) and
// registerAdventureAssetRoutes (the slash-prefixed media routes that
// run through admin handlers for permission checks).
package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// registerStaticAssetRoutes wires every URL whose response is a
// fixed (or version-stamped) blob baked into the binary: favicon,
// the SPA's runtime scripts, /assets/* CSS/JS, the self-hosted
// fonts + Leaflet, robots.txt, and the templated sitemap.xml.
func registerStaticAssetRoutes(r chi.Router, env *routesEnv) {
	cfg := env.cfg

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
		// Inject the build version into the SW's cache constants so
		// every deploy rotates the cache key without anyone bumping
		// a literal by hand. The activate handler then purges the
		// old cache entries automatically. "dev" is the unversioned
		// local-build fallback.
		ver := cfg.Version
		if ver == "" {
			ver = "dev"
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(string(body), "__ZV_BUILD_VERSION__", swCacheSafeVersion(ver))))
	})

	// Shared i18n dictionary + helpers, loaded by all three entry points.
	r.Get("/i18n.js", serveRenderedI18n())
	// Favicon: single SVG served at both the conventional /favicon.ico
	// path and the explicit /favicon.svg so old browsers that sniff
	// the extension don't 404. SVG is tiny, scales for any device.
	r.Get("/favicon.svg", serveAsset("web/favicon.svg", "image/svg+xml"))
	r.Get("/favicon.ico", serveAsset("web/favicon.svg", "image/svg+xml"))

	// Shared topbar CSS + JS, served as long-cached static assets.
	r.Get("/assets/topbar.css", serveAsset("web/topbar.css", "text/css; charset=utf-8"))
	r.Get("/assets/topbar.js", serveAsset("web/topbar.js", "application/javascript; charset=utf-8"))
	r.Get("/assets/page-tokens.css", serveAsset("web/page-tokens.css", "text/css; charset=utf-8"))
	r.Get("/assets/fontface.css", serveAsset("web/fontface.css", "text/css; charset=utf-8"))
	// Self-hosted webfonts (replaces the render-blocking link to
	// fonts.googleapis.com on every marketing page). Bytes are
	// content-addressed by URL filename; ETag from serveAsset gives
	// us 304s on revisit and the variable Open Sans woff2 covers
	// every weight in one ~50 KB download.
	r.Get("/assets/fonts/aclonica-latin.woff2", serveAsset("web/assets/fonts/aclonica-latin.woff2", "font/woff2"))
	r.Get("/assets/fonts/aclonica-latin-ext.woff2", serveAsset("web/assets/fonts/aclonica-latin-ext.woff2", "font/woff2"))
	r.Get("/assets/fonts/opensans-latin.woff2", serveAsset("web/assets/fonts/opensans-latin.woff2", "font/woff2"))
	r.Get("/assets/fonts/opensans-latin-ext.woff2", serveAsset("web/assets/fonts/opensans-latin-ext.woff2", "font/woff2"))
	// Self-hosted Leaflet (the maps library used by the live board,
	// recap, and the arrange-side map editor). Vendored so the player
	// surface never reaches out to unpkg.com mid-event, and so a
	// hostile CDN swap can't inject script into a logged-in arranger.
	// Marker images are referenced via relative ./images/* URLs from
	// inside leaflet.css, which is why the directory layout matches
	// upstream's dist/ exactly.
	r.Get("/assets/leaflet/leaflet.js", serveAsset("web/assets/leaflet/leaflet.js", "application/javascript; charset=utf-8"))
	r.Get("/assets/leaflet/leaflet.css", serveAsset("web/assets/leaflet/leaflet.css", "text/css; charset=utf-8"))
	r.Get("/assets/leaflet/images/marker-icon.png", serveAsset("web/assets/leaflet/images/marker-icon.png", "image/png"))
	r.Get("/assets/leaflet/images/marker-icon-2x.png", serveAsset("web/assets/leaflet/images/marker-icon-2x.png", "image/png"))
	r.Get("/assets/leaflet/images/marker-shadow.png", serveAsset("web/assets/leaflet/images/marker-shadow.png", "image/png"))
	r.Get("/assets/leaflet/images/layers.png", serveAsset("web/assets/leaflet/images/layers.png", "image/png"))
	r.Get("/assets/leaflet/images/layers-2x.png", serveAsset("web/assets/leaflet/images/layers-2x.png", "image/png"))
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
