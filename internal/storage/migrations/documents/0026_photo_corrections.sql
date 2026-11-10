DROP TRIGGER correction_suggestion_insert;
CREATE TRIGGER correction_suggestion_insert BEFORE INSERT ON page_corrections
WHEN NEW.state!='suggested' OR NOT EXISTS(
 SELECT 1 FROM pages p
 JOIN artifacts original ON original.id=p.original_artifact_id AND original.document_id=p.document_id AND original.owner_id=p.owner_id
 JOIN artifacts corrected ON corrected.id=NEW.correction_artifact_id AND corrected.document_id=p.document_id AND corrected.owner_id=p.owner_id
 WHERE p.id=NEW.page_id AND p.document_id=NEW.document_id AND p.owner_id=NEW.owner_id
 AND (p.source_type='scan' OR p.source_type='photo' AND EXISTS(SELECT 1 FROM photo_sources ps JOIN artifacts n ON n.id=ps.normalized_artifact_id AND n.document_id=ps.document_id AND n.owner_id=ps.owner_id WHERE ps.page_id=p.id AND ps.document_id=p.document_id AND ps.owner_id=p.owner_id AND ps.raw_artifact_id=original.id AND ps.raw_sha256=original.sha256 AND n.state='ready' AND n.kind='intermediate' AND n.sha256=ps.normalized_sha256))
 AND p.original_artifact_id=NEW.original_artifact_id AND p.pixel_width IS NOT NULL AND p.pixel_height IS NOT NULL
 AND original.kind='original' AND original.state='ready' AND original.sha256=NEW.source_sha256
 AND corrected.kind='intermediate' AND corrected.state='ready' AND corrected.media_type='image/png' AND corrected.sha256=NEW.correction_sha256
)
BEGIN SELECT RAISE(ABORT,'invalid correction suggestion'); END;
