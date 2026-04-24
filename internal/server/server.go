// Package server wires the HTTP router, middleware, and per-package
// handlers into one http.Handler.
package server

import (
	"database/sql"
	"embed"
	"net/http"

	"git.sr.ht/~klahr/zv/internal/admin"
	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/player"
	"git.sr.ht/~klahr/zv/internal/public"
	"git.sr.ht/~klahr/zv/internal/simulator"
	"git.sr.ht/~klahr/zv/internal/ws"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed web/play.html web/arrange.html web/live.html web/i18n.js web/favicon.svg
var webFS embed.FS

// Config is the bag of knobs New needs.
type Config struct {
	DB           *sql.DB
	Library      *adventure.Library
	Hub          *ws.Hub
	SecureCookie bool
}

// New builds the HTTP router.
func New(cfg Config) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(auth.Middleware(cfg.DB))
	r.Use(auth.SlotMiddleware(cfg.DB))

	hub := cfg.Hub
	if hub == nil {
		hub = ws.NewHub()
	}

	authH := &auth.Handlers{DB: cfg.DB, SecureCookie: cfg.SecureCookie}
	playerH := &player.Handlers{DB: cfg.DB, Lib: cfg.Library, Hub: hub, SecureCookie: cfg.SecureCookie}
	sim := simulator.New(cfg.DB, cfg.Library, hub)
	adminH := &admin.Handlers{DB: cfg.DB, Lib: cfg.Library, Hub: hub, Sim: sim}
	publicH := &public.Handlers{DB: cfg.DB, Lib: cfg.Library, Hub: hub}

	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Root sends visitors straight to the right entry point: players get
	// /play (their magic link will carry them back here), staff get the
	// /arrange UI which renders its own login screen when no staff
	// cookie is present.
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if _, ok := auth.SlotFromContext(req.Context()); ok {
			http.Redirect(w, req, "/play", http.StatusFound)
			return
		}
		http.Redirect(w, req, "/arrange", http.StatusFound)
	})

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
	r.Get("/live/{token}", servePage("web/live.html"))

	// Shared i18n dictionary + helpers, loaded by all three entry points.
	r.Get("/i18n.js", serveAsset("web/i18n.js", "application/javascript; charset=utf-8"))
	// Favicon: single SVG served at both the conventional /favicon.ico
	// path and the explicit /favicon.svg so old browsers that sniff
	// the extension don't 404. SVG is tiny, scales for any device.
	r.Get("/favicon.svg", serveAsset("web/favicon.svg", "image/svg+xml"))
	r.Get("/favicon.ico", serveAsset("web/favicon.svg", "image/svg+xml"))

	// Public, no-auth read-only endpoints for the TV leaderboard. Keyed
	// by an opaque token per run so the URL isn't guessable.
	r.Get("/api/public/live/{token}", publicH.Live)
	r.Get("/api/public/live/{token}/tracks", publicH.Tracks)
	r.Get("/ws/live/{token}", publicH.WSLive)

	// Per-team share page — the arranger sends a single /team/{token}
	// URL to the captain; each member picks their own slot from there.
	r.Get("/team/{token}", publicH.TeamShare)
	// Team-token-authed avatar upload/delete so the team can change
	// their own profile picture from the share page. The token IS the
	// auth; no staff session required.
	r.Post("/api/public/team/{token}/avatar", publicH.SetTeamAvatarByToken)
	r.Delete("/api/public/team/{token}/avatar", publicH.DeleteTeamAvatarByToken)

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

	// Staff authentication.
	r.Post("/api/auth/login", authH.Login)
	r.Post("/api/auth/set-password", authH.SetPassword)
	r.Post("/api/auth/logout", authH.Logout)
	r.Get("/api/auth/whoami", authH.Whoami)

	// Player (slot-authenticated).
	r.Route("/api/player", func(r chi.Router) {
		r.Use(auth.RequireSlot)
		r.Get("/state", playerH.GetState)
		r.Post("/answer", playerH.SubmitAnswer)
		r.Post("/location", playerH.PostLocation)
		r.Get("/summary", playerH.Summary)
		r.Post("/logout", playerH.Logout)
	})

	// Player WebSocket: replaces the 5s /state poll and 15s /location POST
	// with a single push-when-changed socket.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireSlot)
		r.Get("/ws/play", playerH.WSPlay)
	})

	// Arranger subscription stream: mutations still use the /api/admin
	// HTTP endpoints above; this socket exists only to push run-detail
	// updates (positions, tracks, events, run+teams) on change.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAnyRole(auth.RoleAdmin, auth.RoleOrgAdmin, auth.RoleOrgMember))
		r.Get("/ws/arrange", adminH.WSArrange)
	})

	// Admin / org-user surface (staff-authenticated).
	r.Route("/api/admin", func(r chi.Router) {
		r.Use(auth.RequireAnyRole(auth.RoleAdmin, auth.RoleOrgAdmin, auth.RoleOrgMember))

		r.Get("/adventures", adminH.ListAdventures)
		r.Get("/adventures/{slug}", adminH.AdventureDetail)
		// Preview: render any puzzle in the shape /play expects so an
		// arranger can QA the adventure without a real run.
		r.Get("/adventures/{slug}/preview", adminH.PreviewAdventure)
		// Live markdown → HTML for the arrange editor's preview panes.
		// Pure function; no DB access, no org scoping needed.
		r.Post("/markdown/preview", adminH.PreviewMarkdown)

		r.Get("/runs", adminH.ListRuns)
		r.Post("/runs", adminH.CreateRun)
		r.Get("/runs/{runID}", adminH.RunDetail)
		r.Patch("/runs/{runID}", adminH.UpdateRun)
		r.Post("/runs/{runID}/start", adminH.StartRun)
		r.Post("/runs/{runID}/pause", adminH.PauseRun)
		r.Post("/runs/{runID}/resume", adminH.ResumeRun)
		r.Post("/runs/{runID}/teams", adminH.CreateTeam)
		r.Get("/runs/{runID}/positions", adminH.ListPositions)
		r.Get("/runs/{runID}/events", adminH.ListEvents)
		r.Get("/runs/{runID}/tracks", adminH.ListTracks)
		r.Get("/runs/{runID}/results.csv", adminH.ExportRunResults)

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

			// Server logs — stream + level control. Admin-only so org
			// users can't snoop another org's messages.
			r.Get("/logs", adminH.ListLogs)
			r.Get("/logs/level", adminH.LogsLevel)
			r.Put("/logs/level", adminH.SetLogsLevel)

			// Race simulator. Admin-only because it writes runs/teams
			// on behalf of the first org and can take the DB on a walk.
			r.Get("/simulator", adminH.SimulatorStatus)
			r.Post("/simulator/start", adminH.SimulatorStart)
			r.Post("/simulator/pause", adminH.SimulatorPause)
			r.Post("/simulator/resume", adminH.SimulatorResume)
			r.Post("/simulator/reset", adminH.SimulatorReset)
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
// no caching.
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
