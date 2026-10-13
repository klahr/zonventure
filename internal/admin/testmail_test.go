package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/mailer"
)

type sentMailer struct{}

func (sentMailer) Send(context.Context, mailer.Message) error { return nil }

type failingMailer struct{ err error }

func (f failingMailer) Send(context.Context, mailer.Message) error { return f.err }

// The test button exists to show why mail fails, so the cause reaches the admin.
func TestSendTestEmail_ShowsCause(t *testing.T) {
	h := &Handlers{Mailer: failingMailer{errors.New("smtp dial: no such host")}}
	r := httptest.NewRequest(http.MethodPost, "/api/admin/send-test-email", strings.NewReader(`{"to":"ops@example.org"}`))
	w := httptest.NewRecorder()
	h.SendTestEmail(w, r)
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "smtp dial: no such host") {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}

// Without SMTP the test mail only reaches the log, and the reply says so.
func TestSendTestEmail_SaysWhenOnlyLogged(t *testing.T) {
	for name, tc := range map[string]struct {
		m    mailer.Mailer
		want string
	}{
		"log mailer": {mailer.LogMailer{}, "logged"},
		"smtp":       {sentMailer{}, "sent"},
	} {
		t.Run(name, func(t *testing.T) {
			h := &Handlers{Mailer: tc.m}
			r := httptest.NewRequest(http.MethodPost, "/api/admin/send-test-email", strings.NewReader(`{"to":"it@example.org"}`))
			w := httptest.NewRecorder()
			h.SendTestEmail(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
			var got map[string]string
			_ = json.Unmarshal(w.Body.Bytes(), &got)
			if got["status"] != tc.want {
				t.Fatalf("status %q, want %q", got["status"], tc.want)
			}
		})
	}
}
