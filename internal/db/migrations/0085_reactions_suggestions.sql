-- Lighter and politer ways to work together.
--
-- A suggestion is a thread that proposes a change to its step: the step
-- as the suggester found it (base) and as they would have it (proposed).
-- Accepting merges the proposal into the step as it is by then; either
-- way the thread is resolved.
ALTER TABLE adventure_threads
    ADD COLUMN suggestion JSONB,
    ADD COLUMN suggestion_state TEXT NOT NULL DEFAULT ''
        CHECK (suggestion_state IN ('', 'open', 'accepted', 'rejected'));

-- A reaction: one person's emoji on a comment ("comment:<id>").
CREATE TABLE adventure_reactions (
    adventure_id  BIGINT NOT NULL REFERENCES adventures(id) ON DELETE CASCADE,
    target        TEXT NOT NULL,
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    emoji         TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (adventure_id, target, user_id, emoji)
);
CREATE INDEX idx_adventure_reactions_user ON adventure_reactions(user_id);
