package graph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// FormatV2 identifies a graph envelope. v1 ("zonventure-adventure-v1")
// is the lane-and-page export; it stays importable and converts through
// internal/graph/convert.
const FormatV2 = "zonventure-adventure-v2"

// Envelope is the portable form of an adventure's structure: what the
// editor saves, what export writes, what the MCP tools send.
//
// Nodes are addressed by slug and never by database id, which is what
// lets a graph move between instances unchanged.
type Envelope struct {
	Format string `json:"format"`
	Graph  Graph  `json:"graph"`
}

// ErrFormat is returned for an envelope that is not v2.
var ErrFormat = errors.New("not a " + FormatV2 + " envelope")

// maxEnvelopeBytes bounds what ParseEnvelope accepts. A real adventure
// is tens of kilobytes; the cap exists so a malicious or mistaken upload
// cannot make the server allocate without limit.
const maxEnvelopeBytes = 8 << 20

// ParseEnvelope decodes a v2 envelope strictly.
//
// Unknown fields are rejected rather than ignored. An envelope written
// by a newer build carries fields this build does not understand, and
// silently dropping them on a save would destroy the author's work.
// Refusing is the only honest answer.
//
// This checks the format only. Structural validity is valid.Check's job,
// kept separate so a caller can load a broken graph to show the author
// what is wrong with it.
func ParseEnvelope(b []byte) (*Envelope, error) {
	if len(b) > maxEnvelopeBytes {
		return nil, fmt.Errorf("envelope is %d bytes; the limit is %d", len(b), maxEnvelopeBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()

	var env Envelope
	if err := dec.Decode(&env); err != nil {
		return nil, fmt.Errorf("decoding envelope: %w", err)
	}
	if dec.More() {
		return nil, errors.New("decoding envelope: trailing data after the document")
	}
	if env.Format != FormatV2 {
		return nil, fmt.Errorf("%w (format is %q)", ErrFormat, env.Format)
	}
	env.Graph.Normalize()
	return &env, nil
}

// MarshalEnvelope encodes a graph as a v2 envelope.
func MarshalEnvelope(g *Graph) ([]byte, error) {
	return json.Marshal(Envelope{Format: FormatV2, Graph: *g})
}
