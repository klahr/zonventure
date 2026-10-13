-- When the "your account is deleted tomorrow" reminder went out for the
-- current scheduled_deletion_at; cleared whenever deletion is scheduled
-- again or cancelled.
ALTER TABLE users ADD COLUMN deletion_reminder_sent_at TIMESTAMPTZ;
