package updates_test

import (
	"strings"
	"testing"

	"git.sr.ht/~klahr/zv/internal/updates"
)

func TestParseRecognisesVersionHeadings(t *testing.T) {
	src := `# Changelog

Header preamble that should be ignored.

## [Unreleased]

- something planned

## [0.2.0] - 2026-05-15

### Added

- A second release.

## [0.1.0] - 2026-04-30

First release.

[Unreleased]: https://example/log/master
[0.2.0]: https://example/refs/v0.2.0
[0.1.0]: https://example/refs/v0.1.0
`
	cl := updates.Parse(src)
	if got, want := len(cl.Releases), 2; got != want {
		t.Fatalf("releases: got %d, want %d", got, want)
	}
	first := cl.Releases[0]
	if first.Version.String() != "0.2.0" {
		t.Errorf("first.Version = %q, want 0.2.0", first.Version.String())
	}
	if first.Date.Format("2006-01-02") != "2026-05-15" {
		t.Errorf("first.Date = %v, want 2026-05-15", first.Date)
	}
	if !strings.Contains(first.NotesMD, "second release") {
		t.Errorf("first.NotesMD missing body: %q", first.NotesMD)
	}
	if strings.Contains(first.NotesMD, "[0.2.0]: https") {
		t.Errorf("first.NotesMD bled into 0.1.0 / link refs: %q", first.NotesMD)
	}
}

func TestParseSortsReleasesNewestFirst(t *testing.T) {
	// Authors occasionally write older entries above newer ones in a
	// hand-edited file; the parser should recover the right order.
	src := `## [0.1.0] - 2026-01-01

old.

## [1.0.0] - 2026-04-01

new.
`
	cl := updates.Parse(src)
	if len(cl.Releases) != 2 {
		t.Fatalf("releases: %d", len(cl.Releases))
	}
	if cl.Releases[0].Version.String() != "1.0.0" {
		t.Errorf("expected newest-first, got %q first", cl.Releases[0].Version.String())
	}
}

func TestParseTolerantOfMissingDate(t *testing.T) {
	// Some authors omit the date on draft entries before tagging.
	src := `## [0.2.0]

draft.
`
	cl := updates.Parse(src)
	if len(cl.Releases) != 1 {
		t.Fatalf("releases: %d", len(cl.Releases))
	}
	if !cl.Releases[0].Date.IsZero() {
		t.Errorf("Date = %v, want zero", cl.Releases[0].Date)
	}
}

func TestParseSkipsUnreleasedSection(t *testing.T) {
	src := `## [Unreleased]

- in flight

## [0.1.0] - 2026-04-30

shipped.
`
	cl := updates.Parse(src)
	if len(cl.Releases) != 1 {
		t.Fatalf("releases: got %d, want 1 (Unreleased excluded)", len(cl.Releases))
	}
	if cl.Releases[0].Version.String() != "0.1.0" {
		t.Errorf("got %q, want 0.1.0", cl.Releases[0].Version.String())
	}
}

func TestSectionsNewerThan(t *testing.T) {
	src := `## [1.1.0] - 2026-06-01

newest.

## [1.0.1] - 2026-05-15

middle.

## [1.0.0] - 2026-05-01

oldest.
`
	cl := updates.Parse(src)
	current, ok := updates.ParseSemver("1.0.0")
	if !ok {
		t.Fatal("ParseSemver(1.0.0)")
	}
	got := cl.SectionsNewerThan(current)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Version.String() != "1.1.0" || got[1].Version.String() != "1.0.1" {
		t.Errorf("ordering wrong: %q %q", got[0].Version.String(), got[1].Version.String())
	}
}
