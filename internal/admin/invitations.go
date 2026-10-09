package admin

import (
	"context"
	"errors"
	"github.com/klahr/zonventure/internal/emailtpl"
	"net/http"
	"strings"

	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/icscal"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/mailer"
)

// SendEventInvitations triggers an outbound email to every team in
// the event that has a non-empty contact email. Each team gets one
// personalised message with that team's player slot links + the
// event's .ics attached, so participants can both join and add the
// event to their calendar in one go.
//
// Authorisation: authorizeEvent — org_admin and org_member of the
// owning org both pass. Sending invites is a "running the game"
// action (Race directors do this on the day), not authoring, so the
// canAuthor split deliberately does not apply.
//
// Response shape:
//
//	{ "sent": <n>, "skipped_no_email": <n>, "failed": <n> }
//
// Mailer failures count toward `failed` but don't abort the loop —
// one bad SMTP attempt shouldn't lose the rest of the team. Per-
// team errors are logged via logx.Warn so an operator can correlate.
func (h *Handlers) SendEventInvitations(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	if h.Mailer == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "no mailer configured")
		return
	}

	// Build the .ics once — every team's email gets the same
	// attachment, since they're all attending the same event. A
	// missing schedule (event not yet planned) just drops the
	// attachment; the magic links still go out so an arranger can
	// pre-distribute before scheduling.
	var icsAttach []mailer.Attachment
	if evt, err := icscal.BuildForEvent(r.Context(), h.DB, h.Lib, eventID, h.PublicURL); err == nil {
		if bytes, berr := icscal.Bytes(evt); berr == nil {
			icsAttach = []mailer.Attachment{{
				Filename:    icscal.Filename(eventID),
				ContentType: "text/calendar; method=PUBLISH; charset=UTF-8",
				Bytes:       bytes,
			}}
		}
	} else if !errors.Is(err, icscal.ErrNoSchedule) {
		logx.Warn("admin.invitations.ics", "event", eventID, "err", err.Error())
	}

	// Pull every team's contact + slot tokens. One row per slot;
	// teams without slots are filtered out by the inner join, and
	// teams without an email are surfaced so we can count them as
	// `skipped_no_email` rather than silently dropping.
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT t.id, t.name, COALESCE(t.email, ''), t.token, ps.name, ps.token
		FROM teams t
		LEFT JOIN player_slots ps ON ps.team_id = t.id
		WHERE t.event_id = $1
		ORDER BY t.id, ps.id
	`, eventID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "team load failed")
		return
	}
	defer rows.Close()

	type teamInfo struct {
		ID    int64
		Name  string
		Email string
		Token string
		Slots []invitationSlot
	}
	var (
		teams []*teamInfo
		byID  = map[int64]*teamInfo{}
	)
	for rows.Next() {
		var (
			tid    int64
			tname  string
			temail string
			ttoken string
			pname  *string
			ptoken *string
		)
		if err := rows.Scan(&tid, &tname, &temail, &ttoken, &pname, &ptoken); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "team scan failed")
			return
		}
		t, ok := byID[tid]
		if !ok {
			t = &teamInfo{ID: tid, Name: tname, Email: strings.TrimSpace(temail), Token: ttoken}
			byID[tid] = t
			teams = append(teams, t)
		}
		if pname != nil && ptoken != nil {
			t.Slots = append(t.Slots, invitationSlot{Player: *pname, Token: *ptoken})
		}
	}

	var (
		sent           int
		skippedNoEmail int
		failed         int
	)
	publicURL := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/")
	l := h.eventLook(r.Context(), eventID)
	for _, t := range teams {
		if t.Email == "" {
			skippedNoEmail++
			continue
		}
		if len(t.Slots) == 0 {
			// Teams without slots have nothing to send. Lump under
			// skipped_no_email to keep the response shape simple;
			// in practice it never happens (CreateTeam requires at
			// least one member).
			skippedNoEmail++
			continue
		}
		if err := sendOneInvitation(r.Context(), h.Mailer, l, t.Name, t.Email, publicURL, t.Slots, h.fileLinks(r.Context(), eventID, publicURL, t.Token), icsAttach); err != nil {
			logx.Warn("admin.invitations.send", "event", eventID, "team", t.ID, "err", err.Error())
			failed++
			continue
		}
		sent++
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"sent":             sent,
		"skipped_no_email": skippedNoEmail,
		"failed":           failed,
	})
}

// SendTeamInvitation is the per-team variant: resend exactly one
// team's email. The action a Race director reaches for when one
// team lost their original message and pings the arranger mid-event.
//
// Existing tokens are sent verbatim — no re-mint. If a player has
// already claimed their slot and lost their browser session,
// they should hit the public "I lost my magic link" recovery flow
// instead, which intentionally re-mints. The split keeps "arranger
// helps a team" cheap (no surprise session invalidations) while
// still giving the player a self-serve fresh-token path.
//
// Same authorize-event gate as the bulk send.
func (h *Handlers) SendTeamInvitation(w http.ResponseWriter, r *http.Request) {
	teamID, ok := teamIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad team id")
		return
	}
	if h.Mailer == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "no mailer configured")
		return
	}
	// Resolve the team's event_id so the standard event-auth gate
	// applies. Avoids a per-team auth helper duplicating sameOrg.
	var (
		eventID  int64
		teamName string
		teamMail string
		teamTok  string
	)
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT event_id, name, COALESCE(email, ''), token FROM teams WHERE id = $1`, teamID).
		Scan(&eventID, &teamName, &teamMail, &teamTok); err != nil {
		httpx.Error(w, http.StatusNotFound, "team not found")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	teamMail = strings.TrimSpace(teamMail)
	if teamMail == "" {
		httpx.Error(w, http.StatusBadRequest, "team has no contact email — set one first")
		return
	}

	// Slot tokens for this team. ORDER BY so the email body lists
	// players in the same order every send → cleaner re-receive.
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT name, token FROM player_slots WHERE team_id = $1 ORDER BY id`, teamID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "slot load failed")
		return
	}
	var slots []invitationSlot
	for rows.Next() {
		var s invitationSlot
		if err := rows.Scan(&s.Player, &s.Token); err != nil {
			rows.Close()
			httpx.Error(w, http.StatusInternalServerError, "slot scan failed")
			return
		}
		slots = append(slots, s)
	}
	rows.Close()
	if len(slots) == 0 {
		httpx.Error(w, http.StatusBadRequest, "team has no players to invite")
		return
	}

	// Same .ics-once approach as the bulk handler — built per
	// resend rather than cached, since arrangers may be re-sending
	// after a reschedule and the file should reflect the latest
	// scheduled_at.
	var icsAttach []mailer.Attachment
	if evt, err := icscal.BuildForEvent(r.Context(), h.DB, h.Lib, eventID, h.PublicURL); err == nil {
		if bytes, berr := icscal.Bytes(evt); berr == nil {
			icsAttach = []mailer.Attachment{{
				Filename:    icscal.Filename(eventID),
				ContentType: "text/calendar; method=PUBLISH; charset=UTF-8",
				Bytes:       bytes,
			}}
		}
	} else if !errors.Is(err, icscal.ErrNoSchedule) {
		logx.Warn("admin.invitations.team.ics", "event", eventID, "team", teamID, "err", err.Error())
	}

	publicURL := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/")
	if err := sendOneInvitation(r.Context(), h.Mailer, h.eventLook(r.Context(), eventID), teamName, teamMail, publicURL, slots, h.fileLinks(r.Context(), eventID, publicURL, teamTok), icsAttach); err != nil {
		logx.Warn("admin.invitations.team.send", "event", eventID, "team", teamID, "err", err.Error())
		httpx.ServerErr(w, http.StatusBadGateway, "send failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"sent": 1})
}

// invitationSlot is the (player, token) pair the email body
// renders one line for. Hoisted out of the handler so the
// helper signature can name the type cleanly.
type invitationSlot struct {
	Player string
	Token  string
}

// sendOneInvitation builds and sends a single team's email. Pulled
// out so the loop in SendEventInvitations stays readable and the
// per-team body assembly can be unit-tested in isolation.
func sendOneInvitation(
	ctx context.Context,
	m mailer.Mailer,
	l look,
	teamName, teamEmail, publicURL string,
	slots []invitationSlot,
	files []emailtpl.FileLink,
	atts []mailer.Attachment,
) error {
	players := make([]emailtpl.PlayerLink, len(slots))
	for i, s := range slots {
		players[i] = emailtpl.PlayerLink{Team: teamName, Player: s.Player, URL: publicURL + "/play?t=" + s.Token}
	}
	subject, c := emailtpl.TeamInvitation(l.Lang, teamName, l.Event, l.Brand.Name, players, len(atts) > 0)
	c = emailtpl.WithFiles(c, files)
	c.Picture = l.Cover
	msg := l.message([]string{teamEmail}, subject, c)
	msg.Attachments = atts
	return m.Send(ctx, msg)
}
