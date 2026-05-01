package admin

import (
	"strings"
	"testing"
)

func TestGenerateDuplicateSlug(t *testing.T) {
	got, err := generateDuplicateSlug("office-tour")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.HasPrefix(got, "office-tour-") {
		t.Errorf("expected prefix office-tour-, got %q", got)
	}
	suffix := strings.TrimPrefix(got, "office-tour-")
	if len(suffix) == 0 || len(suffix) > 6 {
		t.Errorf("suffix length unreasonable: %q", suffix)
	}
	if !validSlug(got) {
		t.Errorf("generated slug failed validation: %q", got)
	}
	// Two consecutive calls must produce different suffixes — if they
	// don't, the random-source isn't doing its job and we'd be racing
	// the unique index in production.
	got2, err := generateDuplicateSlug("office-tour")
	if err != nil {
		t.Fatal(err)
	}
	if got == got2 {
		t.Errorf("two consecutive calls produced the same slug %q — random source broken", got)
	}
}

func TestIsUniqueViolation(t *testing.T) {
	if isUniqueViolation(nil) {
		t.Error("nil should not be a unique violation")
	}
	if isUniqueViolation(stubErr("connection refused")) {
		t.Error("connection error should not match")
	}
	if !isUniqueViolation(stubErr(`ERROR: duplicate key value violates unique constraint "adventures_slug_key" (SQLSTATE 23505)`)) {
		t.Error("postgres unique-violation should match")
	}
}

func TestDeref(t *testing.T) {
	if got := deref(nil); got != "" {
		t.Errorf("nil should produce empty string, got %q", got)
	}
	v := "hello"
	if got := deref(&v); got != "hello" {
		t.Errorf("expected hello, got %q", got)
	}
}

type stubErr string

func (s stubErr) Error() string { return string(s) }
