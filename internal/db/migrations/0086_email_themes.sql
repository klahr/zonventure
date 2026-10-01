-- Emails in the organisation's look (internal/emailtpl): the theme an
-- organisation sends in, and one an event may send in instead. NULL is
-- "the default": classic for an organisation, the organisation's for
-- an event. The handlers validate the names.
ALTER TABLE organizations ADD COLUMN email_theme TEXT;
ALTER TABLE events ADD COLUMN email_theme TEXT;

-- Which notification emails a person wants. A kind without a row is on.
CREATE TABLE user_email_prefs (
    user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind     TEXT NOT NULL,
    enabled  BOOLEAN NOT NULL,
    PRIMARY KEY (user_id, kind)
);

-- What arrangers wrote to an event's teams, for the event's record.
CREATE TABLE event_emails (
    id          BIGSERIAL PRIMARY KEY,
    event_id    BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    subject     TEXT NOT NULL,
    body        TEXT NOT NULL,
    theme       TEXT NOT NULL,
    sent_by     BIGINT REFERENCES users(id) ON DELETE SET NULL,
    sent_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Teams reached, and teams left out for want of an address.
    sent        INT NOT NULL,
    skipped     INT NOT NULL
);
CREATE INDEX idx_event_emails_event ON event_emails(event_id, sent_at DESC);
CREATE INDEX idx_event_emails_sent_by ON event_emails(sent_by) WHERE sent_by IS NOT NULL;
