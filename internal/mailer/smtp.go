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
	fromAddr := extractAddr(s.From)
	if err := validateAddrSafe(fromAddr); err != nil {
		return fmt.Errorf("smtp MAIL: %w", err)
	}
	if err := c.Mail(fromAddr); err != nil {
		return fmt.Errorf("smtp MAIL: %w", err)
	}
	for _, rcpt := range msg.To {
		if err := validateAddrSafe(rcpt); err != nil {
			return fmt.Errorf("smtp RCPT: %w", err)
		}
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

// buildRFC5322 assembles the message bytes to feed to DATA.
//
// The output structure depends on what the message carries:
//
//   - text only        → single text/plain part
//   - HTML only        → single text/html part
//   - text + HTML      → multipart/alternative (text first, HTML second)
//   - any + attachment → multipart/mixed wrapping the body part(s)
//                        described above, plus one base64 attachment
//                        part per Message.Attachments entry.
//
// The nested multipart/alternative inside multipart/mixed is the
// shape every major mail client (Gmail, Outlook, Apple Mail) reads
// without a fight. Putting the alternative *outside* the mixed (or
// no mixed at all) hides attachments on Outlook in particular.
func buildRFC5322(msg Message, from string) ([]byte, error) {
	var b bytes.Buffer
	hdr := func(k, v string) {
		// CRLF-strip every header value defensively. Any caller that
		// hands us a value with an embedded newline is either a bug
		// or an attacker injecting fake headers (BCC, From override,
		// MIME confusion). Belt-and-braces alongside the per-recipient
		// validateAddrSafe — header values can come from places other
		// than addresses (Subject, Reply-To, the From label).
		fmt.Fprintf(&b, "%s: %s\r\n", k, sanitizeHeader(v))
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

	body := buildBody(msg)
	if len(msg.Attachments) == 0 {
		// No attachments: emit the body inline with whatever the
		// body builder picked.
		hdr("Content-Type", body.contentType)
		b.WriteString("\r\n")
		b.Write(body.payload)
		return b.Bytes(), nil
	}
	// One or more attachments: wrap in multipart/mixed.
	mixed := "zvmix-" + randHex(12)
	hdr("Content-Type", `multipart/mixed; boundary="`+mixed+`"`)
	b.WriteString("\r\n")
	fmt.Fprintf(&b, "--%s\r\n", mixed)
	fmt.Fprintf(&b, "Content-Type: %s\r\n\r\n", body.contentType)
	b.Write(body.payload)
	b.WriteString("\r\n")
	for _, att := range msg.Attachments {
		ct := strings.TrimSpace(att.ContentType)
		if ct == "" {
			ct = "application/octet-stream"
		}
		fname := strings.TrimSpace(att.Filename)
		if fname == "" {
			fname = "attachment"
		}
		fmt.Fprintf(&b, "--%s\r\n", mixed)
		fmt.Fprintf(&b, "Content-Type: %s; name=%q\r\n", ct, fname)
		b.WriteString("Content-Transfer-Encoding: base64\r\n")
		fmt.Fprintf(&b, "Content-Disposition: attachment; filename=%q\r\n\r\n", fname)
		b.WriteString(base64Wrap(att.Bytes))
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", mixed)
	return b.Bytes(), nil
}

// bodyPart is the typed-payload pair for the message body — keeps
// buildBody returning enough information that the wrapping code can
// emit the right Content-Type header without re-deriving it.
type bodyPart struct {
	contentType string
	payload     []byte
}

// buildBody renders the textual body the same way the original
// switch did, but as a value rather than appended directly to the
// outer buffer — so the multipart/mixed wrapper can splice the
// result in as one section.
func buildBody(msg Message) bodyPart {
	switch {
	case msg.Text != "" && msg.HTML != "":
		boundary := "zvalt-" + randHex(12)
		var b bytes.Buffer
		fmt.Fprintf(&b, "--%s\r\n", boundary)
		b.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n")
		b.WriteString(msg.Text)
		b.WriteString("\r\n")
		fmt.Fprintf(&b, "--%s\r\n", boundary)
		b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n")
		b.WriteString(msg.HTML)
		b.WriteString("\r\n")
		fmt.Fprintf(&b, "--%s--\r\n", boundary)
		return bodyPart{
			contentType: `multipart/alternative; boundary="` + boundary + `"`,
			payload:     b.Bytes(),
		}
	case msg.HTML != "":
		return bodyPart{
			contentType: `text/html; charset="UTF-8"`,
			payload:     []byte(msg.HTML),
		}
	default:
		return bodyPart{
			contentType: `text/plain; charset="UTF-8"`,
			payload:     []byte(msg.Text),
		}
	}
}

// base64Wrap encodes bytes with standard base64 and inserts CRLF
// every 76 columns — RFC 2045 § 6.8 mandates a max line length of
// 76 octets for base64. Strict mail servers (Postfix in some
// configurations, exchange) reject longer lines.
func base64Wrap(b []byte) string {
	enc := base64.StdEncoding.EncodeToString(b)
	const wrap = 76
	if len(enc) <= wrap {
		return enc
	}
	var out strings.Builder
	out.Grow(len(enc) + (len(enc)/wrap)*2)
	for i := 0; i < len(enc); i += wrap {
		end := i + wrap
		if end > len(enc) {
			end = len(enc)
		}
		out.WriteString(enc[i:end])
		if end < len(enc) {
			out.WriteString("\r\n")
		}
	}
	return out.String()
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

// sanitizeHeader strips any control byte (0x00..0x1F, 0x7F) from a
// header value to prevent CRLF / NUL injection. RFC 5322 § 2.2 forbids
// CR / LF inside a field-body except for folding whitespace, which we
// don't generate. An attacker who controls part of a Subject or
// Reply-To could otherwise inject "BCC: victim@..." or a forged From.
// Returns the cleaned string; callers that wanted to reject rather
// than sanitise should validate at their own boundary first.
func sanitizeHeader(v string) string {
	var b strings.Builder
	b.Grow(len(v))
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c < 0x20 || c == 0x7F {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// validateAddrSafe rejects an envelope address that contains bytes the
// SMTP / RFC 5321 ABNF would never accept — most importantly CR / LF /
// NUL, which an attacker would use to smuggle "RCPT TO:<victim>" lines
// past net/smtp into a server's command stream. We only check for the
// catastrophic injection bytes here; the SMTP server itself is the
// authority on whether the address is otherwise well-formed.
func validateAddrSafe(addr string) error {
	if addr == "" {
		return errors.New("empty address")
	}
	for i := 0; i < len(addr); i++ {
		c := addr[i]
		if c <= 0x20 || c == 0x7F || c == ',' {
			return fmt.Errorf("address contains forbidden byte 0x%02x", c)
		}
	}
	return nil
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
