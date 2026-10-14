// Package admin implements the HTTP surface used by organizations to run
// adventures plus the admin surface used by global admins to manage
// organizations, their members and their assignments.
//
// Role matrix:
//
//	admin       — global. Manages organizations, members, assignments,
//	              admins. Not scoped to any organization.
//	org_admin   — belongs to one organization. All org_member powers plus
//	              invite/remove members of that organization.
//	org_member  — belongs to one organization. Runs the game: players,
//	              runs, teams.
//	player      — plays, doesn't use this surface.
package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/klahr/zonventure/internal/settings"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/activity"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/components"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/mailer"
	"github.com/klahr/zonventure/internal/payments"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/results"
	"github.com/klahr/zonventure/internal/roster"
	"github.com/klahr/zonventure/internal/storage"
	"github.com/klahr/zonventure/internal/teamimport"
	"github.com/klahr/zonventure/internal/tokenx"
	"github.com/klahr/zonventure/internal/updates"
	"github.com/klahr/zonventure/internal/webhooks"
	"github.com/klahr/zonventure/internal/ws"
)

// Handlers wires the admin endpoints to their dependencies.
type Handlers struct {
	DB  *sql.DB
	Lib *adventure.Library
	Hub *ws.Hub
	// Engine ranks a graph event's teams for the results export; see
	// public.Handlers.Engine.
	Engine *play.Engine
	// Mailer is used by /api/admin/send-test-email and best-effort
	// interactive flows. Always non-nil thanks to the server
	// constructor's LogMailer fallback. For load-bearing transactional
	// sends where a paid user ends up stranded if mail fails (the
	// post-Stripe ticket-fulfilment magic link), use MailOutbox.
	Mailer mailer.Mailer
	// MailOutbox is the durable retry layer in front of Mailer. Nil
	// when not configured (early dev setups) — callers fall back to
	// direct Mailer.Send with a logged warning.
	MailOutbox *mailer.Outbox
	// PublicURL is the externally-reachable origin used to build
	// links in outbound mail (change-email confirmation, etc.). May
	// be empty in dev — the admin handlers fall back to the request
	// Host the same way auth.Handlers does.
	PublicURL string
	// SellerIdentity is the operator's seller line ("Sold by …", in the
	// given language) for the footer of sale mail; empty, or nil, until
	// ZV_COMPANY_NAME is set.
	SellerIdentity func(lang string) string
	// Provider is the abstracted payments seam (see internal/payments).
	// Nil means the operator hasn't configured a real provider; in
	// that case the simulated provider stays the only checkout
	// option (the right default for dev clones) and the webhook
	// route returns 503.
	Provider payments.Provider
	// Pricing returns the platform's prices; nil reads the ones in
	// force (settings.CurrentPricing), a test sets its own.
	Pricing func() settings.Pricing
	// SecureCookie is the operator's force-secure override, mirrored
	// from server.Config. Cookie-emitting paths in this package pass
	// it to auth.CookieSecure for parity with the auth/player handlers.
	SecureCookie bool
	// Components lists the pluggable seams the running binary wired in
	// (mailer impl, payments impl, etc.) and whether the legal pages are
	// published, read per request by the admin-only
	// /api/admin/components endpoint that drives the Settings →
	// Installed components panel.
	Components func() []components.Component
	// Storage owns the binary asset side of the app (team avatars,
	// adventure cover art today). Always non-nil thanks to the server
	// constructor's dbblob fallback. Future S3 / local-FS impls plug
	// in here without touching this struct.
	Storage storage.Blobs
	// TOTPKey is the AES-256-GCM key wrapping users.totp_secret_encrypted.
	// nil disables the enrol surface (the /me/totp/setup endpoint
	// surfaces a clear "TOTP not configured on this server" error
	// instead of generating a secret that could never be redeemed).
	TOTPKey []byte
	// TeamImporters is the set of file-format parsers wired into
	// /events/{id}/teams/import. Constructed and populated by
	// cmd/server/main.go (the per-format subpackages register
	// themselves there) so an operator can leave excelize / etc.
	// out of their build by simply not Register()-ing them. Nil is
	// fine — the import endpoint then 415s every upload.
	TeamImporters *teamimport.Registry
	// Updates polls the upstream CHANGELOG.md and answers the
	// admin-only system-update endpoint. Nil disables the surface
	// (the handler just returns "not configured"); the server
	// constructor wires a real one based on cfg.Version +
	// cfg.ChangelogURL.
	Updates *updates.Service
	// ConfirmLimiter throttles X-Confirm-Password reauth checks
	// (requirePasswordConfirm) per (IP, user) so a stolen session can't
	// brute-force the account password — or burn Argon2id CPU — through
	// the sensitive-action endpoints. Nil disables throttling (tests).
	ConfirmLimiter *auth.LoginLimiter

	// tracks caches buildTracks per event; see tracksTTL.
	tracks tracksCache
	// live shares the console's live views between all who watch an event.
	live snapshots
}

/* ---------------- Shared scoping helpers ---------------- */

// isOrgScoped reports whether the caller is attached to an organization
// (org_admin, org_member or player) rather than being a global admin
// or a lobby user.
func isOrgScoped(u auth.User) bool { return u.OrgID != nil }

// sameOrg returns true when the caller is attached to orgID. The
// platform admin is attached to none: they don't run events.
func sameOrg(u auth.User, orgID int64) bool {
	return u.OrgID != nil && *u.OrgID == orgID
}

// eventOrg loads the organization id for an event. Returns sql.ErrNoRows if the
// event does not exist.
func (h *Handlers) eventOrg(ctx context.Context, eventID int64) (int64, error) {
	var orgID int64
	err := h.DB.QueryRowContext(ctx, `SELECT org_id FROM events WHERE id = $1`, eventID).Scan(&orgID)
	return orgID, err
}

// adventureTitle returns the human title for slug, or "" when the
// library doesn't carry it. Used to enrich webhook payloads + audit
// rows without the same nil-check dance at every call site.
func (h *Handlers) adventureTitle(slug string) string {
	if adv := h.Lib.Get(slug); adv != nil {
		return adv.Meta.Title
	}
	return ""
}

// authorizeEvent writes the appropriate HTTP error and returns false when the
// caller may not operate on the given event. Returns the event's organization
// id on success.
func (h *Handlers) authorizeEvent(w http.ResponseWriter, r *http.Request, eventID int64) (int64, bool) {
	orgID, err := h.eventOrg(r.Context(), eventID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "event not found")
		return 0, false
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return 0, false
	}
	me, _ := auth.FromContext(r.Context())
	if !sameOrg(me, orgID) || !runsEvents(me) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return 0, false
	}
	return orgID, true
}

/* ---------------- Adventure library ---------------- */

