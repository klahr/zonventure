package domains

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"sort"
	"strings"
	"time"
)

// Problems a check recognises, each with a fix the console explains.
const (
	ProblemNone        = ""
	ProblemDoubled     = "doubled"      // the name was typed as the full address
	ProblemAddress     = "address"      // an A/AAAA record instead of, or beside, the CNAME
	ProblemCloudflare  = "cloudflare"   // Cloudflare's proxy is on
	ProblemOtherTarget = "other_target" // a CNAME, to another value
	ProblemCertificate = "certificate"  // the record is right, HTTPS isn't answering
)

// Resolver is the DNS a check asks; net.DefaultResolver in production.
type Resolver interface {
	LookupCNAME(ctx context.Context, host string) (string, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// Result is what a check found.
type Result struct {
	Status  string
	Problem string
	// Seen is what DNS returned, for the console to show next to what
	// it expects.
	Seen string
}

// Check looks up host and says whether it points at target (the full
// CNAME value). It doesn't probe HTTPS; see Probe.
func Check(ctx context.Context, r Resolver, host, target string) Result {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	target = strings.TrimSuffix(strings.ToLower(target), ".")
	cname, cerr := r.LookupCNAME(ctx, host)
	cname = strings.TrimSuffix(strings.ToLower(cname), ".")
	addrs, herr := r.LookupHost(ctx, host)
	if cerr == nil && cname == target {
		return Result{Status: StatusCertificate, Seen: "CNAME " + cname}
	}
	if cerr == nil && cname != "" && cname != host {
		if s := Suffix(); s != "" && strings.HasSuffix(cname, "."+s) {
			return Result{Status: StatusWrong, Problem: ProblemOtherTarget, Seen: "CNAME " + cname}
		}
		if len(addrs) > 0 && cloudflare(addrs) {
			return Result{Status: StatusWrong, Problem: ProblemCloudflare, Seen: "CNAME " + cname}
		}
		return Result{Status: StatusWrong, Problem: ProblemOtherTarget, Seen: "CNAME " + cname}
	}
	if herr == nil && len(addrs) > 0 {
		sort.Strings(addrs)
		seen := strings.Join(addrs, ", ")
		if cloudflare(addrs) {
			return Result{Status: StatusWrong, Problem: ProblemCloudflare, Seen: seen}
		}
		return Result{Status: StatusWrong, Problem: ProblemAddress, Seen: seen}
	}
	// Nothing at host. The name typed as the full address lands one
	// registered domain deeper: adventures.theirbrand.se.theirbrand.se.
	if apex := Apex(host); apex != "" {
		doubled := host + "." + apex
		if c, err := r.LookupCNAME(ctx, doubled); err == nil && strings.TrimSuffix(strings.ToLower(c), ".") == target {
			return Result{Status: StatusWrong, Problem: ProblemDoubled, Seen: "CNAME at " + doubled}
		}
	}
	return Result{Status: StatusWaiting}
}

// cloudflareRanges are Cloudflare's published proxy ranges
// (cloudflare.com/ips): a name answering from them has the orange cloud on.
var cloudflareRanges = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
		"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
		"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
		"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
		"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
		"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func cloudflare(addrs []string) bool {
	for _, a := range addrs {
		ip, err := netip.ParseAddr(a)
		if err != nil {
			continue
		}
		for _, p := range cloudflareRanges {
			if p.Contains(ip.Unmap()) {
				return true
			}
		}
	}
	return false
}

// ProbePath answers every serving custom domain with its hostname, so
// a probe knows it reached this instance and not a parked page.
const ProbePath = "/.well-known/zonventure-domain"

// Probe asks https://host for ProbePath. The first request is what makes
// the TLS proxy fetch a certificate, so a fresh domain may take a few
// probes. ZV_DOMAIN_PROBE_SCHEME=http probes without TLS, for a local
// instance with no proxy in front. ZV_DOMAIN_PROBE_ADDR (host:port)
// dials the TLS proxy there instead of host's address, for a server
// that can't reach its own public IP; the name still goes in the TLS
// handshake and the Host header.
func Probe(ctx context.Context, host string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	scheme := "https"
	if os.Getenv("ZV_DOMAIN_PROBE_SCHEME") == "http" {
		scheme = "http"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+host+ProbePath, nil)
	if err != nil {
		return err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	dial := dialer.DialContext
	if addr := os.Getenv("ZV_DOMAIN_PROBE_ADDR"); addr != "" {
		dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		}
	}
	c := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
			DialContext:       dial,
			DisableKeepAlives: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(b)) != host {
		return errors.New("the address answers, but not from this instance")
	}
	return nil
}
