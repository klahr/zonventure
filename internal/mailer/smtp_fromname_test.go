package mailer

import (
	"strings"
	"testing"
)

func TestFromName(t *testing.T) {
	if got := withFromName("Zonventure <noreply@zv.test>", "Åke's Äventyr"); !strings.Contains(got, "<noreply@zv.test>") || strings.Contains(got, "Zonventure") {
		t.Errorf("withFromName = %q", got)
	}
	if got := withFromName("Zonventure <noreply@zv.test>", ""); got != "Zonventure <noreply@zv.test>" {
		t.Errorf("empty name = %q", got)
	}
	b, err := buildRFC5322(Message{To: []string{"a@b.test"}, Subject: "s", Text: "t", FromName: "Acme"}, "Zonventure <noreply@zv.test>")
	if err != nil || !strings.Contains(string(b), "From: \"Acme\" <noreply@zv.test>") {
		t.Errorf("header: %v\n%s", err, b)
	}
}
