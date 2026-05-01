package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/logx"
)

// ExportEventInviteICS writes an RFC 5545 iCalendar file for one
// scheduled event. Same authorize-event gate as the recap PDF and CSV
// — only org members can pull invites for their own events.
//
// The arranger downloads it once and forwards it to participants; a
// future iteration can attach the same .ics to outgoing magic-link
// invitation emails so participants get a real calendar entry without
// any forward-the-attachment step.
func (h *Handlers) ExportEventInviteICS(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	if _, ok := h.authorizeEvent(w, r, eventID); !ok {
		return
	}
	data, err := h.collectICSData(r.Context(), eventID)
	if err != nil {
		if errors.Is(err, errICSNoSchedule) {
			httpx.Error(w, http.StatusBadRequest, "event has no scheduled time")
			return
		}
		logx.Warn("admin.ics.lookup", "event", eventID, "err", err.Error())
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8; method=PUBLISH")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="event-%d-invite.ics"`, eventID))
	if err := writeICS(w, data); err != nil {
		logx.Warn("admin.ics.render", "event", eventID, "err", err.Error())
	}
}

// errICSNoSchedule indicates the event has neither a scheduled_at nor
// a started_at — there's nothing meaningful to put on a calendar.
var errICSNoSchedule = errors.New("event has no scheduled time")

// icsEvent is the rendered shape — pure data so the writer can be
// unit-tested against fixtures without a database.
type icsEvent struct {
	UID            string
	GeneratedAt    time.Time
	SequenceN      int
	StartAt        time.Time // must be in UTC
	EndAt          time.Time // must be in UTC, > StartAt
	Title          string
	OrgName        string
	OrganizerEmail string
	OrganizerName  string
	LocationLabel  string // free-form; coords or place name
	Description    string // plain text; line breaks become \n in the unfolded value
	JoinURL        string // shown in DESCRIPTION + emitted as URL property
	Status         string // CONFIRMED / TENTATIVE / CANCELLED
}

// collectICSData pulls the event's calendar-relevant fields from the
// DB and the loaded adventure. Errors with errICSNoSchedule when the
// event hasn't been scheduled or started — the caller maps that to a
// 400 since it's a state issue, not a server failure.
func (h *Handlers) collectICSData(ctx context.Context, eventID int64) (icsEvent, error) {
	var (
		out      icsEvent
		slug     string
		state    string
		schedRaw sql.NullString
		startRaw sql.NullString
		endRaw   sql.NullString
		seq      int
		creator  sql.NullString // creator email
		orgName  string
	)
	// SEQUENCE stays at 0 in the current shape — the codebase doesn't
	// emit a reschedule activity yet, so we have no signal to count
	// updates against. When that lands the COUNT can go in here and
	// existing calendar entries on participants' calendars will
	// auto-update on .ics re-import.
	seq = 0
	if err := h.DB.QueryRowContext(ctx, `
		SELECT e.adventure_slug, e.state, e.scheduled_at, e.started_at,
		       e.completed_at,
		       COALESCE(u.email, ''),
		       o.name
		FROM events e
		JOIN organizations o ON o.id = e.org_id
		LEFT JOIN users u ON u.id = e.created_by_user_id
		WHERE e.id = $1
	`, eventID).Scan(&slug, &state, &schedRaw, &startRaw, &endRaw, &creator, &orgName); err != nil {
		return out, err
	}
	adv := h.Lib.Get(slug)
	if adv == nil {
		return out, fmt.Errorf("adventure %q not loaded", slug)
	}

	// Pick the most reliable start time available. A running or
	// completed event uses its actual started_at; a scheduled event
	// uses the planned slot. Either way the time on the invite is
	// the time the participant should show up.
	startTime, hasStart := pickICSStart(state, schedRaw.String, startRaw.String)
	if !hasStart {
		return out, errICSNoSchedule
	}
	endTime, hasEnd := parseRecapTimestamp(endRaw.String)
	if !hasEnd || !endTime.After(startTime) {
		// Default duration when the event hasn't recorded a finish
		// stamp yet (the common case at invite-send time). Two hours
		// matches the typical corporate team-build window; arrangers
		// running longer events can edit the event entry their
		// calendar app creates.
		endTime = startTime.Add(2 * time.Hour)
	}

	out.UID = fmt.Sprintf("event-%d@%s", eventID, h.icsHost())
	out.GeneratedAt = time.Now().UTC()
	out.SequenceN = seq
	out.StartAt = startTime.UTC()
	out.EndAt = endTime.UTC()
	out.Title = adv.Meta.Title
	if out.Title == "" {
		out.Title = "Zonventure event"
	}
	out.OrgName = orgName
	out.OrganizerEmail = strings.TrimSpace(creator.String)
	out.OrganizerName = orgName
	out.LocationLabel = pickICSLocation(adv)
	out.JoinURL = h.icsJoinURL()
	out.Description = buildICSDescription(out.Title, adv.Meta.Description, out.JoinURL, orgName)
	out.Status = "CONFIRMED"
	if state == "cancelled" {
		out.Status = "CANCELLED"
	} else if state == "scheduled" && !startRaw.Valid {
		out.Status = "TENTATIVE"
	}
	return out, nil
}

// icsHost returns the host portion of the operator's PublicURL. Used
// in UID generation. Falls back to "zonventure" so the UID is still
// well-formed when PublicURL is unset.
func (h *Handlers) icsHost() string {
	pu := strings.TrimSpace(h.PublicURL)
	if pu == "" {
		return "zonventure"
	}
	pu = strings.TrimPrefix(pu, "https://")
	pu = strings.TrimPrefix(pu, "http://")
	if i := strings.IndexAny(pu, "/?#"); i >= 0 {
		pu = pu[:i]
	}
	if pu == "" {
		return "zonventure"
	}
	return pu
}

// icsJoinURL points participants at the public play surface. Per-team
// links are minted at invite-send time (out of scope for the calendar
// entry); this is the generic landing the calendar event links to.
func (h *Handlers) icsJoinURL() string {
	pu := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/")
	if pu == "" {
		return ""
	}
	return pu + "/play"
}

// pickICSStart picks the most authoritative start time available for
// the calendar entry. State-aware so a running event's actual start
// wins over a stale scheduled stamp.
func pickICSStart(state, schedRaw, startedRaw string) (time.Time, bool) {
	switch state {
	case "running", "paused", "completed":
		if t, ok := parseRecapTimestamp(startedRaw); ok {
			return t, true
		}
	}
	if t, ok := parseRecapTimestamp(schedRaw); ok {
		return t, true
	}
	if t, ok := parseRecapTimestamp(startedRaw); ok {
		return t, true
	}
	return time.Time{}, false
}

// pickICSLocation surfaces the first enabled puzzle's coordinates as
// a "59.32931, 18.06869" string. iCalendar's LOCATION is free-form,
// so coords work fine; a future iteration could reverse-geocode for
// a human label. Returns an empty string when the adventure has no
// geo-located puzzles (so the LOCATION property is omitted entirely
// rather than emitted with a meaningless "0, 0").
func pickICSLocation(adv *adventure.Adventure) string {
	if adv == nil {
		return ""
	}
	for _, p := range adv.Pages {
		if p.Disabled {
			continue
		}
		if p.Location.Lat == 0 && p.Location.Lng == 0 {
			continue
		}
		return fmt.Sprintf("%.5f, %.5f", p.Location.Lat, p.Location.Lng)
	}
	return ""
}

// buildICSDescription assembles the multi-line plain-text description.
// Newlines come out as "\n" in the unfolded property value (see the
// escape rules in icsEscape).
func buildICSDescription(title, desc, joinURL, orgName string) string {
	var b strings.Builder
	if orgName != "" {
		fmt.Fprintf(&b, "%s — %s\n", orgName, title)
	} else {
		b.WriteString(title)
		b.WriteString("\n")
	}
	if d := strings.TrimSpace(desc); d != "" {
		b.WriteString("\n")
		b.WriteString(d)
		b.WriteString("\n")
	}
	if joinURL != "" {
		b.WriteString("\nJoin link: ")
		b.WriteString(joinURL)
	}
	return b.String()
}

// writeICS emits the VCALENDAR/VEVENT to w. Pure rendering — no DB,
// no http.Request — so it's straightforward to unit-test by feeding
// a fixture.
//
// Wire format follows RFC 5545:
//   - CRLF line endings
//   - lines >75 octets folded onto continuation lines starting with a space
//   - property values escape backslash, semicolon, comma, and newline
func writeICS(w io.Writer, e icsEvent) error {
	var b strings.Builder
	icsLine(&b, "BEGIN:VCALENDAR")
	icsLine(&b, "VERSION:2.0")
	icsLine(&b, "PRODID:-//Zonventure//Event Invite//EN")
	icsLine(&b, "CALSCALE:GREGORIAN")
	icsLine(&b, "METHOD:PUBLISH")
	icsLine(&b, "BEGIN:VEVENT")
	icsLine(&b, "UID:"+e.UID)
	icsLine(&b, "DTSTAMP:"+icsTime(e.GeneratedAt))
	icsLine(&b, "DTSTART:"+icsTime(e.StartAt))
	icsLine(&b, "DTEND:"+icsTime(e.EndAt))
	icsLine(&b, "SEQUENCE:"+fmt.Sprintf("%d", e.SequenceN))
	if status := strings.TrimSpace(e.Status); status != "" {
		icsLine(&b, "STATUS:"+status)
	}
	icsLine(&b, "SUMMARY:"+icsEscape(e.Title))
	if loc := strings.TrimSpace(e.LocationLabel); loc != "" {
		icsLine(&b, "LOCATION:"+icsEscape(loc))
	}
	if desc := strings.TrimSpace(e.Description); desc != "" {
		icsLine(&b, "DESCRIPTION:"+icsEscape(desc))
	}
	if e.JoinURL != "" {
		icsLine(&b, "URL:"+icsEscape(e.JoinURL))
	}
	if e.OrganizerEmail != "" {
		// Email is not iCal-escaped (it goes inside a CAL-ADDRESS),
		// but the CN parameter is — names can contain commas.
		cn := icsEscape(strings.TrimSpace(e.OrganizerName))
		if cn != "" {
			icsLine(&b, "ORGANIZER;CN="+icsParamQuote(cn)+":mailto:"+e.OrganizerEmail)
		} else {
			icsLine(&b, "ORGANIZER:mailto:"+e.OrganizerEmail)
		}
	}
	icsLine(&b, "END:VEVENT")
	icsLine(&b, "END:VCALENDAR")
	_, err := io.WriteString(w, b.String())
	return err
}

// icsLine appends one logical line to b, applying RFC 5545's 75-octet
// folding rule: lines longer than 75 octets are split with CRLF + a
// single leading space on each continuation. We measure in bytes
// rather than runes — RFC 5545's octet count maps to bytes for UTF-8.
func icsLine(b *strings.Builder, line string) {
	const maxOctets = 75
	const crlf = "\r\n"
	if len(line) <= maxOctets {
		b.WriteString(line)
		b.WriteString(crlf)
		return
	}
	// First chunk is the maxOctets prefix; subsequent chunks are
	// (maxOctets-1) because the leading space costs one octet.
	b.WriteString(line[:maxOctets])
	b.WriteString(crlf)
	rest := line[maxOctets:]
	for len(rest) > maxOctets-1 {
		b.WriteString(" ")
		b.WriteString(rest[:maxOctets-1])
		b.WriteString(crlf)
		rest = rest[maxOctets-1:]
	}
	if len(rest) > 0 {
		b.WriteString(" ")
		b.WriteString(rest)
		b.WriteString(crlf)
	}
}

// icsTime formats a time as the "YYYYMMDDTHHMMSSZ" UTC form RFC 5545
// uses for DTSTAMP / DTSTART / DTEND.
func icsTime(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

// icsEscape applies RFC 5545 §3.3.11 text escaping. Values may not
// contain raw backslash, semicolon, comma, or newline; each must be
// replaced with the listed sequence.
func icsEscape(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		";", `\;`,
		",", `\,`,
		"\r\n", `\n`,
		"\n", `\n`,
		"\r", `\n`,
	)
	return r.Replace(s)
}

// icsParamQuote wraps a parameter value in double quotes when it
// contains characters (`:`, `;`, `,`, or whitespace) that would
// otherwise need escaping. Per RFC 5545 §3.2 quoted parameter values
// cannot themselves contain a `"`, so we strip those defensively.
func icsParamQuote(s string) string {
	if strings.ContainsAny(s, `:;,`) || strings.ContainsAny(s, " \t") {
		return `"` + strings.ReplaceAll(s, `"`, "") + `"`
	}
	return s
}
