package admin

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/results"
)

// ExportEventStandingsPDF writes the standings poster as
// `event-<id>-standings.pdf`: one sheet to print and put up where the
// event ends, so the teams see where they placed. Sister to the report
// (ExportEventRecapPDF), which is for the organiser's stakeholders: the
// poster has the ranking alone, large enough to read from a step back,
// and once the event is over its results fingerprint — a photo of the
// sheet is every team's copy of the results as they stood.
//
//	GET /api/admin/events/{eventID}/standings.pdf
func (h *Handlers) ExportEventStandingsPDF(w http.ResponseWriter, r *http.Request) {
	eventID, ok := eventIDFrom(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad event id")
		return
	}
	orgID, ok := h.authorizeEvent(w, r, eventID)
	if !ok {
		return
	}
	d, err := h.collectRecapBase(r.Context(), eventID, orgID)
	if err != nil {
		logx.Warn("admin.standings_pdf.lookup", "event", eventID, "err", err.Error())
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	fingerprint := results.Sealed(r.Context(), h.DB, eventID)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="event-%d-standings.pdf"`, eventID))
	if err := writeStandingsPDF(w, d, fingerprint); err != nil {
		logx.Warn("admin.standings_pdf.render", "event", eventID, "err", err.Error())
	}
}

// writeStandingsPDF draws the poster. The rows share the page between
// them: few teams get large type, many get smaller, down to a size
// still readable on a wall, past which the list runs onto another sheet
// under the same headings. The top three wear the adventure's accent.
// fingerprint is "" while the event runs: the sheet then says these
// are the standings as of when it was printed.
func writeStandingsPDF(w io.Writer, d recapPDFData, fingerprint string) error {
	const (
		pageW, pageH = 210.0, 297.0
		marginX      = 15.0
		usableW      = pageW - 2*marginX
		bottom       = 18.0
		rankW        = 20.0
		valueW       = 32.0
		timeW        = 36.0
		maxRowH      = 16.0
		minRowH      = 9.0
	)
	pdf := fpdf.New("P", "mm", pdfPageSize, "")
	text := recapText(pdf)
	pdf.SetMargins(marginX, 15, marginX)
	pdf.SetAutoPageBreak(false, bottom)
	accentR, accentG, accentB := hexToRGB(d.Accent, 173, 19, 87)

	var (
		ranked   []recapPDFTeam
		unranked []string
	)
	for _, t := range d.Teams {
		if t.Rank > 0 {
			ranked = append(ranked, t)
		} else {
			unranked = append(unranked, t.Name)
		}
	}
	running := d.State == "scheduled" || d.State == "running" || d.State == "paused"

	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetTextColor(150, 150, 150)
		pdf.CellFormat(0, 5, text(d.t("Printed %s  ·  powered by Zonventure", d.in(d.GeneratedAt).Format("2006-01-02 15:04 MST"))), "", 0, "C", false, 0, "")
	})

	// header starts a sheet: the accent strip, and on the first the
	// organisation, the event's title and what this is.
	header := func(first bool) {
		pdf.AddPage()
		pdf.SetFillColor(accentR, accentG, accentB)
		pdf.Rect(0, 0, pageW, 8, "F")
		pdf.SetY(16)
		if !first {
			return
		}
		logo := false
		if len(d.LogoBytes) > 0 {
			const logoH, logoMaxW = 12.0, 60.0
			opts := fpdf.ImageOptions{ImageType: "JPG"}
			pdf.RegisterImageOptionsReader("org-logo", opts, bytes.NewReader(d.LogoBytes))
			if info := pdf.GetImageInfo("org-logo"); info != nil && !pdf.Err() {
				wd, ht := logoH*info.Width()/info.Height(), logoH
				if wd > logoMaxW {
					wd, ht = logoMaxW, logoMaxW*info.Height()/info.Width()
				}
				pdf.ImageOptions("org-logo", marginX, pdf.GetY(), wd, ht, false, opts, 0, "")
				pdf.SetY(pdf.GetY() + ht + 4)
				logo = true
			}
		}
		if !logo && d.OrgName != "" {
			pdf.SetFont("Helvetica", "B", 10)
			pdf.SetTextColor(120, 120, 120)
			pdf.CellFormat(usableW, 5, text(strings.ToUpper(d.OrgName)), "", 1, "L", false, 0, "")
			pdf.Ln(2)
		}
		pdf.SetFont("Helvetica", "B", 30)
		pdf.SetTextColor(30, 30, 30)
		pdf.MultiCell(usableW, 12, text(recapTitle(d)), "", "L", false)
		sub := []string{d.t("Results")}
		if s := recapSubtitle(d); s != "" {
			sub = append(sub, s)
		}
		pdf.SetFont("Helvetica", "", 13)
		pdf.SetTextColor(110, 110, 110)
		pdf.CellFormat(usableW, 8, text(strings.Join(sub, "  ·  ")), "", 1, "L", false, 0, "")
		pdf.Ln(5)
	}

	valueHdr := d.t("Points")
	if !d.Graph {
		valueHdr = d.t("Solved")
	}
	nameW := usableW - rankW - valueW - timeW
	columns := func() {
		pdf.SetFont("Helvetica", "B", 10)
		pdf.SetTextColor(120, 120, 120)
		pdf.SetDrawColor(210, 210, 210)
		pdf.CellFormat(rankW, 7, "#", "B", 0, "C", false, 0, "")
		pdf.CellFormat(nameW, 7, text(d.t("Team")), "B", 0, "L", false, 0, "")
		pdf.CellFormat(valueW, 7, text(valueHdr), "B", 0, "R", false, 0, "")
		pdf.CellFormat(timeW, 7, text(d.t("Time")), "B", 1, "R", false, 0, "")
		pdf.Ln(1)
	}

	// The footer block on the last sheet: the fingerprint, or the
	// moment these standings were taken.
	const closingH = 30.0
	header(true)
	if len(ranked) == 0 {
		pdf.SetFont("Helvetica", "I", 14)
		pdf.SetTextColor(150, 150, 150)
		pdf.CellFormat(usableW, 10, text(d.t("No teams played in this event.")), "", 1, "L", false, 0, "")
	} else {
		columns()
	}
	room := pageH - bottom - closingH - pdf.GetY()
	if len(unranked) > 0 {
		room -= 12
	}
	rowH := maxRowH
	if n := float64(len(ranked)); n > 0 && room/n < rowH {
		rowH = max(minRowH, room/n)
	}
	size := rowH * 1.6 // points to the row's millimetres: the type fills it
	for i, t := range ranked {
		if pdf.GetY()+rowH > pageH-bottom-6 {
			header(false)
			columns()
		}
		y := pdf.GetY()
		if i%2 == 1 {
			pdf.SetFillColor(247, 247, 247)
			pdf.Rect(marginX, y, usableW, rowH, "F")
		}
		podium := t.Rank <= 3
		style := ""
		if podium {
			style = "B"
			r := rowH * 0.36
			pdf.SetFillColor(accentR, accentG, accentB)
			pdf.Circle(marginX+rankW/2, y+rowH/2, r, "F")
			pdf.SetTextColor(255, 255, 255)
		} else {
			pdf.SetTextColor(90, 90, 90)
		}
		pdf.SetFont("Helvetica", "B", size*0.8)
		pdf.SetXY(marginX, y)
		pdf.CellFormat(rankW, rowH, strconv.Itoa(t.Rank), "", 0, "C", false, 0, "")

		// The name shrinks to fit its column before it is cut.
		pdf.SetTextColor(25, 25, 25)
		name := text(t.Name)
		fs := size
		pdf.SetFont("Helvetica", style, fs)
		for fs > size*0.6 && pdf.GetStringWidth(name) > nameW-2 {
			fs -= 0.5
			pdf.SetFont("Helvetica", style, fs)
		}
		for pdf.GetStringWidth(name) > nameW-2 && len([]rune(t.Name)) > 1 {
			t.Name = string([]rune(t.Name)[:len([]rune(t.Name))-1])
			name = text(strings.TrimSpace(t.Name) + "…")
		}
		pdf.CellFormat(nameW, rowH, name, "", 0, "L", false, 0, "")

		value := fmt.Sprintf("%d / %d", t.Solved, d.PageCount)
		if d.Graph {
			value = d.num(t.Points)
		}
		pdf.SetFont("Helvetica", style, size)
		pdf.CellFormat(valueW, rowH, text(value), "", 0, "R", false, 0, "")
		elapsed := d.t("did not finish")
		if running {
			elapsed = d.t("playing")
		}
		pdf.SetFont("Helvetica", "", size*0.7)
		pdf.SetTextColor(110, 110, 110)
		if t.CompletedAt != "" {
			elapsed = "—"
			if t.Elapsed > 0 {
				elapsed = d.hms(t.Elapsed)
			}
		}
		pdf.CellFormat(timeW, rowH, text(elapsed), "", 1, "R", false, 0, "")
	}

	if len(unranked) > 0 {
		pdf.Ln(3)
		pdf.SetFont("Helvetica", "", 10)
		pdf.SetTextColor(130, 130, 130)
		pdf.MultiCell(usableW, 5, text(d.t("Not ranked (withdrew or didn't start): %s", strings.Join(unranked, ", "))), "", "L", false)
	}

	if pdf.GetY() > pageH-bottom-closingH {
		header(false)
	}
	pdf.SetY(pageH - bottom - closingH + 4)
	pdf.SetDrawColor(220, 220, 220)
	pdf.Line(marginX, pdf.GetY(), pageW-marginX, pdf.GetY())
	pdf.Ln(3)
	if fingerprint != "" {
		pdf.SetFont("Helvetica", "B", 10)
		pdf.SetTextColor(60, 60, 60)
		pdf.CellFormat(usableW, 6, text(d.t("The event's code")), "", 1, "L", false, 0, "")
		pdf.SetFont("Courier", "", 10.5)
		pdf.SetTextColor(30, 30, 30)
		pdf.CellFormat(usableW, 6, fingerprint, "", 1, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(120, 120, 120)
		pdf.MultiCell(usableW, 4.5, text(d.t("Take a photo of this sheet. Your team's recap shows the event's code: as long as it matches this one, no one has changed the results since the event ended.")), "", "L", false)
	} else if running {
		pdf.SetFont("Helvetica", "B", 10)
		pdf.SetTextColor(60, 60, 60)
		pdf.CellFormat(usableW, 6, text(d.t("Standings at %s", d.in(d.GeneratedAt).Format("15:04"))), "", 1, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(120, 120, 120)
		pdf.MultiCell(usableW, 4.5, text(d.t("The event is still running: these are not the final results.")), "", "L", false)
	}
	return pdf.Output(w)
}
