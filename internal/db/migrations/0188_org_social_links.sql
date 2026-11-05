-- An organisation's profiles on social networks, by network:
-- {"instagram": "https://www.instagram.com/…", …} (internal/social).
-- The site's own organisation's show in every page's footer.
ALTER TABLE organizations ADD COLUMN social_links JSONB NOT NULL DEFAULT '{}';
