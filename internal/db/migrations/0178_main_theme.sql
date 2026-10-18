-- An adventure's look becomes its main theme: graph.main_theme names one
-- of the graph's saved themes, and publishing copies that theme's look to
-- the adventure's look columns. Until now the look was set on the
-- adventure alone and only matched a saved theme by chance.
--
-- For every adventure made in the designer (trail_cards is NULL), in its
-- draft and its dev copy (version 0): a saved theme with exactly the
-- adventure's look becomes the main theme; otherwise the look is saved as
-- a new theme, "Main", and that is. Saved versions can't change (0126):
-- without a main theme they keep showing the adventure's look, which is
-- the same, until the next publish. A step switching to a built-in theme (dark,
-- night, alarm, parchment) switches to a saved copy of it instead: the
-- main look in the built-in's colours, as the phone painted it.

CREATE FUNCTION pg_temp.zv_look(a adventures) RETURNS JSONB LANGUAGE sql IMMUTABLE AS $$
    SELECT jsonb_strip_nulls(jsonb_build_object(
        'accent', NULLIF(a.accent_color, ''),
        'accent_secondary', NULLIF(a.accent_secondary_color, ''),
        'background', NULLIF(a.background_color, ''),
        'text', NULLIF(a.text_color, ''),
        'submit_bg', NULLIF(a.submit_bg_color, ''),
        'font', NULLIF(a.font, ''),
        'heading_font', NULLIF(a.heading_font, ''),
        'corners', NULLIF(a.corners, ''),
        'cards', NULLIF(a.cards, ''),
        'card_outline', CASE WHEN a.card_outline THEN TRUE END,
        'backdrop', CASE WHEN jsonb_typeof(a.backdrop) = 'object' THEN a.backdrop END,
        'style', CASE WHEN jsonb_typeof(a.look_style) = 'object' AND a.look_style <> '{}'::jsonb THEN a.look_style END
    ))
$$;

CREATE FUNCTION pg_temp.zv_preset(name TEXT) RETURNS JSONB LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE name
        WHEN 'dark' THEN '{"background":"#14181f","text":"#e8edf3","accent":"#5aa9ff","accent_secondary":"#5aa9ff","submit_bg":"#1f2630"}'::jsonb
        WHEN 'night' THEN '{"background":"#0b1026","text":"#d9def5","accent":"#9d8cff","accent_secondary":"#9d8cff","submit_bg":"#161c3a"}'::jsonb
        WHEN 'alarm' THEN '{"background":"#2a0b0b","text":"#ffe9e6","accent":"#ff4d3d","accent_secondary":"#ff4d3d","submit_bg":"#3d1010"}'::jsonb
        WHEN 'parchment' THEN '{"background":"#f3e7c9","text":"#3b2a17","accent":"#8a4b1d","accent_secondary":"#8a4b1d","submit_bg":"#e4d3a8"}'::jsonb
    END
$$;

-- The main theme: a saved theme with the look, or a new one.
CREATE FUNCTION pg_temp.zv_main(g JSONB, lk JSONB, lang TEXT) RETURNS JSONB LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    themes JSONB := CASE WHEN jsonb_typeof(g -> 'themes') = 'array' THEN g -> 'themes' ELSE '[]'::jsonb END;
    slug TEXT;
BEGIN
    IF COALESCE(g ->> 'main_theme', '') <> '' THEN
        RETURN g;
    END IF;
    SELECT t ->> 'slug' INTO slug FROM jsonb_array_elements(themes) WITH ORDINALITY AS e(t, i)
    WHERE t -> 'look' = lk ORDER BY i LIMIT 1;
    IF slug IS NOT NULL THEN
        RETURN g || jsonb_build_object('main_theme', slug);
    END IF;
    slug := 'main';
    IF EXISTS (SELECT 1 FROM jsonb_array_elements(themes) t WHERE t ->> 'slug' = slug) THEN
        slug := 'main-theme';
    END IF;
    RETURN g || jsonb_build_object(
        'themes', themes || jsonb_build_array(jsonb_build_object(
            'slug', slug, 'name', CASE WHEN lang LIKE 'sv%' THEN 'Huvudtema' ELSE 'Main' END, 'look', lk)),
        'main_theme', slug);
END
$$;

-- Steps switching to a built-in theme switch to a saved copy of it.
CREATE FUNCTION pg_temp.zv_builtins(g JSONB, lk JSONB) RETURNS JSONB LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    b TEXT;
    slug TEXT;
    themed JSONB;
BEGIN
    IF jsonb_typeof(g -> 'nodes') <> 'array' THEN
        RETURN g;
    END IF;
    FOR b IN
        SELECT DISTINCT n -> 'effect' ->> 'theme' FROM jsonb_array_elements(g -> 'nodes') n
        WHERE n -> 'effect' ->> 'kind' = 'theme' AND n -> 'effect' ->> 'theme' IN ('dark', 'night', 'alarm', 'parchment')
    LOOP
        slug := b || '-theme';
        IF NOT EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(g -> 'themes', '[]'::jsonb)) t WHERE t ->> 'slug' = slug) THEN
            themed := (lk - 'backdrop') || pg_temp.zv_preset(b);
            IF lk ? 'backdrop' THEN
                themed := themed || jsonb_build_object('backdrop', (lk -> 'backdrop') - 'color' - 'color2');
            END IF;
            g := jsonb_set(g, '{themes}', COALESCE(g -> 'themes', '[]'::jsonb) || jsonb_build_array(jsonb_build_object(
                'slug', slug, 'name', initcap(b), 'look', themed)));
        END IF;
        g := jsonb_set(g, '{nodes}', (
            SELECT jsonb_agg(CASE WHEN n -> 'effect' ->> 'kind' = 'theme' AND n -> 'effect' ->> 'theme' = b
                                  THEN jsonb_set(n, '{effect,theme}', to_jsonb(slug)) ELSE n END ORDER BY i)
            FROM jsonb_array_elements(g -> 'nodes') WITH ORDINALITY AS t(n, i)));
    END LOOP;
    RETURN g;
END
$$;

UPDATE adventure_graph_drafts d
SET graph = pg_temp.zv_builtins(pg_temp.zv_main(d.graph, pg_temp.zv_look(a), a.language), pg_temp.zv_look(a))
FROM adventures a
WHERE a.id = d.adventure_id AND a.trail_cards IS NULL;

UPDATE adventure_graphs v
SET graph = pg_temp.zv_builtins(pg_temp.zv_main(v.graph, pg_temp.zv_look(a), a.language), pg_temp.zv_look(a))
FROM adventures a
WHERE a.id = v.adventure_id AND a.trail_cards IS NULL AND v.version = 0;
