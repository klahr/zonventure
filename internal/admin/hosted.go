package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/activity"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/billing"
	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/tokenx"
	"github.com/klahr/zonventure/internal/ws"
)

// Hosted events: a party is a privately hosted event, run from its host
// link, that plays game after game until it lapses. Teams join from one
// player link (/j/{token}) and name themselves; the first to join when
// no game is being played opens the next one, pinning the graph it plays
// (play.PinRound), so what the host changes between games applies from
// the next. "Finish game" — or an idle game, or the end of the party's
// days — closes it and mails the host its summary. See migration 0109.

// maxPartyTeams is the most teams a party's game can be bought up to.
const maxPartyTeams = 150

// HostedPlayPeriod is how long a hosted event plays from its first game.
const HostedPlayPeriod = 14 * 24 * time.Hour

// roundIdle finishes a game nobody has played for this long.
const roundIdle = 3 * time.Hour

func newPlayerToken() (string, error) { return tokenx.New(12) }

// ensurePlayerToken gives an event made before player links one.
func ensurePlayerToken(ctx context.Context, q graphstore.Querier, eventID int64) (string, error) {
	tok, err := newPlayerToken()
	if err != nil {
		return "", err
	}
	var got string
	err = q.QueryRowContext(ctx, `
		UPDATE events SET player_token = COALESCE(player_token, $2) WHERE id = $1 RETURNING player_token
	`, eventID, tok).Scan(&got)
	return got, err
}

// endTeamSessions signs out the phones of the teams cond picks (on
// teams t) and retires their magic links, as the end of an event does.
func endTeamSessions(ctx context.Context, db activity.Execer, cond string, args ...any) error {
	if _, err := db.ExecContext(ctx, `
		UPDATE slot_sessions SET ended = true
		WHERE slot_id IN (SELECT ps.id FROM player_slots ps JOIN teams t ON t.id = ps.team_id WHERE `+cond+`)
	`, args...); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `
		UPDATE player_slots SET expires_at = now_text()
		WHERE team_id IN (SELECT t.id FROM teams t WHERE `+cond+`)
		  AND (expires_at IS NULL OR expires_at = '' OR expires_at > now_text())
	`, args...)
	return err
}

/* ---------------- the player link ---------------- */

// partyByPlayerToken resolves the player link in the URL.
func (h *Handlers) partyByPlayerToken(w http.ResponseWriter, r *http.Request) (hostParty, bool) {
	token := chi.URLParam(r, "token")
	if token == "" || len(token) > 64 {
		httpx.Error(w, http.StatusNotFound, "no such game")
		return hostParty{}, false
	}
	return h.partyWhere(w, r.Context(), "player_token = $1", token)
}

// joinInfoDTO is what the join page shows before a team names itself.
// Status is "open", "full", "not_ready" (the host is still setting up)
// or "ended".
type joinInfoDTO struct {
	Name           string `json:"name"`
	AdventureTitle string `json:"adventure_title"`
	OrgName        string `json:"org_name"`
	Status         string `json:"status"`
	// Theme is a party adventure's starter theme, for its cover picture.
	Theme string `json:"theme,omitempty"`
	// CoverURL is the adventure's own cover picture, in place of the
	// theme's, served by the player link (JoinCover).
	CoverURL string `json:"cover_url,omitempty"`
	// Look is the adventure's look, so the page matches the game: see partyLook.
	Look *adventure.Colors `json:"look,omitempty"`
}

// GetJoin is the join page's view of a hosted event.
//
// GET /api/join/{token}
func (h *Handlers) GetJoin(w http.ResponseWriter, r *http.Request) {
	p, ok := h.partyByPlayerToken(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var out joinInfoDTO
	if err := h.DB.QueryRowContext(ctx, `
		SELECT e.party_name, COALESCE(NULLIF(a.title, ''), a.slug), o.name, a.trail_theme
		FROM events e JOIN adventures a ON a.slug = e.adventure_slug JOIN organizations o ON o.id = e.org_id
		WHERE e.id = $1
	`, p.EventID).Scan(&out.Name, &out.AdventureTitle, &out.OrgName, &out.Theme); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "game lookup failed", err)
		return
	}
	if cover, _, err := h.adventureCover(ctx, p.AdvSlug, false); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "game lookup failed", err)
		return
	} else if cover > 0 {
		out.CoverURL = "/api/join/" + chi.URLParam(r, "token") + "/cover?v=" + strconv.FormatInt(cover, 10)
	}
	look, _, err := h.partyLook(ctx, p.AdvSlug)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "game lookup failed", err)
		return
	}
	out.Look = look
	status, err := h.joinStatus(ctx, h.DB, p)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "game lookup failed", err)
		return
	}
	out.Status = status
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, out)
}

