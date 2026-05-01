package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/logx"
	"github.com/go-chi/chi/v5"
)

/* ---------------- DTOs ---------------- */

type assignmentDTO struct {
	ID             int64  `json:"id"`
	AdventureSlug  string `json:"adventure_slug"`
	AdventureTitle string `json:"adventure_title,omitempty"`
	MaxTeams       int    `json:"max_teams"`
	CreatedAt      string `json:"created_at"`
	EventID          *int64 `json:"event_id,omitempty"`
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
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
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
}

/* ---------------- Admin-only: organizations CRUD ---------------- */

// ListOrganizations returns every organization with its members, assignments
// and activity counts. Admin-only.
func (h *Handlers) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT o.id, o.name, o.created_at,
		       0 AS players_total,
		       (SELECT COUNT(*) FROM events ar WHERE ar.org_id = o.id),
		       (SELECT COUNT(*) FROM events ar WHERE ar.org_id = o.id AND ar.state = 'running'),
		       (o.logo_content IS NOT NULL AND length(o.logo_content) > 0) AS has_logo
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
		if err := rows.Scan(&o.ID, &o.Name, &o.CreatedAt, &o.PlayersTotal, &o.EventsTotal, &o.EventsRunning, &o.HasLogo); err != nil {
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
		SELECT o.id, o.name, o.created_at,
		       0 AS players_total,
		       (SELECT COUNT(*) FROM events ar WHERE ar.org_id = o.id),
		       (SELECT COUNT(*) FROM events ar WHERE ar.org_id = o.id AND ar.state = 'running'),
		       (o.logo_content IS NOT NULL AND length(o.logo_content) > 0) AS has_logo
		FROM organizations o WHERE o.id = $1
	`, id).Scan(&o.ID, &o.Name, &o.CreatedAt, &o.PlayersTotal, &o.EventsTotal, &o.EventsRunning, &o.HasLogo)
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
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `INSERT INTO organizations(name) VALUES ($1) RETURNING id`, req.Name).Scan(&id); err != nil {
		httpx.Error(w, http.StatusConflict, "name already used")
		return
	}
	httpx.JSON(w, http.StatusCreated, organizationDTO{
		ID: id, Name: req.Name, Members: []orgMemberDTO{}, Assignments: []assignmentDTO{},
	})
}

type updateOrgRequest struct {
	Name *string `json:"name,omitempty"`
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
	if req.Name == nil || strings.TrimSpace(*req.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "name required")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE organizations SET name = $1 WHERE id = $2`, strings.TrimSpace(*req.Name), id); err != nil {
		httpx.Error(w, http.StatusConflict, "name already used")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// DeleteOrganization deletes an organization (and everything it cascades to:
// users in it, runs, teams, assignments). Admin-only and password-gated.
func (h *Handlers) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	if !requirePasswordConfirm(w, r, h.DB) {
		return
	}
	id, ok := orgIDFromURL(r)
	if !ok {
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
	if req.Username == "" {
		httpx.Error(w, http.StatusBadRequest, "username required")
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
	if err := tx.QueryRowContext(r.Context(), `
		INSERT INTO users(username, password_hash, created_by_user_id)
		VALUES ($1, $2, $3)
		RETURNING id
	`, req.Username, hash, me.ID).Scan(&id); err != nil {
		httpx.Error(w, http.StatusConflict, "username not available")
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
	if !requirePasswordConfirm(w, r, h.DB) {
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

	if memberOf <= 1 {
		// Last (or only) membership — preserve legacy hard-delete so
		// the user doesn't end up as a member-of-nothing ghost.
		if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "delete failed")
			return
		}
		httpx.JSON(w, http.StatusNoContent, nil)
		return
	}

	// Multi-org user — surgical remove. Wrap in a tx so the
	// membership drop and active-session reset commit together.
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
	if !requirePasswordConfirm(w, r, h.DB) {
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
		httpx.Error(w, http.StatusInternalServerError, "promote failed: "+err.Error())
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE org_memberships SET role = $1 WHERE user_id = $2 AND org_id = $3`,
		auth.RoleOrgMember, me.ID, orgID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "demote failed: "+err.Error())
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
	MaxTeams      int    `json:"max_teams"`
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
	if h.Lib.Get(req.AdventureSlug) == nil {
		httpx.Error(w, http.StatusBadRequest, "unknown adventure")
		return
	}
	if req.MaxTeams <= 0 {
		httpx.Error(w, http.StatusBadRequest, "max_teams must be > 0")
		return
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO adventure_assignments(org_id, adventure_slug, max_teams)
		VALUES ($1, $2, $3)
		RETURNING id
	`, orgID, req.AdventureSlug, req.MaxTeams).Scan(&id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "assign failed")
		return
	}
	httpx.JSON(w, http.StatusCreated, assignmentDTO{
		ID: id, AdventureSlug: req.AdventureSlug, MaxTeams: req.MaxTeams,
	})
}

type updateAssignmentRequest struct {
	AdventureSlug *string `json:"adventure_slug,omitempty"`
	MaxTeams      *int    `json:"max_teams,omitempty"`
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
	if req.AdventureSlug == nil && req.MaxTeams == nil {
		httpx.Error(w, http.StatusBadRequest, "nothing to update")
		return
	}
	var (
		ownerOrg    int64
		currentSlug string
		currentMax  int
		eventID       sql.NullInt64
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT aa.org_id, aa.adventure_slug, aa.max_teams, ar.id
		FROM adventure_assignments aa
		LEFT JOIN events ar ON ar.assignment_id = aa.id
		WHERE aa.id = $1
	`, assignmentID).Scan(&ownerOrg, &currentSlug, &currentMax, &eventID)
	if errors.Is(err, sql.ErrNoRows) || ownerOrg != orgID {
		httpx.Error(w, http.StatusNotFound, "assignment not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	newSlug := currentSlug
	if req.AdventureSlug != nil && *req.AdventureSlug != currentSlug {
		if eventID.Valid {
			httpx.Error(w, http.StatusConflict, "cannot change adventure on a used assignment")
			return
		}
		if h.Lib.Get(*req.AdventureSlug) == nil {
			httpx.Error(w, http.StatusBadRequest, "unknown adventure")
			return
		}
		newSlug = *req.AdventureSlug
	}
	newMax := currentMax
	if req.MaxTeams != nil {
		if *req.MaxTeams <= 0 {
			httpx.Error(w, http.StatusBadRequest, "max_teams must be > 0")
			return
		}
		if eventID.Valid {
			var teamCount int
			if err := h.DB.QueryRowContext(r.Context(),
				`SELECT COUNT(*) FROM teams WHERE event_id = $1`, eventID.Int64).Scan(&teamCount); err != nil {
				httpx.Error(w, http.StatusInternalServerError, "team count failed")
				return
			}
			if *req.MaxTeams < teamCount {
				httpx.Error(w, http.StatusConflict,
					"the consuming run already has "+strconv.Itoa(teamCount)+" team(s); delete teams first")
				return
			}
		}
		newMax = *req.MaxTeams
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE adventure_assignments SET adventure_slug = $1, max_teams = $2 WHERE id = $3`,
		newSlug, newMax, assignmentID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	httpx.JSON(w, http.StatusOK, assignmentDTO{
		ID: assignmentID, AdventureSlug: newSlug, MaxTeams: newMax,
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
		eventID    sql.NullInt64
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT aa.org_id, ar.id
		FROM adventure_assignments aa
		LEFT JOIN events ar ON ar.assignment_id = aa.id
		WHERE aa.id = $1
	`, assignmentID).Scan(&ownerOrg, &eventID)
	if errors.Is(err, sql.ErrNoRows) || ownerOrg != orgID {
		httpx.Error(w, http.StatusNotFound, "assignment not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if eventID.Valid {
		httpx.Error(w, http.StatusConflict, "assignment is already in use by an event")
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

// CreateAdmin adds another global admin. Admin-only.
func (h *Handlers) CreateAdmin(w http.ResponseWriter, r *http.Request) {
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
	if !requirePasswordConfirm(w, r, h.DB) {
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
		SELECT aa.id, aa.adventure_slug, aa.max_teams, aa.created_at, ar.id
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
			as    assignmentDTO
			eventID sql.NullInt64
		)
		if err := rows.Scan(&as.ID, &as.AdventureSlug, &as.MaxTeams, &as.CreatedAt, &eventID); err != nil {
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
