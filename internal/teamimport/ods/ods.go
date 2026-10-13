// Package ods is the OpenDocument Spreadsheet importer for the
// teamimport registry. ODS files are zip archives whose
// content.xml carries the cell data — same conceptual model as
// xlsx but with a simpler XML schema, no shared-strings
// indirection, no styles to dereference. Stdlib only: archive/zip
// + encoding/xml.
//
// # Format
//
// OpenDocument Format for Office Applications, version 1.3
// (OASIS standard, ratified as ISO/IEC 26300):
//
//	https://docs.oasis-open.org/office/OpenDocument/v1.3/os/OpenDocument-v1.3-os.html
//
// LibreOffice Calc and OpenOffice Calc are the reference writers.
// Apple Numbers and Excel both export to ODS as a non-default
// option. The spreadsheet schema we read here is the
// `urn:oasis:names:tc:opendocument:xmlns:table:1.0` namespace:
//
//	https://docs.oasis-open.org/office/OpenDocument/v1.3/os/part3-schema/OpenDocument-v1.3-os-part3-schema.html
//
// # Wire shape
//
// Only the first table (sheet) is read; other tables are
// ignored. Within that table the row shape mirrors the CSV
// importer: first column = team name, every column after it
// becomes a player slot. Empty rows skipped, header-row keyword
// detected and dropped.
package ods

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/klahr/zonventure/internal/teamimport"
)

// New returns the ODS importer.
func New() teamimport.Importer { return &impl{} }

type impl struct{}

func (impl) Name() string         { return "ODS" }
func (impl) Extensions() []string { return []string{".ods"} }
func (impl) MIMETypes() []string {
	return []string{
		"application/vnd.oasis.opendocument.spreadsheet",
		// Some browsers / proxies write the .fods (flat ODS) MIME but
		// any zip-wrapped ODS lands here too — we accept the same
		// type and the unzip step will fail cleanly on a flat-XML
		// payload.
		"application/x-vnd.oasis.opendocument.spreadsheet",
	}
}

// odsRow is the deserialized shape of one <table:table-row> from
// content.xml. table:number-rows-repeated lets a writer collapse N
// identical empty rows into a single element with a count
// attribute; we read the count so a sheet with a sparse first
// table doesn't blow memory expanding rows we'd skip anyway.
//
// We match by local name only and ignore the OASIS namespace
// prefix — encoding/xml does the right thing when the tag has no
// namespace URL, and writing the full
// urn:oasis:names:tc:opendocument:xmlns:table:1.0 in every tag
// just clutters the struct.
type odsRow struct {
	RepeatedCount int       `xml:"number-rows-repeated,attr"`
	Cells         []odsCell `xml:"table-cell"`
}

// odsCell carries the cell value via either the value-type
// attribute (numbers, dates) or the contained <text:p> children
// (string cells). table:number-columns-repeated handles the same
// run-length collapse as on rows.
type odsCell struct {
	RepeatedCount int      `xml:"number-columns-repeated,attr"`
	ValueType     string   `xml:"value-type,attr"`
	StringValue   string   `xml:"string-value,attr"`
	NumberValue   string   `xml:"value,attr"`
	Texts         []string `xml:"p"`
}

// maxDecompressedBytes caps the decompressed size of content.xml
// so a zip-bomb (a 1 MB archive that expands to 50 GB) can't OOM
// the import worker. Real-world ODS rosters are kilobytes; 50 MB
// is comfortable headroom for hand-typed sheets and well below any
// plausible memory exhaustion threshold.
const maxDecompressedBytes = 50 << 20 // 50 MiB

// maxRowsTotal / maxColsPerRow cap the run-length amplification on
// the table:number-rows-repeated and table:number-columns-repeated
// attributes. The XML parser otherwise honours the integer verbatim
// — a 30-byte attribute like number-rows-repeated="2147483647" runs
// a 2-billion-iteration append loop, OOMing the worker long before
// the 50 MiB content.xml cap kicks in (the attribute itself is
// vanishingly small, the explosion happens in memory).
//
// Real-world rosters are at most a few hundred rows × dozens of
// columns; these caps are two orders of magnitude beyond any
// legitimate input and explicit enough that an operator can spot the
// rejection in logs.
const (
	maxRowsTotal  = 100_000
	maxColsPerRow = 10_000
)

// errRepeatTooLarge is the single error every amplification site
// returns. Wrapped at the parseContent boundary so the operator sees
// a coherent reason instead of a torrent of EOF-style noise.
var errRepeatTooLarge = errors.New("teamimport/ods: row/column repeat count exceeds limit (refusing amplification)")

func (i impl) Parse(r io.Reader) ([]teamimport.Team, error) {
	rows, err := i.ReadRows(r)
	if err != nil {
		return nil, err
	}
	return teamimport.Rows(rows)
}

