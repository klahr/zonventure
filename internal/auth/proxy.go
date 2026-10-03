package auth

import (
	"context"
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

// trustedPeerKey marks a request whose immediate peer was a trusted
// proxy before its RemoteAddr was rewritten to the client address.
type trustedPeerKey struct{}

// MarkTrustedPeer records that r arrived from a trusted proxy. The
// real-IP middleware calls it before rewriting RemoteAddr to the
// client, so IsTrustedPeer keeps answering for the original peer
// (otherwise X-Forwarded-Proto is ignored downstream and cookies lose
// their Secure flag behind the proxy).
func MarkTrustedPeer(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), trustedPeerKey{}, true))
}

// IsTrustedPeer reports whether the immediate peer (r.RemoteAddr, or
// the peer recorded by MarkTrustedPeer) is in the configured
// trusted-proxy set. Used as the predicate for "should this request's
// X-Forwarded-* be honoured?".
//
// A nil request and an unparseable RemoteAddr both return false (fail
// closed). The trusted-proxy list is read atomically, so callers
// don't need to lock.
func IsTrustedPeer(r *http.Request) bool {
	if r == nil {
		return false
	}
	if marked, _ := r.Context().Value(trustedPeerKey{}).(bool); marked {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return addrTrusted(host)
}

// addrTrusted reports whether host (a bare IP, no port) falls in the
// configured trusted-proxy set.
func addrTrusted(host string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(host))
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

// RightmostUntrustedXFF returns the right-most X-Forwarded-For entry
// that is not itself a trusted proxy — the real client as seen by the
// first trusted hop. Walking from the right is the load-bearing bit: a
// proxy that APPENDS to XFF (nginx's proxy_add_x_forwarded_for, the
// common default) leaves any client-forged entries on the LEFT, so the
// left-most value is attacker-controlled. The right-most non-trusted
// entry is the earliest hop we can still vouch for. Only consult this
// once the immediate peer is already known to be trusted. Returns ""
// when XFF is empty; falls back to the left-most entry when every hop
// listed is itself a trusted proxy.
func RightmostUntrustedXFF(xff string) string {
	if strings.TrimSpace(xff) == "" {
		return ""
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		s := strings.TrimSpace(parts[i])
		if s == "" {
			continue
		}
		if !addrTrusted(s) {
			return s
		}
	}
	return strings.TrimSpace(parts[0])
}
