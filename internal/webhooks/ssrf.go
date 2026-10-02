package webhooks

import (
	"errors"
	"net"
	"os"
)

// LoopbackAllowed reports whether the operator has opted in to webhook
// delivery against loopback/localhost targets, via
// ZV_WEBHOOKS_ALLOW_LOOPBACK=1. Off by default so an org_admin can't
// register an endpoint pointing at 127.0.0.1 and turn the delivery
// worker into an SSRF probe of services co-located with the app.
func LoopbackAllowed() bool {
	switch os.Getenv("ZV_WEBHOOKS_ALLOW_LOOPBACK") {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// errNonPublic is the public-facing reason a webhook URL fails the
// SSRF gate. Returned identically from ValidateHost and LookupPublic
// so the user never learns whether the rejection was due to a literal
// private IP or a DNS answer in a private range.
var errNonPublic = errors.New("url resolves to a non-public address (private / loopback / metadata)")

// ValidateHost resolves the supplied host and reports an error when
// any answer falls in a non-public IP range. The check runs in two
// places:
//
//  1. Endpoint registration in internal/admin (validateWebhookURL)
//     — catches obvious mistakes before they're stored.
//  2. Worker delivery (delivery.go) — re-resolves on every send via
//     LookupPublic, which returns the chosen IP so the dialer can
//     bind to that literal address and a DNS-rebinding flip between
//     resolve and dial can't pivot the worker into an internal
//     service.
//
// Loopback hosts are intentionally NOT exempted here — the caller
// (validateWebhookURL or the delivery worker) decides whether to
// allow loopback for dev. This helper just answers "is this address
// unsafe to dial".
func ValidateHost(host string) error {
	_, err := LookupPublic(host)
	return err
}

// LookupPublic resolves host and returns the first IP that is safe
// for outbound webhook delivery (rejects every entry that fails
// isPublicIP). The returned IP is what the worker's DialContext pins
// for the actual TCP connect, closing the DNS-rebinding window
// between this lookup and the dial. Literal IPs short-circuit
// without a DNS query.
func LookupPublic(host string) (net.IP, error) {
	if host == "" {
		return nil, errors.New("url must have a host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return nil, errNonPublic
		}
		return ip, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, errors.New("could not resolve host: " + err.Error())
	}
	if len(ips) == 0 {
		return nil, errors.New("could not resolve host")
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return nil, errNonPublic
		}
	}
	// All answers are safe. Return the first; the dialer will bind
	// to it explicitly so a TTL-flip mid-request can't redirect us.
	return ips[0], nil
}

// IsLoopbackHost reports whether the URL host is one we should
// exempt from the SSRF gate for dev convenience. Recognises:
//   - the literal name "localhost"
//   - any IP literal that satisfies net.IP.IsLoopback (covers all
//     of 127.0.0.0/8 and ::1 without the brittle string-prefix check
//     that previously let `127.evil.com` slip past the gate)
func IsLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// isPublicIP reports whether the given IP is safe for outbound
// webhook delivery. Rejects every "internal" range a webhook might
// pivot through:
//
//   - Loopback (127/8, ::1)
//   - RFC 1918 private (10/8, 172.16/12, 192.168/16)
//   - IPv6 ULAs (fc00::/7) via IsPrivate
//   - Link-local (169.254/16, fe80::/10)
//   - Unspecified (0.0.0.0, ::), multicast, broadcast
//   - IPv4-mapped IPv6 (::ffff:127.0.0.1) — peer could otherwise
//     dodge a v4-only loopback check
//   - The well-known cloud metadata IPs (169.254.169.254 in v4)
//   - Carrier-grade NAT (100.64/10) and IETF benchmarking (198.18/15)
//     — both routable but reserved, attackers behind a CGNAT could
//     otherwise reach a sibling tenant.
func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if ip.IsPrivate() {
		return false
	}
	if v4 := ip.To4(); v4 != nil && (v4[0] == 127 || v4[0] == 0) {
		return false
	}
	if ip.Equal(net.IPv4(169, 254, 169, 254)) {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && (v4[1]&0xc0) == 64 {
			return false
		}
		if v4[0] == 198 && (v4[1] == 18 || v4[1] == 19) {
			return false
		}
	}
	return true
}
