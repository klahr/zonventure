package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/social"
)

func TestFooterSocial(t *testing.T) {
	if got := footerSocial(social.Links{}); got != "" {
		t.Fatalf("no links: got %q", got)
	}
	if got := sameAsJSON(social.Links{}); got != "[]" {
		t.Fatalf("no links: sameAs %q", got)
	}
	l := social.Links{"linkedin": "https://www.linkedin.com/company/zv?a=1&b=2", "instagram": "https://www.instagram.com/zv"}
	got := footerSocial(l)
	if !strings.Contains(got, `href="https://www.linkedin.com/company/zv?a=1&amp;b=2"`) || !strings.Contains(got, `aria-label="Instagram"`) {
		t.Fatalf("got %q", got)
	}
	if strings.Index(got, "Instagram") > strings.Index(got, "LinkedIn") {
		t.Fatalf("not in network order: %q", got)
	}
	var urls []string
	if err := json.Unmarshal([]byte(sameAsJSON(l)), &urls); err != nil || len(urls) != 2 || urls[0] != l["instagram"] || urls[1] != l["linkedin"] {
		t.Fatalf("sameAs %q: %v", sameAsJSON(l), err)
	}
}
