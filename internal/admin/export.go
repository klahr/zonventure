package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
)

// ExportMyData serves a single JSON dump of everything the caller has
// on the platform. Two scopes:
//
//   - Personal (always): user row + active staff sessions + auth_events
//     for them.
//   - Organization (org_admin / org_member only): org row, owned
//     adventures, assignments, purchases, runs (with team + slot
//     summaries; full ping/answer rows are linked by run id but
//     bundled inline so the file is self-contained).
//
// Global admins have no org affinity, so they only see the personal
// section. The endpoint is synchronous — at our scale a single
// org's data fits comfortably in a JSON response. If exports start
// hitting megabytes we'll move to "email me the link" + S3 later.
func (h *Handlers) ExportMyData(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())

	out := exportPayload{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		User: userExport{
			ID: me.ID, Username: me.Username, Role: me.Role,
			Email: me.Email, EmailVerified: me.EmailVerified, OrgID: me.OrgID,
		},
	}

	if err := loadPersonalAuthEvents(r.Context(), h.DB, me.ID, &out); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "audit export failed: "+err.Error())
		return
	}
	if err := loadActiveSessions(r.Context(), h.DB, me.ID, &out); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "session export failed: "+err.Error())
		return
	}
	if me.OrgID != nil {
		if err := loadOrgExport(r.Context(), h.DB, *me.OrgID, &out); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "org export failed: "+err.Error())
			return
		}
	}

	// Headers prompt the browser to download instead of render. The
	// filename includes the user id and date so multiple exports
	// don't overwrite each other on the user's disk.
	filename := fmt.Sprintf("zv-export-user%d-%s.json", me.ID, time.Now().UTC().Format("20060102"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// ---- export shapes ----

type exportPayload struct {
	GeneratedAt    string             `json:"generated_at"`
	User           userExport         `json:"user"`
	AuthEvents     []authEventExport  `json:"auth_events,omitempty"`
	ActiveSessions []sessionExport    `json:"active_sessions,omitempty"`
	Organization   *orgExport         `json:"organization,omitempty"`
	Adventures     []adventureExport  `json:"adventures,omitempty"`
	Assignments    []assignmentExport `json:"assignments,omitempty"`
	Purchases      []purchaseExport   `json:"purchases,omitempty"`
	Runs           []runExport        `json:"runs,omitempty"`
}

type userExport struct {
	ID            int64  `json:"id"`
	Username      string `json:"username"`
	Role          string `json:"role"`
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified"`
	OrgID         *int64 `json:"org_id,omitempty"`
}

type authEventExport struct {
	TS       string `json:"ts"`
	Event    string `json:"event"`
	OK       bool   `json:"ok"`
	Username string `json:"username,omitempty"`
	IP       string `json:"ip,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type sessionExport struct {
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
}

type orgExport struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type adventureExport struct {
	ID          int64  `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	Published   bool   `json:"published"`
	CreatedAt   string `json:"created_at"`
}

type assignmentExport struct {
	ID             int64  `json:"id"`
	AdventureSlug  string `json:"adventure_slug"`
	MaxTeams       int    `json:"max_teams"`
	CreatedAt      string `json:"created_at"`
}

type purchaseExport struct {
	ID             int64  `json:"id"`
	AdventureSlug  string `json:"adventure_slug"`
	MaxTeams       int    `json:"max_teams"`
	AmountCents    int    `json:"amount_cents"`
	Currency       string `json:"currency"`
	State          string `json:"state"`
	Provider       string `json:"provider"`
	CreatedAt      string `json:"created_at"`
	PaidAt         string `json:"paid_at,omitempty"`
}

type runExport struct {
	ID            int64        `json:"id"`
	Name          string       `json:"name,omitempty"`
	AdventureSlug string       `json:"adventure_slug"`
	State         string       `json:"state"`
	StartedAt     string       `json:"started_at,omitempty"`
	CompletedAt   string       `json:"completed_at,omitempty"`
	CreatedAt     string       `json:"created_at"`
	Teams         []teamExport `json:"teams,omitempty"`
}

type teamExport struct {
	ID               int64        `json:"id"`
	Name             string       `json:"name"`
	CurrentPageIndex int          `json:"current_page_index"`
	CompletedAt      string       `json:"completed_at,omitempty"`
	CreatedAt        string       `json:"created_at"`
	Members          []slotExport `json:"members,omitempty"`
}

type slotExport struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	LastLoginAt string `json:"last_login_at,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// ---- loaders ----

func loadPersonalAuthEvents(ctx context.Context, db *sql.DB, userID int64, out *exportPayload) error {
	rows, err := db.QueryContext(ctx, `
		SELECT ts, event, (ok <> 0), COALESCE(username,''), COALESCE(ip,''), COALESCE(reason,'')
		FROM auth_events
		WHERE user_id = $1
		ORDER BY id DESC
		LIMIT 1000
	`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var e authEventExport
		if err := rows.Scan(&e.TS, &e.Event, &e.OK, &e.Username, &e.IP, &e.Reason); err != nil {
			return err
		}
		out.AuthEvents = append(out.AuthEvents, e)
	}
	return rows.Err()
}

func loadActiveSessions(ctx context.Context, db *sql.DB, userID int64, out *exportPayload) error {
	rows, err := db.QueryContext(ctx,
		`SELECT created_at, expires_at FROM sessions WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s sessionExport
		if err := rows.Scan(&s.CreatedAt, &s.ExpiresAt); err != nil {
			return err
		}
		out.ActiveSessions = append(out.ActiveSessions, s)
	}
	return rows.Err()
}

