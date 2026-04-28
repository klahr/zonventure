// Package roster turns external roster representations (CSV uploads,
// JSON request bodies, vendor XML exports) into a uniform list of
// teams the admin handlers can ingest. Importer is the seam for new
// formats: drop a file next to csv.go, register it in Pick, and the
// bulk-create handler picks it up by Content-Type without touching
// the admin package.
//
// Scope on purpose:
//
//   - Format → []TeamInput. Whitespace trimming, BOM stripping, empty
//     row filtering happen here.
//   - Per-row business validation (duplicate names, member uniqueness,
//     event-cap checks) stays in the caller, which already has to
//     surface partial-success results to the UI.
package roster

import (
	"errors"
	"fmt"
	"io"
	"mime"
)

// TeamInput is one team's worth of data extracted from a roster
// source. Field shapes mirror the admin createTeamRequest so the
// handler can map straight through.
type TeamInput struct {
	Name        string
	MemberNames []string
}

// Importer parses a roster source into a flat list of TeamInputs.
// Implementations should be permissive about whitespace and trailing
// blank rows but strict about structural errors (truncated CSV
// quoting, bad JSON shape) — those come back as a single error and
// abort the whole import.
type Importer interface {
	Parse(r io.Reader) ([]TeamInput, error)
}

// ErrUnsupported is returned by Pick when the Content-Type doesn't
// match a registered format. Wrapped with the offending media type so
// the error message is useful.
var ErrUnsupported = errors.New("roster: unsupported content type")

// Pick selects an Importer for a request Content-Type. An empty or
// "application/json" media type maps to JSON (the canonical UI
// shape); "text/csv" maps to CSV. Adding a format is a one-line edit
// here plus a new file in this package.
func Pick(contentType string) (Importer, error) {
	mt, _, _ := mime.ParseMediaType(contentType)
	switch mt {
	case "", "application/json":
		return JSONImporter{}, nil
	case "text/csv":
		return CSVImporter{}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupported, mt)
	}
}
