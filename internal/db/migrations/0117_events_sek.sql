-- Self-guided events priced before 0116 still sold in EUR. The platform
-- sells in SEK only, so they do too; the amount stays as it was set.
UPDATE events SET ticket_currency = 'sek' WHERE ticket_currency <> 'sek';
