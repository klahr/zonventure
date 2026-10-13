package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/orgslug"
)

/* ---------------- DTOs ---------------- */

type assignmentDTO struct {
	ID             int64  `json:"id"`
	AdventureSlug  string `json:"adventure_slug"`
	AdventureTitle string `json:"adventure_title,omitempty"`
	MaxPlayers     int    `json:"max_players"`
	CreatedAt      string `json:"created_at"`
	EventID        *int64 `json:"event_id,omitempty"`
	// EventCancelled says that event was cancelled: it wasn't held.
	EventCancelled bool `json:"event_cancelled,omitempty"`
}

type orgMemberDTO struct {
	ID            int64  `json:"id"`
	Username      string `json:"username"`
	Role          string `json:"role"` // org_admin | org_member
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified"`
	// TOTPEnabled: the member signs in with two-factor authentication.
	TOTPEnabled bool `json:"totp_enabled"`
	// ActiveEventsOwned is the count of non-terminal events
	// (scheduled / running / paused) this member created in this org.
	// Surfaced so the remove-member UI can warn that destroying the
	// member orphans their owned events. 0 means "safe to remove".
	ActiveEventsOwned int `json:"active_events_owned"`
}

// pendingInvitationDTO is an invitation to join the organisation that
// is not yet answered. It names the account by the address it was
// invited at, never its username: the org's admins typed that address.
type pendingInvitationDTO struct {
	UserID    int64  `json:"user_id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
	// Declined: the person said no. Inviting them again asks anew.
	Declined bool `json:"declined,omitempty"`
}

type organizationDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Slug powers the per-org store URL (/o/{slug}/store). Auto-derived
	// from name at creation time, editable by org_admin afterwards.
	// See internal/orgslug.
	Slug        string          `json:"slug,omitempty"`
	CreatedAt   string          `json:"created_at"`
	Members     []orgMemberDTO  `json:"members"`
	Assignments []assignmentDTO `json:"assignments"`
	// OwnAdventures are the org's published adventures, which the
	// operator may grant an event pass for besides the catalog's.
	OwnAdventures   []ownAdventureDTO `json:"own_adventures"`
	PlayersTotal    int               `json:"players_total"`
	EventsTotal     int               `json:"events_total"`
	EventsRunning   int               `json:"events_running"`
	AdventuresTotal int               `json:"adventures_total"`
	// HasLogo is true when the org has a brand logo uploaded. The
	// frontend uses it to decide whether to render the preview vs
	// an upload placeholder, without speculatively GETting the
	// asset. Bytes themselves come from /org-logo/{id} when needed.
	HasLogo bool `json:"has_logo"`
	// Trusted = the org is allowed to buy player slots on invoice
	// instead of paying upfront via Stripe. Toggled by global admins
	// only. Surfaced here so the admin org-detail panel can show
	// the current state and a toggle button.
	Trusted bool `json:"trusted"`
	// IsPublisher marks the org that authors the platform catalog.
	IsPublisher bool `json:"is_publisher"`
	// PartyAdventures = the org's public party adventure creator is
	// open (quiz_trail_enabled). Toggled by global admins only.
	PartyAdventures bool `json:"party_adventures"`
	// VerifiedCreator = the org's listings skip review and the store
	// shows a badge (listing_review.go). Toggled by global admins only.
	VerifiedCreator bool `json:"verified_creator"`
	// RequireTOTP = every member must use two-factor authentication.
	// Set by the org's admins (SetOrgRequireTOTP).
	RequireTOTP bool `json:"require_totp"`
	// Invitations are the pending ones, for whoever manages members.
	Invitations []pendingInvitationDTO `json:"invitations,omitempty"`
}

/* ---------------- Admin-only: organizations CRUD ---------------- */

// ListOrganizations returns every organization with its members, assignments
// and activity counts. Admin-only.
func (h *Handlers) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT o.id, o.name, o.slug, o.created_at,
		       (SELECT COUNT(*) FROM player_slots ps
		         JOIN teams t ON t.id = ps.team_id
		         JOIN events ar ON ar.id = t.event_id
		        WHERE ar.org_id = o.id AND ar.state <> 'cancelled') AS players_total,
		       (SELECT COUNT(*) FROM events ar WHERE ar.org_id = o.id),
		       (SELECT COUNT(*) FROM events ar WHERE ar.org_id = o.id AND ar.state = 'running'),
		       (SELECT COUNT(*) FROM adventures a WHERE a.org_id = o.id),
		       (o.logo_content IS NOT NULL AND length(o.logo_content) > 0) AS has_logo,
		       (o.trusted <> 0) AS trusted,
		       o.is_publisher, o.quiz_trail_enabled, o.verified_creator, o.require_totp
		FROM organizations o
		ORDER BY LOWER(o.name)
	`)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []organizationDTO{}
	for rows.Next() {
		o := organizationDTO{Members: []orgMemberDTO{}, Assignments: []assignmentDTO{}}
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt, &o.PlayersTotal, &o.EventsTotal, &o.EventsRunning, &o.AdventuresTotal, &o.HasLogo, &o.Trusted, &o.IsPublisher, &o.PartyAdventures, &o.VerifiedCreator, &o.RequireTOTP); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		out = append(out, o)
	}
	for i := range out {
		if err := h.loadOrgMembers(r.Context(), &out[i]); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "members failed")
			return
		}
		if err := h.loadOrgAssignments(r.Context(), &out[i]); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "assignments failed")
			return
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"organizations": out})
}

