-- What players are served can't change under a review (listing review,
-- 0123-0125). A saved adventure version is append-only by design —
-- saving makes a new one — and now the database holds it to that: its
-- content can't be updated, only its label. The dev copy (version 0)
-- follows the draft as it is edited and stays writable. Uploaded media
-- is never rewritten either: a new upload is a new row.
--
-- Someone who owns the database can drop these triggers. That is what
-- the review check is for (listing_review_served.go): it compares what
-- each approved listing serves with the fingerprint of what was
-- approved, so an edit made around the triggers shows.

CREATE FUNCTION adventure_graphs_frozen() RETURNS trigger AS $$
BEGIN
    IF OLD.version <> 0 AND (
        NEW.graph IS DISTINCT FROM OLD.graph OR
        NEW.adventure_id IS DISTINCT FROM OLD.adventure_id OR
        NEW.version IS DISTINCT FROM OLD.version OR
        NEW.event_id IS DISTINCT FROM OLD.event_id
    ) THEN
        RAISE EXCEPTION 'adventure version % of adventure % is saved and can''t be changed: save a new version', OLD.version, OLD.adventure_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER adventure_graphs_frozen
    BEFORE UPDATE ON adventure_graphs
    FOR EACH ROW EXECUTE FUNCTION adventure_graphs_frozen();

CREATE FUNCTION adventure_media_frozen() RETURNS trigger AS $$
BEGIN
    IF NEW.content IS DISTINCT FROM OLD.content OR
       NEW.mime_type IS DISTINCT FROM OLD.mime_type OR
       NEW.kind IS DISTINCT FROM OLD.kind OR
       NEW.sha256 IS DISTINCT FROM OLD.sha256 OR
       NEW.adventure_id IS DISTINCT FROM OLD.adventure_id THEN
        RAISE EXCEPTION 'uploaded media % can''t be changed: upload a new file', OLD.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER adventure_media_frozen
    BEFORE UPDATE ON adventure_media
    FOR EACH ROW EXECUTE FUNCTION adventure_media_frozen();