// joinStatus is whether a team may join p now; see joinInfoDTO.
func (h *Handlers) joinStatus(ctx context.Context, q play.Querier, p hostParty) (string, error) {
	if p.State == "completed" || p.State == "cancelled" || partyLapsed(p) {
		return "ended", nil
	}
	// A party plays the games it was bought for; another one ends it.
	if !p.RoundOpen && p.GameLimit > 0 && p.Round >= p.GameLimit {
		return "ended", nil
	}
	if !p.RoundOpen {
		if err := h.checkReadyToStart(ctx, p); err != nil {
			if errors.Is(err, errPlacesMissing) {
				return "not_ready", nil
			}
			return "", err
		}
		return "open", nil
	}
	var teams int
	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM teams WHERE event_id = $1 AND round = $2 AND withdrawn_at IS NULL
	`, p.EventID, p.Round).Scan(&teams); err != nil {
		return "", err
	}
	if p.TeamLimit > 0 && teams >= p.TeamLimit {
		return "full", nil
	}
	return "open", nil
}

type joinRequest struct {
	Team      string `json:"team"`
	Headcount int    `json:"headcount"`
}

// Join makes a team in the game being played — opening the next game
// when none is — and returns the link its phone plays from.
//
// POST /api/join/{token}
func (h *Handlers) Join(w http.ResponseWriter, r *http.Request) {
	var req joinRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Team)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		httpx.Error(w, http.StatusBadRequest, "give the team a name of up to 64 characters")
		return
	}
	// The join page no longer asks: one phone per team counts as one.
	if req.Headcount == 0 {
		req.Headcount = 1
	}
	if req.Headcount < 1 || req.Headcount > 50 {
		httpx.Error(w, http.StatusBadRequest, "a team has 1 to 50 players")
		return
	}
	token := chi.URLParam(r, "token")
	if token == "" || len(token) > 64 {
		httpx.Error(w, http.StatusNotFound, "no such game")
		return
	}
	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "join failed", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck
	// Serialise joins per event, so two teams can't both open a game or
	// both take the last place.
	var eventID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM events WHERE party AND player_token = $1 FOR UPDATE`, token).Scan(&eventID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "no such game")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "join failed", err)
		return
	}
	p, err := h.loadParty(ctx, tx, "e.id = $1", eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "join failed", err)
		return
	}
	status, err := h.joinStatus(ctx, tx, p)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "join failed", err)
		return
	}
	switch status {
	case "ended":
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "this game has ended", "code": "ended"})
		return
	case "not_ready":
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "the host is still setting the game up", "code": "not_ready"})
		return
	case "full":
		// The host page says how many teams couldn't join.
		if _, err := tx.ExecContext(ctx, `
			UPDATE event_rounds SET turned_away = array_append(turned_away, $3)
			WHERE event_id = $1 AND round = $2 AND NOT ($3 = ANY(turned_away)) AND cardinality(turned_away) < 100
		`, p.EventID, p.Round, strings.ToLower(name)); err == nil {
			_ = tx.Commit()
		}
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "this game is full — ask the host to add teams", "code": "full"})
		return
	}
	round := p.Round
	if !p.RoundOpen {
		if round, err = openRound(ctx, tx, p); err != nil {
			httpx.ServerErr(w, http.StatusConflict, "the game could not start; contact the organiser", err)
			return
		}
	}
	var taken bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM teams WHERE event_id = $1 AND round = $2 AND LOWER(name) = LOWER($3))
	`, p.EventID, round, name).Scan(&taken); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "join failed", err)
		return
	}
	if taken {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "there is a team with that name already", "code": "name_taken"})
		return
	}
	teamID, slotToken, err := createPartyTeam(ctx, tx, p.EventID, round, name, req.Headcount)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "join failed", err)
		return
	}
	// The team is paid for as it joins: from the organisation's balance,
	// unless the game was paid at checkout.
	if err := billing.ChargeTeam(ctx, tx, teamID, h.pricing()); errors.Is(err, billing.ErrCannotPay) {
		httpx.JSON(w, http.StatusPaymentRequired, map[string]string{"error": "this game can't take more teams just now: ask the organiser", "code": "org_cannot_pay"})
		return
	} else if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "join failed", err)
		return
	}
	activity.Record(ctx, tx, p.EventID, &teamID, activity.Actor{}, activity.TeamCreated, map[string]any{"name": name, "round": round})
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "join failed", err)
		return
	}
	// Placing the team now spares its phone the wait on first open.
	if err := play.Start(ctx, h.DB, p.EventID); err != nil {
		logx.Warn("admin.hosted.join.place", "event_id", p.EventID, "err", err.Error())
	}
	h.Hub.Publish(ws.EventTopic(p.EventID), "changed", nil)
	httpx.JSON(w, http.StatusCreated, map[string]string{
		"play_url": h.eventBase(ctx, p.EventID) + "/play?t=" + slotToken,
	})
}

// openRound opens the event's next game in tx: it pins the graph the
// game plays — publishing a quiz trail's latest cards first — and, for the first game, starts the event and its days of
// play. It returns the new round.
func openRound(ctx context.Context, tx *sql.Tx, p hostParty) (int, error) {
	// A quiz trail plays its cards as they are now.
	var advID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM adventures WHERE slug = $1`, p.AdvSlug).Scan(&advID); err != nil {
		return 0, err
	}
	if _, err := publishTrail(ctx, tx, advID, 0); err != nil {
		return 0, err
	}
	graphID, err := play.PinRound(ctx, tx, p.EventID)
	if err != nil {
		return 0, err
	}
	round := p.Round + 1
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO event_rounds (event_id, round, graph_id, paused_from)
		VALUES ($1, $2, $3, (SELECT paused_seconds FROM events WHERE id = $1))
	`, p.EventID, round, graphID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE events SET current_round = $2 WHERE id = $1`, p.EventID, round); err != nil {
		return 0, err
	}
	if p.State == "scheduled" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE events SET state = 'running', started_at = now_text(), first_game_at = now(), valid_until = $2
			WHERE id = $1
		`, p.EventID, time.Now().Add(HostedPlayPeriod)); err != nil {
			return 0, err
		}
		activity.Record(ctx, tx, p.EventID, nil, activity.Actor{}, activity.EventStarted, map[string]any{"first_game": true})
	}
	return round, nil
}

/* ---------------- finishing a game ---------------- */

// FinishHostedGame closes the game being played and mails the host its
// summary. The next team to join opens the next game.
//
// POST /api/party/{token}/finish-game
func (h *Handlers) FinishHostedGame(w http.ResponseWriter, r *http.Request) {
	p, ok := h.hostedParty(w, r)
	if !ok {
		return
	}
	done, err := h.finishRound(r.Context(), p.EventID, "host")
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "finish failed", err)
		return
	}
	if !done {
		httpx.Error(w, http.StatusConflict, "no game is being played")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PutHoldResults says whether the party keeps each game's results secret
// until the host finishes it. Turned off mid-game, the phones and the
// live board show them at once.
//
// PUT /api/party/{token}/hold-results
func (h *Handlers) PutHoldResults(w http.ResponseWriter, r *http.Request) {
	p, ok := h.hostedParty(w, r)
	if !ok {
		return
	}
	var req struct {
		Hold bool `json:"hold"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `UPDATE events SET party_hold_results = $1 WHERE id = $2`, req.Hold, p.EventID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "save failed", err)
		return
	}
	h.Hub.Publish(ws.EventTopic(p.EventID), "changed", nil)
	w.WriteHeader(http.StatusNoContent)
}