// GetOrganization returns a single organization with members, assignments
// and counts. Callable by a global admin, or by a user who is in that org.
// An event host (org_member) gets no members or invitations: managing
// them is the org admins', and the console hides Members from them.
func (h *Handlers) GetOrganization(w http.ResponseWriter, r *http.Request) {
	id, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if !readsAsAdmin(me) {
		if me.OrgID == nil || *me.OrgID != id {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
	}
	var o organizationDTO
	o.Members = []orgMemberDTO{}
	o.Assignments = []assignmentDTO{}
	o.OwnAdventures = []ownAdventureDTO{}
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT o.id, o.name, o.slug, o.created_at,
		       (SELECT COUNT(*) FROM player_slots ps
		         JOIN teams t ON t.id = ps.team_id
		         JOIN events ar ON ar.id = t.event_id
		        WHERE ar.org_id = o.id AND ar.state <> 'cancelled') AS players_total,
		       (SELECT COUNT(*) FROM events ar WHERE ar.org_id = o.id),
		       (SELECT COUNT(*) FROM events ar WHERE ar.org_id = o.id AND ar.state = 'running'),
		       (o.logo_content IS NOT NULL AND length(o.logo_content) > 0) AS has_logo,
		       (o.trusted <> 0) AS trusted,
		       o.is_publisher, o.quiz_trail_enabled, o.verified_creator, o.require_totp
		FROM organizations o WHERE o.id = $1
	`, id).Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt, &o.PlayersTotal, &o.EventsTotal, &o.EventsRunning, &o.HasLogo, &o.Trusted, &o.IsPublisher, &o.PartyAdventures, &o.VerifiedCreator, &o.RequireTOTP)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "organization not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if me.Role != auth.RoleOrgMember && me.Role != auth.RoleOrgWriter {
		if err := h.loadOrgMembers(r.Context(), &o); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "members failed")
			return
		}
		if err := h.loadOrgInvitations(r.Context(), &o); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "invitations failed")
			return
		}
	}
	if err := h.loadOrgAssignments(r.Context(), &o); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "assignments failed")
		return
	}
	if err := h.loadOwnAdventures(r.Context(), &o); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "adventures failed")
		return
	}
	httpx.JSON(w, http.StatusOK, o)
}

type createOrgRequest struct {
	Name string `json:"name"`
}

// CreateOwnOrg is the self-service "I want my own org" affordance.
// Available to any authenticated non-admin: a lobby user (no
// memberships) creates their first org, or an existing org user adds
// a second org they're an admin of. The caller is automatically
// inserted as the new org's org_admin in the same transaction so
// half-state can't strand them outside their own org.
//
// Admins are blocked here — they don't carry memberships at all and
// have CreateOrganization as the operator-side path.
func (h *Handlers) CreateOwnOrg(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.IsAdmin {
		httpx.Error(w, http.StatusBadRequest, "admins use the operator-side endpoint to create organizations")
		return
	}
	var req createOrgRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "name required")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "begin tx failed")
		return
	}
	defer tx.Rollback() //nolint:errcheck
	slug, err := orgslug.Reserve(r.Context(), tx, req.Name)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "slug reservation failed")
		return
	}
	var id int64
	if err := tx.QueryRowContext(r.Context(),
		`INSERT INTO organizations(name, slug) VALUES ($1, $2) RETURNING id`, req.Name, slug).Scan(&id); err != nil {
		httpx.Error(w, http.StatusConflict, "name already used")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')
	`, me.ID, id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "membership insert failed")
		return
	}
	// Repoint the caller's current session at the new org so the next
	// page load drops them into it rather than back into the lobby
	// or whatever org they were on before.
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE sessions SET active_org_id = $1 WHERE user_id = $2
	`, id, me.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "session pin failed")
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	httpx.JSON(w, http.StatusCreated, organizationDTO{
		ID: id, Name: req.Name, Slug: slug, Members: []orgMemberDTO{}, Assignments: []assignmentDTO{},
	})
}

// CreateOrganization creates a new organization. Admin-only.
func (h *Handlers) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	var req createOrgRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "name required")
		return
	}
	slug, err := orgslug.Reserve(r.Context(), h.DB, req.Name)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "slug reservation failed")
		return
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(),
		`INSERT INTO organizations(name, slug) VALUES ($1, $2) RETURNING id`, req.Name, slug).Scan(&id); err != nil {
		httpx.Error(w, http.StatusConflict, "name already used")
		return
	}
	noteTarget(r, req.Name)
	httpx.JSON(w, http.StatusCreated, organizationDTO{
		ID: id, Name: req.Name, Slug: slug, Members: []orgMemberDTO{}, Assignments: []assignmentDTO{},
	})
}

type updateOrgRequest struct {
	Name *string `json:"name,omitempty"`
}

// orgSlugDTO is the GET + PUT body for /api/admin/orgs/me/slug.
// Returns the bare slug plus a fully-resolved store URL the
// frontend can drop into a Copy button without re-deriving it.
type orgSlugDTO struct {
	Slug     string `json:"slug"`
	StoreURL string `json:"store_url"`
}

// GetMyOrgSlug returns the slug of the caller's active org. Reads
// the active org from the session (auth.FromContext) so it works
// for any role attached to one — an org_admin reading their slug
// to display, or a future read-only path for org_member that just
// shows the URL without an edit affordance.
func (h *Handlers) GetMyOrgSlug(w http.ResponseWriter, r *http.Request) {
	me, ok := auth.FromContext(r.Context())
	if !ok || me.OrgID == nil || *me.OrgID == 0 {
		httpx.Error(w, http.StatusForbidden, "not in an organization")
		return
	}
	orgID := *me.OrgID
	var slug string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT slug FROM organizations WHERE id = $1`, orgID).Scan(&slug); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "organization not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	httpx.JSON(w, http.StatusOK, orgSlugDTO{
		Slug:     slug,
		StoreURL: storeURLFromSlug(h.PublicURL, slug),
	})
}

