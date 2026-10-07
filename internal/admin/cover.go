package admin

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png" // covers uploaded as PNG
	"net/http"
	"time"

	_ "golang.org/x/image/webp" // and as WebP
)

// An adventure's own cover picture (look.Style.Cover): a 16:9 picture
// of its media, shown across the top of the join page and the players'
// welcome and on the printed cover, in place of its starter theme's.
// Teams fetch it from /media like the rest of the look; the join page,
// which has no team yet, by the player link (JoinCover).

// coverFile is a cover picture's type, version and, when asked, bytes.
type coverFile struct {
	Mime    string
	Sum     string
	Content []byte
}

// adventureCover is the cover picture of adventure slug, 0 when it has
// none; with content its bytes are read too.
func (h *Handlers) adventureCover(ctx context.Context, slug string, content bool) (int64, coverFile, error) {
	var (
		id  int64
		f   coverFile
		col = "NULL::bytea"
	)
	if content {
		col = "m.content"
	}
	err := h.DB.QueryRowContext(ctx, `
		SELECT m.id, m.mime_type, m.sha256, `+col+`
		FROM adventures a JOIN adventure_media m ON m.adventure_id = a.id AND m.kind = 'image'
		 AND m.id = NULLIF(a.look_style->>'cover', '')::bigint
		WHERE a.slug = $1
	`, slug).Scan(&id, &f.Mime, &f.Sum, &f.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, coverFile{}, nil
	}
	return id, f, err
}

// JoinCover is the cover picture of the hosted event a player link
// opens.
//
// GET /api/join/{token}/cover
func (h *Handlers) JoinCover(w http.ResponseWriter, r *http.Request) {
	p, ok := h.partyByPlayerToken(w, r)
	if !ok {
		return
	}
	id, f, err := h.adventureCover(r.Context(), p.AdvSlug, true)
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if id == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", f.Mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("ETag", `"`+f.Sum+`"`)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(f.Content))
}

// printCover is a cover picture as print takes it: a JPEG cut to 16:9
// about its middle, or nil when it can't be read.
func printCover(f coverFile) []byte {
	img, _, err := image.Decode(bytes.NewReader(f.Content))
	if err != nil {
		return nil
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w*9 > h*16 {
		cut := (w - h*16/9) / 2
		b = image.Rect(b.Min.X+cut, b.Min.Y, b.Max.X-cut, b.Max.Y)
	} else {
		cut := (h - w*9/16) / 2
		b = image.Rect(b.Min.X, b.Min.Y+cut, b.Max.X, b.Max.Y-cut)
	}
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 85}); err != nil {
		return nil
	}
	return buf.Bytes()
}
