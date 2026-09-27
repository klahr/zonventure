package graph

import (
	"slices"
	"strconv"
	"strings"

	"github.com/klahr/zonventure/internal/graph/cond"
)

// BlockKind is what a content block shows.
type BlockKind string

const (
	BlockText  BlockKind = "text"
	BlockImage BlockKind = "image"
	BlockAudio BlockKind = "audio"
	BlockVideo BlockKind = "video"
	// BlockCompass points the phone at a zone, with the distance to it.
	BlockCompass BlockKind = "compass"
	// BlockMap shows a zone on a map, with the team on it.
	BlockMap BlockKind = "map"
	// BlockNote is text in costume: a paper note, a telegram, a text
	// message, a newspaper clipping, a sticky note.
	BlockNote BlockKind = "note"
	// BlockChat is a scripted conversation, shown a message at a time.
	BlockChat BlockKind = "chat"
	// BlockCipher is a reference sheet for decoding: a cipher's key.
	BlockCipher BlockKind = "cipher"
	// BlockHotspots is a picture whose tapped regions show captions.
	BlockHotspots BlockKind = "hotspots"
	// BlockDivider paces a step: a line, a gap, an ornament.
	BlockDivider BlockKind = "divider"
	// BlockRadar pulses warmer as the team nears a zone, with no direction.
	BlockRadar BlockKind = "radar"
	// BlockOverlay lays a picture over the camera, to line up with the
	// street: "stand where this photo was taken".
	BlockOverlay BlockKind = "overlay"
	// BlockLevel is a spirit level that shows Text once the phone is
	// held at Pitch degrees: flat, or pointed up at a tower.
	BlockLevel BlockKind = "level"
	// BlockNoise is a sound meter that shows Text once the team is loud
	// enough. Shown by the phone: a flourish, not a secret.
	BlockNoise BlockKind = "noise"
	// BlockTorch is a dark picture the team explores with a torch.
	BlockTorch BlockKind = "torch"
	// BlockCountdown counts down to Seconds after the step or the start.
	BlockCountdown BlockKind = "countdown"
	// BlockStatus shows what the team carries and chosen counters.
	BlockStatus BlockKind = "status"
	// BlockStandings shows the team's place among the event's teams.
	BlockStandings BlockKind = "standings"
	// BlockTitle is where the step's title shows, and BlockPoints where a
	// puzzle says what it is worth. A puzzle or story may have one title,
	// a puzzle one points; Normalize adds them to a step once. The author
	// moves them, or removes them to show no title or points.
	BlockTitle  BlockKind = "title"
	BlockPoints BlockKind = "points"
	// BlockQuiz is a quiz-walk question: Text, three Options called 1, X
	// and 2, and the right Answer, worth Worth points (default 1). The
	// team picks once; it learns what was right, and gets the points,
	// when it finishes. It never routes.
	BlockQuiz BlockKind = "quiz"
	// BlockTiebreak asks for a number, Target the right one. Among teams
	// with the same score, the closest guess ranks first.
	BlockTiebreak BlockKind = "tiebreak"
	// BlockSplit is a secret in Parts, one part to each player's phone, so
	// the team must talk to put it together. A player alone sees them all.
	BlockSplit BlockKind = "split"
	// BlockBearing shows Text once the phone points at Heading degrees
	// from north and is held there. Shown by the phone, like level.
	BlockBearing BlockKind = "bearing"
	// BlockCall is an incoming call from Who: the phone rings, and
	// answering plays Media, a sound.
	BlockCall BlockKind = "call"
	// BlockSuspects is a board of Cards the team strikes out as it rules
	// them out; what it struck is kept in Picks, and never scores.
	BlockSuspects BlockKind = "suspects"
	// BlockPhoto asks the team for a picture (Text says of what). An
	// arranger approves or denies it later, and an approved one is worth
	// Worth points; the team moves on either way, and may retake a
	// denied one.
	BlockPhoto BlockKind = "photo"
	// BlockSpotDiff is two pictures, Media and Other, with the
	// differences in Spots for the team to find by tapping.
	BlockSpotDiff BlockKind = "spotdiff"
	// BlockFlasher flashes Text in Morse on the screen, and by vibration
	// where the phone can, at Speed words a minute.
	BlockFlasher BlockKind = "flasher"
	// BlockLetters lays out Text's letters as tiles to drag about while
	// solving an anagram. It never grades.
	BlockLetters BlockKind = "letters"
	// BlockShake shows Text once the phone is shaken hard enough:
	// Threshold, 1 to 100. Shown by the phone, like noise.
	BlockShake BlockKind = "shake"
	// BlockVote asks the team's players Text, with Options; each votes on
	// their own phone, and all see the tally. It never routes.
	BlockVote BlockKind = "vote"
	// BlockBulletin shows the organisers' latest message to the team.
	BlockBulletin BlockKind = "bulletin"
	// BlockDraw deals the team one of Options at random when it taps, kept
	// in Picks: a show condition may then ask which (cond.OpDrawn).
	BlockDraw BlockKind = "draw"
)

