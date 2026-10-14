package admin

import (
	"context"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/settings"
)

// orgContactDTO is how an organisation is reached and which clock it
// keeps. ContactEmail is the address it set, "" for DefaultContactEmail
// (its first admin's); TimeZone is an IANA name.
type orgContactDTO struct {
	ContactEmail        string `json:"contact_email"`
	DefaultContactEmail string `json:"default_contact_email"`
	TimeZone            string `json:"time_zone"`
}

// GetMyOrgContact is the caller's organisation's contact address and
// time zone.
//
// GET /api/admin/orgs/me/contact
func (h *Handlers) GetMyOrgContact(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	h.writeOrgContact(w, r, *me.OrgID)
}

func (h *Handlers) writeOrgContact(w http.ResponseWriter, r *http.Request, orgID int64) {
	var out orgContactDTO
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT o.contact_email, COALESCE(`+emailtpl.AdminEmailSQL+`, ''), o.time_zone
		FROM organizations o WHERE o.id = $1
	`, orgID).Scan(&out.ContactEmail, &out.DefaultContactEmail, &out.TimeZone); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	out.TimeZone = settings.OrgTimeZone(out.TimeZone)
	httpx.JSON(w, http.StatusOK, out)
}

// SetMyOrgContact sets them: {"contact_email": "", "time_zone":
// "Europe/Stockholm"}; an empty address goes back to the first admin's.
// Org admins only.
//
// PUT /api/admin/orgs/me/contact
func (h *Handlers) SetMyOrgContact(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil || me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "only an organisation's admins change how it is contacted")
		return
	}
	var req struct {
		ContactEmail string `json:"contact_email"`
		TimeZone     string `json:"time_zone"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	contact := strings.ToLower(strings.TrimSpace(req.ContactEmail))
	if contact != "" {
		if a, err := mail.ParseAddress(contact); err != nil || a.Address != contact || len(contact) > 254 {
			httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "not a valid email address", "reason": "invalid_email"})
			return
		}
	}
	tz := strings.TrimSpace(req.TimeZone)
	if _, err := time.LoadLocation(tz); err != nil || tz == "" || tz == "Local" {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "unknown time zone", "reason": "invalid_time_zone"})
		return
	}
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE organizations SET contact_email = $2, time_zone = $3 WHERE id = $1`, *me.OrgID, contact, tz); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving failed", err)
		return
	}
	h.writeOrgContact(w, r, *me.OrgID)
}

// orgContact is an organisation's contact address (emailtpl.ContactSQL),
// "" when it has none.
func (h *Handlers) orgContact(ctx context.Context, orgID int64) string {
	var contact string
	_ = h.DB.QueryRowContext(ctx, `SELECT `+emailtpl.ContactSQL+` FROM organizations o WHERE o.id = $1`, orgID).Scan(&contact)
	return contact
}

// orgTimeZone is the time zone an organisation writes times in.
func (h *Handlers) orgTimeZone(ctx context.Context, orgID int64) string {
	var tz string
	_ = h.DB.QueryRowContext(ctx, `SELECT time_zone FROM organizations WHERE id = $1`, orgID).Scan(&tz)
	return tz
}
