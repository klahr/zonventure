-- The brief: what an adventure is meant to be — theme, audience, length —
-- as agreed with its author. An assistant building the adventure by
-- conversation records it here and designs every step against it.
ALTER TABLE adventures ADD COLUMN brief JSONB NOT NULL DEFAULT '{}';
