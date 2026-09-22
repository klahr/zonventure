package graph

import (
	"errors"
	"strings"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	g := &Graph{
		Nodes: []Node{{Slug: "start", Type: Start}, {Slug: "end", Type: End}},
		Edges: []Edge{{From: "start", FromPort: PortOut, To: "end"}},
	}
	b, err := MarshalEnvelope(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	env, err := ParseEnvelope(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(env.Graph.Nodes) != 2 || env.Graph.Edges[0].To != "end" {
		t.Errorf("round-trip lost structure: %+v", env.Graph)
	}
}

func TestEnvelopeRejectsWrongFormat(t *testing.T) {
	for _, body := range []string{
		`{"format":"zonventure-adventure-v1","graph":{"nodes":[],"edges":[]}}`,
		`{"graph":{"nodes":[],"edges":[]}}`,
	} {
		if _, err := ParseEnvelope([]byte(body)); !errors.Is(err, ErrFormat) {
			t.Errorf("ParseEnvelope(%s) = %v, want ErrFormat", body, err)
		}
	}
}

// A field this build does not understand came from a newer build.
// Dropping it silently on save would destroy the author's work.
func TestEnvelopeRejectsUnknownFields(t *testing.T) {
	body := `{"format":"` + FormatV2 + `","graph":{"nodes":[{"slug":"s","type":"start","sparkle":true}],"edges":[]}}`
	if _, err := ParseEnvelope([]byte(body)); err == nil || !strings.Contains(err.Error(), "sparkle") {
		t.Errorf("expected an unknown-field error naming the field, got %v", err)
	}
}

func TestEnvelopeRejectsTrailingDataAndGarbage(t *testing.T) {
	good := `{"format":"` + FormatV2 + `","graph":{"nodes":[],"edges":[]}}`
	if _, err := ParseEnvelope([]byte(good + good)); err == nil {
		t.Error("two concatenated documents should be rejected")
	}
	if _, err := ParseEnvelope([]byte("not json")); err == nil {
		t.Error("garbage should be rejected")
	}
}

func TestEnvelopeRejectsOversize(t *testing.T) {
	big := make([]byte, maxEnvelopeBytes+1)
	if _, err := ParseEnvelope(big); err == nil {
		t.Error("an oversized envelope should be rejected before decoding")
	}
}
