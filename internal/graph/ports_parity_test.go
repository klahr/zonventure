package graph

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// The same vectors @zonventure/graph's TypeScript tests read
// (web/packages/graph/testdata/ports.json). The editor draws one handle
// per port and the validator reports any unconnected port, so if
// Node.Ports() and the editor's portsOf ever disagree, an author sees an
// "unconnected port" error for a handle that isn't on screen — or a
// handle the server does not believe exists. Both sides run this file.
func TestPortsMatchSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("../../web/packages/graph/testdata/ports.json")
	if err != nil {
		t.Fatalf("reading shared vectors: %v", err)
	}
	var vectors struct {
		Cases []struct {
			Node  Node     `json:"node"`
			Ports []string `json:"ports"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("decoding shared vectors: %v", err)
	}
	if len(vectors.Cases) == 0 {
		t.Fatal("no vectors — the file moved or is empty, and this test would pass vacuously")
	}
	for _, c := range vectors.Cases {
		got := c.Node.Ports()
		if got == nil {
			got = []string{}
		}
		if !slices.Equal(got, c.Ports) {
			t.Errorf("%s %q: Ports() = %v, shared vector says %v", c.Node.Type, c.Node.Slug, got, c.Ports)
		}
	}
}