// QuizChoices are a quiz question's options, as the team picks them.
var QuizChoices = []string{"1", "X", "2"}

// BlockKinds lists every kind, for the shared test vectors.
var BlockKinds = []BlockKind{
	BlockText, BlockImage, BlockAudio, BlockVideo, BlockCompass, BlockMap, BlockNote, BlockChat, BlockCipher,
	BlockHotspots, BlockDivider, BlockRadar, BlockOverlay, BlockLevel, BlockNoise, BlockTorch, BlockCountdown,
	BlockStatus, BlockStandings, BlockTitle, BlockPoints, BlockQuiz, BlockTiebreak, BlockSplit, BlockBearing,
	BlockCall, BlockSuspects, BlockPhoto, BlockSpotDiff, BlockFlasher, BlockLetters, BlockShake, BlockVote,
	BlockBulletin, BlockDraw,
}

// FixedKinds are the kinds a step is given by default, and the only
// steps that may have them: FixedKinds[Puzzle] is title and points. See
// BlockTitle.
var FixedKinds = map[Type][]BlockKind{
	Puzzle: {BlockTitle, BlockPoints},
	Story:  {BlockTitle},
}

// Block is one piece of what a team sees on a puzzle, story or end. A
// step's blocks show in the author's order, above the puzzle's answer.
//
// Fields are per kind, like Answer's; each says which kinds read it. A
// new kind adds its fields here, a case in valid.checkBlocks, and a
// renderer and editor on the web side.
//
// ID is stable within the step, so a translation, an undo step and the
// editor's list all follow a block when it moves.
type Block struct {
	ID   string    `json:"id"`
	Kind BlockKind `json:"kind"`

	// Text is what text, note and countdown say, and what level, noise,
	// bearing and shake reveal; the question of a vote, the prompt of a
	// photo and a draw, a flasher's message and a letters block's
	// letters. Translated under BlockKey.
	Text string `json:"text,omitempty"`
	// Style is a note's costume (NoteStyles), a divider's (DividerStyles)
	// or a torch's (TorchStyles).
	Style string `json:"style,omitempty"`

	// Media is an adventure_media id (see Media): the picture of image,
	// hotspots, overlay and torch, the sound of audio and a call, the film
	// of video, the first picture of a spotdiff, and the avatar of a chat's
	// sender, which a chat may have. Other is a spotdiff's second picture.
	Media int64  `json:"media,omitempty"`
	Alt   string `json:"alt,omitempty"`
	Other int64  `json:"other,omitempty"`
	// Reverse lets the team play an audio backwards, and Spectrogram
	// draws its spectrum as it plays, where a picture may hide.
	Reverse     bool `json:"reverse,omitempty"`
	Spectrogram bool `json:"spectrogram,omitempty"`

	// Zone is the slug of the zone a compass points at, a map shows or a
	// radar senses.
	Zone string `json:"zone,omitempty"`
	// HideDistance leaves the distance off a compass: the team gets a
	// direction only.
	HideDistance bool `json:"hide_distance,omitempty"`
	// Zoom fixes a map's zoom level, as map tiles count it (MinZoom to
	// MaxZoom). Zero fits the map to the zone, and the team when near.
	Zoom int `json:"zoom,omitempty"`
	// HideTeam leaves the team's own position off a map.
	HideTeam bool `json:"hide_team,omitempty"`
	// Markers are points an author pins on a map, each with a label.
	Markers []Marker `json:"markers,omitempty"`

	// Lines are a chat's messages, in order.
	Lines []ChatLine `json:"lines,omitempty"`

	// Cipher names a cipher's sheet (Ciphers). Shift is a Caesar
	// cipher's; Key is a custom sheet, one "symbol = meaning" per line.
	Cipher string `json:"cipher,omitempty"`
	Shift  int    `json:"shift,omitempty"`
	Key    string `json:"key,omitempty"`
	// Wheel shows a Caesar cipher as a wheel the team turns, not a sheet.
	Wheel bool `json:"wheel,omitempty"`

	// Spots are a hotspots picture's regions, and a spotdiff's differences.
	Spots []Spot `json:"spots,omitempty"`

	// Seconds is how long a countdown runs, from the team reaching the
	// step or, with From "start", from the team starting.
	Seconds int    `json:"seconds,omitempty"`
	From    string `json:"from,omitempty"`

	// Pitch is the angle a level wants, in degrees above flat.
	Pitch int `json:"pitch,omitempty"`
	// Threshold is how loud a noise meter must get, or how hard a shake,
	// 1 to 100.
	Threshold int `json:"threshold,omitempty"`
	// Heading is where a bearing wants the phone pointed, in degrees
	// clockwise from north.
	Heading int `json:"heading,omitempty"`
	// Speed is a flasher's pace in words a minute; zero is DefaultSpeed.
	Speed int `json:"speed,omitempty"`

	// Counters and Items are what a status shows: those counters, and
	// whether the team's items.
	Counters []string `json:"counters,omitempty"`
	Items    bool     `json:"items,omitempty"`

	// Options are a quiz question's three answers, for 1, X and 2, and
	// Answer the right one's QuizChoices name. Worth is its points: 0 is
	// one point, and a photo's when approved. Options are also a vote's
	// choices and a draw's cards. Translated under
	// BlockFieldKey(…, "option:<i>").
	Options []string `json:"options,omitempty"`
	Answer  string   `json:"answer,omitempty"`
	Worth   int      `json:"worth,omitempty"`
	// Target is a tie-breaker's right number.
	Target *float64 `json:"target,omitempty"`

	// Parts are a split secret's pieces, Markdown, one to each player.
	// Translated under BlockFieldKey(…, "part:<i>").
	Parts []string `json:"parts,omitempty"`
	// Who is who a call is from. Translated under BlockFieldKey(…, "who").
	Who string `json:"who,omitempty"`
	// Cards are a suspect board's suspects.
	Cards []Card `json:"cards,omitempty"`

	// Show hides the block until its condition holds — near a zone, a
	// while after arriving, carrying something. Any kind may have one.
	Show *Show `json:"show,omitempty"`
	// Disabled switches the block off: kept for the author, never shown,
	// as if it were not there. Any kind may be, the title and points too.
	Disabled bool `json:"disabled,omitempty"`
}

