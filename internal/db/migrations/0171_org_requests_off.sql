-- What an organisation doesn't offer through its request form: its
-- keys (an adventure kind, the game master, an extra, a language) are
-- left out of the form, and a request asking for one is refused. Empty,
-- the default, offers everything.
ALTER TABLE organizations ADD COLUMN requests_off TEXT[] NOT NULL DEFAULT '{}';
