package admin

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func standingsPages(t *testing.T, d recapPDFData, fingerprint string) int {
	t.Helper()
	var buf bytes.Buffer
	if err := writeStandingsPDF(&buf, d, fingerprint); err != nil {
		t.Fatalf("writeStandingsPDF: %v", err)
	}
	if !strings.HasPrefix(buf.String(), pdfMagic) {
		t.Fatalf("not a PDF")
	}
	// When ZV_TEST_STANDINGS_PDF_OUT names a directory, each sheet is
	// written there to look at.
	if dir := os.Getenv("ZV_TEST_STANDINGS_PDF_OUT"); dir != "" {
		name := fmt.Sprintf("%s/standings-%s-%d.pdf", dir, d.Lang, len(d.Teams))
		_ = os.WriteFile(name, buf.Bytes(), 0o644)
	}
	return strings.Count(buf.String(), "/Type /Page") - strings.Count(buf.String(), "/Type /Pages")
}

func standingsTeams(n int) []recapPDFTeam {
	var teams []recapPDFTeam
	for i := range n {
		teams = append(teams, recapPDFTeam{Rank: i + 1, Name: fmt.Sprintf("Lag %d — Måsarna från Öckerö", i+1), Points: float64(100-i) + 0.5,
			CompletedAt: "2026-05-01 11:45:00", Elapsed: time.Duration(90+i) * time.Minute})
	}
	return teams
}

// A handful of teams fits one sheet with the fingerprint under them; a
// big event runs onto another; and either language, finished or not,
// with teams left unranked, renders.
func TestWriteStandingsPDF(t *testing.T) {
	t.Parallel()
	base := recapPDFData{OrgName: "Acme", EventName: "Höstkampen", AdventureTitle: "Old Town", Graph: true, State: "completed",
		EventStartedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC), EventEndedAt: time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC),
		GeneratedAt: time.Date(2026, 5, 1, 13, 5, 0, 0, time.UTC), Accent: "#1b6b43"}
	fp := strings.Repeat("ab", 32)
	for _, lang := range []string{"", "sv"} {
		d := base
		d.Lang = lang
		d.Teams = append(standingsTeams(6), recapPDFTeam{Name: "Gave up", WithdrawnAt: "2026-05-01 11:00:00"})
		if n := standingsPages(t, d, fp); n != 1 {
			t.Errorf("%q, 6 teams: %d pages, want 1", lang, n)
		}
		d.Teams = standingsTeams(60)
		if n := standingsPages(t, d, fp); n != 3 {
			t.Errorf("%q, 60 teams: %d pages, want 3", lang, n)
		}
	}
	running := base
	running.State, running.Teams = "running", standingsTeams(3)
	running.Teams[2].CompletedAt, running.Teams[2].Elapsed = "", 0
	if n := standingsPages(t, running, ""); n != 1 {
		t.Errorf("running: %d pages, want 1", n)
	}
	empty := base
	if n := standingsPages(t, empty, ""); n != 1 {
		t.Errorf("no teams: %d pages, want 1", n)
	}
}

func TestRecapSwedish(t *testing.T) {
	t.Parallel()
	d := recapPDFData{Lang: "sv", EventStartedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC), EventEndedAt: time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)}
	if got := recapDateLine(d); got != "1 maj – 3 maj 2026" {
		t.Errorf("date line = %q", got)
	}
	for dur, want := range map[time.Duration]string{
		83 * time.Minute:               "1 h 23 min",
		12*time.Minute + 4*time.Second: "12 min 04 s",
		47 * time.Second:               "47 s",
	} {
		if got := d.hms(dur); got != want {
			t.Errorf("hms(%v) = %q, want %q", dur, got, want)
		}
	}
	if got := d.num(12.5); got != "12,5" {
		t.Errorf("num = %q", got)
	}
	if got := d.t("Team standings"); got != "Lagens placeringar" {
		t.Errorf("t = %q", got)
	}
}
