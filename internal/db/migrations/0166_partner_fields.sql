-- Gift box partners are each organisation's own, not two built in. A
-- partner names itself and the codes its boxes carry: fields is one or
-- two {label: {lang: text}, example, min, max, digits}, in the order the
-- buyer types them (two: box_number then security_code; one:
-- security_code). hint is {lang: text} under the codes.
ALTER TABLE partners ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE partners ADD COLUMN fields JSONB NOT NULL DEFAULT '[]';
ALTER TABLE partners ADD COLUMN hint JSONB NOT NULL DEFAULT '{}';

UPDATE partners SET name = 'Live it',
    fields = '[{"label": {"en": "Box number", "sv": "Boxnummer"}, "example": "", "min": 1, "max": 20, "digits": true},
               {"label": {"en": "Security code", "sv": "Säkerhetskod"}, "example": "", "min": 1, "max": 32, "digits": false}]',
    hint = '{"en": "Both are printed on the card inside the box.", "sv": "Båda står på kortet i boxen."}'
WHERE kind = 'liveit';
UPDATE partners SET name = 'TrueStory',
    fields = '[{"label": {"en": "Experience code", "sv": "Upplevelsekod"}, "example": "", "min": 9, "max": 9, "digits": false}]',
    hint = '{"en": "The 9-character code on your gift card.", "sv": "Koden med 9 tecken på ditt presentkort."}'
WHERE kind = 'truestory';

ALTER TABLE partners DROP COLUMN kind;
ALTER TABLE partners ADD CONSTRAINT partners_org_id_name_key UNIQUE (org_id, name);

-- A code is redeemable once per partner. A rejected or cancelled
-- request frees it.
DROP INDEX idx_partner_redemptions_code;
CREATE UNIQUE INDEX idx_partner_redemptions_code
    ON partner_redemptions(partner_id, box_number, security_code)
    WHERE state IN ('pending', 'approved');
ALTER TABLE partner_redemptions DROP COLUMN kind;
