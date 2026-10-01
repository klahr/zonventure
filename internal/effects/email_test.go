package effects

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/mailer"
)

// fakeMailer records what it is asked to send, failing with err if set.
type fakeMailer struct {
	sent []mailer.Message
	err  error
}

func (f *fakeMailer) Send(_ context.Context, m mailer.Message) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	return nil
}

func TestEmailDelivered(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	_, _, id := queueEffect(t, db, "email", Email{Subject: "{{team}} rang the bell", Body: "In {{event}}, at {{step}}.", Step: "The bell"}, "captain@example.com")

	m := &fakeMailer{}
	w := &Worker{DB: db, Mailer: m}
	if sent, err := w.Once(context.Background()); !sent || err != nil {
		t.Fatalf("once = %v, %v", sent, err)
	}
	if len(m.sent) != 1 {
		t.Fatalf("sent %d emails, want 1", len(m.sent))
	}
	got := m.sent[0]
	if len(got.To) != 1 || got.To[0] != "captain@example.com" || got.Subject != "Owls rang the bell" || !strings.HasPrefix(got.Text, "In fx, at The bell.") {
		t.Errorf("email = %+v", got)
	}
	// In the organisation's look: HTML beside the text.
	if !strings.Contains(got.HTML, "In fx, at The bell.") || !strings.Contains(got.HTML, "<html") {
		t.Errorf("no themed HTML: %q", got.HTML)
	}
	var delivered sql.NullTime
	must(t, db.QueryRow(`SELECT delivered_at FROM graph_effect_outbox WHERE id = $1`, id).Scan(&delivered))
	if !delivered.Valid {
		t.Error("a sent email marks the row delivered")
	}
	if sent, _ := w.Once(context.Background()); sent {
		t.Error("a delivered email is not sent again")
	}
}

// A team without an address will not gain one by waiting, so the email
// is given up on at once instead of retried for a day.
func TestEmailWithoutAddressGivesUp(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	_, _, id := queueEffect(t, db, "email", Email{Subject: "s", Body: "b"}, "")

	m := &fakeMailer{}
	w := &Worker{DB: db, Mailer: m}
	if _, err := w.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	var dead sql.NullTime
	var why sql.NullString
	must(t, db.QueryRow(`SELECT dead_at, last_error FROM graph_effect_outbox WHERE id = $1`, id).Scan(&dead, &why))
	if len(m.sent) != 0 || !dead.Valid || why.String != "team has no contact email" {
		t.Errorf("sent %d, dead %v, reason %q", len(m.sent), dead.Valid, why.String)
	}
}

func TestEmailMailerFailureRetries(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	_, _, id := queueEffect(t, db, "email", Email{Subject: "s", Body: "b"}, "captain@example.com")

	w := &Worker{DB: db, Mailer: &fakeMailer{err: errors.New("smtp: connection refused")}}
	if _, err := w.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	var attempts int
	var dead, later bool
	var why sql.NullString
	must(t, db.QueryRow(`SELECT attempts, dead_at IS NOT NULL, next_attempt_at > now() + interval '30 seconds', last_error FROM graph_effect_outbox WHERE id = $1`, id).Scan(&attempts, &dead, &later, &why))
	if attempts != 1 || dead || !later || why.String != "smtp: connection refused" {
		t.Errorf("after a mailer failure: attempts %d, dead %v, retried later %v, reason %q", attempts, dead, later, why.String)
	}
}
