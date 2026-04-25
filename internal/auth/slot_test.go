package auth

import (
	"testing"
	"time"
)

func TestParseSlotTimestamp(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Time
		ok   bool
	}{
		{"wire format", "2026-04-25 12:34:56",
			time.Date(2026, 4, 25, 12, 34, 56, 0, time.UTC), true},
		{"rfc3339 with Z", "2026-04-25T12:34:56Z",
			time.Date(2026, 4, 25, 12, 34, 56, 0, time.UTC), true},
		{"rfc3339 with offset", "2026-04-25T14:34:56+02:00",
			time.Date(2026, 4, 25, 12, 34, 56, 0, time.UTC), true},
		{"junk", "not a timestamp", time.Time{}, false},
		{"empty", "", time.Time{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseSlotTimestamp(c.in)
			if c.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !c.ok && err == nil {
				t.Fatalf("expected error for %q", c.in)
			}
			if c.ok && !got.Equal(c.want) {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}
