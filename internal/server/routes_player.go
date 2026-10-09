// Player routes — /api/player REST surface and the /ws/play
// WebSocket. Both gate on a slot session; the REST mutations also
// require CSRF because the player UI can be embedded in a third-
// party context (eg. an arranger sharing the play URL).
package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/player"
)

// registerPlayerRoutes wires the slot-authenticated REST endpoints
// and the WS push channel that replaces the 5 s state poll.
func registerPlayerRoutes(r chi.Router, env *routesEnv) {
	playerH := env.playerH

	// Signing out, and erasing the player's data (GDPR right to
	// erasure: wipes pings + answers + photos + slot name + session for
	// the caller's slot; the slot row stays so the team's run keeps
	// working for everyone else). Both still work once the event has
	// ended, when the endpoints below refuse the session.
	r.With(auth.RequireSlotOrEnded, auth.RequireCSRF()).Post("/api/player/logout", playerH.Logout)
	r.With(auth.RequireSlotOrEnded, auth.RequireCSRF()).Post("/api/player/forget", playerH.Forget)

	// Player (slot-authenticated). Mutations under CSRF; the GET reads
	// stay open since they're idempotent and can't damage state.
	r.Route("/api/player", func(r chi.Router) {
		r.Use(auth.RequireSlot)
		r.Use(auth.RequireCSRF())
		r.Use(player.ForPhone)
		// The React player app.
		r.Get("/play", playerH.GetPlay)
		r.Post("/play/{action}", playerH.PostPlayAction)
		// A team's picture for a photo block, and the team's own pictures.
		r.Post("/play/photo", playerH.PostPlayPhoto)
		// Checks a cheat password before the phone turns cheating on.
		r.Post("/play/unlock", playerH.PostPlayUnlock)
		r.Get("/play/photo/{photoID}", playerH.GetPlayPhoto)
		// A file the organisers left for the event's players.
		r.Get("/files/{fileID}", playerH.GetEventFile)
		// Takes or gives up one of the adventure's roles.
		r.Post("/role", playerH.PostRole)
		// Hands a role's kit item to a teammate: a code to show, and to scan.
		r.Post("/items/give", playerH.PostGiveItem)
		r.Post("/items/receive", playerH.PostReceiveItem)
		r.Post("/location", playerH.PostLocation)
		// Player-driven slot rename — the arranger's pre-set name is
		// often a placeholder ("Player 3"). Letting the player put
		// their own name on the leaderboard improves engagement.
		r.Post("/me/name", playerH.RenameMyself)
		// Player-driven team rename — the arranger's "Team 3"
		// placeholder is a common pre-event setup and the team
		// itself agrees on the real name as a group. Any team
		// member can update; UNIQUE(event_id, name) prevents
		// collisions across teams.
		r.Post("/team/name", playerH.RenameMyTeam)
		// The team's picture, set by its players — like the names, only
		// until the team begins (play.TeamBegun).
		r.Post("/team/avatar", playerH.SetMyTeamAvatar)
		r.Delete("/team/avatar", playerH.DeleteMyTeamAvatar)
		// A dev ticket's phone: jump to a step of the draft, or start over.
		r.Post("/dev/goto", playerH.DevGoTo)
		r.Post("/dev/reset", playerH.DevReset)
	})

	// Player WebSocket: replaces the 5s /state poll and 15s /location POST
	// with a single push-when-changed socket.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireSlot)
		r.Get("/ws/play", playerH.WSPlay)
	})
}
