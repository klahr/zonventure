package graph

import (
	"strconv"
	"strings"
)

// BlockKind is what a content block shows.
type BlockKind string

const (
	BlockText  BlockKind = "text"
	BlockImage BlockKind = "image"
	BlockAudio BlockKind = "audio"
	// BlockCompass points the phone at a zone, with the distance to it.
	BlockCompass BlockKind = "compass"
	// BlockMap shows a zone on a map, with the team on it.
	BlockMap BlockKind = "map"
)

// Block is one piece of what a team sees on a puzzle, story or end: a
// text, a picture, a sound. A step's blocks show in the author's order,
// above the puzzle's answer.
//
// Fields are per kind, like Answer's: Text for text; Media and Alt for
// image and audio; Zone for compass and map, HideDistance for compass,
// and Zoom for map. A new kind adds its fields here, a case in
// valid.checkBlocks, and a renderer and editor on the web side.
//
// ID is stable within the step, so a translation, an undo step and the
// editor's list all follow a block when it moves.
type Block struct {
	ID   string    `json:"id"`
	Kind BlockKind `json:"kind"`

	Text string `json:"text,omitempty"`

	// Media is an adventure_media id; see Media.
	Media int64  `json:"media,omitempty"`
	Alt   string `json:"alt,omitempty"`

	// Zone is the slug of the zone a compass points at or a map shows.
	Zone string `json:"zone,omitempty"`
	// HideDistance leaves the distance off a compass: the team gets a
	// direction only.
	HideDistance bool `json:"hide_distance,omitempty"`
	// Zoom fixes a map's zoom level, as map tiles count it (MinZoom to
	// MaxZoom). Zero fits the map to the zone, and the team when near.
	Zoom int `json:"zoom,omitempty"`
}

// The zoom levels a map block may be fixed at: a region to a building.
const (
	MinZoom = 10
	MaxZoom = 19
)

// AsMedia is the block's picture or sound, for a block that has one.
func (b Block) AsMedia() (Media, bool) {
	switch b.Kind {
	case BlockImage:
		return Media{ID: b.Media, Kind: MediaImage, Alt: b.Alt}, true
	case BlockAudio:
		return Media{ID: b.Media, Kind: MediaAudio, Alt: b.Alt}, true
	}
	return Media{}, false
}

// MediaOf lists the blocks' pictures and sounds in order.
func MediaOf(bs []Block) []Media {
	var out []Media
	for _, b := range bs {
		if m, ok := b.AsMedia(); ok {
			out = append(out, m)
		}
	}
	return out
}

// TextOf joins the blocks' texts in order, as one Markdown document.
func TextOf(bs []Block) string {
	var parts []string
	for _, b := range bs {
		if b.Kind == BlockText && b.Text != "" {
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

// Blocks returns what a puzzle, story or end shows; nil for other steps.
func (n Node) Blocks() []Block {
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
