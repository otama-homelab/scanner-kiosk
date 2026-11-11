-- Imported images may use the existing single-page photo output flow.
-- Original readiness, owner binding and photo_sources provenance stay required.
DROP TRIGGER page_photo_source;
CREATE TRIGGER page_photo_source BEFORE INSERT ON pages
WHEN NEW.source_type='photo' AND (
 NOT EXISTS(SELECT 1 FROM artifacts WHERE id=NEW.original_artifact_id AND document_id=NEW.document_id AND owner_id=NEW.owner_id AND kind='original' AND state='ready' AND media_type IN ('image/jpeg','image/png'))
 OR NOT EXISTS(SELECT 1 FROM documents WHERE id=NEW.document_id AND owner_id=NEW.owner_id AND purpose IN ('document','photo')))
BEGIN SELECT RAISE(ABORT,'invalid photo page source'); END;
