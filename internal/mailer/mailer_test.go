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

func TestBuildRFC5322_TextWithAttachment(t *testing.T) {
	body, err := buildRFC5322(Message{
		To:      []string{"a@b.example"},
		Subject: "Invite",
		Text:    "Hi! Calendar invite attached.",
		Attachments: []Attachment{{
			Filename:    "event-42-invite.ics",
			ContentType: "text/calendar; method=PUBLISH",
			Bytes:       []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"),
		}},
	}, "from@b.example")
	if err != nil {
		t.Fatalf("buildRFC5322: %v", err)
	}
	s := string(body)
	// Top-level wrapper
	if !strings.Contains(s, "Content-Type: multipart/mixed") {
		t.Fatalf("expected multipart/mixed wrapper, got: %s", s)
	}
	// Inner body
	if !strings.Contains(s, `Content-Type: text/plain; charset="UTF-8"`) {
		t.Fatalf("expected text/plain inner part, got: %s", s)
	}
	// Attachment metadata
	if !strings.Contains(s, "Content-Type: text/calendar; method=PUBLISH; name=") {
		t.Fatalf("expected text/calendar attachment, got: %s", s)
	}
	if !strings.Contains(s, `filename="event-42-invite.ics"`) {
		t.Fatalf("expected attachment filename header, got: %s", s)
	}
	if !strings.Contains(s, "Content-Transfer-Encoding: base64") {
		t.Fatalf("attachment must be base64-encoded, got: %s", s)
	}
	// Base64-encoded "BEGIN:VCALENDAR..." starts with "QkVHSU46VkNBTEVOREFS"
	if !strings.Contains(s, "QkVHSU46VkNBTEVOREFS") {
		t.Fatalf("attachment payload not present in base64, got: %s", s)
	}
}

func TestBuildRFC5322_MultipartWithAttachment(t *testing.T) {
	body, err := buildRFC5322(Message{
		To: []string{"a@b.example"}, Subject: "Invite",
		Text: "plain", HTML: "<p>html</p>",
		Attachments: []Attachment{{
			Filename:    "x.ics",
			ContentType: "text/calendar",
			Bytes:       []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"),
		}},
	}, "from@b.example")
	if err != nil {
		t.Fatalf("buildRFC5322: %v", err)
	}
	s := string(body)
	// multipart/mixed at the top
	if !strings.Contains(s, "Content-Type: multipart/mixed") {
		t.Fatalf("expected multipart/mixed wrapper, got: %s", s)
	}
	// multipart/alternative nested inside, holding text + html
	if !strings.Contains(s, "multipart/alternative") {
		t.Fatalf("expected nested multipart/alternative, got: %s", s)
	}
	if !strings.Contains(s, "plain") || !strings.Contains(s, "<p>html</p>") {
		t.Fatalf("both inner parts must appear, got: %s", s)
	}
	// Attachment present
	if !strings.Contains(s, `filename="x.ics"`) {
		t.Fatalf("expected attachment filename, got: %s", s)
	}
}

func TestBuildRFC5322_AttachmentLineWrapping(t *testing.T) {
	// 200 bytes of attachment → base64-encoded length is ~268 chars,
	// must split across multiple 76-column lines.
	bigPayload := make([]byte, 200)
	for i := range bigPayload {
		bigPayload[i] = byte('A' + (i % 26))
	}
	body, err := buildRFC5322(Message{
		To:   []string{"a@b.example"},
		Text: "hi",
		Attachments: []Attachment{{
			Filename:    "big.bin",
			ContentType: "application/octet-stream",
			Bytes:       bigPayload,
		}},
	}, "from@b.example")
	if err != nil {
		t.Fatalf("buildRFC5322: %v", err)
	}
	// Find the attachment section and verify each base64 line is
	// <= 76 octets.
	idx := strings.Index(string(body), "Content-Transfer-Encoding: base64\r\n")
	if idx < 0 {
		t.Fatalf("no base64 attachment section; body=%s", body)
	}
	// Skip past the headers' blank line into the payload.
	rest := string(body)[idx:]
	rest = rest[strings.Index(rest, "\r\n\r\n")+4:]
	// Payload runs until the next "--<boundary>" separator.
	end := strings.Index(rest, "\r\n--")
	if end < 0 {
		t.Fatalf("no boundary closer after payload")
	}
	for _, ln := range strings.Split(rest[:end], "\r\n") {
		if len(ln) > 76 {
			t.Errorf("base64 payload line > 76 octets (%d): %q", len(ln), ln)
		}
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
