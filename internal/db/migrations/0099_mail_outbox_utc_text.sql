-- mail_outbox defaulted its TEXT timestamps to now()::text, which
-- carries fractional seconds and the session's offset, while the worker
-- compares them against UTC "YYYY-MM-DD HH:MM:SS" strings. Under a
-- non-UTC session TimeZone a fresh message was not due for hours. Use
-- now_text() like every other TEXT timestamp, and rewrite rows that
-- still carry an offset.
ALTER TABLE mail_outbox
    ALTER COLUMN next_attempt_at SET DEFAULT now_text(),
    ALTER COLUMN created_at SET DEFAULT now_text();

UPDATE mail_outbox
   SET next_attempt_at = to_char(next_attempt_at::timestamptz AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS')
 WHERE next_attempt_at ~ '[+-][0-9]{2}(:[0-9]{2})?$';

UPDATE mail_outbox
   SET created_at = to_char(created_at::timestamptz AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS')
 WHERE created_at ~ '[+-][0-9]{2}(:[0-9]{2})?$';
