package server

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/sitename"
)

// TestSiteName_EmbeddedPagesFilled pins that every embedded HTML page
// comes out of readWebFile with ZV_SITE_NAME filled in, escaped.
func TestSiteName_EmbeddedPagesFilled(t *testing.T) {
	defer sitename.SetForTest("Acme & Co")()
	pages, err := fs.Glob(webFS, "web/*.html")
	if err != nil || len(pages) == 0 {
		t.Fatalf("no embedded pages: %v", err)
	}
	var used int
	for _, p := range pages {
		raw, _ := webFS.ReadFile(p)
		if !strings.Contains(string(raw), "{{SITE_") {
			continue
		}
		used++
		b, err := readWebFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "{{SITE_") {
			t.Errorf("%s: placeholder left after readWebFile", p)
		}
		if !strings.Contains(string(b), "Acme &amp; Co") {
			t.Errorf("%s: site name missing", p)
		}
	}
	if used == 0 {
		t.Fatal("no page uses the site-name placeholder")
	}
}

// TestSiteName_SPAIndex pins that the apps' index.html still carry the
// exact strings brandSPAIndex replaces, and that it replaces them.
func TestSiteName_SPAIndex(t *testing.T) {
	defer sitename.SetForTest("Acme & Co")()
	for _, app := range spaApps {
		src, err := os.ReadFile("../../web/apps/" + app + "/index.html")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{spaTitle, spaAppName} {
			if !strings.Contains(string(src), want) {
				t.Errorf("%s/index.html: missing %q, which brandSPAIndex replaces", app, want)
			}
		}
		out := string(brandSPAIndex(src))
		if !strings.Contains(out, "<title>Acme &amp; Co</title>") ||
			!strings.Contains(out, `<meta name="application-name" content="Acme &amp; Co" />`) {
			t.Errorf("%s: site name not swapped in", app)
		}
	}
}