// SetMyOrgSlug rewrites the caller's active-org slug. RoleOrgAdmin
// only — an org_member can read the URL but not change it (a
// rename breaks every printed flyer / email link the company sent
// out, so it's an "owner" decision).
//
// Validation runs through orgslug.Validate, so format + length +
// reserved-word checks live in one place. Slug-already-taken
// returns 409 so the caller can show "pick another" inline.
func (h *Handlers) SetMyOrgSlug(w http.ResponseWriter, r *http.Request) {
	me, ok := auth.FromContext(r.Context())
	if !ok || me.OrgID == nil || *me.OrgID == 0 {
		httpx.Error(w, http.StatusForbidden, "not in an organization")
		return
	}
	orgID := *me.OrgID
	if me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "org admins only")
		return
	}
	var req orgSlugDTO
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if err := orgslug.Validate(slug); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE organizations SET slug = $1 WHERE id = $2`, slug, orgID)
	if err != nil {
		// Postgres reports unique violations on (slug). Surface as
		// 409 so the frontend's "pick another" UI lights up rather
		// than a generic 500.
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") ||
			strings.Contains(strings.ToLower(err.Error()), "unique") {
			httpx.Error(w, http.StatusConflict, "that slug is already taken")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "organization not found")
		return
	}
	logx.Info("admin.org.slug.updated", "org_id", orgID, "slug", slug, "by", me.Username)
	httpx.JSON(w, http.StatusOK, orgSlugDTO{
		Slug:     slug,
		StoreURL: storeURLFromSlug(h.PublicURL, slug),
	})
}

// storeURLFromSlug returns the absolute URL of the organisation's page,
// which lists its adventures for sale. Falls back to a relative path
// when the operator hasn't set PublicURL — the Copy button will still
// produce something usable, just origin-relative rather than
// fully-qualified.
func storeURLFromSlug(publicURL, slug string) string {
	base := strings.TrimRight(publicURL, "/")
	if base == "" {
		return "/o/" + slug
	}
	return base + "/o/" + slug
}

// UpdateOrganization renames an organization. Admin-only.
func (h *Handlers) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	id, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	var req updateOrgRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil {
		httpx.Error(w, http.StatusBadRequest, "name required")
		return
	}
	name, err := validateName(*req.Name, MaxNameLen)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE organizations SET name = $1 WHERE id = $2`, name, id); err != nil {
		httpx.Error(w, http.StatusConflict, "name already used")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// SetOrgTrusted toggles the trusted flag. Trusted orgs see an "Add to
// invoice" option in the buy-player-slots modal; the off-platform
// invoice generation lives outside the platform. Admin-only.
func (h *Handlers) SetOrgTrusted(w http.ResponseWriter, r *http.Request) {
	id, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	var req struct {
		Trusted bool `json:"trusted"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	v := 0
	if req.Trusted {
		v = 1
	}
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE organizations SET trusted = $1 WHERE id = $2`, v, id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "organization not found")
		return
	}
	noteAction(r, onOff(req.Trusted))
	httpx.JSON(w, http.StatusOK, map[string]any{"trusted": req.Trusted})
}

// SetOrgPartyAdventures opens or closes the org's public party adventure
// creator (/app/party/trail/start/{slug}). Admin-only.
func (h *Handlers) SetOrgPartyAdventures(w http.ResponseWriter, r *http.Request) {
	id, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE organizations SET quiz_trail_enabled = $1 WHERE id = $2`, req.Enabled, id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "organization not found")
		return
	}
	noteAction(r, onOff(req.Enabled))
	httpx.JSON(w, http.StatusOK, map[string]any{"enabled": req.Enabled})
}

// DeleteOrganization deletes an organization (and everything it cascades to:
// users in it, runs, teams, assignments). Admin-only and password-gated.
func (h *Handlers) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	if !h.requirePasswordConfirm(w, r) {
		return
	}
	id, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	// Deleting the publisher would cascade away the whole catalog —
	// and every other org's events and passes naming it by slug.
	if pid, isPub, _ := publisherOrgID(r.Context(), h.DB); isPub && pid == id {
		httpx.Error(w, http.StatusConflict, "the catalog publisher cannot be deleted while it publishes the catalog")
		return
	}
	res, err := h.DB.ExecContext(r.Context(), `DELETE FROM organizations WHERE id = $1`, id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "organization not found")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

/* ---------------- Org members (admin OR org_admin in the same org) ---------------- */

type createOrgMemberRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`    // required from an org admin; a global admin may name a username instead
	Password string `json:"password"` // a global admin's alternative to inviting a new user by email
	Role     string `json:"role"`     // org_admin | org_member | org_writer
}

// orgRole reports whether role is one an organisation's member may hold.
func orgRole(role string) bool {
	return role == auth.RoleOrgAdmin || role == auth.RoleOrgMember || role == auth.RoleOrgWriter
}

