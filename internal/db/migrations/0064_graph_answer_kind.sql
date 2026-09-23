-- A puzzle's way of answering moves into one field, puzzle.answer =
-- {"kind": "tiles" | "search" | "choice" | "lock", …}, from the four
-- separate puzzle.tiles / search / choices / lock objects of 0061's
-- release — which a puzzle could set several of at once. And a journal
-- section's translation moves from its first note's key,
-- "j:<slug>:section", to the section's own, "js:<section>", so
-- reordering the notes no longer loses it.
--
-- Rewrites drafts and stored versions alike: a version's content does
-- not change, only how it is written down.
--
-- The adventure's language column becomes the one place its language is
-- set; graphstore writes it into each graph it saves. Until now the
-- designer set only the graph's copy, so that copy is carried over to the
-- adventure first, before the next save would overwrite it.

UPDATE adventures a SET language = d.graph ->> 'language'
FROM adventure_graph_drafts d
WHERE d.adventure_id = a.id AND COALESCE(d.graph ->> 'language', '') <> '' AND a.language <> d.graph ->> 'language';

CREATE FUNCTION pg_temp.zv_answer(p JSONB) RETURNS JSONB LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE
        WHEN p ? 'tiles' THEN (p - 'tiles') || jsonb_build_object('answer', jsonb_build_object('kind', 'tiles') || (p -> 'tiles'))
        WHEN p ? 'search' THEN (p - 'search') || jsonb_build_object('answer', jsonb_build_object('kind', 'search') || (p -> 'search'))
        WHEN p ? 'choices' THEN (p - 'choices') || jsonb_build_object('answer', jsonb_build_object('kind', 'choice') || (p -> 'choices'))
        WHEN p ? 'lock' THEN (p - 'lock') || jsonb_build_object('answer', jsonb_build_object('kind', 'lock') || (p -> 'lock'))
        ELSE p
    END
$$;

CREATE FUNCTION pg_temp.zv_nodes(g JSONB) RETURNS JSONB LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN jsonb_typeof(g -> 'nodes') = 'array' THEN jsonb_set(g, '{nodes}', COALESCE((
        SELECT jsonb_agg(CASE WHEN jsonb_typeof(n -> 'puzzle') = 'object'
                              THEN jsonb_set(n, '{puzzle}', pg_temp.zv_answer(n -> 'puzzle'))
                              ELSE n END ORDER BY i)
        FROM jsonb_array_elements(g -> 'nodes') WITH ORDINALITY AS t(n, i)
    ), '[]'::jsonb)) ELSE g END
$$;

CREATE FUNCTION pg_temp.zv_sections(g JSONB) RETURNS JSONB LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN jsonb_typeof(g -> 'translations') = 'object' THEN jsonb_set(g, '{translations}', COALESCE((
        SELECT jsonb_object_agg(lang, (
            SELECT COALESCE(jsonb_object_agg(COALESCE('js:' || s.section, e.k), e.v), '{}'::jsonb)
            FROM jsonb_each(tr) AS e(k, v)
            LEFT JOIN LATERAL (
                SELECT j ->> 'section' AS section
                FROM jsonb_array_elements(COALESCE(g -> 'journal', '[]'::jsonb)) AS j
                WHERE e.k ~ '^j:.+:section$'
                  AND j ->> 'slug' = substring(e.k FROM '^j:(.+):section$')
                  AND COALESCE(j ->> 'section', '') <> ''
                LIMIT 1
            ) s ON true
        ))
        FROM jsonb_each(g -> 'translations') AS l(lang, tr)
    ), '{}'::jsonb)) ELSE g END
$$;

UPDATE adventure_graph_drafts SET graph = pg_temp.zv_sections(pg_temp.zv_nodes(graph))
WHERE graph <> pg_temp.zv_sections(pg_temp.zv_nodes(graph));

UPDATE adventure_graphs SET graph = pg_temp.zv_sections(pg_temp.zv_nodes(graph))
WHERE graph <> pg_temp.zv_sections(pg_temp.zv_nodes(graph));

DROP FUNCTION pg_temp.zv_sections(JSONB);
DROP FUNCTION pg_temp.zv_nodes(JSONB);
DROP FUNCTION pg_temp.zv_answer(JSONB);
