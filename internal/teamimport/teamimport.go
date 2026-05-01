// Package teamimport defines a small, format-agnostic surface for
// turning an uploaded file into a list of (team-name, members)
// records — exactly the shape the existing /teams/bulk endpoint
// already accepts. Each file format is implemented by a separate
// sub-package so an operator can compose only the formats they
// actually want to expose; no init-time side effects, no implicit
// global registry.
//
// Wiring shape at the call site:
//
//	reg := teamimport.New()
//	reg.Register(csv.New())
//	reg.Register(tsv.New())
//	reg.Register(jsonimp.New())
//	reg.Register(text.New())
//	// optional, requires excelize:
//	// reg.Register(xlsx.New())
//
// The HTTP handler then dispatches a multipart upload through
// reg.ForFilename(hdr.Filename) (with a MIME-type fallback) and
// returns the parsed Team list as a preview. Actual team creation
// stays on /teams/bulk so we don't fork the validation / dedup /
// audit path.
package teamimport

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
)

// Team is a single parsed entry from an import. MemberNames is in
// authoring order — the bulk endpoint mints one slot per name in
// that order, so the importer's job is to preserve whatever the
// source file expressed without re-ordering.
type Team struct {
	Name        string   `json:"name"`
	MemberNames []string `json:"member_names"`
}

// SanitizeCellValue defangs a single string against CSV-injection.
// A cell whose first non-whitespace character is one of `=`, `+`,
// `-`, `@`, `\t`, `\r`, `|` is treated as a formula by Excel /
// Sheets / Numbers when re-exported via the clipboard or
// downloaded as CSV — and our team names round-trip through
// arranger CSV exports / leaderboard copy-paste flows. Prefix
// such cells with a single quote so the resulting SQL row
// preserves the original characters but Excel renders them as
// text instead of evaluating.
//
// Applies once at import time. Empty / safe values pass through
// unchanged so a normal "Alpha, alice, bob" row stays exactly as
// the operator typed it. The single-quote prefix is the OWASP-
// recommended mitigation: it doesn't change the visible content
// in spreadsheet clients but disables formula evaluation.
func SanitizeCellValue(s string) string {
	trimmed := strings.TrimLeft(s, " \t")
	if trimmed == "" {
		return s
	}
	switch trimmed[0] {
	case '=', '+', '-', '@', '\t', '\r', '|':
		return "'" + s
	}
	return s
}

// Importer is the contract every format implements. It's small on
// purpose: callers only need to identify (Name + Extensions +
// MIMETypes) and parse. There is no "validate" or "preview" step —
// Parse is allowed to return a partial list along with an error
// when some rows are bad and others are usable, but the simple
// happy path is to return (teams, nil) and let the bulk endpoint
// do per-team validation when the user confirms.
type Importer interface {
	// Name is the human-readable label shown in the upload UI.
	// Translatable on the front-end via the existing t() pipeline.
	Name() string
	// Extensions are the filename extensions this importer claims
	// (each leading-dot, lowercase, e.g. ".csv"). The first entry
	// is the canonical / preferred form for documentation.
	Extensions() []string
	// MIMETypes are the Content-Type values this importer claims.
	// Used as a fallback when the filename has no extension or
	// the browser sent a generic application/octet-stream.
	MIMETypes() []string
	// Parse reads from r and returns parsed teams. The reader is
	// owned by the caller — implementations should not close it.
	Parse(r io.Reader) ([]Team, error)
}

// Registry resolves a filename or MIME type to an Importer. There
// is no implicit global instance: every server creates its own
// Registry and registers exactly the importers it wants — this is
// the "plugin-like, independent" property. Registration is
// expected to happen at startup; the Registry is goroutine-safe
// for concurrent reads but Register is not — call it before
// handing the registry to HTTP handlers.
type Registry struct {
	byExt  map[string]Importer
	byMIME map[string]Importer
	all    []Importer
}

// New returns an empty Registry. Register importers before use.
func New() *Registry {
	return &Registry{
		byExt:  map[string]Importer{},
		byMIME: map[string]Importer{},
	}
}

// ErrUnknownFormat is returned by ForFilename / ForMIME when no
// registered importer claims the given identifier. The handler
// translates this to a 415 Unsupported Media Type so the UI can
// surface "we don't know how to read .foo files" cleanly.
var ErrUnknownFormat = errors.New("teamimport: no importer registered for this format")

// Register adds an importer. The same importer can claim multiple
// extensions and MIME types — the dispatch tables keep one entry
// per claim. A second importer claiming an already-registered
// extension overwrites the first (last-registered wins) so a
// caller can deliberately substitute, e.g. swap the stdlib CSV
// importer for a stricter one.
func (r *Registry) Register(imp Importer) {
	r.all = append(r.all, imp)
	for _, ext := range imp.Extensions() {
		r.byExt[strings.ToLower(ext)] = imp
	}
	for _, mt := range imp.MIMETypes() {
		r.byMIME[strings.ToLower(mt)] = imp
	}
}

// All returns every registered importer in registration order. The
// front-end uses this to render the "supported formats" hint
// alongside the file picker.
func (r *Registry) All() []Importer {
	out := make([]Importer, len(r.all))
	copy(out, r.all)
	return out
}

// ForFilename resolves an importer by filename extension. Empty
// filenames and unknown extensions return ErrUnknownFormat — the
// caller should fall back to ForMIME before giving up.
func (r *Registry) ForFilename(name string) (Importer, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return nil, ErrUnknownFormat
	}
	if imp, ok := r.byExt[ext]; ok {
		return imp, nil
	}
	return nil, ErrUnknownFormat
}

// ForMIME resolves an importer by Content-Type. The lookup is
// case-insensitive on the type (which is what RFC 2045 specifies)
// and ignores any ;charset= tail — text/csv;charset=utf-8 still
// matches a registered text/csv. Returns ErrUnknownFormat on miss.
func (r *Registry) ForMIME(mime string) (Importer, error) {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	if mime == "" {
		return nil, ErrUnknownFormat
	}
	if imp, ok := r.byMIME[mime]; ok {
		return imp, nil
	}
	return nil, ErrUnknownFormat
}

// Resolve combines ForFilename and ForMIME — extension first
// (browsers reliably send Content-Type but it's frequently the
// generic application/octet-stream for non-Office files; the
// filename extension is more specific). Falls back to MIME on
// extension miss. Returns ErrUnknownFormat only when neither
// matches.
func (r *Registry) Resolve(filename, mime string) (Importer, error) {
	if imp, err := r.ForFilename(filename); err == nil {
		return imp, nil
	}
	return r.ForMIME(mime)
}
