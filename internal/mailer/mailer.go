// Package mailer is the transactional email layer. Future signup
// verification, forgot-password, and account-settings flows all run
// through Mailer.Send so the implementation can swap (SMTP today,
// SES/Postmark API tomorrow) without rewiring the call sites.
//
// In dev (no ZV_SMTP_HOST set) the LogMailer logs the message body
// instead of dialling out, so a fresh checkout still produces something
// useful when an email-bound flow runs.
package mailer

import (
	"context"
	"errors"
)

// Message is one outbound email. To holds one or more recipients (each
// receives the same message; if you want personalisation, send N
// messages). Either Text or HTML may be empty, but at least one must
// be non-empty — otherwise SMTPMailer.Send refuses with an error.
type Message struct {
	To      []string
	Subject string
	Text    string
	HTML    string
	ReplyTo string
}

// Mailer is the abstraction the rest of the codebase calls. Send is
// expected to be synchronous and may take a few seconds (TLS handshake
// + send). Callers running in a request handler should consider
// firing-and-forgetting in a goroutine when latency matters.
type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// ErrEmptyBody is returned when both Text and HTML are empty.
var ErrEmptyBody = errors.New("mailer: at least one of Text or HTML must be set")

// ErrNoRecipients is returned when To is empty.
var ErrNoRecipients = errors.New("mailer: no recipients")
