package public

import (
	"context"
	"database/sql"
	"errors"
	"net/url"

	"github.com/klahr/zonventure/internal/graph/look"
)

// LookAssets resolves a look's pictures and typefaces to URLs. The
// server fills it from the play app's build; a nil func leaves the
// pages with the look's colours only.
type LookAssets struct {
	// ThemeCover is the cover picture of a starter theme id, "" for none.
	ThemeCover func(id string) string
	// Font is the stylesheet that loads a look font key, "" for none.
	Font func(key string) string
}

// PageLook is what a server-rendered page (store, recap, live board)
// shows of an adventure's look beyond its colours: the cover picture —
// its own, else its starter theme's — and the title typeface.
type PageLook struct {
	CoverURL string `json:"cover_url,omitempty"`
	// HeadingFont is the typeface's family name; FontCSS loads it.
	HeadingFont string `json:"heading_font,omitempty"`
	FontCSS     string `json:"font_css,omitempty"`
}

// LoadPageLook is the PageLook of adventure slug, nil when it has
// neither a cover nor a typeface the page can load.
func LoadPageLook(ctx context.Context, db *sql.DB, a LookAssets, slug string) (*PageLook, error) {
	var (
		font, theme string
		own         bool
	)
	err := db.QueryRowContext(ctx, `SELECT `+pageLookColumns+` FROM adventures a WHERE a.slug = $1`, slug).
		Scan(&font, &theme, &own)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a.pageLook(slug, font, theme, own), nil
}

// pageLook is the PageLook of adventure slug from its look font key,
// starter theme and whether it has a cover picture of its own; nil for
// nothing to show.
func (a LookAssets) pageLook(slug, font, theme string, own bool) *PageLook {
	var l PageLook
	if own {
		l.CoverURL = "/adventure-assets/cover/" + url.PathEscape(slug)
	} else if a.ThemeCover != nil {
		l.CoverURL = a.ThemeCover(theme)
	}
	if a.Font != nil {
		if css := a.Font(font); css != "" {
			l.HeadingFont, l.FontCSS = look.Fonts[font], css
		}
	}
	if l == (PageLook{}) {
		return nil
	}
	return &l
}

// pageLookColumns select, from adventures a, what pageLook takes: the
// look font key, the starter theme and whether the cover is the
// adventure's own. NULL-safe for a LEFT JOIN.
const pageLookColumns = `
	COALESCE(NULLIF(a.heading_font, ''), a.font, ''), COALESCE(a.trail_theme, ''),
	COALESCE(EXISTS (SELECT 1 FROM adventure_media m
	                 WHERE m.adventure_id = a.id AND m.kind = 'image'
	                   AND m.id = NULLIF(a.look_style->>'cover', '')::bigint), FALSE)`
