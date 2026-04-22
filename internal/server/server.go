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
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed web/play.html web/arrange.html web/live.html
var webFS embed.FS

// Config is the bag of knobs New needs.
type Config struct {
	DB           *sql.DB
	Library      *adventure.Library
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

	authH := &auth.Handlers{DB: cfg.DB, SecureCookie: cfg.SecureCookie}
	playerH := &player.Handlers{DB: cfg.DB, Lib: cfg.Library, SecureCookie: cfg.SecureCookie}
	adminH := &admin.Handlers{DB: cfg.DB, Lib: cfg.Library}
	publicH := &public.Handlers{DB: cfg.DB, Lib: cfg.Library}

	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/play", http.StatusFound)
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
	r.Get("/live/{runID}", servePage("web/live.html"))

	// Public, no-auth read-only endpoint for the TV leaderboard.
	r.Get("/api/public/runs/{runID}/live", publicH.Live)

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
		r.Post("/logout", playerH.Logout)
	})

	// Admin / org-user surface (staff-authenticated).
	r.Route("/api/admin", func(r chi.Router) {
		r.Use(auth.RequireAnyRole(auth.RoleAdmin, auth.RoleOrgAdmin, auth.RoleOrgMember))

		r.Get("/adventures", adminH.ListAdventures)
		r.Get("/adventures/{slug}", adminH.AdventureDetail)

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
		})
	})

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
