-- An organisation's own address (adventures.theirbrand.se): its store,
-- tickets and games served there in its brand. custom_domain_until is
-- the entitlement a platform admin grants; NULL is off. Past it, a grace
-- of 30 days keeps the domain serving before it redirects back.
ALTER TABLE organizations ADD COLUMN custom_domain_until DATE;

-- How an organisation presents itself on its own domain: the name it
-- goes by there ('' is its name), its title typeface (a look font key,
-- '' is the site's), and links to its own terms and privacy policy,
-- shown beside the platform's.
ALTER TABLE organizations ADD COLUMN brand_display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE organizations ADD COLUMN brand_font TEXT NOT NULL DEFAULT '';
ALTER TABLE organizations ADD COLUMN brand_terms_url TEXT NOT NULL DEFAULT '';
ALTER TABLE organizations ADD COLUMN brand_privacy_url TEXT NOT NULL DEFAULT '';

-- One domain per organisation. target is the label of the CNAME value
-- it was told to use (<target>.<ZV_DOMAIN_CNAME_SUFFIX>), fixed when the
-- domain is claimed so a later slug change doesn't break the record.
-- status: waiting (nothing found), wrong (found, but not right: problem
-- names the likely fix, seen what DNS returned), certificate (the record
-- is right, HTTPS is not answering yet), live, removed (given up: kept
-- so links already sent with it redirect back, until another claims it).
CREATE TABLE org_domains (
    org_id         BIGINT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    hostname       TEXT NOT NULL UNIQUE,
    target         TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'waiting' CHECK (status IN ('waiting', 'wrong', 'certificate', 'live', 'removed')),
    problem        TEXT NOT NULL DEFAULT '',
    seen           TEXT NOT NULL DEFAULT '',
    checked_at     TIMESTAMPTZ,
    live_at        TIMESTAMPTZ,
    down_since     TIMESTAMPTZ,
    down_notified  BOOLEAN NOT NULL DEFAULT false,
    -- The entitlement end the "your domain ends" email was sent for.
    ending_notified_for DATE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The From name of a queued email: an organisation on its own domain
-- sends under its own name.
ALTER TABLE mail_outbox ADD COLUMN from_name TEXT NOT NULL DEFAULT '';
