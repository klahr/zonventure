package domains

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	t.Setenv("ZV_DOMAIN_CNAME_SUFFIX", "cname.zonventure.com")
	cases := []struct {
		in, host, refuse string
	}{
		{"adventures.theirbrand.se", "adventures.theirbrand.se", ""},
		{"  https://Adventures.TheirBrand.se/store?x=1 ", "adventures.theirbrand.se", ""},
		{"adventures.theirbrand.se.", "adventures.theirbrand.se", ""},
		{"äventyr.theirbrand.se", "xn--ventyr-9ta.theirbrand.se", ""},
		{"theirbrand.se", "", RefuseApex},
		{"theirbrand.co.uk", "", RefuseApex},
		{"play.theirbrand.co.uk", "play.theirbrand.co.uk", ""},
		{"www.theirbrand.se", "", RefuseWWW},
		{"x.zonventure.com", "", RefuseOurs},
		{"acme.cname.zonventure.com", "", RefuseOurs},
		{"localhost", "", RefuseInvalid},
		{"10.0.0.1", "", RefuseInvalid},
		{"bad_label.theirbrand.se", "", RefuseInvalid},
		{"", "", RefuseInvalid},
	}
	for _, c := range cases {
		h, r := Normalize(c.in, "zonventure.com")
		if h != c.host || r != c.refuse {
			t.Errorf("Normalize(%q) = %q, %q; want %q, %q", c.in, h, r, c.host, c.refuse)
		}
	}
}

func TestName(t *testing.T) {
	if got := Name("adventures.theirbrand.se"); got != "adventures" {
		t.Errorf("Name = %q", got)
	}
	if got := Name("a.b.theirbrand.co.uk"); got != "a.b" {
		t.Errorf("Name = %q", got)
	}
}

func TestTargetLabel(t *testing.T) {
	if got := TargetLabel("Acme_Quests-"); got != "acmequests" {
		t.Errorf("TargetLabel = %q", got)
	}
	if got := TargetLabel("--"); got != "org" {
		t.Errorf("TargetLabel = %q", got)
	}
}

type fakeDNS struct {
	cname map[string]string
	hosts map[string][]string
}

func (f fakeDNS) LookupCNAME(_ context.Context, h string) (string, error) {
	if c, ok := f.cname[h]; ok {
		return c + ".", nil
	}
	if _, ok := f.hosts[h]; ok {
		return h + ".", nil
	}
	return "", errors.New("no such host")
}

func (f fakeDNS) LookupHost(_ context.Context, h string) ([]string, error) {
	if a, ok := f.hosts[h]; ok {
		return a, nil
	}
	if c, ok := f.cname[h]; ok {
		if a, ok := f.hosts[c]; ok {
			return a, nil
		}
		return []string{"129.212.222.90"}, nil
	}
	return nil, errors.New("no such host")
}

func TestCheck(t *testing.T) {
	t.Setenv("ZV_DOMAIN_CNAME_SUFFIX", "cname.zonventure.com")
	const host, target = "adventures.theirbrand.se", "acme.cname.zonventure.com"
	ctx := context.Background()
	cases := []struct {
		name    string
		dns     fakeDNS
		status  string
		problem string
	}{
		{"right", fakeDNS{cname: map[string]string{host: target}}, StatusCertificate, ""},
		{"nothing", fakeDNS{}, StatusWaiting, ""},
		{"doubled", fakeDNS{cname: map[string]string{host + ".theirbrand.se": target}}, StatusWrong, ProblemDoubled},
		{"a record", fakeDNS{hosts: map[string][]string{host: {"129.212.222.90"}}}, StatusWrong, ProblemAddress},
		{"cloudflare", fakeDNS{hosts: map[string][]string{host: {"104.21.3.4"}}}, StatusWrong, ProblemCloudflare},
		{"other org", fakeDNS{cname: map[string]string{host: "other.cname.zonventure.com"}}, StatusWrong, ProblemOtherTarget},
		{"elsewhere", fakeDNS{cname: map[string]string{host: "theirbrand.github.io"}}, StatusWrong, ProblemOtherTarget},
	}
	for _, c := range cases {
		got := Check(ctx, c.dns, host, target)
		if got.Status != c.status || got.Problem != c.problem {
			t.Errorf("%s: got %+v; want %s/%s", c.name, got, c.status, c.problem)
		}
	}
}

func TestEntryWindows(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	e := Entry{Status: StatusLive, Until: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)}
	if !e.Live(now) || e.InGrace(now) {
		t.Error("the last day should be live, not in grace")
	}
	if !e.Live(now.Add(48*time.Hour)) || !e.InGrace(now.Add(48*time.Hour)) {
		t.Error("two days later should be in grace")
	}
	if e.Active(now.Add(Grace + 48*time.Hour)) {
		t.Error("after the grace it should be off")
	}
	if (Entry{Status: StatusLive}).Active(now) {
		t.Error("no entitlement should be off")
	}
}

func TestForever(t *testing.T) {
	until, err := time.Parse("2006-01-02", Forever)
	if err != nil {
		t.Fatal(err)
	}
	e := Entry{Status: StatusLive, Until: until}
	if !e.Live(time.Now()) || e.InGrace(time.Now()) {
		t.Error("an entitlement with no end date should be live and never in grace")
	}
}
