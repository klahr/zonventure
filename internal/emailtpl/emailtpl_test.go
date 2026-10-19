package emailtpl

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
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
	h, _ := Render(Classic, Platform(), "s", c)
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
	h1, _ := Render("nope", Platform(), "s", Content{Heading: "H"})
	h2, _ := Render(Classic, Platform(), "s", Content{Heading: "H"})
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
	for _, f := range []string{"kinds.go", "emailtpl.go", "pictures.go"} {
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
	for _, p := range housePictures {
		if _, ok := catalog["sv"][p.alt]; !ok {
			missing[p.alt] = true
		}
	}
	for k := range missing {
		t.Errorf("no Swedish for %q", k)
	}
}

func TestSwedishEmail(t *testing.T) {
	subject, c := TeamInvitation("sv", "Måsarna", "Hamnnatten", "Äventyr AB", []PlayerLink{{Player: "Anna", URL: "https://e.org/p"}}, true)
	h, text := Render(Classic, Brand{Name: "Äventyr AB"}, subject, c)
	for _, want := range []string{`lang="sv"`, "Dina laglänkar till Äventyr AB", "Du är med: Hamnnatten", "Ditt lag", "kalenderinbjudan", "skickat med Zonventure", "Du får det här eftersom ditt lag är med i Hamnnatten från Äventyr AB."} {
		if !strings.Contains(h+subject, want) {
			t.Errorf("Swedish invitation lacks %q", want)
		}
	}
	if !strings.Contains(text, "Ditt lag Måsarna är redo.") || !strings.Contains(text, "- Anna: https://e.org/p\r\n") || strings.ContainsAny(text, "*[") {
		t.Errorf("text = %q", text)
	}
	if Lang("sv-SE") != "sv" || Lang("de") != "en" || Lang("") != "en" {
		t.Error("Lang does not map tags to email languages")
	}
}

// TestHostMails: the host is told they host, never that they play, and
// a host link sent again promises no receipt.
func TestHostMails(t *testing.T) {
	_, paid := TrailPaid("sv", "https://e.org/party/x", "https://e.org/j/y", "Kusinjakten", "1 november 2026", "Äventyr AB", false, true)
	_, again := TrailPaid("sv", "https://e.org/party/x", "https://e.org/j/y", "Kusinjakten", "1 november 2026", "Äventyr AB", false, false)
	_, playing := TrailPaid("sv", "https://e.org/party/x", "https://e.org/j/y", "Kusinjakten", "27 oktober 2026", "Äventyr AB", true, false)
	_, host := PartyHost("sv", "https://e.org/party/x", "Elsas kalas", "Äventyr AB")
	_, summary := RoundSummary("sv", "https://e.org/party/", "Elsas kalas", "Äventyr AB", 1, nil, nil)
	for _, c := range []Content{paid, again, host, summary} {
		if strings.Contains(c.Footer, "spelar med") || !strings.Contains(c.Footer, "eftersom du är värd för") {
			t.Errorf("footer = %q", c.Footer)
		}
	}
	if !strings.Contains(paid.Body, "Kvittot") || strings.Contains(again.Body, "Kvittot") || !strings.Contains(paid.Body, "värdlänken") {
		t.Errorf("paid = %q, again = %q", paid.Body, again.Body)
	}
	// Sent again once games have started: no first game to start by.
	if strings.Contains(playing.Body, "första omgången") || !strings.Contains(playing.Body, "fram till 27 oktober 2026") {
		t.Errorf("playing = %q", playing.Body)
	}
	if _, invite := HostedPlayerLink("sv", "https://e.org/j/y", "Alva fyller 8", "Zonventure"); !strings.Contains(invite.Footer, "värden för Alva fyller 8") {
		t.Errorf("invite footer = %q", invite.Footer)
	}
	_, summary = RoundSummary("sv", "https://e.org/party/", "Elsas kalas", "Äventyr AB", 1, nil, []RoundCard{{Title: "Fråga 3", Reached: 3, Right: 2}})
	if !strings.Contains(summary.Body, "Fråga 3: 67 % rätt (2 av 3 lag)") {
		t.Errorf("summary = %q", summary.Body)
	}
}