// ChatLine is one message in a chat. Me puts it on the team's side.
type ChatLine struct {
	Who  string `json:"who,omitempty"`
	Text string `json:"text"`
	Me   bool   `json:"me,omitempty"`
}

// Marker is a pin on a map. Label is translated under
// BlockFieldKey(…, "marker:<i>").
type Marker struct {
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
	Label string  `json:"label,omitempty"`
}

// Card is one suspect on a board: a name, a few facts in Markdown, and a
// picture it may have. Name and Text are translated under
// BlockFieldKey(…, "card:<i>:name") and "card:<i>:text".
type Card struct {
	Name  string `json:"name"`
	Text  string `json:"text,omitempty"`
	Media int64  `json:"media,omitempty"`
	Alt   string `json:"alt,omitempty"`
}

// Spot is a hotspots region: its centre as fractions of the picture's
// width and height, and what tapping it says.
type Spot struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Text string  `json:"text"`
}

// Show is when a block appears. The server withholds a hidden block —
// its text, its file — so a curious team cannot read ahead. Tease shows
// that something is there, blurred, instead of nothing.
type Show struct {
	When  cond.Predicate `json:"when"`
	Tease bool           `json:"tease,omitempty"`
}

// The styles a note and a divider may take, the ciphers a sheet may
// show, and where a countdown may count from.
var (
	NoteStyles    = []string{"paper", "telegram", "sms", "newspaper", "sticky", "mirror"}
	DividerStyles = []string{"line", "space", "ornament"}
	// TorchStyles are how a torch's picture is found: by a torch in the
	// dark, a scratch card rubbed off, a UV lens over hidden ink, or a
	// red lens over red noise.
	TorchStyles    = []string{"", "scratch", "uv", "red"}
	Ciphers        = []string{"pigpen", "morse", "semaphore", "caesar", "custom"}
	CountdownFroms = []string{"", "step", "start"}
)

