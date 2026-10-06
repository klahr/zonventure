package admin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-pdf/fpdf"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/trail"
	qrcode "github.com/skip2/go-qrcode"
)

// A quiz trail on paper: the cards to hide — two to an A4 sheet, each a
// big number, the question and its options — and the host's answer key.
// From a hosted event, the cards come after a cover with the player
// link's QR code. Printing the cards records the revision printed, so
// the creator can say when they changed since.

// trailPrint is what the cards PDF is made from.
type trailPrint struct {
	Title     string
	Lang      string
	Accent    string
	Cards     []trail.Card
	PlayerURL string // a hosted event's player link, for the cover
}

func (h *Handlers) printTrail(ctx context.Context, slug string) (trailPrint, loadedTrail, error) {
	t, err := loadTrail(ctx, h.DB, slug, false)
	if err != nil {
		return trailPrint{}, t, err
	}
	var accent string
	_ = h.DB.QueryRowContext(ctx, `SELECT COALESCE(accent_color, '') FROM adventures WHERE id = $1`, t.ID).Scan(&accent)
	return trailPrint{Title: t.dto.Title, Lang: trailLang(t.dto.Language), Accent: accent, Cards: t.dto.Cards}, t, nil
}

// sendTrailPDF writes the cards (or, with ?answers=1, the answer key) as
// a PDF download, and marks the cards printed.
func (h *Handlers) sendTrailPDF(w http.ResponseWriter, r *http.Request, p trailPrint, t loadedTrail) {
	answers := r.URL.Query().Get("answers") == "1"
	name := "cards"
	if answers {
		name = "answers"
	}
	var buf bytes.Buffer
	var err error
	if answers {
		err = writeTrailAnswersPDF(&buf, p)
	} else {
		err = writeTrailCardsPDF(&buf, p)
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "pdf failed", err)
		return
	}
	if !answers {
		if _, err := h.DB.ExecContext(r.Context(), `UPDATE adventures SET trail_printed = trail_revision WHERE id = $1`, t.ID); err != nil {
			logx.Warn("admin.trail_pdf.printed", "adventure", t.dto.Slug, "err", err.Error())
		}
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.pdf"`, t.dto.Slug, name))
	_, _ = w.Write(buf.Bytes())
}

// TrailPDF is a trail's cards, or its answer key, from the console.
//
// GET /api/admin/adventures/{slug}/cards.pdf[?answers=1]
func (h *Handlers) TrailPDF(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.trailFor(w, r); !ok {
		return
	}
	p, t, err := h.printTrail(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "trail lookup failed", err)
		return
	}
	h.sendTrailPDF(w, r, p, t)
}

// HostedTrailPDF is a hosted quiz trail's cards, after a cover with the
// player link, or its answer key: from the host page or the console.
//
// GET /api/party/{token}/cards.pdf[?answers=1]
// GET /api/admin/events/{eventID}/hosted/cards.pdf[?answers=1]
func (h *Handlers) HostedTrailPDF(w http.ResponseWriter, r *http.Request) {
	ev, ok := h.hostedParty(w, r)
	if !ok {
		return
	}
	p, t, err := h.printTrail(r.Context(), ev.AdvSlug)
	if errors.Is(err, errNotTrail) {
		httpx.Error(w, http.StatusNotFound, "this adventure has no cards to print")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "trail lookup failed", err)
		return
	}
	_ = h.DB.QueryRowContext(r.Context(), `SELECT party_name FROM events WHERE id = $1`, ev.EventID).Scan(&p.Title)
	p.PlayerURL = strings.TrimRight(h.PublicURL, "/") + "/j/" + ev.PlayerToken
	h.sendTrailPDF(w, r, p, t)
}

type trailWords struct {
	card, info, writeIt, scan, howTo, hide, answers, right string
}

func trailPDFWords(lang string) trailWords {
	if lang == "sv" {
		return trailWords{
			card: "Kort %d", info: "Information", writeIt: "Skriv svaret i mobilen.",
			scan: "Skanna för att vara med", howTo: "Varje lag skannar koden med en mobil, ger sig ett namn och letar upp korten i ordning. Svaren skrivs i mobilen.",
			hide: "Göm korten i ordning, 1 till %d, där lagen kan hitta dem.", answers: "Facit", right: "Rätt svar",
		}
	}
	return trailWords{
		card: "Card %d", info: "Information", writeIt: "Write the answer on your phone.",
		scan: "Scan to join", howTo: "Each team scans the code with one phone, gives itself a name and finds the cards in order. Answers go on the phone.",
		hide: "Hide the cards in order, 1 to %d, where the teams can find them.", answers: "Answer key", right: "Right answer",
	}
}