// CreateOrgMember adds a user to this organisation. Callable by global
// admin, or by an org_admin of the same organization. Two flows behind
// one endpoint:
//
//   - The email address is an existing account's → invite them: an
//     org_invitations row the user accepts or declines from their
//     console (AcceptOrgInvitation), plus an email. No membership
//     until they accept. The username and password in the request are
//     ignored — we don't want admins accidentally resetting someone's
//     credentials by clicking "Add to org".
//   - Otherwise → create the user with the username and no password,
//     and invite them the same way: the email's link lets them choose
//     their password, then they accept from their console. A global
//     admin may instead give an initial password, which creates the
//     user AND the org_memberships row at once.
//
// An existing account is found only by its exact email address, and
// the answer is the same 202 either way, so an org admin can't probe
// which addresses have accounts. A global admin, who sees every
// account anyway, may also name an existing account by username (the
// Organizations page's typeahead). Inviting an admin account does
// nothing; an existing member is a 409.
func (h *Handlers) CreateOrgMember(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleAdmin {
		if me.Role != auth.RoleOrgAdmin || me.OrgID == nil || *me.OrgID != orgID {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
	}
	var req createOrgMemberRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	if req.Username == "" {
		httpx.Error(w, http.StatusBadRequest, "username required")
		return
	}
	if req.Email != "" && !strings.Contains(req.Email, "@") {
		httpx.Error(w, http.StatusBadRequest, "email must contain @")
		return
	}
	// A new user is invited by email to set their own password; only a
	// global admin may set one for them instead.
	invite := me.Role != auth.RoleAdmin || req.Password == ""
	if me.Role != auth.RoleAdmin && req.Email == "" {
		httpx.Error(w, http.StatusBadRequest, "email required")
		return
	}
	if !orgRole(req.Role) {
		httpx.Error(w, http.StatusBadRequest, "role must be 'org_admin', 'org_member' or 'org_writer'")
		return
	}

	// Resolve the user — nil row → new-user flow, populated row →
	// invite-existing-user flow.
	var (
		existingID      int64
		existingIsAdmin bool
		lookupErr       = sql.ErrNoRows
	)
	if req.Email != "" {
		lookupErr = h.DB.QueryRowContext(r.Context(),
			`SELECT id, is_admin FROM users WHERE email = $1`, req.Email,
		).Scan(&existingID, &existingIsAdmin)
	} else if me.Role == auth.RoleAdmin {
		lookupErr = h.DB.QueryRowContext(r.Context(),
			`SELECT id, is_admin FROM users WHERE username = $1`, req.Username,
		).Scan(&existingID, &existingIsAdmin)
	}
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if lookupErr != nil && invite && req.Email == "" {
		httpx.Error(w, http.StatusBadRequest, "email or password required for new users")
		return
	}
	if lookupErr != nil && !invite {
		if err := auth.ValidatePassword(req.Password); err != nil {
			if errors.Is(err, auth.ErrPasswordTooCommon) {
				httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error(), "code": "password_too_common"})
				return
			}
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if lookupErr == nil {
		if existingIsAdmin {
			// Admins are global and have no memberships.
			httpx.JSON(w, http.StatusAccepted, map[string]string{"status": "sent"})
			return
		}
		// An existing account joins only when its owner says yes:
		// pulling a login in without their say-so would hand this
		// org's admin a foothold on it (and, with the credential-reset
		// endpoints, a takeover). So record an invitation they accept
		// or decline from their console.
		var member bool
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM org_memberships WHERE user_id = $1 AND org_id = $2)`,
			existingID, orgID).Scan(&member); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if member {
			httpx.Error(w, http.StatusConflict, "user is already a member of this organization")
			return
		}
		if _, err := h.DB.ExecContext(r.Context(), `
			INSERT INTO org_invitations(user_id, org_id, role, invited_by_user_id, email) VALUES ($1, $2, $3, $4, NULLIF($5, ''))
			ON CONFLICT (user_id, org_id) DO UPDATE SET role = excluded.role, invited_by_user_id = excluded.invited_by_user_id, email = excluded.email,
			    declined_at = NULL, created_at = CASE WHEN org_invitations.declined_at IS NULL THEN org_invitations.created_at ELSE now_text() END
		`, existingID, orgID, req.Role, me.ID, req.Email); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "invitation insert failed")
			return
		}
		h.sendOrgInvitation(r, existingID, orgID)
		httpx.JSON(w, http.StatusAccepted, map[string]string{"status": "sent"})
		return
	}

	if invite {
		h.inviteNewOrgMember(w, r, orgID, req)
		return
	}

	// New-user flow with a password set by a global admin.
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "hash failed")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "begin tx failed")
		return
	}
	defer tx.Rollback() //nolint:errcheck
	var id int64
	// Email is optional. When supplied it stays unverified and the
	// user gets the verification mail at once, so a mistyped address
	// is caught before password resets go to it. Without SMTP the
	// mail goes to the log, where the operator finds the link.
	var emailParam any
	if req.Email != "" {
		emailParam = req.Email
	}
	if err := tx.QueryRowContext(r.Context(), `
		INSERT INTO users(username, email, email_verified, password_hash, password_set_by_admin, created_by_user_id, created_by_org_id)
		VALUES ($1, $2, 0, $3, 1, $4, $5)
		RETURNING id
	`, req.Username, emailParam, hash, me.ID, orgID).Scan(&id); err != nil {
		httpx.Error(w, http.StatusConflict, "username or email not available")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, $3)
	`, id, orgID, req.Role); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "membership insert failed")
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	if req.Email != "" {
		h.sendVerification(r, id, req.Email)
	}
	httpx.JSON(w, http.StatusAccepted, map[string]string{"status": "sent"})
}

