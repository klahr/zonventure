package admin

import (
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
)

var countryRe = regexp.MustCompile(`^[A-Z]{2}$`)

// GetMyShipCountries lists the countries the caller's organisation posts
// letters to: where its buyers' printed letters may go.
//
// GET /api/admin/orgs/me/ship-countries
func (h *Handlers) GetMyShipCountries(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	var list string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT array_to_string(ship_countries, ',') FROM organizations WHERE id = $1`, *me.OrgID).Scan(&list); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"countries": strings.Split(list, ",")})
}

// SetMyShipCountries sets them: {"countries": ["SE", "NO"]}, at least
// one ISO 3166-1 alpha-2 code. Org admins only.
//
// PUT /api/admin/orgs/me/ship-countries
func (h *Handlers) SetMyShipCountries(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil || me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "only an organisation's admins choose where it posts letters")
		return
	}
	var req struct {
		Countries []string `json:"countries"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	var countries []string
	for _, c := range req.Countries {
		c = strings.ToUpper(strings.TrimSpace(c))
		if !countryRe.MatchString(c) {
			httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "not a country code: " + c, "reason": "invalid_country"})
			return
		}
		if !slices.Contains(countries, c) {
			countries = append(countries, c)
		}
	}
	if len(countries) == 0 || len(countries) > 50 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "post to between 1 and 50 countries", "reason": "invalid_country"})
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE organizations SET ship_countries = string_to_array($2, ',') WHERE id = $1`, *me.OrgID, strings.Join(countries, ",")); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"countries": countries})
}
