package mailer

import (
	"context"

	"git.sr.ht/~klahr/zv/internal/logx"
)

// LogMailer writes a structured log entry instead of sending email. The
// fallback when ZV_SMTP_HOST is unset — keeps a dev environment usable
// without forcing an SMTP server. Operators see what would have been
// sent and can act manually (e.g. copy a verification link out of the
// log) until they wire up a real provider.
type LogMailer struct{}

func (LogMailer) Send(_ context.Context, m Message) error {
	if len(m.To) == 0 {
		return ErrNoRecipients
	}
	if m.Text == "" && m.HTML == "" {
		return ErrEmptyBody
	}
	logx.Info("mail.send",
		"to", m.To,
		"subject", m.Subject,
		"text_len", len(m.Text),
		"html_len", len(m.HTML),
		"reply_to", m.ReplyTo,
	)
	// A second entry at DEBUG carries the full body so devs can
	// hand-copy verification links from the log when no real SMTP is
	// configured. Keeps INFO clean for production.
	logx.Debug("mail.body", "to", m.To, "text", m.Text, "html", m.HTML)
	return nil
}
