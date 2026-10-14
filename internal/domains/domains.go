// Package domains serves an organisation on its own address
// (adventures.theirbrand.se): which hostnames belong to which
// organisation, whether their DNS is right, and the origin its
// player-facing links use.
//
// The organisation points one CNAME at <target>.<suffix>, where suffix
// is ZV_DOMAIN_CNAME_SUFFIX (a wildcard record at the operator's DNS)
// and target is fixed when the domain is claimed. That one record
// proves control of the name and says which organisation it is for.
// Without a suffix the feature is off.
package domains

import (
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// Forever is the entitlement end a platform admin gives an
// organisation that keeps its domain for good, at no charge: its own
// operator, say.
const Forever = "9999-12-31"

// Grace is how long a domain keeps serving after its organisation's
// entitlement ends.
const Grace = 30 * 24 * time.Hour

// Status values, as stored in org_domains.status.
const (
	StatusWaiting     = "waiting"
	StatusWrong       = "wrong"
	StatusCertificate = "certificate"
	StatusLive        = "live"
	StatusRemoved     = "removed"
)

// Suffix is the CNAME target suffix, e.g. "cname.zonventure.com"; ""
// turns custom domains off.
func Suffix() string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(os.Getenv("ZV_DOMAIN_CNAME_SUFFIX"))), ".")
}

// Enabled reports whether the instance offers custom domains.
func Enabled() bool { return Suffix() != "" }

// TargetFor is the full CNAME value for target.
func TargetFor(target string) string { return target + "." + Suffix() }

// Refusals Normalize returns, for the console to explain.
const (
	RefuseInvalid = "invalid"
	RefuseApex    = "apex"
	RefuseWWW     = "www"
	RefuseOurs    = "ours"
)

// Normalize turns what someone typed — maybe with https://, a path, a
// trailing dot or capitals — into a lower-case ASCII hostname, or names
// why it can't be used. ours are the instance's own hostnames, which no
// organisation may claim nor any name under them.
func Normalize(in string, ours ...string) (string, string) {
	h := strings.ToLower(strings.TrimSpace(in))
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	if i := strings.LastIndexByte(h, '@'); i >= 0 {
		h = h[i+1:]
	}
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	h = strings.TrimSuffix(h, ".")
	a, err := idna.Lookup.ToASCII(h)
	if err != nil || a == "" || len(a) > 253 || net.ParseIP(a) != nil {
		return "", RefuseInvalid
	}
	labels := strings.Split(a, ".")
	if len(labels) < 2 {
		return "", RefuseInvalid
	}
	for _, l := range labels {
		if !validLabel(l) {
			return "", RefuseInvalid
		}
	}
	for _, o := range ours {
		o = strings.ToLower(strings.Trim(o, "."))
		if o != "" && (a == o || strings.HasSuffix(a, "."+o)) {
			return "", RefuseOurs
		}
	}
	if s := Suffix(); s != "" && (a == s || strings.HasSuffix(a, "."+s)) {
		return "", RefuseOurs
	}
	apex, err := publicsuffix.EffectiveTLDPlusOne(a)
	if err != nil {
		return "", RefuseInvalid
	}
	if a == apex {
		return "", RefuseApex
	}
	if labels[0] == "www" {
		return "", RefuseWWW
	}
	return a, ""
}

// Apex is the registered domain host is under (theirbrand.se), "" when
// it can't tell.
func Apex(host string) string {
	a, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return ""
	}
	return a
}

// Name is host's part under its registered domain, what a DNS panel
// asks for as the record's name: "adventures" for
// adventures.theirbrand.se.
func Name(host string) string {
	a := Apex(host)
	if a == "" || a == host {
		return "@"
	}
	return strings.TrimSuffix(host, "."+a)
}

func validLabel(l string) bool {
	if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
		return false
	}
	for _, c := range l {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// TargetLabel makes a CNAME target label from an organisation's slug.
func TargetLabel(slug string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(slug) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			b.WriteRune(c)
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 50 {
		s = strings.Trim(s[:50], "-")
	}
	if s == "" {
		s = "org"
	}
	return s
}
