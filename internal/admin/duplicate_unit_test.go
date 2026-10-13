package admin

import (
	"testing"
)

func TestDuplicateSlug(t *testing.T) {
	t.Parallel()
	taken := map[string]bool{"office-tour-copy": true, "office-tour-copy-2": true}
	isTaken := func(s string) (bool, error) { return taken[s], nil }
	for source, want := range map[string]string{
		"office-tour":        "office-tour-copy-3",
		"office-tour-copy-2": "office-tour-copy-3",
		"park":               "park-copy",
	} {
		got, err := duplicateSlug(source, isTaken)
		if err != nil || got != want {
			t.Errorf("duplicateSlug(%q) = %q, %v; want %q", source, got, err, want)
		}
		if !validSlug(got) {
			t.Errorf("%q is not a valid slug", got)
		}
	}
}

func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
