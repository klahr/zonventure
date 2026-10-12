// Package look is how an adventure looks to players: the palette,
// typefaces, shapes and backdrop of the adventure itself
// (adventure.Colors) and of the themes saved in its graph. The player
// paints it (play-ui/brand.ts, play-ui/backdrop.ts); nothing here is CSS.
package look

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Look is an adventure's look — its brand palette, typefaces, shapes
// and backdrop — or a theme saved in its graph. Any field left at
// the zero value means "fall back to the default theme". Values are stored
// as "#rrggbb" strings so they paste straight into CSS.
type Look struct {
	Accent          string `json:"accent,omitempty"`
	AccentSecondary string `json:"accent_secondary,omitempty"`
	Background      string `json:"background,omitempty"`
	Text            string `json:"text,omitempty"`
	// SubmitBackground paints the sticky answer dock at the bottom of
	// /play. Independent of Background so a brand can keep a neutral
	// page body and still have a contrasting brand panel hosting the
	// submit / scan buttons. The play UI also reuses this value as
	// the buttons' text colour, producing the "punched-through"
	// effect the customisation copy describes.
	SubmitBackground string `json:"submit_bg,omitempty"`
	// Font is one of Fonts by key; empty is the default typeface.
	Font string `json:"font,omitempty"`
	// HeadingFont sets titles apart from body text, from the same list;
	// empty follows Font.
	HeadingFont string `json:"heading_font,omitempty"`
	// Corners is one of Corners; empty is "rounded".
	Corners string `json:"corners,omitempty"`
	// Cards is one of Cards; empty is "raised".
	Cards string `json:"cards,omitempty"`
	// CardOutline draws a clear line round cards, raised or flat.
	CardOutline bool `json:"card_outline,omitempty"`
	// Backdrop is a pattern behind the cards; nil is the plain page.
	Backdrop *Backdrop `json:"backdrop,omitempty"`
	// Style holds the look's finer choices, one object rather than a
	// column each: see Style. Nil is the defaults throughout.
	Style *Style `json:"style,omitempty"`
}

// Style is a look's finer choices. Every field's zero value is the
// default look, so an empty Style changes nothing.
type Style struct {
	// CardOpacity lets the backdrop show through the cards: 1 (or 0,
	// unset) is solid, down to MinCardOpacity.
	CardOpacity float64 `json:"card_opacity,omitempty"`
	// CardBlur frosts what shows through a see-through card, so text on
	// it stays readable over a busy backdrop.
	CardBlur bool `json:"card_blur,omitempty"`
	// Transition is how one step gives way to the next: one of
	// Transitions; empty is the player's own gentle rise.
	Transition string `json:"transition,omitempty"`

	// The buttons: ButtonShape one of ButtonShapes (empty is a pill),
	// ButtonFill one of ButtonFills (empty is classic: the accent's
	// gradient, punched through a coloured answer bar; a chosen fill
	// keeps the accent there too),
	// ButtonSize one of ButtonSizes (empty is normal).
	ButtonShape string `json:"button_shape,omitempty"`
	ButtonFill  string `json:"button_fill,omitempty"`
	ButtonSize  string `json:"button_size,omitempty"`
	// AnswerButton and AnswerButtonText colour the answer bar's button
	// (#rrggbb), over the fill and the bar; empty AnswerButtonText is
	// black or white, whichever reads on AnswerButton.
	AnswerButton     string `json:"answer_button_color,omitempty"`
	AnswerButtonText string `json:"answer_button_text_color,omitempty"`
	// Dock is the answer bar's style: one of Docks; empty is the bar
	// across the bottom.
	Dock string `json:"dock,omitempty"`
	// TextScale sizes the text, titles with it: MinTextScale–MaxTextScale;
	// 0 is 1.
	TextScale float64 `json:"text_scale,omitempty"`
	// TitleStyle dresses step titles: one of TitleStyles; empty is plain.
	// TitleUpper sets them in capitals.
	TitleStyle string `json:"title_style,omitempty"`
	TitleUpper bool   `json:"title_upper,omitempty"`

	// Celebrate is how a right answer is marked: one of Celebrations;
	// empty is the confetti.
	Celebrate string `json:"celebrate,omitempty"`
	// ConfettiColors paint the confetti (up to MaxBackdropColors, #rrggbb);
	// ConfettiEmoji, a short run of emoji, rains instead of paper.
	ConfettiColors []string `json:"confetti_colors,omitempty"`
	ConfettiEmoji  string   `json:"confetti_emoji,omitempty"`
	// CheerText and MissText replace "Correct!" and "Not quite" — in the
	// adventure's own words, up to MaxCheerLen characters.
	CheerText string `json:"cheer_text,omitempty"`
	MissText  string `json:"miss_text,omitempty"`
	// SoundRight and SoundWrong are adventure_media ids of sounds played
	// on a right and a wrong answer.
	SoundRight int64 `json:"sound_right,omitempty"`
	SoundWrong int64 `json:"sound_wrong,omitempty"`
	// Cover is an adventure_media id: the adventure's own cover picture,
	// 16:9, across the top of the join page and the players' welcome and
	// on a printed cover, in place of its starter theme's.
	// Only the adventure's own look shows it, not a theme in its graph.
	Cover int64 `json:"cover,omitempty"`
}

