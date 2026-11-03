package documents

import (
	"context"
	"database/sql"
	"errors"
)

// CaptureBundleRecorded reconciles private immutable producing-job metadata.
// A page edit/delete or owner disable/TTL cannot invalidate a committed bundle.
// Any observed committed-but-inconsistent bundle remains an error, never a
// negative acknowledgement authorizing deletion of potentially published files.
func (s *Store) CaptureBundleRecorded(ctx context.Context, owner, job string, original ArtifactRecord, thumbnails []ThumbnailRecord) (bool, error) {
	if !id(owner) || !id(job) || !id(original.ID) || len(thumbnails) > 1 {
		return false, ErrInvalid
	}
	expected := map[string]ArtifactRecord{original.ID: original}
	for _, t := range thumbnails {
		if t.OriginalID != original.ID || !id(t.Artifact.ID) || t.Artifact.ID == original.ID {
			return false, ErrInvalid
		}
		expected[t.Artifact.ID] = t.Artifact
	}
	known, err := s.CaptureRecorded(ctx, owner, job, original.ID)
	if err != nil {
		return false, err
	}
	if !known {
		var state string
		err = s.documents.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=? AND owner_id=? AND kind='scan'`, job, owner).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, ErrStorage
		}
		if state == "succeeded" {
			return false, ErrConflict
		}
		return false, nil
	}
	rows, err := s.documents.QueryContext(ctx, `SELECT a.id,a.kind,a.relative_path,a.media_type,a.byte_size,a.sha256,COALESCE(t.original_artifact_id,''),COALESCE(t.pixel_width,0),COALESCE(t.pixel_height,0) FROM jobs j JOIN job_artifacts x ON x.job_id=j.id AND x.owner_id=j.owner_id JOIN artifacts a ON a.id=x.artifact_id AND a.document_id=x.document_id AND a.owner_id=x.owner_id LEFT JOIN page_thumbnails t ON t.artifact_id=a.id AND t.document_id=a.document_id AND t.owner_id=a.owner_id WHERE j.id=? AND j.owner_id=? AND j.kind='scan' AND j.state='succeeded' AND a.state IN ('ready','deleting') LIMIT 3`, job, owner)
	if err != nil {
		return false, ErrStorage
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		var id, kind, path, media, source string
		var size int64
		var digest []byte
		var width, height int
		if err = rows.Scan(&id, &kind, &path, &media, &size, &digest, &source, &width, &height); err != nil {
			return false, ErrStorage
		}
		a, ok := expected[id]
		if !ok || a.RelativePath != path || a.MediaType != media || a.Size != size || !sameHash(digest, a.SHA256) {
			return false, ErrConflict
		}
		if id == original.ID {
			if kind != "original" {
				return false, ErrConflict
			}
		} else if kind != "thumbnail" || source != original.ID || width != a.PixelWidth || height != a.PixelHeight {
			return false, ErrConflict
		}
		found++
	}
	if err = rows.Err(); err != nil {
		return false, ErrStorage
	}
	if found != len(expected) {
		return false, ErrConflict
	}
	return true, nil
}
