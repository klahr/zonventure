package admin

import (
	"bytes"
	"testing"

	"github.com/skip2/go-qrcode"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
)

func TestAcceptsExactly(t *testing.T) {
	t.Parallel()
	g := &graph.Graph{Nodes: []graph.Node{{Slug: "scan", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
		Outputs: []graph.Output{{Port: "ok", Matchers: []match.Matcher{{Kind: match.Exact, Value: "LANTERN"}, {Kind: match.Contains, Value: "oak"}}}},
	}}}}
	for _, c := range []struct {
		node, value string
		want        bool
	}{
		{"scan", "LANTERN", true},
		{"scan", "oak", false},
		{"scan", "", false},
		{"other", "LANTERN", false},
	} {
		if got := acceptsExactly(g, c.node, c.value); got != c.want {
			t.Errorf("acceptsExactly(%q, %q) = %v, want %v", c.node, c.value, got, c.want)
		}
	}
}

func TestAnswerQRSheet(t *testing.T) {
	t.Parallel()
	png, err := qrcode.Encode("LANTERN", qrcode.Medium, 600)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := writeAnswerQRSheet(&buf, "Västerås stadsvandring", "Torget", png); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
		t.Fatalf("not a PDF: %q", buf.Bytes()[:min(16, buf.Len())])
	}
}
