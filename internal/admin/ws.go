package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/play"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/ws"
)

// accessRecheck is how often a running watch re-checks that its user
// may still see the event, in nanoseconds. Atomic because a test may
// change it while an earlier test's socket is still winding down.
var accessRecheck atomic.Int64

func init() { accessRecheck.Store(int64(30 * time.Second)) }

// SetWatchAccessRecheck sets how often a running watch re-checks access
// and returns the previous interval; 0 checks on every push. For tests.
func SetWatchAccessRecheck(d time.Duration) time.Duration {
	return time.Duration(accessRecheck.Swap(int64(d)))
}

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

	// The session behind this socket, re-checked while a watch runs.
	var sessionToken string
	if c, err := r.Cookie(auth.CookieName); err == nil {
		sessionToken = c.Value
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

		// Access was checked once above, against the user as of connect.
		// Re-check it now and then so a user who is removed from the org,
		// switches org or logs out stops receiving pushes rather than
		// streaming until the socket reconnects. A failed lookup keeps
		// the watch; the next check retries.
		checked := time.Now()
		allowed := func() bool {
			if time.Since(checked) < time.Duration(accessRecheck.Load()) {
				return true
			}
			checked = time.Now()
			u, ok, err := auth.Lookup(wctx, h.DB, sessionToken)
			if err != nil {
				return true
			}
			return ok && sameOrg(u, orgID)
		}

		push := func() {
			if !allowed() {
				conn.SendKind("watch_error", map[string]any{"error": "forbidden"})
				cancel()
				unsub()
				return
			}
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
		// Every location ping publishes on the event topic; rebuild at
		// most once a second rather than once per publish.
		go ws.Coalesce(wctx, events, time.Second, push)
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

// eventDetailDTO is the event + teams payload: GET /events/{id} and the
// watch_event push both serve it, so the page renders one shape.
type eventDetailDTO struct {
	Event eventDTO             `json:"event"`
	Teams []eventDetailTeamDTO `json:"teams"`
	// ServerNow lets the console run its clocks on server time, so a
	// phone whose clock is off doesn't show a wrong elapsed time.
	ServerNow string `json:"server_now"`
	// Graph is set for an event that plays a graph: what its teams'
	// steps are called, and where its zones are.
	Graph *eventGraphDTO `json:"graph,omitempty"`
}

// eventGraphDTO is what the console needs of an event's graph to show it:
// its puzzle count, each step's title for the activity feed and team
// rows, and its zones for the map.
type eventGraphDTO struct {
	PuzzleCount int               `json:"puzzle_count"`
	Titles      map[string]string `json:"titles"`
	Zones       []graph.Zone      `json:"zones"`
}

type eventDetailTeamDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Email is the captain-contact address — arranger-side only,
	// never echoed onto the public team-share page.
	Email       string `json:"email,omitempty"`
	PageIndex   int    `json:"page_index"`
	CompletedAt string `json:"completed_at,omitempty"`
	WithdrawnAt string `json:"withdrawn_at,omitempty"`
	Token       string `json:"token"`
	// RecapToken matches the HTTP EventDetail field — surfaced on
	// every state push so the post-completion view can link to
	// each team's recap page without a follow-up fetch.
	RecapToken string                 `json:"recap_token,omitempty"`
	AvatarURL  string                 `json:"avatar_url,omitempty"`
	Members    []eventDetailMemberDTO `json:"members"`
	// LastActivityAt is the team's newest activity row. Race control
	// reads it to spot a stuck team; the feed it used to derive this
	// from is capped, so the quietest teams fell off it.
	LastActivityAt string `json:"last_activity_at,omitempty"`
	// A graph event's team: points and place (play.Ahead; level teams
	// share it), the step it is on, and how it ended. page_index is a
	// lane cursor and means nothing there.
	Score   *float64 `json:"score,omitempty"`
	Rank    int      `json:"rank,omitempty"`
	Node    string   `json:"node,omitempty"`
	Outcome string   `json:"outcome,omitempty"`
}

type eventDetailMemberDTO struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Token       string `json:"token"`
	LastLoginAt string `json:"last_login_at,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// buildEventDetail assembles the event-with-teams-and-slots payload, for
// the HTTP EventDetail handler and the watch_event push alike.
func (h *Handlers) buildEventDetail(ctx context.Context, eventID int64) (eventDetailDTO, error) {
	var (
		d          eventDTO
		showMap    int
		priceCents sql.NullInt64
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
		        WHERE t.event_id = ar.id) AS player_slots_count,
		       ar.public_for_sale, ar.ticket_price_cents,
		       ar.ticket_currency, ar.ticket_team_size, ar.public_blurb,
		       (ar.cover_image_mime_type IS NOT NULL) AS has_cover_image,
		       ar.paused_seconds, COALESCE(ar.paused_at, '')
		FROM events ar
		LEFT JOIN adventure_assignments aa ON aa.id = ar.assignment_id
		WHERE ar.id = $1
	`, eventID).Scan(&d.ID, &d.Name, &d.AdventureSlug, &d.State, &d.ScheduledAt, &d.StartedAt,
		&d.CompletedAt, &d.CancelledAt,
		&d.OrgID, &d.CreatedByUserID, &d.AssignmentID, &d.MaxPlayers,
		&d.LiveToken, &showMap, &d.TeamsCount, &d.PlayerSlotsCount,
		&d.PublicForSale, &priceCents, &d.TicketCurrency, &d.TicketTeamSize, &d.PublicBlurb,
		&d.HasCoverImage, &d.PausedSeconds, &d.PausedAt); err != nil {
		return eventDetailDTO{}, err
	}
	if priceCents.Valid {
		d.TicketPriceCents = int(priceCents.Int64)
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
	// One query for every team's slots, not one per team: this runs on
	// every arranger push.
	teamIdx := make(map[int64]int, len(teams))
	for i := range teams {
		teamIdx[teams[i].ID] = i
	}
	mrows, err := h.DB.QueryContext(ctx, `
		SELECT ps.team_id, ps.id, ps.name, ps.token,
		       COALESCE(ps.last_login_at, ''), COALESCE(ps.expires_at, '')
		FROM player_slots ps JOIN teams t ON t.id = ps.team_id
		WHERE t.event_id = $1 ORDER BY ps.team_id, LOWER(ps.name)
	`, eventID)
	if err != nil {
		return eventDetailDTO{}, err
	}
	for mrows.Next() {
		var teamID int64
		var s eventDetailMemberDTO
		if err := mrows.Scan(&teamID, &s.ID, &s.Name, &s.Token, &s.LastLoginAt, &s.ExpiresAt); err != nil {
			mrows.Close()
			return eventDetailDTO{}, err
		}
		if i, ok := teamIdx[teamID]; ok {
			teams[i].Members = append(teams[i].Members, s)
		}
	}
	mrows.Close()
	arows, err := h.DB.QueryContext(ctx, `
		SELECT team_id, MAX(created_at) FROM event_activity
		WHERE event_id = $1 AND team_id IS NOT NULL GROUP BY team_id
	`, eventID)
	if err != nil {
		return eventDetailDTO{}, err
	}
	for arows.Next() {
		var teamID int64
		var at string
		if err := arows.Scan(&teamID, &at); err != nil {
			arows.Close()
			return eventDetailDTO{}, err
		}
		if i, ok := teamIdx[teamID]; ok {
			teams[i].LastActivityAt = at
		}
	}
	arows.Close()
	d.TeamsCount = len(teams)
	out := eventDetailDTO{Event: d, Teams: teams, ServerNow: time.Now().UTC().Format(time.RFC3339)}
	uses, err := play.UsesGraph(ctx, h.DB, eventID)
	if err != nil {
		return eventDetailDTO{}, err
	}
	out.Event.Engine = "lanes"
	if uses {
		out.Event.Engine = "graph"
		if err := h.addGraphDetail(ctx, &out); err != nil {
			return eventDetailDTO{}, err
		}
	}
	return out, nil
}