// DefaultSpeed is a flasher's pace when it sets none, in words a minute:
// slow enough to write down.
const DefaultSpeed = 8

// The zoom levels a map block may be fixed at: a region to a building.
const (
	MinZoom = 10
	MaxZoom = 19
)

// AsMedia is the block's file, for a kind that has one.
func (b Block) AsMedia() (Media, bool) {
	switch b.Kind {
	case BlockImage, BlockHotspots, BlockOverlay, BlockTorch:
		return Media{ID: b.Media, Kind: MediaImage, Alt: b.Alt}, true
	case BlockAudio, BlockCall:
		return Media{ID: b.Media, Kind: MediaAudio, Alt: b.Alt}, true
	case BlockSpotDiff:
		return Media{ID: b.Media, Kind: MediaImage, Alt: b.Alt}, true
	case BlockVideo:
		return Media{ID: b.Media, Kind: MediaVideo, Alt: b.Alt}, true
	case BlockChat:
		if b.Media != 0 {
			return Media{ID: b.Media, Kind: MediaImage, Alt: b.Alt}, true
		}
	}
	return Media{}, false
}

// Files are all the block's files: its own (AsMedia), then a spotdiff's
// second picture and a suspect board's pictures.
func (b Block) Files() []Media {
	var out []Media
	if m, ok := b.AsMedia(); ok {
		out = append(out, m)
	}
	switch b.Kind {
	case BlockSpotDiff:
		if b.Other != 0 {
			out = append(out, Media{ID: b.Other, Kind: MediaImage, Alt: b.Alt})
		}
	case BlockSuspects:
		for _, c := range b.Cards {
			if c.Media != 0 {
				out = append(out, Media{ID: c.Media, Kind: MediaImage, Alt: c.Alt})
			}
		}
	}
	return out
}

// MediaOf lists the blocks' files in order.
func MediaOf(bs []Block) []Media {
	var out []Media
	for _, b := range bs {
		out = append(out, b.Files()...)
	}
	return out
}

// BlockFieldKey is the Translations key of one of a block's texts other
// than Text: a chat line's "line:<i>" or "who:<i>", a spot's "spot:<i>".
func BlockFieldKey(slug, id, field string) string { return BlockKey(slug, id) + ":" + field }

