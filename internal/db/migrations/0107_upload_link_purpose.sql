-- An upload link also sets the adventure's icon or an event's cover, and
-- opens as a page where the user picks the file: from a phone, where the
-- assistant can neither run curl nor send a photo's bytes.
ALTER TABLE media_upload_tickets
    ADD COLUMN purpose  TEXT NOT NULL DEFAULT 'media' CHECK (purpose IN ('media', 'icon', 'event_cover')),
    ADD COLUMN event_id BIGINT REFERENCES events(id) ON DELETE CASCADE;
