package tokenx

import (
	"encoding/base64"
	"testing"
)

func TestNew_LengthAndAlphabet(t *testing.T) {
	cases := []int{16, 18, 24, 32}
	for _, n := range cases {
		got, err := New(n)
		if err != nil {
			t.Fatalf("New(%d): %v", n, err)
		}
		// RawURLEncoding has a known formula: ceil(n*4/3) characters,
		// no padding.
		want := base64.RawURLEncoding.EncodedLen(n)
		if len(got) != want {
			t.Errorf("New(%d) length = %d, want %d", n, len(got), want)
		}
		// The result must round-trip through the same decoder, and the
		// decoded bytes must match the encoded length.
		dec, err := base64.RawURLEncoding.DecodeString(got)
		if err != nil {
			t.Errorf("New(%d) decode: %v", n, err)
		}
		if len(dec) != n {
			t.Errorf("New(%d) decoded length = %d, want %d", n, len(dec), n)
		}
	}
}

// TestNew_Random rejects the (astronomically unlikely) case where two
// successive calls return the same string — would mean rand is broken.
func TestNew_Random(t *testing.T) {
	a, err := New(16)
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(16)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("two consecutive New(16) calls returned identical %q", a)
	}
}
