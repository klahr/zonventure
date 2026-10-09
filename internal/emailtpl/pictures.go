package emailtpl

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
)

// Picture is an email's picture: a banner across the top of the card
// (Wide), or a small spot above the heading. The zero Picture is none.
// Nothing an email says lives only in its picture: mail clients that
// block images show Alt.
type Picture struct {
	URL  string
	Alt  string
	Wide bool
}

// The house pictures (ILLUSTRATIONS.md), by id: the file under
// /assets/email/ the server serves (internal/server/web/assets/email),
// whether it is a banner, and its alt text. A new picture is one line
// here and its file there.
var housePictures = map[string]struct {
	file string
	wide bool
	alt  string
}{
	"header":      {"header.jpg", true, "A dotted route across a town, from map pin to map pin"},
	"check-email": {"check-email.jpg", false, "An envelope flying out of a phone"},
	"invitation":  {"invitation.jpg", false, "A golden ticket rising out of an envelope"},
	"gift-box":    {"gift-box.jpg", false, "A wrapped gift box with a key tag"},
}

// The house picture each kind of email shows; one name each, so a
// picture made for one later is a one-line change.
const (
	PictureVerify   = "check-email"
	PictureReset    = "check-email"
	PictureGiftBox  = "gift-box"
	PictureFallback = "header"
)

// CoverThemes are the starter themes with an email cover
// (/assets/email/covers/<id>.jpg, made by `make theme-cover`).
var CoverThemes = []string{"autumn", "back-to-school", "candy", "christmas", "crayfish", "detective", "easter", "halloween", "ice-cream", "jungle", "lucia", "midsummer", "new-year", "ocean", "office", "party", "pen-and-paper", "pirate", "rainbow", "retro-80s", "space", "spring", "summer", "sunshine", "unicorn", "valentines", "walpurgis", "winter"}

// HouseFiles are the house pictures' files, for the server to serve.
func HouseFiles() []string {
	out := make([]string, 0, len(housePictures))
	for _, p := range housePictures {
		out = append(out, p.file)
	}
	return out
}

// House is house picture id at base, the instance's public URL. No
// base, or an unknown id, is no picture: an email needs an absolute
// address.
func House(base, lang, id string) Picture {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	p, ok := housePictures[id]
	if !ok || base == "" {
		return Picture{}
	}
	return Picture{URL: base + "/assets/email/" + p.file, Alt: T(lang, p.alt), Wide: p.wide}
}

// ThemeCover is starter theme id's cover at base; no picture for a
// theme without one, or without a base.
func ThemeCover(base, lang, id string) Picture {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return Picture{}
	}
	for _, t := range CoverThemes {
		if t == id {
			return Picture{URL: base + "/assets/email/covers/" + id + ".jpg", Alt: T(lang, "The adventure's cover picture"), Wide: true}
		}
	}
	return Picture{}
}

// coverColumns select, from adventures a, what coverOf takes: its
// slug, its starter theme and whether it has a cover picture of its own.
// NULL-safe for a LEFT JOIN.
const coverColumns = `COALESCE(a.slug, ''), COALESCE(a.trail_theme, ''),
	COALESCE(EXISTS (SELECT 1 FROM adventure_media m
	                 WHERE m.adventure_id = a.id AND m.kind = 'image'
	                   AND m.id = NULLIF(a.look_style->>'cover', '')::bigint), FALSE)`

// coverOf is an adventure's picture for its players' emails: its own
// cover, else its starter theme's, else the house banner. No base, no
// picture: an email needs an absolute address.
func coverOf(base, lang, slug, theme string, own bool) Picture {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return Picture{}
	}
	if own && slug != "" {
		return Picture{URL: base + "/adventure-assets/cover/" + url.PathEscape(slug), Alt: T(lang, "The adventure's cover picture"), Wide: true}
	}
	if p := ThemeCover(base, lang, theme); p.URL != "" {
		return p
	}
	return House(base, lang, PictureFallback)
}

// CoverFor is adventure slug's picture for its players' emails (see
// coverOf), for an email sent outside an event.
func CoverFor(ctx context.Context, q Querier, publicURL, lang, slug string) Picture {
	var (
		s, theme string
		own      bool
	)
	err := q.QueryRowContext(ctx, `SELECT `+coverColumns+` FROM adventures a WHERE a.slug = $1`, slug).Scan(&s, &theme, &own)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Picture{}
	}
	return coverOf(publicURL, lang, s, theme, own)
}
