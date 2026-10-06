package admin

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"
)

// mintTinyJPEG returns a small valid JPEG byte slice for tests that
// exercise the org-logo embed path. fpdf needs something it can
// actually decode — a stub of magic bytes alone wouldn't survive.
func mintTinyJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 16), B: 64, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}
	return buf.Bytes()
}

// pdfMagic is the four-byte signature every valid PDF 1.x stream
// starts with. Used as a cheap "did the renderer actually produce a
// PDF?" sanity check in the writeRecapPDF tests.
const pdfMagic = "%PDF-"

func TestWriteRecapPDF_HappyPath(t *testing.T) {
	t.Parallel()
	d := recapPDFData{
		OrgName:        "Acme Corp",
		AdventureTitle: "Stockholm Old Town",
		AdventureSlug:  "stockholm-old-town",
		EventID:        42,
		EventStartedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		EventEndedAt:   time.Date(2026, 5, 1, 12, 30, 0, 0, time.UTC),
		Accent:         "#ad1357",
		PageCount:      10,
		GeneratedAt:    time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC),
		Teams: []recapPDFTeam{
			{Rank: 1, Name: "Alpha", Members: "Alice, Bob", Solved: 10, CompletedAt: "2026-05-01 11:45:00", Elapsed: 1*time.Hour + 45*time.Minute},
			{Rank: 2, Name: "Beta", Members: "Carol, Dave", Solved: 9, CompletedAt: "", Elapsed: 0},
			{Rank: 3, Name: "Gamma", Members: "Eve", Solved: 5, CompletedAt: "", Elapsed: 0},
		},
	}
	var buf bytes.Buffer
	if err := writeRecapPDF(&buf, d); err != nil {
		t.Fatalf("writeRecapPDF: %v", err)
	}
	if !strings.HasPrefix(buf.String(), pdfMagic) {
		t.Errorf("output does not start with PDF magic; first bytes = %q", buf.String()[:min(8, buf.Len())])
	}
	if buf.Len() < 1024 {
		t.Errorf("PDF suspiciously small (%d bytes) — render likely incomplete", buf.Len())
	}
	// Optional debug: when ZV_TEST_RECAP_PDF_OUT is set, dump the
	// rendered PDF to that path so the developer can open it in a
	// reader to confirm the visual layout looks right. The test
	// still passes either way; missing env = no side effect.
	if path := os.Getenv("ZV_TEST_RECAP_PDF_OUT"); path != "" {
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Logf("could not write %s: %v", path, err)
		} else {
			t.Logf("wrote sample PDF to %s (%d bytes)", path, buf.Len())
		}
	}
}

func TestWriteRecapPDF_EmptyTeams(t *testing.T) {
	t.Parallel()
	d := recapPDFData{
		OrgName:        "Acme Corp",
		AdventureTitle: "Empty event",
		EventStartedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		PageCount:      5,
		GeneratedAt:    time.Now(),
	}
	var buf bytes.Buffer
	if err := writeRecapPDF(&buf, d); err != nil {
		t.Fatalf("empty-teams render: %v", err)
	}
	if !strings.HasPrefix(buf.String(), pdfMagic) {
		t.Fatalf("not a PDF")
	}
}

func TestWriteRecapPDF_LongNamesAndUnicode(t *testing.T) {
	t.Parallel()
	d := recapPDFData{
		OrgName:        "Företaget AB", // Swedish chars
		AdventureTitle: "En väldigt lång äventyrstitel som testar radbrytning",
		EventStartedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		PageCount:      3,
		GeneratedAt:    time.Now(),
		Teams: []recapPDFTeam{
			{
				Rank:    1,
				Name:    "Lag med ett rejält långt namn som garanterat överskrider kolumnbredden",
				Members: "Ångström, Östergård, Müller, García, Sørensen",
				Solved:  3,
				Elapsed: 47 * time.Minute,
			},
		},
	}
	var buf bytes.Buffer
	if err := writeRecapPDF(&buf, d); err != nil {
		t.Fatalf("unicode render: %v", err)
	}
	if !strings.HasPrefix(buf.String(), pdfMagic) {
		t.Fatalf("not a PDF")
	}
}

