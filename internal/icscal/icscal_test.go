package icscal

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/adventure"
)

// ----- Write structural shape -------------------------------------

func TestWrite_HappyPath(t *testing.T) {
	e := Event{
		UID:            "event-42@zv.example.com",
		GeneratedAt:    time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC),
		SequenceN:      0,
		StartAt:        time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		EndAt:          time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		Title:          "Stockholm Old Town",
		OrgName:        "Acme Corp",
		OrganizerEmail: "hr@acme.com",
		OrganizerName:  "Acme Corp",
		LocationLabel:  "59.32931, 18.06869",
		Description:    "Acme Corp — Stockholm Old Town\n\nJoin link: https://zv.example.com/play",
		JoinURL:        "https://zv.example.com/play",
		Status:         "CONFIRMED",
	}
	var buf bytes.Buffer
	if err := Write(&buf, e); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()

	mustContain(t, out, "BEGIN:VCALENDAR\r\n")
	mustContain(t, out, "VERSION:2.0\r\n")
	mustContain(t, out, "PRODID:-//Zonventure//Event Invite//EN\r\n")
	mustContain(t, out, "METHOD:PUBLISH\r\n")
	mustContain(t, out, "BEGIN:VEVENT\r\n")
	mustContain(t, out, "UID:event-42@zv.example.com\r\n")
	mustContain(t, out, "DTSTAMP:20260501T090000Z\r\n")
	mustContain(t, out, "DTSTART:20260501T100000Z\r\n")
	mustContain(t, out, "DTEND:20260501T120000Z\r\n")
	mustContain(t, out, "SEQUENCE:0\r\n")
	mustContain(t, out, "STATUS:CONFIRMED\r\n")
	mustContain(t, out, "SUMMARY:Stockholm Old Town\r\n")
	mustContain(t, out, "LOCATION:59.32931")
	mustContain(t, out, "URL:https://zv.example.com/play\r\n")
	mustContain(t, out, "ORGANIZER")
	mustContain(t, out, "mailto:hr@acme.com")
	mustContain(t, out, "END:VEVENT\r\n")
	mustContain(t, out, "END:VCALENDAR\r\n")

	// CRLF everywhere — bare LF lines tank Outlook parsing.
	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Errorf("output contains bare LF; must be CRLF only")
	}
}

func TestWrite_OrganizerOptional(t *testing.T) {
	e := Event{
		UID:         "event-1@zv",
		GeneratedAt: time.Now().UTC(),
		StartAt:     time.Now().UTC(),
		EndAt:       time.Now().UTC().Add(time.Hour),
		Title:       "test",
	}
	var buf bytes.Buffer
	if err := Write(&buf, e); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "ORGANIZER") {
		t.Errorf("ORGANIZER should be omitted when email is empty")
	}
}

// ----- Bytes returns a sensible byte slice ------------------------

func TestBytes(t *testing.T) {
	e := Event{
		UID:         "event-1@zv",
		GeneratedAt: time.Now().UTC(),
		StartAt:     time.Now().UTC(),
		EndAt:       time.Now().UTC().Add(time.Hour),
		Title:       "test",
	}
	out, err := Bytes(e)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte("BEGIN:VCALENDAR\r\n")) {
		t.Errorf("Bytes output should start with BEGIN:VCALENDAR; first 32 = %q", out[:min(32, len(out))])
	}
}

// ----- escape rules -----------------------------------------------

func TestEscape(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"plain", "plain"},
		{`back\slash`, `back\\slash`},
		{"a;b", `a\;b`},
		{"a,b", `a\,b`},
		{"line1\nline2", `line1\nline2`},
		{"line1\r\nline2", `line1\nline2`},
		{"all of: ; , \\ \n once", `all of: \; \, \\ \n once`},
	}
	for _, c := range cases {
		got := Escape(c.in)
		if got != c.want {
			t.Errorf("Escape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEscapeAppliedToProperties(t *testing.T) {
	e := Event{
		UID:         "event-1@zv",
		GeneratedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		StartAt:     time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		EndAt:       time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC),
		Title:       "Hunt; with, special\nchars\\",
	}
	var buf bytes.Buffer
	if err := Write(&buf, e); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `SUMMARY:Hunt\; with\, special\nchars\\`) {
		t.Errorf("escape rules not applied to SUMMARY:\n%s", out)
	}
}

// ----- line folding -----------------------------------------------

func TestLineFolding(t *testing.T) {
	long := "DESCRIPTION:" + strings.Repeat("x", 200)
	var b strings.Builder
	line(&b, long)
	out := b.String()

	for _, s := range strings.Split(out, "\r\n") {
		if s == "" {
			continue
		}
		if len(s) > 75 {
			t.Errorf("line exceeds 75 octets (%d): %q", len(s), s)
		}
	}
	lines := strings.Split(out, "\r\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] == "" {
			continue
		}
		if lines[i][0] != ' ' {
			t.Errorf("continuation line %d does not start with space: %q", i, lines[i])
		}
	}
	reassembled := lines[0]
	for i := 1; i < len(lines); i++ {
		if lines[i] == "" {
			continue
		}
		reassembled += lines[i][1:]
	}
	if reassembled != long {
		t.Errorf("reassembled value mismatch\nwant: %q\ngot:  %q", long, reassembled)
	}
}

func TestLineFoldingShortLine(t *testing.T) {
	short := "UID:event-42@zv"
	var b strings.Builder
	line(&b, short)
	if b.String() != short+"\r\n" {
		t.Errorf("short line was folded: %q", b.String())
	}
}

// ----- PickStart / pickLocation -----------------------------------

func TestPickStart(t *testing.T) {
	cases := []struct {
		name   string
		state  string
		sched  string
		start  string
		wantOK bool
		wantH  int
	}{
		{"running uses started_at", "running", "2026-05-01 09:00:00", "2026-05-01 10:00:00", true, 10},
		{"completed uses started_at", "completed", "2026-05-01 09:00:00", "2026-05-01 10:00:00", true, 10},
		{"scheduled uses scheduled_at", "scheduled", "2026-05-01 09:00:00", "", true, 9},
		{"running falls back to scheduled when started missing", "running", "2026-05-01 09:00:00", "", true, 9},
		{"none → !ok", "scheduled", "", "", false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := PickStart(c.state, c.sched, c.start)
			if ok != c.wantOK {
				t.Fatalf("ok=%v want %v", ok, c.wantOK)
			}
			if ok && got.Hour() != c.wantH {
				t.Errorf("hour = %d, want %d", got.Hour(), c.wantH)
			}
		})
	}
}

func TestPickLocation(t *testing.T) {
	adv := &adventure.Adventure{Pages: []adventure.Page{
		{Disabled: true, Location: adventure.Location{Lat: 1, Lng: 2}},
		{Location: adventure.Location{Lat: 0, Lng: 0}},
		{Location: adventure.Location{Lat: 59.32931, Lng: 18.06869}},
	}}
	got := pickLocation(adv)
	want := "59.32931, 18.06869"
	if got != want {
		t.Errorf("pickLocation = %q, want %q", got, want)
	}
	if got := pickLocation(&adventure.Adventure{}); got != "" {
		t.Errorf("empty adventure should yield empty location, got %q", got)
	}
	if got := pickLocation(nil); got != "" {
		t.Errorf("nil adventure should yield empty location, got %q", got)
	}
}

// ----- helpers -----

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("missing %q in:\n%s", needle, haystack)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