// writeTrailCardsPDF draws the cards, two to an A4 sheet with a cut line
// between, after a cover when there is a player link.
func writeTrailCardsPDF(w io.Writer, p trailPrint) error {
	pdf := fpdf.New("P", "mm", pdfPageSize, "")
	text := recapText(pdf)
	pdf.SetAutoPageBreak(false, 0)
	words := trailPDFWords(p.Lang)
	ar, ag, ab := hexToRGB(p.Accent, 173, 19, 87)
	nums := trail.Number(p.Cards)
	questions := 0
	for _, n := range nums {
		questions = max(questions, n)
	}
	const pageW, pageH, margin = 210.0, 297.0, 18.0

	if p.PlayerURL != "" {
		png, err := qrcode.Encode(p.PlayerURL, qrcode.Medium, 600)
		if err != nil {
			return err
		}
		pdf.AddPage()
		pdf.SetFillColor(ar, ag, ab)
		pdf.Rect(0, 0, pageW, 10, "F")
		pdf.SetTextColor(30, 30, 30)
		pdf.SetFont("Helvetica", "B", 28)
		pdf.SetXY(margin, 30)
		pdf.MultiCell(pageW-2*margin, 12, text(p.Title), "", "C", false)
		pdf.RegisterImageOptionsReader("player-qr", fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(png))
		pdf.ImageOptions("player-qr", (pageW-110)/2, pdf.GetY()+10, 110, 110, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
		pdf.SetY(pdf.GetY() + 125)
		pdf.SetFont("Helvetica", "B", 18)
		pdf.CellFormat(0, 10, text(words.scan), "", 1, "C", false, 0, "")
		pdf.SetFont("Helvetica", "", 11)
		pdf.CellFormat(0, 7, text(p.PlayerURL), "", 1, "C", false, 0, "")
		pdf.Ln(8)
		pdf.SetX(margin + 10)
		pdf.MultiCell(pageW-2*margin-20, 6, text(words.howTo), "", "C", false)
		if questions > 0 {
			pdf.Ln(4)
			pdf.SetX(margin + 10)
			pdf.MultiCell(pageW-2*margin-20, 6, text(fmt.Sprintf(words.hide, questions)), "", "C", false)
		}
	}

	half := pageH / 2
	for i, c := range p.Cards {
		top := 0.0
		if i%2 == 0 {
			pdf.AddPage()
			pdf.SetDrawColor(170, 170, 170)
			pdf.SetDashPattern([]float64{2, 2}, 0)
			pdf.Line(8, half, pageW-8, half)
			pdf.SetDashPattern(nil, 0)
		} else {
			top = half
		}
		pdf.SetFillColor(ar, ag, ab)
		pdf.Rect(0, top, 14, half, "F")
		x, y := margin+6, top+margin
		pdf.SetTextColor(ar, ag, ab)
		pdf.SetFont("Helvetica", "B", 44)
		pdf.SetXY(x, y)
		label := words.info
		if nums[i] > 0 {
			label = fmt.Sprintf(words.card, nums[i])
		}
		pdf.CellFormat(0, 18, text(label), "", 1, "L", false, 0, "")
		pdf.SetTextColor(30, 30, 30)
		pdf.SetFont("Helvetica", "B", 20)
		pdf.SetX(x)
		pdf.MultiCell(pageW-x-margin, 9, text(strings.TrimSpace(c.Text)), "", "L", false)
		pdf.Ln(6)
		switch c.Kind {
		case trail.Quiz:
			pdf.SetFont("Helvetica", "", 18)
			for j, mark := range []string{"1", "X", "2"} {
				opt := ""
				if j < len(c.Options) {
					opt = c.Options[j]
				}
				pdf.SetX(x)
				pdf.SetFont("Helvetica", "B", 18)
				pdf.CellFormat(14, 10, mark, "", 0, "L", false, 0, "")
				pdf.SetFont("Helvetica", "", 18)
				pdf.MultiCell(pageW-x-margin-14, 10, text(opt), "", "L", false)
			}
		case trail.Answer:
			pdf.SetFont("Helvetica", "I", 14)
			pdf.SetX(x)
			pdf.CellFormat(0, 8, text(words.writeIt), "", 1, "L", false, 0, "")
		}
	}
	return pdf.Output(w)
}

// writeTrailAnswersPDF is the host's answer key: each question's right
// answer, on one page.
func writeTrailAnswersPDF(w io.Writer, p trailPrint) error {
	pdf := fpdf.New("P", "mm", pdfPageSize, "")
	text := recapText(pdf)
	pdf.SetMargins(18, 18, 18)
	pdf.SetAutoPageBreak(true, 18)
	words := trailPDFWords(p.Lang)
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 20)
	pdf.CellFormat(0, 10, text(words.answers+" - "+p.Title), "", 1, "L", false, 0, "")
	pdf.Ln(4)
	nums := trail.Number(p.Cards)
	for i, c := range p.Cards {
		if nums[i] == 0 {
			continue
		}
		var right string
		switch c.Kind {
		case trail.Quiz:
			for j, mark := range []string{"1", "X", "2"} {
				if mark == c.Correct && j < len(c.Options) {
					right = mark + "  " + c.Options[j]
				}
			}
		case trail.Answer:
			right = strings.Join(c.Answers, " / ")
		}
		pdf.SetFont("Helvetica", "B", 12)
		pdf.CellFormat(28, 7, text(fmt.Sprintf(words.card, nums[i])), "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 12)
		pdf.MultiCell(0, 7, text(strings.TrimSpace(c.Text)), "", "L", false)
		pdf.SetX(18 + 28)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.MultiCell(0, 7, text(words.right+": "+right), "", "L", false)
		pdf.Ln(3)
	}
	return pdf.Output(w)
}