// DeleteOrgMember removes an org user. Global admin, or an org_admin of the
// same org. The caller can't delete themselves.
// DeleteOrgMember removes a user from this organisation. Two flows:
//
//   - User is in this org and at least one other → drop just the
//     org_memberships row. Keep the user account, their other
//     memberships, sessions, etc. Sessions whose active_org_id was
//     the removed org get reset to NULL so the user picks again on
//     next page load.
//   - User is in this org only → hard-delete the user account
//     (legacy behaviour). Cascades through FKs as before.
func (h *Handlers) DeleteOrgMember(w http.ResponseWriter, r *http.Request) {
	if !h.requirePasswordConfirm(w, r) {
		return
	}
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	userID, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.ID == userID {
		httpx.Error(w, http.StatusConflict, "cannot delete yourself")
		return
	}
	if me.Role != auth.RoleAdmin {
		if me.Role != auth.RoleOrgAdmin || me.OrgID == nil || *me.OrgID != orgID {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
	}
	// Confirm the user exists, isn't an admin, and actually has a
	// membership row in this org.
	var isAdmin bool
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT is_admin FROM users WHERE id = $1`, userID).Scan(&isAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if isAdmin {
		httpx.Error(w, http.StatusBadRequest, "not an org member")
		return
	}
	var memberOf int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM org_memberships WHERE user_id = $1`, userID,
	).Scan(&memberOf); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "membership count failed")
		return
	}
	var hasHere int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT 1 FROM org_memberships WHERE user_id = $1 AND org_id = $2`, userID, orgID,
	).Scan(&hasHere); errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "user not in this organization")
		return
	} else if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	// memberOf is read above for legacy reasons; org-less users are
	// now a first-class concept (RoleLobby), so dropping the last
	// membership is non-destructive — the user stays around and can
	// be re-invited or create a new org from their dashboard. We no
	// longer hard-delete on the last membership.
	_ = memberOf

	// Drop the membership. Wrap in a tx so the drop and the
	// active-session reset commit together.
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "begin tx failed")
		return
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM org_memberships WHERE user_id = $1 AND org_id = $2`, userID, orgID,
	); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "membership delete failed")
		return
	}
	// Force any session whose active_org was the removed org to
	// re-pick. NULL active_org_id triggers the picker on next page
	// load.
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE sessions SET active_org_id = NULL WHERE user_id = $1 AND active_org_id = $2
	`, userID, orgID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "session reset failed")
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// WithdrawOrgInvitation drops a pending invitation to join this
// organisation, so accepting it no longer works. Global admin, or an
// org_admin of the same org.
func (h *Handlers) WithdrawOrgInvitation(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	userID, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleAdmin {
		if me.Role != auth.RoleOrgAdmin || me.OrgID == nil || *me.OrgID != orgID {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
	}
	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM org_invitations WHERE user_id = $1 AND org_id = $2`, userID, orgID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "withdraw failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "no such invitation")
		return
	}
	logx.Info("admin.org_invitation.withdrawn", "user_id", userID, "org_id", orgID, "by", me.ID)
	w.WriteHeader(http.StatusNoContent)
}

type setOrgMemberRoleRequest struct {
	Role string `json:"role"` // org_admin | org_member | org_writer
}

// SetOrgMemberRole changes a member's role in this organisation in
// place (Settings → Members). Global admin, or an org_admin of the
// same org, with X-Confirm-Password. Nobody changes their own role
// (TransferOwnership is how an admin steps down), and the last admin
// can't be demoted, so the org is never left without one.
func (h *Handlers) SetOrgMemberRole(w http.ResponseWriter, r *http.Request) {
	if !h.requirePasswordConfirm(w, r) {
		return
	}
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	userID, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleAdmin {
		if me.Role != auth.RoleOrgAdmin || me.OrgID == nil || *me.OrgID != orgID {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
	}
	if me.ID == userID {
		httpx.Error(w, http.StatusConflict, "cannot change your own role")
		return
	}
	var req setOrgMemberRoleRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if !orgRole(req.Role) {
		httpx.Error(w, http.StatusBadRequest, "role must be 'org_admin', 'org_member' or 'org_writer'")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "begin tx failed")
		return
	}
	defer tx.Rollback() //nolint:errcheck
	var was string
	err = tx.QueryRowContext(r.Context(),
		`SELECT role FROM org_memberships WHERE user_id = $1 AND org_id = $2 FOR UPDATE`, userID, orgID).Scan(&was)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "user not in this organization")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if was == auth.RoleOrgAdmin && req.Role != auth.RoleOrgAdmin {
		var admins int
		if err := tx.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM org_memberships WHERE org_id = $1 AND role = $2`, orgID, auth.RoleOrgAdmin).Scan(&admins); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if admins <= 1 {
			httpx.Error(w, http.StatusConflict, "an organization needs at least one admin")
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE org_memberships SET role = $1 WHERE user_id = $2 AND org_id = $3`, req.Role, userID, orgID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "role change failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	logx.Info("admin.org_member.role_changed", "user_id", userID, "org_id", orgID, "from", was, "to", req.Role, "by", me.ID)
	httpx.JSON(w, http.StatusOK, map[string]string{"role": req.Role})
}

/* ---------------- Two-factor requirement ---------------- */

