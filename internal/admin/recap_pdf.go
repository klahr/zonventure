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

	"github.com/klahr/zonventure/internal/emailtpl"
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
	OrgName string
	// EventName is the organiser's name for the event; the cover leads
	// with it when set, and names the adventure under it.
	EventName      string
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
	// Stats are the event page's statistics, for their own page; nil
	// without a graph to read them from.
	Stats *EventStats
	// State is the event's: scheduled, running, paused, completed or
	// cancelled.
	State string
	// Lang is the organisation's language, the words' and the dates';
	// "" is English. Loc is its time zone; nil leaves times as they are.
	Lang string
	Loc  *time.Location
}

// t is s in the report's language (emailtpl's catalog), args put in.
func (d recapPDFData) t(s string, args ...any) string { return emailtpl.T(d.Lang, s, args...) }

// in is t in the report's time zone.
func (d recapPDFData) in(t time.Time) time.Time {
	if d.Loc == nil {
		return t
	}
	return t.In(d.Loc)
}

// num is a number as the report's language writes it: a decimal comma
// in Swedish.
func (d recapPDFData) num(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if d.Lang == "sv" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

// hms is formatHMS in the report's language.
func (d recapPDFData) hms(dur time.Duration) string {
	s := formatHMS(dur)
	if d.Lang == "sv" {
		s = strings.NewReplacer("h ", " h ", "m ", " min ", "s", " s").Replace(s)
		if strings.HasSuffix(s, "m") {
			s = strings.TrimSuffix(s, "m") + " min"
		}
	}
	return s
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
	d, err := h.collectRecapBase(ctx, eventID, orgID)
	if err != nil {
		return d, err
	}
	g, err := h.engine().EventGraph(ctx, h.DB, eventID)
	if err != nil && !errors.Is(err, play.ErrNoGraph) {
		return d, fmt.Errorf("graph: %w", err)
	}
	if g != nil {
		st := EventStats{Puzzles: []PuzzleStats{}}
		if err := h.fillEventStats(ctx, eventID, g, &st); err != nil {
			return d, fmt.Errorf("statistics: %w", err)
		}
		d.Stats = &st
	}
	return d, nil
}

// collectRecapBase is what the report and the standings poster share:
// the organisation, the event, its adventure and its teams, ranked.
func (h *Handlers) collectRecapBase(ctx context.Context, eventID, orgID int64) (recapPDFData, error) {
	var d recapPDFData
	l := h.orgLook(ctx, orgID)
	d.Lang, d.Loc = l.Lang, emailtpl.Location(l.TimeZone)
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
		SELECT adventure_slug, name, started_at, completed_at, state
		FROM events WHERE id = $1
	`, eventID).Scan(&slug, &d.EventName, &startedRaw, &completedRaw, &d.State); err != nil {
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
	h.addRecapLogo(ctx, &d, orgID)
	if err := h.collectGraphRecap(ctx, &d, eventID); err != nil {
		return d, err
	}
	return d, nil
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
	ranked, err := h.engine().EventRanking(ctx, eventID)
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
	text := recapText(pdf)
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
		footer := d.t("Generated %s  ·  powered by Zonventure", d.in(d.GeneratedAt).Format("2006-01-02 15:04 MST"))
		pdf.CellFormat(0, 5, text(footer), "", 0, "C", false, 0, "")
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
		pdf.CellFormat(usableW, 5, text(strings.ToUpper(d.OrgName)), "", 1, "L", false, 0, "")
		pdf.Ln(2)
	}
	pdf.SetFont("Helvetica", "B", 22)
	pdf.SetTextColor(40, 40, 40)
	pdf.MultiCell(usableW, 8.5, text(recapTitle(d)), "", "L", false)
	pdf.SetFont("Helvetica", "", 11)
	pdf.SetTextColor(120, 120, 120)
	pdf.CellFormat(usableW, 6, text(recapSubtitle(d)), "", 1, "L", false, 0, "")
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
		pdf.CellFormat(statW, 8, text(value), "", 0, "C", false, 0, "")
		pdf.SetXY(x, statY+13)
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(120, 120, 120)
		pdf.CellFormat(statW, 6, text(label), "", 0, "C", false, 0, "")
	}
	stat(0, d.t("Teams"), strconv.Itoa(teamsCount))
	stat(1, d.t("Completed"), fmt.Sprintf("%d / %d", completedCount, teamsCount))
	stat(2, d.t("Puzzles"), strconv.Itoa(d.PageCount))
	if longest > 0 {
		stat(3, d.t("Longest run"), d.hms(longest))
	} else {
		stat(3, d.t("Longest run"), "—")
	}
	pdf.SetY(statY + statBoxH + 8)

	// Team rankings.
	pdf.SetFont("Helvetica", "B", 14)
	pdf.SetTextColor(40, 40, 40)
	pdf.CellFormat(usableW, 8, text(d.t("Team standings")), "", 1, "L", false, 0, "")
	pdf.Ln(1)

	// Column widths sum to 180 (= usableW). Tweak together if you
	// add a column.
	cols := []struct {
		w     float64
		hdr   string
		align string
	}{
		{12, "#", "L"},
		{60, d.t("Team"), "L"},
		{58, d.t("Members"), "L"},
		{20, d.t("Solved"), "L"},
		{30, d.t("Time"), "L"},
	}
	if d.Graph {
		cols[3].hdr = d.t("Points")
	}
	pdf.SetFillColor(245, 245, 245)
	pdf.SetTextColor(80, 80, 80)
	pdf.SetFont("Helvetica", "B", 9)
	for _, c := range cols {
		pdf.CellFormat(c.w, 7, text(c.hdr), "B", 0, c.align, true, 0, "")
	}
	pdf.Ln(7)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(40, 40, 40)
	if len(d.Teams) == 0 {
		pdf.SetTextColor(160, 160, 160)
		pdf.SetFont("Helvetica", "I", 9)
		pdf.CellFormat(usableW, 8, text(d.t("No teams played in this event.")), "", 1, "L", false, 0, "")
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
			pdf.CellFormat(usableW, 6, text(d.t("Withdrew")), "", 1, "L", false, 0, "")
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
			elapsed = d.hms(t.Elapsed)
		}
		// Withdrawn row's "Time" column shows "withdrew" rather than
		// the dash, so the column communicates intent rather than
		// "we don't know."
		if t.WithdrawnAt != "" {
			elapsed = d.t("withdrew")
		}
		progress := fmt.Sprintf("%d / %d", t.Solved, d.PageCount)
		if d.Graph {
			progress = d.num(t.Points)
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
			pdf.CellFormat(c.w, 6, text(row[j]), "", 0, c.align, false, 0, "")
		}
		if t.WithdrawnAt != "" {
			pdf.SetTextColor(40, 40, 40)
		}
		pdf.Ln(6)
	}

	if d.Stats != nil && d.Stats.Teams > 0 {
		writeRecapStats(pdf, text, d, *d.Stats, accentR, accentG, accentB)
	}
	return pdf.Output(w)
}

// writeRecapStats adds the statistics page: the event page's numbers,
// across every team and never naming one.
func writeRecapStats(pdf *fpdf.Fpdf, text func(string) string, d recapPDFData, st EventStats, accentR, accentG, accentB int) {
	const (
		marginX  = 15.0
		usableW  = 180.0
		statBoxH = 22.0
		statGap  = 3.0
	)
	pct := func(n, of int) string {
		if of == 0 {
			return "—"
		}
		return fmt.Sprintf("%d%%", (n*100+of/2)/of)
	}
	dur := func(s *int) string {
		if s == nil {
			return "—"
		}
		return d.hms(time.Duration(*s) * time.Second)
	}
	num := func(f *float64) string { return d.num(*f) }

	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 18)
	pdf.SetTextColor(40, 40, 40)
	pdf.CellFormat(usableW, 9, text(d.t("Statistics")), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(120, 120, 120)
	pdf.CellFormat(usableW, 6, text(d.t("Anonymous — across every team that played, without names.")), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	statW := (usableW - 3*statGap) / 4
	statY := pdf.GetY()
	for i, s := range [][2]string{
		{strconv.Itoa(st.Guesses), d.t("guesses")},
		{pct(st.FirstTry, st.Solved), d.t("solved on the first try")},
		{strconv.Itoa(st.Hints), d.t("hints used")},
		{dur(st.MedianSeconds), d.t("median finish time")},
	} {
		x := marginX + float64(i)*(statW+statGap)
		pdf.SetFillColor(248, 248, 248)
		pdf.SetDrawColor(225, 225, 225)
		pdf.RoundedRect(x, statY, statW, statBoxH, 2.5, "1234", "FD")
		pdf.SetXY(x, statY+3)
		pdf.SetFont("Helvetica", "B", 16)
		pdf.SetTextColor(accentR, accentG, accentB)
		pdf.CellFormat(statW, 8, text(s[0]), "", 0, "C", false, 0, "")
		pdf.SetXY(x, statY+13)
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(120, 120, 120)
		pdf.CellFormat(statW, 6, text(s[1]), "", 0, "C", false, 0, "")
	}
	pdf.SetY(statY + statBoxH + 6)

	// The trickiest takes the most wrong guesses per team that got
	// there, the quickest is solved fastest in the middle: as the event
	// page picks them.
	var trickiest, quickest *PuzzleStats
	for i := range st.Puzzles {
		p := &st.Puzzles[i]
		if p.Reached == 0 {
			continue
		}
		if p.Wrong > 0 && (trickiest == nil || float64(p.Wrong)/float64(p.Reached) > float64(trickiest.Wrong)/float64(trickiest.Reached)) {
			trickiest = p
		}
		if p.MedianSeconds != nil && (quickest == nil || *p.MedianSeconds < *quickest.MedianSeconds) {
			quickest = p
		}
	}
	facts := []string{d.t("%d of %d teams finished", st.Finished, st.Teams)}
	if st.Failed > 0 {
		facts[0] += " · " + d.t("%d failed", st.Failed)
	}
	if st.FastestSeconds != nil {
		facts = append(facts, d.t("Fastest %s, slowest %s", dur(st.FastestSeconds), dur(st.SlowestSeconds)))
	}
	if st.TopScore != nil {
		facts = append(facts, d.t("Top score %s, median %s", num(st.TopScore), num(st.MedianScore)))
	}
	if trickiest != nil {
		guesses := d.t("wrong guesses")
		if trickiest.Wrong == 1 {
			guesses = d.t("wrong guess")
		}
		facts = append(facts, d.t("Trickiest: %s — %d %s", trickiest.Title, trickiest.Wrong, guesses))
	}
	if quickest != nil {
		facts = append(facts, d.t("Quickest: %s — solved in %s", quickest.Title, dur(quickest.MedianSeconds)))
	}
	if st.AvgDistanceM != nil {
		facts = append(facts, fmt.Sprintf("%.1f km walked per team", *st.AvgDistanceM/1000))
	}
	if st.Photos > 0 {
		facts = append(facts, fmt.Sprintf("%d photos taken", st.Photos))
	}
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(60, 60, 60)
	for _, f := range facts {
		pdf.CellFormat(usableW, 6, text("•  "+f), "", 1, "L", false, 0, "")
	}
	pdf.Ln(5)

	pdf.SetFont("Helvetica", "B", 14)
	pdf.SetTextColor(40, 40, 40)
	pdf.CellFormat(usableW, 8, text(d.t("Puzzles")), "", 1, "L", false, 0, "")
	pdf.Ln(1)
	// Column widths sum to 180 (= usableW).
	cols := []struct {
		w     float64
		hdr   string
		align string
	}{
		{64, d.t("Puzzle"), "L"},
		{20, d.t("Reached"), "R"},
		{20, d.t("Solved"), "R"},
		{20, d.t("First try"), "R"},
		{18, d.t("Wrong"), "R"},
		{16, d.t("Hints"), "R"},
		{22, d.t("Median time"), "R"},
	}
	pdf.SetFillColor(245, 245, 245)
	pdf.SetTextColor(80, 80, 80)
	pdf.SetFont("Helvetica", "B", 9)
	for _, c := range cols {
		pdf.CellFormat(c.w, 7, text(c.hdr), "B", 0, c.align, true, 0, "")
	}
	pdf.Ln(7)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(40, 40, 40)
	for i, p := range st.Puzzles {
		if i%2 == 1 {
			pdf.SetFillColor(252, 252, 252)
			pdf.Rect(marginX, pdf.GetY(), usableW, 6, "F")
		}
		row := []string{truncateText(p.Title, 34), strconv.Itoa(p.Reached), pct(p.Solved, p.Reached), pct(p.FirstTry, p.Solved), strconv.Itoa(p.Wrong), strconv.Itoa(p.Hints), dur(p.MedianSeconds)}
		for j, c := range cols {
			pdf.CellFormat(c.w, 6, text(row[j]), "", 0, c.align, false, 0, "")
		}
		pdf.Ln(6)
	}
}

// recapTitle is the cover's heading: the event's own name when it has
// one, else the adventure's title.
func recapTitle(d recapPDFData) string {
	if strings.TrimSpace(d.EventName) != "" {
		return d.EventName
	}
	return d.AdventureTitle
}

// recapSubtitle is the line under the heading: the adventure (when the
// heading is the event's name) and the dates.
func recapSubtitle(d recapPDFData) string {
	parts := []string{}
	if t := recapTitle(d); t != d.AdventureTitle && d.AdventureTitle != "" {
		parts = append(parts, d.AdventureTitle)
	}
	if s := recapDateLine(d); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "  ·  ")
}

// recapDateLine formats the cover-page subtitle. Same date repeated
// when start and end share a calendar day; "May 1 – May 3, 2026"
// shape when the event spans multiple days; bare start when the
// event hasn't recorded a finish yet. Swedish writes "1 maj 2026".
func recapDateLine(d recapPDFData) string {
	if d.EventStartedAt.IsZero() {
		return ""
	}
	start := d.in(d.EventStartedAt)
	day, dayYear := func(t time.Time) string { return t.Format("January 2") }, func(t time.Time) string { return t.Format("January 2, 2006") }
	if d.Lang == "sv" {
		day = func(t time.Time) string { return fmt.Sprintf("%d %s", t.Day(), svMonths[t.Month()-1]) }
		dayYear = func(t time.Time) string { return fmt.Sprintf("%s %d", day(t), t.Year()) }
	}
	if d.EventEndedAt.IsZero() {
		return dayYear(start)
	}
	end := d.in(d.EventEndedAt)
	if start.Year() == end.Year() && start.Month() == end.Month() && start.Day() == end.Day() {
		return dayYear(start)
	}
	if start.Year() == end.Year() {
		return day(start) + " – " + dayYear(end)
	}
	return dayYear(start) + " – " + dayYear(end)
}

var svMonths = []string{"januari", "februari", "mars", "april", "maj", "juni", "juli", "augusti", "september", "oktober", "november", "december"}

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

// recapText prepares text for the PDF's built-in Helvetica, which reads
// single-byte Windows-1252, not UTF-8: without this, "Gröna" prints as
// "GrÃ¶na". Western European letters come through; a character outside
// the code page prints as ".". One per PDF, as the translator is not
// safe for concurrent use.
func recapText(pdf *fpdf.Fpdf) func(string) string {
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	return func(s string) string { return tr(safeText(s)) }
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
