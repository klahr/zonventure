package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/ws"
)

// WSArrange is the arranger-side subscription stream. Mutations still flow
// over HTTP because WebSockets don't buy anything for command/response on a
// desktop admin UI; what this stream does is push the changing parts of a
// run-detail view (positions, events, tracks, run metadata, teams) so the
// arranger sees the same real-time state the live TV does without polling.
//
// Protocol:
//
//	client → {"kind":"watch_run",  "data":{"run_id": N}}   start watching
//	client → {"kind":"unwatch_run"}                         stop watching
//	server → {"kind":"positions", "data":{positions:[…]}}   on change
//	server → {"kind":"tracks",    "data":{tracks:[…]}}      on change
//	server → {"kind":"events",    "data":{events:[…]}}      on change
//	server → {"kind":"run",       "data":{…}}               on change
//	server → {"kind":"watch_error","data":{"error":"…"}}    auth failure
func (h *Handlers) WSArrange(w http.ResponseWriter, r *http.Request) {
	me, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := ws.Accept(w, r)
	if err != nil {
		return
	}
	defer conn.Close()

	// Only one watch is active at a time per connection; switching runs
	// cancels the previous subscriber goroutine.
	var (
		mu          sync.Mutex
		cancelWatch context.CancelFunc
	)
	stopWatch := func() {
		mu.Lock()
		if cancelWatch != nil {
			cancelWatch()
			cancelWatch = nil
		}
		mu.Unlock()
	}
	defer stopWatch()

	startWatch := func(runID int64) {
		// Verify the caller can read this run before we open a topic subscription.
		var orgID int64
		err := h.DB.QueryRowContext(conn.Context(),
			`SELECT org_id FROM adventure_runs WHERE id = $1`, runID).Scan(&orgID)
		if errors.Is(err, sql.ErrNoRows) {
			conn.SendKind("watch_error", map[string]any{"error": "run not found"})
			return
		}
		if err != nil {
			conn.SendKind("watch_error", map[string]any{"error": "lookup failed"})
			return
		}
		if !sameOrg(me, orgID) {
			conn.SendKind("watch_error", map[string]any{"error": "forbidden"})
			return
		}

		stopWatch()

		events, unsub := h.Hub.Subscribe(ws.RunTopic(runID))
		wctx, cancel := context.WithCancel(conn.Context())
		mu.Lock()
		cancelWatch = func() {
			cancel()
			unsub()
		}
		mu.Unlock()

		push := func() {
			if pos, err := h.buildPositions(wctx, runID); err == nil {
				conn.SendKind("positions", map[string]any{"positions": pos})
			}
			if tr, err := h.buildTracks(wctx, runID); err == nil {
				conn.SendKind("tracks", map[string]any{"tracks": tr})
			}
			if ev, err := h.buildEvents(wctx, runID, nil); err == nil {
				conn.SendKind("events", map[string]any{"events": ev})
			}
			if rd, err := h.buildRunDetail(wctx, runID); err == nil {
				conn.SendKind("run_detail", rd)
			}
		}

		push()
		go func() {
			for {
				select {
				case <-wctx.Done():
					return
				case _, ok := <-events:
					if !ok {
						return
					}
					push()
				}
			}
		}()
	}

	for {
		msg, err := conn.Read()
		if err != nil {
			return
		}
		switch msg.Kind {
		case "watch_run":
			var req struct {
				RunID int64 `json:"run_id"`
			}
			if err := json.Unmarshal(msg.Data, &req); err != nil || req.RunID <= 0 {
				continue
			}
			startWatch(req.RunID)
		case "unwatch_run":
			stopWatch()
		}
	}
}

// runDetailDTO is the run + teams payload served on watch_run. Mirrors the
// existing HTTP RunDetail JSON shape so the client can reuse the same
// rendering code.
type runDetailDTO struct {
	Run   runDTO             `json:"run"`
	Teams []runDetailTeamDTO `json:"teams"`
}

type runDetailTeamDTO struct {
	ID          int64                `json:"id"`
	Name        string               `json:"name"`
	PageIndex   int                  `json:"page_index"`
	CompletedAt string               `json:"completed_at,omitempty"`
	Token       string               `json:"token"`
	AvatarURL   string               `json:"avatar_url,omitempty"`
	Members     []runDetailMemberDTO `json:"members"`
}

type runDetailMemberDTO struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Token       string `json:"token"`
	LastLoginAt string `json:"last_login_at,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// buildRunDetail assembles the run-with-teams-and-slots payload. The HTTP
// RunDetail handler should also call this, but keeping the two code paths
// side by side for now avoids a larger refactor; the shapes match by design.
func (h *Handlers) buildRunDetail(ctx context.Context, runID int64) (runDetailDTO, error) {
	var (
		d       runDTO
		showMap int
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT ar.id, ar.name, ar.adventure_slug, ar.state,
		       COALESCE(ar.scheduled_at,''), COALESCE(ar.started_at,''),
		       ar.org_id, COALESCE(ar.created_by_user_id, 0),
		       COALESCE(ar.assignment_id, 0), COALESCE(aa.max_teams, 0),
		       ar.live_token, ar.show_map
		FROM adventure_runs ar
		LEFT JOIN adventure_assignments aa ON aa.id = ar.assignment_id
		WHERE ar.id = $1
	`, runID).Scan(&d.ID, &d.Name, &d.AdventureSlug, &d.State, &d.ScheduledAt, &d.StartedAt,
		&d.OrgID, &d.CreatedByUserID, &d.AssignmentID, &d.MaxTeams,
		&d.LiveToken, &showMap); err != nil {
		return runDetailDTO{}, err
	}
	d.ShowMap = showMap != 0

	teams := []runDetailTeamDTO{}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name, t.current_page_index, COALESCE(t.completed_at, ''), t.token,
		       (t.avatar_mime_type IS NOT NULL)::int AS has_avatar
		FROM teams t WHERE t.run_id = $1 ORDER BY t.name
	`, runID)
	if err != nil {
		return runDetailDTO{}, err
	}
	defer rows.Close()
	for rows.Next() {
		t := runDetailTeamDTO{Members: []runDetailMemberDTO{}}
		var hasAvatar int
		if err := rows.Scan(&t.ID, &t.Name, &t.PageIndex, &t.CompletedAt, &t.Token, &hasAvatar); err != nil {
			return runDetailDTO{}, err
		}
		if hasAvatar != 0 {
			t.AvatarURL = teamAvatarURL(t.ID)
		}
		teams = append(teams, t)
	}
	for i := range teams {
		mrows, err := h.DB.QueryContext(ctx, `
			SELECT id, name, token, COALESCE(last_login_at, ''), COALESCE(expires_at, '')
			FROM player_slots WHERE team_id = $1 ORDER BY LOWER(name)
		`, teams[i].ID)
		if err != nil {
			return runDetailDTO{}, err
		}
		for mrows.Next() {
			var s runDetailMemberDTO
			if err := mrows.Scan(&s.ID, &s.Name, &s.Token, &s.LastLoginAt, &s.ExpiresAt); err != nil {
				mrows.Close()
				return runDetailDTO{}, err
			}
			teams[i].Members = append(teams[i].Members, s)
		}
		mrows.Close()
	}
	return runDetailDTO{Run: d, Teams: teams}, nil
}
