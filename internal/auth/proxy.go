package auth

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
)

// trustedProxies holds the set of CIDR ranges the server treats as
// reverse proxies whose forwarded headers are honoured. An atomic
// pointer keeps reads lock-free on the request hot path.
//
// The default (empty slice) means "trust nothing": every request is
// treated as if it came directly from a client, X-Forwarded-* are
// ignored, and the per-IP limiter / audit log records the immediate
// peer's address. This is the safest default for a fresh self-host —
// an operator who actually has a proxy in front explicitly opts in
// via SetTrustedProxies (cmd/server reads ZV_TRUSTED_PROXIES at
// boot).
var trustedProxies atomic.Pointer[[]netip.Prefix]

// SetTrustedProxies replaces the trusted-proxy CIDR list. cmd/server
// calls this once at startup; tests may call it to install a fixture.
// Passing nil or an empty slice disables forwarded-header trust
// (the safe default).
func SetTrustedProxies(prefixes []netip.Prefix) {
	cp := append([]netip.Prefix(nil), prefixes...)
	trustedProxies.Store(&cp)
}

// ParseTrustedProxies turns a comma-separated list of CIDR strings
// (or bare IPs, which are upgraded to /32 or /128) into a slice
// suitable for SetTrustedProxies. Empty input returns a nil slice.
//
// Example accepted values:
//
//	"10.0.0.0/8"
//	"10.0.0.0/8,127.0.0.1,::1"
//	"172.16.0.0/12, 192.168.0.0/16"
func ParseTrustedProxies(spec string) ([]netip.Prefix, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	var out []netip.Prefix
	for raw := range strings.SplitSeq(spec, ",") {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			ip, err := netip.ParseAddr(s)
			if err != nil {
				return nil, fmt.Errorf("trusted proxy %q: %w", s, err)
			}
			bits := 32
			if ip.Is6() {
				bits = 128
			}
			out = append(out, netip.PrefixFrom(ip, bits))
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", s, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// IsTrustedPeer reports whether the immediate peer (r.RemoteAddr) is
// in the configured trusted-proxy set. Used as the predicate for
// "should this request's X-Forwarded-* be honoured?".
//
// A nil request and an unparseable RemoteAddr both return false (fail
// closed). The trusted-proxy list is read atomically, so callers
// don't need to lock.
func IsTrustedPeer(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	pp := trustedProxies.Load()
	if pp == nil {
		return false
	}
	for _, p := range *pp {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