// finishRound closes the event's open game, if it has one: its teams'
// phones are signed out, a paused event runs again for the next game,
// and the host gets the summary. by says who finished it, for the log.
func (h *Handlers) finishRound(ctx context.Context, eventID int64, by string) (bool, error) {
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck
	var round int
	if err := tx.QueryRowContext(ctx, `SELECT current_round FROM events WHERE id = $1 FOR UPDATE`, eventID).Scan(&round); err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE event_rounds SET finished_at = now() WHERE event_id = $1 AND round = $2 AND finished_at IS NULL
	`, eventID, round)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	if err := endTeamSessions(ctx, tx, "t.event_id = $1 AND t.round = $2", eventID, round); err != nil {
		return false, err
	}
	if err := h.engine().SettleRound(ctx, tx, eventID, round); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE events SET state = 'running', `+activity.ClosePause+` WHERE id = $1 AND state = 'paused'
	`, eventID); err != nil {
		return false, err
	}
	// What the game spent paused, for the live board's clock.
	if _, err := tx.ExecContext(ctx, `
		UPDATE event_rounds r SET paused_seconds = GREATEST(0, e.paused_seconds - r.paused_from)
		FROM events e WHERE e.id = r.event_id AND r.event_id = $1 AND r.round = $2 AND r.paused_from IS NOT NULL
	`, eventID, round); err != nil {
		return false, err
	}
	activity.Record(ctx, tx, eventID, nil, activity.Actor{}, "round.finished", map[string]any{"round": round, "by": by})
	if err := tx.Commit(); err != nil {
		return false, err
	}
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	if err := h.sendRoundSummary(ctx, eventID, round); err != nil {
		// The sweeper tries again; see SweepHosted.
		logx.Warn("admin.hosted.summary", "event_id", eventID, "round", round, "err", err.Error())
	}
	return true, nil
}

