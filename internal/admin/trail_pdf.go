package admin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
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
// They wear the trail's look as the creator draws it — each card's header
// in the look's gradient, white text and a white number badge — and
// nothing else in colour, to spare the printer.
// From a hosted event, the cards come after a cover with the player
// link's QR code. Printing the cards records the revision printed, so
// the creator can say when they changed since.

// trailPrint is what the cards PDF is made from.
type trailPrint struct {
	Title     string
	Lang      string
	Accent    string
	Second    string // the look's second colour; information cards wear it
	Cards     []trail.Card
	PlayerURL string // a hosted event's player link, for the cover
	Cover     []byte // the look's cover picture (JPEG), across the cover's top
}

func (h *Handlers) printTrail(ctx context.Context, slug string) (trailPrint, loadedTrail, error) {
	t, err := loadTrail(ctx, h.DB, slug, false)
	if err != nil {
		return trailPrint{}, t, err
	}
	var accent, second string
	_ = h.DB.QueryRowContext(ctx, `SELECT COALESCE(accent_color, ''), COALESCE(accent_secondary_color, '') FROM adventures WHERE id = $1`, t.ID).Scan(&accent, &second)
	// The adventure's own cover, else its starter theme's.
	cover := trail.Cover(t.dto.Theme)
	if id, f, err := h.adventureCover(ctx, slug, true); err != nil {
		return trailPrint{}, t, err
	} else if id > 0 {
		if jpg := printCover(f); jpg != nil {
			cover = jpg
		}
	}
	return trailPrint{Title: t.dto.Title, Lang: trailLang(t.dto.Language), Accent: accent, Second: second, Cards: t.dto.Cards,
		Cover: cover}, t, nil
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

// darkLeft is whether a cover picture's left side, where the title goes,
// is dark (Space, Retro 80s): the title is then white.
func darkLeft(jpg []byte) bool {
	img, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		return false
	}
	b := img.Bounds()
	var sum, n float64
	for y := b.Min.Y + b.Dy()/5; y < b.Max.Y-b.Dy()/5; y += 8 {
		for x := b.Min.X; x < b.Min.X+b.Dx()*45/100; x += 8 {
			r, g, bl, _ := img.At(x, y).RGBA()
			sum += 0.2126*float64(r>>8) + 0.7152*float64(g>>8) + 0.0722*float64(bl>>8)
			n++
		}
	}
	return n > 0 && sum/n < 110
}

// rgb is a colour for the PDF.
type rgb struct{ r, g, b int }

func hexRGB(hex string, def rgb) rgb {
	r, g, b := hexToRGB(hex, def.r, def.g, def.b)
	return rgb{r, g, b}
}

// tint is c mixed with white: f of c, the rest paper. Pale tints cost
// the printer next to nothing.
func (c rgb) tint(f float64) rgb {
	mix := func(v int) int { return int(float64(v)*f + 255*(1-f) + 0.5) }
	return rgb{mix(c.r), mix(c.g), mix(c.b)}
}

// ink is c dark enough to read as text on white: a light colour (a
// sunshine yellow) is darkened until it is.
func (c rgb) ink() rgb {
	for i := 0; i < 8 && 0.2126*float64(c.r)+0.7152*float64(c.g)+0.0722*float64(c.b) > 140; i++ {
		c = rgb{c.r * 4 / 5, c.g * 4 / 5, c.b * 4 / 5}
	}
	return c
}

// shade is play-ui's shade: toward white by amount, or toward black
// when amount is negative.
func (c rgb) shade(amount float64) rgb {
	mix, a := 255.0, amount
	if amount < 0 {
		mix, a = 0, -amount
	}
	f := func(v int) int { return int(float64(v) + (mix-float64(v))*a + 0.5) }
	return rgb{f(c.r), f(c.g), f(c.b)}
}

// gradient is a header's colours as the creator's CSS has them
// (packages/trail/src/themes.ts): from, to mid at split, then to.
type gradient struct {
	from, mid, to rgb
	split         float64
}

// brandGradient is --trail-brand, and infoGradient --trail-info, of a
// look's accent and second colour.
func brandGradient(a, b rgb) gradient { return gradient{a.shade(0.18), a, b, 0.5} }
func infoGradient(b rgb) gradient     { return gradient{b.shade(0.12), b, b.shade(-0.32), 0.55} }

// band fills a header band, its top corners rounded, with g: across,
// in two runs, as the wide band's diagonal is nearly across anyway.
func band(pdf *fpdf.Fpdf, x, y, w, h, r float64, g gradient) {
	pdf.ClipRoundedRectExt(x, y, w, h, r, r, 0, 0, false)
	cut := w * g.split
	pdf.LinearGradient(x, y, cut, h, g.from.r, g.from.g, g.from.b, g.mid.r, g.mid.g, g.mid.b, 0, 0, 1, 0)
	// The second run starts a hair early: abutting, viewers draw a seam.
	pdf.LinearGradient(x+cut-0.5, y, w-cut+0.5, h, g.mid.r, g.mid.g, g.mid.b, g.to.r, g.to.g, g.to.b, 0, 0, 1, 0)
	pdf.ClipEnd()
}

