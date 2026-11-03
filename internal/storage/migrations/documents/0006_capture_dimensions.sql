-- Existing pages retain unknown dimensions; do not infer pixels from paper/DPI.
ALTER TABLE pages ADD COLUMN pixel_width INTEGER CHECK(pixel_width IS NULL OR pixel_width BETWEEN 1 AND 1000000);
ALTER TABLE pages ADD COLUMN pixel_height INTEGER CHECK(pixel_height IS NULL OR pixel_height BETWEEN 1 AND 1000000);
CREATE TRIGGER page_dimensions_pair BEFORE INSERT ON pages
WHEN (NEW.pixel_width IS NULL)!=(NEW.pixel_height IS NULL)
BEGIN SELECT RAISE(ABORT,'incomplete image dimensions'); END;
CREATE TRIGGER page_dimensions_immutable BEFORE UPDATE ON pages
WHEN NEW.pixel_width IS NOT OLD.pixel_width OR NEW.pixel_height IS NOT OLD.pixel_height
BEGIN SELECT RAISE(ABORT,'immutable image dimensions'); END;
