package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// mediaAsset mirrors the export's image / audio asset entry shape.
// Only the fields the markdown view actually shows — filename plus
// any author-supplied descriptive text — are decoded; the binary
// bytes never travel through MCP at all (ExportAdventureEnvelopeJSON
// strips them).
type mediaAsset struct {
	File        string `json:"file"`
	AltText     string `json:"alt_text"`
	Description string `json:"description"`
}

// renderEnvelopeMarkdown produces a human-readable view of an
// exported adventure envelope. The rendered string is emitted as a
// second content block alongside the raw JSON: clients (Claude Code,
// claude.ai's connector UI) render text blocks as markdown, so puzzle
// prompts — which are themselves authored as markdown — show with
// proper headings, emphasis, and lists instead of as JSON-escaped
// strings.
//
// We unmarshal into a small mirror of admin.exportAdventureDTO rather
// than importing the private type. The rendering only needs the
// human-facing fields, so keeping this struct minimal is fine; the
// JSON bytes themselves remain authoritative for round-tripping.
func renderEnvelopeMarkdown(envelopeJSON []byte) string {
	var env struct {
		Adventure struct {
			Slug        string   `json:"slug"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Author      string   `json:"author"`
			IntroText   string   `json:"intro_text"`
			OutroText   string   `json:"outro_text"`
			Language    string   `json:"language"`
			StartLat    *float64 `json:"start_lat"`
			StartLng    *float64 `json:"start_lng"`
			StartLabel  string   `json:"start_label"`
			Lanes       []struct {
				Idx  int    `json:"idx"`
				Mode string `json:"mode"`
				Name string `json:"name"`
			} `json:"lanes"`
			Pages []struct {
				Idx     int    `json:"idx"`
				LaneIdx int    `json:"lane_idx"`
				Title   string `json:"title"`
				Prompt  string `json:"prompt"`
				Accept  []struct {
					Text  string `json:"text"`
					Regex bool   `json:"regex"`
				} `json:"accept"`
				Lat      float64  `json:"lat"`
				Lng      float64  `json:"lng"`
				RadiusM  *float64 `json:"radius_m"`
				Disabled bool     `json:"disabled"`
				Notes    string   `json:"notes"`
				Hints    []struct {
					Idx          int    `json:"idx"`
					Text         string `json:"text"`
					DelaySeconds int    `json:"delay_seconds"`
					Images       []mediaAsset `json:"images"`
					Audio        []mediaAsset `json:"audio"`
				} `json:"hints"`
				Images []mediaAsset `json:"images"`
				Audio  []mediaAsset `json:"audio"`
			} `json:"pages"`
		} `json:"adventure"`
	}
	if err := json.Unmarshal(envelopeJSON, &env); err != nil {
		// Fall back to raw JSON in a code fence — better than
		// returning nothing, and makes the parse failure visible.
		return "```json\n" + string(envelopeJSON) + "\n```"
	}
	a := env.Adventure

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", nonEmpty(a.Title, a.Slug))
	if a.Author != "" {
		fmt.Fprintf(&b, "*by %s*", a.Author)
		if a.Language != "" {
			fmt.Fprintf(&b, " · %s", a.Language)
		}
		b.WriteString("\n\n")
	} else if a.Language != "" {
		fmt.Fprintf(&b, "*Language: %s*\n\n", a.Language)
	}
	fmt.Fprintf(&b, "**Slug:** `%s`\n\n", a.Slug)
	if a.Description != "" {
		b.WriteString(a.Description)
		b.WriteString("\n\n")
	}
	if a.StartLat != nil && a.StartLng != nil {
		fmt.Fprintf(&b, "**Start:** %.5f, %.5f", *a.StartLat, *a.StartLng)
		if a.StartLabel != "" {
			fmt.Fprintf(&b, " (%s)", a.StartLabel)
		}
		b.WriteString("\n\n")
	}
	if a.IntroText != "" {
		b.WriteString("## Intro\n\n")
		b.WriteString(a.IntroText)
		b.WriteString("\n\n")
	}

	// Group pages by lane idx so each lane's puzzles render together
	// in puzzle-order. The export shape already separates lanes and
	// pages; we re-stitch them for readability.
	pagesByLane := map[int][]int{}
	for i, p := range a.Pages {
		pagesByLane[p.LaneIdx] = append(pagesByLane[p.LaneIdx], i)
	}

	for _, lane := range a.Lanes {
		laneName := lane.Name
		if laneName == "" {
			laneName = fmt.Sprintf("Lane %d", lane.Idx+1)
		}
		fmt.Fprintf(&b, "## %s · *%s*\n\n", laneName, nonEmpty(lane.Mode, "linear"))
		idxs := pagesByLane[lane.Idx]
		if len(idxs) == 0 {
			b.WriteString("*(no puzzles)*\n\n")
			continue
		}
		for n, pi := range idxs {
			p := a.Pages[pi]
			heading := nonEmpty(p.Title, fmt.Sprintf("Puzzle %d", n+1))
			fmt.Fprintf(&b, "### %d. %s", n+1, heading)
			if p.Disabled {
				b.WriteString(" *(disabled)*")
			}
			b.WriteString("\n\n")
			if p.Prompt != "" {
				b.WriteString(p.Prompt)
				b.WriteString("\n\n")
			}
			if len(p.Accept) > 0 {
				vals := make([]string, len(p.Accept))
				for i, a := range p.Accept {
					if a.Regex {
						vals[i] = "`" + a.Text + "` (regex)"
					} else {
						vals[i] = "`" + a.Text + "`"
					}
				}
				fmt.Fprintf(&b, "**Accept:** %s\n\n", strings.Join(vals, ", "))
			}
			loc := fmt.Sprintf("%.5f, %.5f", p.Lat, p.Lng)
			if p.RadiusM != nil {
				fmt.Fprintf(&b, "**Location:** %s (radius %.0fm)\n\n", loc, *p.RadiusM)
			} else {
				fmt.Fprintf(&b, "**Location:** %s\n\n", loc)
			}
			renderMedia(&b, "Images", p.Images)
			renderMedia(&b, "Audio", p.Audio)
			if len(p.Hints) > 0 {
				b.WriteString("**Hints:**\n\n")
				for _, h := range p.Hints {
					if h.DelaySeconds > 0 {
						fmt.Fprintf(&b, "- *(after %ds)* %s", h.DelaySeconds, h.Text)
					} else {
						fmt.Fprintf(&b, "- %s", h.Text)
					}
					// Hint-attached media stays hidden until the
					// player reveals the hint, but flagging it on
					// the export lets the model know the puzzle has
					// extra material if needed.
					if n := len(h.Images) + len(h.Audio); n > 0 {
						fmt.Fprintf(&b, " *(+%d media)*", n)
					}
					b.WriteString("\n")
				}
				b.WriteString("\n")
			}
			if p.Notes != "" {
				fmt.Fprintf(&b, "> *Author notes:* %s\n\n", p.Notes)
			}
		}
	}

	if a.OutroText != "" {
		b.WriteString("## Outro\n\n")
		b.WriteString(a.OutroText)
		b.WriteString("\n\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// nonEmpty returns s if non-empty, otherwise fallback. Saves the
// per-call ternary boilerplate when picking a heading from an
// optional field.
func nonEmpty(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

// renderMedia writes an "**Images:**" or "**Audio:**" block listing
// each asset's filename plus alt text / description when the author
// supplied one. The binary bytes themselves never leave the server,
// so this is the entire signal the model gets that supplementary
// media exists. No-op when the slice is empty.
func renderMedia(b *strings.Builder, label string, assets []mediaAsset) {
	if len(assets) == 0 {
		return
	}
	fmt.Fprintf(b, "**%s:**\n\n", label)
	for _, a := range assets {
		desc := nonEmpty(a.AltText, a.Description)
		if desc != "" {
			fmt.Fprintf(b, "- `%s` — %s\n", a.File, desc)
		} else {
			fmt.Fprintf(b, "- `%s`\n", a.File)
		}
	}
	b.WriteString("\n")
}
