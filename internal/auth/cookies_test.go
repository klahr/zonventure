package auth

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"
)

func TestCookieSecure(t *testing.T) {
	cases := []struct {
		name  string
		setup func() (forceFlag bool, request *fakeReq)
		want  bool
	}{
		{
			name: "force flag wins on plain HTTP",
			setup: func() (bool, *fakeReq) {
				return true, &fakeReq{}
			},
			want: true,
		},
		{
			name: "no force, no TLS, no XFF: insecure",
			setup: func() (bool, *fakeReq) {
				return false, &fakeReq{}
			},
			want: false,
		},
		{
			name: "direct TLS connection: secure",
			setup: func() (bool, *fakeReq) {
				return false, &fakeReq{tls: true}
			},
			want: true,
		},
		{
			name: "X-Forwarded-Proto https: secure",
			setup: func() (bool, *fakeReq) {
				return false, &fakeReq{xff: "https"}
			},
			want: true,
		},
		{
			name: "X-Forwarded-Proto http: not secure",
			setup: func() (bool, *fakeReq) {
				return false, &fakeReq{xff: "http"}
			},
			want: false,
		},
		{
			name: "X-Forwarded-Proto HTTPS uppercase: secure",
			setup: func() (bool, *fakeReq) {
				return false, &fakeReq{xff: "HTTPS"}
			},
			want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			force, fr := c.setup()
			r := httptest.NewRequest("GET", "/", nil)
			if fr.tls {
				r.TLS = &tls.ConnectionState{}
			}
			if fr.xff != "" {
				r.Header.Set("X-Forwarded-Proto", fr.xff)
			}
			if got := CookieSecure(r, force); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

type fakeReq struct {
	tls bool
	xff string
}

func TestCookieSecure_NilRequest(t *testing.T) {
	if got := CookieSecure(nil, false); got {
		t.Fatalf("nil request without force should be insecure")
	}
	if got := CookieSecure(nil, true); !got {
		t.Fatalf("force should win even with nil request")
	}
}
