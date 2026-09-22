package server

import (
	"io/fs"
	"path"
	"strings"
	"testing"
)

// The placeholder must stay committed. Without it `//go:embed
// all:web/dist` matches nothing and the package fails to COMPILE, so a
// fresh clone or a CI runner without Node could not build the server at
// all. Deleting it is an easy mistake to make while cleaning build
// output; this test makes it a loud one.
func TestSPAPlaceholderEmbedded(t *testing.T) {
	b, err := spaFS.ReadFile("web/dist/index.html")
	if err != nil {
		t.Fatalf("web/dist/index.html missing from the embed: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("web/dist/index.html is empty")
	}
}

// The failure this guards against is silent: `go build` happily
// succeeds against a stale or half-written web/dist, producing a binary
// whose index.html points at chunks that were never embedded. The page
// then loads blank in production with only a 404 in the browser
// console — nothing in the server log, nothing in the build output.
//
// Skips when an app hasn't been built, so `go test ./...` works without
// Node. CI runs the frontend build first, so there it actually asserts.
func TestSPAAssetsPresent(t *testing.T) {
	for _, app := range spaApps {
		t.Run(app, func(t *testing.T) {
			if _, err := spaFS.Open(path.Join("web/dist", app, "index.html")); err != nil {
				t.Skipf("app %q not built; run `make web`", app)
			}
			if missing := spaAssetsPresent(app); len(missing) > 0 {
				t.Errorf("index.html references assets absent from the embed:\n  %s",
					strings.Join(missing, "\n  "))
			}
		})
	}
}

// Vite writes its manifest to .vite/manifest.json. Plain //go:embed
// skips dot-prefixed entries, so losing the `all:` prefix would drop it
// from the binary without any other symptom. Asserting on the manifest
// specifically is what makes that regression visible.
func TestSPAEmbedIncludesDotfiles(t *testing.T) {
	for _, app := range spaApps {
		if _, err := spaFS.Open(path.Join("web/dist", app, "index.html")); err != nil {
			t.Skipf("app %q not built; run `make web`", app)
		}
		manifest := path.Join("web/dist", app, ".vite", "manifest.json")
		if _, err := spaFS.Open(manifest); err != nil {
			t.Errorf("%s missing from the embed — has the `all:` prefix been dropped from the go:embed directive? (%v)", manifest, err)
		}
	}
}

// Every app in spaApps needs its Vite `base` set to /app/<name>/, or
// the emitted asset URLs are absolute from "/" and 404 behind the
// prefix. A built app whose index.html contains no reference under its
// own prefix is the signature of that misconfiguration.
func TestSPABaseMatchesRoute(t *testing.T) {
	for _, app := range spaApps {
		t.Run(app, func(t *testing.T) {
			b, err := spaFS.ReadFile(path.Join("web/dist", app, "index.html"))
			if err != nil {
				t.Skipf("app %q not built; run `make web`", app)
			}
			prefix := "/app/" + app + "/"
			for _, ref := range extractAssetRefs(string(b)) {
				if strings.HasPrefix(ref, prefix) {
					return // at least one correctly-based asset
				}
			}
			t.Errorf("no asset in %s/index.html is served from %q; check `base` in web/apps/%s/vite.config.ts", app, prefix, app)
		})
	}
}

func TestExtractAssetRefs(t *testing.T) {
	const html = `<link rel="icon" href="/favicon.svg">` +
		`<script type="module" src="/app/arrange/assets/index-abc.js"></script>` +
		`<link rel="stylesheet" href="https://example.test/x.css">`

	got := extractAssetRefs(html)
	want := map[string]bool{
		"/favicon.svg":                     false,
		"/app/arrange/assets/index-abc.js": false,
		"https://example.test/x.css":       false,
	}
	for _, g := range got {
		if _, ok := want[g]; !ok {
			t.Errorf("unexpected ref %q", g)
			continue
		}
		want[g] = true
	}
	for ref, seen := range want {
		if !seen {
			t.Errorf("ref %q not extracted", ref)
		}
	}
}

// spaAssetsPresent must ignore external and cross-handler references —
// /favicon.svg is served by the existing static asset routes, not from
// web/dist, so flagging it would make the assertion useless.
func TestSPAAssetsPresentIgnoresForeignRefs(t *testing.T) {
	if _, err := spaFS.Open(path.Join("web/dist", "arrange", "index.html")); err != nil {
		t.Skip("arrange not built; run `make web`")
	}
	for _, m := range spaAssetsPresent("arrange") {
		if !strings.HasPrefix(m, "web/dist/arrange/") {
			t.Errorf("reported %q as missing, but it is not this app's asset", m)
		}
	}
}

// An unbuilt app must report its own index.html as missing rather than
// silently passing, otherwise the guard above would be vacuous exactly
// when the build is broken.
func TestSPAAssetsPresentUnbuiltApp(t *testing.T) {
	missing := spaAssetsPresent("no-such-app")
	if len(missing) != 1 || missing[0] != "web/dist/no-such-app/index.html" {
		t.Errorf("spaAssetsPresent(unbuilt) = %v, want its index.html reported missing", missing)
	}
}

func TestSPABuildIDStableAndDistinct(t *testing.T) {
	if got := spaBuildID("no-such-app"); got != "dev" {
		t.Errorf("spaBuildID(unbuilt) = %q, want %q", got, "dev")
	}
	if _, err := spaFS.Open(path.Join("web/dist", "arrange", "index.html")); err != nil {
		t.Skip("arrange not built; run `make web`")
	}
	first := spaBuildID("arrange")
	if first == "dev" || len(first) != 16 {
		t.Fatalf("spaBuildID(arrange) = %q, want a 16-char digest", first)
	}
	if second := spaBuildID("arrange"); second != first {
		t.Errorf("spaBuildID not stable: %q then %q", first, second)
	}
}

// Guards the embed root itself: if web/dist were ever embedded from the
// wrong path the other tests would skip rather than fail, so assert the
// directory is walkable and non-empty.
func TestSPAEmbedRootWalkable(t *testing.T) {
	n := 0
	err := fs.WalkDir(spaFS, "web/dist", func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking web/dist: %v", err)
	}
	if n == 0 {
		t.Fatal("web/dist embedded but contains no files")
	}
}
