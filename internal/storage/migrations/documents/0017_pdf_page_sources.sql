-- No table references pages; replace only this leaf table, preserving the job graph.
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
 source_type TEXT NOT NULL DEFAULT 'scan' CHECK(source_type IN ('scan','pdf')),
 pdf_width_points REAL,
 pdf_height_points REAL,
 CHECK((pixel_width IS NULL)=(pixel_height IS NULL)),
 CHECK((source_type='scan' AND capture_size IS NOT NULL AND capture_color IS NOT NULL AND capture_dpi IS NOT NULL AND pdf_width_points IS NULL AND pdf_height_points IS NULL)
    OR (source_type='pdf' AND capture_size IS NULL AND capture_color IS NULL AND capture_dpi IS NULL AND pixel_width IS NULL AND pixel_height IS NULL AND pdf_width_points IS NOT NULL AND pdf_height_points IS NOT NULL AND pdf_width_points>0 AND pdf_width_points<=20000 AND pdf_height_points>0 AND pdf_height_points<=20000)),
 UNIQUE(document_id,ordinal),
 FOREIGN KEY(document_id,owner_id) REFERENCES documents(id,owner_id) ON DELETE CASCADE,
 FOREIGN KEY(original_artifact_id,document_id,owner_id) REFERENCES artifacts(id,document_id,owner_id) ON DELETE RESTRICT
) STRICT;
INSERT INTO pages_next(id,document_id,owner_id,ordinal,original_artifact_id,rotation,capture_size,capture_color,capture_dpi,pixel_width,pixel_height)
 SELECT id,document_id,owner_id,ordinal,original_artifact_id,rotation,capture_size,capture_color,capture_dpi,pixel_width,pixel_height FROM pages;
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
