package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/ws"
)

// WSArrange is the arranger-side subscription stream. Mutations still flow
// over HTTP because WebSockets don't buy anything for command/response on a
// desktop admin UI; what this stream does is push the changing parts of a
// event-detail view (positions, events, tracks, event metadata, teams) so the
// arranger sees the same real-time state the live TV does without polling.
//
// Protocol:
//
//	client → {"kind":"watch_event",  "data":{"event_id": N}}   start watching
//	client → {"kind":"unwatch_event"}                         stop watching
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

	// Only one watch is active at a time per connection; switching events
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

	startWatch := func(eventID int64) {
		// Verify the caller can read this event before we open a topic subscription.
		var orgID int64
		err := h.DB.QueryRowContext(conn.Context(),
			`SELECT org_id FROM events WHERE id = $1`, eventID).Scan(&orgID)
		if errors.Is(err, sql.ErrNoRows) {
			conn.SendKind("watch_error", map[string]any{"error": "event not found"})
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

		events, unsub := h.Hub.Subscribe(ws.EventTopic(eventID))
		wctx, cancel := context.WithCancel(conn.Context())
		mu.Lock()
		cancelWatch = func() {
			cancel()
			unsub()
		}
		mu.Unlock()

		push := func() {
			if pos, err := h.buildPositions(wctx, eventID); err == nil {
				conn.SendKind("positions", map[string]any{"positions": pos})
			}
			if tr, err := h.buildTracks(wctx, eventID); err == nil {
				conn.SendKind("tracks", map[string]any{"tracks": tr})
			}
			if ev, err := h.buildActivity(wctx, eventID, nil); err == nil {
				conn.SendKind("activity", map[string]any{"activity": ev})
			}
			if rd, err := h.buildEventDetail(wctx, eventID); err == nil {
				conn.SendKind("event_detail", rd)
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
		case "watch_event":
			var req struct {
				EventID int64 `json:"event_id"`
			}
			if err := json.Unmarshal(msg.Data, &req); err != nil || req.EventID <= 0 {
				continue
			}
			startWatch(req.EventID)
		case "unwatch_event":
			stopWatch()
		default:
			// Unknown frame — ignore but log at debug. No rate-
			// limiter here because admin sessions are already
			// strongly authenticated; this is purely for
			// observability if a misconfigured client floods.
			logx.Debug("admin.ws.unknown_kind", "kind", msg.Kind)
		}
	}
}

// eventDetailDTO is the event + teams payload served on watch_event.
// Mirrors the existing HTTP EventDetail JSON shape so the client can
// reuse the same rendering code.
type eventDetailDTO struct {
	Event eventDTO             `json:"event"`
	Teams []eventDetailTeamDTO `json:"teams"`
}

type eventDetailTeamDTO struct {
	ID          int64                `json:"id"`
	Name        string               `json:"name"`
	// Email is the captain-contact address — arranger-side only,
	// never echoed onto the public team-share page.
	Email       string               `json:"email,omitempty"`
	PageIndex   int                  `json:"page_index"`
	CompletedAt string               `json:"completed_at,omitempty"`
	WithdrawnAt string               `json:"withdrawn_at,omitempty"`
	Token       string               `json:"token"`
	// RecapToken matches the HTTP EventDetail field — surfaced on
	// every state push so the post-completion view can link to
	// each team's recap page without a follow-up fetch.
	RecapToken  string               `json:"recap_token,omitempty"`
	AvatarURL   string               `json:"avatar_url,omitempty"`
	Members     []eventDetailMemberDTO `json:"members"`
}

type eventDetailMemberDTO struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Token       string `json:"token"`
	LastLoginAt string `json:"last_login_at,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// buildEventDetail assembles the event-with-teams-and-slots payload. The HTTP
// EventDetail handler should also call this, but keeping the two code paths
// side by side for now avoids a larger refactor; the shapes match by design.
func (h *Handlers) buildEventDetail(ctx context.Context, eventID int64) (eventDetailDTO, error) {
	var (
		d       eventDTO
		showMap int
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT ar.id, ar.name, ar.adventure_slug, ar.state,
		       COALESCE(ar.scheduled_at,''), COALESCE(ar.started_at,''),
		       COALESCE(ar.completed_at, ''), COALESCE(ar.cancelled_at, ''),
		       ar.org_id, COALESCE(ar.created_by_user_id, 0),
		       COALESCE(ar.assignment_id, 0), COALESCE(aa.max_players, 0),
		       ar.live_token, ar.show_map,
		       (SELECT COUNT(*) FROM teams WHERE event_id = ar.id) AS teams_count,
		       (SELECT COUNT(*) FROM player_slots ps
		         JOIN teams t ON t.id = ps.team_id
		        WHERE t.event_id = ar.id) AS player_slots_count
		FROM events ar
		LEFT JOIN adventure_assignments aa ON aa.id = ar.assignment_id
		WHERE ar.id = $1
	`, eventID).Scan(&d.ID, &d.Name, &d.AdventureSlug, &d.State, &d.ScheduledAt, &d.StartedAt,
		&d.CompletedAt, &d.CancelledAt,
		&d.OrgID, &d.CreatedByUserID, &d.AssignmentID, &d.MaxPlayers,
		&d.LiveToken, &showMap, &d.TeamsCount, &d.PlayerSlotsCount); err != nil {
		return eventDetailDTO{}, err
	}
	d.ShowMap = showMap != 0
	d.AllowedOps = allowedOpsForState(d.State)

	teams := []eventDetailTeamDTO{}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name, COALESCE(t.email, ''),
		       t.current_page_index, COALESCE(t.completed_at, ''),
		       COALESCE(t.withdrawn_at, ''),
		       t.token, COALESCE(t.recap_token, ''),
		       (t.avatar_mime_type IS NOT NULL)::int AS has_avatar
		FROM teams t WHERE t.event_id = $1 ORDER BY t.name
	`, eventID)
	if err != nil {
		return eventDetailDTO{}, err
	}
	defer rows.Close()
	for rows.Next() {
		t := eventDetailTeamDTO{Members: []eventDetailMemberDTO{}}
		var hasAvatar int
		if err := rows.Scan(&t.ID, &t.Name, &t.Email, &t.PageIndex, &t.CompletedAt, &t.WithdrawnAt, &t.Token, &t.RecapToken, &hasAvatar); err != nil {
			return eventDetailDTO{}, err
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
			return eventDetailDTO{}, err
		}
		for mrows.Next() {
			var s eventDetailMemberDTO
			if err := mrows.Scan(&s.ID, &s.Name, &s.Token, &s.LastLoginAt, &s.ExpiresAt); err != nil {
				mrows.Close()
				return eventDetailDTO{}, err
			}
			teams[i].Members = append(teams[i].Members, s)
		}
		mrows.Close()
	}
	d.TeamsCount = len(teams)
	return eventDetailDTO{Event: d, Teams: teams}, nil
}
