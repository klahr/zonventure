-- What the store says about an adventure is the adventure's description,
-- written and translated with the adventure (graph meta:description),
-- no longer each event's public_blurb. The store's current text wins:
-- an adventure sold in the store takes its listing's approved blurb and
-- its translations, one not sold takes an event's blurb only when it has
-- no description of its own. The events' blurbs are then emptied; the
-- columns stay until no reader is left on a running server.

-- One text per adventure: a listed event's approved copy first, then an
-- event sold in the store, the newest first.
CREATE TEMP TABLE zv_pitch AS
SELECT DISTINCT ON (a.id)
       a.id AS adventure_id,
       btrim(COALESCE(l.blurb, e.public_blurb)) AS blurb,
       COALESCE(l.translations, e.translations) AS tr,
       l.event_id IS NOT NULL AS listed
  FROM events e
  JOIN adventures a ON a.org_id = e.org_id AND a.slug = e.adventure_slug
  LEFT JOIN event_listings l ON l.event_id = e.id
 WHERE btrim(COALESCE(l.blurb, e.public_blurb)) <> ''
 ORDER BY a.id, (l.event_id IS NOT NULL) DESC, e.public_for_sale DESC, e.id DESC;

DELETE FROM zv_pitch p
 USING adventures a
 WHERE a.id = p.adventure_id AND NOT p.listed AND btrim(a.description) <> '';

UPDATE adventures a SET description = left(p.blurb, 4000)
  FROM zv_pitch p WHERE a.id = p.adventure_id;

-- A blurb translated into a language the adventure is played in becomes
-- the description's translation there, made from that description, so
-- the designer doesn't call it out of date. A language the graph has no
-- translations for is left out: it would make the adventure played in it.
-- Only the draft takes it: a saved version never changes (0126), so the
-- translation reaches the store with the next version published.
CREATE FUNCTION pg_temp.zv_with_description(g JSONB, main TEXT, tr JSONB) RETURNS JSONB LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    lang TEXT;
    t JSONB;
BEGIN
    IF jsonb_typeof(g -> 'translations') IS DISTINCT FROM 'object' OR jsonb_typeof(tr) IS DISTINCT FROM 'object' THEN
        RETURN g;
    END IF;
    FOR lang, t IN SELECT key, value FROM jsonb_each(tr) LOOP
        IF btrim(COALESCE(t ->> 'blurb', '')) = '' OR jsonb_typeof(g -> 'translations' -> lang) IS DISTINCT FROM 'object' THEN
            CONTINUE;
        END IF;
        g := jsonb_set(g, ARRAY['translations', lang, 'meta:description'], to_jsonb(btrim(t ->> 'blurb')));
        IF jsonb_typeof(g -> 'translation_originals') IS DISTINCT FROM 'object' THEN
            g := jsonb_set(g, '{translation_originals}', '{}');
        END IF;
        IF jsonb_typeof(g -> 'translation_originals' -> lang) IS DISTINCT FROM 'object' THEN
            g := jsonb_set(g, ARRAY['translation_originals', lang], '{}');
        END IF;
        g := jsonb_set(g, ARRAY['translation_originals', lang, 'meta:description'], to_jsonb(main));
    END LOOP;
    RETURN g;
END $$;

UPDATE adventure_graph_drafts d SET graph = pg_temp.zv_with_description(d.graph, a.description, p.tr)
  FROM zv_pitch p JOIN adventures a ON a.id = p.adventure_id
 WHERE d.adventure_id = p.adventure_id;

-- The events keep their names and meeting places in other languages; a
-- language left with nothing goes.
CREATE FUNCTION pg_temp.zv_without_blurb(tr JSONB) RETURNS JSONB LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN jsonb_typeof(tr) = 'object' THEN COALESCE((
        SELECT jsonb_object_agg(key, value - 'blurb')
          FROM jsonb_each(tr)
         WHERE jsonb_typeof(value) = 'object' AND value - 'blurb' <> '{}'
    ), '{}') ELSE tr END
$$;

UPDATE events SET public_blurb = '', translations = pg_temp.zv_without_blurb(translations)
 WHERE public_blurb <> '' OR translations::text LIKE '%"blurb"%';

UPDATE event_listings SET blurb = '', translations = pg_temp.zv_without_blurb(translations)
 WHERE blurb <> '' OR translations::text LIKE '%"blurb"%';

DROP TABLE zv_pitch;
