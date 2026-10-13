package admin

import (
	"context"
	"time"

	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/logx"
)

// deletionReminderBefore is how long before a scheduled account
// deletion SweepDeletionReminders mails the reminder.
const deletionReminderBefore = 24 * time.Hour

// sendDeletionMail tells userID when their account is deleted, with the
// way back: at once when they schedule it, and as the reminder the day
// before. Best-effort, through the outbox; an account without an
// address gets nothing.
func (h *Handlers) sendDeletionMail(ctx context.Context, userID int64, reminder bool) {
	var to, lang string
	var when time.Time
	if err := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE(TRIM(email), ''), COALESCE(language, ''), scheduled_deletion_at FROM users
		WHERE id = $1 AND scheduled_deletion_at IS NOT NULL`, userID).Scan(&to, &lang, &when); err != nil || to == "" {
		return
	}
	lang = emailtpl.Lang(lang)
	subject, c := emailtpl.AccountDeletion(lang, trailDate(lang, when), h.arrangeLink("#/settings/account"), reminder)
	kind := "account_deletion"
	if reminder {
		kind = "account_deletion_reminder"
	}
	if err := h.send(ctx, kind, emailtpl.Message([]string{to}, subject, emailtpl.Classic, emailtpl.Platform(), c)); err != nil {
		logx.Warn("admin.me.delete.mail_failed", "user_id", userID, "reminder", reminder, "err", err.Error())
	}
}

// SweepDeletionReminders mails the reminder to every account deleted
// within deletionReminderBefore that hasn't had it yet. Periodic.
func (h *Handlers) SweepDeletionReminders(ctx context.Context) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT id FROM users
		WHERE scheduled_deletion_at IS NOT NULL AND deletion_reminder_sent_at IS NULL
		  AND scheduled_deletion_at > now() AND scheduled_deletion_at <= now() + make_interval(secs => $1)
		LIMIT 100`, deletionReminderBefore.Seconds())
	if err != nil {
		logx.Error("admin.me.delete.reminder_sweep", "err", err.Error())
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		// Marked first: a mail that fails is not sent again and again.
		if _, err := h.DB.ExecContext(ctx, `UPDATE users SET deletion_reminder_sent_at = now() WHERE id = $1`, id); err != nil {
			logx.Error("admin.me.delete.reminder_sweep", "user_id", id, "err", err.Error())
			continue
		}
		h.sendDeletionMail(ctx, id, true)
	}
}