// sendRoundSummary mails the host the summary of a finished game and
// marks it sent.
func (h *Handlers) sendRoundSummary(ctx context.Context, eventID int64, round int) error {
	publicURL := h.eventBase(ctx, eventID)
	var to, party string
	if err := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT buyer_email FROM ticket_purchases WHERE event_id = e.id AND buyer_email <> '' ORDER BY id LIMIT 1),
		                (SELECT email FROM trails WHERE event_id = e.id), ''), e.party_name
		FROM events e WHERE e.id = $1
	`, eventID).Scan(&to, &party); err != nil {
		return err
	}
	if to != "" {
		teams, cards, err := h.roundSummary(ctx, eventID, round)
		if err != nil {
			return err
		}
		l := h.eventLook(ctx, eventID)
		// The summary goes to the host, so it carries the host link. One
		// not kept would have to be replaced, signing the host page out,
		// so then the summary links to the host page's resend form.
		hostURL := publicURL + "/app/party/host/"
		if token, err := h.keptHostToken(ctx, eventID); err != nil {
			return err
		} else if token != "" {
			hostURL = publicURL + "/party/" + token
		}
		subject, c := emailtpl.RoundSummary(l.Lang, hostURL, party, l.Brand.Name, round, teams, cards)
		c.Picture = l.Cover
		msg := l.message([]string{to}, subject, c)
		if h.MailOutbox != nil {
			err = h.MailOutbox.Enqueue(ctx, "round_summary", msg)
		} else if h.Mailer != nil {
			err = h.Mailer.Send(ctx, msg)
		} else {
			err = errors.New("no mailer configured")
		}
		if err != nil {
			return err
		}
	}
	_, err := h.DB.ExecContext(ctx, `
		UPDATE event_rounds SET summary_sent_at = now() WHERE event_id = $1 AND round = $2
	`, eventID, round)
	return err
}

// roundSummary is one game's ranking and how each question went: of the
// teams that reached it, how many got it right.
func (h *Handlers) roundSummary(ctx context.Context, eventID int64, round int) ([]emailtpl.RoundTeam, []emailtpl.RoundCard, error) {
	ranked, err := h.engine().EventRanking(ctx, eventID)
	if err != nil {
		return nil, nil, err
	}
	type info struct {
		players int
		minutes int
	}
	in := map[int64]info{}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.id, COALESCE(t.headcount, 1),
		       COALESCE(EXTRACT(EPOCH FROM g.completed_at - g.started_at)::int / 60, 0)
		FROM teams t LEFT JOIN team_graph_state g ON g.team_id = t.id
		WHERE t.event_id = $1 AND t.round = $2 AND t.withdrawn_at IS NULL
	`, eventID, round)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var (
			id int64
			i  info
		)
		if err := rows.Scan(&id, &i.players, &i.minutes); err != nil {
			rows.Close()
			return nil, nil, err
		}
		in[id] = i
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	teams := []emailtpl.RoundTeam{}
	for _, r := range ranked {
		i, ok := in[r.TeamID]
		if !ok {
			continue
		}
		teams = append(teams, emailtpl.RoundTeam{Name: r.Name, Players: i.players, Score: r.Score, Finished: r.Done, Minutes: i.minutes})
	}

	var graphID sql.NullInt64
	if err := h.DB.QueryRowContext(ctx, `SELECT graph_id FROM event_rounds WHERE event_id = $1 AND round = $2`, eventID, round).Scan(&graphID); err != nil {
		return nil, nil, err
	}
	if !graphID.Valid {
		return teams, nil, nil
	}
	stored, err := graphstore.ByID(ctx, h.DB, graphID.Int64)
	if err != nil {
		return nil, nil, err
	}
	at := map[string]int{}
	// A quiz question is a block: its card is reached with its step and
	// right when the team picked its answer.
	type quizCard struct {
		card   int
		answer string
	}
	quiz := map[[2]string]quizCard{}
	reachedWith := map[string][]int{}
	cards := []emailtpl.RoundCard{}
	for _, n := range stored.Graph.Nodes {
		title := n.Title
		if title == "" {
			title = n.Slug
		}
		if n.Type == graph.Puzzle {
			at[n.Slug] = len(cards)
			cards = append(cards, emailtpl.RoundCard{Title: title})
			continue
		}
		var blocks []graph.Block
		for _, b := range n.Blocks() {
			if b.Kind == graph.BlockQuiz {
				blocks = append(blocks, b)
			}
		}
		for _, b := range blocks {
			t := title
			if len(blocks) > 1 && b.Text != "" {
				t = b.Text
			}
			quiz[[2]string{n.Slug, b.ID}] = quizCard{len(cards), b.Answer}
			reachedWith[n.Slug] = append(reachedWith[n.Slug], len(cards))
			cards = append(cards, emailtpl.RoundCard{Title: t})
		}
	}
	rows, err = h.DB.QueryContext(ctx, `
		SELECT s.node, COUNT(*), COUNT(s.solved_at)
		FROM team_node_state s JOIN teams t ON t.id = s.team_id
		WHERE t.event_id = $1 AND t.round = $2 AND t.withdrawn_at IS NULL
		GROUP BY s.node
	`, eventID, round)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			node           string
			reached, right int
		)
		if err := rows.Scan(&node, &reached, &right); err != nil {
			return nil, nil, err
		}
		if i, ok := at[node]; ok {
			cards[i].Reached, cards[i].Right = reached, right
		}
		for _, i := range reachedWith[node] {
			cards[i].Reached = reached
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(quiz) == 0 {
		return teams, cards, nil
	}
	picks, err := h.DB.QueryContext(ctx, `
		SELECT p.node, p.block_id, p.choice, COUNT(*)
		FROM team_block_picks p JOIN teams t ON t.id = p.team_id
		WHERE t.event_id = $1 AND t.round = $2 AND t.withdrawn_at IS NULL
		GROUP BY p.node, p.block_id, p.choice
	`, eventID, round)
	if err != nil {
		return nil, nil, err
	}
	defer picks.Close()
	for picks.Next() {
		var (
			node, block, choice string
			n                   int
		)
		if err := picks.Scan(&node, &block, &choice, &n); err != nil {
			return nil, nil, err
		}
		if q, ok := quiz[[2]string{node, block}]; ok && choice != "" && choice == q.answer {
			cards[q.card].Right += n
		}
	}
	return teams, cards, picks.Err()
}