// Helvetica in the PDF reads Windows-1252; UTF-8 bytes passed straight
// through print "Gröna" as "GrÃ¶na".
func TestRecapTextIsWindows1252(t *testing.T) {
	t.Parallel()
	text := recapText(fpdf.New("P", "mm", pdfPageSize, ""))
	if got, want := text("Operation Gröna linjen – Åre"), "Operation Gr\xf6na linjen - \xc5re"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	// The org line is upper-cased: that has to happen before, on UTF-8.
	if got, want := text(strings.ToUpper("Företaget AB")), "F\xd6RETAGET AB"; got != want {
		t.Errorf("upper-cased = %q, want %q", got, want)
	}
}

func TestWriteRecapPDF_WithLogo(t *testing.T) {
	t.Parallel()
	d := recapPDFData{
		OrgName:        "Acme Corp",
		AdventureTitle: "Stockholm Old Town",
		EventStartedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		EventEndedAt:   time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		Accent:         "#1976d2",
		PageCount:      5,
		GeneratedAt:    time.Now(),
		LogoBytes:      mintTinyJPEG(t),
		LogoMIME:       "image/jpeg",
		Teams: []recapPDFTeam{
			{Rank: 1, Name: "Alpha", Members: "Alice, Bob", Solved: 5, Elapsed: 90 * time.Minute},
		},
	}
	var buf bytes.Buffer
	if err := writeRecapPDF(&buf, d); err != nil {
		t.Fatalf("writeRecapPDF: %v", err)
	}
	if !strings.HasPrefix(buf.String(), pdfMagic) {
		t.Fatalf("output is not a PDF")
	}
	// PDFs that embed an image are noticeably larger than text-only
	// reports — a sanity check that the logo actually made it in.
	// Without the logo the same fixture renders ~2.4 KB; with the
	// 32×16 JPEG embedded we expect at least 3 KB.
	if buf.Len() < 3000 {
		t.Errorf("logo path PDF suspiciously small (%d bytes); logo may have been dropped", buf.Len())
	}
	if path := os.Getenv("ZV_TEST_RECAP_PDF_LOGO_OUT"); path != "" {
		_ = os.WriteFile(path, buf.Bytes(), 0o644)
		t.Logf("wrote logo-variant PDF to %s (%d bytes)", path, buf.Len())
	}
}

func TestWriteRecapPDF_NoStartTime(t *testing.T) {
	t.Parallel()
	// Event that hasn't started yet — the cover-page date line should
	// just render empty rather than blowing up on a zero time.
	d := recapPDFData{
		OrgName:        "Acme Corp",
		AdventureTitle: "Scheduled event",
		PageCount:      3,
		GeneratedAt:    time.Now(),
	}
	var buf bytes.Buffer
	if err := writeRecapPDF(&buf, d); err != nil {
		t.Fatalf("no-start render: %v", err)
	}
}

func TestHexToRGB(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in       string
		want     [3]int
		fallback bool
	}{
		{"#ad1357", [3]int{0xad, 0x13, 0x57}, false},
		{"ad1357", [3]int{0xad, 0x13, 0x57}, false},
		{"0xad1357", [3]int{0xad, 0x13, 0x57}, false},
		{"#AD1357", [3]int{0xad, 0x13, 0x57}, false},
		{"", [3]int{1, 2, 3}, true},
		{"nope", [3]int{1, 2, 3}, true},
		{"#abcd", [3]int{1, 2, 3}, true},    // wrong length
		{"#zzzzzz", [3]int{1, 2, 3}, true},  // invalid hex
		{"#abcdefg", [3]int{1, 2, 3}, true}, // too long
	}
	for _, c := range cases {
		r, g, b := hexToRGB(c.in, 1, 2, 3)
		if c.fallback {
			if r != 1 || g != 2 || b != 3 {
				t.Errorf("hexToRGB(%q) should fall back, got (%d,%d,%d)", c.in, r, g, b)
			}
			continue
		}
		if r != c.want[0] || g != c.want[1] || b != c.want[2] {
			t.Errorf("hexToRGB(%q) = (%d,%d,%d), want (%d,%d,%d)",
				c.in, r, g, b, c.want[0], c.want[1], c.want[2])
		}
	}
}

