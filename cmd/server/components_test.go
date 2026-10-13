package main

import (
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/mailer"
)

// A half-configured Stripe says which variable is missing on Settings → System.
func TestDescribeComponents_StripeHalfConfigured(t *testing.T) {
	notes := func(key, hook string) string {
		for _, c := range describeComponents(mailer.LogMailer{}, key, hook, "", "", false) {
			if c.Name == "payments" {
				return c.Notes
			}
		}
		t.Fatal("no payments component")
		return ""
	}
	if n := notes("sk_live_x", ""); !strings.Contains(n, "ZV_STRIPE_WEBHOOK_SECRET missing") {
		t.Errorf("secret key only: notes = %q", n)
	}
	if n := notes("", "whsec_x"); !strings.Contains(n, "ZV_STRIPE_SECRET_KEY missing") {
		t.Errorf("webhook secret only: notes = %q", n)
	}
	if n := notes("sk_live_x", "whsec_x"); n != "" {
		t.Errorf("both set: notes = %q", n)
	}
}

func TestOIDCRedirectMismatch(t *testing.T) {
	for _, c := range []struct {
		redirect, public string
		want             bool
	}{
		{"https://zv.example/auth/oidc/callback", "https://zv.example", false},
		{"https://zv.example/auth/oidc/callback", "", false},
		{"http://localhost:9999/wrong/callback", "http://localhost:8097", true},
		{"http://localhost:9999/auth/oidc/callback", "http://localhost:8097", true},
		{"https://zv.example/wrong/callback", "https://zv.example", true},
	} {
		if got := oidcRedirectMismatch(c.redirect, c.public); got != c.want {
			t.Errorf("oidcRedirectMismatch(%q, %q) = %v, want %v", c.redirect, c.public, got, c.want)
		}
	}
}

func TestPlainHTTPPublicURL(t *testing.T) {
	for u, want := range map[string]bool{
		"":                          false,
		"https://zv.example.com":    false,
		"http://localhost:8096":     false,
		"http://127.0.0.1:8080":     false,
		"http://[::1]:8080":         false,
		"http://zv.example.com":     true,
		"HTTP://192.168.1.10:8080/": true,
	} {
		if got := plainHTTPPublicURL(u); got != want {
			t.Errorf("plainHTTPPublicURL(%q) = %v, want %v", u, got, want)
		}
	}
}