// SetOrgRequireTOTP flips whether every member of the organisation
// must use two-factor authentication (auth.RequireTOTPSetup enforces
// it). An admin of that organisation acting in it may, and so may a
// full global admin (the route logs that in the admin action log).
// Turning it on needs ZV_TOTP_KEY, else nobody could comply, and the
// actor's own 2FA; turning it off needs the actor's password. Either
// way it goes in the org's Settings → Audit.
func (h *Handlers) SetOrgRequireTOTP(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleAdmin && (me.Role != auth.RoleOrgAdmin || me.OrgID == nil || *me.OrgID != orgID) {
		httpx.Error(w, http.StatusForbidden, "only an organization's admins choose whether its members need two-factor authentication")
		return
	}
	var req setBoolRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Enabled && h.TOTPKey == nil {
		httpx.Error(w, http.StatusConflict, "two-factor authentication is not available on this server")
		return
	}
	if req.Enabled && refuseWithoutOwnTOTP(w, me) {
		return
	}
	if !req.Enabled && !h.requirePasswordConfirm(w, r) {
		return
	}
	noteAction(r, onOff(req.Enabled))
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE organizations SET require_totp = $1 WHERE id = $2`, req.Enabled, orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "organization not found")
		return
	}
	reason := auth.ReasonRequireTOTPOff
	if req.Enabled {
		reason = auth.ReasonRequireTOTPOn
	}
	auth.Record(r.Context(), h.DB, auth.AuditEvent{
		Event: auth.EventOrgRequireTOTP, OK: true, UserID: &me.ID, Username: me.Username,
		IP: auth.ClientIP(r), UserAgent: auth.UserAgent(r), Reason: reason, OrgID: &orgID,
	})
	httpx.JSON(w, http.StatusOK, map[string]bool{"require_totp": req.Enabled})
}

/* ---------------- Transfer of ownership ---------------- */

type transferOwnershipRequest struct {
	ToUserID int64 `json:"to_user_id"`
}

// TransferOwnership atomically promotes a target member to org_admin
// and demotes the calling org_admin to org_member. The endpoint
// exists so the previously-impossible "I'm the only admin and want
// to step down" path becomes a single confirmed click instead of a
// careful sequence (promote-then-demote) that fails open if the user
// gets distracted between calls.
//
// Caller must already be an org_admin of this org. Target must be a
// member of the same org and a different user. Wrapped in a single
// transaction so a partial failure can never leave the org with two
// admins or zero admins.
func (h *Handlers) TransferOwnership(w http.ResponseWriter, r *http.Request) {
	if !h.requirePasswordConfirm(w, r) {
		return
	}
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleOrgAdmin || me.OrgID == nil || *me.OrgID != orgID {
		httpx.Error(w, http.StatusForbidden, "only an org admin can transfer ownership")
		return
	}
	var req transferOwnershipRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.ToUserID == me.ID {
		httpx.Error(w, http.StatusBadRequest, "cannot transfer ownership to yourself")
		return
	}
	// Verify the target has a membership in this org. The role
	// column is guaranteed to be 'org_admin' or 'org_member' by the
	// table CHECK, so we don't need to read it — existence alone is
	// the precondition for ownership transfer.
	var found int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT 1 FROM org_memberships WHERE user_id = $1 AND org_id = $2`,
		req.ToUserID, orgID).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "target user is not in this organization")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "begin tx")
		return
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE org_memberships SET role = $1 WHERE user_id = $2 AND org_id = $3`,
		auth.RoleOrgAdmin, req.ToUserID, orgID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "promote failed", err)
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE org_memberships SET role = $1 WHERE user_id = $2 AND org_id = $3`,
		auth.RoleOrgMember, me.ID, orgID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "demote failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}
	logx.Info("admin.org.ownership_transferred",
		"org_id", orgID, "from_user_id", me.ID, "to_user_id", req.ToUserID)
	httpx.JSON(w, http.StatusNoContent, nil)
}

/* ---------------- Assignments (admin grants, org consumes) ---------------- */

type createAssignmentRequest struct {
	AdventureSlug string `json:"adventure_slug"`
	MaxPlayers    int    `json:"max_players"`
}

