package documents

import (
	"context"
	"database/sql"
)

// loadCorrectionBaseline shares geometry authority without replacing raw provenance.
func loadCorrectionBaseline(ctx context.Context, tx *sql.Tx, owner, doc string, page CapturedPage, original ArtifactRecord) (ArtifactRecord, error) {
	switch page.SourceType {
	case "photo":
		return loadPhotoBaseline(ctx, tx, owner, doc, page.ID, original)
	case "", "scan":
		if original.MediaType != "image/png" && original.MediaType != "image/jpeg" {
			return ArtifactRecord{}, ErrConflict
		}
		return original, nil
	default:
		return ArtifactRecord{}, ErrInvalid
	}
}

// loadPhotoBaseline binds immutable raw provenance to its normalized pixels.
func loadPhotoBaseline(ctx context.Context, tx *sql.Tx, owner, doc, page string, original ArtifactRecord) (ArtifactRecord, error) {
	var a ArtifactRecord
	var digest, rawHash []byte
	err := tx.QueryRowContext(ctx, `SELECT a.id,a.relative_path,a.media_type,a.byte_size,a.sha256,ps.pixel_width,ps.pixel_height,ps.raw_sha256
 FROM photo_sources ps JOIN pages p ON p.id=ps.page_id AND p.document_id=ps.document_id AND p.owner_id=ps.owner_id
 JOIN artifacts a ON a.id=ps.normalized_artifact_id AND a.document_id=ps.document_id AND a.owner_id=ps.owner_id
 WHERE ps.page_id=? AND ps.document_id=? AND ps.owner_id=? AND ps.raw_artifact_id=? AND p.original_artifact_id=ps.raw_artifact_id AND p.source_type='photo'
 AND a.kind='intermediate' AND a.state='ready' AND a.media_type='image/png' AND a.sha256=ps.normalized_sha256`, page, doc, owner, original.ID).Scan(&a.ID, &a.RelativePath, &a.MediaType, &a.Size, &digest, &a.PixelWidth, &a.PixelHeight, &rawHash)
	if err != nil {
		return ArtifactRecord{}, readError(err)
	}
	if !sameHash(rawHash, original.SHA256) || len(digest) != 32 || !localPath(a.RelativePath) || a.Size < 1 || a.PixelWidth < 1 || a.PixelHeight < 1 {
		return ArtifactRecord{}, ErrConflict
	}
	copy(a.SHA256[:], digest)
	return a, nil
}

// ReadPageEffective preserves ReadPageOriginal as an explicit provenance reader.
// Photo previews select the exact source frozen by Save and reOCR.
func (s *Store) ReadPageEffective(ctx context.Context, owner, doc, page string) (ArtifactReference, error) {
	var out ArtifactReference
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var p CapturedPage
		err := tx.QueryRowContext(ctx, `SELECT p.id,p.original_artifact_id,CASE p.source_type WHEN 'scan' THEN '' ELSE p.source_type END,p.rotation,COALESCE(p.capture_size,''),COALESCE(p.capture_color,''),COALESCE(p.capture_dpi,0),COALESCE(p.pdf_width_points,0),COALESCE(p.pdf_height_points,0),d.expires_at_ms
 FROM pages p JOIN documents d ON d.id=p.document_id AND d.owner_id=p.owner_id WHERE p.id=? AND p.document_id=? AND p.owner_id=? AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting')`, page, doc, owner, s.now().UnixMilli()).Scan(&p.ID, &p.OriginalArtifactID, &p.SourceType, &p.Rotation, &p.Size, &p.Color, &p.DPI, &p.PDFWidthPoints, &p.PDFHeightPoints, &out.ExpiresAtMS)
		if err != nil {
			return readError(err)
		}
		sources, err := loadReOCRSources(ctx, tx, owner, doc, []CapturedPage{p})
		if err != nil {
			return err
		}
		out.Artifact = sources[0].Original
		if p.SourceType == "photo" {
			out.Artifact = sources[0].Effective
		}
		if s.now().UnixMilli() >= out.ExpiresAtMS {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return ArtifactReference{}, err
	}
	return out, nil
}

func hasPhotoPages(pages []CapturedPage) bool {
	for _, p := range pages {
		if p.SourceType == "photo" {
			return true
		}
	}
	return false
}
