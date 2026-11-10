package documents

import (
	"context"
	"database/sql"
)

// ThumbnailRecord is trusted worker metadata, never a client-supplied path.
// Actual image validation and sealed-file ownership precede DB publication.
type ThumbnailRecord struct {
	OriginalID string
	Artifact   ArtifactRecord
}

// ReadPageThumbnail returns no original fallback. A missing derivative is 404,
// allowing the client to opt into a separately authorized full preview.
func (s *Store) ReadPageThumbnail(ctx context.Context, owner, documentID, pageID string) (ArtifactReference, error) {
	var out ArtifactReference
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var digest []byte
		err := tx.QueryRowContext(ctx, `SELECT a.id,a.relative_path,a.media_type,a.byte_size,a.sha256,d.expires_at_ms
 FROM pages p JOIN documents d ON d.id=p.document_id AND d.owner_id=p.owner_id
 JOIN artifacts original ON original.id=p.original_artifact_id AND original.document_id=p.document_id AND original.owner_id=p.owner_id
 JOIN page_thumbnails t ON t.original_artifact_id=(CASE WHEN p.source_type='photo' THEN COALESCE((SELECT c.correction_artifact_id FROM page_corrections c WHERE c.page_id=p.id AND c.document_id=p.document_id AND c.owner_id=p.owner_id AND c.state='applied'),(SELECT ps.normalized_artifact_id FROM photo_sources ps WHERE ps.page_id=p.id AND ps.document_id=p.document_id AND ps.owner_id=p.owner_id)) ELSE p.original_artifact_id END) AND t.document_id=p.document_id AND t.owner_id=p.owner_id
 JOIN artifacts a ON a.id=t.artifact_id AND a.document_id=t.document_id AND a.owner_id=t.owner_id
 WHERE p.id=? AND d.id=? AND d.owner_id=? AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting') AND original.kind='original' AND original.state='ready' AND a.kind='thumbnail' AND a.state='ready'`, pageID, documentID, owner, s.now().UnixMilli()).Scan(&out.Artifact.ID, &out.Artifact.RelativePath, &out.Artifact.MediaType, &out.Artifact.Size, &digest, &out.ExpiresAtMS)
		if err != nil {
			return readError(err)
		}
		if len(digest) != 32 || out.Artifact.MediaType != "image/jpeg" || out.Artifact.Size < 1 || out.Artifact.Size > 1<<20 {
			return ErrStorage
		}
		copy(out.Artifact.SHA256[:], digest)
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
