package admin

import (
	"context"
	"database/sql"
	"errors"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/mdx"
)

/* ---------------- User typeahead search ---------------- */

type userSearchMatch struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// SearchUsers powers the typeahead in the Organizations page's "Add
// user" modal. Returns up to 10 username matches for a prefix query,
// excluding global admins (they don't fit the per-org role model) and
// optionally users who are already members of a target org. Global
// admin only: an org admin finds an existing account solely by its
// exact email address in CreateOrgMember, which doesn't say whether
// it exists — any lookup here would tell them which usernames and
// addresses on the instance have accounts.
//
// Username is a CITEXT column so LIKE is case-insensitive without
// extra ILIKE / LOWER() decoration. The pattern is ESCAPE-quoted so
// a query like "10%_off" doesn't accidentally turn into a wildcard
// fishing expedition.
func (h *Handlers) SearchUsers(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleAdmin {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	// Minimum length 2 — nudges callers toward intentional lookups
	// rather than slow-typing a wildcard scan of the user table.
	if len(q) < 2 {
		httpx.JSON(w, http.StatusOK, map[string]any{"users": []userSearchMatch{}})
		return
	}
	escape := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)
	pattern := escape.Replace(q) + "%"
	match := `u.username LIKE $1 ESCAPE '\'`

	var orgID int64
	if v := r.URL.Query().Get("org_id"); v != "" {
		orgID, _ = strconv.ParseInt(v, 10, 64)
	}

	var (
		rows *sql.Rows
		err  error
	)
	if orgID > 0 {
		rows, err = h.DB.QueryContext(r.Context(), `
			SELECT u.id, u.username FROM users u
			WHERE u.is_admin = FALSE
			  AND `+match+`
			  AND NOT EXISTS (
			    SELECT 1 FROM org_memberships m WHERE m.user_id = u.id AND m.org_id = $2
			  )
			ORDER BY LOWER(u.username) LIMIT 10
		`, pattern, orgID)
	} else {
		rows, err = h.DB.QueryContext(r.Context(), `
			SELECT u.id, u.username FROM users u
			WHERE u.is_admin = FALSE AND `+match+`
			ORDER BY LOWER(u.username) LIMIT 10
		`, pattern)
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "search failed")
		return
	}
	defer rows.Close()
	out := []userSearchMatch{}
	for rows.Next() {
		var u userSearchMatch
		if err := rows.Scan(&u.ID, &u.Username); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "scan failed")
			return
		}
		out = append(out, u)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"users": out})
}

