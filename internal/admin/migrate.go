package admin

// HTTP surface for the self-host → hosted migration flow. Three
// endpoints, all RoleAdmin-gated at the route level:
//
//   GET  /api/admin/migrate/export?org_id=N
//        Streams the org-bundle zip for org N.
//   POST /api/admin/migrate/import
//        Accepts a multipart upload (field name "bundle") and replays
//        the bundle into a fresh org on this instance. Returns the
//        new org id + an import summary.
//   POST /api/admin/migrate/redirect-mode
//        Body {"target": "<url>"} flips redirect mode. Empty target
//        disables. Audit-logged.
//
// The whole flow is also reachable from the GUI page on
// arrange.html (#migrate). Keeping the GUI on top of the same
// HTTP API means scripted operators get the same semantics.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/settings"
)

// MigrateSelfExport is the org-admin counterpart to MigrateExport:
// the caller is org-scoped (RoleOrgAdmin) and gets back the bundle
// for *their own* org, never any other. Provided so customers can
// extract their data without going through platform support — the
// "right to portability" surface, not the operator surface.
//
// `org_id` is read from the caller's session (auth.User.OrgID), so
// the handler refuses anonymous + admin (admin has no org context;
// they should use MigrateExport instead) + RoleOrgMember (the
// bundle carries answer keys, ticket buyer emails, and slot tokens
// — too sensitive for org_member). Mirrors the operator's surface
// otherwise: streams a zip with the same Content-Disposition shape.
//
// Snapshot consistency: this path does NOT gate on maintenance mode
// because it's per-org on a possibly-multi-tenant instance. A
// concurrent player answering a puzzle during an export can produce
// a slightly inconsistent snapshot — typically harmless for the
// adventure / event / team rows but observable on `event_activity`
// timestamps. A future round wraps this path in a REPEATABLE READ
// transaction; today's recommendation in the GUI copy is to export
// during a quiet window.
func (h *Handlers) MigrateSelfExport(w http.ResponseWriter, r *http.Request) {
	me, ok := auth.FromContext(r.Context())
	if !ok || me.Role != auth.RoleOrgAdmin || me.OrgID == nil {
		// 403 covers RoleOrgMember + RoleLobby + RoleAdmin (admin's
		// path is the operator-surface MigrateExport which takes
		// org_id explicitly).
		httpx.Error(w, http.StatusForbidden, "self-export is for org admins only")
		return
	}
	orgID := *me.OrgID

	env, files, err := h.ExportOrgEnvelope(r.Context(), orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "export failed", err)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="zonventure-org-%s-%s.zip"`,
		sanitiseFilename(env.Org.Name), time.Now().UTC().Format("2006-01-02")))
	if err := WriteOrgBundle(w, env, files); err != nil {
		logx.Warn("admin.migrate.self_export.write", "org_id", orgID, "by", me.Username, "err", err.Error())
	}
	logx.Info("admin.migrate.self_export", "org_id", orgID, "by", me.Username)
}

// MigrateExport streams the org-bundle zip. Auth: RoleAdmin at the
// route level. Query-param `org_id` selects which org; required
// because admins are not org-scoped (org_admin / org_member would
// be, but they don't reach this endpoint).
//
// The whole-instance operator surface refuses to run unless
// maintenance mode is on, because concurrent writes during the
// export would otherwise tear the snapshot — a player answering a
// puzzle mid-export would see their team row scanned at one
// progression state but ticket purchases / activity scanned at a
// later state. The org-admin self-export (`MigrateSelfExport`)
// doesn't have this gate because freezing the whole platform
// because one tenant is exporting would be wrong; that path
// relies on REPEATABLE READ for snapshot consistency.
func (h *Handlers) MigrateExport(w http.ResponseWriter, r *http.Request) {
	if !settings.MaintenanceMode() {
		httpx.Error(w, http.StatusConflict,
			"enable maintenance mode before exporting — concurrent writes would tear the snapshot. POST /api/admin/migrate/maintenance with {\"on\": true}, then retry.")
		return
	}
	idStr := strings.TrimSpace(r.URL.Query().Get("org_id"))
	if idStr == "" {
		httpx.Error(w, http.StatusBadRequest, "org_id query param required")
		return
	}
	orgID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || orgID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad org_id")
		return
	}

	env, files, err := h.ExportOrgEnvelope(r.Context(), orgID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "export failed", err)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="zonventure-org-%s-%s.zip"`,
		sanitiseFilename(env.Org.Name), time.Now().UTC().Format("2006-01-02")))
	if err := WriteOrgBundle(w, env, files); err != nil {
		// Headers are already sent so we can't switch to a JSON 500;
		// log + bail. The client will see a truncated zip and notice.
		logx.Warn("admin.migrate.export.write", "org_id", orgID, "err", err.Error())
	}
}

