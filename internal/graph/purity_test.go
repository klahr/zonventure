package graph_test

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The graph packages are the domain core, and their value is that they
// can be reasoned about and tested without running an event. That only
// holds while they stay pure: the moment one of them can reach a
// database, a clock or the network, a test for it needs a fixture, and
// the fixture becomes the reason nobody adds the next test.
//
// So the constraint is enforced rather than documented. This is the CI
// gate the plan asks for, and it lives here rather than in a shell
// script so it runs with `go test ./...` on a developer's machine too.
func TestDomainPackagesStayPure(t *testing.T) {
	const modulePrefix = "github.com/klahr/zonventure/"

	// Reaching any of these would mean the package can no longer be
	// exercised as a pure function of its inputs.
	banned := map[string]string{
		"database/sql": "the domain must not touch a database; internal/play persists",
		"net/http":     "the domain must not speak HTTP; the server layer does",
		"net":          "the domain must not reach the network",
		"os":           "the domain must not read the environment or the filesystem",
		"math/rand":    "use a seeded hash instead, so a run reproduces in the simulator and the recap",
	}

	// time is allowed for types and arithmetic, but reading the process
	// clock makes a replayed or simulated event evaluate differently
	// from the one the team actually played. The clock arrives through
	// cond.State.LocalTime instead.
	const clockNote = "read the clock through cond.State.LocalTime, not time.Now"

	pkgs := []string{
		"internal/graph",
		"internal/graph/cond",
		"internal/graph/match",
		"internal/graph/walk",
		"internal/graph/valid",
		"internal/graph/convert",
	}

	for _, rel := range pkgs {
		t.Run(rel, func(t *testing.T) {
			pkg, err := build.Import(modulePrefix+rel, "", 0)
			if err != nil {
				t.Fatalf("importing %s: %v", rel, err)
			}

			// Only non-test imports: a test may legitimately use os or
			// time to build a fixture.
			for _, imp := range pkg.Imports {
				if why, bad := banned[imp]; bad {
					t.Errorf("%s imports %q — %s", rel, imp, why)
				}
				// Nothing in the domain may depend on a heavier layer of
				// the application; dependencies point inward only.
				if strings.HasPrefix(imp, modulePrefix) {
					inner := strings.TrimPrefix(imp, modulePrefix)
					if !strings.HasPrefix(inner, "internal/graph") {
						t.Errorf("%s imports %q; the domain may only depend on internal/graph/*", rel, inner)
					}
				}
			}

			// time.Now is the subtle one, so check the call rather than
			// the import.
			if usesTimeNow(t, pkg.Dir, pkg.GoFiles) {
				t.Errorf("%s calls time.Now — %s", rel, clockNote)
			}
		})
	}
}

func usesTimeNow(t *testing.T, dir string, files []string) bool {
	t.Helper()
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		if strings.Contains(string(b), "time.Now(") {
			return true
		}
	}
	return false
}
