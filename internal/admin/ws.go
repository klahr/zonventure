package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/play"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/domains"
	"github.com/klahr/zonventure/internal/listing"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/ws"
)

// liveSnapshotAge is the oldest a shared live view may be when sent.
const liveSnapshotAge = time.Second

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
//
//	client → {"kind":"watch_adventure","data":{"slug":"…"}}  the designer
//	server → {"kind":"graph_saved","data":{saved_at, by, user_id, assistant, touched, note, kept}}
//	                                                       its draft was saved; kept {user_id, steps}:
//	                                                       whose steps it overwrote after a conflict
//
// Whatever is watched, the watcher is present on it (ws.Peer):
//
//	server → {"kind":"presence_self","data":{"id":"c12"}}   this socket's peer
//	server → {"kind":"presence","data":{peers:[…]}}         who is here
//	client → {"kind":"focus","data":{"node":"…"}}           what this peer has open
//	client → {"kind":"pointer","data":{…}}                  relayed to the others
//	server → {"kind":"pointer","data":{"id":"c12","at":{…}}} as {id, at: what was sent}
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
		// liveTopic is what this socket is present on as livePeer, ""
		// when nothing. Each watch is a peer of its own, so one winding
		// down never takes the next one's presence with it.
		liveTopic   string
		livePeer    string
		lastPointer time.Time
	)

	// joinLive makes this socket present on topic and forwards the
	// topic's presence and pointers to it, until wctx ends.
	joinLive := func(wctx context.Context, topic string) {
		peerID := ws.NewPeerID()
		live, unsub := h.Hub.Subscribe(ws.LiveTopic(topic))
		conn.SendKind("presence_self", map[string]any{"id": peerID})
		leave := h.Hub.Join(topic, ws.Peer{ID: peerID, UserID: me.ID, Name: me.Username, Kind: ws.PeerHuman, Avatar: auth.AvatarURLs(wctx, h.DB, []int64{me.ID})[me.ID]})
		mu.Lock()
		liveTopic, livePeer = topic, peerID
		mu.Unlock()
		go func() {
			defer logx.Guard("admin.ws.live")
			defer func() {
				leave()
				unsub()
				mu.Lock()
				if livePeer == peerID {
					liveTopic, livePeer = "", ""
				}
				mu.Unlock()
			}()
			for {
				select {
				case <-wctx.Done():
					return
				case raw, ok := <-live:
					if !ok {
						return
					}
					conn.Send(raw)
				}
			}
		}()
	}
	present := func() (string, string) {
		mu.Lock()
		defer mu.Unlock()
		return liveTopic, livePeer
	}
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
		if !sameOrg(me, orgID) || !runsEvents(me) {
			conn.SendKind("watch_error", map[string]any{"error": "forbidden"})
			return
		}

		stopWatch()

		// Pings and changes to the event wake apart: a ping moves positions
		// and tracks only, so it no longer rebuilds the event's detail and
		// activity for every watcher once a second.
		eventTopic, pingTopic := ws.EventTopic(eventID), ws.PositionsTopic(eventID)
		changes, unsubChanges := h.Hub.Subscribe(eventTopic)
		pings, unsubPings := h.Hub.Subscribe(pingTopic)
		unsub := func() {
			unsubChanges()
			unsubPings()
		}
		wctx, cancel := context.WithCancel(conn.Context())
		mu.Lock()
		cancelWatch = func() {
			cancel()
			unsub()
		}
		mu.Unlock()
		joinLive(wctx, eventTopic)

		// Access was checked once above, against the user as of connect.
		// Re-check it now and then so a user who is removed from the org,
		// switches org or logs out stops receiving pushes rather than
		// streaming until the socket reconnects. A failed lookup keeps
		// the watch; the next check retries.
		var checkMu sync.Mutex
		checked := time.Now()
		allowed := func() bool {
			checkMu.Lock()
			defer checkMu.Unlock()
			if time.Since(checked) < time.Duration(accessRecheck.Load()) {
				return true
			}
			checked = time.Now()
			u, ok, err := auth.Lookup(wctx, h.DB, sessionToken)
			if err != nil {
				return true
			}
			return ok && sameOrg(u, orgID) && runsEvents(u)
		}

		// Each view is built once per event and shared by every watcher
		// (h.live): a snapshot built after the topic's latest publish is
		// current, and none is older than a second — the detail carries
		// the server's clock.
		send := func(view string, after time.Time, build func(ctx context.Context) (string, any, error)) {
			frame, err := h.live.get(view+":"+strconv.FormatInt(eventID, 10), latest(after, time.Now().Add(-liveSnapshotAge)), func() ([]byte, error) {
				// Not this socket's context: others wait on the build.
				kind, data, err := build(context.WithoutCancel(wctx))
				if err != nil {
					return nil, err
				}
				return ws.Frame(kind, data)
			})
			if err == nil {
				conn.Send(frame)
			}
		}
		refuse := func() bool {
			if allowed() {
				return false
			}
			conn.SendKind("watch_error", map[string]any{"error": "forbidden"})
			cancel()
			unsub()
			return true
		}
		pushPositions := func() {
			if refuse() {
				return
			}
			moved := latest(h.Hub.LastPublish(pingTopic), h.Hub.LastPublish(eventTopic))
			send("positions", moved, func(ctx context.Context) (string, any, error) {
				pos, err := h.buildPositions(ctx, eventID)
				return "positions", map[string]any{"positions": pos}, err
			})
			send("tracks", moved, func(ctx context.Context) (string, any, error) {
				tr, err := h.buildTracks(ctx, eventID)
				return "tracks", map[string]any{"tracks": tr}, err
			})
		}
		pushAll := func() {
			pushPositions()
			if wctx.Err() != nil {
				return
			}
			changed := h.Hub.LastPublish(eventTopic)
			send("activity", changed, func(ctx context.Context) (string, any, error) {
				ev, err := h.buildActivity(ctx, eventID, nil)
				return "activity", map[string]any{"activity": ev}, err
			})
			send("event_detail", changed, func(ctx context.Context) (string, any, error) {
				rd, err := h.buildEventDetail(ctx, eventID)
				return "event_detail", rd, err
			})
		}

		pushAll()
		// Rebuild at most once a second rather than once per publish.
		go ws.Coalesce(wctx, changes, time.Second, pushAll)
		go ws.Coalesce(wctx, pings, time.Second, pushPositions)
	}

	// The designer watches its adventure's draft: each save is forwarded
	// as it happens, so it can load what an assistant changed.
	startAdventureWatch := func(slug string) {
		advID, _, err := AuthorizeAdventureEdit(conn.Context(), h.DB, slug, me)
		if err != nil {
			conn.SendKind("watch_error", map[string]any{"error": "forbidden"})
			return
		}
		stopWatch()
		saves, unsub := h.Hub.Subscribe(ws.AdventureTopic(advID))
		wctx, cancel := context.WithCancel(conn.Context())
		mu.Lock()
		cancelWatch = func() {
			cancel()
			unsub()
		}
		mu.Unlock()
		joinLive(wctx, ws.AdventureTopic(advID))
		go func() {
			defer logx.Guard("admin.ws.saves")
			checked := time.Now()
			for {
				select {
				case <-wctx.Done():
					return
				case raw, ok := <-saves:
					if !ok {
						return
					}
					// As for events: stop pushing to someone who lost access.
					if time.Since(checked) >= time.Duration(accessRecheck.Load()) {
						checked = time.Now()
						if u, ok, err := auth.Lookup(wctx, h.DB, sessionToken); err == nil {
							if _, _, err := AuthorizeAdventureEdit(wctx, h.DB, slug, u); !ok || err != nil {
								conn.SendKind("watch_error", map[string]any{"error": "forbidden"})
								cancel()
								unsub()
								return
							}
						}
					}
					conn.Send(raw)
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
		case "watch_adventure":
			var req struct {
				Slug string `json:"slug"`
			}
			if err := json.Unmarshal(msg.Data, &req); err != nil || req.Slug == "" {
				continue
			}
			startAdventureWatch(req.Slug)
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
		case "focus":
			var req struct {
				Node string `json:"node"`
			}
			if err := json.Unmarshal(msg.Data, &req); err != nil || len(req.Node) > 200 {
				continue
			}
			if topic, peer := present(); topic != "" {
				h.Hub.Focus(topic, peer, req.Node)
			}
		case "pointer":
			// Relayed, never kept; at most 25 a second per socket.
			topic, peer := present()
			if topic == "" || len(msg.Data) > 512 || time.Since(lastPointer) < 40*time.Millisecond {
				continue
			}
			lastPointer = time.Now()
			h.Hub.Publish(ws.LiveTopic(topic), "pointer", map[string]any{"id": peer, "at": msg.Data})
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
// rows, its zones for the map, and the steps a team can be moved past.
type eventGraphDTO struct {
	PuzzleCount int               `json:"puzzle_count"`
	Titles      map[string]string `json:"titles"`
	Zones       []graph.Zone      `json:"zones"`
	Rescuable   []string          `json:"rescuable"`
}

type eventDetailTeamDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Email is the captain-contact address — arranger-side only,
	// never echoed onto the public team-share page.
	Email       string `json:"email,omitempty"`
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
	// The team's points and place (play.Ahead; level teams share it),
	// the step it is on, and how it ended.
	Score   *float64 `json:"score,omitempty"`
	Rank    int      `json:"rank,omitempty"`
	Node    string   `json:"node,omitempty"`
	Outcome string   `json:"outcome,omitempty"`
	// Cheated says the team used the adventure's cheat password; it has
	// no rank.
	Cheated bool `json:"cheated,omitempty"`
}

type eventDetailMemberDTO struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Token       string `json:"token"`
	LastLoginAt string `json:"last_login_at,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// eventGraph is the graph an event plays: the one it was started on, or,
// before it starts, its adventure's published one.
func (h *Handlers) eventGraph(ctx context.Context, slug string, graphID int64) (*graph.Graph, error) {
	if graphID != 0 {
		st, err := graphstore.ByID(ctx, h.DB, graphID)
		return st.Graph, err
	}
	var adventureID int64
	if err := h.DB.QueryRowContext(ctx, `SELECT id FROM adventures WHERE slug = $1`, slug).Scan(&adventureID); err != nil {
		return nil, err
	}
	st, err := graphstore.Published(ctx, h.DB, adventureID)
	return st.Graph, err
}

// teamNameOrder lists teams by name with their numbers in order:
// "Team 2" before "Team 10", which plain ORDER BY t.name puts first.
const teamNameOrder = `regexp_replace(t.name, '\d+', '', 'g'), length(substring(t.name from '\d+')), t.name`

// buildEventDetail assembles the event-with-teams-and-slots payload, for
// the HTTP EventDetail handler and the watch_event push alike.
func (h *Handlers) buildEventDetail(ctx context.Context, eventID int64) (eventDetailDTO, error) {
	var (
		d           eventDTO
		showMap     int
		priceCents  sql.NullInt64
		secretRoles bool
		graphID     int64
		connect     bool
		listingTr   []byte
		mainLang    string
		translated  []byte
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT ar.id, ar.name, ar.adventure_slug, COALESCE(adv.title, ''), ar.state,
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
		       ar.paused_seconds, COALESCE(ar.paused_at, ''),
		       ar.secret_roles, COALESCE(ar.graph_id, 0), ar.party,
		       COALESCE(ar.listing_review, ''), ar.listing_reject_reason,
		       l.event_id IS NOT NULL, COALESCE(l.price_cents, 0), COALESCE(l.currency, ''),
		       COALESCE(o.stripe_connect_charges_enabled, FALSE),
		       ar.sales_stopped,
		       COALESCE(adv.cheat_password, ''), ar.meeting_place,
		       ar.listing_translations, `+listing.VersionLanguages("gv", "adv")+`
		FROM events ar
		LEFT JOIN organizations o ON o.id = ar.org_id
		LEFT JOIN event_listings l ON l.event_id = ar.id
		LEFT JOIN adventure_assignments aa ON aa.id = ar.assignment_id
		LEFT JOIN adventures adv ON adv.slug = ar.adventure_slug
		LEFT JOIN adventure_graphs gv ON gv.id = COALESCE(ar.next_graph_id, ar.graph_id)
		WHERE ar.id = $1
	`, eventID).Scan(&d.ID, &d.Name, &d.AdventureSlug, &d.AdventureTitle, &d.State, &d.ScheduledAt, &d.StartedAt,
		&d.CompletedAt, &d.CancelledAt,
		&d.OrgID, &d.CreatedByUserID, &d.AssignmentID, &d.MaxPlayers,
		&d.LiveToken, &showMap, &d.TeamsCount, &d.PlayerSlotsCount,
		&d.PublicForSale, &priceCents, &d.TicketCurrency, &d.TicketTeamSize, &d.PublicBlurb,
		&d.HasCoverImage, &d.PausedSeconds, &d.PausedAt, &secretRoles, &graphID, &d.Party,
		&d.ListingReview, &d.ListingRejectReason, &d.Listed, &d.ListedPriceCents, &d.ListedCurrency, &connect,
		&d.SalesStopped, &d.CheatPassword, &d.MeetingPlace,
		&listingTr, &mainLang, &translated); err != nil {
		return eventDetailDTO{}, err
	}
	if err := json.Unmarshal(listingTr, &d.ListingTranslations); err != nil {
		return eventDetailDTO{}, err
	}
	var langs []string
	if err := json.Unmarshal(translated, &langs); err != nil {
		return eventDetailDTO{}, err
	}
	d.Languages = listing.Languages(mainLang, langs)
	if d.PublicForSale && d.Listed {
		d.StoreBlocked = h.storeBlocked(connect)
	}
	if d.PublicForSale {
		d.EmbedBase = domains.BaseForEvent(ctx, h.DB, d.ID, h.PublicURL)
	}
	// Only an adventure with a saboteur has the setting to show.
	if g, err := h.eventGraph(ctx, d.AdventureSlug, graphID); err == nil && len(g.SecretRoles()) > 0 {
		d.HasSecretRoles, d.SecretRoles = true, &secretRoles
	}
	if priceCents.Valid {
		d.TicketPriceCents = int(priceCents.Int64)
	}
	d.ShowMap = showMap != 0
	d.AllowedOps = allowedOpsForState(d.State, d.Party)

	teams := []eventDetailTeamDTO{}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name, COALESCE(t.email, ''),
		       COALESCE(t.completed_at, ''),
		       COALESCE(t.withdrawn_at, ''),
		       t.token, COALESCE(t.recap_token, ''),
		       (t.avatar_mime_type IS NOT NULL)::int AS has_avatar
		FROM teams t WHERE t.event_id = $1 ORDER BY `+teamNameOrder+`
	`, eventID)
	if err != nil {
		return eventDetailDTO{}, err
	}
	defer rows.Close()
	for rows.Next() {
		t := eventDetailTeamDTO{Members: []eventDetailMemberDTO{}}
		var hasAvatar int
		if err := rows.Scan(&t.ID, &t.Name, &t.Email, &t.CompletedAt, &t.WithdrawnAt, &t.Token, &t.RecapToken, &hasAvatar); err != nil {
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
	if err := h.addGraphDetail(ctx, &out); err != nil {
		return eventDetailDTO{}, err
	}
	return out, nil
}

// addGraphDetail fills in each team's points, place, step and outcome,
// and the graph's step titles and zones.
func (h *Handlers) addGraphDetail(ctx context.Context, d *eventDetailDTO) error {
	var graphID sql.NullInt64
	if err := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE(e.graph_id, a.published_graph_id)
		FROM events e JOIN adventures a ON a.slug = e.adventure_slug WHERE e.id = $1
	`, d.Event.ID).Scan(&graphID); err != nil {
		return err
	}
	gd := &eventGraphDTO{Titles: map[string]string{}, Zones: []graph.Zone{}, Rescuable: []string{}}
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
		if session.CanRescue(n) {
			gd.Rescuable = append(gd.Rescuable, n.Slug)
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
		SELECT team_id, COALESCE(current_node, ''), COALESCE(outcome, ''), cheated
		FROM team_graph_state WHERE event_id = $1`, d.Event.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type where struct {
		node, outcome string
		cheated       bool
	}
	at := map[int64]where{}
	for rows.Next() {
		var id int64
		var w where
		if err := rows.Scan(&id, &w.node, &w.outcome, &w.cheated); err != nil {
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
			t.Node, t.Outcome, t.Cheated = w.node, w.outcome, w.cheated
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