// addGraphDetail fills in what a graph event's page shows instead of lane
// progress: each team's points, place, step and outcome, and the graph's
// step titles and zones.
func (h *Handlers) addGraphDetail(ctx context.Context, d *eventDetailDTO) error {
	var graphID sql.NullInt64
	if err := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE(e.graph_id, a.published_graph_id)
		FROM events e JOIN adventures a ON a.slug = e.adventure_slug WHERE e.id = $1
	`, d.Event.ID).Scan(&graphID); err != nil {
		return err
	}
	gd := &eventGraphDTO{Titles: map[string]string{}, Zones: []graph.Zone{}}
	d.Graph = gd
	if !graphID.Valid {
		return nil
	}
	st, err := graphstore.ByID(ctx, h.DB, graphID.Int64)
	if err != nil {
		return err
	}
	for _, n := range st.Graph.Nodes {
		title := n.Title
		if title == "" {
			title = n.Slug
		}
		gd.Titles[n.Slug] = title
		if n.Type == graph.Puzzle {
			gd.PuzzleCount++
		}
	}
	if st.Graph.Zones != nil {
		gd.Zones = st.Graph.Zones
	}

	e := h.Engine
	if e == nil {
		e = play.New(h.DB)
	}
	ranked, err := e.EventRanking(ctx, d.Event.ID)
	if err != nil {
		return err
	}
	byTeam := map[int64]play.Ranked{}
	for _, r := range ranked {
		byTeam[r.TeamID] = r
	}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT team_id, COALESCE(current_node, ''), COALESCE(outcome, '')
		FROM team_graph_state WHERE event_id = $1`, d.Event.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type where struct{ node, outcome string }
	at := map[int64]where{}
	for rows.Next() {
		var id int64
		var w where
		if err := rows.Scan(&id, &w.node, &w.outcome); err != nil {
			return err
		}
		at[id] = w
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range d.Teams {
		t := &d.Teams[i]
		if w, ok := at[t.ID]; ok {
			t.Node, t.Outcome = w.node, w.outcome
		}
		r, ok := byTeam[t.ID]
		if !ok {
			continue
		}
		score := r.Score
		t.Score = &score
		t.Rank = 1
		for _, o := range ranked {
			if play.Ahead(o, r) {
				t.Rank++
			}
		}
	}
	return nil
}
