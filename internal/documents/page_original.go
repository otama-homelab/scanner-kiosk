package documents

import (
	"context"
	"database/sql"
)

type ArtifactReference struct {
	Artifact    ArtifactRecord
	ExpiresAtMS int64
}

type PageOriginal = ArtifactReference

// ReadPageOriginal is an internal file-serving capability. It must not be
// serialized: paths and hashes stay inside the trusted HTTP/file adapter.
func (s *Store) ReadPageOriginal(ctx context.Context, owner, documentID, pageID string) (PageOriginal, error) {
	var out PageOriginal
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var digest []byte
		var source string
		err := tx.QueryRowContext(ctx, `SELECT a.id,a.relative_path,a.media_type,a.byte_size,a.sha256,d.expires_at_ms,p.source_type FROM pages p JOIN documents d ON d.id=p.document_id AND d.owner_id=p.owner_id JOIN artifacts a ON a.id=p.original_artifact_id AND a.document_id=d.id AND a.owner_id=d.owner_id WHERE p.id=? AND d.id=? AND d.owner_id=? AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting') AND a.kind='original' AND a.state='ready'`, pageID, documentID, owner, s.now().UnixMilli()).Scan(&out.Artifact.ID, &out.Artifact.RelativePath, &out.Artifact.MediaType, &out.Artifact.Size, &digest, &out.ExpiresAtMS, &source)
		if err != nil {
			return readError(err)
		}
		if len(digest) != 32 || !(source == "pdf" && out.Artifact.MediaType == "application/pdf" || source == "scan" && (out.Artifact.MediaType == "image/png" || out.Artifact.MediaType == "image/jpeg")) {
			return ErrStorage
		}
		copy(out.Artifact.SHA256[:], digest)
		if s.now().UnixMilli() >= out.ExpiresAtMS {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return PageOriginal{}, err
	}
	return out, nil
}