// CreateAssignment grants the organization one more single-use licence.
// Admin-only.
func (h *Handlers) CreateAssignment(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	var req createAssignmentRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if !h.passableSlug(r.Context(), orgID, req.AdventureSlug) {
		httpx.Error(w, http.StatusBadRequest, "unknown adventure")
		return
	}
	if req.MaxPlayers <= 0 {
		httpx.Error(w, http.StatusBadRequest, "max_players must be > 0")
		return
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO adventure_assignments(org_id, adventure_slug, max_players)
		VALUES ($1, $2, $3)
		RETURNING id
	`, orgID, req.AdventureSlug, req.MaxPlayers).Scan(&id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "assign failed")
		return
	}
	httpx.JSON(w, http.StatusCreated, assignmentDTO{
		ID: id, AdventureSlug: req.AdventureSlug, MaxPlayers: req.MaxPlayers,
	})
}

type updateAssignmentRequest struct {
	AdventureSlug *string `json:"adventure_slug,omitempty"`
	MaxPlayers    *int    `json:"max_players,omitempty"`
}

// UpdateAssignment amends an organization's assignment. Admin-only.
func (h *Handlers) UpdateAssignment(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	assignmentID, err := strconv.ParseInt(chi.URLParam(r, "assignmentID"), 10, 64)
	if err != nil || assignmentID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad assignment id")
		return
	}
	var req updateAssignmentRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.AdventureSlug == nil && req.MaxPlayers == nil {
		httpx.Error(w, http.StatusBadRequest, "nothing to update")
		return
	}
	var (
		ownerOrg    int64
		currentSlug string
		currentMax  int
		eventID     sql.NullInt64
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT aa.org_id, aa.adventure_slug, aa.max_players, ar.id
		FROM adventure_assignments aa
		LEFT JOIN events ar ON ar.assignment_id = aa.id
		WHERE aa.id = $1
	`, assignmentID).Scan(&ownerOrg, &currentSlug, &currentMax, &eventID)
	if errors.Is(err, sql.ErrNoRows) || ownerOrg != orgID {
		httpx.Error(w, http.StatusNotFound, "event pass not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	newSlug := currentSlug
	if req.AdventureSlug != nil && *req.AdventureSlug != currentSlug {
		if eventID.Valid {
			httpx.Error(w, http.StatusConflict, "cannot change the adventure of a used event pass")
			return
		}
		if !h.passableSlug(r.Context(), orgID, *req.AdventureSlug) {
			httpx.Error(w, http.StatusBadRequest, "unknown adventure")
			return
		}
		newSlug = *req.AdventureSlug
	}
	newMax := currentMax
	if req.MaxPlayers != nil {
		if *req.MaxPlayers <= 0 {
			httpx.Error(w, http.StatusBadRequest, "max_players must be > 0")
			return
		}
		if eventID.Valid {
			var slotCount int
			if err := h.DB.QueryRowContext(r.Context(), `
				SELECT COUNT(*) FROM player_slots ps
				 JOIN teams t ON t.id = ps.team_id
				WHERE t.event_id = $1
			`, eventID.Int64).Scan(&slotCount); err != nil {
				httpx.Error(w, http.StatusInternalServerError, "slot count failed")
				return
			}
			if *req.MaxPlayers < slotCount {
				httpx.Error(w, http.StatusConflict,
					"the consuming event already has "+strconv.Itoa(slotCount)+" player slot(s); delete teams first")
				return
			}
		}
		newMax = *req.MaxPlayers
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE adventure_assignments SET adventure_slug = $1, max_players = $2 WHERE id = $3`,
		newSlug, newMax, assignmentID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	httpx.JSON(w, http.StatusOK, assignmentDTO{
		ID: assignmentID, AdventureSlug: newSlug, MaxPlayers: newMax,
	})
}

// DeleteAssignment revokes an unused assignment. Admin-only.
func (h *Handlers) DeleteAssignment(w http.ResponseWriter, r *http.Request) {
	orgID, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	assignmentID, err := strconv.ParseInt(chi.URLParam(r, "assignmentID"), 10, 64)
	if err != nil || assignmentID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad assignment id")
		return
	}
	var (
		ownerOrg int64
		eventID  sql.NullInt64
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT aa.org_id, ar.id
		FROM adventure_assignments aa
		LEFT JOIN events ar ON ar.assignment_id = aa.id
		WHERE aa.id = $1
	`, assignmentID).Scan(&ownerOrg, &eventID)
	if errors.Is(err, sql.ErrNoRows) || ownerOrg != orgID {
		httpx.Error(w, http.StatusNotFound, "event pass not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if eventID.Valid {
		httpx.Error(w, http.StatusConflict, "this event pass is already used by an event")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM adventure_assignments WHERE id = $1`, assignmentID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// ListMyAssignments returns the caller's organization's assignments.
func (h *Handlers) ListMyAssignments(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"assignments": []assignmentDTO{}})
		return
	}
	list, err := h.listAssignments(r.Context(), *me.OrgID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"assignments": list})
}

/* ---------------- Admins (global admin users) ---------------- */

type adminDTO struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	// Role is auth.RoleAdmin or auth.RoleSupport.
	Role string `json:"role"`
	// TOTPEnabled: the admin signs in with two-factor authentication.
	TOTPEnabled bool `json:"totp_enabled"`
	// HasEmail: the admin has an address a reset link can go to.
	HasEmail bool `json:"has_email"`
}

// adminRoleSupport maps a requested admin role to users.is_support.
// ok is false for anything but auth.RoleAdmin and auth.RoleSupport.
func adminRoleSupport(role string) (support, ok bool) {
	switch role {
	case auth.RoleAdmin:
		return false, true
	case auth.RoleSupport:
		return true, true
	}
	return false, false
}

// ListAdmins returns every global admin with their role. Admin-only.
func (h *Handlers) ListAdmins(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, username, is_support, totp_enabled, COALESCE(email, '') <> '' FROM users WHERE is_admin = TRUE ORDER BY LOWER(username)
	`)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []adminDTO{}
	for rows.Next() {
		var a adminDTO
		var support bool
		if err := rows.Scan(&a.ID, &a.Username, &support, &a.TOTPEnabled, &a.HasEmail); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		a.Role = auth.User{Support: support}.AdminRole()
		out = append(out, a)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"admins": out})
}

type createAdminRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	// Role is auth.RoleAdmin (the default) or auth.RoleSupport.
	Role string `json:"role"`
}

// CreateAdmin adds another global admin. Admin-only, cookie-only
// (RequireSession on the route), and reauthed with X-Confirm-Password —
// the same bar DeleteAdmin holds, so minting an admin is no weaker than
// removing one.
func (h *Handlers) CreateAdmin(w http.ResponseWriter, r *http.Request) {
	if !h.requirePasswordConfirm(w, r) {
		return
	}
	var req createAdminRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		httpx.Error(w, http.StatusBadRequest, "username and password required")
		return
	}
	if req.Role == "" {
		req.Role = auth.RoleAdmin
	}
	support, ok := adminRoleSupport(req.Role)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "role must be admin or support")
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "hash failed")
		return
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO users(username, password_hash, is_admin, is_support) VALUES ($1, $2, TRUE, $3)
		RETURNING id
	`, req.Username, hash, support).Scan(&id); err != nil {
		httpx.Error(w, http.StatusConflict, "username not available")
		return
	}
	noteTarget(r, req.Username)
	httpx.JSON(w, http.StatusCreated, adminDTO{ID: id, Username: req.Username, Role: req.Role})
}

// DeleteAdmin removes another admin. Refuses to delete the caller and
// refuses to delete the last remaining admin.
func (h *Handlers) DeleteAdmin(w http.ResponseWriter, r *http.Request) {
	if !h.requirePasswordConfirm(w, r) {
		return
	}
	id, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.ID == id {
		httpx.Error(w, http.StatusConflict, "cannot delete yourself")
		return
	}
	var isAdmin bool
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT is_admin FROM users WHERE id = $1`, id).Scan(&isAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !isAdmin {
		httpx.Error(w, http.StatusBadRequest, "not an admin")
		return
	}
	var remaining int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE is_admin = TRUE`).Scan(&remaining); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "count failed")
		return
	}
	if remaining <= 1 {
		httpx.Error(w, http.StatusConflict, "cannot delete the last admin")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM users WHERE id = $1`, id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// SetAdminRole makes another global admin a full admin or Support.
// Admin-only, cookie-only and reauthed with X-Confirm-Password like
// CreateAdmin. Refuses the caller's own role, so a full admin remains.
func (h *Handlers) SetAdminRole(w http.ResponseWriter, r *http.Request) {
	if !h.requirePasswordConfirm(w, r) {
		return
	}
	id, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	support, ok := adminRoleSupport(req.Role)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "role must be admin or support")
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.ID == id {
		httpx.Error(w, http.StatusConflict, "cannot change your own role")
		return
	}
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE users SET is_support = $1 WHERE id = $2 AND is_admin = TRUE`, support, id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(w, http.StatusNotFound, "not an admin")
		return
	}
	noteAction(r, auth.User{Support: support}.AdminRole())
	httpx.JSON(w, http.StatusNoContent, nil)
}

// SendAdminResetLink mails a global admin the reset link "Forgot
// password" sends, to their own address. It never sets the password:
// the admin chooses it from the link. Admin-only, cookie-only.
func (h *Handlers) SendAdminResetLink(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	var email string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(email, '') FROM users WHERE id = $1 AND is_admin = TRUE`, id).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "not an admin")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if email == "" {
		httpx.Error(w, http.StatusConflict, "this admin has no email address")
		return
	}
	if h.PublicURL == "" {
		httpx.Error(w, http.StatusConflict, "ZV_PUBLIC_URL is not set, so a reset link would not work")
		return
	}
	tok, err := auth.IssueForgotPasswordToken(r.Context(), h.DB, id)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "token issue failed", err)
		return
	}
	lang := h.userLang(r, id)
	subject, c := emailtpl.PasswordReset(lang, auth.ResetLink(h.PublicURL, tok))
	c.Picture = emailtpl.House(h.PublicURL, lang, emailtpl.PictureReset)
	if err := h.send(r.Context(), "password_reset", emailtpl.Message([]string{email}, subject, emailtpl.Classic, emailtpl.Platform(), c)); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "sending failed", err)
		return
	}
	me, _ := auth.FromContext(r.Context())
	logx.Info("admin.admins.reset_link", "user_id", id, "by", me.ID)
	httpx.JSON(w, http.StatusNoContent, nil)
}

