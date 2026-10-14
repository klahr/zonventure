package admin

import (
	"testing"

	"github.com/klahr/zonventure/internal/domains"
)

// Names under the instance's own domain: the public hostname and what
// is under it stay refused; its sibling subdomains are own-zone names,
// for an organisation given its domain with no end date.
func TestOwnZoneNames(t *testing.T) {
	t.Setenv("ZV_DOMAIN_CNAME_SUFFIX", "cname.zonventure.com")
	for _, public := range []string{"https://v4.zonventure.com", "https://zonventure.com"} {
		h := &Handlers{PublicURL: public}
		cases := []struct {
			in, refuse string
			own        bool
		}{
			{"adventures.zonventure.com", "", true},
			{"zonventure.com", domains.RefuseApex, false},
			{"x.cname.zonventure.com", domains.RefuseOurs, false},
			{"adventures.theirbrand.se", "", false},
		}
		if public == "https://v4.zonventure.com" {
			cases = append(cases, struct {
				in, refuse string
				own        bool
			}{"v4.zonventure.com", domains.RefuseOurs, false}, struct {
				in, refuse string
				own        bool
			}{"a.v4.zonventure.com", domains.RefuseOurs, false})
		}
		for _, c := range cases {
			host, refuse := domains.Normalize(c.in, h.ourHosts()...)
			if refuse != c.refuse {
				t.Errorf("%s: Normalize(%q) refused %q, want %q", public, c.in, refuse, c.refuse)
				continue
			}
			if refuse == "" && h.ownZone(host) != c.own {
				t.Errorf("%s: ownZone(%q) = %v, want %v", public, host, !c.own, c.own)
			}
		}
	}
}