// Celebrations: "quiet" keeps the mark without confetti, "none" leaves
// only the words under the answer.
var Celebrations = []string{"quiet", "none"}

// The bounds of a celebration's own words.
const (
	MaxCheerLen = 40
	MaxEmojiLen = 8
)

// Sounds are the style's sound ids, the unset ones left out.
func (s Style) Sounds() []int64 {
	var out []int64
	for _, id := range []int64{s.SoundRight, s.SoundWrong} {
		if id > 0 {
			out = append(out, id)
		}
	}
	return out
}

// MediaIDs are the look's own media: its picture, its sounds and its cover.
func (c Look) MediaIDs() []int64 {
	var out []int64
	if b := c.Backdrop; b != nil && b.Kind == "picture" && b.Media > 0 {
		out = append(out, b.Media)
	}
	if c.Style != nil {
		out = append(out, c.Style.Sounds()...)
		if c.Style.Cover > 0 {
			out = append(out, c.Style.Cover)
		}
	}
	return out
}

// The finer choices' lists and bounds.
var (
	ButtonShapes = []string{"rounded", "square"}
	ButtonFills  = []string{"gradient", "flat", "outline", "soft"}
	ButtonSizes  = []string{"small", "large"}
	Docks        = []string{"floating"}
	TitleStyles  = []string{"ribbon", "underline", "shadow"}
)

const (
	MinTextScale = 0.85
	MaxTextScale = 1.4
)

// Transitions are the ways a step may enter: "none" is no movement at all.
var Transitions = []string{"none", "fade", "slide", "zoom", "flip", "pop"}

// MinCardOpacity keeps a card a card: below it, text would sit on the
// backdrop itself.
const MinCardOpacity = 0.2

// Validate reports the first value out of bounds.
func (s Style) Validate() error {
	if s.Cover < 0 {
		return errors.New("cover must be a media id")
	}
	if s.CardOpacity != 0 && (s.CardOpacity < MinCardOpacity || s.CardOpacity > 1) {
		return fmt.Errorf("card_opacity must be %g–1", MinCardOpacity)
	}
	if s.Transition != "" && !slices.Contains(Transitions, s.Transition) {
		return fmt.Errorf("transition must be one of %v", Transitions)
	}
	for _, c := range []struct {
		name, v string
		list    []string
	}{
		{"button_shape", s.ButtonShape, ButtonShapes},
		{"button_fill", s.ButtonFill, ButtonFills},
		{"button_size", s.ButtonSize, ButtonSizes},
		{"dock", s.Dock, Docks},
		{"title_style", s.TitleStyle, TitleStyles},
	} {
		if c.v != "" && !slices.Contains(c.list, c.v) {
			return fmt.Errorf("%s must be empty or one of %v", c.name, c.list)
		}
	}
	if !hexOrEmpty(s.AnswerButton) || !hexOrEmpty(s.AnswerButtonText) {
		return errors.New(`answer_button_color and answer_button_text_color must be empty or "#rrggbb"`)
	}
	if s.TextScale != 0 && (s.TextScale < MinTextScale || s.TextScale > MaxTextScale) {
		return fmt.Errorf("text_scale must be %g–%g", MinTextScale, MaxTextScale)
	}
	if s.Celebrate != "" && !slices.Contains(Celebrations, s.Celebrate) {
		return fmt.Errorf("celebrate must be empty or one of %v", Celebrations)
	}
	if len(s.ConfettiColors) > MaxBackdropColors {
		return fmt.Errorf("at most %d confetti colours", MaxBackdropColors)
	}
	for _, c := range s.ConfettiColors {
		if c == "" || !hexOrEmpty(c) {
			return errors.New(`each confetti colour must be "#rrggbb"`)
		}
	}
	for _, t := range []struct {
		name, v string
		max     int
	}{{"confetti_emoji", s.ConfettiEmoji, MaxEmojiLen}, {"cheer_text", s.CheerText, MaxCheerLen}, {"miss_text", s.MissText, MaxCheerLen}} {
		if utf8.RuneCountInString(t.v) > t.max || strings.ContainsFunc(t.v, unicode.IsControl) {
			return fmt.Errorf("%s must be at most %d characters on one line", t.name, t.max)
		}
	}
	if s.SoundRight < 0 || s.SoundWrong < 0 {
		return errors.New("a sound must be a media id")
	}
	return nil
}