/* ---------------- Internal loaders ---------------- */

func (h *Handlers) loadOrgMembers(ctx context.Context, o *organizationDTO) error {
	// A member is anyone with an org_memberships row for this org.
	// The role shown is the per-membership role (a user can be
	// admin here and member elsewhere). The active-events count is
	// scoped to events created by this user IN THIS ORG and still
	// in a non-terminal state — used by the UI to warn before
	// removing a member who would otherwise orphan live events.
	rows, err := h.DB.QueryContext(ctx, `
		SELECT u.id, u.username, m.role, COALESCE(u.email, ''), (u.email_verified <> 0), u.totp_enabled,
		       (SELECT COUNT(*) FROM events e
		          WHERE e.org_id = $1
		            AND e.created_by_user_id = u.id
		            AND e.state IN ('scheduled', 'running', 'paused'))
		FROM org_memberships m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1
		ORDER BY m.role, LOWER(u.username)
	`, o.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var m orgMemberDTO
		if err := rows.Scan(&m.ID, &m.Username, &m.Role, &m.Email, &m.EmailVerified, &m.TOTPEnabled, &m.ActiveEventsOwned); err != nil {
			return err
		}
		o.Members = append(o.Members, m)
	}
	return nil
}

func (h *Handlers) loadOrgInvitations(ctx context.Context, o *organizationDTO) error {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT i.user_id, COALESCE(i.email, u.username), i.role, i.created_at, i.declined_at IS NOT NULL
		FROM org_invitations i JOIN users u ON u.id = i.user_id
		WHERE i.org_id = $1
		ORDER BY i.created_at, i.user_id
	`, o.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var inv pendingInvitationDTO
		if err := rows.Scan(&inv.UserID, &inv.Email, &inv.Role, &inv.CreatedAt, &inv.Declined); err != nil {
			return err
		}
		o.Invitations = append(o.Invitations, inv)
	}
	return rows.Err()
}

func (h *Handlers) loadOrgAssignments(ctx context.Context, o *organizationDTO) error {
	list, err := h.listAssignments(ctx, o.ID)
	if err != nil {
		return err
	}
	o.Assignments = list
	return nil
}

type ownAdventureDTO struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

func (h *Handlers) loadOwnAdventures(ctx context.Context, o *organizationDTO) error {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT slug, title FROM adventures
		WHERE org_id = $1 AND published_graph_id IS NOT NULL AND NOT trail_hidden
		ORDER BY title, slug
	`, o.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a ownAdventureDTO
		if err := rows.Scan(&a.Slug, &a.Title); err != nil {
			return err
		}
		o.OwnAdventures = append(o.OwnAdventures, a)
	}
	return rows.Err()
}

// passableSlug reports whether the operator may grant orgID an event
// pass for slug: a catalog adventure, or one of the org's own published
// ones (loadOwnAdventures).
func (h *Handlers) passableSlug(ctx context.Context, orgID int64, slug string) bool {
	if h.isCatalogSlug(ctx, slug) {
		return true
	}
	var own bool
	err := h.DB.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM adventures
		  WHERE slug = $1 AND org_id = $2 AND published_graph_id IS NOT NULL AND NOT trail_hidden)
	`, slug, orgID).Scan(&own)
	return err == nil && own
}

func (h *Handlers) listAssignments(ctx context.Context, orgID int64) ([]assignmentDTO, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT aa.id, aa.adventure_slug, aa.max_players, aa.created_at, ar.id, COALESCE(ar.state = 'cancelled', false)
		FROM adventure_assignments aa
		LEFT JOIN events ar ON ar.assignment_id = aa.id
		WHERE aa.org_id = $1
		ORDER BY aa.created_at DESC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []assignmentDTO{}
	for rows.Next() {
		var (
			as      assignmentDTO
			eventID sql.NullInt64
		)
		if err := rows.Scan(&as.ID, &as.AdventureSlug, &as.MaxPlayers, &as.CreatedAt, &eventID, &as.EventCancelled); err != nil {
			return nil, err
		}
		if eventID.Valid {
			v := eventID.Int64
			as.EventID = &v
		}
		if adv := h.Lib.Get(as.AdventureSlug); adv != nil {
			as.AdventureTitle = adv.Meta.Title
		}
		out = append(out, as)
	}
	return out, nil
}

func orgIDFromURL(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "orgID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
