package mailer

import (
	"context"
	"strings"
	"testing"
)

func TestLogMailer_RejectsMissingTo(t *testing.T) {
	if err := (LogMailer{}).Send(context.Background(), Message{Text: "hi"}); err == nil {
		t.Fatalf("expected ErrNoRecipients, got nil")
	}
}

func TestLogMailer_RejectsEmptyBody(t *testing.T) {
	err := (LogMailer{}).Send(context.Background(), Message{To: []string{"a@b"}, Subject: "s"})
	if err == nil {
		t.Fatalf("expected ErrEmptyBody, got nil")
	}
}

func TestLogMailer_AcceptsValidMessage(t *testing.T) {
	err := (LogMailer{}).Send(context.Background(), Message{
		To: []string{"a@b"}, Subject: "s", Text: "body",
	})
	if err != nil {
		t.Fatalf("LogMailer.Send: %v", err)
	}
}

func TestBuildRFC5322_TextOnly(t *testing.T) {
	body, err := buildRFC5322(Message{
		To: []string{"a@b.example"}, Subject: "Hello", Text: "hi",
	}, "from@b.example")
	if err != nil {
		t.Fatalf("buildRFC5322: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "From: from@b.example") {
		t.Fatalf("missing From header: %q", s)
	}
	if !strings.Contains(s, "To: a@b.example") {
		t.Fatalf("missing To header: %q", s)
	}
	if !strings.Contains(s, "Subject: Hello") {
		t.Fatalf("missing Subject: %q", s)
	}
	if !strings.Contains(s, `Content-Type: text/plain; charset="UTF-8"`) {
		t.Fatalf("expected text/plain, got: %q", s)
	}
	if !strings.HasSuffix(s, "hi") {
		t.Fatalf("body must end with text content, got: %q", s)
	}
}

func TestBuildRFC5322_Multipart(t *testing.T) {
	body, err := buildRFC5322(Message{
		To: []string{"a@b.example"}, Subject: "x", Text: "plain", HTML: "<p>html</p>",
	}, "From <from@b.example>")
	if err != nil {
		t.Fatalf("buildRFC5322: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "multipart/alternative") {
		t.Fatalf("expected multipart/alternative, got: %q", s)
	}
	if !strings.Contains(s, "plain") || !strings.Contains(s, "<p>html</p>") {
		t.Fatalf("both parts must appear, got: %q", s)
	}
}

func TestEncodeSubject(t *testing.T) {
	if encodeSubject("Hello") != "Hello" {
		t.Fatalf("ASCII subject should be returned unchanged")
	}
	if got := encodeSubject("Välkommen"); !strings.HasPrefix(got, "=?") {
		t.Fatalf("non-ASCII subject should be MIME-encoded, got %q", got)
	}
}

func TestExtractAddr(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"a@b", "a@b"},
		{"Name <a@b>", "a@b"},
		{`"Quoted, Name" <a@b>`, "a@b"},
	}
	for _, c := range cases {
		if got := extractAddr(c.in); got != c.want {
			t.Fatalf("extractAddr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
