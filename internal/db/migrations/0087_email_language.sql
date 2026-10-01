-- The language emails are written in (internal/emailtpl). An event's
-- emails follow its adventure's language; these cover the rest:
--
--   users.language          the language a person uses the console in,
--                           for the notifications they get
--   organizations.language  for what an organisation sends outside any
--                           event, such as box redemptions
--
-- NULL is English. The handlers keep both to the languages emails exist in.
ALTER TABLE users ADD COLUMN language TEXT;
ALTER TABLE organizations ADD COLUMN language TEXT;