type adventureDTO struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	PageCount   int    `json:"page_count"`
	OrgID       *int64 `json:"org_id,omitempty"` // owning org; the publisher's are the catalog
	MineToEdit  bool   `json:"mine_to_edit"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Theme       string `json:"theme,omitempty"` // the look's starter theme, for a missing avatar (lookCoverSQL)
	Cover       int64  `json:"cover,omitempty"` // the look's own cover picture, a media id
	Published   bool   `json:"published"`
	Shared      bool   `json:"shared"` // visible to caller via a commissioned share
	// Language of the puzzle content (BCP 47). Surfaced on the
	// Catalog so a buyer can filter / spot adventures matching the
	// language they intend to run in. Independent of the UI locale.
	Language string `json:"language"`
	// Playable says an event could run it now: it has a published
	// graph. A new adventure has none.
	Playable bool `json:"playable"`
}

// ListAdventures returns adventures visible to the caller's org: its
// own adventures + the published catalog + anything held via an
// assignment or commissioned to the org via a share. The Catalog tab
// filters further to `!mine_to_edit && published`. The platform admin
// has no org and curates the catalog through /catalog instead.
func (h *Handlers) ListAdventures(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	// `shared` is 1 when the caller reaches the row via an adventure_shares
	// grant — the UI uses that to flag "commissioned by admin" on the card.
	query := `
		SELECT a.slug, a.title, a.description, a.author, a.org_id,
		       ` + PuzzleCountSQL() + ` AS page_count,
		       (a.published_graph_id IS NOT NULL)::int AS playable,
		       (a.avatar_mime_type IS NOT NULL)::int AS has_avatar,
		       a.published, a.language, ` + lookCoverSQL("a") + `,
	`
	query += `
	       (EXISTS (
	         SELECT 1 FROM adventure_shares s
	         WHERE s.adventure_slug = a.slug AND s.org_id = $1
	       ))::int AS shared
	FROM adventures a
	WHERE NOT a.trail_hidden AND (
	  a.org_id = $1
	  OR ` + CatalogFilter() + `
	  OR EXISTS (
	    SELECT 1 FROM adventure_assignments aa
	    WHERE aa.adventure_slug = a.slug AND aa.org_id = $1
	  )
	  OR EXISTS (
	    SELECT 1 FROM adventure_shares s
	    WHERE s.adventure_slug = a.slug AND s.org_id = $1
	  )
	)
	`
	args := []any{*me.OrgID}
	query += ` ORDER BY a.slug`

	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []adventureDTO{}
	for rows.Next() {
		var (
			d         adventureDTO
			orgID     sql.NullInt64
			hasAvatar int
			published int
			shared    int
			playable  int
		)
		if err := rows.Scan(&d.Slug, &d.Title, &d.Description, &d.Author, &orgID, &d.PageCount, &playable, &hasAvatar, &published, &d.Language, &d.Theme, &d.Cover, &shared); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if orgID.Valid {
			v := orgID.Int64
			d.OrgID = &v
		}
		d.MineToEdit = canAuthor(me, orgID)
		d.Published = published != 0
		d.Playable = playable != 0
		d.Shared = shared != 0
		if hasAvatar != 0 {
			d.AvatarURL = adventureAvatarURL(d.Slug)
		}
		out = append(out, d)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"adventures": out})
}

/* ---------------- Runs ---------------- */

type eventDTO struct {
	ID            int64  `json:"id"`
	Name          string `json:"name,omitempty"`
	AdventureSlug string `json:"adventure_slug"`
	// AdventureTitle is the human-friendly name. Joined in via the
	// list query so the UI doesn't have to round-trip per row to
	// pick a slug-or-title fallback.
	AdventureTitle string `json:"adventure_title,omitempty"`
	// AdventureAvatarURL is set when the adventure has cover art
	// uploaded, so the events list can render the same icon used on
	// the Adventures cards next to each row without a follow-up
	// fetch per event. Empty means the client should fall back to
	// the initials tile.
	AdventureAvatarURL string `json:"adventure_avatar_url,omitempty"`
	AdventureTheme     string `json:"adventure_theme,omitempty"` // stand-ins for a missing avatar (lookCoverSQL)
	AdventureCover     int64  `json:"adventure_cover,omitempty"`
	State              string `json:"state"`
	ScheduledAt        string `json:"scheduled_at,omitempty"`
	StartedAt          string `json:"started_at,omitempty"`
	// CompletedAt and CancelledAt freeze the "elapsed" counter on
	// the event-detail page once the event reaches a terminal
	// state. Without these the UI uses Date.now() and shows an
	// ever-increasing duration on a finished event.
	CompletedAt string `json:"completed_at,omitempty"`
	CancelledAt string `json:"cancelled_at,omitempty"`
	// PausedSeconds totals the event's ended pauses; PausedAt is set only
	// while it is paused. The detail payload fills them in so elapsed
	// time can leave pauses out, the same as the live board's.
	PausedSeconds   int    `json:"paused_seconds,omitempty"`
	PausedAt        string `json:"paused_at,omitempty"`
	OrgID           int64  `json:"org_id"`
	CreatedByUserID int64  `json:"created_by_user_id,omitempty"`
	AssignmentID    int64  `json:"assignment_id"`
	// MaxPlayers is the assignment's player-slot cap (0 for share-
	// backed events that have no per-event cap). The team-creation
	// gate enforces sum(player_slots in event) ≤ MaxPlayers.
	MaxPlayers int    `json:"max_players"`
	LiveToken  string `json:"live_token"`
	ShowMap    bool   `json:"show_map"`
	// HasSecretRoles says the event's adventure has a saboteur, and
	// SecretRoles whether the event deals them. Detail payload only.
	HasSecretRoles bool  `json:"has_secret_roles,omitempty"`
	SecretRoles    *bool `json:"secret_roles,omitempty"`
	// CheatPassword is the adventure's cheat password, for the event's
	// organisers to test with on a phone. Detail payload only.
	CheatPassword string `json:"cheat_password,omitempty"`
	// TeamsCount is the number of teams attached to the event. The
	// arrange UI gates the "Start now" affordance on this so a
	// scheduled event with zero teams gets a concrete error instead
	// of a vague server reject. Populated on list and detail; the
	// create response leaves it at 0 since a fresh event has no teams.
	TeamsCount int `json:"teams_count"`
	// PlayerSlotsCount is the number of player slots already created
	// across every team in this event. The arrange UI displays it as
	// "(N/M players)" alongside the team list and uses it client-side
	// to gate the Add-team button before round-tripping to the server.
	PlayerSlotsCount int `json:"player_slots_count"`
	// AllowedOps lists the lifecycle-mutating operations the server
	// will accept at this event's current state. Populated only on
	// detail responses so the client can disable affordances that
	// would otherwise round-trip to a 409. Empty for list responses
	// where the rendering only needs name/state.
	AllowedOps []string `json:"allowed_ops,omitempty"`
	// Public-store fields. Populated on detail responses; the list
	// endpoint omits them since the events table doesn't render them
	// per row. PublicForSale + a non-zero TicketPriceCents is what
	// makes a row appear on /store; the other fields tune the card.
	PublicForSale    bool   `json:"public_for_sale"`
	TicketPriceCents int    `json:"ticket_price_cents,omitempty"`
	TicketCurrency   string `json:"ticket_currency,omitempty"`
	TicketTeamSize   int    `json:"ticket_team_size,omitempty"`
	PublicBlurb      string `json:"public_blurb,omitempty"`
	// MeetingPlace is where teams meet, said in the invitation and the
	// calendar entry. Detail payload only.
	MeetingPlace string `json:"meeting_place,omitempty"`
	// Party marks a hosted event (see party.go, hosted.go): its host runs
	// it from the host page, of which the console shows the same views.
	Party bool `json:"party,omitempty"`
	// HasCoverImage tells the arrange UI whether to render the
	// upload-or-replace affordance. The bytes themselves are served
	// out-of-band at /store-cover/event/{id}.
	HasCoverImage bool `json:"has_cover_image,omitempty"`
	// Listing review, for a self-guided event:
	// where its last store changes are ("", pending, approved,
	// rejected with ListingRejectReason), and whether an approved
	// version is up (Listed).
	ListingReview       string `json:"listing_review,omitempty"`
	ListingRejectReason string `json:"listing_reject_reason,omitempty"`
	Listed              bool   `json:"listed,omitempty"`
	// ListedPriceCents and ListedCurrency are what the store sells a
	// ticket at: the approved version's, until a changed price is
	// approved.
	ListedPriceCents int    `json:"listed_price_cents,omitempty"`
	ListedCurrency   string `json:"listed_currency,omitempty"`
	// StoreBlocked says why an approved listing can't be bought and so
	// isn't in the store; see storeBlocked.
	StoreBlocked string `json:"store_blocked,omitempty"`
	// SalesStopped is a self-guided event its organiser stopped selling:
	// out of the store, while teams with a ticket play on.
	SalesStopped bool `json:"sales_stopped,omitempty"`
}

// storeBlocked is why the store keeps an approved listing out
// (public/store.go storeItemRowFilter): "no_payments" when this server
// has no payment provider, "connect_required" when a platform fee is
// set and the org's Stripe account can't take charges (connect false).
func (h *Handlers) storeBlocked(connect bool) string {
	switch {
	case !h.paymentsEnabled():
		return "no_payments"
	case h.platformFee().Enabled() && !connect:
		return "connect_required"
	}
	return ""
}

type createEventRequest struct {
	AssignmentID  int64  `json:"assignment_id"`
	AdventureSlug string `json:"adventure_slug,omitempty"`
	ScheduledAt   string `json:"scheduled_at,omitempty"`
	Name          string `json:"name,omitempty"`
	// AttestNoMinors must be true for the event to be created. The
	// arranger confirms either "no minors will participate" or "I
	// have parental consent for any under-13 players". Stored as a
	// timestamp on the event row so the consent moment is auditable.
	// A self-guided event (PublicForSale) needs none: its players buy
	// their own tickets, so the arranger cannot know who they are.
	AttestNoMinors bool `json:"attest_no_minors,omitempty"`
	// PublicForSale marks the event as a Quest at creation time —
	// listed on /store, ticket-purchased, always-on. The flag is
	// immutable after creation: a quest stays a quest, a regular
	// event stays a regular event. UpdateEvent refuses changes.
	// Default false makes every legacy create call (no field on the
	// wire) keep producing a regular event, which is what we want.
	PublicForSale bool `json:"public_for_sale,omitempty"`
}

// CreateEvent schedules an event in one of two modes:
//
//  1. Assignment-backed: the caller holds an unused adventure_assignments
//     row (from the per-event purchase flow). `assignment_id` picks it,
//     and the assignment is consumed on creation. The same path covers
//     legacy assignments minted before pay-at-schedule and the new
//     "buy then schedule" path the schedule modal walks through for
//     org-owned content.
//  2. Share-backed: an admin has commissioned the adventure for the
//     caller's org via adventure_shares. `adventure_slug` names it, and
//     no assignment is consumed — runs are effectively unlimited.
//
// Org-owned content (self-authored or forked from the Catalog) does
// NOT get a free fast-path here any more — the schedule modal mints
// an assignment via /api/admin/purchases first, free or paid, and
// then loops back to this endpoint with `assignment_id`. The
// uniform per-player meter is the entire point: a forked adventure
// runs through identical billing to a self-authored one.
func (h *Handlers) CreateEvent(w http.ResponseWriter, r *http.Request) {
	var req createEventRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if !isOrgScoped(me) {
		httpx.Error(w, http.StatusForbidden, "admins don't schedule events; do it from within an organization")
		return
	}
	if !me.EmailVerified {
		// Surface a code so the UI can swap in a "verify your email"
		// CTA instead of a generic 403 — same shape we use for
		// "password_not_set" on /login.
		httpx.JSON(w, http.StatusForbidden, map[string]string{
			"error": "verify your email before scheduling runs",
			"code":  "verification_required",
		})
		return
	}

	var (
		slug          string
		maxPlayers    int
		assignmentArg any // nil for share-backed, int64 for assignment-backed
	)
	if req.AssignmentID > 0 {
		// Validate assignment: belongs to caller's org, still unused, slug known.
		var (
			advSlug string
			orgID   int64
			used    sql.NullInt64
		)
		err := h.DB.QueryRowContext(r.Context(), `
			SELECT aa.adventure_slug, aa.org_id, aa.max_players, ar.id
			FROM adventure_assignments aa
			LEFT JOIN events ar ON ar.assignment_id = aa.id
			WHERE aa.id = $1
		`, req.AssignmentID).Scan(&advSlug, &orgID, &maxPlayers, &used)
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "assignment not found")
			return
		}
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if *me.OrgID != orgID {
			httpx.Error(w, http.StatusForbidden, "that assignment is not yours")
			return
		}
		if used.Valid {
			httpx.Error(w, http.StatusConflict, "assignment already used")
			return
		}
		slug = advSlug
		assignmentArg = req.AssignmentID
	} else if strings.TrimSpace(req.AdventureSlug) != "" {
		slug = strings.TrimSpace(req.AdventureSlug)
		// Slug-only path covers two cases:
		//   1. Share-backed regular events — an admin has commissioned
		//      this adventure for the caller's org, so per-event
		//      billing is handled out-of-band via the bilateral deal.
		//   2. Quests on org-owned adventures — per-ticket billing at
		//      /store handles revenue, so the upfront-assignment step
		//      doesn't apply: ownership of the adventure is enough.
		// Regular events on org-owned content still bounce through
		// the buy-an-assignment flow (so per-event billing is
		// recorded before play); the 409 below tells the frontend
		// to start that flow rather than retrying.
		var hasShare bool
		if err := h.DB.QueryRowContext(r.Context(), `
			SELECT EXISTS (
			  SELECT 1 FROM adventure_shares s
			  WHERE s.adventure_slug = $1 AND s.org_id = $2
			)
		`, slug, *me.OrgID).Scan(&hasShare); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if !hasShare {
			// Confirm the org can at least see the adventure; if so,
			// branch on quest-vs-regular; otherwise, plain 403.
			var advOrg sql.NullInt64
			if err := h.DB.QueryRowContext(r.Context(),
				`SELECT org_id FROM adventures WHERE slug = $1`, slug).Scan(&advOrg); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					httpx.Error(w, http.StatusNotFound, "unknown adventure")
					return
				}
				httpx.Error(w, http.StatusInternalServerError, "lookup failed")
				return
			}
			ownsIt := advOrg.Valid && *me.OrgID == advOrg.Int64
			if !ownsIt && advOrg.Valid {
				httpx.Error(w, http.StatusForbidden, "no access to that adventure")
				return
			}
			if !(ownsIt && req.PublicForSale) {
				httpx.JSON(w, http.StatusConflict, map[string]string{
					"error": "buy a player-slot assignment for this adventure first",
					"code":  "assignment_required",
				})
				return
			}
		}
	} else {
		httpx.Error(w, http.StatusBadRequest, "assignment_id or adventure_slug required")
		return
	}

	if h.Lib.Get(slug) == nil {
		httpx.Error(w, http.StatusInternalServerError, "adventure not loaded: "+slug)
		return
	}
	// An event is tied to the version published when it is created, and
	// keeps it until moved to a newer one in its settings. Without a
	// published version there is nothing to play: refuse it here rather
	// than leave an event that can never start.
	var graphID sql.NullInt64
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT published_graph_id FROM adventures WHERE slug = $1`, slug).Scan(&graphID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !graphID.Valid {
		httpx.JSON(w, http.StatusConflict, map[string]string{
			"error": "publish the adventure before creating an event for it",
			"code":  "adventure_not_published",
		})
		return
	}

	if !req.AttestNoMinors && !req.PublicForSale {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{
			"error": "you must confirm there are no under-13 players (or that you have parental consent) before creating an event",
			"code":  "minor_attestation_required",
		})
		return
	}
	// Event name is optional — but if set, cap it before it lands in
	// the events table and starts riding every WS state-change ping.
	eventName := strings.TrimSpace(req.Name)
	if _, err := validateBlurb(eventName, MaxNameLen); err != nil {
		httpx.Error(w, http.StatusBadRequest, "name "+err.Error())
		return
	}

	var schedAt any
	if req.ScheduledAt != "" {
		t, perr := parseISO8601(req.ScheduledAt)
		if perr != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid scheduled_at: "+perr.Error())
			return
		}
		schedAt = t.UTC().Format("2006-01-02 15:04:05")
	}

	liveToken, err := newLiveToken()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "token gen failed")
		return
	}
	var id int64
	// show_map is intentionally left to its column default (0 = off).
	// The /live/{token} page is shareable — competing teams who get
	// the URL would otherwise see opponents' real-time positions, a
	// reconnaissance edge in any race-format adventure. Arrangers
	// who want a closed-room spectator screen flip it on per-event
	// from the dashboard. DO NOT add show_map=1 to this INSERT.
	err = h.DB.QueryRowContext(r.Context(), `
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, scheduled_at, assignment_id, live_token, name, attested_no_minors_at, public_for_sale, graph_id)
		VALUES ($1, $2, $3, 'scheduled', $4, $5, $6, $7, CASE WHEN $8 THEN NULL ELSE now_text() END, $8, $9)
		RETURNING id
	`, slug, *me.OrgID, me.ID, schedAt, assignmentArg, liveToken, eventName, req.PublicForSale, graphID.Int64).Scan(&id)
	if err != nil {
		httpx.ServerErr(w, http.StatusConflict, "could not create run", err)
		return
	}
	activity.Record(r.Context(), h.DB, id, nil, activity.UserActor(me.ID), activity.EventCreated,
		map[string]any{"adventure_slug": slug, "max_players": maxPlayers})
	// event.scheduled fires for every fresh event — even ones with no
	// scheduled_at, since the canonical state is still "scheduled" on
	// creation. Customers wiring calendar / kickoff comms branch on
	// the scheduled_at field being non-empty.
	webhooks.TryPublish(r.Context(), h.DB, *me.OrgID, webhooks.KindEventScheduled, map[string]any{
		"event_id":        id,
		"name":            strings.TrimSpace(req.Name),
		"adventure_slug":  slug,
		"adventure_title": h.adventureTitle(slug),
		"scheduled_at":    req.ScheduledAt,
		"max_players":     maxPlayers,
	})
	h.Hub.Publish(ws.EventTopic(id), "changed", nil)
	httpx.JSON(w, http.StatusCreated, eventDTO{
		ID: id, AdventureSlug: slug, State: "scheduled",
		ScheduledAt: req.ScheduledAt, OrgID: *me.OrgID, CreatedByUserID: me.ID,
		AssignmentID: req.AssignmentID, MaxPlayers: maxPlayers,
		LiveToken: liveToken,
	})
}

