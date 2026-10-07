package player

import (
	"context"
	"errors"
	"github.com/klahr/zonventure/internal/emailtpl"
	"net/http"
	"strconv"
	"strings"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/icscal"
	"github.com/klahr/zonventure/internal/logx"
	mailerpkg "github.com/klahr/zonventure/internal/mailer"
)

// RecoverLinkRequest is the body of POST /api/player/recover-link.
// The team's contact email is the only input — server looks up
// every still-active slot tied to teams with that email and re-mints
// each slot's magic-link token, then mails the fresh links to the
// same address. The reset of claimed_at is cosmetic (the timestamp
// is no longer a gate) — it just keeps the audit column reading
// "claimed when the new link was first opened" rather than
// "claimed by whoever opened the dead link".
type RecoverLinkRequest struct {
	Email string `json:"email"`
}

// RecoverLink rotates magic-link tokens for every slot whose team
// carries the supplied contact email, then mails the fresh links.
// Always returns 204 regardless of whether the email matched any
// team — that's the email-enumeration defence; an attacker can't
// learn whether a given address is associated with an event by
// timing or by response shape.
//
// Privacy boundary: the team email IS the team's auth equivalent
// for this flow. Anyone holding the address gets back the team's
// player links. That's the same trust the arranger placed in the
// address when they typed it into the team form, so no escalation.
//
// Rate limit: per-IP, same shape as MagicLinkLogin. A scripted
// enumeration is bounded to 20 attempts/minute.
func (h *Handlers) RecoverLink(w http.ResponseWriter, r *http.Request) {
	var req RecoverLinkRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	ip := auth.ClientIP(r)
	if ok, _ := h.rateLimiter().AllowMagicLink(ip); !ok {
		// 204 silently (don't leak the rate-limit by surfacing 429
		// only on real-email attempts). The attacker just sees their
		// requests stop arriving in inboxes.
		httpx.JSON(w, http.StatusNoContent, nil)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		httpx.JSON(w, http.StatusNoContent, nil)
		return
	}

	// Find every still-active slot whose team's email matches. The
	// LEFT JOIN excludes slots whose event has already completed or
	// been cancelled — there's nothing to recover into a finished
	// event, and re-emailing dead links would be confusing UX.
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT ps.id, ps.name, t.name, e.name, e.id
		FROM player_slots ps
		JOIN teams t ON t.id = ps.team_id
		JOIN events e ON e.id = t.event_id
		WHERE LOWER(t.email) = $1
		  AND e.state IN ('scheduled', 'running', 'paused')
	`, email)
	if err != nil {
		logx.Error("player.recover_link.lookup", "err", err.Error())
		httpx.JSON(w, http.StatusNoContent, nil)
		return
	}
	type slotRow struct {
		ID         int64
		PlayerName string
		TeamName   string
		EventName  string
		EventID    int64
	}
	var slots []slotRow
	for rows.Next() {
		var s slotRow
		if err := rows.Scan(&s.ID, &s.PlayerName, &s.TeamName, &s.EventName, &s.EventID); err != nil {
			rows.Close()
			httpx.JSON(w, http.StatusNoContent, nil)
			return
		}
		slots = append(slots, s)
	}
	rows.Close()

	// Mint new tokens, persist them, build the email body. We do
	// the rotation even if Mailer fails — the new tokens going to
	// stderr (LogMailer fallback) is still useful for self-host
	// debugging, and the old tokens are now dead either way so a
	// retry with a fresh request would just rotate again.
	type freshLink struct {
		Player  string
		Team    string
		Event   string
		EventID int64
		URL     string
	}
	var links []freshLink
	for _, s := range slots {
		tok, err := auth.NewSlotToken()
		if err != nil {
			continue
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`UPDATE player_slots SET token = $1, claimed_at = NULL WHERE id = $2`,
			tok, s.ID); err != nil {
			continue
		}
		// Drop any existing slot sessions for this slot. Without this
		// step a previously-claimed device keeps a valid session cookie
		// (LookupSlot joins by slot_id, not the magic token), so the
		// recover-link email's "old links are now dead" promise would
		// only kill future claims, not active ones. Mirrors the admin
		// rotate path in admin/teams.go.
		_, _ = h.DB.ExecContext(r.Context(),
			`DELETE FROM slot_sessions WHERE slot_id = $1`, s.ID)
		base := strings.TrimRight(h.PublicURL, "/")
		// Origin-relative fallback for misconfigured self-host:
		// the email body becomes useless to the recipient, which
		// nudges the operator to set ZV_PUBLIC_URL — same posture
		// as buildResetLink.
		url := base + "/play?t=" + tok
		links = append(links, freshLink{
			Player: s.PlayerName, Team: s.TeamName, Event: s.EventName, EventID: s.EventID,
			URL: url,
		})
	}

	if len(links) > 0 && h.Mailer != nil {
		players := make([]emailtpl.PlayerLink, len(links))
		for i, l := range links {
			players[i] = emailtpl.PlayerLink{Team: l.Team, Player: l.Player, Event: l.Event, URL: l.URL}
		}
		// In the look of the organisation that runs the first game.
		lk := emailtpl.Lookup(r.Context(), h.DB, h.PublicURL, 0, links[0].EventID)
		subject, content := emailtpl.PlayLinks(lk.Lang, players, lk.Brand.Name)
		content.Picture = lk.Cover
		// Build one .ics per unique event referenced by the
		// recovered links so the recipient can drop each event onto
		// their calendar in one click. Best-effort: a missing
		// schedule (event has no scheduled_at / started_at) drops
		// just that attachment rather than failing the whole send.
		seenEvents := make(map[int64]struct{}, len(links))
		uniqueEvents := make([]int64, 0, len(links))
		for _, l := range links {
			if _, dup := seenEvents[l.EventID]; dup {
				continue
			}
			seenEvents[l.EventID] = struct{}{}
			uniqueEvents = append(uniqueEvents, l.EventID)
		}
		attachments := buildEventInviteAttachments(r.Context(), h, uniqueEvents)
		msg := emailtpl.Message([]string{email}, subject, lk.Theme, lk.Brand, content)
		msg.Attachments = attachments
		if err := h.Mailer.Send(r.Context(), msg); err != nil {
			logx.Warn("player.recover_link.send", "err", err.Error())
		}
	}
	logx.Info("player.recover_link.processed",
		"email_hash", strconv.Itoa(len(email)),
		"slots", len(links))
	httpx.JSON(w, http.StatusNoContent, nil)
}

// buildEventInviteAttachments returns one .ics attachment per event
// id passed in. Failures (events with no scheduled_at, library load
// issues) drop just the affected attachment rather than failing the
// whole recovery. The caller is responsible for de-duplication; the
// helper only does the format work.
func buildEventInviteAttachments(ctx context.Context, h *Handlers, eventIDs []int64) []mailerpkg.Attachment {
	if h == nil || h.DB == nil || h.Lib == nil {
		return nil
	}
	var atts []mailerpkg.Attachment
	for _, eventID := range eventIDs {
		evt, err := icscal.BuildForEvent(ctx, h.DB, h.Lib, eventID, h.PublicURL)
		if err != nil {
			if !errors.Is(err, icscal.ErrNoSchedule) {
				logx.Warn("player.recover_link.ics_build", "event", eventID, "err", err.Error())
			}
			continue
		}
		bytes, err := icscal.Bytes(evt)
		if err != nil {
			logx.Warn("player.recover_link.ics_render", "event", eventID, "err", err.Error())
			continue
		}
		atts = append(atts, mailerpkg.Attachment{
			Filename:    icscal.Filename(eventID),
			ContentType: "text/calendar; method=PUBLISH; charset=UTF-8",
			Bytes:       bytes,
		})
	}
	return atts
}
