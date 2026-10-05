-- An organisation's own themes (look.Look, without pictures or sounds,
-- which belong to one adventure): made once, picked in any of its
-- adventures, where they are copied into the graph as a saved theme.
CREATE TABLE org_themes (
    id                 BIGSERIAL PRIMARY KEY,
    org_id             BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    look               JSONB NOT NULL,
    created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);
