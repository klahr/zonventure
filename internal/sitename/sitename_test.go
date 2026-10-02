package sitename

import "testing"

func TestFillEscapesName(t *testing.T) {
	defer SetForTest(`Acme "Quests" & <Co>`)()
	got := Fill(`<title>Play · {{SITE_NAME}}</title>`)
	want := `<title>Play · Acme &#34;Quests&#34; &amp; &lt;Co&gt;</title>`
	if got != want {
		t.Fatalf("Fill = %q, want %q", got, want)
	}
	if s := "no placeholder"; Fill(s) != s {
		t.Fatalf("Fill changed text without a placeholder")
	}
}

func TestWordmark(t *testing.T) {
	defer SetForTest(Default)()
	if got := Fill("{{SITE_WORDMARK}}"); got != "zonventure" {
		t.Errorf("default wordmark = %q", got)
	}
	SetForTest("Acme Quests")
	if got := Fill("{{SITE_WORDMARK}}"); got != "Acme Quests" {
		t.Errorf("custom wordmark = %q", got)
	}
}
