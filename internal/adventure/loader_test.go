package adventure

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Demo(t *testing.T) {
	root := filepath.Join("..", "..", "adventures")
	lib, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	adv := lib.Get("demo")
	if adv == nil {
		t.Fatalf("demo adventure not loaded")
	}
	if len(adv.Pages) != 3 {
		t.Fatalf("want 3 pages, got %d", len(adv.Pages))
	}
	if !adv.Pages[1].Location.Gated() {
		t.Fatalf("page 2 should have a gated location")
	}
	for i, p := range adv.Pages {
		if p.Location.Lat == 0 || p.Location.Lng == 0 {
			t.Fatalf("page %d missing location", i)
		}
	}
}

func TestLoad_IgnoresMissingRoot(t *testing.T) {
	lib, err := Load(filepath.Join(os.TempDir(), "zv-definitely-not-here"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(lib.List()) != 0 {
		t.Fatalf("expected empty library")
	}
}

func TestCheckAnswer(t *testing.T) {
	p := Page{Accept: []string{"Blue", "azure"}}
	cases := []struct {
		in   string
		want bool
	}{
		{"blue", true},
		{"  BLUE  ", true},
		{"AZURE", true},
		{"red", false},
		{"", false},
		{"blu e", false},
	}
	for _, c := range cases {
		if got := p.CheckAnswer(c.in); got != c.want {
			t.Errorf("CheckAnswer(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestLoadOne_RequiresPrompt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "adventure.toml"), []byte(`title = "x"`), 0o644); err != nil {
		t.Fatal(err)
	}
	pagesDir := filepath.Join(dir, "pages")
	if err := os.Mkdir(pagesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pagesDir, "01.toml"), []byte(`accept = ["x"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOne(dir, "x"); err == nil {
		t.Fatalf("expected error for missing prompt")
	}
}