// orgAdminMayManageCredentials reports whether an org_admin of orgID may
// reset the password / clear the TOTP of user targetID. The bar is
// deliberately higher than plain org membership: the account must have
// been CREATED by this org (CreateOrgMember's new-user flow) AND must
// not have since joined any other org. That keeps credential control
// with the org that provisioned a service account, and refuses it for
// self-signup logins or accounts shared with another org — the cases a
// malicious org_admin would otherwise exploit to take over a login the
// person uses elsewhere. Self-service password reset (the forgot-password
// email) and the global admin remain the recovery paths for those.
func orgAdminMayManageCredentials(ctx context.Context, db *sql.DB, orgID, targetID int64) (bool, error) {
	var createdBy sql.NullInt64
	if err := db.QueryRowContext(ctx,
		`SELECT created_by_org_id FROM users WHERE id = $1`, targetID).Scan(&createdBy); err != nil {
		return false, err
	}
	if !createdBy.Valid || createdBy.Int64 != orgID {
		return false, nil
	}
	var otherOrgs int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM org_memberships WHERE user_id = $1 AND org_id <> $2`,
		targetID, orgID).Scan(&otherOrgs); err != nil {
		return false, err
	}
	return otherOrgs == 0, nil
}

/* ---------------- Confirm-password gating ---------------- */

const confirmHeader = "X-Confirm-Password"

type confirmedKey struct{}

// WithConfirmed marks an in-process call as confirmed by the user in
// place of re-entering the password: the MCP tools pass it on an explicit
// confirm: true, so a token never has to carry the account password. Only
// Go code can set it; an HTTP request cannot.
func WithConfirmed(ctx context.Context) context.Context {
	return context.WithValue(ctx, confirmedKey{}, true)
}

func (h *Handlers) requirePasswordConfirm(w http.ResponseWriter, r *http.Request) bool {
	if ok, _ := r.Context().Value(confirmedKey{}).(bool); ok {
		return true
	}
	me, _ := auth.FromContext(r.Context())
	pw := r.Header.Get(confirmHeader)
	if pw == "" {
		httpx.Error(w, http.StatusUnauthorized, "password confirmation required")
		return false
	}
	// Throttle per (IP, user): without this, a session that doesn't know
	// the password can brute-force it through any sensitive-action
	// endpoint, and each miss burns an Argon2id verification. Keyed by
	// user ID (not username) so it tracks the account being reauthed.
	ipKey := auth.ClientIP(r)
	userKey := "uid:" + strconv.FormatInt(me.ID, 10)
	if h.ConfirmLimiter != nil {
		if ok, retry := h.ConfirmLimiter.Allow(ipKey, userKey); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(max(int(retry.Seconds()), 1)))
			httpx.Error(w, http.StatusTooManyRequests, "too many attempts, try again later")
			return false
		}
	}
	if err := auth.VerifyUserPassword(r.Context(), h.DB, me.ID, pw); err != nil {
		if h.ConfirmLimiter != nil {
			h.ConfirmLimiter.RecordFailure(ipKey, userKey)
		}
		httpx.Error(w, http.StatusUnauthorized, "wrong password")
		return false
	}
	if h.ConfirmLimiter != nil {
		h.ConfirmLimiter.RecordSuccess(ipKey, userKey)
	}
	return true
}

/* ---------------- Adventure & run detail ---------------- */

// AdventureDetail returns the page list for an adventure. Powers both
// the catalog "view this adventure" page and the in-event adventure
// summary strip — so it carries everything the layout needs:
// intro/outro copy, per-page prompt_html / images / hints,
// permission flags (mine_to_edit), and running_event_count.
//
// Visibility mirrors ListAdventures: a slug visible there is fetchable
// here, anything else is 404 (don't leak existence of foreign-org
// drafts). Within the visible set, the answer key (pages[].accept) and
// hint bodies (pages[].hints[].text_html / .images / .audio) are
// stripped for non-editor callers — i.e. anyone who isn't an admin or
// a member of the owning org. This is the gate against a lobby /
// foreign-org account browsing the catalog and reading the answer key
// out of the JSON response. Editors keep the full payload because
// race directors legitimately need to see accepts to help stuck teams
// during a live event.
//
// Field shape is a map[string]any rather than a typed struct
// because the frontend already keys off literal strings; a future
// pass can collapse this to a typed adventureDetailDTO when we
// settle on a stable surface.
func (h *Handlers) AdventureDetail(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	me, _ := auth.FromContext(r.Context())

	// One DB lookup serves both the visibility gate and the metadata
	// fields the response carries. Run BEFORE Lib.Get so a 404 doesn't
	// leak via a different error path. The previous version of this
	// handler discarded this query's error and proceeded to leak full
	// content on any signed-up session.
	var (
		hasAvatar    int
		publishedInt int
		ownerOrg     sql.NullInt64
		language     string
		runningN     int
		isTrail      bool
		theme        string
		cover        int64
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT (avatar_mime_type IS NOT NULL)::int, published, org_id, language,
		       (SELECT COUNT(*) FROM events WHERE adventure_slug = $1 AND state = 'running' AND NOT dev),
		       a.trail_cards IS NOT NULL, `+lookCoverSQL("a")+`
		FROM adventures a WHERE a.slug = $1
	`, slug).Scan(&hasAvatar, &publishedInt, &ownerOrg, &language, &runningN, &isTrail, &theme, &cover)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	published := publishedInt != 0

	if !h.staffCanSeeAdventure(r.Context(), me, ownerOrg, published, slug) {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}

	adv := h.Lib.Get(slug)
	if adv == nil {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	out := map[string]any{
		"slug":        adv.Slug,
		"title":       adv.Meta.Title,
		"description": adv.Meta.Description,
		"author":      adv.Meta.Author,
		"intro_text":  adv.Meta.IntroText,
		"outro_text":  adv.Meta.OutroText,
	}
	// Pre-render markdown to HTML so the inspect page can render
	// formatting (same goldmark pipeline puzzle prompts use). Empty
	// strings are skipped so the JSON shape stays clean for
	// adventures with no intro/outro authored.
	if adv.Meta.IntroText != "" {
		out["intro_html"] = mdx.Render(adv.Meta.IntroText)
	}
	if adv.Meta.OutroText != "" {
		out["outro_html"] = mdx.Render(adv.Meta.OutroText)
	}
	if !adv.Colors.Empty() {
		out["colors"] = adv.Colors
	}
	if hasAvatar != 0 {
		out["avatar_url"] = adventureAvatarURL(slug)
	}
	if theme != "" {
		out["theme"] = theme
	}
	if cover != 0 {
		out["cover"] = cover
	}
	out["published"] = published
	out["running_event_count"] = runningN
	// A quiz trail is edited in the simple creator (trails.go).
	out["trail"] = isTrail
	out["language"] = language
	if ownerOrg.Valid {
		out["org_id"] = ownerOrg.Int64
	}
	// mine_to_edit is the canonical flag the UI keys off for
	// "show Edit, hide Buy" decisions. Tracks canAuthor (stricter than
	// canEdit) so org_member sees the row but doesn't see edit
	// affordances — the role can still run events that use the
	// adventure, just not modify the puzzle content.
	out["mine_to_edit"] = canAuthor(me, ownerOrg)
	// Starting location surfaces on the catalog detail page (a "Start
	// here" pill with the label and a map link) and on the player's
	// intro overlay. Both coords need to be set together; callers
	// fall back to the first gated puzzle if either is nil.
	if adv.Start.HasCoords() {
		out["start_lat"] = *adv.Start.Lat
		out["start_lng"] = *adv.Start.Lng
		if adv.Start.RadiusM != nil {
			out["start_radius_m"] = *adv.Start.RadiusM
		}
	}
	if adv.Start.Label != "" {
		out["start_label"] = adv.Start.Label
	}
	summary, err := publishedGraphSummary(r.Context(), h.DB, adv.ID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading graph failed", err)
		return
	}
	if summary != nil {
		out["graph"] = summary
	}
	httpx.JSON(w, http.StatusOK, out)
}

