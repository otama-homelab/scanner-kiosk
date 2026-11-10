-- The runner disables FK actions and checks the complete graph before commit.
PRAGMA legacy_alter_table=ON;
CREATE TABLE pages_next (
 id TEXT PRIMARY KEY CHECK(length(id)>=16),
 document_id TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 ordinal INTEGER NOT NULL CHECK(ordinal>=0),
 original_artifact_id TEXT NOT NULL,
 rotation INTEGER NOT NULL DEFAULT 0 CHECK(rotation IN (0,90,180,270)),
 capture_size TEXT CHECK(capture_size IN ('a4','a5','a6','letter','photo10x15')),
 capture_color TEXT CHECK(capture_color IN ('color','gray')),
 capture_dpi INTEGER CHECK(capture_dpi IN (75,100,150,200,300,600)),
 pixel_width INTEGER CHECK(pixel_width IS NULL OR pixel_width BETWEEN 1 AND 1000000),
 pixel_height INTEGER CHECK(pixel_height IS NULL OR pixel_height BETWEEN 1 AND 1000000),
 source_type TEXT NOT NULL DEFAULT 'scan' CHECK(source_type IN ('scan','pdf','photo')),
 pdf_width_points REAL,
 pdf_height_points REAL,
 CHECK((pixel_width IS NULL)=(pixel_height IS NULL)),
 CHECK((source_type='scan' AND capture_size IS NOT NULL AND capture_color IS NOT NULL AND capture_dpi IS NOT NULL AND pdf_width_points IS NULL AND pdf_height_points IS NULL)
    OR (source_type='pdf' AND capture_size IS NULL AND capture_color IS NULL AND capture_dpi IS NULL AND pixel_width IS NULL AND pixel_height IS NULL AND pdf_width_points IS NOT NULL AND pdf_height_points IS NOT NULL AND pdf_width_points>0 AND pdf_width_points<=20000 AND pdf_height_points>0 AND pdf_height_points<=20000)
    OR (source_type='photo' AND capture_size IS NULL AND capture_color IS NULL AND capture_dpi IS NULL AND pixel_width IS NOT NULL AND pixel_height IS NOT NULL AND pdf_width_points IS NULL AND pdf_height_points IS NULL)),
 UNIQUE(document_id,ordinal),
 FOREIGN KEY(document_id,owner_id) REFERENCES documents(id,owner_id) ON DELETE CASCADE,
 FOREIGN KEY(original_artifact_id,document_id,owner_id) REFERENCES artifacts(id,document_id,owner_id) ON DELETE RESTRICT
) STRICT;
INSERT INTO pages_next SELECT * FROM pages;
DROP TABLE pages;
ALTER TABLE pages_next RENAME TO pages;
CREATE TRIGGER page_original_ready BEFORE INSERT ON pages
WHEN NOT EXISTS(SELECT 1 FROM artifacts WHERE id=NEW.original_artifact_id AND kind='original' AND state='ready')
BEGIN SELECT RAISE(ABORT,'original artifact is not ready'); END;
CREATE TRIGGER page_capture_immutable BEFORE UPDATE ON pages
WHEN NEW.id!=OLD.id OR NEW.document_id!=OLD.document_id OR NEW.owner_id!=OLD.owner_id OR NEW.original_artifact_id!=OLD.original_artifact_id
 OR NEW.capture_size IS NOT OLD.capture_size OR NEW.capture_color IS NOT OLD.capture_color OR NEW.capture_dpi IS NOT OLD.capture_dpi
 OR NEW.pixel_width IS NOT OLD.pixel_width OR NEW.pixel_height IS NOT OLD.pixel_height
 OR NEW.source_type!=OLD.source_type OR NEW.pdf_width_points IS NOT OLD.pdf_width_points OR NEW.pdf_height_points IS NOT OLD.pdf_height_points
BEGIN SELECT RAISE(ABORT,'immutable page source'); END;
CREATE TRIGGER page_pdf_source BEFORE INSERT ON pages
WHEN NEW.source_type='pdf' AND (
 NOT EXISTS(SELECT 1 FROM artifacts WHERE id=NEW.original_artifact_id AND media_type='application/pdf')
 OR NOT EXISTS(SELECT 1 FROM documents WHERE id=NEW.document_id AND purpose='document'))
BEGIN SELECT RAISE(ABORT,'invalid PDF page source'); END;
CREATE TRIGGER photo_single_page BEFORE INSERT ON pages
WHEN (SELECT purpose FROM documents WHERE id=NEW.document_id)='photo' AND EXISTS(SELECT 1 FROM pages WHERE document_id=NEW.document_id)
BEGIN SELECT RAISE(ABORT,'photo already has a page'); END;

PRAGMA legacy_alter_table=OFF;