// ReadRows reads the first sheet's rows.
func (impl) ReadRows(r io.Reader) ([][]string, error) {
	// archive/zip needs a ReaderAt + size, so buffer the whole
	// upload first. ODS rosters are tiny — a 5 MiB cap upstream in
	// the handler is the size budget we live within here.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var content *zip.File
	for _, f := range zr.File {
		if f.Name == "content.xml" {
			content = f
			break
		}
	}
	if content == nil {
		return nil, errors.New("teamimport/ods: no content.xml in archive")
	}
	// Pre-flight bomb check: a content.xml whose declared
	// uncompressed size already exceeds the cap is rejected before
	// we open the decompression stream. The size in the central
	// directory IS attacker-controlled (zip metadata can lie about
	// the contents), but a writer that announces a 60 GB decompressed
	// size is unambiguously malicious; a writer that announces a
	// reasonable size and then streams more bytes hits the
	// LimitReader gate below.
	if content.UncompressedSize64 > maxDecompressedBytes {
		return nil, errors.New("teamimport/ods: content.xml decompressed size exceeds limit (refusing zip-bomb)")
	}
	rc, err := content.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	// LimitReader+1 lets us distinguish "exactly at the limit" from
	// "over the limit" — io.Copy(io.Discard, lr) reads until EOF or
	// the cap. The +1 is so a stream that fills up exactly to the
	// cap can be read but anything past it is rejected.
	limited := io.LimitReader(rc, maxDecompressedBytes+1)
	return parseContent(&limitTrip{r: limited, limit: maxDecompressedBytes})
}

// limitTrip wraps an io.Reader and returns a "decompressed size
// exceeds limit" error if the underlying reader yields more than
// `limit` bytes total. Differs from a plain io.LimitReader (which
// just returns EOF at the cap) — we want the parser to see an
// explicit error, not a truncated input it might happily accept.
type limitTrip struct {
	r        io.Reader
	limit    int64
	consumed int64
}

func (lt *limitTrip) Read(p []byte) (int, error) {
	n, err := lt.r.Read(p)
	lt.consumed += int64(n)
	if lt.consumed > lt.limit {
		return n, errors.New("teamimport/ods: decompressed size exceeded limit (refusing zip-bomb)")
	}
	return n, err
}

// parseContent walks content.xml as a stream — easier on memory
// than full Unmarshal, and lets us bail at end-of-first-table
// without parsing the rest of the document.
func parseContent(r io.Reader) ([][]string, error) {
	dec := xml.NewDecoder(r)
	var (
		rows         [][]string
		insideTable  bool
		seenAnyTable bool
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "table": // table:table — one sheet
			if seenAnyTable {
				// Skip subsequent sheets without parsing them.
				_ = dec.Skip()
				continue
			}
			seenAnyTable = true
			insideTable = true
		case "table-row":
			if !insideTable {
				_ = dec.Skip()
				continue
			}
			var row odsRow
			if err := dec.DecodeElement(&row, &start); err != nil {
				return nil, err
			}
			cells, err := flattenCells(row.Cells)
			if err != nil {
				return nil, err
			}
			repeats := row.RepeatedCount
			if repeats <= 0 {
				repeats = 1
			}
			if repeats > maxRowsTotal {
				return nil, errRepeatTooLarge
			}
			// Sparse-row collapse: a writer might encode "rows 5–999
			// are empty" as a single repeated row with no cells.
			// Replaying that 995 times would balloon the parsed
			// rows slice for nothing — skip empty repeats.
			if isAllEmpty(cells) {
				continue
			}
			if len(rows)+repeats > maxRowsTotal {
				return nil, errRepeatTooLarge
			}
			for i := 0; i < repeats; i++ {
				rows = append(rows, cells)
			}
		}
	}
	return rows, nil
}

// flattenCells expands repeated-cell run lengths into a flat slice
// of strings. Non-string cells (numbers, dates) fall back to the
// office:value attribute so a numeric ID column parses as a name
// rather than disappearing. Repeats are capped at maxColsPerRow so
// an attacker-controlled number-columns-repeated attribute can't
// drive the slice past sane bounds.
func flattenCells(cells []odsCell) ([]string, error) {
	var out []string
	for _, c := range cells {
		text := cellText(c)
		repeats := c.RepeatedCount
		if repeats <= 0 {
			repeats = 1
		}
		if repeats > maxColsPerRow {
			return nil, errRepeatTooLarge
		}
		if len(out)+repeats > maxColsPerRow {
			return nil, errRepeatTooLarge
		}
		// A run of empty repeated cells at the row-end is a writer
		// convention for "this row has no more data" — preserving
		// it would just produce trailing empty member names. Trim
		// at row-shape time, but leave interior repeats so a row
		// with [name, "", "", member3] keeps its column alignment.
		for i := 0; i < repeats; i++ {
			out = append(out, text)
		}
	}
	return out, nil
}

func cellText(c odsCell) string {
	if c.StringValue != "" {
		return c.StringValue
	}
	if len(c.Texts) > 0 {
		return strings.TrimSpace(strings.Join(c.Texts, " "))
	}
	if c.NumberValue != "" {
		// Strip the trailing ".0" you get when a writer encodes a
		// whole number as "42.0" — the trimmed form reads as an ID.
		if f, err := strconv.ParseFloat(c.NumberValue, 64); err == nil {
			return strconv.FormatFloat(f, 'f', -1, 64)
		}
		return c.NumberValue
	}
	return ""
}

func isAllEmpty(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}
