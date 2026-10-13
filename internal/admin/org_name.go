package admin

import (
	"net/http"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
)

// SetMyOrgName renames the caller's organisation. RoleOrgAdmin only.
// Names are unique, so a taken one is a 409. Once something of the
// organisation's is for sale the name waits for review, as the tagline
// does (presentation.go): the reply's proposed says so, and name stays
// the live one until a reviewer approves.
//
//	PUT /api/admin/orgs/me/name {name}
func (h *Handlers) SetMyOrgName(w http.ResponseWriter, r *http.Request) {
	me, ok := auth.FromContext(r.Context())
	if !ok || me.OrgID == nil || *me.OrgID == 0 {
		httpx.Error(w, http.StatusForbidden, "not in an organization")
		return
	}
	if me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "org admins only")
		return
	}
	orgID := *me.OrgID
	var req struct {
		Name string `json:"name"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	name, err := validateName(req.Name, MaxNameLen)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	var live string
	var taken bool
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT name, EXISTS (SELECT 1 FROM organizations WHERE name = $2 AND id <> $1)
		FROM organizations WHERE id = $1`, orgID, name).Scan(&live, &taken); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if taken {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "another organization already has that name", "code": "org_name_taken"})
		return
	}
	gated, err := orgGated(r.Context(), h.DB, orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "update failed", err)
		return
	}
	if gated {
		fields, unset := map[string]any{"name": name}, []string(nil)
		if name == live {
			fields, unset = map[string]any{}, []string{"name"}
		}
		if err := propose(r.Context(), h.DB, listingOrg, orgID, orgID, fields, unset, nil, nil); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "update failed", err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"name": live, "proposed": name != live})
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `UPDATE organizations SET name = $1 WHERE id = $2`, name, orgID); err != nil {
		if isUniqueViolation(err) {
			httpx.JSON(w, http.StatusConflict, map[string]string{"error": "another organization already has that name", "code": "org_name_taken"})
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "update failed", err)
		return
	}
	logx.Info("admin.org.name.updated", "org_id", orgID, "name", name, "by", me.Username)
	httpx.JSON(w, http.StatusOK, map[string]any{"name": name})
}