CREATE TABLE photo_sources (
 page_id TEXT PRIMARY KEY REFERENCES pages(id) ON DELETE CASCADE,
 document_id TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 raw_artifact_id TEXT NOT NULL,
 normalized_artifact_id TEXT NOT NULL UNIQUE,
 raw_sha256 BLOB NOT NULL CHECK(length(raw_sha256)=32),
 normalized_sha256 BLOB NOT NULL CHECK(length(normalized_sha256)=32),
 pixel_width INTEGER NOT NULL CHECK(pixel_width BETWEEN 1 AND 12000),
 pixel_height INTEGER NOT NULL CHECK(pixel_height BETWEEN 1 AND 12000),
 orientation INTEGER NOT NULL CHECK(orientation BETWEEN 1 AND 8),
 CHECK(raw_artifact_id!=normalized_artifact_id),
 CHECK(pixel_width*pixel_height<=40000000),
 FOREIGN KEY(raw_artifact_id,document_id,owner_id) REFERENCES artifacts(id,document_id,owner_id) ON DELETE RESTRICT,
 FOREIGN KEY(normalized_artifact_id,document_id,owner_id) REFERENCES artifacts(id,document_id,owner_id) ON DELETE RESTRICT
) STRICT;
CREATE TRIGGER page_photo_source BEFORE INSERT ON pages
WHEN NEW.source_type='photo' AND (
 NOT EXISTS(SELECT 1 FROM artifacts WHERE id=NEW.original_artifact_id AND document_id=NEW.document_id AND owner_id=NEW.owner_id AND kind='original' AND state='ready' AND media_type IN ('image/jpeg','image/png'))
 OR NOT EXISTS(SELECT 1 FROM documents WHERE id=NEW.document_id AND owner_id=NEW.owner_id AND purpose='document'))
BEGIN SELECT RAISE(ABORT,'invalid photo page source'); END;
CREATE TRIGGER photo_source_ready BEFORE INSERT ON photo_sources
WHEN NOT EXISTS(
 SELECT 1 FROM pages p
 JOIN artifacts raw ON raw.id=p.original_artifact_id AND raw.document_id=p.document_id AND raw.owner_id=p.owner_id
 JOIN artifacts n ON n.id=NEW.normalized_artifact_id AND n.document_id=p.document_id AND n.owner_id=p.owner_id
 WHERE p.id=NEW.page_id AND p.document_id=NEW.document_id AND p.owner_id=NEW.owner_id AND p.source_type='photo'
 AND p.original_artifact_id=NEW.raw_artifact_id AND raw.kind='original' AND raw.state='ready' AND raw.sha256=NEW.raw_sha256
 AND n.kind='intermediate' AND n.state='ready' AND n.media_type='image/png' AND n.sha256=NEW.normalized_sha256
 AND p.pixel_width=NEW.pixel_width AND p.pixel_height=NEW.pixel_height)
BEGIN SELECT RAISE(ABORT,'invalid photo provenance'); END;
CREATE TRIGGER photo_source_immutable BEFORE UPDATE ON photo_sources
BEGIN SELECT RAISE(ABORT,'immutable photo provenance'); END;
DROP TRIGGER thumbnail_binding_ready;
CREATE TRIGGER thumbnail_binding_ready BEFORE INSERT ON page_thumbnails
WHEN NOT EXISTS(SELECT 1 FROM artifacts a WHERE a.id=NEW.original_artifact_id AND a.document_id=NEW.document_id AND a.owner_id=NEW.owner_id AND a.state='ready'
 AND (a.kind='original' OR a.kind='intermediate' AND EXISTS(SELECT 1 FROM photo_sources ps WHERE ps.document_id=a.document_id AND ps.owner_id=a.owner_id AND (ps.normalized_artifact_id=a.id OR EXISTS(SELECT 1 FROM page_corrections c WHERE c.page_id=ps.page_id AND c.correction_artifact_id=a.id AND c.state='applied')))))
 OR NOT EXISTS(SELECT 1 FROM artifacts WHERE id=NEW.artifact_id AND document_id=NEW.document_id AND owner_id=NEW.owner_id AND kind='thumbnail' AND state='ready' AND media_type='image/jpeg' AND byte_size BETWEEN 1 AND 1048576)
BEGIN SELECT RAISE(ABORT,'invalid thumbnail binding'); END;

DROP TRIGGER import_intake_insert;
CREATE TRIGGER import_intake_insert BEFORE INSERT ON import_intakes
WHEN NEW.state!='pending' OR NOT EXISTS(
 SELECT 1 FROM jobs j JOIN artifacts a ON a.id=NEW.artifact_id AND a.document_id=j.document_id AND a.owner_id=j.owner_id
 WHERE j.id=NEW.job_id AND j.document_id=NEW.document_id AND j.owner_id=NEW.owner_id AND j.kind='import' AND j.state='queued'
 AND a.kind='original' AND a.state='staging' AND a.media_type IN ('application/pdf','image/jpeg','image/png') AND COALESCE(json_extract(j.configuration_json,'$.media_type'),'application/pdf')=a.media_type AND a.byte_size=0 AND a.sha256 IS NULL
 AND json_extract(j.configuration_json,'$.input_artifact_id')=NEW.artifact_id)
BEGIN SELECT RAISE(ABORT,'invalid import intake'); END;
