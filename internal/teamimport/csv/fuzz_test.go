package csv

import (
	"bytes"
	"strings"
	"testing"
)

// FuzzParse drives the CSV importer with arbitrary bytes. The
// invariant is panic-freedom — invalid CSV must come back as a clean
// error, not a panic. This is the file-upload path on
// /api/admin/teams/import, which accepts arbitrary user-supplied
// bytes from a multipart POST.
func FuzzParse(f *testing.F) {
	seeds := [][]byte{
		[]byte(""),
		[]byte("name,email\nAlice,alice@example.com\nBob,bob@example.com"),
		[]byte("Alice\nBob\n"),
		[]byte(",,,,,"),
		[]byte("\xff\xfe\x00\x00"), // BOM/garbage
		[]byte(strings.Repeat("a,", 10000)),
		[]byte("\"unterminated"),
		[]byte("\n\n\n\n"),
		[]byte("name\nAlice\rBob\rCharlie"),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	imp := New()
	f.Fuzz(func(t *testing.T, body []byte) {
		// Cap input so a 10MB pathological case doesn't drown the
		// fuzzer. The handler guards body size at a higher layer.
		if len(body) > 256*1024 {
			body = body[:256*1024]
		}
		_, _ = imp.Parse(bytes.NewReader(body))
	})
}