// newLiveToken mints the opaque /live/{token} URL segment. 128 bits of
// entropy rendered URL-safe base64, enough to make guessing hopeless and
// short enough to fit comfortably in an SMS.
func newLiveToken() (string, error) { return tokenx.New(16) }

// ListEvents returns events visible to the caller: admins see everything,
// org users see their own org.
//
// Optional `?public_for_sale=true|false` partitions Quests (the
// public-store-listed subset) from regular events. The Events page
// passes `false` so quests don't clutter it; the Quests page passes
// `true` to render only quests. Omitted = both. `?party=true|false`
// does the same for hosted events, which the Hosted page lists.
func (h *Handlers) ListEvents(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	h.writeOrgEvents(w, r, *me.OrgID, false)
}

// ListOrgEvents is the platform operator's read-only view of one org's
// events, for the Organizations page: enough to grant slots or cancel,
// without the live token that opens the audience board.
//
// GET /api/admin/organizations/{orgID}/events
func (h *Handlers) ListOrgEvents(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad org id")
		return
	}
	h.writeOrgEvents(w, r, orgID, true)
}

// writeOrgEvents lists orgID's events, optionally filtered by
// ?public_for_sale= and ?party=. operator drops the live token.
func (h *Handlers) writeOrgEvents(w http.ResponseWriter, r *http.Request, orgID int64, operator bool) {
	query := `
		SELECT ar.id, ar.name, ar.adventure_slug, COALESCE(adv.title, ''), ar.state,
		       COALESCE(ar.scheduled_at, ''), COALESCE(ar.started_at, ''),
		       COALESCE(ar.completed_at, ''), COALESCE(ar.cancelled_at, ''),
		       ar.org_id, COALESCE(ar.created_by_user_id, 0),
		       COALESCE(ar.assignment_id, 0), COALESCE(aa.max_players, 0),
		       ar.live_token, ar.show_map,
		       COALESCE((adv.avatar_mime_type IS NOT NULL)::int, 0) AS has_avatar,
		       (SELECT COUNT(*) FROM teams WHERE event_id = ar.id) AS teams_count,
		       (SELECT COUNT(*) FROM player_slots ps
		         JOIN teams t ON t.id = ps.team_id
		        WHERE t.event_id = ar.id) AS player_slots_count,
		       ar.public_for_sale,
		       COALESCE(ar.ticket_price_cents, 0),
		       COALESCE(ar.ticket_currency, ''),
		       COALESCE(ar.ticket_team_size, 0),
		       COALESCE(ar.cover_image_mime_type, '') <> '' AS has_cover_image,
		       ar.party, ` + lookCoverSQL("adv") + `,
		       COALESCE(ar.listing_review, ''),
		       l.event_id IS NOT NULL, COALESCE(l.price_cents, 0), COALESCE(l.currency, ''),
		       o.stripe_connect_charges_enabled, ar.sales_stopped
		FROM events ar
		JOIN organizations o ON o.id = ar.org_id
		LEFT JOIN event_listings l ON l.event_id = ar.id
		LEFT JOIN adventure_assignments aa ON aa.id = ar.assignment_id
		LEFT JOIN adventures adv ON adv.slug = ar.adventure_slug
	`
	args := []any{orgID}
	// A dev event (an author trying the draft on site) is not an event to list.
	wheres := []string{"ar.org_id = $1", "NOT ar.dev"}
	for _, col := range []string{"public_for_sale", "party"} {
		v := strings.TrimSpace(r.URL.Query().Get(col))
		if v == "" {
			continue
		}
		want, err := strconv.ParseBool(v)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, col+" must be true or false")
			return
		}
		args = append(args, want)
		wheres = append(wheres, fmt.Sprintf("ar.%s = $%d", col, len(args)))
	}
	query += " WHERE " + strings.Join(wheres, " AND ")
	query += ` ORDER BY ar.created_at DESC`
	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []eventDTO{}
	for rows.Next() {
		var (
			d         eventDTO
			showMap   int
			hasAvatar int
			connect   bool
		)
		if err := rows.Scan(&d.ID, &d.Name, &d.AdventureSlug, &d.AdventureTitle, &d.State, &d.ScheduledAt, &d.StartedAt,
			&d.CompletedAt, &d.CancelledAt,
			&d.OrgID, &d.CreatedByUserID, &d.AssignmentID, &d.MaxPlayers,
			&d.LiveToken, &showMap, &hasAvatar, &d.TeamsCount, &d.PlayerSlotsCount,
			&d.PublicForSale, &d.TicketPriceCents, &d.TicketCurrency, &d.TicketTeamSize, &d.HasCoverImage, &d.Party,
			&d.AdventureTheme, &d.AdventureCover, &d.ListingReview, &d.Listed,
			&d.ListedPriceCents, &d.ListedCurrency, &connect, &d.SalesStopped); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if d.PublicForSale && d.Listed {
			d.StoreBlocked = h.storeBlocked(connect)
		}
		d.ShowMap = showMap != 0
		if hasAvatar != 0 {
			d.AdventureAvatarURL = adventureAvatarURL(d.AdventureSlug)
		}
		if operator {
			d.LiveToken = ""
		}
		out = append(out, d)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"events": out})
}

