package player

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
	mailerpkg "github.com/klahr/zonventure/internal/mailer"
)

type recoverMailer struct{ sent []mailerpkg.Message }

func (m *recoverMailer) Send(_ context.Context, msg mailerpkg.Message) error {
	m.sent = append(m.sent, msg)
	return nil
}

// Anyone who knows a team's address can ask for its links, so asking
// mails the links the team already has instead of replacing them: the
// team stays signed in and its links keep working.
func TestRecoverLink_MailsCurrentLinks(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID, eventID, teamID, slotID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name)
		VALUES ('demo', $1, 'running', 'lt-recover', 'Hamnnatten')
		RETURNING id
	`, orgID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO teams(event_id, name, email, token, recap_token)
		VALUES ($1, 'Måsarna', 'Lag@Acme.test', 'team-tok', 'recap-tok') RETURNING id
	`, eventID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO player_slots(team_id, name, token) VALUES ($1, 'Anna', 'slot-tok-anna') RETURNING id`, teamID).Scan(&slotID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO slot_sessions(slot_id, token, expires_at) VALUES ($1, 'sess-anna', '2099-01-01T00:00:00Z')`, slotID); err != nil {
		t.Fatal(err)
	}

	mail := &recoverMailer{}
	h := &Handlers{DB: db, Mailer: mail, PublicURL: "https://zv.test"}
	w := httptest.NewRecorder()
	h.RecoverLink(w, httptest.NewRequest("POST", "/api/player/recover-link", strings.NewReader(`{"email":"lag@acme.test"}`)))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d", w.Code)
	}
	var token string
	var sessions int
	if err := db.QueryRow(`SELECT token, (SELECT COUNT(*) FROM slot_sessions WHERE slot_id = $1) FROM player_slots WHERE id = $1`, slotID).Scan(&token, &sessions); err != nil {
		t.Fatal(err)
	}
	if token != "slot-tok-anna" || sessions != 1 {
		t.Errorf("token = %q with %d sessions, want the old token and its session kept", token, sessions)
	}
	if len(mail.sent) != 1 || !strings.Contains(mail.sent[0].Text, "https://zv.test/play?t=slot-tok-anna") {
		t.Fatalf("mails = %+v, want one with the current link", mail.sent)
	}
	if strings.Contains(mail.sent[0].Text, "no longer work") {
		t.Errorf("mail says the old links are dead:\n%s", mail.sent[0].Text)
	}
}
