package webhooks

import (
	"errors"
	"net"
)

// ValidateHost resolves the supplied host and reports an error when
// any answer falls in a non-public IP range. The check runs in two
// places:
//
//  1. Endpoint registration in internal/admin (validateWebhookURL)
//     — catches obvious mistakes before they're stored.
//  2. Worker delivery (delivery.go) — re-resolves on every send, so
//     a DNS-rebinding flip between registration and dispatch can't
//     pivot the worker into an internal service.
//
// Loopback hosts are intentionally NOT exempted here — the caller
// (validateWebhookURL or the delivery worker) decides whether to
// allow loopback for dev. This helper just answers "is this address
// unsafe to dial".
func ValidateHost(host string) error {
	if host == "" {
		return errors.New("url must have a host")
	}
	// Reject literal IPs that fall in the bad ranges before any DNS
	// resolution — net.LookupIP would still return them, but
	// short-circuiting saves a syscall on the obvious cases.
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return errors.New("url resolves to a non-public address (private / loopback / metadata)")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return errors.New("could not resolve host: " + err.Error())
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return errors.New("url resolves to a non-public address (private / loopback / metadata)")
		}
	}
	return nil
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