// TextOf joins the texts of text and note blocks in order, as one
// Markdown document.
func TextOf(bs []Block) string {
	var parts []string
	for _, b := range bs {
		if (b.Kind == BlockText || b.Kind == BlockNote) && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// BlockKey is the Translations key of a text block.
func BlockKey(slug, id string) string { return NodeKey(slug, "block:"+id) }

// legacyTextID is the block a step's single text became when steps grew
// blocks. Its translations moved from the "text" key with it.
const legacyTextID = "text"

// LegacyBlocks turns a step's old text and media into blocks, in the
// order they were shown: the media above the text.
func LegacyBlocks(text string, media []Media) []Block {
	var out []Block
	for i, m := range media {
		kind := BlockImage
		if m.Kind == MediaAudio {
			kind = BlockAudio
		}
		out = append(out, Block{ID: "media-" + strconv.Itoa(i+1), Kind: kind, Media: m.ID, Alt: m.Alt})
	}
	if text != "" {
		out = append(out, Block{ID: legacyTextID, Kind: BlockText, Text: text})
	}
	return out
}

// Normalize moves the content of steps saved before blocks — a prompt
// or text, and media — into blocks, and their text's translations with
// it. Every decode of a stored or uploaded graph calls it, so releases
// frozen in the old shape still play and an agent may still send the
// old shape; the old fields are never written back.
//
// A custom UnmarshalJSON would do this without the callers, but it
// would decode its subtree without the envelope's DisallowUnknownFields.
func (g *Graph) Normalize() {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		switch {
		case n.PuzzleCfg != nil:
			c := n.PuzzleCfg
			c.Blocks = upgrade(c.Blocks, c.LegacyPrompt, c.LegacyMedia)
			c.LegacyPrompt, c.LegacyMedia = "", nil
		case n.StoryCfg != nil:
			c := n.StoryCfg
			c.Blocks = upgrade(c.Blocks, c.LegacyText, c.LegacyMedia)
			c.LegacyText, c.LegacyMedia = "", nil
		case n.EndCfg != nil:
			c := n.EndCfg
			c.Blocks = upgrade(c.Blocks, c.LegacyText, c.LegacyMedia)
			c.LegacyText, c.LegacyMedia = "", nil
		default:
			continue
		}
		addFixed(n)
		if !hasBlock(n.Blocks(), legacyTextID) {
			continue
		}
		from, to := NodeKey(n.Slug, "text"), BlockKey(n.Slug, legacyTextID)
		for _, tr := range g.Translations {
			if s, ok := tr[from]; ok {
				if _, taken := tr[to]; !taken {
					tr[to] = s
				}
				delete(tr, from)
			}
		}
	}
}

// addFixed gives a step its default blocks once, where missing: the
// title first, as the title always showed, and the points after it. A
// step marked Fixed has had them, so one missing was removed.
func addFixed(n *Node) {
	var bs *[]Block
	var done *bool
	switch {
	case n.PuzzleCfg != nil:
		bs, done = &n.PuzzleCfg.Blocks, &n.PuzzleCfg.Fixed
	case n.StoryCfg != nil:
		bs, done = &n.StoryCfg.Blocks, &n.StoryCfg.Fixed
	default:
		return
	}
	if *done {
		return
	}
	*done = true
	at := 0
	for _, k := range FixedKinds[n.Type] {
		if i := slices.IndexFunc(*bs, func(b Block) bool { return b.Kind == k }); i >= 0 {
			at = i + 1
			continue
		}
		id := string(k)
		for hasBlock(*bs, id) {
			id += "-"
		}
		*bs = slices.Insert(*bs, at, Block{ID: id, Kind: k})
		at++
	}
}

func upgrade(blocks []Block, text string, media []Media) []Block {
	if blocks != nil {
		return blocks
	}
	return LegacyBlocks(text, media)
}

func hasBlock(bs []Block, id string) bool {
	for _, b := range bs {
		if b.ID == id {
			return true
		}
	}
	return false
}

// Blocks returns what a puzzle, story or end shows, less the blocks
// switched off; nil for other steps.
func (n Node) Blocks() []Block {
	bs := n.AllBlocks()
	if !slices.ContainsFunc(bs, disabled) {
		return bs
	}
	return slices.DeleteFunc(slices.Clone(bs), disabled)
}

func disabled(b Block) bool { return b.Disabled }

// AllBlocks is every block of a puzzle, story or end, switched off or
// not: what the author edits, and what their ids and texts are checked on.
func (n Node) AllBlocks() []Block {
	switch {
	case n.PuzzleCfg != nil:
		return n.PuzzleCfg.Blocks
	case n.StoryCfg != nil:
		return n.StoryCfg.Blocks
	case n.EndCfg != nil:
		return n.EndCfg.Blocks
	}
	return nil
}
