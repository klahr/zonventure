package admin

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"git.sr.ht/~klahr/zv/internal/adventure"
)

// ----- writeICS structural shape ------------------------------------

func TestWriteICS_HappyPath(t *testing.T) {
	e := icsEvent{
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
	if err := writeICS(&buf, e); err != nil {
		t.Fatalf("writeICS: %v", err)
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

func TestWriteICS_OrganizerOptional(t *testing.T) {
	// No organizer email → no ORGANIZER line.
	e := icsEvent{
		UID:         "event-1@zv",
		GeneratedAt: time.Now().UTC(),
		StartAt:     time.Now().UTC(),
		EndAt:       time.Now().UTC().Add(time.Hour),
		Title:       "test",
	}
	var buf bytes.Buffer
	if err := writeICS(&buf, e); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "ORGANIZER") {
		t.Errorf("ORGANIZER should be omitted when email is empty")
	}
}

// ----- escape rules ------------------------------------------------

func TestICSEscape(t *testing.T) {
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
		got := icsEscape(c.in)
		if got != c.want {
			t.Errorf("icsEscape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestICSEscapeAppliedToProperties(t *testing.T) {
	// A title with reserved characters should appear escaped in the
	// SUMMARY property — otherwise calendar apps will choke on the
	// raw semicolon as a parameter separator.
	e := icsEvent{
		UID:         "event-1@zv",
		GeneratedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		StartAt:     time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		EndAt:       time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC),
		Title:       "Hunt; with, special\nchars\\",
	}
	var buf bytes.Buffer
	if err := writeICS(&buf, e); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `SUMMARY:Hunt\; with\, special\nchars\\`) {
		t.Errorf("escape rules not applied to SUMMARY:\n%s", out)
	}
}

// ----- line folding ------------------------------------------------

func TestICSLineFolding(t *testing.T) {
	// Build a line longer than 75 octets and ensure it gets folded.
	long := "DESCRIPTION:" + strings.Repeat("x", 200)
	var b strings.Builder
	icsLine(&b, long)
	out := b.String()

	// Every physical line must be <= 75 octets. (The continuation
	// lines themselves include their leading space in the count.)
	for _, line := range splitCRLF(out) {
		if line == "" {
			continue
		}
		if len(line) > 75 {
			t.Errorf("line exceeds 75 octets (%d): %q", len(line), line)
		}
	}
	// Continuation lines must start with a single space.
	lines := splitCRLF(out)
	for i := 1; i < len(lines); i++ {
		if lines[i] == "" {
			continue
		}
		if lines[i][0] != ' ' {
			t.Errorf("continuation line %d does not start with space: %q", i, lines[i])
		}
	}
	// Reassembled (drop the leading space on continuations) must
	// equal the original input.
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

func TestICSLineFoldingShortLine(t *testing.T) {
	// Lines <= 75 octets must NOT be folded.
	short := "UID:event-42@zv"
	var b strings.Builder
	icsLine(&b, short)
	out := b.String()
	if out != short+"\r\n" {
		t.Errorf("short line was folded: %q", out)
	}
}

// ----- pickICSStart prefers most authoritative time ----------------

func TestPickICSStart(t *testing.T) {
	cases := []struct {
		name   string
		state  string
		sched  string
		start  string
		wantOK bool
		wantH  int // hour, sanity-only
	}{
		{"running uses started_at", "running", "2026-05-01 09:00:00", "2026-05-01 10:00:00", true, 10},
		{"completed uses started_at", "completed", "2026-05-01 09:00:00", "2026-05-01 10:00:00", true, 10},
		{"scheduled uses scheduled_at", "scheduled", "2026-05-01 09:00:00", "", true, 9},
		{"running falls back to scheduled when started missing", "running", "2026-05-01 09:00:00", "", true, 9},
		{"none → !ok", "scheduled", "", "", false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := pickICSStart(c.state, c.sched, c.start)
			if ok != c.wantOK {
				t.Fatalf("ok=%v want %v", ok, c.wantOK)
			}
			if ok && got.Hour() != c.wantH {
				t.Errorf("hour = %d, want %d", got.Hour(), c.wantH)
			}
		})
	}
}

// ----- pickICSLocation -----

func TestPickICSLocation(t *testing.T) {
	adv := &adventure.Adventure{Pages: []adventure.Page{
		{Disabled: true, Location: adventure.Location{Lat: 1, Lng: 2}}, // skipped
		{Location: adventure.Location{Lat: 0, Lng: 0}},                 // skipped (zero)
		{Location: adventure.Location{Lat: 59.32931, Lng: 18.06869}},
	}}
	got := pickICSLocation(adv)
	want := "59.32931, 18.06869"
	if got != want {
		t.Errorf("pickICSLocation = %q, want %q", got, want)
	}

	// Empty / no-location adventure → empty string (caller drops the
	// LOCATION property entirely).
	if got := pickICSLocation(&adventure.Adventure{}); got != "" {
		t.Errorf("empty adventure should yield empty location, got %q", got)
	}
	if got := pickICSLocation(nil); got != "" {
		t.Errorf("nil adventure should yield empty location, got %q", got)
	}
}

// ----- helpers ----

func splitCRLF(s string) []string {
	return strings.Split(s, "\r\n")
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("missing %q in:\n%s", needle, haystack)
	}
}
