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
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/mailer"
	"git.sr.ht/~klahr/zv/internal/activity"
	"git.sr.ht/~klahr/zv/internal/simulator"
	"git.sr.ht/~klahr/zv/internal/webhooks"
	"git.sr.ht/~klahr/zv/internal/ws"
	"github.com/go-chi/chi/v5"
)

// Handlers wires the admin endpoints to their dependencies.
type Handlers struct {
	DB  *sql.DB
	Lib *adventure.Library
	Hub *ws.Hub
	Sim *simulator.Simulator
	// SimulatorEnabled mirrors the server.Config flag — /whoami uses
	// it to tell the client whether to show the sidebar entry for the
	// race simulator. Disabled-by-default because a prod admin
	// nuking their event with a stray click would be catastrophic.
	SimulatorEnabled bool
	// Mailer is used by /api/admin/send-test-email and (future) signup
	// + forgot-password flows. Always non-nil thanks to the server
	// constructor's LogMailer fallback.
	Mailer mailer.Mailer
	// PublicURL is the externally-reachable origin used to build
	// links in outbound mail (change-email confirmation, etc.). May
	// be empty in dev — the admin handlers fall back to the request
	// Host the same way auth.Handlers does.
	PublicURL string
	// StripeSecretKey, when set, enables provider=stripe on
	// CreatePurchase. Empty leaves the simulated provider as the
	// only option, which is the right default for dev clones.
	StripeSecretKey string
	// StripeWebhookSecret authenticates POST /api/webhooks/stripe.
	// Required to enable Stripe — without it we'd be a payment
	// processor that accepts arbitrary "the customer paid" requests.
	StripeWebhookSecret string
	// SecureCookie is the operator's force-secure override, mirrored
	// from server.Config. Cookie-emitting paths in this package pass
	// it to auth.CookieSecure for parity with the auth/player handlers.
	SecureCookie bool
}

/* ---------------- Shared scoping helpers ---------------- */

// isOrgScoped reports whether the caller is attached to an organization
// (org_admin, org_member or player) rather than being a global admin.
func isOrgScoped(u auth.User) bool { return u.OrgID != nil }

// sameOrg returns true when the caller is either a global admin or is
// attached to orgID.
func sameOrg(u auth.User, orgID int64) bool {
	if u.Role == auth.RoleAdmin {
		return true
	}
	return u.OrgID != nil && *u.OrgID == orgID
}

