// Arranger / admin routes — the /api/admin REST tree, the
// /ws/arrange and /ws/logs sockets, and the /purchase/{id}
// simulated-checkout landing. Wired by registerAdminRoutes; called
// from New() in server.go.
package server

import (
	"github.com/go-chi/chi/v5"

	"git.sr.ht/~klahr/zv/internal/auth"
)

// registerAdminRoutes wires the staff-authenticated surface. The
// big /api/admin route tree lives in this one helper to keep
// arranger-side endpoints findable as a unit; the WS streams and
// the dev-checkout landing live alongside because they share the
// "logged-in arranger" audience.
func registerAdminRoutes(r chi.Router, env *routesEnv) {
	adminH := env.adminH

	// Arranger subscription stream: mutations still use the /api/admin
	// HTTP endpoints above; this socket exists only to push event-detail
	// updates (positions, tracks, events, run+teams) on change. Lobby
	// users have no org scope to subscribe to and are excluded.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAnyRole(auth.RoleAdmin, auth.RoleOrgAdmin, auth.RoleOrgMember))
		r.Get("/ws/arrange", adminH.WSArrange)
	})

	// Admin / org-user surface (staff-authenticated). RequireCSRF runs
	// after RequireAnyRole so the 401 for "no session" still wins
	// (pre-auth callers don't care about the CSRF distinction).
	r.Route("/api/admin", func(r chi.Router) {
		r.Use(auth.RequireAnyRole(auth.RoleAdmin, auth.RoleOrgAdmin, auth.RoleOrgMember, auth.RoleLobby))
		r.Use(auth.RequireCSRF())

		r.Get("/adventures", adminH.ListAdventures)
		r.Get("/adventures/{slug}", adminH.AdventureDetail)
		// Preview: render any puzzle in the shape /play expects so an
		// arranger can QA the adventure without a real run.
		r.Get("/adventures/{slug}/preview", adminH.PreviewAdventure)
		// Live markdown → HTML for the arrange editor's preview panes.
		// Pure function; no DB access, no org scoping needed.
		r.Post("/markdown/preview", adminH.PreviewMarkdown)

		r.Get("/events", adminH.ListEvents)
		r.Post("/events", adminH.CreateEvent)
		r.Get("/events/{eventID}", adminH.EventDetail)
		r.Patch("/events/{eventID}", adminH.UpdateEvent)
		r.Post("/events/{eventID}/cover", adminH.SetEventCover)
		r.Delete("/events/{eventID}/cover", adminH.DeleteEventCover)
		r.Post("/events/{eventID}/start", adminH.StartEvent)
		r.Post("/events/{eventID}/pause", adminH.PauseEvent)
		r.Post("/events/{eventID}/resume", adminH.ResumeEvent)
		r.Post("/events/{eventID}/cancel", adminH.CancelEvent)
		r.Post("/events/{eventID}/teams", adminH.CreateTeam)
		r.Post("/events/{eventID}/teams/bulk", adminH.BulkCreateTeams)
		// Team-roster file import — multipart upload, returns a
		// parsed preview the client then submits to /teams/bulk
		// after the arranger inspects it.
		r.Post("/events/{eventID}/teams/import", adminH.ImportTeams)
		r.Get("/events/{eventID}/positions", adminH.ListPositions)
		r.Get("/events/{eventID}/activity", adminH.ListActivity)
		r.Get("/events/{eventID}/tracks", adminH.ListTracks)
		r.Get("/events/{eventID}/results.csv", adminH.ExportEventResults)
		// Branded recap PDF — same data, different shape: a polished
		// post-event artifact arrangers can hand to their stakeholders.
		// Mounted alongside the CSV so the same authorize-event check
		// gates both.
		r.Get("/events/{eventID}/recap.pdf", adminH.ExportEventRecapPDF)
		// iCalendar invite — arrangers download once and forward to
		// participants so the event lands on everyone's calendar.
		r.Get("/events/{eventID}/invite.ics", adminH.ExportEventInviteICS)
		// Send-invitations: proactive variant of the recover-link
		// flow. Iterates teams with non-empty contact emails and
		// mails each one their own slot links + the .ics. Same
		// authorize-event gate as the export endpoints — Race directors
		// (org_member) can invoke it; this is "running the game",
		// not authoring.
		r.Post("/events/{eventID}/invitations/send", adminH.SendEventInvitations)

		r.Patch("/teams/{teamID}", adminH.UpdateTeam)
		r.Delete("/teams/{teamID}", adminH.DeleteTeam)
		r.Post("/teams/{teamID}/avatar", adminH.SetTeamAvatar)
		r.Delete("/teams/{teamID}/avatar", adminH.DeleteTeamAvatar)
		r.Post("/teams/{teamID}/slots", adminH.AddSlot)
		r.Delete("/teams/{teamID}/slots/{slotID}", adminH.RemoveSlot)
		r.Post("/teams/{teamID}/slots/{slotID}/rotate", adminH.RotateSlotToken)
		r.Post("/teams/{teamID}/unlock", adminH.UnlockTeam)
		// Withdraw / unwithdraw soft-flag a team that gave up mid-event
		// without deleting them — they stay in the recap as
		// "withdrew at puzzle 4" instead of vanishing from history.
		r.Post("/teams/{teamID}/withdraw", adminH.WithdrawTeam)
		r.Post("/teams/{teamID}/unwithdraw", adminH.UnwithdrawTeam)
		// Per-team resend of the invitation email. Re-uses the team's
		// existing slot tokens (no re-mint), so opening already-
		// claimed sessions stays valid. Players who lost their session
		// state should hit the public lost-magic-link recovery path
		// instead — that's the explicit re-mint flow.
		r.Post("/teams/{teamID}/invitations/send", adminH.SendTeamInvitation)

		// Staff password reset (admins & org users).
		r.Post("/users/{userID}/password", adminH.ResetPassword)
		// Admin TOTP factory-reset — recovery path for users who lost
		// both their authenticator and every recovery code. Same
		// authorization rules as the password reset above; requires
		// X-Confirm-Password from the acting admin.
		r.Post("/users/{userID}/totp/disable", adminH.AdminDisableUserTOTP)
		// Username typeahead for the "Add user to org" modal — admin
		// / org_admin only. Returns up to 10 prefix matches that
		// aren't admins and aren't already members of the target org.
		r.Get("/users/search", adminH.SearchUsers)

		// Caller's own assignments list.
		r.Get("/assignments", adminH.ListMyAssignments)

		// Org admin or global admin: manage an organization's members + a
		// detail endpoint both admins and in-org users can read.
		r.Get("/organizations/{orgID}", adminH.GetOrganization)
		r.Post("/organizations/{orgID}/members", adminH.CreateOrgMember)
		r.Delete("/organizations/{orgID}/members/{userID}", adminH.DeleteOrgMember)
		// Atomic ownership transfer — promotes a member to org_admin
		// and demotes the calling org_admin to org_member in one tx.
		r.Post("/organizations/{orgID}/transfer-ownership", adminH.TransferOwnership)

		// Stripe Connect onboarding for the /store marketplace flow.
		// Each handler re-checks org_admin-of-named-org or global
		// admin; rejecting silently here would leak that endpoints
		// exist to org members of other orgs.
		r.Post("/organizations/{orgID}/stripe-connect/start", adminH.StartStripeConnect)
		r.Get("/organizations/{orgID}/stripe-connect/status", adminH.GetStripeConnectStatus)
		r.Post("/organizations/{orgID}/stripe-connect/refresh", adminH.RefreshStripeConnectStatus)

		// Adventure authoring. Available to every authoring tier —
		// handlers enforce ownership inline (admin edits the global
		// catalog; org users edit their own org's adventures).
		r.Get("/adventures/{slug}/edit", adminH.LoadAdventureForEdit)
		r.Post("/adventures", adminH.CreateAdventure)
		r.Patch("/adventures/{slug}", adminH.UpdateAdventure)
		r.Delete("/adventures/{slug}", adminH.DeleteAdventure)
		r.Post("/adventures/{slug}/avatar", adminH.SetAdventureAvatar)
		r.Delete("/adventures/{slug}/avatar", adminH.DeleteAdventureAvatar)
		// Clone a globally-visible adventure (catalog row or template)
		// into the caller's org. Authoring-tier action: org_admin or
		// admin only. See internal/admin/duplicate.go for the
		// transactional copy logic.
		r.Post("/adventures/{slug}/duplicate", adminH.DuplicateAdventure)
		// Portability: zip-archive export of a single adventure
		// (manifest + assets) and import that recreates one under
		// the caller's org. The import endpoint sits at the
		// adventures collection root because it has no slug yet.
		r.Get("/adventures/{slug}/export", adminH.ExportAdventure)
		r.Post("/adventures/import", adminH.ImportAdventure)
		// Org-admin self-export of the whole org bundle (data-portability
		// surface). Operator-side full export + import + redirect-mode
		// flip live in the RoleAdmin-only block further down. The handler
		// pins org_id to the caller's session and refuses anyone other
		// than RoleOrgAdmin.
		r.Get("/migrate/self-export", adminH.MigrateSelfExport)
		// Org slug self-management: read + write the slug that powers
		// /o/{slug}/store. Pinned to the caller's active org and
		// gated to RoleOrgAdmin inside the handler so an org_member
		// can't rename the URL out from under their team.
		r.Get("/orgs/me/slug", adminH.GetMyOrgSlug)
		r.Put("/orgs/me/slug", adminH.SetMyOrgSlug)
		// Public-profile read/write (tagline, website, cover image)
		// surfaced on /o/{slug}. Read is org-user, writes are
		// gated to RoleOrgAdmin inside each handler.
		r.Get("/orgs/me/profile", adminH.GetMyOrgProfile)
		r.Put("/orgs/me/profile", adminH.SetMyOrgProfile)
		r.Post("/orgs/me/cover", adminH.SetMyOrgCover)
		r.Delete("/orgs/me/cover", adminH.DeleteMyOrgCover)
		// Org-admin self-serve logo (mirror of admin-side
		// /organizations/{id}/logo, but pinned to the caller's
		// session). Lets an org_admin upload + replace the brand
		// logo without involving platform support.
		r.Post("/orgs/me/logo", adminH.SetMyOrgLogo)
		r.Delete("/orgs/me/logo", adminH.DeleteMyOrgLogo)
		r.Get("/adventures/{slug}/shares", adminH.ListAdventureShares)
		r.Post("/adventures/{slug}/shares", adminH.GrantAdventureShare)
		r.Delete("/adventures/{slug}/shares/{orgID}", adminH.RevokeAdventureShare)
		r.Post("/adventures/{slug}/pages", adminH.CreatePage)
		r.Patch("/adventures/{slug}/pages/reorder", adminH.ReorderPages)
		// Lane CRUD. Reorder lives at /lanes/reorder rather than as
		// a method-keyed verb so the router pattern stays decisive
		// (no overlap with /lanes/{laneID}).
		r.Post("/adventures/{slug}/lanes", adminH.CreateLane)
		r.Post("/adventures/{slug}/lanes/reorder", adminH.ReorderLanes)
		r.Patch("/adventures/{slug}/lanes/{laneID}", adminH.UpdateLane)
		r.Delete("/adventures/{slug}/lanes/{laneID}", adminH.DeleteLane)
		r.Post("/adventures/{slug}/pages/copy-from/{pageID}", adminH.CopyPuzzleToAdventure)
		r.Patch("/pages/{pageID}", adminH.UpdatePage)
		r.Delete("/pages/{pageID}", adminH.DeletePage)
		// Duplicate one puzzle in place — Google Slides shape. The
		// new row lands at sourceIdx + 1 with the rest of the
		// adventure shifted down. Auth via requireEditPage so the
		// org_member tightening carries through.
		r.Post("/pages/{pageID}/duplicate", adminH.DuplicatePage)
		r.Post("/pages/{pageID}/images", adminH.AddPageImage)
		r.Patch("/images/{imageID}", adminH.UpdatePageImage)
		r.Delete("/images/{imageID}", adminH.DeletePageImage)
		// Per-hint image attachments. Hint images are addressed by
		// their own image id under /hint-images/ so the puzzle-image
		// patch/delete routes don't have to disambiguate which kind
		// they're acting on.
		r.Post("/hints/{hintID}/images", adminH.AddHintImage)
		r.Patch("/hint-images/{imageID}", adminH.UpdateHintImage)
		r.Delete("/hint-images/{imageID}", adminH.DeleteHintImage)
		// Audio attachments — same shape as the image routes. PATCH
		// only carries `description` (the WCAG transcript fallback);
		// the blob itself is replaced by delete + re-upload.
		r.Post("/pages/{pageID}/audio", adminH.AddPageAudio)
		r.Patch("/audio/{audioID}", adminH.UpdatePageAudio)
		r.Delete("/audio/{audioID}", adminH.DeletePageAudio)
		r.Post("/hints/{hintID}/audio", adminH.AddHintAudio)
		r.Patch("/hint-audio/{audioID}", adminH.UpdateHintAudio)
		r.Delete("/hint-audio/{audioID}", adminH.DeleteHintAudio)

		// Puzzle library (cross-adventure pool).
		r.Get("/puzzles", adminH.ListPuzzles)
		r.Post("/puzzles", adminH.CreateLibraryPuzzle)
		r.Get("/puzzles/{pageID}", adminH.GetPuzzle)

		// Purchases: arrangers buy player-slot assignments here. The
		// simulated flow just needs the confirm endpoint; a future
		// Stripe integration will also hit /confirm from a webhook.
		r.Post("/purchases", adminH.CreatePurchase)
		r.Get("/purchases", adminH.ListPurchases)
		r.Get("/purchases/{purchaseID}", adminH.GetPurchase)
		r.Post("/purchases/{purchaseID}/confirm", adminH.ConfirmPurchase)

		// Auth audit feed. The handler scopes per role: admins see
		// every event, org_admins see only their org's users, anyone
		// else gets 403. Mounted here (not inside the admin-only
		// group below) so org_admins reach it.
		r.Get("/auth-events", adminH.ListAuthEvents)

		// Self-service org creation. A lobby user (no memberships)
		// uses it to leave the empty-state dashboard; an existing
		// org user uses it to spin up a second org they admin.
		// Admins are blocked inside the handler.
		r.Post("/me/organizations", adminH.CreateOwnOrg)

		// Account settings — every logged-in staff user can change
		// their own password / email / delete themselves. No role
		// gate beyond the surrounding RequireAnyRole.
		r.Post("/me/password", adminH.ChangeMyPassword)
		r.Post("/me/email", adminH.ChangeMyEmail)
		// Revertable email change: ConfirmEmailChange stashes the old
		// address; this endpoint reads the status and reverts within
		// the grace window.
		r.Get("/me/email/revert-status", adminH.MyEmailChangeStatus)
		r.Post("/me/email/revert", adminH.RevertMyEmailChange)
		r.Delete("/me", adminH.DeleteMyAccount)
		// Account deletion is soft — DeleteMyAccount sets a 7-day
		// timer; this endpoint clears it before the sweeper runs.
		r.Post("/me/cancel-deletion", adminH.CancelMyDeletion)
		// Stripe Customer Portal: hosted UI for the org's payment
		// method, invoices, and cancellations. Returns a one-shot
		// session URL the client redirects to.
		r.Post("/me/portal-session", adminH.CreatePortalSession)
		// GDPR data export — JSON dump of personal + (when scoped)
		// org data, served as a downloadable file.
		r.Get("/me/export", adminH.ExportMyData)
		// Self audit feed — every logged-in user can see their own
		// recent login / password / email events for transparency.
		r.Get("/me/audit", adminH.ListMyAuthEvents)
		// Active sessions: list + per-token revoke + log-out-everywhere.
		r.Get("/me/sessions", adminH.ListMySessions)
		r.Delete("/me/sessions/{token}", adminH.RevokeMySession)
		r.Delete("/me/sessions", adminH.RevokeMyOtherSessions)
		// MCP API tokens: long-lived bearer credentials a user pastes
		// into Claude desktop / claude.ai connectors so the LLM can
		// call /mcp on their behalf. Plaintext is returned once at
		// creation; subsequent reads only surface name + prefix.
		r.Post("/me/mcp-tokens", adminH.CreateMCPToken)
		r.Get("/me/mcp-tokens", adminH.ListMCPTokens)
		r.Delete("/me/mcp-tokens/{id}", adminH.RevokeMCPToken)
		// TOTP 2FA: status, enrol (setup → confirm), disable, and
		// recovery-code regenerate. Disable + regenerate require
		// X-Confirm-Password reauth.
		r.Get("/me/totp", adminH.TOTPStatus)
		r.Post("/me/totp/setup", adminH.SetupTOTP)
		r.Post("/me/totp/confirm", adminH.ConfirmTOTP)
		r.Post("/me/totp/disable", adminH.DisableTOTP)
		r.Post("/me/totp/recovery-codes/regenerate", adminH.RegenerateTOTPRecoveryCodes)

		// Outbound webhooks: per-org subscription CRUD. Org-scoped
		// (handlers reject admins without an org). The kinds catalog
		// is read-only for clients to drive their UI.
		r.Get("/webhooks", adminH.ListWebhooks)
		r.Post("/webhooks", adminH.CreateWebhook)
		r.Patch("/webhooks/{endpointID}", adminH.UpdateWebhook)
		r.Delete("/webhooks/{endpointID}", adminH.DeleteWebhook)
		r.Post("/webhooks/{endpointID}/rotate-secret", adminH.RotateWebhookSecret)
		r.Post("/webhooks/{endpointID}/test", adminH.SendTestWebhook)
		r.Get("/webhook-event-kinds", adminH.ListWebhookEventKinds)
		// Delivery log: every attempt, statuses, errors, the raw
		// payload bytes. Org-scoped via the join to webhook_endpoints
		// inside the handler.
		r.Get("/webhooks/deliveries", adminH.ListWebhookDeliveries)

		// Global admin only.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdmin))
			r.Get("/organizations", adminH.ListOrganizations)
			r.Post("/organizations", adminH.CreateOrganization)
			r.Patch("/organizations/{orgID}", adminH.UpdateOrganization)
			r.Post("/organizations/{orgID}/trusted", adminH.SetOrgTrusted)
			r.Delete("/organizations/{orgID}", adminH.DeleteOrganization)
			// Brand logo upload — surfaced on the recap PDF cover and
			// (later) on the play / arrange chrome. PNG/JPEG/WebP only,
			// re-encoded by imageutil.Shrink.
			r.Post("/organizations/{orgID}/logo", adminH.SetOrgLogo)
			r.Delete("/organizations/{orgID}/logo", adminH.DeleteOrgLogo)
			r.Post("/organizations/{orgID}/assignments", adminH.CreateAssignment)
			r.Patch("/organizations/{orgID}/assignments/{assignmentID}", adminH.UpdateAssignment)
			r.Delete("/organizations/{orgID}/assignments/{assignmentID}", adminH.DeleteAssignment)

			r.Get("/admins", adminH.ListAdmins)
			r.Post("/admins", adminH.CreateAdmin)
			r.Delete("/admins/{userID}", adminH.DeleteAdmin)

			// Operator-tunable runtime settings. Admin-only because
			// these knobs affect everyone hitting the deployment —
			// public signup, the marketing landing, etc.
			r.Get("/settings", adminH.GetSettings)
			r.Put("/settings/signup", adminH.SetSignupEnabled)
			r.Put("/settings/landing", adminH.SetLandingEnabled)

			// Self-host → hosted migration. Admin-only operator
			// surface — never an org-self-service flow. Export
			// streams a portable bundle for one org; import replays
			// a bundle into a fresh org on this instance; the
			// redirect-mode toggle flips the post-cutover 301
			// forwarder. The redirect-mode path is also on the
			// redirectMode middleware allowlist so an admin can
			// turn the flag off from inside a redirected instance.
			r.Get("/migrate/export", adminH.MigrateExport)
			r.Post("/migrate/import", adminH.MigrateImport)
			r.Get("/migrate/redirect-mode", adminH.MigrateRedirectMode)
			r.Post("/migrate/redirect-mode", adminH.MigrateRedirectMode)
			r.Get("/migrate/maintenance", adminH.MigrateMaintenance)
			r.Post("/migrate/maintenance", adminH.MigrateMaintenance)

			// Read-only "what's wired in?" panel — surfaces the
			// build's pluggable seams (mailer/payments/etc.) and
			// whether each is enabled in this deployment. Drives the
			// Settings → Installed components card.
			r.Get("/components", adminH.ListComponents)

			// Software-update surface: cached snapshot of the upstream
			// CHANGELOG.md plus an on-demand "check now" trigger. The
			// background poll runs every 24h regardless of the GET; the
			// POST exists so an operator clicking Check now in the UI
			// gets a fresh answer in one round trip.
			r.Get("/system-update", adminH.SystemUpdate)
			r.Post("/system-update/check", adminH.SystemUpdateCheck)

			// Manual email verification — admin escape hatch when an
			// arranger never received the self-serve verification mail.
			// Idempotent + audit-logged in auth_events as
			// event=email_verify reason=admin_manual.
			r.Post("/users/{userID}/verify-email", adminH.VerifyMemberEmail)

			// Test-email knob: lets the operator verify SMTP config
			// before customers hit the broken signup flow. Body just
			// names a recipient — content is fixed server-side so
			// this can't be repurposed as a spam relay.
			r.Post("/send-test-email", adminH.SendTestEmail)

			// Refunds touch real money — admin-only. The handler
			// just calls Stripe; the actual state flip happens via
			// the charge.refunded webhook so the same code path
			// works whether the refund originated here or in the
			// Stripe Dashboard.
			r.Post("/purchases/{purchaseID}/refund", adminH.RefundPurchase)
			// Off-platform invoice tracking — admin marks an invoice
			// purchase as billed once the real invoice has been sent.
			// The Purchases page's "to bill" filter ignores billed
			// rows so the same row doesn't keep nagging.
			r.Post("/purchases/{purchaseID}/billed", adminH.SetPurchaseBilled)
			// "Makulera" — drop the row out of the to-bill filter
			// without unwinding the assignment grant. Idempotent.
			r.Post("/purchases/{purchaseID}/void", adminH.VoidPurchase)
			// Admin-on-behalf-of grant: add player slots to an org's
			// event. Free for non-trusted orgs (forced); for trusted
			// orgs the admin chooses zero (goodwill) or a positive
			// amount routed via the invoice provider.
			r.Post("/events/{eventID}/grant-slots", adminH.GrantSlotsToEvent)

			// Server logs — stream + level control. Admin-only so org
			// users can't snoop another org's messages.
			r.Get("/logs", adminH.ListLogs)
			r.Get("/logs/level", adminH.LogsLevel)
			r.Put("/logs/level", adminH.SetLogsLevel)

		})

		// Race simulator. Open to arrangers (org_admin/org_member) plus
		// global admins — each call is org-scoped via the caller's
		// session, so two arrangers in different orgs can run sims in
		// parallel without colliding.
		r.Get("/simulator", adminH.SimulatorStatus)
		r.Post("/simulator/start", adminH.SimulatorStart)
		r.Post("/simulator/pause", adminH.SimulatorPause)
		r.Post("/simulator/resume", adminH.SimulatorResume)
		r.Post("/simulator/reset", adminH.SimulatorReset)
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
}