// transitionEvent performs start / pause / resume with org authorization.
func (h *Handlers) transitionEvent(w http.ResponseWriter, r *http.Request, action string) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	orgID, ok := h.authorizeEvent(w, r, eventID)
	if !ok {
		return
	}
	var (
		state         string
		publicForSale bool
		party         bool
	)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT state, public_for_sale, party FROM events WHERE id = $1`, eventID).Scan(&state, &publicForSale, &party); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	// A hosted event starts when its first team joins.
	if party && action == "start" {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "a hosted event starts when its first team joins", "reason": "event_hosted"})
		return
	}
	var want, update string
	switch action {
	case "start":
		want, update = "scheduled", `UPDATE events SET state='running', started_at=now_text() WHERE id=$1 AND state=$2`
	case "pause":
		want, update = "running", `UPDATE events SET state='paused', paused_at=now_text() WHERE id=$1 AND state=$2`
	case "resume":
		want, update = "paused", `UPDATE events SET state='running', `+activity.ClosePause+` WHERE id=$1 AND state=$2`
	default:
		httpx.Error(w, http.StatusBadRequest, "unknown action")
		return
	}
	if state != want {
		httpx.Error(w, http.StatusConflict, "cannot "+action+" an event in state "+state)
		return
	}
	// Empty-event guard: starting a 'scheduled' event with zero teams
	// produces a degenerate run state — there's no team to advance,
	// no live board to populate, and the arranger almost certainly
	// meant to add a team first. The arrange UI catches this in
	// confirmStart, but a stale cache or scripted client would
	// otherwise slip through.
	//
	// Quests (public_for_sale) intentionally bypass the guard: every
	// team comes from a /store ticket purchase AFTER the quest is
	// running, so a fresh quest is necessarily empty at start time.
	if action == "start" && !publicForSale {
		var teamCount int
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM teams WHERE event_id = $1`, eventID).Scan(&teamCount); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "team count failed")
			return
		}
		if teamCount == 0 {
			httpx.Error(w, http.StatusConflict, "add at least one team before starting the event")
			return
		}
	}
	if action == "start" {
		var published bool
		if err := h.DB.QueryRowContext(r.Context(), `
			SELECT EXISTS (SELECT 1 FROM events e JOIN adventures a ON a.slug = e.adventure_slug
			               WHERE e.id = $1 AND (e.graph_id IS NOT NULL OR a.published_graph_id IS NOT NULL))
		`, eventID).Scan(&published); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if !published {
			httpx.Error(w, http.StatusConflict, "publish the adventure before starting the event")
			return
		}
	}
	res, err := h.DB.ExecContext(r.Context(), update, eventID, want)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	// The state moved since the read above (auto-completion, a
	// concurrent transition): refuse rather than overwrite it.
	if n, _ := res.RowsAffected(); n == 0 {
		_ = h.DB.QueryRowContext(r.Context(), `SELECT state FROM events WHERE id = $1`, eventID).Scan(&state)
		httpx.Error(w, http.StatusConflict, "cannot "+action+" an event in state "+state)
		return
	}
	// Starting pins the adventure's graph and places every team. A
	// failure is audited; teams added later are placed when they first
	// open the game, and StartEvent is idempotent.
	if action == "start" {
		if err := play.Start(r.Context(), h.DB, eventID); err != nil {
			activity.Record(r.Context(), h.DB, eventID, nil, activity.Actor{},
				"graph.start.failed", map[string]any{"error": err.Error()})
		}
	}
	me, _ := auth.FromContext(r.Context())
	kind := map[string]string{"start": activity.EventStarted, "pause": activity.EventPaused, "resume": activity.EventResumed}[action]
	activity.Record(r.Context(), h.DB, eventID, nil, activity.UserActor(me.ID), kind, nil)
	// Emit the matching webhook so customers can wire kickoff
	// notifications, lobby-TV banners, etc. Only "start" has a v1
	// kind today; pause / resume are intra-event lifecycle that
	// customers haven't asked for.
	if action == "start" {
		// Best-effort: load the slug + name so the webhook payload
		// carries them. A miss just emits the webhook with empty
		// strings — better than skipping the publish.
		var (
			advSlug string
			runName string
		)
		_ = h.DB.QueryRowContext(r.Context(),
			`SELECT adventure_slug, name FROM events WHERE id = $1`, eventID).Scan(&advSlug, &runName)
		webhooks.TryPublish(r.Context(), h.DB, orgID, webhooks.KindEventStarted, map[string]any{
			"event_id":        eventID,
			"name":            runName,
			"adventure_slug":  advSlug,
			"adventure_title": h.adventureTitle(advSlug),
			"manual":          true,
		})
	}
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// StartEvent moves a scheduled event into the running state.
func (h *Handlers) StartEvent(w http.ResponseWriter, r *http.Request) {
	h.transitionEvent(w, r, "start")
}

