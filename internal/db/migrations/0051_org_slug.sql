-- Per-org store URL slug.
--
-- Each organization gets a URL-safe slug (e.g. "mindrace") used as
-- the path prefix for its public-facing pages: /o/{slug}/store and
-- /o/{slug}/store/event/{id}. The bare /store remains a global
-- discovery surface; this column powers the per-org variant.
--
-- Backfill derives a slug from the existing name via zv_slugify().
-- A BEFORE INSERT trigger fills the column when callers omit it
-- (most test fixtures do `INSERT INTO organizations(name) VALUES …`
-- without a slug; updating every fixture would be churn). Production
-- callers always supply an explicit slug so they can show the
-- generated value in the response, but the trigger is the safety
-- net.

ALTER TABLE organizations ADD COLUMN slug TEXT;

-- zv_slugify: lowercase, ASCII-fold the obvious Nordic + Latin
-- diacritics, collapse anything not [a-z0-9] into a single hyphen,
-- trim leading / trailing hyphens, cap at 30. The Go-side helper
-- in internal/admin/orgs.go does the heavier Unicode work; this
-- SQL form is "good enough" for backfilling existing rows + the
-- trigger fallback. New rows that go through the Go code path get
-- the richer transliteration.
CREATE OR REPLACE FUNCTION zv_slugify(input TEXT) RETURNS TEXT
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    s TEXT;
BEGIN
    s := lower(coalesce(input, ''));
    s := translate(s, 'åäöéèêëüÿñçßæøœáàâãíìîïóòôõúùûýÿ',
                       'aaoeeeeuyncsaoeaaaaiiiooooouuuyy');
    s := regexp_replace(s, '[^a-z0-9]+', '-', 'g');
    s := trim(both '-' from s);
    IF length(s) = 0 THEN s := 'org'; END IF;
    IF length(s) > 30 THEN
        s := left(s, 30);
        s := trim(both '-' from s);
    END IF;
    RETURN s;
END;
$$;

-- Backfill: derive slug from name, append -2/-3/… on collision so
-- the UNIQUE constraint added below holds. Using ROW_NUMBER over
-- the slugified base gives a deterministic ordering by org id.
WITH numbered AS (
    SELECT id,
           zv_slugify(name) AS base,
           ROW_NUMBER() OVER (PARTITION BY zv_slugify(name) ORDER BY id) AS rn
    FROM organizations
)
UPDATE organizations o
   SET slug = CASE WHEN n.rn = 1 THEN n.base
                   ELSE n.base || '-' || n.rn::TEXT END
  FROM numbered n
 WHERE o.id = n.id;

ALTER TABLE organizations ADD CONSTRAINT organizations_slug_key UNIQUE (slug);
ALTER TABLE organizations ALTER COLUMN slug SET NOT NULL;

-- BEFORE INSERT trigger: when slug is NULL, derive it from name
-- with a uniqueness loop. Production handlers supply an explicit
-- slug so the trigger short-circuits via the early RETURN; the
-- trigger only fires for raw-INSERT test fixtures.
CREATE OR REPLACE FUNCTION zv_organizations_set_slug() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE
    base TEXT;
    candidate TEXT;
    n INTEGER;
BEGIN
    IF NEW.slug IS NOT NULL THEN
        RETURN NEW;
    END IF;
    base := zv_slugify(NEW.name);
    candidate := base;
    n := 2;
    WHILE EXISTS (SELECT 1 FROM organizations WHERE slug = candidate) LOOP
        candidate := base || '-' || n::TEXT;
        n := n + 1;
        EXIT WHEN n > 9999;
    END LOOP;
    NEW.slug := candidate;
    RETURN NEW;
END;
$$;

CREATE TRIGGER organizations_set_slug
    BEFORE INSERT ON organizations
    FOR EACH ROW
    EXECUTE FUNCTION zv_organizations_set_slug();
