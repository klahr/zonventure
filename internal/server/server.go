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

// New builds the HTTP router. It does not call ListenAndServe — the caller
// owns the server lifecycle.
func New(cfg Config) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(auth.Middleware(cfg.DB))

	authH := &auth.Handlers{DB: cfg.DB, SecureCookie: cfg.SecureCookie}
	playerH := &player.Handlers{DB: cfg.DB, Lib: cfg.Library}
	adminH := &admin.Handlers{DB: cfg.DB, Lib: cfg.Library}
	publicH := &public.Handlers{DB: cfg.DB, Lib: cfg.Library}

	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/play", http.StatusFound)
	})
	r.Get("/play", servePage("web/play.html"))
	r.Get("/arrange", servePage("web/arrange.html"))
	r.Get("/live/{runID}", servePage("web/live.html"))

	// Public, no-auth read-only endpoint for the TV leaderboard.
	r.Get("/api/public/runs/{runID}/live", publicH.Live)

	r.Post("/api/auth/login", authH.Login)
	r.Post("/api/auth/set-password", authH.SetPassword)
	r.Post("/api/auth/logout", authH.Logout)
	r.Get("/api/auth/whoami", authH.Whoami)

	r.Route("/api/player", func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RolePlayer))
		r.Get("/state", playerH.GetState)
		r.Post("/answer", playerH.SubmitAnswer)
		r.Post("/location", playerH.PostLocation)
	})

	r.Route("/api/admin", func(r chi.Router) {
		r.Use(auth.RequireAnyRole(auth.RoleArranger, auth.RoleRoot))
		r.Get("/adventures", adminH.ListAdventures)
		r.Get("/adventures/{slug}", adminH.AdventureDetail)
		r.Get("/runs", adminH.ListRuns)
		r.Post("/runs", adminH.CreateRun)
		r.Get("/runs/{runID}", adminH.RunDetail)
		r.Post("/runs/{runID}/start", adminH.StartRun)
		r.Post("/runs/{runID}/pause", adminH.PauseRun)
		r.Post("/runs/{runID}/resume", adminH.ResumeRun)
		r.Post("/runs/{runID}/teams", adminH.CreateTeam)
		r.Get("/runs/{runID}/positions", adminH.ListPositions)
		r.Get("/runs/{runID}/events", adminH.ListEvents)
		r.Get("/runs/{runID}/tracks", adminH.ListTracks)
		r.Patch("/teams/{teamID}", adminH.RenameTeam)
		r.Delete("/teams/{teamID}", adminH.DeleteTeam)
		r.Post("/teams/{teamID}/members", adminH.AddMember)
		r.Delete("/teams/{teamID}/members/{userID}", adminH.RemoveMember)
		r.Post("/teams/{teamID}/unlock", adminH.UnlockTeam)
		r.Get("/users", adminH.ListUsers)
		r.Post("/users", adminH.CreateUser)
		r.Delete("/users/{userID}", adminH.DeleteUser)
		r.Post("/users/{userID}/password", adminH.ResetPassword)

		// Root-only sub-surface for managing arrangers and their assignments.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleRoot))
			r.Get("/arrangers", adminH.ListArrangers)
			r.Post("/arrangers/{userID}/assignments", adminH.CreateAssignment)
			r.Delete("/arrangers/{userID}/assignments/{assignmentID}", adminH.DeleteAssignment)
		})

		// Arranger-facing: what am I allowed to run, and for how many teams.
		r.Get("/assignments", adminH.ListMyAssignments)
	})

	return r
}

// servePage returns a handler that writes a single embedded HTML page with
// no caching — these are dev-grade test pages, not production assets.
func servePage(path string) http.HandlerFunc {
	body, err := webFS.ReadFile(path)
	if err != nil {
		// Fail fast at startup: a broken embed pattern is a build-time bug.
		panic("embed: " + err.Error())
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	}
}
