-- Requests for a quote: a company planning a live event (a kickoff, a
-- team day) fills in a guided form on an organisation's public page,
-- and the organisation answers it with its own quote, outside
-- Zonventure. The organisation is the controller of what is in them.
--
-- requests_open shows the form (/o/{slug}/request); requests_intro is
-- the organisation's few lines at its top.
ALTER TABLE organizations
    ADD COLUMN requests_open  BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN requests_intro TEXT NOT NULL DEFAULT '';

-- form names the form a request came from; answers are its answers,
-- in that form's shape. The contact is in columns of its own. status
-- and notes are the organisation's, never shown to the requester.
-- A request is deleted a year after its last change (retention).
CREATE TABLE org_requests (
    id         BIGSERIAL PRIMARY KEY,
    org_id     BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    form       TEXT NOT NULL DEFAULT 'company' CHECK (form IN ('company')),
    answers    JSONB NOT NULL,
    name       TEXT NOT NULL,
    company    TEXT NOT NULL DEFAULT '',
    email      TEXT NOT NULL,
    phone      TEXT NOT NULL DEFAULT '',
    -- The language the requester wrote in; their confirmation follows it.
    lang       TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'answered', 'won', 'lost')),
    notes      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_org_requests_org ON org_requests(org_id, created_at DESC);
CREATE INDEX idx_org_requests_updated ON org_requests(updated_at);
