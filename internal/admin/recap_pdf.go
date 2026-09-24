package admin

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/play"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"bytes"
	"errors"

	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/storage"

	"github.com/go-pdf/fpdf"
)

// ExportEventRecapPDF builds a single-page (occasionally two-page for
// large events) PDF report of an event's outcome and writes it as
// `event-<id>-recap.pdf`. Sister to ExportEventResults (CSV) — same
// data, different audience: the CSV is for spreadsheet drilldown, the
// PDF is the polished artifact an arranger forwards to their boss /
// the participating team / their HR Slack.
//
// The page is branded with the adventure's accent colour and stamped
// with the operator's organisation name, so the file lands as
// "Acme Corp · Team Building 2026" rather than a generic Zonventure
// export.
func (h *Handlers) ExportEventRecapPDF(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	orgID, ok := h.authorizeEvent(w, r, eventID)
	if !ok {
		return
	}
	data, err := h.collectRecapPDFData(r.Context(), eventID, orgID)
	if err != nil {
		logx.Warn("admin.recap_pdf.lookup", "event", eventID, "err", err.Error())
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="event-%d-recap.pdf"`, eventID))
	if err := writeRecapPDF(w, data); err != nil {
		// Headers are already on the wire, so we can't switch to a
		// JSON error here — a partial body is the only signal the
		// download was truncated. Logging it lets ops correlate a
		// support ticket back to the cause.
		logx.Warn("admin.recap_pdf.render", "event", eventID, "err", err.Error())
	}
}

// recapPDFData is the full payload the builder consumes. Kept
// separate from the SQL collection so writeRecapPDF can be unit-
// tested without a database.
type recapPDFData struct {
	OrgName        string
	AdventureTitle string
	AdventureSlug  string
	EventID        int64
	EventStartedAt time.Time
	EventEndedAt   time.Time
	// Accent is a hex colour like "#ad1357". Empty falls back to the
	// Zonventure brand accent.
	Accent    string
	PageCount int // enabled puzzle count
	// Graph is set for an event that plays a graph: teams rank by
	// points (play.Ahead), as in the CSV, and the table shows points.
	Graph       bool
	Teams       []recapPDFTeam
	GeneratedAt time.Time
	// LogoBytes + LogoMIME, when present, are the operator's brand
	// mark embedded on the cover page in place of the text-only
	// org-name strip. PNG/JPEG only — see admin/org_logo.go for the
	// upload-side restrictions. Nil = no logo configured, fall back
	// to text.
	LogoBytes []byte
	LogoMIME  string
}

type recapPDFTeam struct {
	Rank        int
	Name        string
	Members     string
	Solved      int
	Points      float64 // graph events only
	CompletedAt string  // raw stamp from DB; "" when team didn't finish
	WithdrawnAt string  // raw stamp; non-empty teams render in their own group
	Elapsed     time.Duration
}

func (h *Handlers) collectRecapPDFData(ctx context.Context, eventID, orgID int64) (recapPDFData, error) {
	var d recapPDFData
	d.EventID = eventID
	d.GeneratedAt = time.Now().UTC()

	// Org name — what the cover page leads with. Fallback to
	// "Organization #N" if we somehow can't resolve, rather than
	// erroring; the PDF is still useful without the polished header.
	if err := h.DB.QueryRowContext(ctx,
		`SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&d.OrgName); err != nil {
		if err != sql.ErrNoRows {
			return d, fmt.Errorf("org name: %w", err)
		}
		d.OrgName = fmt.Sprintf("Organization #%d", orgID)
	}

	var (
		slug         string
		startedRaw   sql.NullString
		completedRaw sql.NullString
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT adventure_slug, started_at, completed_at
		FROM events WHERE id = $1
	`, eventID).Scan(&slug, &startedRaw, &completedRaw); err != nil {
		return d, fmt.Errorf("event: %w", err)
	}
	d.AdventureSlug = slug
	if t, ok := parseRecapTimestamp(startedRaw.String); ok {
		d.EventStartedAt = t
	}
	if t, ok := parseRecapTimestamp(completedRaw.String); ok {
		d.EventEndedAt = t
	}

	adv := h.Lib.Get(slug)
	if adv == nil {
		return d, fmt.Errorf("adventure %q not loaded", slug)
	}
	d.AdventureTitle = adv.Meta.Title
	if !adv.Colors.Empty() {
		d.Accent = adv.Colors.Accent
	}
	if uses, err := play.UsesGraph(ctx, h.DB, eventID); err != nil {
		return d, err
	} else if uses {
		h.addRecapLogo(ctx, &d, orgID)
		return d, h.collectGraphRecap(ctx, &d, eventID)
	}
	for _, p := range adv.Pages {
		if !p.Disabled {
			d.PageCount++
		}
	}
	enabled := make([]int, 0, len(adv.Pages))
	for _, p := range adv.Pages {
		if !p.Disabled {
			enabled = append(enabled, p.Index)
		}
	}

	// Same ordering as ExportEventResults (CSV) so both exports rank
	// teams identically. A stakeholder cross-checking the PDF against
	// the spreadsheet should never see disagreement.
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name, t.current_page_index,
		       COALESCE(t.completed_at, ''),
		       COALESCE(t.withdrawn_at, '')
		FROM teams t
		WHERE t.event_id = $1
		ORDER BY
		  -- Group order: finishers first, then unfinished (still active),
		  -- then withdrawn at the bottom. Each group sorts internally.
		  (CASE
		     WHEN t.completed_at IS NOT NULL THEN 0
		     WHEN t.withdrawn_at IS NULL    THEN 1
		     ELSE 2
		   END),
		  t.completed_at ASC,
		  t.current_page_index DESC,
		  COALESCE(
		    (SELECT MAX(created_at) FROM event_activity
		     WHERE event_id = t.event_id AND team_id = t.id AND kind = 'answer.right'),
		    t.created_at
		  ) ASC,
		  t.created_at ASC
	`, eventID)
	if err != nil {
		return d, fmt.Errorf("teams: %w", err)
	}
	defer rows.Close()

	h.addRecapLogo(ctx, &d, orgID)

	rank := 0
	for rows.Next() {
		var (
			teamID    int64
			name      string
			rawCursor int
			completed string
			withdrawn string
		)
		if err := rows.Scan(&teamID, &name, &rawCursor, &completed, &withdrawn); err != nil {
			return d, err
		}
		rank++
		members, _ := h.teamMemberNames(ctx, teamID)
		// current_page_index is the team's monotonic count of solves
		// (see results.go for the same treatment under lanes). Cap at
		// the adventure's enabled page count for display sanity.
		solved := rawCursor
		if solved > d.PageCount {
			solved = d.PageCount
		}
		if solved < 0 {
			solved = 0
		}
		_ = enabled // kept above for parity with the legacy code path; no longer load-bearing
		var elapsed time.Duration
		if completed != "" && !d.EventStartedAt.IsZero() {
			if finished, ok := parseRecapTimestamp(completed); ok {
				elapsed = finished.Sub(d.EventStartedAt).Round(time.Second)
			}
		}
		d.Teams = append(d.Teams, recapPDFTeam{
			Rank:        rank,
			Name:        name,
			Members:     members,
			Solved:      solved,
			CompletedAt: completed,
			WithdrawnAt: withdrawn,
			Elapsed:     elapsed,
		})
	}
	return d, rows.Err()
}

