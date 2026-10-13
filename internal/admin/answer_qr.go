package admin

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-pdf/fpdf"
	"github.com/skip2/go-qrcode"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/orgslug"
)

// AnswerQR draws a QR code of one of a step's exact accepted answers, for
// the author to print and hang at the place. Only an answer the saved
// draft accepts, so the endpoint is no QR maker for anything else.
// With sheet=1 it is an A4 PDF labelled with the step and the adventure,
// never the answer, so codes for several places can be told apart once
// printed and give nothing away where they hang.
//
// GET /api/admin/adventures/{slug}/graph/qr?node=…&value=…[&sheet=1]
func (h *Handlers) AnswerQR(w http.ResponseWriter, r *http.Request) {
	advID, ok := h.graphTarget(w, r)
	if !ok {
		return
	}
	draft, err := graphstore.LoadDraft(r.Context(), h.DB, advID)
	if errors.Is(err, graphstore.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "this adventure has no graph yet")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading graph failed", err)
		return
	}
	value := r.URL.Query().Get("value")
	if !acceptsExactly(draft.Graph, r.URL.Query().Get("node"), value) {
		httpx.Error(w, http.StatusNotFound, "no such answer")
		return
	}
	png, err := qrcode.Encode(value, qrcode.Medium, 600)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "qr encode failed", err)
		return
	}
	if r.URL.Query().Get("sheet") == "1" {
		var title string
		if err := h.DB.QueryRowContext(r.Context(), `SELECT title FROM adventures WHERE id = $1`, advID).Scan(&title); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "loading adventure failed", err)
			return
		}
		n, _ := draft.Graph.Node(r.URL.Query().Get("node"))
		step := n.Title
		if step == "" {
			step = n.Slug
		}
		var buf bytes.Buffer
		if err := writeAnswerQRSheet(&buf, title, step, png); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "pdf failed", err)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s-qr.pdf"`, orgslug.Slugify(step)))
		_, _ = w.Write(buf.Bytes())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write(png)
}

// writeAnswerQRSheet draws one QR code on an A4 page under the step's
// name, with the adventure's name above it.
func writeAnswerQRSheet(w io.Writer, adventure, step string, png []byte) error {
	pdf := fpdf.New("P", "mm", pdfPageSize, "")
	text := recapText(pdf)
	pdf.SetAutoPageBreak(false, 0)
	const pageW, margin, qr = 210.0, 20.0, 130.0
	pdf.AddPage()
	pdf.SetTextColor(90, 90, 90)
	pdf.SetFont("Helvetica", "", 14)
	pdf.SetXY(margin, 28)
	pdf.MultiCell(pageW-2*margin, 7, text(adventure), "", "C", false)
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Helvetica", "B", 30)
	pdf.SetXY(margin, pdf.GetY()+6)
	pdf.MultiCell(pageW-2*margin, 13, text(step), "", "C", false)
	pdf.RegisterImageOptionsReader("qr", fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(png))
	pdf.ImageOptions("qr", (pageW-qr)/2, pdf.GetY()+16, qr, qr, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	return pdf.Output(w)
}

// acceptsExactly reports whether the step has value as an exact answer.
func acceptsExactly(g *graph.Graph, node, value string) bool {
	n, ok := g.Node(node)
	if !ok || n.PuzzleCfg == nil || value == "" {
		return false
	}
	for _, o := range n.PuzzleCfg.Outputs {
		for _, m := range o.Matchers {
			if m.Kind == match.Exact && m.Value == value {
				return true
			}
		}
	}
	return false
}
