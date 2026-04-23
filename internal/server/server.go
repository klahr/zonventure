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
	"git.sr.ht/~klahr/zv/internal/ws"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed web/play.html web/arrange.html web/live.html web/i18n.js
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
	adminH := &admin.Handlers{DB: cfg.DB, Lib: cfg.Library, Hub: hub}
	publicH := &public.Handlers{DB: cfg.DB, Lib: cfg.Library, Hub: hub}

	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Root picks the right landing spot based on which session cookie is
	// present. Without a session we show a tiny landing page with both
	// entry points so admins can actually reach /arrange.
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if _, ok := auth.FromContext(req.Context()); ok {
			http.Redirect(w, req, "/arrange", http.StatusFound)
			return
		}
		if _, ok := auth.SlotFromContext(req.Context()); ok {
			http.Redirect(w, req, "/play", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(landingHTML))
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

	// Public, no-auth read-only endpoints for the TV leaderboard. Keyed
	// by an opaque token per run so the URL isn't guessable.
	r.Get("/api/public/live/{token}", publicH.Live)
	r.Get("/api/public/live/{token}/tracks", publicH.Tracks)
	r.Get("/ws/live/{token}", publicH.WSLive)

	// Image serving: permission is decided inside the handler (staff
	// sessions always pass; slot sessions only when they're on the page).
	r.Get("/adventure-assets/image/{imageID}", adminH.ServeImage)

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

		r.Patch("/teams/{teamID}", adminH.RenameTeam)
		r.Delete("/teams/{teamID}", adminH.DeleteTeam)
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

			// Adventure editor (DB-backed authoring).
			r.Get("/adventures/{slug}/edit", adminH.LoadAdventureForEdit)
			r.Post("/adventures", adminH.CreateAdventure)
			r.Patch("/adventures/{slug}", adminH.UpdateAdventure)
			r.Delete("/adventures/{slug}", adminH.DeleteAdventure)
			r.Post("/adventures/{slug}/pages", adminH.CreatePage)
			r.Patch("/adventures/{slug}/pages/reorder", adminH.ReorderPages)
			r.Post("/adventures/{slug}/pages/copy-from/{pageID}", adminH.CopyPuzzleToAdventure)
			r.Patch("/pages/{pageID}", adminH.UpdatePage)
			r.Delete("/pages/{pageID}", adminH.DeletePage)
			r.Post("/pages/{pageID}/images", adminH.AddPageImage)
			r.Delete("/images/{imageID}", adminH.DeletePageImage)

			// Puzzle library: the cross-adventure pool of puzzles.
			r.Get("/puzzles", adminH.ListPuzzles)
			r.Post("/puzzles", adminH.CreateLibraryPuzzle)
			r.Get("/puzzles/{pageID}", adminH.GetPuzzle)
		})
	})

	return r
}

// landingHTML is a tiny in-lined page shown at / when no session exists.
// It exists so admins can reach /arrange without guessing the URL.
const landingHTML = `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Zonventure</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Aclonica&family=Open+Sans:wght@400;600;700&display=swap" rel="stylesheet">
<style>
  :root { color-scheme: light dark; --accent:#ad1357; --ink:#1f3349; --bg:#fbfbfd; --muted:#6b7f96; --border:#c6d2e166; }
  @media (prefers-color-scheme: dark) { :root { --bg:#14202e; --ink:#e7ecf4; --muted:#9db1cc; --accent:#e24080; --border:#3a517266; } }
  body { font-family:'Open Sans',system-ui,sans-serif; color:var(--ink); background:var(--bg); margin:0; min-height:100vh; display:grid; place-items:center; padding:1rem; }
  .brand { font-family:'Aclonica',Georgia,serif; color:var(--accent); font-size:2.4rem; text-align:center; margin:0 0 .25rem; }
  .sub { color:var(--muted); text-align:center; text-transform:uppercase; font-size:.8rem; letter-spacing:.1em; margin:0 0 1.5rem; font-weight:700; }
  .wrap { width:100%; max-width:360px; }
  a.btn { display:block; margin:.5rem 0; padding:.85rem 1.2rem; border-radius:32px; text-align:center; font-weight:700; text-decoration:none; border:1px solid var(--border); color:var(--ink); }
  a.btn.primary { background:var(--accent); color:#fff; border-color:var(--accent); }
  p.hint { color:var(--muted); font-size:.85rem; text-align:center; margin-top:1rem; }
</style></head>
<body><div class="wrap">
  <h1 class="brand">zonventure</h1>
  <a class="btn primary" href="/arrange">Log in</a>
  <p class="hint">Players arrive via their personal magic link.</p>
</div></body></html>`

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