// eventOrg loads the organization id for an event. Returns sql.ErrNoRows if the
// run does not exist.
func (h *Handlers) eventOrg(ctx context.Context, eventID int64) (int64, error) {
	var orgID int64
	err := h.DB.QueryRowContext(ctx, `SELECT org_id FROM events WHERE id = $1`, eventID).Scan(&orgID)
	return orgID, err
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
	if !sameOrg(me, orgID) {
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
	OrgID       *int64 `json:"org_id,omitempty"`   // nil = admin-owned global catalog
	MineToEdit  bool   `json:"mine_to_edit"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Published   bool   `json:"published"`
	Shared      bool   `json:"shared"` // visible to caller via a commissioned share
}

// ListAdventures returns adventures visible to the caller. Admins see
// everything (incl. drafts); org users see their own org's adventures
// + admin-owned entries that are either (a) published, (b) already
// held via an assignment, or (c) commissioned to the org via a share.
// The Store tab filters further to `!mine_to_edit && published`.
func (h *Handlers) ListAdventures(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())

	// `shared` is 1 when the caller reaches the row via an adventure_shares
	// grant — the UI uses that to flag "commissioned by admin" on the card.
	// For admins we return 0 uniformly (they reach every row unconditionally).
	query := `
		SELECT a.slug, a.title, a.description, a.author, a.org_id,
		       (SELECT COUNT(*) FROM adventure_pages p WHERE p.adventure_id = a.id) AS page_count,
		       (a.avatar_mime_type IS NOT NULL)::int AS has_avatar,
		       a.published,
	`
	var args []any
	if isAdmin(me) {
		query += ` 0 AS shared FROM adventures a`
	} else {
		query += `
		       (EXISTS (
		         SELECT 1 FROM adventure_shares s
		         WHERE s.adventure_slug = a.slug AND s.org_id = $1
		       ))::int AS shared
		FROM adventures a
		WHERE (
		  a.org_id = $2
		  OR (a.org_id IS NULL AND a.published = 1)
		  OR EXISTS (
		    SELECT 1 FROM adventure_assignments aa
		    WHERE aa.adventure_slug = a.slug AND aa.org_id = $3
		  )
		  OR EXISTS (
		    SELECT 1 FROM adventure_shares s
		    WHERE s.adventure_slug = a.slug AND s.org_id = $4
		  )
		)
		`
		args = append(args, *me.OrgID, *me.OrgID, *me.OrgID, *me.OrgID)
	}
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
		)
		if err := rows.Scan(&d.Slug, &d.Title, &d.Description, &d.Author, &orgID, &d.PageCount, &hasAvatar, &published, &shared); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if orgID.Valid {
			v := orgID.Int64
			d.OrgID = &v
		}
		d.MineToEdit = canEditOwned(me, orgID)
		d.Published = published != 0
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
	ID              int64  `json:"id"`
	Name            string `json:"name,omitempty"`
	AdventureSlug   string `json:"adventure_slug"`
	State           string `json:"state"`
	ScheduledAt     string `json:"scheduled_at,omitempty"`
	StartedAt       string `json:"started_at,omitempty"`
	OrgID           int64  `json:"org_id"`
	CreatedByUserID int64  `json:"created_by_user_id,omitempty"`
	AssignmentID    int64  `json:"assignment_id"`
	MaxTeams        int    `json:"max_teams"`
	LiveToken       string `json:"live_token"`
	ShowMap         bool   `json:"show_map"`
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
	AttestNoMinors bool `json:"attest_no_minors,omitempty"`
}

// CreateEvent schedules an event in one of two modes:
//
//  1. Assignment-backed: the caller holds an unused adventure_assignments
//     row (from the Store purchase flow). `assignment_id` picks it, and
//     the assignment is consumed on creation.
//  2. Share-backed: an admin has commissioned the adventure for the
//     caller's org via adventure_shares. `adventure_slug` names it, and
//     no assignment is consumed — runs are effectively unlimited.
//
// Share-backed also covers the "org runs an adventure they author" case:
// if the slug resolves to an adventure owned by the caller's org, run
// creation is allowed without a share row.
func (h *Handlers) CreateEvent(w http.ResponseWriter, r *http.Request) {
	var req createEventRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if !isOrgScoped(me) {
		httpx.Error(w, http.StatusForbidden, "admins don't schedule runs; do it from within an organization")
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
		maxTeams      int
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
			SELECT aa.adventure_slug, aa.org_id, aa.max_teams, ar.id
			FROM adventure_assignments aa
			LEFT JOIN events ar ON ar.assignment_id = aa.id
			WHERE aa.id = $1
		`, req.AssignmentID).Scan(&advSlug, &orgID, &maxTeams, &used)
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
		// Confirm the caller has share or ownership access to this slug.
		var ok bool
		if err := h.DB.QueryRowContext(r.Context(), `
			SELECT EXISTS (
			  SELECT 1 FROM adventures a
			  WHERE a.slug = $1 AND (
			    a.org_id = $2
			    OR EXISTS (
			      SELECT 1 FROM adventure_shares s
			      WHERE s.adventure_slug = a.slug AND s.org_id = $3
			    )
			  )
			)
		`, slug, *me.OrgID, *me.OrgID).Scan(&ok); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if !ok {
			httpx.Error(w, http.StatusForbidden, "no access to that adventure")
			return
		}
	} else {
		httpx.Error(w, http.StatusBadRequest, "assignment_id or adventure_slug required")
		return
	}

	if h.Lib.Get(slug) == nil {
		httpx.Error(w, http.StatusInternalServerError, "adventure not loaded: "+slug)
		return
	}

	if !req.AttestNoMinors {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{
			"error": "you must confirm there are no under-13 players (or that you have parental consent) before creating an event",
			"code":  "minor_attestation_required",
		})
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
	err = h.DB.QueryRowContext(r.Context(), `
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, scheduled_at, assignment_id, live_token, name, attested_no_minors_at)
		VALUES ($1, $2, $3, 'scheduled', $4, $5, $6, $7, now_text())
		RETURNING id
	`, slug, *me.OrgID, me.ID, schedAt, assignmentArg, liveToken, strings.TrimSpace(req.Name)).Scan(&id)
	if err != nil {
		httpx.Error(w, http.StatusConflict, "could not create run: "+err.Error())
		return
	}
	activity.Record(r.Context(), h.DB, id, nil, activity.UserActor(me.ID), activity.EventCreated,
		map[string]any{"adventure_slug": slug, "max_teams": maxTeams})
	// event.scheduled fires for every fresh event — even ones with no
	// scheduled_at, since the canonical state is still "scheduled" on
	// creation. Customers wiring calendar / kickoff comms branch on
	// the scheduled_at field being non-empty.
	advTitle := ""
	if adv := h.Lib.Get(slug); adv != nil {
		advTitle = adv.Meta.Title
	}
	webhooks.TryPublish(r.Context(), h.DB, *me.OrgID, webhooks.KindEventScheduled, map[string]any{
		"event_id":          id,
		"name":            strings.TrimSpace(req.Name),
		"adventure_slug":  slug,
		"adventure_title": advTitle,
		"scheduled_at":    req.ScheduledAt,
		"max_teams":       maxTeams,
	})
	h.Hub.Publish(ws.EventTopic(id), "changed", nil)
	httpx.JSON(w, http.StatusCreated, eventDTO{
		ID: id, AdventureSlug: slug, State: "scheduled",
		ScheduledAt: req.ScheduledAt, OrgID: *me.OrgID, CreatedByUserID: me.ID,
		AssignmentID: req.AssignmentID, MaxTeams: maxTeams,
		LiveToken: liveToken,
	})
}

