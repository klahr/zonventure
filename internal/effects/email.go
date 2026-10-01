package effects

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/klahr/zonventure/internal/emailtpl"
)

// Email is a queued email: the text as the step said it when the team
// passed, and the step's title for {{step}}. It goes to the team's
// contact address as it is at send time, so an arranger who fills in a
// missing address before the worker gets there still reaches the team.
type Email struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Step    string `json:"step,omitempty"`
}

// sendEmail renders the email for the team and hands it to the mailer.
// A team without a usable address is not retried: nothing a retry does
// will give it one, and the arranger sees why on the row.
func (w *Worker) sendEmail(ctx context.Context, m Email, q queued) error {
	if w.Mailer == nil {
		return permanent{errors.New("no mailer configured")}
	}
	to := strings.TrimSpace(q.teamEmail)
	if to == "" {
		return permanent{errors.New("team has no contact email")}
	}
	if _, err := mail.ParseAddress(to); err != nil {
		return permanent{fmt.Errorf("team contact email %q is not an address", to)}
	}
	step := m.Step
	if step == "" {
		step = q.node
	}
	r := strings.NewReplacer("{{team}}", q.team, "{{event}}", q.event, "{{step}}", step)
	// In the event's look, the organisation's unless the event has its own.
	lk := emailtpl.Lookup(ctx, w.DB, w.PublicURL, 0, q.eventID)
	subject, c := emailtpl.GameEmail(lk.Lang, r.Replace(m.Subject), r.Replace(m.Body), q.event, lk.Brand.Name)
	return w.Mailer.Send(ctx, emailtpl.Message([]string{to}, subject, lk.Theme, lk.Brand, c))
}
