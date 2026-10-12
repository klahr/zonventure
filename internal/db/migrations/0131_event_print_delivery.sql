-- How an event delivers each of its adventure's print materials, by the
-- material's id, where it differs from the adventure's own: a letter
-- emailed for self-printing in summer and posted in winter.
ALTER TABLE events ADD COLUMN print_delivery JSONB NOT NULL DEFAULT '{}';
