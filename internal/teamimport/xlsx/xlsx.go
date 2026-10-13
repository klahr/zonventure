// Package xlsx is the .xlsx importer for the teamimport registry.
// Reads the first sheet of the workbook with the same row shape
// every other importer uses: first column = team name, every
// column after it = a player slot.
//
// # Format
//
// Office Open XML Spreadsheet (xlsx), standardised as
// ECMA-376 and republished as ISO/IEC 29500. The 5th-edition
// ECMA-376 documents are the canonical reference:
//
//	https://ecma-international.org/publications-and-standards/standards/ecma-376/
//	https://www.iso.org/standard/71691.html
//
// Excel (Windows / Mac), Google Sheets, Apple Numbers, and
// LibreOffice Calc all default to writing this format. The
// physical container is a zip archive of XML parts — see
// `xl/worksheets/sheet1.xml` and the `sharedStrings.xml`
// indirection.
//
// Wraps github.com/xuri/excelize/v2 because writing a correct
// xlsx parser by hand is a fool's errand: the format has shared-
// strings indirection, inline strings, conditional formatting,
// rich-text runs, dates encoded as fractional days since 1900,
// and a fistful of other corners excelize already handles. The
// dep adds ~3 MB to the binary; operators who don't want xlsx
// support can simply not call xlsx.New() at registration time —
// that's the entire point of the per-format subpackages.
package xlsx

import (
	"bytes"
	"errors"
	"io"

	"github.com/klahr/zonventure/internal/teamimport"
	"github.com/xuri/excelize/v2"
)

// New returns the xlsx importer.
func New() teamimport.Importer { return &impl{} }

type impl struct{}

func (impl) Name() string         { return "Excel (.xlsx)" }
func (impl) Extensions() []string { return []string{".xlsx"} }
func (impl) MIMETypes() []string {
	// The official OOXML MIME plus the legacy .xlsx-as-zip and
	// generic-octet-stream cases that browsers still emit when
	// the extension is the only signal.
	return []string{
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	}
}

// maxDecompressedBytes caps excelize's unzip step so a zip-bomb
// (a 1 MB archive that expands to 50 GB) can't OOM the worker.
// excelize.Options.UnzipSizeLimit gates the total decompressed
// payload across all parts in the archive; setting it ~50 MB is
// far above any plausible roster sheet (kilobytes in practice)
// while well under any memory-exhaustion threshold.
const maxDecompressedBytes = 50 << 20 // 50 MiB

func (i impl) Parse(r io.Reader) ([]teamimport.Team, error) {
	rows, err := i.ReadRows(r)
	if err != nil {
		return nil, err
	}
	return teamimport.Rows(rows)
}

// ReadRows reads the first sheet's rows.
func (impl) ReadRows(r io.Reader) ([][]string, error) {
	// excelize wants an io.Reader for OpenReader, not a path —
	// buffer the upload so it can seek-via-bytes if needed. The
	// upstream handler caps file size, so we trust the caller's
	// budget here.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{
		UnzipSizeLimit: maxDecompressedBytes,
	})
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, errors.New("teamimport/xlsx: workbook has no sheets")
	}
	return f.GetRows(sheets[0])
}
