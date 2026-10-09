// Package emailtpl renders every email Zonventure sends — to players,
// to designers, for signing in — in one of a few themes, coloured and
// signed with the organisation's brand: its logo, its name and its four
// colours. Each email is written once as Content and comes out as HTML
// for mail clients that show it and as text for those that do not.
//
// Email HTML is its own dialect: tables for layout, every style inline,
// no web fonts or scripts. The themes stay inside it, so they look the
// same in Gmail, Outlook and Apple Mail.
package emailtpl

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"html"
	"html/template"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/klahr/zonventure/internal/mailer"
	"github.com/klahr/zonventure/internal/mdx"
	"github.com/klahr/zonventure/internal/sitename"
)

// Theme is how an email is laid out; the brand colours it.
type Theme string

const (
	Classic   Theme = "classic"
	Adventure Theme = "adventure"
	Bold      Theme = "bold"
	Minimal   Theme = "minimal"
)

// ThemeInfo describes a theme for the picker.
type ThemeInfo struct {
	ID          Theme  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Themes are the ones an organisation can pick from, the default first.
var Themes = []ThemeInfo{
	{Classic, "Classic", "A clean card on a soft background, the logo on top and a coloured edge."},
	{Adventure, "Adventure", "Parchment and a serif heading, like a page from the game's own journal."},
	{Bold, "Bold", "A full-width band in your colour with the logo, and a large heading."},
	{Minimal, "Minimal", "Plain text on white with a small logo: reads like a personal note."},
}

// Valid reports whether t is a theme; "" is not one.
func Valid(t Theme) bool {
	for _, x := range Themes {
		if x.ID == t {
			return true
		}
	}
	return false
}

// Brand is who an email comes from and how it looks.
type Brand struct {
	Name    string
	LogoURL string
	Accent  string
	Accent2 string
	Surface string
	Ink     string
}

// Platform is the instance's own brand (named by ZV_SITE_NAME), for
// what no organisation sends.
func Platform() Brand {
	return Brand{Name: sitename.Name(), Accent: "#b0154f", Accent2: "#f59e0b", Surface: "#f7f5f2", Ink: "#1f2937"}
}

// Button is a call to action.
type Button struct {
	Label string
	URL   string
}

// Content is an email's words, independent of its theme.
type Content struct {
	// Lang is the language it is written in ("en", "sv"): for the page
	// around it and for mail clients.
	Lang string
	// Preheader is the line inboxes show after the subject.
	Preheader string
	Heading   string
	// Body is Markdown.
	Body string
	// Quote is quoted text, shown set off: what someone wrote.
	Quote   string
	Buttons []Button
	// Footer says why the reader gets this.
	Footer string
	// Picture shows above the words, when it is set.
	Picture Picture
}

var hexRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// fill gives a brand the platform's colour for any it lacks.
func (b Brand) fill() Brand {
	d := Platform()
	pick := func(v, def string) string {
		if hexRe.MatchString(strings.TrimSpace(v)) {
			return strings.TrimSpace(v)
		}
		return def
	}
	b.Accent, b.Accent2 = pick(b.Accent, d.Accent), pick(b.Accent2, d.Accent2)
	b.Surface, b.Ink = pick(b.Surface, d.Surface), pick(b.Ink, d.Ink)
	if strings.TrimSpace(b.Name) == "" {
		b.Name = d.Name
	}
	return b
}

// on is the text colour that reads on bg: white or near-black.
func on(bg string) string {
	r, _ := strconv.ParseInt(bg[1:3], 16, 0)
	g, _ := strconv.ParseInt(bg[3:5], 16, 0)
	b, _ := strconv.ParseInt(bg[5:7], 16, 0)
	lum := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 255
	if lum > 0.62 {
		return "#111827"
	}
	return "#ffffff"
}

// luminance is a colour's relative luminance (WCAG).
func luminance(hex string) float64 {
	lin := func(i int) float64 {
		v, _ := strconv.ParseInt(hex[i:i+2], 16, 0)
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(1) + 0.7152*lin(3) + 0.0722*lin(5)
}

// readable is fg when it reads on bg (WCAG AA), else what does: a
// brand's text colour picked for a dark site must not vanish on a
// white card.
func readable(fg, bg string) string {
	a, b := luminance(fg), luminance(bg)
	if a < b {
		a, b = b, a
	}
	if (a+0.05)/(b+0.05) >= 4.5 {
		return fg
	}
	return on(bg)
}

// mix blends hex a toward b by t (0..1), for tints the brand lacks.
func mix(a, b string, t float64) string {
	ch := func(s string, i int) float64 { v, _ := strconv.ParseInt(s[i:i+2], 16, 0); return float64(v) }
	out := "#"
	for i := 1; i < 7; i += 2 {
		out += fmt.Sprintf("%02x", int(ch(a, i)*(1-t)+ch(b, i)*t+0.5))
	}
	return out
}

// style is a theme's look, every value inline CSS.
type style struct {
	Page, Card, Band, Heading, Text, Muted, Quote, Button, ButtonText, Rule string
	// Font stacks are constants, trusted as CSS: html/template would
	// otherwise refuse their quotes and write ZgotmplZ.
	Font, HeadFont template.CSS
	CardWidth      int
	BandLogo       bool
}

func styleOf(t Theme, b Brand) style {
	sans := template.CSS(`-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif`)
	serif := template.CSS(`Georgia,'Times New Roman',serif`)
	s := style{Font: sans, HeadFont: sans, CardWidth: 600, Text: b.Ink, Muted: mix(b.Ink, "#ffffff", 0.45), Button: b.Accent, ButtonText: on(b.Accent)}
	switch t {
	case Adventure:
		parchment := mix(b.Surface, "#f1e4c3", 0.6)
		s.Page = mix(parchment, "#c9b48a", 0.25)
		s.Card = parchment
		s.HeadFont = serif
		s.Font = serif
		s.Rule = mix(b.Accent2, parchment, 0.3)
		s.Quote = mix(parchment, b.Accent2, 0.15)
	case Bold:
		s.Page = mix(b.Surface, "#ffffff", 0.3)
		s.Card = "#ffffff"
		s.Band = b.Accent
		s.BandLogo = true
		s.Button = b.Accent2
		s.ButtonText = on(b.Accent2)
		s.Quote = mix(b.Accent, "#ffffff", 0.9)
	case Minimal:
		s.Page = "#ffffff"
		s.Card = "#ffffff"
		s.CardWidth = 560
		s.Quote = "#f4f4f5"
	default: // Classic
		s.Page = b.Surface
		s.Card = "#ffffff"
		s.Rule = b.Accent
		s.Quote = mix(b.Accent, "#ffffff", 0.92)
	}
	s.Text = readable(b.Ink, s.Card)
	s.Muted = mix(s.Text, s.Card, 0.4)
	return s
}

var page = template.Must(template.New("email").Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light"><title>{{.Subject}}</title></head>
<body style="margin:0;padding:0;background:{{.S.Page}};">
<div style="display:none;max-height:0;overflow:hidden;opacity:0;">{{.C.Preheader}}</div>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:{{.S.Page}};"><tr><td align="center" style="padding:{{if eq .Theme "minimal"}}24px 16px{{else}}32px 16px{{end}};">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:{{.S.CardWidth}}px;background:{{.S.Card}};{{if eq .Theme "classic"}}border-top:4px solid {{.S.Rule}};border-radius:10px;box-shadow:0 1px 3px rgba(0,0,0,.08);{{end}}{{if eq .Theme "adventure"}}border:1px solid {{.S.Rule}};outline:3px double {{.S.Rule}};outline-offset:-8px;{{end}}{{if eq .Theme "bold"}}border-radius:12px;overflow:hidden;{{end}}">
{{if .S.BandLogo}}<tr><td style="background:{{.S.Band}};padding:28px 32px;">{{if .B.LogoURL}}<img src="{{.B.LogoURL}}" alt="{{.B.Name}}" height="40" style="display:block;height:40px;max-width:200px;border:0;">{{else}}<div style="font:700 20px/1.2 {{.S.HeadFont}};color:{{.BandText}};">{{.B.Name}}</div>{{end}}</td></tr>{{end}}
{{if and .C.Picture.URL .C.Picture.Wide}}<tr><td style="padding:{{if eq .Theme "adventure"}}14px 14px 0{{else}}0{{end}};line-height:0;font-size:0;"><img src="{{.C.Picture.URL}}" alt="{{.C.Picture.Alt}}" width="{{.S.CardWidth}}" style="display:block;width:100%;max-width:{{.S.CardWidth}}px;height:auto;border:0;{{.PictureRadius}}font:13px/1.4 {{.S.Font}};color:{{.S.Muted}};"></td></tr>{{end}}
<tr><td style="padding:{{if eq .Theme "minimal"}}8px 4px{{else if eq .Theme "adventure"}}40px 44px{{else}}32px{{end}};font:16px/1.6 {{.S.Font}};color:{{.S.Text}};">
{{if not .S.BandLogo}}<div style="margin-bottom:24px;{{if eq .Theme "adventure"}}text-align:center;{{end}}">{{if .B.LogoURL}}<img src="{{.B.LogoURL}}" alt="{{.B.Name}}" height="{{if eq .Theme "minimal"}}24{{else}}36{{end}}" style="{{if eq .Theme "adventure"}}display:inline-block;{{else}}display:block;{{end}}height:{{if eq .Theme "minimal"}}24{{else}}36{{end}}px;max-width:180px;border:0;">{{else}}<span style="font:700 {{if eq .Theme "minimal"}}14px{{else}}18px{{end}}/1.2 {{.S.HeadFont}};color:{{.B.Accent}};letter-spacing:.02em;">{{.B.Name}}</span>{{end}}</div>{{end}}
{{if eq .Theme "adventure"}}<div style="text-align:center;color:{{.S.Rule}};font-size:14px;letter-spacing:.6em;margin:-8px 0 12px;">✦ ✦ ✦</div>{{end}}
{{if and .C.Picture.URL (not .C.Picture.Wide)}}<div style="margin:0 0 20px;{{if ne .Theme "minimal"}}text-align:center;{{end}}"><img src="{{.C.Picture.URL}}" alt="{{.C.Picture.Alt}}" width="140" style="display:inline-block;width:140px;max-width:140px;height:auto;border:0;border-radius:16px;font:13px/1.4 {{.S.Font}};color:{{.S.Muted}};"></div>{{end}}
{{if .C.Heading}}<h1 style="margin:0 0 16px;font:700 {{if eq .Theme "bold"}}30px{{else if eq .Theme "minimal"}}20px{{else}}24px{{end}}/1.25 {{.S.HeadFont}};color:{{.S.Text}};{{if eq .Theme "adventure"}}text-align:center;{{end}}">{{.C.Heading}}</h1>{{end}}
{{.BodyHTML}}
{{if .C.Quote}}<div style="margin:16px 0;padding:12px 16px;background:{{.S.Quote}};border-left:4px solid {{.B.Accent}};border-radius:4px;white-space:pre-wrap;">{{.C.Quote}}</div>{{end}}
{{range .C.Buttons}}<table role="presentation" cellpadding="0" cellspacing="0" style="margin:24px {{if eq $.Theme "adventure"}}auto{{else}}0{{end}};"><tr><td style="{{if eq $.Theme "minimal"}}{{else}}background:{{$.S.Button}};border-radius:{{if eq $.Theme "adventure"}}4px{{else}}999px{{end}};{{end}}">{{if eq $.Theme "minimal"}}<a href="{{.URL}}" style="font:600 16px/1.4 {{$.S.Font}};color:{{$.B.Accent}};text-decoration:underline;">{{.Label}} →</a>{{else}}<a href="{{.URL}}" style="display:inline-block;padding:12px 28px;font:700 16px/1.2 {{$.S.Font}};color:{{$.S.ButtonText}};text-decoration:none;">{{.Label}}</a>{{end}}</td></tr></table>{{end}}
{{if .C.Footer}}<p style="margin:28px 0 0;padding-top:16px;border-top:1px solid {{.FooterRule}};font-size:13px;line-height:1.5;color:{{.S.Muted}};{{if eq .Theme "adventure"}}text-align:center;{{end}}">{{.C.Footer}}</p>{{end}}
</td></tr></table>
<p style="margin:16px 0 0;font:12px/1.4 {{.S.Font}};color:{{.FinePrint}};">{{.Signature}}</p>
</td></tr></table>
</body></html>`))

// Render lays out c in theme t, branded b, as HTML and as text.
func Render(t Theme, b Brand, subject string, c Content) (htmlOut, text string) {
	if !Valid(t) {
		t = Classic
	}
	b = b.fill()
	s := styleOf(t, b)
	var buf bytes.Buffer
	_ = page.Execute(&buf, map[string]any{
		"Theme":         string(t),
		"Lang":          Lang(c.Lang),
		"Signature":     signature(b, Lang(c.Lang)),
		"Subject":       subject,
		"C":             c,
		"B":             b,
		"S":             s,
		"BodyHTML":      template.HTML(styleBody(mdx.Render(c.Body), s, b)),
		"BandText":      bandText(s),
		"FooterRule":    mix(s.Card, s.Text, 0.12),
		"PictureRadius": pictureRadius(t),
		"FinePrint":     mix(s.Page, s.Text, 0.45),
	})
	return buf.String(), Text(c)
}

// signature is the line under every email: who sent it.
func signature(b Brand, lang string) string {
	if b.Name == sitename.Name() {
		return b.Name
	}
	return T(lang, "%s · sent with %s", b.Name, sitename.Name())
}

// pictureRadius rounds a banner's top corners as its card's are, unless
// a band sits above it.
func pictureRadius(t Theme) template.CSS {
	switch t {
	case Classic:
		return "border-radius:6px 6px 0 0;"
	}
	return ""
}

func bandText(s style) string {
	if s.Band == "" {
		return s.Text
	}
	return on(s.Band)
}

// Text is c as plain text: the email for clients that show no HTML.
func Text(c Content) string {
	var b strings.Builder
	if c.Heading != "" {
		b.WriteString(c.Heading + "\r\n\r\n")
	}
	if c.Body != "" {
		b.WriteString(strings.ReplaceAll(strings.TrimSpace(c.Body), "\n", "\r\n") + "\r\n")
	}
	if c.Quote != "" {
		b.WriteString("\r\n")
		for _, line := range strings.Split(c.Quote, "\n") {
			b.WriteString("> " + strings.TrimRight(line, "\r") + "\r\n")
		}
	}
	for _, x := range c.Buttons {
		fmt.Fprintf(&b, "\r\n%s: %s\r\n", x.Label, x.URL)
	}
	if c.Footer != "" {
		b.WriteString("\r\n-- \r\n" + c.Footer + "\r\n")
	}
	return b.String()
}

// styleBody gives the rendered Markdown the theme's inline styles: mail
// clients drop style sheets.
func styleBody(h string, s style, b Brand) string {
	r := strings.NewReplacer(
		"<p>", `<p style="margin:0 0 14px;">`,
		"<a href=", fmt.Sprintf(`<a style="color:%s;" href=`, html.EscapeString(b.Accent)),
		"<ul>", `<ul style="margin:0 0 14px;padding-left:22px;">`,
		"<ol>", `<ol style="margin:0 0 14px;padding-left:22px;">`,
		"<li>", `<li style="margin:0 0 6px;">`,
		"<h2>", fmt.Sprintf(`<h2 style="margin:20px 0 8px;font:700 18px/1.3 %s;">`, s.HeadFont),
		"<h3>", fmt.Sprintf(`<h3 style="margin:16px 0 6px;font:700 16px/1.3 %s;">`, s.HeadFont),
		"<blockquote>", fmt.Sprintf(`<blockquote style="margin:0 0 14px;padding:8px 14px;border-left:4px solid %s;background:%s;">`, b.Accent, s.Quote),
		"<code>", `<code style="font-family:Menlo,Consolas,monospace;font-size:14px;">`,
	)
	return r.Replace(h)
}

// Message is an email to `to` in theme t from brand b.
func Message(to []string, subject string, t Theme, b Brand, c Content) mailer.Message {
	htmlOut, text := Render(t, b, subject, c)
	return mailer.Message{To: to, Subject: subject, HTML: htmlOut, Text: text}
}

// Querier is what Lookup reads through.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Look is how an organisation's — or an event's — emails go out: in
// which theme, with which brand, in which language.
type Look struct {
	Theme Theme
	Brand Brand
	Lang  string
	// Cover is the event's adventure's picture for its players' emails
	// (coverOf); none without an event or a public URL.
	Cover Picture
}

// Lookup returns the look an organisation's emails go out in; with an
// event, its theme wins where it has one and its adventure's language is
// the email's. publicURL makes the logo's address absolute, as an email
// needs; without it, no logo.
func Lookup(ctx context.Context, q Querier, publicURL string, orgID, eventID int64) Look {
	var (
		org                       sql.NullInt64
		name                      string
		eventTheme, orgTheme      sql.NullString
		accent, accent2, bg, text sql.NullString
		lang                      sql.NullString
		logoVer                   string
		slug, theme               string
		ownCover                  bool
	)
	var err error
	if eventID > 0 {
		err = q.QueryRowContext(ctx, `
			SELECT o.id, o.name, e.email_theme, o.email_theme, o.accent_color, o.accent_secondary_color, o.background_color, o.text_color,
			       COALESCE(left(md5(o.logo_content), 8), ''), COALESCE(a.language, o.language), `+coverColumns+`
			FROM events e JOIN organizations o ON o.id = e.org_id LEFT JOIN adventures a ON a.slug = e.adventure_slug
			WHERE e.id = $1
		`, eventID).Scan(&org, &name, &eventTheme, &orgTheme, &accent, &accent2, &bg, &text, &logoVer, &lang, &slug, &theme, &ownCover)
	} else if orgID > 0 {
		err = q.QueryRowContext(ctx, `
			SELECT o.id, o.name, NULL, o.email_theme, o.accent_color, o.accent_secondary_color, o.background_color, o.text_color,
			       COALESCE(left(md5(o.logo_content), 8), ''), o.language
			FROM organizations o WHERE o.id = $1
		`, orgID).Scan(&org, &name, &eventTheme, &orgTheme, &accent, &accent2, &bg, &text, &logoVer, &lang)
	} else {
		return Look{Theme: Classic, Brand: Platform(), Lang: "en"}
	}
	if err != nil {
		return Look{Theme: Classic, Brand: Platform(), Lang: "en"}
	}
	b := Brand{Name: name, Accent: accent.String, Accent2: accent2.String, Surface: bg.String, Ink: text.String}
	// The logo's address carries a hash of it: mail clients cache images
	// by URL for good, so a new logo needs a new address to show.
	if base := strings.TrimRight(strings.TrimSpace(publicURL), "/"); logoVer != "" && base != "" {
		b.LogoURL = base + "/org-logo/" + strconv.FormatInt(org.Int64, 10) + "?v=" + logoVer
	}
	t := Theme(orgTheme.String)
	if Valid(Theme(eventTheme.String)) {
		t = Theme(eventTheme.String)
	}
	if !Valid(t) {
		t = Classic
	}
	lk := Look{Theme: t, Brand: b, Lang: Lang(lang.String)}
	if eventID > 0 {
		lk.Cover = coverOf(publicURL, lk.Lang, slug, theme, ownCover)
	}
	return lk
}