// newLiveToken mints the opaque /live/{token} URL segment. 128 bits of
// entropy rendered URL-safe base64, enough to make guessing hopeless and
// short enough to fit comfortably in an SMS.
func newLiveToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// ListEvents returns events visible to the caller: admins see everything,
// org users see their own org.
func (h *Handlers) ListEvents(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	query := `
		SELECT ar.id, ar.name, ar.adventure_slug, ar.state,
		       COALESCE(ar.scheduled_at, ''), COALESCE(ar.started_at, ''),
		       ar.org_id, COALESCE(ar.created_by_user_id, 0),
		       COALESCE(ar.assignment_id, 0), COALESCE(aa.max_teams, 0),
		       ar.live_token, ar.show_map
		FROM events ar
		LEFT JOIN adventure_assignments aa ON aa.id = ar.assignment_id
	`
	var args []any
	if me.Role != auth.RoleAdmin {
		query += ` WHERE ar.org_id = $1`
		args = append(args, *me.OrgID)
	}
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
			d       eventDTO
			showMap int
		)
		if err := rows.Scan(&d.ID, &d.Name, &d.AdventureSlug, &d.State, &d.ScheduledAt, &d.StartedAt,
			&d.OrgID, &d.CreatedByUserID, &d.AssignmentID, &d.MaxTeams,
			&d.LiveToken, &showMap); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		d.ShowMap = showMap != 0
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
	var state string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT state FROM events WHERE id = $1`, eventID).Scan(&state); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	var want, update string
	switch action {
	case "start":
		want, update = "scheduled", `UPDATE events SET state='running', started_at=now_text() WHERE id=$1`
	case "pause":
		want, update = "running", `UPDATE events SET state='paused', paused_at=now_text() WHERE id=$1`
	case "resume":
		want, update = "paused", `UPDATE events SET state='running' WHERE id=$1`
	default:
		httpx.Error(w, http.StatusBadRequest, "unknown action")
		return
	}
	if state != want {
		httpx.Error(w, http.StatusConflict, "cannot "+action+" an event in state "+state)
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), update, eventID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	me, _ := auth.FromContext(r.Context())
	kind := map[string]string{"start": activity.EventStarted, "pause": activity.EventPaused, "resume": activity.EventResumed}[action]
	activity.Record(r.Context(), h.DB, eventID, nil, activity.UserActor(me.ID), kind, nil)
	// Emit the matching webhook so customers can wire kickoff
	// notifications, lobby-TV banners, etc. Only "start" has a v1
	// kind today; pause / resume are intra-event lifecycle that
	// customers haven't asked for.
	if action == "start" {
		var (
			advSlug string
			runName string
		)
		_ = h.DB.QueryRowContext(r.Context(),
			`SELECT adventure_slug, name FROM events WHERE id = $1`, eventID).Scan(&advSlug, &runName)
		advTitle := ""
		if adv := h.Lib.Get(advSlug); adv != nil {
			advTitle = adv.Meta.Title
		}
		webhooks.TryPublish(r.Context(), h.DB, orgID, webhooks.KindEventStarted, map[string]any{
			"event_id":          eventID,
			"name":            runName,
			"adventure_slug":  advSlug,
			"adventure_title": advTitle,
			"manual":          true,
		})
	}
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

// StartEvent moves a scheduled event into the running state.
func (h *Handlers) StartEvent(w http.ResponseWriter, r *http.Request) { h.transitionEvent(w, r, "start") }

// PauseEvent pauses a running run.
func (h *Handlers) PauseEvent(w http.ResponseWriter, r *http.Request) { h.transitionEvent(w, r, "pause") }

// ResumeEvent resumes a paused run.
func (h *Handlers) ResumeEvent(w http.ResponseWriter, r *http.Request) { h.transitionEvent(w, r, "resume") }

type updateEventRequest struct {
	Name        *string `json:"name,omitempty"`
	ScheduledAt *string `json:"scheduled_at,omitempty"`
	ShowMap     *bool   `json:"show_map,omitempty"`
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
	if req.ScheduledAt == nil && req.ShowMap == nil && req.Name == nil {
		httpx.Error(w, http.StatusBadRequest, "nothing to update")
		return
	}
	if req.Name != nil {
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE events SET name = $1 WHERE id = $2`, strings.TrimSpace(*req.Name), eventID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
			return
		}
	}

	if req.ScheduledAt != nil {
		var state string
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT state FROM events WHERE id = $1`, eventID).Scan(&state); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if state != "scheduled" {
			httpx.Error(w, http.StatusConflict, "can only reschedule an event while it is 'scheduled'")
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
			`UPDATE events SET scheduled_at = $1 WHERE id = $2`, schedAt, eventID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "update failed")
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
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusNoContent, nil)
}

/* ---------------- Teams (create is here; edit/delete in teams.go) ---------------- */

type createTeamRequest struct {
	Name        string   `json:"name"`
	MemberNames []string `json:"member_names"`
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
	ID      int64         `json:"id"`
	Name    string        `json:"name"`
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
	var req createTeamRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "team name required")
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

	// Enforce the assignment's team cap.
	var (
		maxTeams   int
		teamsCount int
	)
	if err := tx.QueryRowContext(r.Context(), `
		SELECT COALESCE(aa.max_teams, 0),
		       (SELECT COUNT(*) FROM teams t WHERE t.event_id = $1)
		FROM events ar
		LEFT JOIN adventure_assignments aa ON aa.id = ar.assignment_id
		WHERE ar.id = $2
	`, eventID, eventID).Scan(&maxTeams, &teamsCount); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "cap lookup failed")
		return
	}
	if maxTeams > 0 && teamsCount >= maxTeams {
		httpx.Error(w, http.StatusConflict,
			"this event's assignment allows up to "+strconv.Itoa(maxTeams)+" team(s); buy another assignment for more")
		return
	}

	teamTok, err := auth.NewSlotToken()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "token gen failed")
		return
	}
	var teamID int64
	if err := tx.QueryRowContext(r.Context(),
		`INSERT INTO teams(event_id, name, token) VALUES ($1, $2, $3) RETURNING id`,
		eventID, req.Name, teamTok).Scan(&teamID); err != nil {
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
			httpx.Error(w, http.StatusConflict, "could not add "+name+": "+err.Error())
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
		"event_id":       eventID,
		"team_id":      teamID,
		"team_name":    req.Name,
		"member_count": len(members),
	})
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	httpx.JSON(w, http.StatusCreated, teamDTO{ID: teamID, Name: req.Name, Members: members})
}

/* ---------------- Positions & Tracks ---------------- */

type positionDTO struct {
	TeamID    int64   `json:"team_id"`
	TeamName  string  `json:"team_name"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	ServerTS  string  `json:"server_ts"`
	PageIndex int     `json:"page_index"`
}