// Corners are the corner styles a theme may pick: pills and round cards,
// small radii, or square. Cards are how panels stand off the page: a
// shadow, or a tinted fill.
var (
	Corners = map[string]bool{"rounded": true, "soft": true, "sharp": true}
	Cards   = map[string]bool{"raised": true, "flat": true}
)

// Fonts are the typefaces an adventure may choose, by key. A short list
// rather than any font name: each ships with the player, self-hosted from
// web/packages/play-ui/src/fonts, and has been checked to stay legible
// outdoors on a phone. Chewy, Luckiest Guy, Creepster, Pacifico, VT323,
// Orbitron and Press Start 2P are for titles; the designer offers them
// only there.
var Fonts = map[string]string{
	"inconsolata":    "Inconsolata",    // terminal — the Obscurium mysteries
	"special-elite":  "Special Elite",  // typewriter
	"merriweather":   "Merriweather",   // book
	"fredoka":        "Fredoka",        // playful
	"baloo-2":        "Baloo 2",        // bubbly
	"patrick-hand":   "Patrick Hand",   // handwritten
	"chewy":          "Chewy",          // cartoon, titles only
	"luckiest-guy":   "Luckiest Guy",   // comic, titles only
	"nunito":         "Nunito",         // friendly
	"inter":          "Inter",          // clean
	"creepster":      "Creepster",      // spooky, titles only
	"pacifico":       "Pacifico",       // romantic, titles only
	"vt323":          "VT323",          // retro terminal, titles only
	"orbitron":       "Orbitron",       // futuristic, titles only
	"press-start-2p": "Press Start 2P", // arcade, titles only
}

// Empty reports whether no colours are set — handy for skipping the field
// entirely in JSON responses.
func (c Look) Empty() bool {
	return c.Accent == "" && c.AccentSecondary == "" && c.Background == "" && c.Text == "" && c.SubmitBackground == "" && c.Font == "" &&
		c.HeadingFont == "" && c.Corners == "" && c.Cards == "" && !c.CardOutline && c.Backdrop == nil && c.Style == nil
}

// Backdrop is a pattern painted on the page behind the cards, from a
// fixed set of kinds and ranges: the player builds the CSS from these
// values (play-ui/backdrop.ts), so an author never writes CSS. Text sits
// on the cards, which stay solid, so a backdrop cannot hide it.
type Backdrop struct {
	// Kind is one of BackdropKinds.
	Kind string `json:"kind"`
	// Color paints the pattern; empty follows the palette (the text
	// colour, or the accent for confetti and gradients), and so a step's
	// theme. Color2 is a second colour for stripes, confetti and gradients;
	// empty follows the palette, or leaves every other stripe clear.
	Color  string `json:"color,omitempty"`
	Color2 string `json:"color2,omitempty"`
	// Opacity is how strongly the pattern shows over the page colour.
	Opacity float64 `json:"opacity"`
	// Scale is the size of one repeat, in CSS pixels.
	Scale float64 `json:"scale"`
	// Angle turns the kinds that have a direction, in degrees.
	Angle float64 `json:"angle,omitempty"`
	// Fade melts the pattern into the page colour toward the bottom.
	Fade bool `json:"fade,omitempty"`
	// Fixed keeps the pattern still while the page scrolls over it.
	Fixed bool `json:"fixed,omitempty"`
	// Motion drifts the pattern slowly; never under reduced motion.
	Motion bool `json:"motion,omitempty"`
	// Points are the colour spots of a "spots" backdrop, which blend into
	// each other; Scale is then how far each spreads.
	Points []Spot `json:"points,omitempty"`
	// Colors are confetti's or stripes' colours in turn — a rainbow, a
	// party — in place of Color and Color2; for a horizon, its sky from
	// the top down to the horizon.
	Colors []string `json:"colors,omitempty"`
	// Media is a "picture" backdrop's image, an adventure_media id; Blur
	// softens it, in CSS pixels (0–MaxPictureBlur), so text over it reads.
	Media int64   `json:"media,omitempty"`
	Blur  float64 `json:"blur,omitempty"`
}

// MaxPictureBlur bounds how far a picture backdrop may be blurred.
const MaxPictureBlur = 20

// MaxBackdropColors bounds a backdrop's colour list.
const MaxBackdropColors = 8

// Spot is one colour of a "spots" backdrop, placed in percent of the
// page's width and height. An empty colour follows the palette.
type Spot struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Color string  `json:"color,omitempty"`
}

// MaxSpots bounds a "spots" backdrop: each spot is a layer the phone paints.
const MaxSpots = 12