func TestAsGift(t *testing.T) {
	_, c := TeamLink("sv", "https://x/team/t", "", "", "Västerås Turism")
	subject, got := AsGift("sv", "Västerås Turism", "Kort stadsvandring", c)
	if subject != "En gåva från Västerås Turism: din biljett till Kort stadsvandring" || !strings.Contains(got.Heading, "En gåva från Västerås Turism") {
		t.Errorf("subject = %q, heading = %q", subject, got.Heading)
	}
	if got.Body != c.Body {
		t.Errorf("body changed: %q", got.Body)
	}
}

func TestWithFiles(t *testing.T) {
	_, c := TeamLink("sv", "https://x/team/t", "", "", "Org")
	if got := WithFiles(c, nil); got.Body != c.Body {
		t.Errorf("no files changed the body: %q", got.Body)
	}
	got := WithFiles(c, []FileLink{{Name: "Karta [v2].pdf", URL: "https://x/team/t/files/7"}})
	for _, want := range []string{"**Filer från arrangörerna**", `- [Karta \[v2\].pdf](https://x/team/t/files/7)`} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, got.Body)
		}
	}
}

// A Swedish invitation states the start in Swedish, in the
// organisation's time zone, and says when it has moved.
func TestWithScheduleSwedish(t *testing.T) {
	at := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	if got := When("sv", at, "Europe/Stockholm"); got != "fredag 1 maj 2026 kl. 12:00 (CEST)" {
		t.Errorf("When = %q", got)
	}
	if got := When("en", at, "Europe/London"); got != "Friday 1 May 2026, 11:00 (BST)" {
		t.Errorf("When in London = %q", got)
	}
	subject, c := TeamInvitation("sv", "Måsarna", "Hamnnatten", "Äventyr AB", nil, false)
	subject, c = WithSchedule(subject, c, "Hamnnatten", When("sv", at, ""), "Vid fontänen", true)
	if !strings.HasPrefix(subject, "Ny tid: ") || c.Heading != "Ny tid för Hamnnatten" ||
		!strings.Contains(c.Body, "Tiden har ändrats: det börjar nu **fredag 1 maj 2026 kl. 12:00 (CEST)**.") || !strings.Contains(c.Body, "Mötesplats: **Vid fontänen**") {
		t.Errorf("changed invitation = %q %q\n%s", subject, c.Heading, c.Body)
	}
}

// TestOrgInvitationNewAccount: an account made for the invitation is
// asked to choose its password, in the role it was invited to.
func TestOrgInvitationNewAccount(t *testing.T) {
	_, c := OrgInvitation("sv", "Acme", "org_writer", "https://x.test/arrange#reset?token=t", true)
	if !strings.Contains(c.Body, "skribent") || !strings.Contains(c.Body, "Välj ditt lösenord") || c.Buttons[0].URL != "https://x.test/arrange#reset?token=t" {
		t.Errorf("new account = %+v", c)
	}
	_, c = OrgInvitation("en", "Acme", "org_member", "https://x.test/arrange", false)
	if !strings.Contains(c.Body, "event host") || !strings.Contains(c.Body, "Sign in") {
		t.Errorf("existing account = %+v", c)
	}
}

// An organisation on its own domain heads its emails in its store's
// typeface, loaded from there, with the theme's as the fallback.
func TestTitleFont(t *testing.T) {
	b := Platform()
	b.TitleFont, b.TitleFontCSS = "Aclonica", "https://play.example.com/assets/fontface.css"
	h, _ := Render(Classic, b, "s", Content{Heading: "Hello"})
	if !strings.Contains(h, `<link rel="stylesheet" href="https://play.example.com/assets/fontface.css">`) {
		t.Fatalf("no font stylesheet: %s", h)
	}
	if !strings.Contains(h, `font:700 24px/1.25 &#34;Aclonica&#34;,-apple-system`) {
		t.Fatalf("heading not in the title font: %s", h)
	}
	if h, _ := Render(Classic, Platform(), "s", Content{Heading: "Hello"}); strings.Contains(h, "stylesheet") {
		t.Fatalf("a font without one chosen: %s", h)
	}
}
