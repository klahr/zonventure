-- PDFs for an adventure's print materials (graph print_materials): a
-- letter posted to a buyer, a map. Videos for video blocks were stored
-- as 'video' all along, which the original check refused.
ALTER TABLE adventure_media DROP CONSTRAINT adventure_media_kind_check;
ALTER TABLE adventure_media ADD CONSTRAINT adventure_media_kind_check
    CHECK (kind IN ('image', 'audio', 'video', 'pdf'));