// addRecapLogo embeds the org's logo on the cover. Best-effort: a missing
// or unreadable logo just means the text-only header; the report does
// not fail over a flaky asset read.
func (h *Handlers) addRecapLogo(ctx context.Context, d *recapPDFData, orgID int64) {
	if logoBytes, logoMime, err := h.Storage.GetOrgLogo(ctx, orgID); err == nil {
		d.LogoBytes = logoBytes
		d.LogoMIME = logoMime
	} else if !errors.Is(err, storage.ErrNotFound) {
		logx.Warn("admin.recap_pdf.logo", "org", orgID, "err", err.Error())
	}
}

// collectGraphRecap ranks a graph event's teams as the CSV does
// (exportGraphResults): by the shared ranking, level teams sharing a
// place; then the teams that withdrew or never started, unranked.
func (h *Handlers) collectGraphRecap(ctx context.Context, d *recapPDFData, eventID int64) error {
	d.Graph = true
	var graphID sql.NullInt64
	if err := h.DB.QueryRowContext(ctx, `SELECT graph_id FROM events WHERE id = $1`, eventID).Scan(&graphID); err != nil {
		return err
	}
	if graphID.Valid {
		st, err := graphstore.ByID(ctx, h.DB, graphID.Int64)
		if err != nil {
			return err
		}
		for _, n := range st.Graph.Nodes {
			if n.Type == graph.Puzzle {
				d.PageCount++
			}
		}
	}
	e := h.Engine
	if e == nil {
		e = play.New(h.DB)
	}
	ranked, err := e.EventRanking(ctx, eventID)
	if err != nil {
		return err
	}
	for _, t := range ranked {
		rank := 1
		for _, o := range ranked {
			if play.Ahead(o, t) {
				rank++
			}
		}
		members, _ := h.teamMemberNames(ctx, t.TeamID)
		row := recapPDFTeam{Rank: rank, Name: t.Name, Members: members, Points: t.Score}
		if t.Done {
			row.CompletedAt = t.CompletedAt.UTC().Format("2006-01-02 15:04:05")
			if !d.EventStartedAt.IsZero() {
				row.Elapsed = t.CompletedAt.Sub(d.EventStartedAt).Round(time.Second)
			}
		}
		d.Teams = append(d.Teams, row)
	}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.name, COALESCE(t.withdrawn_at, '')
		FROM teams t
		WHERE t.event_id = $1 AND (t.withdrawn_at IS NOT NULL OR NOT EXISTS (SELECT 1 FROM team_graph_state s WHERE s.team_id = t.id))
		ORDER BY LOWER(t.name)`, eventID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row recapPDFTeam
		if err := rows.Scan(&row.Name, &row.WithdrawnAt); err != nil {
			return err
		}
		d.Teams = append(d.Teams, row)
	}
	return rows.Err()
}

// parseRecapTimestamp tolerates both Postgres' "YYYY-MM-DD HH:MM:SS"
// shape (what now_text() emits) and full RFC3339 strings (what some
// older imports may carry).
func parseRecapTimestamp(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999999",
		time.RFC3339,
		time.RFC3339Nano,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// pdfPageSize is the page size every PDF this package emits MUST use.
// A4 is the standard for our buyer base (Sweden / EU); Letter would
// surprise the customer who prints a recap and finds the trim wrong.
// Any future PDF added here goes through this constant — the page-
// size assertion in writeRecapPDF tests pins this to A4 across
// fpdf upgrades.
const pdfPageSize = "A4"

// writeRecapPDF emits the report onto w. Pure rendering — no DB,
// no http.Request — so it's straightforward to unit-test by feeding
// in a fixture and asserting the result is a non-trivial PDF.
func writeRecapPDF(w io.Writer, d recapPDFData) error {
	pdf := fpdf.New("P", "mm", pdfPageSize, "")
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 18)
	// Footer callback fires once per page, so we don't have to
	// SetY(-15) at the end of the body (which on a near-full page
	// would otherwise spill onto a fresh sheet just to land the
	// footer line). Using fpdf's hook is the idiomatic way to keep
	// pagination predictable.
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetTextColor(150, 150, 150)
		footer := fmt.Sprintf("Generated %s  ·  powered by Zonventure",
			d.GeneratedAt.Format("2006-01-02 15:04 UTC"))
		pdf.CellFormat(0, 5, safeText(footer), "", 0, "C", false, 0, "")
	})
	pdf.AddPage()

	const (
		pageW       = 210.0
		marginX     = 15.0
		usableW     = pageW - 2*marginX
		brandStripH = 8.0
	)
	accentR, accentG, accentB := hexToRGB(d.Accent, 173, 19, 87)

	// Brand strip — full-width accent bar at the very top, the
	// arranger's customer sees this colour first and matches it to
	// the in-app live board / play page.
	pdf.SetFillColor(accentR, accentG, accentB)
	pdf.Rect(0, 0, pageW, brandStripH, "F")

	// Cover header. When the org has uploaded a logo, render it in
	// place of the uppercase text strip; the org name still appears
	// implicitly via the brand mark, and the adventure title carries
	// the rest of the identity. Logo bytes are JPEG (re-encoded by
	// imageutil.Shrink at upload time), so fpdf reads them with
	// ImageType "JPG"; failures degrade to the text fallback rather
	// than aborting the report.
	pdf.SetY(18)
	logoRendered := false
	if len(d.LogoBytes) > 0 {
		const logoH = 14.0 // mm; bounded so it never overwhelms the page
		const logoMaxW = 70.0
		opts := fpdf.ImageOptions{ImageType: "JPG", ReadDpi: false}
		// RegisterImageOptionsReader hands fpdf the bytes once,
		// keyed by name. The caller can then call ImageOptions
		// repeatedly without re-decoding.
		const logoName = "org-logo"
		pdf.RegisterImageOptionsReader(logoName, opts, bytes.NewReader(d.LogoBytes))
		// Auto-fit: pass 0 for width, fpdf scales to keep the height.
		// Then check the rendered width and re-render at logoMaxW
		// if it overshot — fpdf doesn't expose pre-render dims so
		// the simplest path is "render, look at GetX, decide".
		pdf.ImageOptions(logoName, marginX, pdf.GetY(), 0, logoH, false, opts, 0, "")
		// pdf.GetImageInfo lets us check actual dimensions for
		// width-cap enforcement so a wide wordmark doesn't bleed
		// into the page margin on the right.
		if info := pdf.GetImageInfo(logoName); info != nil {
			renderedW := logoH * info.Width() / info.Height()
			if renderedW > logoMaxW {
				// Wide wordmark — re-render at the width cap, with
				// height auto so the aspect stays right.
				pdf.SetXY(marginX, pdf.GetY())
				pdf.ImageOptions(logoName, marginX, pdf.GetY(), logoMaxW, 0, false, opts, 0, "")
			}
		}
		pdf.SetY(pdf.GetY() + logoH + 4)
		logoRendered = true
	}
	if !logoRendered {
		pdf.SetFont("Helvetica", "B", 10)
		pdf.SetTextColor(120, 120, 120)
		pdf.CellFormat(usableW, 5, strings.ToUpper(safeText(d.OrgName)), "", 1, "L", false, 0, "")
		pdf.Ln(2)
	}
	pdf.SetFont("Helvetica", "B", 22)
	pdf.SetTextColor(40, 40, 40)
	pdf.MultiCell(usableW, 8.5, safeText(d.AdventureTitle), "", "L", false)
	pdf.SetFont("Helvetica", "", 11)
	pdf.SetTextColor(120, 120, 120)
	pdf.CellFormat(usableW, 6, safeText(recapDateLine(d)), "", 1, "L", false, 0, "")
	pdf.Ln(6)

	// Stats grid: 4 boxes summarising the event.
	const (
		statBoxH = 22.0
		statGap  = 3.0
	)
	statW := (usableW - 3*statGap) / 4
	statY := pdf.GetY()
	teamsCount := len(d.Teams)
	completedCount := 0
	var longest time.Duration
	for _, t := range d.Teams {
		if t.CompletedAt != "" {
			completedCount++
		}
		if t.Elapsed > longest {
			longest = t.Elapsed
		}
	}
	stat := func(i int, label, value string) {
		x := marginX + float64(i)*(statW+statGap)
		pdf.SetFillColor(248, 248, 248)
		pdf.SetDrawColor(225, 225, 225)
		pdf.RoundedRect(x, statY, statW, statBoxH, 2.5, "1234", "FD")
		pdf.SetXY(x, statY+3)
		pdf.SetFont("Helvetica", "B", 16)
		pdf.SetTextColor(accentR, accentG, accentB)
		pdf.CellFormat(statW, 8, safeText(value), "", 0, "C", false, 0, "")
		pdf.SetXY(x, statY+13)
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(120, 120, 120)
		pdf.CellFormat(statW, 6, safeText(label), "", 0, "C", false, 0, "")
	}
	stat(0, "Teams", strconv.Itoa(teamsCount))
	stat(1, "Completed", fmt.Sprintf("%d / %d", completedCount, teamsCount))
	stat(2, "Puzzles", strconv.Itoa(d.PageCount))
	if longest > 0 {
		stat(3, "Longest run", formatHMS(longest))
	} else {
		stat(3, "Longest run", "—")
	}
	pdf.SetY(statY + statBoxH + 8)

	// Team rankings.
	pdf.SetFont("Helvetica", "B", 14)
	pdf.SetTextColor(40, 40, 40)
	pdf.CellFormat(usableW, 8, "Team standings", "", 1, "L", false, 0, "")
	pdf.Ln(1)

	// Column widths sum to 180 (= usableW). Tweak together if you
	// add a column.
	cols := []struct {
		w     float64
		hdr   string
		align string
	}{
		{12, "#", "L"},
		{60, "Team", "L"},
		{58, "Members", "L"},
		{20, "Solved", "L"},
		{30, "Time", "L"},
	}
	if d.Graph {
		cols[3].hdr = "Points"
	}
	pdf.SetFillColor(245, 245, 245)
	pdf.SetTextColor(80, 80, 80)
	pdf.SetFont("Helvetica", "B", 9)
	for _, c := range cols {
		pdf.CellFormat(c.w, 7, c.hdr, "B", 0, c.align, true, 0, "")
	}
	pdf.Ln(7)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(40, 40, 40)
	if len(d.Teams) == 0 {
		pdf.SetTextColor(160, 160, 160)
		pdf.SetFont("Helvetica", "I", 9)
		pdf.CellFormat(usableW, 8, "No teams played in this event.", "", 1, "L", false, 0, "")
	}
	// Print a small "Withdrew" header the first time a withdrawn team
	// appears so the reader sees the group break clearly. Same data
	// table; just a faint divider row.
	withdrawnHeaderPrinted := false
	for i, t := range d.Teams {
		if t.WithdrawnAt != "" && !withdrawnHeaderPrinted {
			withdrawnHeaderPrinted = true
			pdf.Ln(2)
			pdf.SetFont("Helvetica", "I", 9)
			pdf.SetTextColor(140, 140, 140)
			pdf.CellFormat(usableW, 6, safeText("Withdrew"), "", 1, "L", false, 0, "")
			pdf.SetFont("Helvetica", "", 9)
			pdf.SetTextColor(40, 40, 40)
		}
		// Faint zebra striping so a long roster stays scannable.
		if i%2 == 1 {
			y := pdf.GetY()
			pdf.SetFillColor(252, 252, 252)
			pdf.Rect(marginX, y, usableW, 6, "F")
		}
		elapsed := "-"
		if t.Elapsed > 0 {
			elapsed = formatHMS(t.Elapsed)
		}
		// Withdrawn row's "Time" column shows "withdrew" rather than
		// the dash, so the column communicates intent rather than
		// "we don't know."
		if t.WithdrawnAt != "" {
			elapsed = "withdrew"
		}
		progress := fmt.Sprintf("%d / %d", t.Solved, d.PageCount)
		if d.Graph {
			progress = strconv.FormatFloat(t.Points, 'f', -1, 64)
		}
		rank := strconv.Itoa(t.Rank)
		if t.Rank == 0 {
			rank = "–" // withdrew or never started: unranked
		}
		row := []string{
			rank,
			truncateText(t.Name, 28),
			truncateText(t.Members, 30),
			progress,
			elapsed,
		}
		// Withdrawn rows render in muted grey so they read as a
		// supporting note rather than a competitor's standing.
		if t.WithdrawnAt != "" {
			pdf.SetTextColor(140, 140, 140)
		}
		for j, c := range cols {
			pdf.CellFormat(c.w, 6, safeText(row[j]), "", 0, c.align, false, 0, "")
		}
		if t.WithdrawnAt != "" {
			pdf.SetTextColor(40, 40, 40)
		}
		pdf.Ln(6)
	}

	return pdf.Output(w)
}

// recapDateLine formats the cover-page subtitle. Same date repeated
// when start and end share a calendar day; "May 1 – May 3, 2026"
// shape when the event spans multiple days; bare start when the
// event hasn't recorded a finish yet.
func recapDateLine(d recapPDFData) string {
	if d.EventStartedAt.IsZero() {
		return ""
	}
	start := d.EventStartedAt
	if d.EventEndedAt.IsZero() {
		return start.Format("January 2, 2006")
	}
	end := d.EventEndedAt
	if start.Year() == end.Year() && start.Month() == end.Month() && start.Day() == end.Day() {
		return start.Format("January 2, 2006")
	}
	if start.Year() == end.Year() {
		return start.Format("January 2") + " – " + end.Format("January 2, 2006")
	}
	return start.Format("January 2, 2006") + " – " + end.Format("January 2, 2006")
}

// formatHMS renders a duration as "1h 23m" / "12m 04s" / "47s".
// Compact + readable; never hits the four-digit-hours range any
// real treasure-hunt event would.
func formatHMS(dur time.Duration) string {
	if dur <= 0 {
		return "—"
	}
	h := int(dur.Hours())
	m := int(dur.Minutes()) % 60
	s := int(dur.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// hexToRGB parses "#rrggbb" / "rrggbb" into 0–255 components, falling
// back to the supplied defaults when the input is malformed. Empty
// or unparseable hex codes produce the brand pink (#ad1357) for the
// accent strip so a half-configured adventure still ships a pretty
// PDF.
func hexToRGB(hex string, defR, defG, defB int) (int, int, int) {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(hex, "#"), "0x"))
	if len(s) != 6 {
		return defR, defG, defB
	}
	r, errR := strconv.ParseInt(s[0:2], 16, 32)
	g, errG := strconv.ParseInt(s[2:4], 16, 32)
	b, errB := strconv.ParseInt(s[4:6], 16, 32)
	if errR != nil || errG != nil || errB != nil {
		return defR, defG, defB
	}
	return int(r), int(g), int(b)
}

// truncateText caps a string at maxRunes, appending an ellipsis when
// it would otherwise overflow. Rune-counted (not byte-counted) so
// non-ASCII team names truncate to the visual width users expect.
func truncateText(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	if maxRunes < 1 {
		return ""
	}
	out := make([]rune, 0, maxRunes)
	for i, r := range s {
		_ = i
		if len(out) == maxRunes-1 {
			break
		}
		out = append(out, r)
	}
	return string(out) + "…"
}

// safeText replaces characters fpdf's built-in (Latin-1-ish) Helvetica
// can't render with their closest ASCII equivalent. fpdf doesn't
// crash on unsupported runes — it draws a "?" — but the swap below
// catches the common ones so most European names + the en-dash and
// ellipsis we emit ourselves render properly.
func safeText(s string) string {
	repl := strings.NewReplacer(
		"…", "...",
		"–", "-",
		"—", "-",
		"’", "'",
		"‘", "'",
		"“", `"`,
		"”", `"`,
		"·", "-",
	)
	return repl.Replace(s)
}