func loadOrgExport(ctx context.Context, db *sql.DB, orgID int64, out *exportPayload) error {
	var o orgExport
	if err := db.QueryRowContext(ctx,
		`SELECT id, name, created_at FROM organizations WHERE id = $1`, orgID).Scan(&o.ID, &o.Name, &o.CreatedAt); err != nil {
		return err
	}
	out.Organization = &o

	if err := loadOrgAdventures(ctx, db, orgID, out); err != nil {
		return err
	}
	if err := loadOrgAssignments(ctx, db, orgID, out); err != nil {
		return err
	}
	if err := loadOrgPurchases(ctx, db, orgID, out); err != nil {
		return err
	}
	return loadOrgRuns(ctx, db, orgID, out)
}

func loadOrgAdventures(ctx context.Context, db *sql.DB, orgID int64, out *exportPayload) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, slug, title, description, author, (published <> 0), created_at
		FROM adventures WHERE org_id = $1 ORDER BY id
	`, orgID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a adventureExport
		if err := rows.Scan(&a.ID, &a.Slug, &a.Title, &a.Description, &a.Author, &a.Published, &a.CreatedAt); err != nil {
			return err
		}
		out.Adventures = append(out.Adventures, a)
	}
	return rows.Err()
}

func loadOrgAssignments(ctx context.Context, db *sql.DB, orgID int64, out *exportPayload) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, adventure_slug, max_teams, created_at
		FROM adventure_assignments WHERE org_id = $1 ORDER BY id
	`, orgID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a assignmentExport
		if err := rows.Scan(&a.ID, &a.AdventureSlug, &a.MaxTeams, &a.CreatedAt); err != nil {
			return err
		}
		out.Assignments = append(out.Assignments, a)
	}
	return rows.Err()
}

func loadOrgPurchases(ctx context.Context, db *sql.DB, orgID int64, out *exportPayload) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, adventure_slug, max_teams, amount_cents, currency, state, provider,
		       created_at, COALESCE(paid_at, '')
		FROM purchases WHERE org_id = $1 ORDER BY id
	`, orgID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p purchaseExport
		if err := rows.Scan(&p.ID, &p.AdventureSlug, &p.MaxTeams, &p.AmountCents,
			&p.Currency, &p.State, &p.Provider, &p.CreatedAt, &p.PaidAt); err != nil {
			return err
		}
		out.Purchases = append(out.Purchases, p)
	}
	return rows.Err()
}

func loadOrgRuns(ctx context.Context, db *sql.DB, orgID int64, out *exportPayload) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, adventure_slug, state,
		       COALESCE(started_at,''), COALESCE(completed_at,''), created_at
		FROM adventure_runs WHERE org_id = $1 ORDER BY id
	`, orgID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var runs []runExport
	for rows.Next() {
		var ru runExport
		if err := rows.Scan(&ru.ID, &ru.Name, &ru.AdventureSlug, &ru.State,
			&ru.StartedAt, &ru.CompletedAt, &ru.CreatedAt); err != nil {
			return err
		}
		runs = append(runs, ru)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range runs {
		teams, err := loadRunTeams(ctx, db, runs[i].ID)
		if err != nil {
			return fmt.Errorf("run %d teams: %w", runs[i].ID, err)
		}
		runs[i].Teams = teams
	}
	out.Runs = runs
	return nil
}

func loadRunTeams(ctx context.Context, db *sql.DB, runID int64) ([]teamExport, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, current_page_index, COALESCE(completed_at,''), created_at
		FROM teams WHERE run_id = $1 ORDER BY id
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var teams []teamExport
	for rows.Next() {
		var tm teamExport
		if err := rows.Scan(&tm.ID, &tm.Name, &tm.CurrentPageIndex, &tm.CompletedAt, &tm.CreatedAt); err != nil {
			return nil, err
		}
		teams = append(teams, tm)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range teams {
		mems, err := loadTeamSlots(ctx, db, teams[i].ID)
		if err != nil {
			return nil, fmt.Errorf("team %d slots: %w", teams[i].ID, err)
		}
		teams[i].Members = mems
	}
	return teams, nil
}

func loadTeamSlots(ctx context.Context, db *sql.DB, teamID int64) ([]slotExport, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, COALESCE(last_login_at,''), COALESCE(expires_at,'')
		FROM player_slots WHERE team_id = $1 ORDER BY id
	`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []slotExport
	for rows.Next() {
		var s slotExport
		if err := rows.Scan(&s.ID, &s.Name, &s.LastLoginAt, &s.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

