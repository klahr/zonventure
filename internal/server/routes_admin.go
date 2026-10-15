// Arranger / admin routes — the /api/admin REST tree, the
// /ws/arrange and /ws/logs sockets, and the /purchase/{id}
// simulated-checkout landing. Wired by registerAdminRoutes; called
// from New() in server.go.
package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/mcp"
	"github.com/klahr/zonventure/internal/oauth"
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
	// users and the platform admin have no org scope to subscribe to
	// and are excluded; a writer watches adventures only (startWatch).
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAnyRole(auth.RoleOrgAdmin, auth.RoleOrgMember, auth.RoleOrgWriter))
		r.Get("/ws/arrange", adminH.WSArrange)
	})

	// Admin / org-user surface (staff-authenticated). RequireCSRF runs
	// after RequireAnyRole so the 401 for "no session" still wins
	// (pre-auth callers don't care about the CSRF distinction).
	//
	// Three audiences: routes every staff user reaches (accounts,
	// members, purchases read-side), org-user routes (authoring and
	// running events — the platform admin gets 403, they neither author
	// nor run events), and the platform admin's operator routes.
	//
	// Requests may also authenticate with an API token (Authorization:
	// Bearer zvmcp_…, minted under /me/mcp-tokens); RequireSession
	// keeps account and credential management cookie-only.
	r.Route("/api/admin", func(r chi.Router) {
		r.Use(admin.APITokenAuth(env.cfg.DB))
		r.Use(auth.RequireAnyRole(auth.RoleAdmin, auth.RoleSupport, auth.RoleOrgAdmin, auth.RoleOrgMember, auth.RoleOrgWriter, auth.RoleLobby))
		// A writer reaches their account and authoring only (admin/writer.go).
		r.Use(admin.RestrictWriter)
		r.Use(auth.RequireCSRF())
		// A user still on the seeded default password can do nothing here
		// but change it — so admin/admin isn't a working account until
		// the operator rotates it. See auth.RequireNonDefaultPassword.
		r.Use(auth.RequireNonDefaultPassword)
		// A user whom an organisation or the admin switch requires to
		// use 2FA can do nothing here but enrol, and their API tokens
		// are refused. See auth.RequireTOTPSetup.
		r.Use(auth.RequireTOTPSetup(env.cfg.DB, env.cfg.TOTPKey != nil))
		// Every change a global admin makes here, whichever group the
		// route sits in, goes in the admin action log.
		r.Use(admin.LogAdminActions(env.cfg.DB))

		// Staff password reset (admins & org users).
		r.With(auth.RequireSession).Post("/users/{userID}/password", adminH.ResetPassword)
		// Admin TOTP factory-reset — recovery path for users who lost
		// both their authenticator and every recovery code. Same
		// authorization rules as the password reset above; requires
		// X-Confirm-Password from the acting admin. Support may use it
		// too; a global admin's reset goes in the admin action log.
		r.With(auth.RequireSession).Post("/users/{userID}/totp/disable", adminH.AdminDisableUserTOTP)
		// Username typeahead for the Organizations page's "Add user"
		// modal — global admin only. Returns up to 10 prefix matches
		// that aren't admins and aren't already members of the target org.
		r.Get("/users/search", adminH.SearchUsers)

		// Org admin or global admin: manage an organization's members + a
		// detail endpoint both admins and in-org users can read.
		r.Get("/organizations/{orgID}", adminH.GetOrganization)
		// Membership + ownership are credential-adjacent: they create
		// accounts and move who controls an org. Keep them cookie-only
		// (RequireSession) so a leaked API token can't provision members
		// or seize ownership, mirroring the account-management routes.
		r.With(auth.RequireSession).Post("/organizations/{orgID}/members", adminH.CreateOrgMember)
		r.With(auth.RequireSession).Delete("/organizations/{orgID}/members/{userID}", adminH.DeleteOrgMember)
		// Changing a member's role in place (Settings → Members); X-Confirm-Password.
		r.With(auth.RequireSession).Put("/organizations/{orgID}/members/{userID}/role", adminH.SetOrgMemberRole)
		// Withdrawing a pending invitation (Settings → Members): it stops working.
		r.With(auth.RequireSession).Delete("/organizations/{orgID}/invitations/{userID}", adminH.WithdrawOrgInvitation)
		// Atomic ownership transfer — promotes a member to org_admin
		// and demotes the calling org_admin to org_member in one tx.
		r.With(auth.RequireSession).Post("/organizations/{orgID}/transfer-ownership", adminH.TransferOwnership)
		// Whether every member must use 2FA: the org's own admins, or a
		// full global admin (whose change goes in the admin action log).
		r.With(auth.RequireSession).Put("/organizations/{orgID}/require-totp", adminH.SetOrgRequireTOTP)

		// Stripe Connect onboarding for the /store marketplace flow.
		// Each handler re-checks org_admin-of-named-org or global
		// admin; rejecting silently here would leak that endpoints
		// exist to org members of other orgs.
		r.Post("/organizations/{orgID}/stripe-connect/start", adminH.StartStripeConnect)
		r.Get("/organizations/{orgID}/stripe-connect/status", adminH.GetStripeConnectStatus)
		r.Post("/organizations/{orgID}/stripe-connect/refresh", adminH.RefreshStripeConnectStatus)
		r.Post("/organizations/{orgID}/stripe-connect/dashboard", adminH.OpenStripeConnectDashboard)

		// Purchases read-side + confirm: arrangers see their own org's,
		// the platform admin reconciles across orgs. Buying is org-only.
		r.Get("/purchases", adminH.ListPurchases)
		// Ticket sales: self-guided tickets, party bookings and hosted
		// passes, for Orders (the platform admin's Purchases tab).
		r.Get("/ticket-purchases", adminH.ListTicketSales)
		// Refunding one: the platform admin any, an org admin their own
		// org's tickets and parties; the handler checks which. As with
		// purchases, the charge.refunded webhook flips the row.
		r.Post("/ticket-purchases/{purchaseID}/refund", adminH.RefundTicketSale)

		// Gift box redemptions: org admins set up partners and their
		// products; org admins and members work the approval queue.
		// Each handler checks the caller's role and active org.
		r.Get("/partners", adminH.ListPartners)
		r.Post("/partners", adminH.CreatePartner)
		r.Patch("/partners/{partnerID}", adminH.UpdatePartner)
		r.Delete("/partners/{partnerID}", adminH.DeletePartner)
		r.Post("/partners/{partnerID}/products", adminH.CreatePartnerProduct)
		r.Patch("/partner-products/{productID}", adminH.UpdatePartnerProduct)
		r.Delete("/partner-products/{productID}", adminH.DeletePartnerProduct)
		r.Get("/redemptions", adminH.ListRedemptions)
		r.Post("/redemptions", adminH.CreateRedemption)
		r.Patch("/redemptions/{redemptionID}", adminH.UpdateRedemption)
		r.Patch("/ticket-purchases/{purchaseID}", adminH.UpdatePurchaseOrder)
		r.Post("/redemptions/{redemptionID}/approve", adminH.ApproveRedemption)
		r.Post("/redemptions/{redemptionID}/reject", adminH.RejectRedemption)
		r.Post("/redemptions/{redemptionID}/cancel", adminH.CancelRedemption)
		// Requests for a quote from the organisation's public form
		// (admin/quote_requests.go). Org admins only, checked inside.
		r.Get("/requests", adminH.ListQuoteRequests)
		r.Patch("/requests/{id}", adminH.UpdateQuoteRequest)
		r.Delete("/requests/{id}", adminH.DeleteQuoteRequest)
		r.Get("/purchases/{purchaseID}", adminH.GetPurchase)
		r.Get("/purchases/{purchaseID}/receipt", adminH.PurchaseReceipt)

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
		// Answering an organisation's invitation to join it (see
		// CreateOrgMember). Cookie-only: a token must not add its
		// owner to an organisation.
		r.With(auth.RequireSession).Post("/me/org-invitations/{orgID}/accept", adminH.AcceptOrgInvitation)
		r.With(auth.RequireSession).Post("/me/org-invitations/{orgID}/decline", adminH.DeclineOrgInvitation)

		// The author's last position, reported by their phone while
		// building on site and read by an assistant placing zones.
		// No RequireSession: an MCP token reads it.
		r.Put("/me/position", adminH.PutMyPosition)
		r.Get("/me/position", adminH.GetMyPosition)

		// OAuth consent, from the console's #/authorize page. Cookie
		// sessions only: a token must not approve more tokens.
		if o := oauth.New(env.cfg.DB, env.cfg.PublicURL); mcp.Enabled() && o.Enabled() {
			r.With(auth.RequireSession).Get("/oauth/request", o.Request)
			r.With(auth.RequireSession).Post("/oauth/approve", o.Approve)
		}

		// Account settings — every logged-in staff user can change
		// their own password / email / username / delete themselves. No role
		// gate beyond the surrounding RequireAnyRole.
		r.With(auth.RequireSession).Post("/me/password", adminH.ChangeMyPassword)
		r.With(auth.RequireSession).Post("/me/email", adminH.ChangeMyEmail)
		r.With(auth.RequireSession).Post("/me/username", adminH.ChangeMyUsername)
		r.With(auth.RequireSession).Put("/me/display-name", adminH.SetMyDisplayName)
		// The console's welcome dialog, shown once per person (admin/me.go).
		r.With(auth.RequireSession).Post("/me/welcome-seen", adminH.MarkWelcomeSeen)
		r.With(auth.RequireSession).Delete("/me/welcome-seen", adminH.ForgetWelcomeSeen)
		// A person's own picture (admin/me_avatar.go).
		r.With(auth.RequireSession).Post("/me/avatar", adminH.SetMyAvatar)
		r.With(auth.RequireSession).Delete("/me/avatar", adminH.DeleteMyAvatar)
		r.Get("/users/{userID}/avatar", adminH.ServeUserAvatar)
		// Which notification emails the caller gets (admin/emails.go).
		r.Get("/me/email-prefs", adminH.GetEmailPrefs)
		r.With(auth.RequireSession).Put("/me/email-prefs", adminH.SetEmailPrefs)
		// The console's language, for the notifications the caller gets.
		r.With(auth.RequireSession).Put("/me/language", adminH.SetMyLanguage)
		// Revertable email change: ConfirmEmailChange stashes the old
		// address; this endpoint reads the status and reverts within
		// the grace window.
		r.Get("/me/email/revert-status", adminH.MyEmailChangeStatus)
		r.With(auth.RequireSession).Post("/me/email/revert", adminH.RevertMyEmailChange)
		// Drops an email change still waiting for confirmation.
		r.With(auth.RequireSession).Delete("/me/email/pending", adminH.CancelMyEmailChange)
		r.With(auth.RequireSession).Delete("/me", adminH.DeleteMyAccount)
		// Account deletion is soft — DeleteMyAccount sets a 7-day
		// timer; this endpoint clears it before the sweeper runs.
		r.With(auth.RequireSession).Post("/me/cancel-deletion", adminH.CancelMyDeletion)
		// Stripe Customer Portal: hosted UI for the org's payment
		// method, invoices, and cancellations. Returns a one-shot
		// session URL the client redirects to.
		r.With(auth.RequireSession).Post("/me/portal-session", adminH.CreatePortalSession)
		// GDPR data export — JSON dump of personal + (when scoped)
		// org data, served as a downloadable file.
		r.With(auth.RequireSession).Get("/me/export", adminH.ExportMyData)
		// Self audit feed — every logged-in user can see their own
		// recent login / password / email events for transparency.
		r.Get("/me/audit", adminH.ListMyAuthEvents)
		// Active sessions: list + per-token revoke + log-out-everywhere.
		r.With(auth.RequireSession).Get("/me/sessions", adminH.ListMySessions)
		r.With(auth.RequireSession).Delete("/me/sessions/{token}", adminH.RevokeMySession)
		r.With(auth.RequireSession).Delete("/me/sessions", adminH.RevokeMyOtherSessions)
		// MCP API tokens: long-lived bearer credentials a user pastes
		// into Claude desktop / claude.ai connectors so the LLM can
		// call /mcp on their behalf. Plaintext is returned once at
		// creation; subsequent reads only surface name + prefix.
		r.With(auth.RequireSession).Post("/me/mcp-tokens", adminH.CreateMCPToken)
		// A listing reviewer's passkeys, which sign their decisions.
		r.With(auth.RequireSession).Get("/me/reviewer-passkeys", adminH.ListReviewerPasskeys)
		r.With(auth.RequireSession).Post("/me/reviewer-passkeys/begin", adminH.BeginReviewerPasskey)
		r.With(auth.RequireSession).Post("/me/reviewer-passkeys/finish", adminH.FinishReviewerPasskey)
		r.With(auth.RequireSession).Delete("/me/reviewer-passkeys/{id}", adminH.RevokeReviewerPasskey)
		// Listing review: what organisations put up for sale waits here
		// for a listing reviewer — any account a platform admin made one,
		// in an organisation or none. The handlers check the flag.
		r.Get("/listing-reviews", adminH.ListListingReviews)
		r.Get("/listing-reviews/log", adminH.ListListingLog)
		r.Get("/listing-reviews/log/verify", adminH.VerifyListingLog)
		r.Get("/listing-reviews/{kind}/{id}", adminH.GetListingReview)
		r.Get("/listing-reviews/{kind}/{id}/media/{ref}", adminH.GetListingReviewMedia)
		r.Post("/listing-reviews/{kind}/{id}/sign", adminH.SignListingDecision)
		r.Post("/listing-reviews/{kind}/{id}/approve", adminH.ApproveListing)
		r.Post("/listing-reviews/{kind}/{id}/reject", adminH.RejectListing)
		// Organisation page changes, reviewed after they apply
		// (admin/org_page_changes.go).
		r.Get("/org-page-changes", adminH.ListOrgPageChanges)
		r.Get("/org-page-changes/{id}/previous", adminH.OrgPageChangePrevious)
		r.Post("/org-page-changes/{id}/approve", adminH.ApproveOrgPageChange)
		r.Post("/org-page-changes/{id}/roll-back", adminH.RollBackOrgPageChange)
		r.With(auth.RequireSession).Get("/me/mcp-tokens", adminH.ListMCPTokens)
		r.With(auth.RequireSession).Delete("/me/mcp-tokens/{id}", adminH.RevokeMCPToken)
		// TOTP 2FA: status, enrol (setup → confirm), disable, and
		// recovery-code regenerate. Disable + regenerate require
		// X-Confirm-Password reauth.
		r.With(auth.RequireSession).Get("/me/totp", adminH.TOTPStatus)
		r.With(auth.RequireSession).Post("/me/totp/setup", adminH.SetupTOTP)
		r.With(auth.RequireSession).Post("/me/totp/confirm", adminH.ConfirmTOTP)
		r.With(auth.RequireSession).Post("/me/totp/disable", adminH.DisableTOTP)
		r.With(auth.RequireSession).Post("/me/totp/recovery-codes/regenerate", adminH.RegenerateTOTPRecoveryCodes)

		// Org users only: authoring and running events.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAnyRole(auth.RoleOrgAdmin, auth.RoleOrgMember, auth.RoleOrgWriter))
			r.Get("/adventures", adminH.ListAdventures)
			r.Get("/adventures/{slug}", adminH.AdventureDetail)
			// Live markdown → HTML for the arrange editor's preview panes.
			// Pure function; no DB access, no org scoping needed.
			r.Post("/markdown/preview", adminH.PreviewMarkdown)

			// Parties: which adventures the org sells for children's
			// parties, at what price. Setting one is the org admin's
			// (checked in the handler).
			r.Get("/party-offers", adminH.ListPartyOffers)
			r.Put("/party-offers/{slug}", adminH.PutPartyOffer)
			r.Post("/party-offers/{slug}/submit", adminH.SubmitPartyOffer)
			// Held-back changes to the org page and adventures (presentation.go).
			r.Get("/orgs/me/proposal", adminH.GetMyOrgProposal)
			r.Post("/orgs/me/proposal/submit", adminH.SubmitMyOrgProposal)
			r.Delete("/orgs/me/proposal", adminH.DiscardMyOrgProposal)
			r.Get("/orgs/me/proposal/{which}", adminH.GetMyOrgProposalPicture)
			r.Get("/adventures/{slug}/proposal", adminH.GetAdventureProposal)
			r.Post("/adventures/{slug}/proposal/submit", adminH.SubmitAdventureProposal)
			r.Delete("/adventures/{slug}/proposal", adminH.DiscardAdventureProposal)
			r.Get("/adventures/{slug}/proposal/image", adminH.GetAdventureProposalPicture)
			// A hosted event's host page, in the console: the same
			// handlers as /api/party/{token}/…, the event authorised by
			// org instead of by host link.
			r.Get("/events/{eventID}/hosted", adminH.GetHostParty)
			r.Get("/events/{eventID}/hosted/qr", adminH.HostPartyQR)
			r.Post("/events/{eventID}/hosted/finish-game", adminH.FinishHostedGame)
			r.Post("/events/{eventID}/hosted/send-player-link", adminH.SendHostedPlayerLink)
			r.Post("/events/{eventID}/hosted/teams", adminH.AddHostedTeams)
			r.Post("/events/{eventID}/hosted/resend-host-link", adminH.ResendHostLinkToBuyer)
			// Host it: a hosted event of the org's own adventure, at the
			// platform's pass price (hosted_pricing.go).
			r.Get("/hosted-price", adminH.GetHostedPrice)
			r.Post("/adventures/{slug}/host", adminH.HostAdventure)
			// Quiz trails: adventures written in the simple creator as
			// cards (trails.go, trail_pdf.go).
			r.Get("/trail-templates", adminH.ListTrailTemplates)
			// Keep it: a public quiz trail moves into the caller's org.
			r.Post("/trail-keep", adminH.KeepTrail)
			r.Post("/trails", adminH.CreateTrail)
			r.Get("/adventures/{slug}/cards", adminH.GetTrail)
			r.Put("/adventures/{slug}/cards", adminH.PutTrail)
			r.Post("/adventures/{slug}/cards/publish", adminH.PublishTrail)
			r.Post("/adventures/{slug}/cards/designer", adminH.OpenTrailInDesigner)
			r.Put("/adventures/{slug}/cards/theme", adminH.PutTrailTheme)
			r.Get("/adventures/{slug}/cards.pdf", adminH.TrailPDF)
			r.Get("/events/{eventID}/hosted/cards.pdf", adminH.HostedTrailPDF)
			r.Put("/events/{eventID}/hosted/personalise", adminH.PutHostPersonalise)
			r.Put("/events/{eventID}/hosted/mode", adminH.PutHostMode)
			r.Put("/events/{eventID}/hosted/hold-results", adminH.PutHoldResults)
			r.Put("/events/{eventID}/hosted/places/{slug}", adminH.PutHostPlace)
			r.Post("/events/{eventID}/hosted/places/shift", adminH.PostHostShift)

			r.Get("/events", adminH.ListEvents)
			r.Post("/events", adminH.CreateEvent)
			r.Get("/events/{eventID}", adminH.EventDetail)
			r.Patch("/events/{eventID}", adminH.UpdateEvent)
			r.Post("/events/{eventID}/cover", adminH.SetEventCover)
			// The event's tamper-evident results record, recomputed.
			r.Get("/events/{eventID}/results/verify", adminH.VerifyEventResults)
			r.Post("/events/{eventID}/listing/submit", adminH.SubmitEventListing)
			r.Delete("/events/{eventID}/cover", adminH.DeleteEventCover)
			// Files the players can download before the start (and after).
			r.Get("/events/{eventID}/files", adminH.ListEventFiles)
			r.Post("/events/{eventID}/files", adminH.AddEventFile)
			r.Post("/events/{eventID}/files/announce", adminH.AnnounceEventFiles)
			r.Get("/events/{eventID}/files/{fileID}", adminH.GetEventFile)
			r.Delete("/events/{eventID}/files/{fileID}", adminH.DeleteEventFile)
			// Which adventure version a self-guided event plays, and moving
			// it to the latest (through listing review when it is for sale).
			r.Get("/events/{eventID}/version", adminH.EventVersion)
			r.Post("/events/{eventID}/version/update", adminH.UpdateEventVersion)
			// How the event delivers its adventure's print materials.
			r.Get("/events/{eventID}/print-delivery", adminH.EventPrintDelivery)
			r.Put("/events/{eventID}/print-delivery", adminH.SetEventPrintDelivery)
			r.Post("/events/{eventID}/start", adminH.StartEvent)
			r.Post("/events/{eventID}/pause", adminH.PauseEvent)
			r.Get("/events/{eventID}/messages", adminH.ListEventMessages)
			r.Post("/events/{eventID}/messages", adminH.SendEventMessage)
			r.Delete("/events/{eventID}/messages/{messageID}", adminH.DeleteEventMessage)
			// Team pictures for photo blocks, and the organisers' verdicts.
			r.Get("/events/{eventID}/photos", adminH.ListEventPhotos)
			r.Get("/events/{eventID}/photos/{photoID}/image", adminH.EventPhotoImage)
			r.Post("/events/{eventID}/photos/{photoID}/review", adminH.ReviewEventPhoto)
			r.Post("/events/{eventID}/resume", adminH.ResumeEvent)
			r.Post("/events/{eventID}/cancel", adminH.CancelEvent)
			r.Post("/events/{eventID}/end", adminH.EndEvent)
			r.Post("/events/{eventID}/teams", adminH.CreateTeam)
			r.Post("/events/{eventID}/teams/bulk", adminH.BulkCreateTeams)
			r.Post("/events/{eventID}/free-ticket", adminH.GiveFreeTicket)
			// Pay-later voucher codes (admin/voucher_batches.go).
			r.Get("/events/{eventID}/voucher-batches", adminH.ListVoucherBatches)
			r.Post("/events/{eventID}/voucher-batches", adminH.CreateVoucherBatch)
			r.Get("/voucher-batches/{batchID}/codes.csv", adminH.VoucherBatchCSV)
			r.Post("/voucher-batches/{batchID}/void", adminH.VoidVoucherBatch)
			// Team-roster file import — multipart upload, returns a
			// parsed preview the client then submits to /teams/bulk
			// after the arranger inspects it.
			r.Post("/events/{eventID}/teams/import", adminH.ImportTeams)
			r.Get("/events/{eventID}/positions", adminH.ListPositions)
			r.Get("/events/{eventID}/activity", adminH.ListActivity)
			r.Get("/events/{eventID}/tracks", adminH.ListTracks)
			r.Get("/events/{eventID}/results.csv", adminH.ExportEventResults)
			r.Get("/events/{eventID}/statistics", adminH.EventStatistics)
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

			// Emails to an event's teams, in its look (admin/emails.go).
			r.Get("/events/{eventID}/emails", adminH.ListEventEmails)
			r.Post("/events/{eventID}/emails", adminH.EmailTeams)
			r.Put("/events/{eventID}/email-theme", adminH.SetEventEmailTheme)
			r.Get("/email-themes", adminH.EmailThemes)
			r.Get("/email-preview", adminH.EmailPreview)
			r.Post("/email-preview", adminH.DraftPreview)
			r.Post("/email-test", adminH.SendMeTest)

			r.Patch("/teams/{teamID}", adminH.UpdateTeam)
			r.Delete("/teams/{teamID}", adminH.DeleteTeam)
			r.Post("/teams/{teamID}/avatar", adminH.SetTeamAvatar)
			r.Delete("/teams/{teamID}/avatar", adminH.DeleteTeamAvatar)
			r.Post("/teams/{teamID}/slots", adminH.AddSlot)
			r.Delete("/teams/{teamID}/slots/{slotID}", adminH.RemoveSlot)
			r.Post("/teams/{teamID}/slots/{slotID}/rotate", adminH.RotateSlotToken)
			r.Post("/teams/{teamID}/unlock", adminH.UnlockTeam)
			r.Post("/teams/{teamID}/start-anywhere", adminH.LetTeamStartAnywhere)
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

			// Caller's own assignments list.
			r.Get("/assignments", adminH.ListMyAssignments)

			// Adventure authoring. Handlers enforce ownership inline: org
			// admins edit their own org's adventures.
			r.Get("/adventures/{slug}/edit", adminH.LoadAdventureForEdit)

			// The adventure as a graph (internal/graph). Saving creates a new
			// version.
			r.Get("/adventures/{slug}/graph", adminH.GetAdventureGraph)
			r.Put("/adventures/{slug}/graph", adminH.PutAdventureGraph)
			r.Post("/adventures/{slug}/graph/publish", adminH.PublishAdventureGraph)
			r.Get("/adventures/{slug}/graph/versions", adminH.ListAdventureGraphVersions)
			r.Get("/adventures/{slug}/graph/versions/{version}", adminH.GetAdventureGraphVersion)
			r.Post("/adventures/{slug}/graph/versions/{version}/publish", adminH.PublishAdventureGraphVersion)
			r.Patch("/adventures/{slug}/graph/versions/{version}", adminH.LabelAdventureGraphVersion)
			r.Post("/adventures/{slug}/graph/validate", adminH.ValidateAdventureGraph)
			r.Get("/adventures/{slug}/graph/qr", adminH.AnswerQR)

			// Working on it together (admin/collab.go): conversations on
			// steps, where each step is in the work, what each author saw.
			r.Get("/adventures/{slug}/collab", adminH.GetCollab)
			r.Post("/adventures/{slug}/threads", adminH.CreateThread)
			r.Post("/adventures/{slug}/threads/{threadID}/comments", adminH.ReplyToThread)
			r.Patch("/adventures/{slug}/threads/{threadID}", adminH.UpdateThread)
			r.Patch("/adventures/{slug}/comments/{commentID}", adminH.EditComment)
			r.Delete("/adventures/{slug}/comments/{commentID}", adminH.DeleteComment)
			r.Put("/adventures/{slug}/steps/{node}/status", adminH.SetStepStatus)
			r.Post("/adventures/{slug}/reactions", adminH.ToggleReaction)
			r.Get("/adventures/{slug}/seen", adminH.GetSeen)
			r.Post("/adventures/{slug}/seen", adminH.MarkSeen)
			// Play the unsaved draft with the live rules: the designer's
			// preview and simulator. Stateless — the client carries the team.
			r.Post("/adventures/{slug}/simulate", adminH.SimulateAdventureGraph)
			r.Post("/adventures/{slug}/dev-ticket", adminH.CreateDevTicket)
			r.Post("/adventures/{slug}/dev-ticket/send", adminH.SendDevTicket)
			r.Get("/adventures/{slug}/dev-screen", adminH.DevScreen)
			r.Post("/adventures/{slug}/dev-goto", adminH.DevGoTo)
			r.Post("/adventures/{slug}/dev-reset", adminH.DevReset)
			r.Post("/adventures/{slug}/simulate-event", adminH.SimulateEvent)
			r.Post("/adventures/{slug}/media", adminH.UploadAdventureMedia)
			r.Post("/adventures", adminH.CreateAdventure)
			r.Patch("/adventures/{slug}", adminH.UpdateAdventure)
			r.Delete("/adventures/{slug}", adminH.DeleteAdventure)
			r.Post("/adventures/{slug}/avatar", adminH.SetAdventureAvatar)
			r.Delete("/adventures/{slug}/avatar", adminH.DeleteAdventureAvatar)
			// Clone a visible adventure (catalog row or template) into the
			// caller's org. Authoring-tier action: org_admin only. See
			// internal/admin/duplicate.go for the transactional copy logic.
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
			r.Get("/org-themes", adminH.ListOrgThemes)
			r.Post("/org-themes", adminH.CreateOrgTheme)
			r.Put("/org-themes/{id}", adminH.UpdateOrgTheme)
			r.Delete("/org-themes/{id}", adminH.DeleteOrgTheme)
			r.Get("/orgs/me/slug", adminH.GetMyOrgSlug)
			r.Put("/orgs/me/slug", adminH.SetMyOrgSlug)
			// The organisation's name; like the tagline, reviewed after it
			// applies once something is for sale (org_name.go).
			r.Put("/orgs/me/name", adminH.SetMyOrgName)
			// Public-profile read/write (tagline, website, cover image)
			// surfaced on /o/{slug}. Read is org-user, writes are
			// gated to RoleOrgAdmin inside each handler.
			// Where the organisation posts printed letters to buyers.
			r.Get("/orgs/me/ship-countries", adminH.GetMyShipCountries)
			r.Put("/orgs/me/ship-countries", adminH.SetMyShipCountries)
			// The contact address its mails reply to, and its time zone.
			r.Get("/orgs/me/contact", adminH.GetMyOrgContact)
			r.Put("/orgs/me/contact", adminH.SetMyOrgContact)
			// Its balance, how it pays for its teams, top-ups and the
			// saved card (admin/billing.go). Org admins only, checked inside.
			r.Get("/orgs/me/billing", adminH.GetMyBilling)
			r.Put("/orgs/me/billing", adminH.SetMyBilling)
			r.Post("/orgs/me/billing/top-up", adminH.TopUpMyBalance)
			r.Post("/orgs/me/billing/card", adminH.SaveMyCard)
			r.Delete("/orgs/me/billing/card", adminH.RemoveMyCard)
			r.Post("/orgs/me/billing/pay", adminH.PayMyBills)
			// Whether it takes requests for a quote, and the form's intro.
			r.Get("/orgs/me/requests", adminH.GetMyQuoteSettings)
			r.Put("/orgs/me/requests", adminH.SetMyQuoteSettings)
			r.Get("/orgs/me/email-theme", adminH.GetOrgEmailTheme)
			r.Put("/orgs/me/email-theme", adminH.SetOrgEmailTheme)
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
			// The organisation's own domain (admin/domains.go). Read is
			// org-user; claiming, removing and sending instructions are
			// gated to RoleOrgAdmin inside each handler.
			r.Get("/orgs/me/domain", adminH.GetMyDomain)
			r.Put("/orgs/me/domain", adminH.SetMyDomain)
			r.Delete("/orgs/me/domain", adminH.DeleteMyDomain)
			r.Post("/orgs/me/domain/check", adminH.CheckMyDomain)
			r.Post("/orgs/me/domain/send", adminH.SendDomainInstructions)
			r.Get("/orgs/me/domain/guide/{provider}", adminH.DomainGuide)
			r.Get("/orgs/me/domain/brand", adminH.GetMyDomainBrand)
			r.Put("/orgs/me/domain/brand", adminH.SetMyDomainBrand)

			// Purchases: arrangers buy player-slot assignments here. The
			// simulated flow just needs the confirm endpoint; a future
			// Stripe integration will also hit /confirm from a webhook.

			// Outbound webhooks: per-org subscription CRUD. Org-scoped
			// (handlers reject callers without an org). The kinds catalog
			// is read-only for clients to drive their UI. Creating, editing
			// and rotating are cookie-only: a leaked API token must not set
			// up a signed event feed that outlives revoking the token.
			r.Get("/webhooks", adminH.ListWebhooks)
			r.With(auth.RequireSession).Post("/webhooks", adminH.CreateWebhook)
			r.With(auth.RequireSession).Patch("/webhooks/{endpointID}", adminH.UpdateWebhook)
			r.Delete("/webhooks/{endpointID}", adminH.DeleteWebhook)
			r.With(auth.RequireSession).Post("/webhooks/{endpointID}/rotate-secret", adminH.RotateWebhookSecret)
			r.Post("/webhooks/{endpointID}/test", adminH.SendTestWebhook)
			r.Get("/webhook-event-kinds", adminH.ListWebhookEventKinds)
			// Delivery log: every attempt, statuses, errors, the raw
			// payload bytes. Org-scoped via the join to webhook_endpoints
			// inside the handler.
			r.Get("/webhooks/deliveries", adminH.ListWebhookDeliveries)
		})

		// Global admins, full or Support. Support reads the console and
		// may only verify an email, resend a host link and reset 2FA
		// (that route is above); everything else is full-admin only.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAnyRole(auth.RoleAdmin, auth.RoleSupport))
			r.Get("/admin-actions", adminH.ListAdminActions)
			r.Get("/organizations", adminH.ListOrganizations)
			r.Get("/listing-reviewers", adminH.ListListingReviewers)
			r.Get("/organizations/{orgID}/events", adminH.ListOrgEvents)
			// An event's teams, and mailing one team its links again.
			r.Get("/organizations/{orgID}/events/{eventID}/teams", adminH.ListOrgEventTeams)
			r.Post("/organizations/{orgID}/teams/{teamID}/invitations/send", adminH.OperatorSendTeamInvitation)
			r.Get("/catalog", adminH.ListCatalog)
			r.Get("/catalog/{slug}/shares", adminH.ListAdventureShares)

			// Manual email verification — admin escape hatch when an
			// arranger never received the self-serve verification mail.
			// Idempotent + audit-logged in auth_events as
			// event=email_verify reason=admin_manual.
			r.Post("/users/{userID}/verify-email", adminH.VerifyMemberEmail)
			// Support: find a party by its host's email and mail a
			// lost host link again.
			r.Get("/parties", adminH.AdminPartiesByHost)
			r.Post("/parties/{eventID}/resend-host-link", adminH.AdminResendHostLink)

			// Server logs — read here, level change below. Global admins
			// only so org users can't snoop another org's messages.
			r.Get("/logs", adminH.ListLogs)
			r.Get("/logs/level", adminH.LogsLevel)
			// The dashboard reads whether public sign-up is on.
			r.Get("/settings", adminH.GetSettings)

			// Global admin only.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireRole(auth.RoleAdmin))
				r.Post("/organizations", adminH.CreateOrganization)
				r.Patch("/organizations/{orgID}", adminH.UpdateOrganization)
				r.Post("/organizations/{orgID}/trusted", adminH.SetOrgTrusted)
				r.Post("/organizations/{orgID}/party-adventures", adminH.SetOrgPartyAdventures)
				r.Post("/organizations/{orgID}/verified-creator", adminH.SetOrgVerifiedCreator)
				// Grant or end an organisation's own domain.
				r.Get("/organizations/{orgID}/custom-domain", adminH.GetOrgCustomDomain)
				r.Put("/organizations/{orgID}/custom-domain", adminH.SetOrgCustomDomain)
				// Used voucher codes to invoice, and marking them invoiced.
				// Paying for teams: every org's bills, invoices marked
				// paid, an org's credit limit and adjustments
				// (admin/billing.go).
				r.Get("/billing/bills", adminH.ListBills)
				r.Post("/billing/bills/{billID}/paid", adminH.MarkBillPaid)
				r.Get("/organizations/{orgID}/billing", adminH.GetOrgBilling)
				r.Put("/organizations/{orgID}/billing", adminH.SetOrgBilling)
				// Who may review listings (the review itself is below).
				r.Post("/listing-reviewers", adminH.SetListingReviewer)
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
				// Minting / removing global admins is the most sensitive
				// credential op there is. Cookie-only (a leaked API token
				// must not create a permanent admin that outlives the token)
				// and each handler also re-checks X-Confirm-Password.
				r.With(auth.RequireSession).Post("/admins", adminH.CreateAdmin)
				r.With(auth.RequireSession).Delete("/admins/{userID}", adminH.DeleteAdmin)
				r.With(auth.RequireSession).Put("/admins/{userID}/role", adminH.SetAdminRole)
				// Mails the admin the forgot-password link; never sets the password.
				r.With(auth.RequireSession).Post("/admins/{userID}/reset-link", adminH.SendAdminResetLink)

				// Operator-tunable runtime settings. Admin-only because
				// these knobs affect everyone hitting the deployment —
				// public signup, etc. Reading them is in the group above.
				r.Put("/settings/signup", adminH.SetSignupEnabled)
				// Whether every global admin and Support user must use 2FA.
				r.With(auth.RequireSession).Put("/settings/admin-totp", adminH.SetAdminTOTPRequired)
				// The platform's prices (Settings → Pricing).
				r.Get("/pricing", adminH.GetPricing)
				r.Put("/pricing", adminH.SetPricing)
				// Who runs the platform, contacts, retention (Settings → Legal).
				r.Get("/legal", adminH.GetLegal)
				r.Put("/legal", adminH.SetLegal)
				r.Post("/legal/preview", legalPreview(env.cfg.Legal))

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
				r.Post("/purchases/{purchaseID}/unvoid", adminH.UnvoidPurchase)
				// Operator view of one org's events (Organizations page):
				// cancel (the list is in the group above).
				r.Post("/organizations/{orgID}/events/{eventID}/cancel", adminH.OperatorCancelEvent)

				// Catalog curation: the publisher org authors the catalog;
				// the operator flags the publisher, lists / unlists its
				// adventures and commissions them for other orgs.
				r.Put("/organizations/{orgID}/publisher", adminH.SetOrgPublisher)
				r.Put("/catalog/{slug}/published", adminH.SetCatalogPublished)
				r.Post("/catalog/{slug}/shares", adminH.GrantAdventureShare)
				r.Delete("/catalog/{slug}/shares/{orgID}", adminH.RevokeAdventureShare)

				r.Put("/logs/level", adminH.SetLogsLevel)
			})
		})
	})

	// Log stream socket. Lives outside the /api/admin route tree so it
	// can use the WS auth path the other sockets do. Global admins only.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAnyRole(auth.RoleAdmin, auth.RoleSupport))
		r.Get("/ws/logs", adminH.WSLogs)
	})

	// The simulated-checkout HTML page. Lives outside /api/admin so the
	// arranger's browser lands at a clean URL after clicking "Pay".
	// With Stripe this becomes a redirect target from Checkout Sessions.
}
