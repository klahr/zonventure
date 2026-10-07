package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/klahr/zonventure/internal/adventure"
)

// partyLook is the look the party pages (join, host, book) wear: an
// adventure's palette, typefaces, shapes and pattern backdrop, so they
// match the game the teams then play. A picture backdrop and the look's
// finer style stay out: their media is the teams' to fetch, and the
// pages have no answer bar or step cards to style. Nil when the
// adventure has no look of its own. Theme is a party adventure's
// starter theme, for its cover picture.
func (h *Handlers) partyLook(ctx context.Context, slug string) (*adventure.Colors, string, error) {
	var (
		l                  adventure.Colors
		accent, second, bg sql.NullString
		ink                sql.NullString
		backdrop           []byte
		theme              string
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT accent_color, accent_secondary_color, background_color, text_color, font,
		       heading_font, corners, cards, card_outline, backdrop, trail_theme
		FROM adventures WHERE slug = $1
	`, slug).Scan(&accent, &second, &bg, &ink, &l.Font, &l.HeadingFont, &l.Corners, &l.Cards, &l.CardOutline, &backdrop, &theme)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	l.Accent, l.AccentSecondary, l.Background, l.Text = accent.String, second.String, bg.String, ink.String
	if backdrop != nil {
		var b adventure.Backdrop
		if json.Unmarshal(backdrop, &b) == nil && b.Kind != "picture" {
			l.Backdrop = &b
		}
	}
	if l.Empty() {
		return nil, theme, nil
	}
	return &l, theme, nil
}