func TestTruncateText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hell…"},
		{"Ångström team", 6, "Ångst…"}, // rune-counted, not byte-counted
		{"x", 0, ""},
		{"", 5, ""},
	}
	for _, c := range cases {
		got := truncateText(c.in, c.max)
		if got != c.want {
			t.Errorf("truncateText(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestFormatHMS(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "—"},
		{-5 * time.Second, "—"},
		{47 * time.Second, "47s"},
		{12*time.Minute + 4*time.Second, "12m 04s"},
		{1*time.Hour + 23*time.Minute, "1h 23m"},
		{2 * time.Hour, "2h 00m"},
	}
	for _, c := range cases {
		got := formatHMS(c.in)
		if got != c.want {
			t.Errorf("formatHMS(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRecapDateLine(t *testing.T) {
	t.Parallel()
	zero := time.Time{}
	may1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	may1evening := time.Date(2026, 5, 1, 22, 0, 0, 0, time.UTC)
	may3 := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	jan1nextYear := time.Date(2027, 1, 1, 10, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		d    recapPDFData
		want string
	}{
		{"no start", recapPDFData{}, ""},
		{"start only", recapPDFData{EventStartedAt: may1}, "May 1, 2026"},
		{"same day", recapPDFData{EventStartedAt: may1, EventEndedAt: may1evening}, "May 1, 2026"},
		{"same year", recapPDFData{EventStartedAt: may1, EventEndedAt: may3}, "May 1 – May 3, 2026"},
		{"cross year", recapPDFData{EventStartedAt: may1, EventEndedAt: jan1nextYear}, "May 1, 2026 – January 1, 2027"},
		{"end zero falls back to start only", recapPDFData{EventStartedAt: may1, EventEndedAt: zero}, "May 1, 2026"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := recapDateLine(c.d)
			if got != c.want {
				t.Errorf("recapDateLine = %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseRecapTimestamp(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in    string
		ok    bool
		check func(t time.Time) bool
	}{
		{"", false, nil},
		{"   ", false, nil},
		{"2026-05-01 10:30:00", true, func(tm time.Time) bool {
			return tm.Year() == 2026 && tm.Month() == 5 && tm.Day() == 1 && tm.Hour() == 10
		}},
		{"2026-05-01T10:30:00Z", true, func(tm time.Time) bool {
			return tm.Year() == 2026 && tm.Hour() == 10
		}},
		{"not-a-time", false, nil},
	}
	for _, c := range cases {
		tm, ok := parseRecapTimestamp(c.in)
		if ok != c.ok {
			t.Errorf("parseRecapTimestamp(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && c.check != nil && !c.check(tm) {
			t.Errorf("parseRecapTimestamp(%q) parsed wrong: %v", c.in, tm)
		}
	}
}

func TestSafeText(t *testing.T) {
	t.Parallel()
	in := "Lag · with “fancy” quotes — and an ellipsis…"
	want := `Lag - with "fancy" quotes - and an ellipsis...`
	if got := safeText(in); got != want {
		t.Errorf("safeText = %q, want %q", got, want)
	}
}

// Pins every PDF this package emits to A4. Failing this test means
// someone added a new PDF call site without going through the
// pdfPageSize constant, OR fpdf changed its MediaBox encoding —
// either way, a human should look. A4 is mandatory because the
// buyer base prints these in EU offices; Letter (US) would
// chop the margins.
func TestWriteRecapPDF_PageSizeIsA4(t *testing.T) {
	t.Parallel()
	ref := func(size string) []byte {
		var buf bytes.Buffer
		p := newPDFRef(size)
		if err := p.Output(&buf); err != nil {
			t.Fatalf("ref %s: %v", size, err)
		}
		return mediaBoxBytes(t, buf.Bytes())
	}
	a4 := ref("A4")
	letter := ref("Letter")
	if bytes.Equal(a4, letter) {
		t.Fatalf("A4 and Letter MediaBoxes are identical — fpdf changed?")
	}

	d := recapPDFData{
		OrgName:        "Acme",
		AdventureTitle: "T",
		AdventureSlug:  "t",
		EventID:        1,
		Accent:         "#ad1357",
		PageCount:      1,
		GeneratedAt:    time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC),
	}
	var buf bytes.Buffer
	if err := writeRecapPDF(&buf, d); err != nil {
		t.Fatalf("writeRecapPDF: %v", err)
	}
	got := mediaBoxBytes(t, buf.Bytes())
	if !bytes.Equal(got, a4) {
		t.Errorf("recap MediaBox = %q, want A4 %q (Letter would be %q)", got, a4, letter)
	}
}

// mediaBoxBytes extracts the first MediaBox declaration from a PDF
// stream so the assertion above is decoupled from fpdf's exact
// numeric formatting (different fpdf releases trim trailing zeros
// differently). The returned slice excludes the surrounding "/MediaBox "
// prefix and stops at the closing bracket.
func mediaBoxBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	marker := []byte("/MediaBox")
	i := bytes.Index(b, marker)
	if i < 0 {
		t.Fatalf("no MediaBox in PDF stream")
	}
	rest := b[i+len(marker):]
	end := bytes.IndexByte(rest, ']')
	if end < 0 {
		t.Fatalf("MediaBox not closed")
	}
	return bytes.TrimSpace(rest[:end+1])
}

// newPDFRef returns a minimal one-page fpdf doc at the requested
// page size. Only used by TestWriteRecapPDF_PageSizeIsA4 to compute
// reference MediaBox bytes for the comparison.
func newPDFRef(size string) interface {
	Output(io.Writer) error
} {
	p := fpdf.New("P", "mm", size, "")
	p.AddPage()
	return p
}

// Regression test for the unstarted-event recap: when no team has
// completed a run, the "Longest run" stat falls back to a dash. The
// initial implementation passed a UTF-8 em dash directly, which the
// fpdf core fonts (Latin-1) interpreted byte-by-byte and rendered as
// "â€”" mojibake. Same shape applied to the multi-day en-dash in
// recapDateLine. Both call sites must now go through safeText, so the
// emitted PDF stream contains no UTF-8 em-dash sequence (e2 80 94).
func TestWriteRecapPDF_NoMojibakeOnDashes(t *testing.T) {
	t.Parallel()
	multiDay := recapPDFData{
		OrgName:        "Acme",
		AdventureTitle: "T",
		AdventureSlug:  "t",
		EventID:        1,
		EventStartedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		EventEndedAt:   time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC),
		Accent:         "#ad1357",
		PageCount:      3,
		GeneratedAt:    time.Date(2026, 5, 3, 13, 0, 0, 0, time.UTC),
		// No completed teams → longest == 0 → the "—" fallback fires.
		Teams: []recapPDFTeam{
			{Rank: 1, Name: "Alpha", Members: "Alice", Solved: 1, Elapsed: 0},
		},
	}
	var buf bytes.Buffer
	if err := writeRecapPDF(&buf, multiDay); err != nil {
		t.Fatalf("writeRecapPDF: %v", err)
	}
	utf8EmDash := []byte{0xe2, 0x80, 0x94}
	utf8EnDash := []byte{0xe2, 0x80, 0x93}
	if bytes.Contains(buf.Bytes(), utf8EmDash) {
		t.Errorf("PDF stream contains raw UTF-8 em-dash bytes (mojibake risk)")
	}
	if bytes.Contains(buf.Bytes(), utf8EnDash) {
		t.Errorf("PDF stream contains raw UTF-8 en-dash bytes (mojibake risk)")
	}
}

// min — Go 1.21+ has it builtin; this lets the test file build on
// older toolchains as well.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
