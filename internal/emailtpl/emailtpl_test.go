package emailtpl

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestEveryThemeRendersEveryKind(t *testing.T) {
	b := Brand{Name: "Acme Quests", LogoURL: "https://example.org/org-logo/7", Accent: "#0f766e", Accent2: "#f97316", Surface: "#f0fdfa", Ink: "#134e4a"}
	for _, th := range Themes {
		for _, k := range Kinds {
			subject, c := k.Sample("en", b.Name)
			h, text := Render(th.ID, b, subject, c)
			if !strings.Contains(h, "<html") || !strings.Contains(h, b.LogoURL) {
				t.Errorf("%s/%s: no page or logo", th.ID, k.ID)
			}
			for _, x := range c.Buttons {
				if !strings.Contains(h, x.URL) || !strings.Contains(text, x.URL) {
					t.Errorf("%s/%s: button %q missing from HTML or text", th.ID, k.ID, x.URL)
				}
			}
			if strings.Contains(h, "ZgotmplZ") {
				t.Errorf("%s/%s: a style was refused by the template", th.ID, k.ID)
			}
			if c.Heading != "" && !strings.Contains(text, c.Heading) {
				t.Errorf("%s/%s: text lacks the heading", th.ID, k.ID)
			}
		}
	}
}

func TestNamesCannotInjectHTML(t *testing.T) {
	_, c := TeamInvitation("en", `<script>x</script>`, "E", "O", []PlayerLink{{Player: "<b>p</b>", URL: "https://e.org/p"}}, false)
	h, _ := Render(Classic, Zonventure(), "s", c)
	if strings.Contains(h, "<script>x") || strings.Contains(h, "<b>p</b>") {
		t.Errorf("unescaped names in %s", h)
	}
}

func TestDarkBrandStaysReadable(t *testing.T) {
	// A brand made for a dark site: light text. On a white card it would vanish.
	if got := readable("#f5f5f5", "#ffffff"); got != "#111827" {
		t.Errorf("readable(light on white) = %s", got)
	}
	if got := readable("#134e4a", "#ffffff"); got != "#134e4a" {
		t.Errorf("readable(dark on white) = %s, want the brand's own", got)
	}
}

func TestLinesKeepsLineBreaks(t *testing.T) {
	if got := Lines("Hi\nthere\n\nBye"); got != "Hi  \nthere\n\nBye" {
		t.Errorf("Lines = %q", got)
	}
}

func TestUnknownThemeFallsBack(t *testing.T) {
	h1, _ := Render("nope", Zonventure(), "s", Content{Heading: "H"})
	h2, _ := Render(Classic, Zonventure(), "s", Content{Heading: "H"})
	if h1 != h2 {
		t.Error("an unknown theme should render as classic")
	}
}

// Every string an email says has its Swedish: the English is the key,
// so one left out would go out in English to a Swedish team.
func TestEverySwedishStringIsThere(t *testing.T) {
	re := regexp.MustCompile("T\\(lang, (\"(?:[^\"\\\\]|\\\\.)*\")")
	missing := map[string]bool{}
	found := 0
	for _, f := range []string{"kinds.go", "emailtpl.go"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			key, err := strconv.Unquote(m[1])
			if err != nil {
				t.Fatalf("%s: %v", m[1], err)
			}
			found++
			if _, ok := catalog["sv"][key]; !ok {
				missing[key] = true
			}
		}
	}
	if found < 60 {
		t.Fatalf("found only %d strings: the pattern no longer matches the code", found)
	}
	// Strings chosen by a variable, as SuggestionDecided does.
	for _, key := range []string{"%[1]s accepted your suggestion on “%[2]s”", "%[1]s declined your suggestion on “%[2]s”", "Your suggestion was accepted.", "Your suggestion was declined.", "Suggestion accepted", "Suggestion declined", "%[1]s accepted the change you suggested to %[2]s.", "%[1]s declined the change you suggested to %[2]s."} {
		if _, ok := catalog["sv"][key]; !ok {
			missing[key] = true
		}
	}
	for k := range missing {
		t.Errorf("no Swedish for %q", k)
	}
}

func TestSwedishEmail(t *testing.T) {
	subject, c := TeamInvitation("sv", "Måsarna", "Hamnnatten", "Äventyr AB", []PlayerLink{{Player: "Anna", URL: "https://e.org/p"}}, true)
	h, text := Render(Classic, Brand{Name: "Äventyr AB"}, subject, c)
	for _, want := range []string{`lang="sv"`, "Era laglänkar till Zonventure", "Ni är med: Hamnnatten", "Ditt lag", "kalenderinbjudan", "skickat med Zonventure", "Du får det här eftersom ditt lag spelar Hamnnatten med Äventyr AB."} {
		if !strings.Contains(h+subject, want) {
			t.Errorf("Swedish invitation lacks %q", want)
		}
	}
	if !strings.Contains(text, "Ditt lag **Måsarna** är redo att spela") {
		t.Errorf("text = %q", text)
	}
	if Lang("sv-SE") != "sv" || Lang("de") != "en" || Lang("") != "en" {
		t.Error("Lang does not map tags to email languages")
	}
}
