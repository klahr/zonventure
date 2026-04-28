package roster

import (
	"encoding/json"
	"io"
)

// JSONImporter consumes the canonical wire shape:
//
//	{"teams":[{"name":"…","member_names":["…",…]}, …]}
//
// It's the format the in-app paste-roster textarea posts after
// client-side parsing. Strict about unknown fields so a typo in the
// UI surfaces immediately rather than silently dropping members.
type JSONImporter struct{}

type jsonRoster struct {
	Teams []jsonTeam `json:"teams"`
}

type jsonTeam struct {
	Name        string   `json:"name"`
	MemberNames []string `json:"member_names"`
}

// Parse decodes one jsonRoster envelope. The body-size cap is the
// caller's job (handler wraps with http.MaxBytesReader); here we just
// trust the reader.
func (JSONImporter) Parse(r io.Reader) ([]TeamInput, error) {
	var body jsonRoster
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return nil, err
	}
	out := make([]TeamInput, 0, len(body.Teams))
	for _, t := range body.Teams {
		out = append(out, TeamInput{Name: t.Name, MemberNames: t.MemberNames})
	}
	return out, nil
}
