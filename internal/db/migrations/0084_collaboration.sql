-- Working on an adventure together: conversations on its steps, who is
-- doing which step and how far it is, and what each author last saw.
-- None of it is in the graph: it is about the work, not what teams
-- play, so it stays out of saves, merges, versions and exports.

-- A thread is a conversation about one step — or one block of it — or,
-- with node '', about the adventure as a whole. A thread that asks the
-- assistant (an @assistant mention) is a task until it is resolved.
CREATE TABLE adventure_threads (
    id             BIGSERIAL PRIMARY KEY,
    adventure_id   BIGINT NOT NULL REFERENCES adventures(id) ON DELETE CASCADE,
    node           TEXT NOT NULL DEFAULT '',
    block_id       TEXT NOT NULL DEFAULT '',
    created_by     BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at    TIMESTAMPTZ,
    resolved_by    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    for_assistant  BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX idx_adventure_threads_adventure ON adventure_threads(adventure_id, node);
CREATE INDEX idx_adventure_threads_created_by ON adventure_threads(created_by) WHERE created_by IS NOT NULL;
CREATE INDEX idx_adventure_threads_resolved_by ON adventure_threads(resolved_by) WHERE resolved_by IS NOT NULL;

CREATE TABLE adventure_comments (
    id          BIGSERIAL PRIMARY KEY,
    thread_id   BIGINT NOT NULL REFERENCES adventure_threads(id) ON DELETE CASCADE,
    author_id   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    -- Written through an API token: by an assistant on the author's behalf.
    assistant   BOOLEAN NOT NULL DEFAULT false,
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    edited_at   TIMESTAMPTZ
);
CREATE INDEX idx_adventure_comments_thread ON adventure_comments(thread_id, created_at);
CREATE INDEX idx_adventure_comments_author ON adventure_comments(author_id) WHERE author_id IS NOT NULL;

-- Who a comment @mentions, for "mentions you" and the mail it sends.
CREATE TABLE adventure_mentions (
    comment_id  BIGINT NOT NULL REFERENCES adventure_comments(id) ON DELETE CASCADE,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (comment_id, user_id)
);
CREATE INDEX idx_adventure_mentions_user ON adventure_mentions(user_id);

-- A step's place in the work: how far along it is and who has it.
CREATE TABLE adventure_step_status (
    adventure_id  BIGINT NOT NULL REFERENCES adventures(id) ON DELETE CASCADE,
    node          TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT '' CHECK (status IN ('', 'idea', 'draft', 'review', 'ready')),
    owner_id      BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_by    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (adventure_id, node)
);
CREATE INDEX idx_adventure_step_status_owner ON adventure_step_status(owner_id) WHERE owner_id IS NOT NULL;
CREATE INDEX idx_adventure_step_status_updated_by ON adventure_step_status(updated_by) WHERE updated_by IS NOT NULL;

-- The draft as each author last saw it, so the designer can say what
-- changed since.
CREATE TABLE adventure_seen (
    adventure_id  BIGINT NOT NULL REFERENCES adventures(id) ON DELETE CASCADE,
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    graph         JSONB NOT NULL,
    seen_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (adventure_id, user_id)
);
CREATE INDEX idx_adventure_seen_user ON adventure_seen(user_id);