// buildPositions is the query-only helper used by both the HTTP ListPositions
// handler and the WS push path. Caller does authorisation.
func (h *Handlers) buildPositions(ctx context.Context, eventID int64) ([]positionDTO, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name, t.current_page_index,
		       COALESCE(p.lat, 0), COALESCE(p.lng, 0), COALESCE(p.server_ts, '')
		FROM teams t
		LEFT JOIN (
			SELECT team_id, lat, lng, server_ts
			FROM location_pings
			WHERE id IN (
				SELECT MAX(id) FROM location_pings WHERE event_id = $1 GROUP BY team_id
			)
		) p ON p.team_id = t.id
		WHERE t.event_id = $2
		ORDER BY t.name
	`, eventID, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []positionDTO{}
	for rows.Next() {
		var d positionDTO
		if err := rows.Scan(&d.TeamID, &d.TeamName, &d.PageIndex, &d.Lat, &d.Lng, &d.ServerTS); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
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

// buildTracks is the query-only helper used by ListTracks and the WS push
// path.
func (h *Handlers) buildTracks(ctx context.Context, eventID int64) ([]trackDTO, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT p.team_id, t.name, (t.avatar_mime_type IS NOT NULL)::int AS has_avatar,
		       p.lat, p.lng, p.server_ts
		FROM location_pings p JOIN teams t ON t.id = p.team_id
		WHERE p.event_id = $1
		ORDER BY p.team_id, p.server_ts
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

// parseISO8601 accepts the ISO-8601 variants browsers emit.
func parseISO8601(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("need RFC3339 (e.g. 2026-04-22T14:00:00Z)")
}
