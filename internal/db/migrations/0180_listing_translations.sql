-- A self-guided event's store listing in the other languages its
-- adventure is played in (internal/listing): its name and blurb by
-- language tag, {"en": {"name": "…", "blurb": "…"}}. The event keeps the
-- draft, written in the console; the approved copy keeps what a
-- listing reviewer approved, as it keeps the main-language name and
-- blurb (listing review, 0123). The adventure's own words on a listing
-- are translations in the adventure version it plays.

ALTER TABLE events ADD COLUMN listing_translations JSONB NOT NULL DEFAULT '{}';
ALTER TABLE event_listings ADD COLUMN translations JSONB NOT NULL DEFAULT '{}';
