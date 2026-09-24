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
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/mdx"
)

/* ---------------- User typeahead search ---------------- */

type userSearchMatch struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// SearchUsers powers the typeahead in the "Add user to org" modal.
// Returns up to 10 username matches for a prefix query, excluding
// global admins (they don't fit the per-org role model) and
// optionally users who are already members of a target org. Auth:
// admin or org_admin, mirroring CreateOrgMember's envelope.
//
// Username is a CITEXT column so LIKE is case-insensitive without
// extra ILIKE / LOWER() decoration. The pattern is ESCAPE-quoted so
// a query like "10%_off" doesn't accidentally turn into a wildcard
// fishing expedition.
func (h *Handlers) SearchUsers(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	if me.Role != auth.RoleAdmin && me.Role != auth.RoleOrgAdmin {
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
			  AND u.username LIKE $1 ESCAPE '\'
			  AND NOT EXISTS (
			    SELECT 1 FROM org_memberships m WHERE m.user_id = u.id AND m.org_id = $2
			  )
			ORDER BY LOWER(u.username) LIMIT 10
		`, pattern, orgID)
	} else {
		rows, err = h.DB.QueryContext(r.Context(), `
			SELECT id, username FROM users
			WHERE is_admin = FALSE AND username LIKE $1 ESCAPE '\'
			ORDER BY LOWER(username) LIMIT 10
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

/* ---------------- Confirm-password gating ---------------- */

const confirmHeader = "X-Confirm-Password"

func requirePasswordConfirm(w http.ResponseWriter, r *http.Request, db *sql.DB) bool {
	me, _ := auth.FromContext(r.Context())
	pw := r.Header.Get(confirmHeader)
	if pw == "" {
		httpx.Error(w, http.StatusUnauthorized, "password confirmation required")
		return false
	}
	if err := auth.VerifyUserPassword(r.Context(), db, me.ID, pw); err != nil {
		httpx.Error(w, http.StatusUnauthorized, "wrong password")
		return false
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
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT (avatar_mime_type IS NOT NULL)::int, published, org_id, language,
		       (SELECT COUNT(*) FROM events WHERE adventure_slug = $1 AND state = 'running')
		FROM adventures WHERE slug = $1
	`, slug).Scan(&hasAvatar, &publishedInt, &ownerOrg, &language, &runningN)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	published := publishedInt != 0

	// canEdit covers any member of the owning org (org_admin
	// and org_member). Race directors are intentionally included so
	// they can read the answer key while running events; the stricter
	// canAuthor check still gates writes.
	canEdit := canEditOwned(me, ownerOrg)
	if !h.staffCanSeeAdventure(r.Context(), me, ownerOrg, published, slug) {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}

	adv := h.Lib.Get(slug)
	if adv == nil {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	type locDTO struct {
		Lat     float64  `json:"lat"`
		Lng     float64  `json:"lng"`
		RadiusM *float64 `json:"radius_m,omitempty"`
	}
	type imageDTO struct {
		ID      int64  `json:"id"`
		URL     string `json:"url"`
		AltText string `json:"alt_text,omitempty"`
	}
	type audioDTO struct {
		ID          int64  `json:"id"`
		URL         string `json:"url"`
		MimeType    string `json:"mime_type,omitempty"`
		Description string `json:"description,omitempty"`
	}
	// Hint shape mirrors the player's hintDTO: TextHTML for inline
	// rendering, DelaySeconds so the new card can show "(unlocks
	// after 2m)" caps. Images per hint travel under the same
	// /adventure-assets/hint-image visibility gate; staff sessions
	// always pass, so the catalog viewer sees them straight away.
	type hintDTO struct {
		TextHTML     string     `json:"text_html"`
		DelaySeconds int        `json:"delay_seconds"`
		Images       []imageDTO `json:"images,omitempty"`
		Audio        []audioDTO `json:"audio,omitempty"`
	}
	type pageDTO struct {
		Index      int                     `json:"index"`
		Title      string                  `json:"title"`
		Prompt     string                  `json:"prompt"`
		PromptHTML string                  `json:"prompt_html,omitempty"`
		Accept     []adventure.AcceptEntry `json:"accept"`
		Location   locDTO                  `json:"location"`
		Gated      bool                    `json:"gated"`
		Images     []imageDTO              `json:"images,omitempty"`
		Audio      []audioDTO              `json:"audio,omitempty"`
		Hints      []hintDTO               `json:"hints,omitempty"`
		Disabled   bool                    `json:"disabled,omitempty"`
		LaneID     int64                   `json:"lane_id,omitempty"`
	}
	pages := make([]pageDTO, len(adv.Pages))
	for i, p := range adv.Pages {
		imgs := make([]imageDTO, 0, len(p.Images))
		for _, im := range p.Images {
			imgs = append(imgs, imageDTO{
				ID: im.ID, AltText: im.AltText,
				URL: "/adventure-assets/image/" + strconv.FormatInt(im.ID, 10),
			})
		}
		audio := make([]audioDTO, 0, len(p.Audio))
		for _, a := range p.Audio {
			audio = append(audio, audioDTO{
				ID: a.ID, MimeType: a.MimeType, Description: a.Description,
				URL: "/adventure-assets/audio/" + strconv.FormatInt(a.ID, 10),
			})
		}
		hints := make([]hintDTO, 0, len(p.Hints))
		for _, hint := range p.Hints {
			himgs := make([]imageDTO, 0, len(hint.Images))
			for _, im := range hint.Images {
				himgs = append(himgs, imageDTO{
					ID: im.ID, AltText: im.AltText,
					URL: "/adventure-assets/hint-image/" + strconv.FormatInt(im.ID, 10),
				})
			}
			haudio := make([]audioDTO, 0, len(hint.Audio))
			for _, a := range hint.Audio {
				haudio = append(haudio, audioDTO{
					ID: a.ID, MimeType: a.MimeType, Description: a.Description,
					URL: "/adventure-assets/hint-audio/" + strconv.FormatInt(a.ID, 10),
				})
			}
			hints = append(hints, hintDTO{
				TextHTML:     mdx.Render(hint.Text),
				DelaySeconds: hint.DelaySeconds,
				Images:       himgs,
				Audio:        haudio,
			})
		}
		pages[i] = pageDTO{
			Index: p.Index, Title: p.Title,
			Prompt:     p.Prompt,
			PromptHTML: mdx.Render(p.Prompt),
			Accept:     p.Accept,
			Location:   locDTO{Lat: p.Location.Lat, Lng: p.Location.Lng, RadiusM: p.Location.RadiusM},
			Gated:      p.Location.Gated(),
			Images:     imgs,
			Audio:      audio,
			Hints:      hints,
			Disabled:   p.Disabled,
			LaneID:     p.LaneID,
		}
	}
	// Strip puzzle solutions for non-editor callers (catalog browsers,
	// foreign org users with an assignment/share, …). canEdit was
	// computed up top from the same query that populated ownerOrg.
	if !canEdit {
		for i := range pages {
			pages[i].Accept = nil
			for j := range pages[i].Hints {
				pages[i].Hints[j].TextHTML = ""
				pages[i].Hints[j].Images = nil
				pages[i].Hints[j].Audio = nil
			}
		}
	}

	out := map[string]any{
		"slug":        adv.Slug,
		"title":       adv.Meta.Title,
		"description": adv.Meta.Description,
		"author":      adv.Meta.Author,
		"intro_text":  adv.Meta.IntroText,
		"outro_text":  adv.Meta.OutroText,
		"page_count":  len(adv.Pages),
		"pages":       pages,
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
	out["published"] = published
	out["running_event_count"] = runningN
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
	// Lanes ride alongside pages so the catalog detail view can
	// visualise the lane structure (header + name + mode) instead
	// of rendering one undifferentiated puzzle list. Single-lane
	// adventures still render flat client-side.
	if lanes, err := loadAdventureLanes(r.Context(), h.DB, adv.ID); err == nil {
		out["lanes"] = lanes
	}
	// Starting location surfaces on the catalog detail page (a "Start
	// here" pill with the label and a map link) and on the player's
	// intro overlay. Both coords need to be set together; callers
	// fall back to the first gated puzzle if either is nil.
	if adv.Start.HasCoords() {
		out["start_lat"] = *adv.Start.Lat
		out["start_lng"] = *adv.Start.Lng
	}
	if adv.Start.Label != "" {
		out["start_label"] = adv.Start.Label
	}
	// An adventure with a published graph plays the graph: its lane
	// pages above are what it was before, never shown to a team again.
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
	s := &graphSummary{Steps: []graphSummaryStep{}, Zones: []graph.Zone{}}
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
	case me.IsAdmin:
		if targetIsAdmin && me.ID != id {
			httpx.Error(w, http.StatusForbidden, "admin-on-admin reset is not supported")
			return
		}
	case me.ID == id:
		// anyone can reset their own
	default:
		// org_admin can reset anyone in their own active org (besides
		// admins). Caller must have an active org and be org_admin in
		// it; target must be a member of that same org.
		if me.OrgID == nil || me.Role != auth.RoleOrgAdmin {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		var found int
		mErr := h.DB.QueryRowContext(r.Context(),
			`SELECT 1 FROM org_memberships WHERE user_id = $1 AND org_id = $2`,
			id, *me.OrgID).Scan(&found)
		if errors.Is(mErr, sql.ErrNoRows) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		if mErr != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
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
		`UPDATE users SET password_hash = $1, password_is_default = 0 WHERE id = $2`,
		hash, id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update failed")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id = $1`, id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "session cleanup failed")
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
		var found int
		mErr := h.DB.QueryRowContext(r.Context(),
			`SELECT 1 FROM org_memberships WHERE user_id = $1 AND org_id = $2`,
			id, *me.OrgID).Scan(&found)
		if errors.Is(mErr, sql.ErrNoRows) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		if mErr != nil {
			httpx.Error(w, http.StatusInternalServerError, "lookup failed")
			return
		}
	}
	if !requirePasswordConfirm(w, r, h.DB) {
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
	auth.Record(r.Context(), h.DB, auth.AuditEvent{
		Event: auth.EventTOTPAdminReset, OK: true, UserID: &me.ID, Username: targetUsername,
		IP: auth.ClientIP(r), UserAgent: auth.UserAgent(r), Reason: auth.ReasonTOTPAdminReset,
	})
	httpx.JSON(w, http.StatusNoContent, nil)
}