// graphSummary is what the console shows of a published graph: the steps
// a team sees, in graph order, and the zones they happen in.
type graphSummary struct {
	Steps       []graphSummaryStep `json:"steps"`
	PuzzleCount int                `json:"puzzle_count"`
	StoryCount  int                `json:"story_count"`
	Zones       []graph.Zone       `json:"zones"`
	HasQuiz     bool               `json:"has_quiz,omitempty"`
	// Languages are the main language and its translations, absent
	// without any (graph.Languages).
	Languages []string `json:"languages,omitempty"`
}

type graphSummaryStep struct {
	Slug  string     `json:"slug"`
	Type  graph.Type `json:"type"`
	Title string     `json:"title,omitempty"`
	Zone  string     `json:"zone,omitempty"`
	// What the step carries, for the catalog's "at a glance": its
	// pictures, its sounds and films, and a puzzle's hints.
	Pictures int `json:"pictures,omitempty"`
	Sounds   int `json:"sounds,omitempty"`
	Hints    int `json:"hints,omitempty"`
}

// publishedGraphSummary summarises an adventure's published graph, or
// returns nil for an adventure without one.
func publishedGraphSummary(ctx context.Context, db *sql.DB, advID int64) (*graphSummary, error) {
	st, err := graphstore.Published(ctx, db, advID)
	if errors.Is(err, graphstore.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := &graphSummary{Steps: []graphSummaryStep{}, Zones: []graph.Zone{}, Languages: st.Graph.Languages()}
	if st.Graph.Zones != nil {
		s.Zones = st.Graph.Zones
	}
	for _, n := range st.Graph.Nodes {
		step := graphSummaryStep{Slug: n.Slug, Type: n.Type, Title: n.Title}
		switch n.Type {
		case graph.Puzzle:
			s.PuzzleCount++
			if n.PuzzleCfg != nil {
				step.Zone = n.PuzzleCfg.Zone
				step.Hints = len(n.PuzzleCfg.Hints)
			}
		case graph.Story:
			s.StoryCount++
			if n.StoryCfg != nil {
				step.Zone = n.StoryCfg.Zone
			}
		case graph.Hub, graph.End:
		default:
			// Starts, gates, switches and effects happen without the
			// team seeing a step.
			continue
		}
		for _, b := range n.Blocks() {
			s.HasQuiz = s.HasQuiz || b.Kind == graph.BlockQuiz
		}
		for _, m := range graph.MediaOf(n.Blocks()) {
			if m.Kind == graph.MediaImage {
				step.Pictures++
			} else {
				step.Sounds++
			}
		}
		s.Steps = append(s.Steps, step)
	}
	return s, nil
}

// EventDetail returns an event with its teams and slots (players).
func (h *Handlers) EventDetail(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	d, err := h.buildEventDetail(r.Context(), eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

// userIDFrom remains for admin/org user URL params (admins list, delete).
func userIDFrom(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// ResetPassword clears or replaces a staff user's password. Players don't
// have passwords any more (they log in with magic-link tokens).
func (h *Handlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if r.ContentLength > 0 {
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
	}
	me, _ := auth.FromContext(r.Context())
	var targetIsAdmin bool
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT is_admin FROM users WHERE id = $1`, id).Scan(&targetIsAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	switch {
	case me.Role == auth.RoleAdmin:
		if targetIsAdmin && me.ID != id {
			httpx.Error(w, http.StatusForbidden, "admin-on-admin reset is not supported")
			return
		}
	case me.ID == id:
		// Resetting your own password through this endpoint still proves
		// the current password: a stolen cookie must not be enough to
		// rotate the password and lock the owner out. (/me/password is
		// the primary self-service path; this case keeps parity.)
		if !h.requirePasswordConfirm(w, r) {
			return
		}
	default:
		// org_admin can reset an account their own org created and that
		// hasn't since joined another org — see
		// orgAdminMayManageCredentials. Caller must have an active org
		// and be org_admin in it.
		if me.OrgID == nil || me.Role != auth.RoleOrgAdmin {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		ok, mErr := orgAdminMayManageCredentials(r.Context(), h.DB, *me.OrgID, id)
		if mErr != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if !ok {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
	}
	if req.Password == "" {
		httpx.Error(w, http.StatusBadRequest, "password required")
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
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE users SET password_hash = $1, password_is_default = 0, password_set_by_admin = 1 WHERE id = $2`,
		hash, id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id = $1`, id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "session cleanup failed")
		return
	}
	// Revoke the target's API/MCP tokens: a password reset is often a
	// recovery from compromise, and a token minted beforehand must not
	// survive it.
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM mcp_tokens WHERE user_id = $1`, id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "token cleanup failed")
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// AdminDisableUserTOTP factory-resets another user's TOTP enrolment.
// Used when a user has lost both their authenticator and every
// recovery code — they have to ask an admin to clear the second
// factor before they can log in with just the password again.
//
// Authorization mirrors ResetPassword exactly:
//   - global admin can reset any non-admin (and themselves)
//   - org_admin can reset members of their own active org
//   - anyone can reset their own
//
// Reauth: X-Confirm-Password header. The acting admin's password,
// not the target's. Audit-loud — both actor and target are recorded.
func (h *Handlers) AdminDisableUserTOTP(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad user id")
		return
	}
	me, _ := auth.FromContext(r.Context())
	var (
		targetIsAdmin  bool
		targetUsername string
	)
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT is_admin, username FROM users WHERE id = $1`, id).Scan(&targetIsAdmin, &targetUsername)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	switch {
	case me.IsAdmin:
		if targetIsAdmin && me.ID != id {
			httpx.Error(w, http.StatusForbidden, "admin-on-admin reset is not supported")
			return
		}
	case me.ID == id:
		// Self-disable already lives at /me/totp/disable; reaching this
		// endpoint for self is fine, just unusual. Allow it.
	default:
		if me.OrgID == nil || me.Role != auth.RoleOrgAdmin {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		ok, mErr := orgAdminMayManageCredentials(r.Context(), h.DB, *me.OrgID, id)
		if mErr != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if !ok {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
	}
	if !h.requirePasswordConfirm(w, r) {
		return
	}
	if err := auth.DisableTOTP(r.Context(), h.DB, id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "disable failed")
		return
	}
	// Admin-driven TOTP reset is the "user lost their phone" recovery
	// flow. Force the affected user's other sessions out so any
	// leftover authenticated browser is required to re-auth — at the
	// new (weaker) factor level, which is exactly the right posture
	// after a lost-device reset. Unlike the user-driven path, there's
	// no "current session for that user" to preserve from this admin
	// caller's request, so drop everything.
	_, _ = h.DB.ExecContext(r.Context(),
		`DELETE FROM sessions WHERE user_id = $1`, id)
	// Also revoke the target's API/MCP tokens — this is the lost-device
	// recovery path, so nothing minted under the old factor should live on.
	_, _ = h.DB.ExecContext(r.Context(),
		`DELETE FROM mcp_tokens WHERE user_id = $1`, id)
	auth.Record(r.Context(), h.DB, auth.AuditEvent{
		Event: auth.EventTOTPAdminReset, OK: true, UserID: &me.ID, Username: targetUsername,
		IP: auth.ClientIP(r), UserAgent: auth.UserAgent(r), Reason: auth.ReasonTOTPAdminReset,
	})
	httpx.JSON(w, http.StatusNoContent, nil)
}