// BackdropKinds are the patterns a backdrop may be.
var BackdropKinds = map[string]bool{
	"dots": true, "stripes": true, "checks": true, "grid": true, "sunburst": true,
	"waves": true, "zigzag": true, "confetti": true, "snow": true, "gradient": true, "spots": true, "horizon": true, "lined": true, "picture": true,
}

// The ranges a backdrop's numbers must keep to. MaxBackdropOpacity keeps
// a pattern a background: past it, it fights the cards for attention.
// Colour spots and a horizon are the background itself, so they may go
// to full colour.
const (
	MinBackdropOpacity = 0.02
	MaxBackdropOpacity = 0.6
	MaxSpotsOpacity    = 1
	MinBackdropScale   = 8
	MaxBackdropScale   = 200
)

// Validate reports the first value out of bounds.
func (b Backdrop) Validate() error {
	if !BackdropKinds[b.Kind] {
		return errors.New("kind is not a known pattern")
	}
	if !hexOrEmpty(b.Color) || !hexOrEmpty(b.Color2) {
		return errors.New(`colours must be empty or "#rrggbb"`)
	}
	maxOpacity := MaxBackdropOpacity
	if b.Kind == "spots" || b.Kind == "horizon" || b.Kind == "picture" {
		maxOpacity = MaxSpotsOpacity
	}
	if b.Opacity < MinBackdropOpacity || b.Opacity > maxOpacity {
		return fmt.Errorf("opacity must be %g–%g", MinBackdropOpacity, maxOpacity)
	}
	switch {
	case b.Kind == "spots" && (len(b.Points) == 0 || len(b.Points) > MaxSpots):
		return fmt.Errorf("spots needs 1–%d points", MaxSpots)
	case b.Kind != "spots" && len(b.Points) > 0:
		return errors.New("only spots has points")
	}
	if len(b.Colors) > 0 && b.Kind != "confetti" && b.Kind != "snow" && b.Kind != "stripes" && b.Kind != "horizon" {
		return errors.New("only confetti, stripes and horizon take a list of colours")
	}
	if len(b.Colors) > MaxBackdropColors {
		return fmt.Errorf("at most %d colours", MaxBackdropColors)
	}
	for _, c := range b.Colors {
		if c == "" || !hexOrEmpty(c) {
			return errors.New(`each colour must be "#rrggbb"`)
		}
	}
	switch {
	case b.Kind == "picture" && b.Media <= 0:
		return errors.New("a picture needs its image")
	case b.Kind != "picture" && (b.Media != 0 || b.Blur != 0):
		return errors.New("only a picture has an image and a blur")
	case b.Blur < 0 || b.Blur > MaxPictureBlur:
		return fmt.Errorf("blur must be 0–%d", MaxPictureBlur)
	}
	for _, p := range b.Points {
		if p.X < 0 || p.X > 100 || p.Y < 0 || p.Y > 100 {
			return errors.New("a spot must lie at 0–100 % across and down")
		}
		if !hexOrEmpty(p.Color) {
			return errors.New(`a spot's colour must be empty or "#rrggbb"`)
		}
	}
	if b.Scale < MinBackdropScale || b.Scale > MaxBackdropScale {
		return fmt.Errorf("scale must be %d–%d", MinBackdropScale, MaxBackdropScale)
	}
	if b.Angle < 0 || b.Angle >= 360 {
		return errors.New("angle must be 0–359")
	}
	return nil
}

func hexOrEmpty(s string) bool {
	if s == "" {
		return true
	}
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for i := 1; i < 7; i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// Validate reports the first value of a look out of bounds: colours
// #rrggbb or empty, fonts, corners and cards from their lists, and the
// backdrop's own ranges. The admin API checks the adventure's look field
// by field; a theme saved in a graph is checked whole, here.
func (c Look) Validate() error {
	for _, v := range []string{c.Accent, c.AccentSecondary, c.Background, c.Text, c.SubmitBackground} {
		if !hexOrEmpty(v) {
			return errors.New(`colours must be empty or "#rrggbb"`)
		}
	}
	for _, f := range []string{c.Font, c.HeadingFont} {
		if _, ok := Fonts[f]; !ok && f != "" {
			return fmt.Errorf("font %q is not one of the listed fonts", f)
		}
	}
	if c.Corners != "" && !Corners[c.Corners] {
		return fmt.Errorf("corners %q is not rounded, soft or sharp", c.Corners)
	}
	if c.Cards != "" && !Cards[c.Cards] {
		return fmt.Errorf("cards %q is not raised or flat", c.Cards)
	}
	if c.Backdrop != nil {
		if err := c.Backdrop.Validate(); err != nil {
			return fmt.Errorf("backdrop %w", err)
		}
	}
	if c.Style != nil {
		if err := c.Style.Validate(); err != nil {
			return fmt.Errorf("style %w", err)
		}
	}
	return nil
}
