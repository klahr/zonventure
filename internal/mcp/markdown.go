package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// renderEnvelopeMarkdown produces a human-readable view of an
// exported adventure envelope. The rendered string is emitted as a
// second content block alongside the raw JSON: clients (Claude Code,
// claude.ai's connector UI) render text blocks as markdown, so the
// intro and outro — themselves authored as markdown — show with proper
// headings, emphasis, and lists instead of as JSON-escaped strings, and
// the graph's steps show as an outline.
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
		} `json:"adventure"`
		Graph *struct {
			Graph struct {
				Nodes []struct {
					Slug  string `json:"slug"`
					Type  string `json:"type"`
					Title string `json:"title"`
				} `json:"nodes"`
			} `json:"graph"`
		} `json:"graph"`
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

	// The steps, in the graph's order. Their content is in the JSON
	// envelope; this is the outline.
	if env.Graph != nil && len(env.Graph.Graph.Nodes) > 0 {
		b.WriteString("## Steps\n\n")
		for _, n := range env.Graph.Graph.Nodes {
			fmt.Fprintf(&b, "- **%s** · *%s* (`%s`)\n", nonEmpty(n.Title, n.Slug), n.Type, n.Slug)
		}
		b.WriteString("\n")
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