// PauseEvent pauses a running run.
func (h *Handlers) PauseEvent(w http.ResponseWriter, r *http.Request) {
	h.transitionEvent(w, r, "pause")
}

// ResumeEvent resumes a paused run.
func (h *Handlers) ResumeEvent(w http.ResponseWriter, r *http.Request) {
	h.transitionEvent(w, r, "resume")
}

// CancelEvent terminates an event before it has finished. Allowed
// from scheduled / running / paused; rejected from completed or
// already-cancelled. Password-gated because it discards everything
// in flight (live pings, leaderboard, etc.) without any rollback —
// the same protection delete-org and delete-team-member use.
//
// Doesn't go through transitionEvent because that helper assumes a
// single source state and skips the password check; cancellation
// works from three states and needs the gate.
func (h *Handlers) CancelEvent(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	h.cancelEvent(w, r, eventID)
}

// OperatorCancelEvent is CancelEvent for the platform operator, who
// reaches an org's events from its Organizations page rather than
// running them. The event must belong to the org in the path so a
// stale page can't cancel the wrong org's event.
//
// POST /api/admin/organizations/{orgID}/events/{eventID}/cancel
func (h *Handlers) OperatorCancelEvent(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad org id")
		return
	}
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	eventOrgID, err := h.eventOrg(r.Context(), eventID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && eventOrgID != orgID) {
		httpx.Error(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	h.cancelEvent(w, r, eventID)
}

// cancelEvent is the password-gated cancellation both entry points
// share once they have authorised the caller.
//
// With {"email_teams": true} every team with a contact email is told
// the event is cancelled.
func (h *Handlers) cancelEvent(w http.ResponseWriter, r *http.Request, eventID int64) {
	if !h.requirePasswordConfirm(w, r) {
		return
	}
	var req struct {
		EmailTeams bool `json:"email_teams"`
	}
	if r.ContentLength > 0 && !httpx.DecodeJSON(w, r, &req) {
		return
	}
	var state string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT state FROM events WHERE id = $1`, eventID).Scan(&state); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if cancelRejected(w, state) {
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE events SET state='cancelled', cancelled_at=now_text(), ics_sequence = ics_sequence + 1, `+activity.ClosePause+`
		 WHERE id=$1 AND state IN ('scheduled','running','paused')`,
		eventID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = h.DB.QueryRowContext(r.Context(), `SELECT state FROM events WHERE id = $1`, eventID).Scan(&state)
		if !cancelRejected(w, state) {
			httpx.Error(w, http.StatusConflict, "cannot cancel an event in state "+state)
		}
		return
	}
	// Symmetrical to MaybeCompleteEvent: a cancelled event should
	// stop accepting player traffic immediately. Best-effort —
	// failure here doesn't unwind the cancellation.
	_ = activity.ExpireEventSlotSessions(r.Context(), h.DB, eventID)
	me, _ := auth.FromContext(r.Context())
	var payload map[string]any
	if req.EmailTeams {
		payload = map[string]any{"emailed": h.emailTeamsCancelled(r.Context(), eventID)}
	}
	activity.Record(r.Context(), h.DB, eventID, nil, activity.UserActor(me.ID), activity.EventCancelled, payload)
	if _, err := results.Append(r.Context(), h.DB, eventID, 0, results.EventCancelled, map[string]any{"by": me.Username}); err != nil {
		logx.Warn("results.append", "kind", results.EventCancelled, "event_id", eventID, "err", err.Error())
	}
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// EndEvent ends a running or paused event now, as if every team had
// finished: teams still playing keep their progress and the results and
// recaps open. It goes through activity.CompleteEvent, the same
// completion the last team's finish triggers. Password-gated like
// cancel: it signs every player out and cannot be undone.
//
// POST /api/admin/events/{eventID}/end
func (h *Handlers) EndEvent(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	orgID, ok := h.authorizeEvent(w, r, eventID)
	if !ok {
		return
	}
	var selfGuided bool
	if err := h.DB.QueryRowContext(r.Context(), `SELECT public_for_sale FROM events WHERE id = $1`, eventID).Scan(&selfGuided); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if selfGuided {
		httpx.JSON(w, http.StatusConflict, map[string]any{
			"error":  "a self-guided adventure does not end: stop selling tickets instead, or cancel it to close it for good",
			"reason": "self_guided",
		})
		return
	}
	if !h.requirePasswordConfirm(w, r) {
		return
	}
	me, _ := auth.FromContext(r.Context())
	done, err := activity.CompleteEvent(r.Context(), h.DB, eventID, activity.UserActor(me.ID),
		map[string]any{"by_arranger": true})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	if !done {
		var state string
		_ = h.DB.QueryRowContext(r.Context(), `SELECT state FROM events WHERE id = $1`, eventID).Scan(&state)
		httpx.JSON(w, http.StatusConflict, map[string]any{
			"error":  "cannot end an event in state " + state,
			"reason": reasonForState(state),
			"state":  state,
		})
		return
	}
	var advSlug string
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT adventure_slug FROM events WHERE id = $1`, eventID).Scan(&advSlug)
	webhooks.TryPublish(r.Context(), h.DB, orgID, webhooks.KindEventCompleted, map[string]any{
		"event_id":        eventID,
		"adventure_slug":  advSlug,
		"adventure_title": h.adventureTitle(advSlug),
		"manual":          true,
	})
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// cancelRejected writes the 409 for a state cancellation can't leave
// and reports whether it did.
func cancelRejected(w http.ResponseWriter, state string) bool {
	switch state {
	case "scheduled", "running", "paused":
		return false
	case "completed":
		httpx.JSON(w, http.StatusConflict, map[string]any{
			"error":  "event already completed",
			"reason": "event_completed",
			"state":  state,
		})
	case "cancelled":
		httpx.JSON(w, http.StatusConflict, map[string]any{
			"error":  "event already cancelled",
			"reason": "event_cancelled",
			"state":  state,
		})
	default:
		httpx.Error(w, http.StatusConflict, "cannot cancel an event in state "+state)
	}
	return true
}

type updateEventRequest struct {
	Name        *string `json:"name,omitempty"`
	ScheduledAt *string `json:"scheduled_at,omitempty"`
	ShowMap     *bool   `json:"show_map,omitempty"`
	// Public-store fields. nil pointers leave the column untouched
	// so a partial PATCH from the arrange UI doesn't accidentally
	// reset them. PublicForSale=false implicitly hides the row from
	// /store regardless of what the price columns say; the listing
	// query filters on public_for_sale alone.
	PublicForSale    *bool   `json:"public_for_sale,omitempty"`
	TicketPriceCents *int    `json:"ticket_price_cents,omitempty"`
	TicketCurrency   *string `json:"ticket_currency,omitempty"`
	TicketTeamSize   *int    `json:"ticket_team_size,omitempty"`
	PublicBlurb      *string `json:"public_blurb,omitempty"`
	// SecretRoles turns the adventure's saboteur on or off for this
	// event. Teams already dealt keep what they were dealt.
	SecretRoles *bool `json:"secret_roles,omitempty"`
	// SalesStopped takes a self-guided event out of the store and
	// refuses checkout, without pausing the teams that play it.
	SalesStopped *bool `json:"sales_stopped,omitempty"`
	// MeetingPlace is where teams meet, in the organiser's words; ""
	// clears it.
	MeetingPlace *string `json:"meeting_place,omitempty"`
}

// UpdateEvent amends mutable run settings. scheduled_at is only editable
// while the event is still "scheduled"; show_map can be toggled at any
// time so the arranger can turn the audience map on/off live.
func (h *Handlers) UpdateEvent(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	var req updateEventRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.ScheduledAt == nil && req.ShowMap == nil && req.Name == nil &&
		req.PublicForSale == nil && req.TicketPriceCents == nil &&
		req.TicketCurrency == nil && req.TicketTeamSize == nil && req.PublicBlurb == nil && req.SecretRoles == nil && req.SalesStopped == nil && req.MeetingPlace == nil {
		httpx.Error(w, http.StatusBadRequest, "nothing to update")
		return
	}
	// Tickets are sold in the platform's currency only: the one value
	// accepted, kept for clients that still send it.
	if req.TicketCurrency != nil && !strings.EqualFold(strings.TrimSpace(*req.TicketCurrency), settings.Currency) {
		httpx.Error(w, http.StatusBadRequest, "tickets are sold in "+strings.ToUpper(settings.Currency))
		return
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if _, err := validateBlurb(name, MaxNameLen); err != nil {
			httpx.Error(w, http.StatusBadRequest, "name "+err.Error())
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE events SET name = $1 WHERE id = $2`, name, eventID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	}

	if req.ScheduledAt != nil {
		if !h.requireEventOp(w, r.Context(), eventID, OpRescheduleEvent) {
			return
		}
		var schedAt any
		if *req.ScheduledAt != "" {
			t, perr := parseISO8601(*req.ScheduledAt)
			if perr != nil {
				httpx.Error(w, http.StatusBadRequest, "invalid scheduled_at: "+perr.Error())
				return
			}
			schedAt = t.UTC().Format("2006-01-02 15:04:05")
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE events SET scheduled_at = $1::text, ics_sequence = ics_sequence + 1 WHERE id = $2 AND scheduled_at IS DISTINCT FROM $1::text`, schedAt, eventID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	if req.MeetingPlace != nil {
		place := strings.TrimSpace(*req.MeetingPlace)
		if len([]rune(place)) > 200 {
			httpx.Error(w, http.StatusBadRequest, "meeting_place is too long (max 200 chars)")
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE events SET meeting_place = $1 WHERE id = $2`, place, eventID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	if req.SecretRoles != nil {
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE events SET secret_roles = $1 WHERE id = $2`, *req.SecretRoles, eventID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	if req.SalesStopped != nil {
		res, err := h.DB.ExecContext(r.Context(),
			`UPDATE events SET sales_stopped = $1 WHERE id = $2 AND public_for_sale`, *req.SalesStopped, eventID)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			httpx.Error(w, http.StatusConflict, "only a self-guided event sells tickets")
			return
		}
	}
	if req.ShowMap != nil {
		v := 0
		if *req.ShowMap {
			v = 1
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE events SET show_map = $1 WHERE id = $2`, v, eventID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	if req.PublicForSale != nil {
		// public_for_sale is the Quest/Event type marker — set once
		// at CreateEvent, immutable thereafter. Flipping it on a
		// live event would silently change the type (regular event
		// becomes a quest mid-flight, or vice versa) which is never
		// what the operator wants. Refuse the change unless the
		// requested value matches the stored one (idempotent no-op).
		var current bool
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT public_for_sale FROM events WHERE id = $1`, eventID).Scan(&current); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if *req.PublicForSale != current {
			httpx.Error(w, http.StatusConflict,
				"public_for_sale is set at creation time and cannot be changed afterwards")
			return
		}
	}
	if req.TicketPriceCents != nil {
		var v sql.NullInt64
		if *req.TicketPriceCents > 0 {
			v = sql.NullInt64{Int64: int64(*req.TicketPriceCents), Valid: true}
		}
		// A price is always in the platform's currency: saving one moves
		// an older listing in another currency over.
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE events SET ticket_price_cents = $1, ticket_currency = $3 WHERE id = $2`, v, eventID, settings.Currency); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	if req.TicketTeamSize != nil {
		if *req.TicketTeamSize < 1 || *req.TicketTeamSize > 50 {
			httpx.Error(w, http.StatusBadRequest, "ticket_team_size must be between 1 and 50")
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE events SET ticket_team_size = $1 WHERE id = $2`, *req.TicketTeamSize, eventID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	if req.PublicBlurb != nil {
		blurb := strings.TrimSpace(*req.PublicBlurb)
		if len(blurb) > 1000 {
			httpx.Error(w, http.StatusBadRequest, "public_blurb is too long (max 1000 chars)")
			return
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE events SET public_blurb = $1 WHERE id = $2`, blurb, eventID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	// What the store shows changed: a draft until it is submitted for review.
	if req.Name != nil || req.TicketPriceCents != nil || req.TicketTeamSize != nil || req.PublicBlurb != nil {
		if err := h.settleEventListing(r.Context(), eventID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "listing review failed", err)
			return
		}
	}
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

/* ---------------- Teams (create is here; edit/delete in teams.go) ---------------- */

// normaliseTeamEmail trims, lower-cases, and validates an optional
// team contact email. Empty input is fine (the field defaults to
// ” in the schema). Non-empty input must parse via net/mail and
// be ≤ 320 chars (the RFC 5321 ceiling). Returns the cleaned form
// or an error suitable for a 400 response.
func normaliseTeamEmail(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	if len(s) > 320 {
		return "", errors.New("email is too long (max 320 chars)")
	}
	if _, err := mail.ParseAddress(s); err != nil {
		return "", errors.New("email is not a valid address")
	}
	return strings.ToLower(s), nil
}

type createTeamRequest struct {
	Name        string   `json:"name"`
	MemberNames []string `json:"member_names"`
	// Email is optional arranger-side metadata: who to ping when
	// the team's running late, where the recap URL goes when the
	// event completes. Empty by default; the player-side share
	// page never surfaces it.
	Email string `json:"email,omitempty"`
}

// teamSlotDTO mirrors slotDTO at the team-create response level.
// ExpiresAt surfaces the magic-link cutoff so the front-end can render
// "expires in 7 days" without a follow-up fetch.
type teamSlotDTO struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type teamDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Email is the optional captain-contact address arrangers
	// fill in for ops use. omitempty so old code paths that
	// don't yet read this field don't see a "" they have to
	// special-case.
	Email   string        `json:"email,omitempty"`
	Members []teamSlotDTO `json:"members"`
}

// CreateTeam creates a team on an event together with its slots (one per
// member name). Each slot gets a freshly-minted magic-link token the
// arranger can deliver individually to each player.
func (h *Handlers) CreateTeam(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	orgID, ok := h.authorizeEvent(w, r, eventID)
	if !ok {
		return
	}
	if !h.requireEventOp(w, r.Context(), eventID, OpCreateTeam) {
		return
	}
	var req createTeamRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "team name required")
		return
	}
	email, err := normaliseTeamEmail(req.Email)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	// De-duplicate and trim member names.
	seen := map[string]struct{}{}
	names := make([]string, 0, len(req.MemberNames))
	for _, n := range req.MemberNames {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, dup := seen[strings.ToLower(n)]; dup {
			httpx.Error(w, http.StatusBadRequest, "duplicate member name: "+n)
			return
		}
		seen[strings.ToLower(n)] = struct{}{}
		names = append(names, n)
	}
	if len(names) == 0 {
		httpx.Error(w, http.StatusBadRequest, "at least one member required")
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "tx failed")
		return
	}
	defer tx.Rollback()

	// Enforce the assignment's player-slot cap. Existing slots in
	// the event + the slots this team would add must not exceed
	// max_players. Share-backed events have max_players=0 and skip
	// the gate entirely (effectively unlimited).
	var (
		maxPlayers       int
		playerSlotsCount int
	)
	if err := tx.QueryRowContext(r.Context(), `
		SELECT COALESCE(aa.max_players, 0),
		       (SELECT COUNT(*) FROM player_slots ps
		         JOIN teams t ON t.id = ps.team_id
		        WHERE t.event_id = $1)
		FROM events ar
		LEFT JOIN adventure_assignments aa ON aa.id = ar.assignment_id
		WHERE ar.id = $2
	`, eventID, eventID).Scan(&maxPlayers, &playerSlotsCount); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "cap lookup failed")
		return
	}
	if maxPlayers > 0 && playerSlotsCount+len(names) > maxPlayers {
		httpx.JSON(w, http.StatusConflict, map[string]any{
			"error": "this event's pass covers " + strconv.Itoa(maxPlayers) +
				" players and they're all in use; add players to the pass first",
			"code":        "pass_full",
			"max_players": maxPlayers,
		})
		return
	}

	teamTok, err := auth.NewSlotToken()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "token gen failed")
		return
	}
	// recap_token is minted at team-create rather than at event-end
	// so the URL is stable from day zero. The /recap/{token} route
	// gates on event.state, so the URL existing before completion
	// just 404s; no leak.
	recapTok, err := auth.NewSlotToken()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "token gen failed")
		return
	}
	voucher, err := uniqueVoucherCode(r.Context(), tx)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "token gen failed")
		return
	}
	var teamID int64
	if err := tx.QueryRowContext(r.Context(),
		`INSERT INTO teams(event_id, name, token, recap_token, email, voucher_code)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		eventID, req.Name, teamTok, recapTok, email, voucher).Scan(&teamID); err != nil {
		httpx.Error(w, http.StatusConflict, "team name already used for this event")
		return
	}

	members := make([]teamSlotDTO, 0, len(names))
	expiresAt := auth.DefaultSlotExpiresAt(r.Context(), h.DB, eventID)
	for _, name := range names {
		token, err := auth.NewSlotToken()
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "token gen failed")
			return
		}
		var slotID int64
		if err := tx.QueryRowContext(r.Context(),
			`INSERT INTO player_slots(team_id, name, token, expires_at) VALUES ($1, $2, $3, $4) RETURNING id`,
			teamID, name, token, expiresAt).Scan(&slotID); err != nil {
			httpx.ServerErr(w, http.StatusConflict, "could not add "+name, err)
			return
		}
		members = append(members, teamSlotDTO{ID: slotID, Name: name, Token: token, ExpiresAt: expiresAt})
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	me, _ := auth.FromContext(r.Context())
	memberNames := make([]string, 0, len(members))
	for _, m := range members {
		memberNames = append(memberNames, m.Name)
	}
	activity.Record(r.Context(), h.DB, eventID, &teamID, activity.UserActor(me.ID), activity.TeamCreated,
		map[string]any{"name": req.Name, "members": memberNames})
	webhooks.TryPublish(r.Context(), h.DB, orgID, webhooks.KindTeamCreated, map[string]any{
		"event_id":     eventID,
		"team_id":      teamID,
		"team_name":    req.Name,
		"member_count": len(members),
	})
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusCreated, teamDTO{ID: teamID, Name: req.Name, Email: email, Members: members})
}

type bulkTeamResultDTO struct {
	// Index points back at the request's teams[] slot so the UI can
	// highlight the offending paste line. -1 for entries the server
	// dropped during input normalisation (shouldn't happen with the
	// current code; reserved for future per-row preflight checks).
	Index int      `json:"index"`
	Name  string   `json:"name"`
	Team  *teamDTO `json:"team,omitempty"`
	Error string   `json:"error,omitempty"`
	// Code is "pass_full" when the team did not fit the event pass, so
	// the UI can offer to add seats and retry.
	Code string `json:"code,omitempty"`
}

// passFullError is createOneTeam's refusal of a team the event pass has
// no room for.
type passFullError struct{ room, need int }

func (e *passFullError) Error() string {
	return fmt.Sprintf("the event pass has room for %d more players, and this team has %d; add players to the event pass to continue", e.room, e.need)
}

// BulkCreateTeams ingests a roster — multiple teams with their member
// lists in one round-trip — and reports per-team success/failure so the
// arranger sees exactly which paste rows worked. Each team is created
// in its own transaction (a duplicate name in row 5 doesn't roll back
// rows 1-4); the assignment cap is rechecked between teams so a roster
// that exceeds the purchased slot count partially succeeds and the
// arranger can buy more.
//
// The wire format is selected by Content-Type via roster.Pick:
// application/json (the canonical UI shape) or text/csv (one team per
// row, first cell = team name, remaining cells = member names). New
// formats slot in next to internal/roster/csv.go without touching this
// handler.
//
// Returns 200 OK with a results array even when individual teams fail
// — partial success is the common case, and a 4xx would force the UI
// to lose the per-row detail. A 4xx is reserved for malformed input or
// caller-not-allowed-on-this-event.
func (h *Handlers) BulkCreateTeams(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	orgID, ok := h.authorizeEvent(w, r, eventID)
	if !ok {
		return
	}
	if !h.requireEventOp(w, r.Context(), eventID, OpCreateTeam) {
		return
	}
	imp, err := roster.Pick(r.Header.Get("Content-Type"))
	if err != nil {
		httpx.Error(w, http.StatusUnsupportedMediaType, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	teams, err := imp.Parse(r.Body)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "parse failed: "+err.Error())
		return
	}
	if len(teams) == 0 {
		httpx.Error(w, http.StatusBadRequest, "no teams in request")
		return
	}
	// Hard cap the batch size so a single request can't tie up the
	// server: 200 teams × ~10 members each is comfortably more than
	// any real tournament.
	const maxBatch = 200
	if len(teams) > maxBatch {
		httpx.Error(w, http.StatusBadRequest, "too many teams in one request")
		return
	}

	results := make([]bulkTeamResultDTO, 0, len(teams))
	createdCount := 0
	for i, t := range teams {
		t.Name = strings.TrimSpace(t.Name)
		res := bulkTeamResultDTO{Index: i, Name: t.Name}
		team, err := h.createOneTeam(r.Context(), eventID, orgID, createTeamRequest{
			Name:        t.Name,
			MemberNames: t.MemberNames,
			Email:       t.Email,
		})
		if err != nil {
			res.Error = err.Error()
			var full *passFullError
			if errors.As(err, &full) {
				res.Code = "pass_full"
			}
		} else {
			res.Team = team
			createdCount++
		}
		results = append(results, res)
	}
	if createdCount > 0 {
		h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"results": results,
		"created": createdCount,
		"failed":  len(results) - createdCount,
	})
}

// createOneTeam runs the same insert-team-and-slots pipeline as the
// single CreateTeam handler, but as a callable that returns
// (team, err) so BulkCreateTeams can iterate without re-implementing
// any of the validation / cap-checking / activity-log glue. The cap
// check happens inside the transaction so concurrent writers can't
// both squeeze past it.
func (h *Handlers) createOneTeam(ctx context.Context, eventID, orgID int64, req createTeamRequest) (*teamDTO, error) {
	if req.Name == "" {
		return nil, errors.New("team name required")
	}
	seen := map[string]struct{}{}
	names := make([]string, 0, len(req.MemberNames))
	for _, n := range req.MemberNames {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, dup := seen[strings.ToLower(n)]; dup {
			return nil, errors.New("duplicate member name: " + n)
		}
		seen[strings.ToLower(n)] = struct{}{}
		names = append(names, n)
	}
	if len(names) == 0 {
		return nil, errors.New("at least one member required")
	}
	email, err := normaliseTeamEmail(req.Email)
	if err != nil {
		return nil, err
	}
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.New("tx failed")
	}
	defer tx.Rollback()
	var (
		maxPlayers       int
		playerSlotsCount int
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(aa.max_players, 0),
		       (SELECT COUNT(*) FROM player_slots ps
		         JOIN teams t ON t.id = ps.team_id
		        WHERE t.event_id = $1)
		FROM events ar
		LEFT JOIN adventure_assignments aa ON aa.id = ar.assignment_id
		WHERE ar.id = $2
	`, eventID, eventID).Scan(&maxPlayers, &playerSlotsCount); err != nil {
		return nil, errors.New("cap lookup failed")
	}
	if maxPlayers > 0 && playerSlotsCount+len(names) > maxPlayers {
		return nil, &passFullError{room: max(maxPlayers-playerSlotsCount, 0), need: len(names)}
	}
	teamTok, err := auth.NewSlotToken()
	if err != nil {
		return nil, errors.New("token gen failed")
	}
	recapTok, err := auth.NewSlotToken()
	if err != nil {
		return nil, errors.New("token gen failed")
	}
	voucher, err := uniqueVoucherCode(ctx, tx)
	if err != nil {
		return nil, errors.New("token gen failed")
	}
	var teamID int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO teams(event_id, name, token, recap_token, email, voucher_code) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		eventID, req.Name, teamTok, recapTok, email, voucher).Scan(&teamID); err != nil {
		return nil, errors.New("team name already used for this event")
	}
	members := make([]teamSlotDTO, 0, len(names))
	expiresAt := auth.DefaultSlotExpiresAt(ctx, h.DB, eventID)
	for _, name := range names {
		token, err := auth.NewSlotToken()
		if err != nil {
			return nil, errors.New("token gen failed")
		}
		var slotID int64
		if err := tx.QueryRowContext(ctx,
			`INSERT INTO player_slots(team_id, name, token, expires_at) VALUES ($1, $2, $3, $4) RETURNING id`,
			teamID, name, token, expiresAt).Scan(&slotID); err != nil {
			return nil, errors.New("could not add " + name + ": " + err.Error())
		}
		members = append(members, teamSlotDTO{ID: slotID, Name: name, Token: token, ExpiresAt: expiresAt})
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.New("commit failed")
	}
	me, _ := auth.FromContext(ctx)
	memberNames := make([]string, 0, len(members))
	for _, m := range members {
		memberNames = append(memberNames, m.Name)
	}
	activity.Record(ctx, h.DB, eventID, &teamID, activity.UserActor(me.ID), activity.TeamCreated,
		map[string]any{"name": req.Name, "members": memberNames})
	webhooks.TryPublish(ctx, h.DB, orgID, webhooks.KindTeamCreated, map[string]any{
		"event_id":     eventID,
		"team_id":      teamID,
		"team_name":    req.Name,
		"member_count": len(members),
	})
	return &teamDTO{ID: teamID, Name: req.Name, Email: email, Members: members}, nil
}