// MigrateImport accepts the bundle multipart upload and creates a
// fresh org with everything replayed under new primary keys but
// preserved tokens. RoleAdmin at the route level.
func (h *Handlers) MigrateImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxOrgImportZipBytes+(2<<20))
	if err := r.ParseMultipartForm(maxOrgImportZipBytes + (1 << 20)); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad multipart upload")
		return
	}
	f, hdr, err := r.FormFile("bundle")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bundle field required")
		return
	}
	defer f.Close()
	if hdr.Size > maxOrgImportZipBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "bundle too large")
		return
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, io.LimitReader(f, maxOrgImportZipBytes+1)); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read upload")
		return
	}
	if int64(buf.Len()) > maxOrgImportZipBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "bundle too large")
		return
	}

	summary, err := ImportOrgBundle(r.Context(), h.DB, buf.Bytes())
	if err != nil {
		var ie *importError
		if errors.As(err, &ie) {
			httpx.Error(w, ie.status, ie.msg)
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "import failed", err)
		return
	}
	me, _ := auth.FromContext(r.Context())
	logx.Info("admin.migrate.import", "org_id", summary.OrgID, "by", me.Username,
		"adventures", summary.Adventures, "events", summary.Events,
		"teams", summary.Teams, "slots", summary.Slots, "purchases", summary.Purchases)
	httpx.JSON(w, http.StatusOK, summary)
}

// RedirectModeRequest mirrors the JSON shape the toggle endpoint
// accepts. Empty Target disables redirect mode.
type RedirectModeRequest struct {
	Target string `json:"target"`
}

// RedirectModeStatus is the response shape: current target + whether
// the env var has pinned it (in which case the GUI disables the
// toggle and shows the operator why).
type RedirectModeStatus struct {
	Target      string `json:"target"`
	PinnedByEnv bool   `json:"pinned_by_env"`
}

// MigrateRedirectMode flips the cutover redirect target on or off.
// On success returns the new status. Refuses with 409 when the env
// var has pinned the value.
func (h *Handlers) MigrateRedirectMode(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		httpx.JSON(w, http.StatusOK, RedirectModeStatus{
			Target:      settings.RedirectTarget(),
			PinnedByEnv: settings.RedirectPinnedByEnv(),
		})
		return
	}
	var req RedirectModeRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	target := strings.TrimSpace(req.Target)
	if target != "" {
		// Validate: must be a syntactically-sensible https/http URL
		// and end up routable. We don't try to verify the destination
		// is alive — that's the operator's responsibility — but we do
		// reject obvious typos before they brick the instance.
		if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
			httpx.Error(w, http.StatusBadRequest, "target must start with http:// or https://")
			return
		}
	}
	if err := settings.SetRedirectTarget(r.Context(), h.DB, target); err != nil {
		// Env-pinned: 409 so the GUI shows a clear "the operator
		// pinned this in the env file; unset it there to manage
		// from here" message.
		if strings.Contains(err.Error(), "pinned by env") {
			httpx.Error(w, http.StatusConflict, err.Error())
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "save redirect target", err)
		return
	}
	me, _ := auth.FromContext(r.Context())
	state := "enabled"
	if target == "" {
		state = "disabled"
	}
	logx.Info("admin.migrate.redirect_mode", "state", state, "target", target, "by", me.Username)
	httpx.JSON(w, http.StatusOK, RedirectModeStatus{
		Target:      settings.RedirectTarget(),
		PinnedByEnv: settings.RedirectPinnedByEnv(),
	})
}

// MaintenanceModeRequest mirrors the JSON shape the toggle accepts.
type MaintenanceModeRequest struct {
	On bool `json:"on"`
}

// MaintenanceModeStatus is the response shape: current flag + whether
// the env var pinned it. The GUI surfaces both so a self-host
// operator who pinned the value via .env doesn't waste time
// wondering why the toggle has no effect.
type MaintenanceModeStatus struct {
	On          bool `json:"on"`
	PinnedByEnv bool `json:"pinned_by_env"`
}

// MigrateMaintenance is the operator-side toggle for read-only
// maintenance mode. RoleAdmin at the route level. Pinning via
// ZV_MAINTENANCE_MODE in the env file disables the toggle (the
// 409 response surfaces that). When ON, the maintenance middleware
// 503s every non-allowlisted mutation; the migrate toggles + admin
// auth paths stay open so the operator can run the export and turn
// the flag back off.
func (h *Handlers) MigrateMaintenance(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		httpx.JSON(w, http.StatusOK, MaintenanceModeStatus{
			On:          settings.MaintenanceMode(),
			PinnedByEnv: settings.MaintenancePinnedByEnv(),
		})
		return
	}
	var req MaintenanceModeRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := settings.SetMaintenanceMode(r.Context(), h.DB, req.On); err != nil {
		if strings.Contains(err.Error(), "pinned by env") {
			httpx.Error(w, http.StatusConflict, err.Error())
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "save maintenance flag", err)
		return
	}
	me, _ := auth.FromContext(r.Context())
	state := "off"
	if req.On {
		state = "on"
	}
	logx.Info("admin.migrate.maintenance", "state", state, "by", me.Username)
	httpx.JSON(w, http.StatusOK, MaintenanceModeStatus{
		On:          settings.MaintenanceMode(),
		PinnedByEnv: settings.MaintenancePinnedByEnv(),
	})
}

// EncodeOrgManifestJSON is a small helper used by the CLI fallback
// (cmd/server/migrate.go) to dump the manifest portion of a bundle
// without round-tripping through the zip writer. Lives here so the
// types it touches stay package-private.
func EncodeOrgManifestJSON(env orgExportEnvelope) ([]byte, error) {
	return json.MarshalIndent(env, "", "  ")
}