/* ---------------- sending the player link ---------------- */

// SendHostedPlayerLink mails the player link to one address the host
// types, and nothing else.
//
// POST /api/party/{token}/send-player-link
func (h *Handlers) SendHostedPlayerLink(w http.ResponseWriter, r *http.Request) {
	p, ok := h.hostedParty(w, r)
	if !ok {
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	email := strings.TrimSpace(req.Email)
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email {
		httpx.Error(w, http.StatusBadRequest, "enter a valid email address")
		return
	}
	if !h.partyOpen(w, p) {
		return
	}
	ctx := r.Context()
	// A host link can't be used to mail strangers at volume.
	var sent int
	if err := h.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM event_activity WHERE event_id = $1 AND kind = 'player_link.sent' AND created_at > to_char((now() - interval '1 day') AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS')
	`, p.EventID).Scan(&sent); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "send failed", err)
		return
	}
	if sent >= maxPlayerLinkMails {
		httpx.Error(w, http.StatusTooManyRequests, "too many emails today; share the link or QR code instead")
		return
	}
	var party string
	_ = h.DB.QueryRowContext(ctx, `SELECT party_name FROM events WHERE id = $1`, p.EventID).Scan(&party)
	l := h.eventLook(ctx, p.EventID)
	subject, c := emailtpl.HostedPlayerLink(l.Lang, h.eventBase(ctx, p.EventID)+"/j/"+p.PlayerToken, party, l.Brand.Name)
	c.Picture = l.Cover
	msg := l.message([]string{email}, subject, c)
	var err error
	if h.MailOutbox != nil {
		err = h.MailOutbox.Enqueue(ctx, "player_link", msg)
	} else if h.Mailer != nil {
		err = h.Mailer.Send(ctx, msg)
	} else {
		err = errors.New("no mailer configured")
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "send failed", err)
		return
	}
	activity.Record(ctx, h.DB, p.EventID, nil, activity.Actor{}, "player_link.sent", nil)
	w.WriteHeader(http.StatusNoContent)
}

// maxPlayerLinkMails caps the player links a hosted event mails a day.
const maxPlayerLinkMails = 50

/* ---------------- the sweeper ---------------- */

// SweepHosted finishes the games nobody has played for a while, ends
// hosted events whose days are over (finishing their game first), and
// retries summaries that did not go out. Run every minute or so.
func (h *Handlers) SweepHosted(ctx context.Context) {
	// A game's last move is its teams' latest, or its start.
	idle, err := h.eventIDs(ctx, `
		SELECT r.event_id FROM event_rounds r
		WHERE r.finished_at IS NULL
		  AND GREATEST(r.started_at, COALESCE((
		        SELECT MAX(s.updated_at) FROM team_graph_state s JOIN teams t ON t.id = s.team_id
		        WHERE t.event_id = r.event_id AND t.round = r.round), r.started_at))
		      < now() - make_interval(secs => $1)
	`, roundIdle.Seconds())
	if err != nil {
		logx.Error("hosted.sweep.idle", "err", err.Error())
	}
	for _, id := range idle {
		if _, err := h.finishRound(ctx, id, "idle"); err != nil {
			logx.Error("hosted.sweep.finish", "event_id", id, "err", err.Error())
		}
	}

	lapsed, err := h.eventIDs(ctx, `
		SELECT id FROM events WHERE party AND state IN ('running','paused') AND valid_until < now()
	`)
	if err != nil {
		logx.Error("hosted.sweep.lapsed", "err", err.Error())
	}
	for _, id := range lapsed {
		if _, err := h.finishRound(ctx, id, "lapsed"); err != nil {
			logx.Error("hosted.sweep.finish", "event_id", id, "err", err.Error())
			continue
		}
		if _, err := activity.CompleteEvent(ctx, h.DB, id, activity.Actor{}, map[string]any{"lapsed": true}); err != nil {
			logx.Error("hosted.sweep.complete", "event_id", id, "err", err.Error())
			continue
		}
		h.Hub.Publish(ws.EventTopic(id), "changed", nil)
	}

	rows, err := h.DB.QueryContext(ctx, `
		SELECT event_id, round FROM event_rounds
		WHERE finished_at IS NOT NULL AND summary_sent_at IS NULL
		  AND finished_at BETWEEN now() - interval '1 day' AND now() - interval '1 minute'
	`)
	if err != nil {
		logx.Error("hosted.sweep.summaries", "err", err.Error())
		return
	}
	type pending struct {
		event int64
		round int
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.event, &p.round); err != nil {
			rows.Close()
			logx.Error("hosted.sweep.summaries", "err", err.Error())
			return
		}
		todo = append(todo, p)
	}
	rows.Close()
	for _, p := range todo {
		if err := h.sendRoundSummary(ctx, p.event, p.round); err != nil {
			logx.Warn("hosted.sweep.summary", "event_id", p.event, "round", p.round, "err", err.Error())
		}
	}
}

// eventIDs runs a query of event ids.
func (h *Handlers) eventIDs(ctx context.Context, q string, args ...any) ([]int64, error) {
	rows, err := h.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