/* ---------------- Positions & Tracks ---------------- */

type positionDTO struct {
	TeamID   int64   `json:"team_id"`
	TeamName string  `json:"team_name"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	ServerTS string  `json:"server_ts"`
	// Node is the graph step the team is on.
	Node string `json:"node,omitempty"`
}

// buildPositions is the query-only helper used by both the HTTP ListPositions
// handler and the WS push path. Caller does authorisation.
func (h *Handlers) buildPositions(ctx context.Context, eventID int64) ([]positionDTO, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name, COALESCE(gs.current_node, ''),
		       COALESCE(p.lat, 0), COALESCE(p.lng, 0), COALESCE(p.server_ts, '')
		FROM teams t
		LEFT JOIN team_graph_state gs ON gs.team_id = t.id
		-- Each team's latest ping, read off idx_pings_team_ts rather than
		-- by scanning the event's whole ping history on every push.
		LEFT JOIN LATERAL (
			SELECT lat, lng, server_ts
			FROM location_pings
			WHERE team_id = t.id
			ORDER BY server_ts DESC, id DESC
			LIMIT 1
		) p ON true
		WHERE t.event_id = $1
		ORDER BY `+teamNameOrder+`
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []positionDTO{}
	for rows.Next() {
		var d positionDTO
		if err := rows.Scan(&d.TeamID, &d.TeamName, &d.Node, &d.Lat, &d.Lng, &d.ServerTS); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListPositions returns the most recent location ping per team for an event.
func (h *Handlers) ListPositions(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	out, err := h.buildPositions(r.Context(), eventID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"positions": out})
}

type trackPoint struct {
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	ServerTS string  `json:"server_ts"`
}

type trackDTO struct {
	TeamID    int64        `json:"team_id"`
	TeamName  string       `json:"team_name"`
	AvatarURL string       `json:"avatar_url,omitempty"`
	Points    []trackPoint `json:"points"`
}

// tracksTTL is how long buildTracks reuses a result. Every watcher's
// push and every race-control refresh asks for the same aggregate over
// the event's whole ping history, and a trail only moves in buckets of
// 10 s or more, so a few seconds' reuse costs nothing visible.
const tracksTTL = 5 * time.Second

type tracksCache struct {
	mu sync.Mutex
	m  map[int64]tracksEntry
	// building holds one lock per event being rebuilt, so watchers whose
	// cache ran out together wait for one rebuild instead of each running
	// the aggregate over the whole ping history.
	building map[int64]*sync.Mutex
}

type tracksEntry struct {
	at     time.Time
	tracks []trackDTO
}

// buildTracks is the cached helper used by ListTracks and the WS push
// path. Callers must not modify the returned slice: it is shared.
func (h *Handlers) buildTracks(ctx context.Context, eventID int64) ([]trackDTO, error) {
	c := &h.tracks
	now := time.Now()
	c.mu.Lock()
	if e, ok := c.m[eventID]; ok && now.Sub(e.at) < tracksTTL {
		c.mu.Unlock()
		return e.tracks, nil
	}
	if c.building == nil {
		c.building = map[int64]*sync.Mutex{}
	}
	lock := c.building[eventID]
	if lock == nil {
		lock = &sync.Mutex{}
		c.building[eventID] = lock
	}
	c.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	// Someone else may have rebuilt it while this waited.
	c.mu.Lock()
	if e, ok := c.m[eventID]; ok && time.Since(e.at) < tracksTTL {
		c.mu.Unlock()
		return e.tracks, nil
	}
	c.mu.Unlock()
	now = time.Now()
	tracks, err := h.queryTracks(ctx, eventID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.m == nil {
		c.m = map[int64]tracksEntry{}
	}
	for id, e := range c.m {
		if now.Sub(e.at) >= tracksTTL {
			delete(c.m, id)
			if id != eventID {
				delete(c.building, id)
			}
		}
	}
	c.m[eventID] = tracksEntry{at: now, tracks: tracks}
	c.mu.Unlock()
	return tracks, nil
}

// queryTracks aggregates an event's ping history into per-team tracks.
func (h *Handlers) queryTracks(ctx context.Context, eventID int64) ([]trackDTO, error) {
	// Smooth the per-team line by collapsing pings into time buckets and
	// emitting one centroid per bucket. Buckets are 10s wide, widened in
	// 10s steps once the event's ping history spans more than 20 minutes
	// so a track never exceeds ~120 points: this is resent on every push,
	// and full 10s resolution over a multi-hour event made it megabytes.
	// server_ts is a UTC 'YYYY-MM-DD HH24:MI:SS' string from now_text(),
	// and the bucket start goes back out in that format. The centroid is an
	// inverse-variance weighted mean: pings with smaller `accuracy` (in
	// metres) dominate, with a 1m floor to bound the weight and a 50m
	// fallback when accuracy is null.
	rows, err := h.DB.QueryContext(ctx, `
		WITH span AS (
		  SELECT GREATEST(10, CEIL(EXTRACT(EPOCH FROM
		           MAX(server_ts::timestamp) - MIN(server_ts::timestamp)) / 1200) * 10) AS w
		  FROM location_pings WHERE event_id = $1
		)
		SELECT p.team_id, t.name, (t.avatar_mime_type IS NOT NULL)::int AS has_avatar,
		       SUM(p.lat / power(GREATEST(COALESCE(p.accuracy, 50), 1), 2)) /
		         SUM(1.0 / power(GREATEST(COALESCE(p.accuracy, 50), 1), 2)) AS lat,
		       SUM(p.lng / power(GREATEST(COALESCE(p.accuracy, 50), 1), 2)) /
		         SUM(1.0 / power(GREATEST(COALESCE(p.accuracy, 50), 1), 2)) AS lng,
		       to_char(to_timestamp(FLOOR(EXTRACT(EPOCH FROM p.server_ts::timestamp) / s.w) * s.w)
		         AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS') AS server_ts
		FROM location_pings p JOIN teams t ON t.id = p.team_id CROSS JOIN span s
		WHERE p.event_id = $1
		GROUP BY p.team_id, t.name, (t.avatar_mime_type IS NOT NULL),
		         FLOOR(EXTRACT(EPOCH FROM p.server_ts::timestamp) / s.w), s.w
		ORDER BY p.team_id, server_ts
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byTeam := map[int64]*trackDTO{}
	order := []int64{}
	for rows.Next() {
		var (
			teamID    int64
			name      string
			hasAvatar int
			p         trackPoint
		)
		if err := rows.Scan(&teamID, &name, &hasAvatar, &p.Lat, &p.Lng, &p.ServerTS); err != nil {
			return nil, err
		}
		t, ok := byTeam[teamID]
		if !ok {
			t = &trackDTO{TeamID: teamID, TeamName: name}
			if hasAvatar != 0 {
				t.AvatarURL = teamAvatarURL(teamID)
			}
			byTeam[teamID] = t
			order = append(order, teamID)
		}
		t.Points = append(t.Points, p)
	}
	tracks := make([]trackDTO, 0, len(order))
	for _, id := range order {
		tracks = append(tracks, *byTeam[id])
	}
	return tracks, nil
}

// ListTracks returns the full ping history per team for an event.
func (h *Handlers) ListTracks(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	tracks, err := h.buildTracks(r.Context(), eventID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"tracks": tracks})
}

/* ---------------- URL helpers ---------------- */

func eventIDFrom(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "eventID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// parseISO8601 accepts the ISO-8601 variants browsers emit. The
// fallback formats without a timezone offset (`2006-01-02T15:04:05`,
// `2006-01-02T15:04`) used to be accepted too; time.Parse silently
// treats those as UTC, so a user in CET typing "14:00" stored 14:00
// UTC = 16:00 their local — confusing on the .ics export. We now
// require an explicit offset (Z or ±HH:MM) so the server's
// interpretation matches what the user wrote.
func parseISO8601(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("need RFC3339 with timezone (e.g. 2026-04-22T14:00:00Z or 2026-04-22T14:00:00+02:00)")
}
