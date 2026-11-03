-- Immutable source-bound derivatives survive metadata-only page edits.
CREATE TABLE page_thumbnails (
 original_artifact_id TEXT PRIMARY KEY,
 document_id TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 artifact_id TEXT NOT NULL UNIQUE,
 pixel_width INTEGER NOT NULL CHECK(pixel_width BETWEEN 1 AND 512),
 pixel_height INTEGER NOT NULL CHECK(pixel_height BETWEEN 1 AND 512),
 FOREIGN KEY(original_artifact_id,document_id,owner_id) REFERENCES artifacts(id,document_id,owner_id) ON DELETE CASCADE,
 FOREIGN KEY(artifact_id,document_id,owner_id) REFERENCES artifacts(id,document_id,owner_id) ON DELETE CASCADE
) STRICT;
CREATE TRIGGER thumbnail_binding_ready BEFORE INSERT ON page_thumbnails
WHEN NOT EXISTS(SELECT 1 FROM artifacts WHERE id=NEW.original_artifact_id AND kind='original' AND state='ready')
 OR NOT EXISTS(SELECT 1 FROM artifacts WHERE id=NEW.artifact_id AND kind='thumbnail' AND state='ready' AND media_type='image/jpeg' AND byte_size BETWEEN 1 AND 1048576)
BEGIN SELECT RAISE(ABORT,'invalid thumbnail binding'); END;
CREATE TRIGGER thumbnail_binding_immutable BEFORE UPDATE ON page_thumbnails
BEGIN SELECT RAISE(ABORT,'immutable thumbnail binding'); END;
