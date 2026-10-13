package mailer

import (
	"context"

	"github.com/klahr/zonventure/internal/logx"
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
	attNames := make([]string, 0, len(m.Attachments))
	attBytes := 0
	for _, a := range m.Attachments {
		attNames = append(attNames, a.Filename)
		attBytes += len(a.Bytes)
	}
	logx.Info("mail.send",
		"to", m.To,
		"subject", m.Subject,
		"text_len", len(m.Text),
		"html_len", len(m.HTML),
		"reply_to", m.ReplyTo,
		"attachments", attNames,
		"attachments_bytes", attBytes,
	)
	// Without SMTP the log is the mailbox: the full body goes out at
	// INFO so links can be copied from it. The SMTP mailer never logs
	// bodies, at any level.
	logx.Info("mail.body", "to", m.To, "text", m.Text, "html", m.HTML)
	return nil
}
