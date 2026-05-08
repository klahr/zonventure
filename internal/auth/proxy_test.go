package auth

import (
	"net/http/httptest"
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	cases := []struct {
		name      string
		spec      string
		wantCount int
		wantErr   bool
	}{
		{name: "empty", spec: "", wantCount: 0},
		{name: "whitespace only", spec: "   ", wantCount: 0},
		{name: "single CIDR", spec: "10.0.0.0/8", wantCount: 1},
		{name: "bare IPv4 upgrades to /32", spec: "127.0.0.1", wantCount: 1},
		{name: "bare IPv6 upgrades to /128", spec: "::1", wantCount: 1},
		{name: "comma-separated mix",
			spec: "127.0.0.1, ::1, 10.0.0.0/8, 192.168.0.0/16", wantCount: 4},
		{name: "trailing comma OK", spec: "127.0.0.1,", wantCount: 1},
		{name: "garbage CIDR", spec: "not-a-cidr", wantErr: true},
		{name: "garbage in list", spec: "127.0.0.1,oops", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseTrustedProxies(c.spec)
			if c.wantErr {
				if err == nil {
					t.Fatalf("err = nil, want non-nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if len(got) != c.wantCount {
				t.Fatalf("len(prefixes) = %d, want %d", len(got), c.wantCount)
			}
		})
	}
}

func TestIsTrustedPeer(t *testing.T) {
	withTrustedProxies(t, "127.0.0.0/8", "10.0.0.0/8")

	t.Run("loopback IPv4 trusted", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "127.0.0.5:1234"
		if !IsTrustedPeer(r) {
			t.Fatal("127.0.0.5 should be trusted by 127.0.0.0/8")
		}
	})

	t.Run("private rfc1918 trusted", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "10.1.2.3:1234"
		if !IsTrustedPeer(r) {
			t.Fatal("10.1.2.3 should be trusted by 10.0.0.0/8")
		}
	})

	t.Run("public IP not trusted", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "203.0.113.7:1234"
		if IsTrustedPeer(r) {
			t.Fatal("203.0.113.7 should NOT be trusted")
		}
	})

	t.Run("nil request not trusted", func(t *testing.T) {
		if IsTrustedPeer(nil) {
			t.Fatal("nil request should not be trusted")
		}
	})

	t.Run("malformed remote addr not trusted", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "garbage"
		if IsTrustedPeer(r) {
			t.Fatal("malformed RemoteAddr should not be trusted")
		}
	})
}
