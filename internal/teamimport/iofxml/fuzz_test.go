package iofxml

import (
	"bytes"
	"strings"
	"testing"
)

// FuzzParse drives the IOF-XML importer with arbitrary bytes. The
// invariant is panic-freedom — XML decoders are notorious sources of
// stack overflow / billion-laughs / external-entity bugs.
func FuzzParse(f *testing.F) {
	seeds := [][]byte{
		[]byte(""),
		[]byte("<?xml version=\"1.0\"?><EntryList></EntryList>"),
		[]byte("<not>valid</xml>"),
		[]byte("<!DOCTYPE x [<!ENTITY a \"AAAA\">]><x>&a;</x>"), // simplified billion-laughs
		[]byte("<a><b><c><d></d></c></b></a>"),
		[]byte(strings.Repeat("<a>", 1000) + strings.Repeat("</a>", 1000)),
		[]byte("\xff\xfe\x00\x00"),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	imp := New()
	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > 256*1024 {
			body = body[:256*1024]
		}
		_, _ = imp.Parse(bytes.NewReader(body))
	})
}
