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
}

type orgMemberDTO struct {
	ID            int64  `json:"id"`
	Username      string `json:"username"`
	Role          string `json:"role"` // org_admin | org_member
	EmailVerified bool   `json:"email_verified"`
	// ActiveEventsOwned is the count of non-terminal events
	// (scheduled / running / paused) this member created in this org.
	// Surfaced so the remove-member UI can warn that destroying the
	// member orphans their owned events. 0 means "safe to remove".
	ActiveEventsOwned int `json:"active_events_owned"`
}

type organizationDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Slug powers the per-org store URL (/o/{slug}/store). Auto-derived
	// from name at creation time, editable by org_admin afterwards.
	// See internal/orgslug.
	Slug          string          `json:"slug,omitempty"`
	CreatedAt     string          `json:"created_at"`
	Members       []orgMemberDTO  `json:"members"`
	Assignments   []assignmentDTO `json:"assignments"`
	PlayersTotal  int             `json:"players_total"`
	EventsTotal   int             `json:"events_total"`
	EventsRunning int             `json:"events_running"`
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
		        WHERE ar.org_id = o.id) AS players_total,
		       (SELECT COUNT(*) FROM events ar WHERE ar.org_id = o.id),
		       (SELECT COUNT(*) FROM events ar WHERE ar.org_id = o.id AND ar.state = 'running'),
		       (o.logo_content IS NOT NULL AND length(o.logo_content) > 0) AS has_logo,
		       (o.trusted <> 0) AS trusted,
		       o.is_publisher
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
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt, &o.PlayersTotal, &o.EventsTotal, &o.EventsRunning, &o.HasLogo, &o.Trusted, &o.IsPublisher); err != nil {
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
func (h *Handlers) GetOrganization(w http.ResponseWriter, r *http.Request) {
	id, ok := orgIDFromURL(r)
	if !ok {
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleAdmin {
		if me.OrgID == nil || *me.OrgID != id {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
	}
	var o organizationDTO
	o.Members = []orgMemberDTO{}
	o.Assignments = []assignmentDTO{}
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT o.id, o.name, o.slug, o.created_at,
		       (SELECT COUNT(*) FROM player_slots ps
		         JOIN teams t ON t.id = ps.team_id
		         JOIN events ar ON ar.id = t.event_id
		        WHERE ar.org_id = o.id) AS players_total,
		       (SELECT COUNT(*) FROM events ar WHERE ar.org_id = o.id),
		       (SELECT COUNT(*) FROM events ar WHERE ar.org_id = o.id AND ar.state = 'running'),
		       (o.logo_content IS NOT NULL AND length(o.logo_content) > 0) AS has_logo,
		       (o.trusted <> 0) AS trusted,
		       o.is_publisher
		FROM organizations o WHERE o.id = $1
	`, id).Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt, &o.PlayersTotal, &o.EventsTotal, &o.EventsRunning, &o.HasLogo, &o.Trusted, &o.IsPublisher)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "organization not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if err := h.loadOrgMembers(r.Context(), &o); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "members failed")
		return
	}
	if err := h.loadOrgAssignments(r.Context(), &o); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "assignments failed")
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
	httpx.JSON(w, http.StatusOK, map[string]any{"trusted": req.Trusted})
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
	Email    string `json:"email"` // optional; only consumed on the new-user branch
	Password string `json:"password"`
	Role     string `json:"role"` // org_admin | org_member
}

// CreateOrgMember adds a new user into an organization as either org_admin
// or org_member. Callable by global admin, or by an org_admin of the same
// organization. The user's initial password is set here (the seed admin
// flow is out-of-band for admins, but these are ordinary org accounts).
// CreateOrgMember adds a user to this organisation. Two flows behind
// one endpoint:
//
//   - Username doesn't exist yet → create the user (legacy path) AND
//     insert the org_memberships row. Password is required.
//   - Username exists → only insert the org_memberships row (the user
//     is already onboarded; this just grants an additional org). Any
//     password supplied in the request is ignored — we don't want
//     admins accidentally resetting someone's credentials by clicking
//     "Add to org".
//
// Refuses to add admins (Role == admin) — admins are global and have
// no memberships. Returns 409 if the user is already a member.
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
	if req.Role != auth.RoleOrgAdmin && req.Role != auth.RoleOrgMember {
		httpx.Error(w, http.StatusBadRequest, "role must be 'org_admin' or 'org_member'")
		return
	}

	// Resolve the user — nil row → new-user flow, populated row →
	// add-existing-user-to-org flow.
	var (
		existingID      int64
		existingIsAdmin bool
	)
	lookupErr := h.DB.QueryRowContext(r.Context(),
		`SELECT id, is_admin FROM users WHERE username = $1`, req.Username,
	).Scan(&existingID, &existingIsAdmin)
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if lookupErr == nil {
		if existingIsAdmin {
			httpx.Error(w, http.StatusBadRequest, "admins are not org members")
			return
		}
		// Only accounts that belong to no other organisation can be
		// added directly: a person who signed up and is still in the
		// lobby. Pulling in an account that already belongs to another
		// org without that person's say-so would hand this org's admin
		// a foothold on a login they use elsewhere (and, combined with
		// the credential-reset endpoints, a takeover). Adding an
		// existing member of another org needs an invitation the person
		// accepts — a flow that does not exist yet, so refuse for now.
		var otherOrgs int
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM org_memberships WHERE user_id = $1 AND org_id <> $2`,
			existingID, orgID).Scan(&otherOrgs); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if otherOrgs > 0 {
			httpx.Error(w, http.StatusConflict, "that user already belongs to another organisation and must accept an invitation to join")
			return
		}
		// Insert only the membership row — no password reset, no
		// account changes. Conflict on (user_id, org_id) means
		// already a member of this org.
		res, err := h.DB.ExecContext(r.Context(), `
			INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, $3)
			ON CONFLICT (user_id, org_id) DO NOTHING
		`, existingID, orgID, req.Role)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "membership insert failed")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			httpx.Error(w, http.StatusConflict, "user is already a member of this organization")
			return
		}
		httpx.JSON(w, http.StatusCreated, orgMemberDTO{ID: existingID, Username: req.Username, Role: req.Role})
		return
	}

	// New-user flow. Password is required since we're creating the
	// account from scratch.
	if req.Password == "" {
		httpx.Error(w, http.StatusBadRequest, "password required for new users")
		return
	}
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
	// Email is optional. When supplied, mark it pre-verified the same
	// way migration 0004 grandfathered admin-created users — the org
	// admin is creating the account on the user's behalf and takes
	// responsibility for the address. Without this, the new account
	// would default to email_verified = 0 and be locked out of
	// money-touching actions until it could verify, which makes no
	// sense for an org-admin-provisioned user.
	var emailParam any
	emailVerified := 0
	if req.Email != "" {
		emailParam = req.Email
		emailVerified = 1
	}
	if err := tx.QueryRowContext(r.Context(), `
		INSERT INTO users(username, email, email_verified, password_hash, created_by_user_id, created_by_org_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, req.Username, emailParam, emailVerified, hash, me.ID, orgID).Scan(&id); err != nil {
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
	httpx.JSON(w, http.StatusCreated, orgMemberDTO{ID: id, Username: req.Username, Role: req.Role})
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
	if h.Lib.Get(req.AdventureSlug) == nil || !h.isCatalogSlug(r.Context(), req.AdventureSlug) {
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
		if h.Lib.Get(*req.AdventureSlug) == nil || !h.isCatalogSlug(r.Context(), *req.AdventureSlug) {
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
}

// ListAdmins returns every global admin. Admin-only.
func (h *Handlers) ListAdmins(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, username FROM users WHERE is_admin = TRUE ORDER BY LOWER(username)
	`)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	out := []adminDTO{}
	for rows.Next() {
		var a adminDTO
		if err := rows.Scan(&a.ID, &a.Username); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		out = append(out, a)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"admins": out})
}

type createAdminRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
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
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "hash failed")
		return
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO users(username, password_hash, is_admin) VALUES ($1, $2, TRUE)
		RETURNING id
	`, req.Username, hash).Scan(&id); err != nil {
		httpx.Error(w, http.StatusConflict, "username not available")
		return
	}
	httpx.JSON(w, http.StatusCreated, adminDTO{ID: id, Username: req.Username})
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

/* ---------------- Internal loaders ---------------- */

func (h *Handlers) loadOrgMembers(ctx context.Context, o *organizationDTO) error {
	// A member is anyone with an org_memberships row for this org.
	// The role shown is the per-membership role (a user can be
	// admin here and member elsewhere). The active-events count is
	// scoped to events created by this user IN THIS ORG and still
	// in a non-terminal state — used by the UI to warn before
	// removing a member who would otherwise orphan live events.
	rows, err := h.DB.QueryContext(ctx, `
		SELECT u.id, u.username, m.role, (u.email_verified <> 0),
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
		if err := rows.Scan(&m.ID, &m.Username, &m.Role, &m.EmailVerified, &m.ActiveEventsOwned); err != nil {
			return err
		}
		o.Members = append(o.Members, m)
	}
	return nil
}

func (h *Handlers) loadOrgAssignments(ctx context.Context, o *organizationDTO) error {
	list, err := h.listAssignments(ctx, o.ID)
	if err != nil {
		return err
	}
	o.Assignments = list
	return nil
}

func (h *Handlers) listAssignments(ctx context.Context, orgID int64) ([]assignmentDTO, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT aa.id, aa.adventure_slug, aa.max_players, aa.created_at, ar.id
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
		if err := rows.Scan(&as.ID, &as.AdventureSlug, &as.MaxPlayers, &as.CreatedAt, &eventID); err != nil {
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