func (c rgb) fill(pdf *fpdf.Fpdf) { pdf.SetFillColor(c.r, c.g, c.b) }
func (c rgb) draw(pdf *fpdf.Fpdf) { pdf.SetDrawColor(c.r, c.g, c.b) }
func (c rgb) text(pdf *fpdf.Fpdf) { pdf.SetTextColor(c.r, c.g, c.b) }

// The plain parts: text, and the grey of lines and hints.
var (
	pdfInk   = rgb{30, 41, 56}
	pdfLine  = rgb{205, 211, 219}
	pdfMuted = rgb{120, 130, 145}
)

// writeTrailCardsPDF draws the cards, two to an A4 sheet with a cut line
// between, after a cover when there is a player link. Each card is an
// outline in the look's colour with a pale band across the top holding
// its number; information cards wear the second colour.
func writeTrailCardsPDF(w io.Writer, p trailPrint) error {
	pdf := fpdf.New("P", "mm", pdfPageSize, "")
	text := recapText(pdf)
	pdf.SetAutoPageBreak(false, 0)
	words := trailPDFWords(p.Lang)
	accent := hexRGB(p.Accent, rgb{173, 19, 87})
	second := hexRGB(p.Second, accent)
	if p.Second == "" || strings.EqualFold(p.Second, p.Accent) {
		second = accent.shade(-0.35)
	}
	brand, info := brandGradient(accent, second), infoGradient(second)
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
		qy := 74.0
		if p.Cover != nil {
			// The look's picture across the top, its calm left side
			// holding the title.
			const bannerH = 78.0
			info := pdf.RegisterImageOptionsReader("cover", fpdf.ImageOptions{ImageType: "JPG"}, bytes.NewReader(p.Cover))
			w := pageW - 24
			h := w * info.Height() / info.Width()
			pdf.ClipRoundedRectExt(12, 12, w, bannerH, 8, 8, 0, 0, false)
			pdf.ImageOptions("cover", 12, 12-(h-bannerH)*0.12, w, h, false, fpdf.ImageOptions{ImageType: "JPG"}, 0, "")
			pdf.ClipEnd()
			pdf.SetFont("Helvetica", "B", 26)
			lines := pdf.SplitText(p.Title, w*0.42)
			ty := 12 + bannerH/2 - float64(len(lines))*11/2
			if darkLeft(p.Cover) {
				// White, over a soft shadow, to hold on a busy picture.
				pdf.SetAlpha(0.55, "Normal")
				pdf.SetTextColor(20, 12, 8)
				pdf.SetXY(margin+4.5, ty+0.6)
				pdf.MultiCell(w*0.42, 11, text(p.Title), "", "L", false)
				pdf.SetAlpha(1, "Normal")
				pdf.SetTextColor(255, 255, 255)
			} else {
				accent.ink().shade(-0.45).text(pdf)
			}
			pdf.SetXY(margin+4, ty)
			pdf.MultiCell(w*0.42, 11, text(p.Title), "", "L", false)
			qy = 12 + bannerH + 14
		} else {
			band(pdf, 12, 12, pageW-24, 44, 8, brand)
			pdf.SetTextColor(255, 255, 255)
			pdf.SetFont("Helvetica", "B", 28)
			pdf.SetXY(margin+4, 24)
			pdf.MultiCell(pageW-2*margin-8, 12, text(p.Title), "", "C", false)
		}
		pdf.SetLineWidth(0.4)
		pdfLine.draw(pdf)
		pdf.RoundedRect(12, 12, pageW-24, pageH-24, 8, "1234", "D")
		qr := (pageW - 104) / 2
		pdf.SetLineWidth(0.4)
		pdfLine.draw(pdf)
		pdf.RoundedRect(qr-6, qy-6, 116, 116, 6, "1234", "D")
		pdf.RegisterImageOptionsReader("player-qr", fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(png))
		pdf.ImageOptions("player-qr", qr, qy, 104, 104, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
		pdf.SetY(qy + 120)
		accent.ink().text(pdf)
		pdf.SetFont("Helvetica", "B", 20)
		pdf.CellFormat(0, 10, text(words.scan), "", 1, "C", false, 0, "")
		pdfInk.text(pdf)
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
	const inset, bandH, pad = 10.0, 28.0, 10.0
	for i, c := range p.Cards {
		top := 0.0
		if i%2 == 0 {
			pdf.AddPage()
			pdf.SetLineWidth(0.3)
			pdf.SetDrawColor(170, 170, 170)
			pdf.SetDashPattern([]float64{2, 2}, 0)
			pdf.Line(8, half, pageW-8, half)
			pdf.SetDashPattern(nil, 0)
		} else {
			top = half
		}
		col, head := accent, brand
		if nums[i] == 0 {
			col, head = second, info
		}
		fx, fy, fw, fh := inset, top+inset, pageW-2*inset, half-2*inset
		// The card: the header in the look's gradient, on it a white
		// badge with the number in the look's colour; the rest plain.
		band(pdf, fx, fy, fw, bandH, 7, head)
		pdf.SetLineWidth(0.4)
		pdfLine.draw(pdf)
		pdf.RoundedRect(fx, fy, fw, fh, 7, "1234", "D")
		cx, cy := fx+pad+9, fy+bandH/2
		pdf.SetFillColor(255, 255, 255)
		pdf.Circle(cx, cy, 9, "F")
		col.ink().text(pdf)
		badge, label := "i", words.info
		if nums[i] > 0 {
			badge, label = fmt.Sprint(nums[i]), fmt.Sprintf(words.card, nums[i])
		}
		pdf.SetFont("Helvetica", "B", 20)
		pdf.SetXY(cx-9, cy-5)
		pdf.CellFormat(18, 10, badge, "", 0, "C", false, 0, "")
		pdf.SetTextColor(255, 255, 255)
		pdf.SetFont("Helvetica", "B", 26)
		pdf.SetXY(cx+14, cy-6)
		pdf.CellFormat(0, 12, text(label), "", 0, "L", false, 0, "")

		x, width := fx+pad, fw-2*pad
		pdfInk.text(pdf)
		pdf.SetFont("Helvetica", "B", 20)
		pdf.SetXY(x, fy+bandH+8)
		pdf.MultiCell(width, 9, text(strings.TrimSpace(c.Text)), "", "L", false)
		pdf.Ln(5)
		switch c.Kind {
		case trail.Quiz:
			pdf.SetLineWidth(0.4)
			for j, mark := range []string{"1", "X", "2"} {
				opt := ""
				if j < len(c.Options) {
					opt = c.Options[j]
				}
				y := pdf.GetY()
				pdfLine.draw(pdf)
				pdf.RoundedRect(x, y, width, 13, 6.5, "1234", "D")
				pdfMuted.draw(pdf)
				pdf.Circle(x+7, y+6.5, 4.6, "D")
				pdfInk.text(pdf)
				pdf.SetFont("Helvetica", "B", 13)
				pdf.SetXY(x+2.4, y+1.5)
				pdf.CellFormat(9.2, 10, mark, "", 0, "C", false, 0, "")
				pdfInk.text(pdf)
				pdf.SetFont("Helvetica", "", 16)
				pdf.SetXY(x+15, y+1.5)
				pdf.CellFormat(width-18, 10, text(opt), "", 0, "L", false, 0, "")
				pdf.SetY(y + 16)
			}
		case trail.Answer:
			y := pdf.GetY()
			pdf.SetLineWidth(0.4)
			pdfMuted.draw(pdf)
			pdf.SetDashPattern([]float64{1.5, 1.5}, 0)
			pdf.RoundedRect(x, y, width, 16, 5, "1234", "D")
			pdf.SetDashPattern(nil, 0)
			pdfMuted.text(pdf)
			pdf.SetFont("Helvetica", "I", 14)
			pdf.SetXY(x+6, y+3)
			pdf.CellFormat(width-12, 10, text(words.writeIt), "", 0, "L", false, 0, "")
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
	accent := hexRGB(p.Accent, rgb{173, 19, 87})
	pdf.AddPage()
	accent.ink().text(pdf)
	pdf.SetFont("Helvetica", "B", 20)
	pdf.CellFormat(0, 10, text(words.answers+" - "+p.Title), "", 1, "L", false, 0, "")
	pdf.SetLineWidth(0.8)
	accent.draw(pdf)
	pdf.Line(18, pdf.GetY()+1, 192, pdf.GetY()+1)
	pdf.Ln(6)
	nums := trail.Number(p.Cards)
	pdf.SetLineWidth(0.3)
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
		accent.ink().text(pdf)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.CellFormat(28, 7, text(fmt.Sprintf(words.card, nums[i])), "", 0, "L", false, 0, "")
		pdfInk.text(pdf)
		pdf.SetFont("Helvetica", "", 12)
		pdf.MultiCell(0, 7, text(strings.TrimSpace(c.Text)), "", "L", false)
		pdf.SetX(18 + 28)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.MultiCell(0, 7, text(words.right+": "+right), "", "L", false)
		pdf.Ln(2)
		accent.tint(0.3).draw(pdf)
		pdf.Line(18, pdf.GetY(), 192, pdf.GetY())
		pdf.Ln(3)
	}
	return pdf.Output(w)
}
