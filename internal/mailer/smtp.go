package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// TLSMode picks how SMTPMailer brings up TLS. starttls is the modern
// default (port 587 + STARTTLS upgrade). tls dials TLS directly (port
// 465). none is plaintext — only useful for an in-network relay you
// fully trust.
type TLSMode string

const (
	TLSStartTLS TLSMode = "starttls"
	TLSImplicit TLSMode = "tls"
	TLSNone     TLSMode = "none"
)

// SMTPMailer connects via plain SMTP with optional STARTTLS or implicit
// TLS, authenticates with PLAIN if User is set, and sends a
// multipart/alternative message when both Text and HTML are present
// (otherwise the available body is sent as a single part).
type SMTPMailer struct {
	Host string
	Port string // "587" / "465" / "25"
	User string
	Pass string
	From string  // e.g. "Zonventure <noreply@example.com>"
	TLS  TLSMode // default TLSStartTLS when zero
	// DialTimeout caps how long we wait for the TCP/TLS handshake.
	// Defaults to 10 seconds; total Send() time is bounded by this
	// plus how long the server takes to accept DATA.
	DialTimeout time.Duration
}

// Send delivers msg via the configured SMTP server. Returns the first
// error encountered; partial deliveries (some recipients accepted,
// others rejected) result in a non-nil error and the caller should
// treat the whole send as failed for retry purposes.
func (s *SMTPMailer) Send(ctx context.Context, msg Message) error {
	if len(msg.To) == 0 {
		return ErrNoRecipients
	}
	if msg.Text == "" && msg.HTML == "" {
		return ErrEmptyBody
	}
	if s.From == "" {
		return errors.New("mailer: SMTPMailer.From is required")
	}
	if s.Host == "" {
		return errors.New("mailer: SMTPMailer.Host is required")
	}
	port := s.Port
	if port == "" {
		port = "587"
	}
	mode := s.TLS
	if mode == "" {
		mode = TLSStartTLS
	}
	timeout := s.DialTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	addr := net.JoinHostPort(s.Host, port)
	dialer := &net.Dialer{Timeout: timeout}
	tlsCfg := &tls.Config{ServerName: s.Host}

	var (
		c   *smtp.Client
		err error
	)
	switch mode {
	case TLSImplicit:
		conn, terr := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
		if terr != nil {
			return fmt.Errorf("smtp tls dial: %w", terr)
		}
		c, err = smtp.NewClient(conn, s.Host)
		if err != nil {
			conn.Close()
			return fmt.Errorf("smtp client: %w", err)
		}
	default: // starttls / none
		conn, derr := dialer.Dial("tcp", addr)
		if derr != nil {
			return fmt.Errorf("smtp dial: %w", derr)
		}
		c, err = smtp.NewClient(conn, s.Host)
		if err != nil {
			conn.Close()
			return fmt.Errorf("smtp client: %w", err)
		}
		if mode == TLSStartTLS {
			if err := c.StartTLS(tlsCfg); err != nil {
				c.Close()
				return fmt.Errorf("smtp starttls: %w", err)
			}
		}
	}
	defer c.Quit() //nolint:errcheck — best-effort close

	if s.User != "" {
		auth := smtp.PlainAuth("", s.User, s.Pass, s.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(extractAddr(s.From)); err != nil {
		return fmt.Errorf("smtp MAIL: %w", err)
	}
	for _, rcpt := range msg.To {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp RCPT %s: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	body, err := buildRFC5322(msg, s.From)
	if err != nil {
		w.Close()
		return err
	}
	if _, err := w.Write(body); err != nil {
		w.Close()
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp end body: %w", err)
	}
	_ = ctx // net/smtp doesn't accept a context; honour DialTimeout
	return nil
}

// extractAddr returns the bare addr-spec from a From header value
// like "Name <addr@example>" — needed for the SMTP envelope, which
// rejects display-name forms.
func extractAddr(from string) string {
	if i := strings.LastIndexByte(from, '<'); i >= 0 {
		if j := strings.IndexByte(from[i+1:], '>'); j >= 0 {
			return from[i+1 : i+1+j]
		}
	}
	return from
}

// buildRFC5322 assembles the message bytes to feed to DATA. Builds a
// multipart/alternative when both Text and HTML are present, a single
// text/plain or text/html part otherwise.
func buildRFC5322(msg Message, from string) ([]byte, error) {
	var b bytes.Buffer
	hdr := func(k, v string) {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	hdr("From", from)
	hdr("To", strings.Join(msg.To, ", "))
	if msg.ReplyTo != "" {
		hdr("Reply-To", msg.ReplyTo)
	}
	hdr("Subject", encodeSubject(msg.Subject))
	hdr("Date", time.Now().UTC().Format(time.RFC1123Z))
	hdr("Message-ID", newMessageID(from))
	hdr("MIME-Version", "1.0")

	switch {
	case msg.Text != "" && msg.HTML != "":
		boundary := "zv-" + randHex(12)
		hdr("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
		b.WriteString("\r\n")
		fmt.Fprintf(&b, "--%s\r\n", boundary)
		b.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n")
		b.WriteString(msg.Text)
		b.WriteString("\r\n")
		fmt.Fprintf(&b, "--%s\r\n", boundary)
		b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n")
		b.WriteString(msg.HTML)
		b.WriteString("\r\n")
		fmt.Fprintf(&b, "--%s--\r\n", boundary)
	case msg.HTML != "":
		hdr("Content-Type", `text/html; charset="UTF-8"`)
		b.WriteString("\r\n")
		b.WriteString(msg.HTML)
	default:
		hdr("Content-Type", `text/plain; charset="UTF-8"`)
		b.WriteString("\r\n")
		b.WriteString(msg.Text)
	}
	return b.Bytes(), nil
}

// encodeSubject MIME-encodes the subject if it contains any non-ASCII
// byte. Otherwise returns it unchanged so simple subjects stay
// human-readable in raw mail dumps.
func encodeSubject(s string) string {
	for _, r := range s {
		if r > 127 {
			return mime.QEncoding.Encode("UTF-8", s)
		}
	}
	return s
}

// newMessageID returns an RFC 2822 Message-ID. The local part is
// random hex; the domain is parsed out of the From address (everything
// after the last '@'), with "zv.local" as the fallback so mail-test
// reports show a sensible identifier even when From is malformed.
func newMessageID(from string) string {
	addr := extractAddr(from)
	domain := "zv.local"
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		domain = addr[i+1:]
	}
	return "<" + randHex(16) + "@" + domain + ">"
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand really should not fail; fall back to a fixed
		// fingerprint of the buffer address rather than panicking
		// inside a mail send.
		return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%p", &b)))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
