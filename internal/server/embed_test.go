package server

import (
	"os"
	"path"
	"strings"
	"testing"
)

// TestEmbed_AssetsPresent is the H17 regression guard. The //go:embed
// directive enumerates files by name; a contributor adding a new
// asset to internal/server/web/ but forgetting the embed line gets
// a build that succeeds, ships, and panics at request time. This
// test walks the on-disk tree and asserts each shippable asset is
// also visible through webFS — catching the gap in CI.
//
// Files that exist on disk but are intentionally NOT embedded
// (e.g. *.bak) live in the excluded set below.
func TestEmbed_AssetsPresent(t *testing.T) {
	excluded := map[string]bool{}
	wantDirs := []string{
		"web",
		"web/locales/web",
		"web/locales/landing",
	}

	for _, dir := range wantDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if excluded[name] {
				continue
			}
			// Skip dotfiles and editor backups so a stray .swp doesn't
			// cause a CI failure.
			if strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~") {
				continue
			}
			rel := path.Join(dir, name)
			if _, err := webFS.ReadFile(rel); err != nil {
				t.Errorf("on-disk asset %q is not embedded: %v", rel, err)
			}
		}
	}
}
