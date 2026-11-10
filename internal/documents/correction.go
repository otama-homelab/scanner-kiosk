package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"math"
	"time"

	"scanner-kiosk/internal/correction"
)

type CorrectionRequest struct {
	DocumentID     string
	PageID         string
	SuggestionID   string
	SourceSHA256   string
	Revision       int64
	IdempotencyKey string
}

func (s *Store) ApplyCorrection(ctx context.Context, owner string, request CorrectionRequest, retention time.Duration) (Projection, error) {
	if !validCorrectionRequest(request, true) {
		return Projection{}, ErrInvalid
	}
	source, err := hex.DecodeString(request.SourceSHA256)
	if err != nil || len(source) != 32 {
		return Projection{}, ErrInvalid
	}
	var sourceHash [32]byte
	copy(sourceHash[:], source)
	return s.mutateCorrection(ctx, owner, request, "apply_correction", sourceHash, retention)
}

func (s *Store) UndoCorrection(ctx context.Context, owner string, request CorrectionRequest, retention time.Duration) (Projection, error) {
	if !validCorrectionRequest(request, false) {
		return Projection{}, ErrInvalid
	}
	return s.mutateCorrection(ctx, owner, request, "undo_correction", [32]byte{}, retention)
}

func validCorrectionRequest(request CorrectionRequest, apply bool) bool {
	if !id(request.DocumentID) || len(request.DocumentID) > 96 || !id(request.PageID) || len(request.PageID) > 96 ||
		!id(request.IdempotencyKey) || request.Revision < 1 || request.Revision == math.MaxInt64 {
		return false
	}
	if apply {
		return id(request.SuggestionID) && len(request.SuggestionID) <= 96 && len(request.SourceSHA256) == 64
	}
	return request.SuggestionID == "" && request.SourceSHA256 == ""
}

func (s *Store) mutateCorrection(ctx context.Context, owner string, request CorrectionRequest, operation string, source [32]byte, retention time.Duration) (Projection, error) {
	if retention < time.Second || retention > 365*24*time.Hour {
		return Projection{}, ErrInvalid
	}
	canonical, err := canonicalCommand(struct {
		Version      int    `json:"version"`
		Operation    string `json:"operation"`
		Document     string `json:"document"`
		Page         string `json:"page"`
		Suggestion   string `json:"suggestion"`
		SourceSHA256 string `json:"source_sha256"`
		Revision     int64  `json:"revision"`
	}{1, operation, request.DocumentID, request.PageID, request.SuggestionID, request.SourceSHA256, request.Revision})
	if err != nil {
		return Projection{}, ErrInvalid
	}
	key := sha256Sum([]byte(request.IdempotencyKey))
	digest := sha256Sum(canonical)
	var out Projection
	err = s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		var storedOperation string
		var storedHash []byte
		var expiry int64
		err := tx.QueryRowContext(ctx, `SELECT operation,request_hash,expires_at_ms FROM request_receipts WHERE owner_id=? AND key_hash=?`, owner, key[:]).Scan(&storedOperation, &storedHash, &expiry)
		if err == nil {
			if storedOperation != operation || !sameHash(storedHash, digest) {
				return ErrIdempotencyConflict
			}
			if expiry <= now {
				return ErrGone
			}
			var projection string
			err = tx.QueryRowContext(ctx, `SELECT e.projection_json FROM edit_results e JOIN documents d ON d.id=e.document_id AND d.owner_id=e.owner_id WHERE e.owner_id=? AND e.key_hash=? AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting')`, owner, key[:], now).Scan(&projection)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrGone
			}
			if err != nil || json.Unmarshal([]byte(projection), &out) != nil {
				return ErrStorage
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ErrStorage
		}
		keyExpiry, err := s.freshRequestKey(ctx, tx, owner, request.IdempotencyKey, now)
		if err != nil {
			return err
		}
		var revision, documentExpiry int64
		var state string
		if err = tx.QueryRowContext(ctx, `SELECT revision,state,expires_at_ms FROM documents WHERE id=? AND owner_id=? AND expires_at_ms>? AND state NOT IN ('expired','deleting')`, request.DocumentID, owner, now).Scan(&revision, &state, &documentExpiry); err != nil {
			return readError(err)
		}
		if revision != request.Revision {
			return ErrRevision
		}
		if state != "review" && state != "ready" && state != "failed" {
			return ErrConflict
		}
		var active int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE document_id=? AND owner_id=? AND state IN ('queued','running','cancelling','reconciling')`, request.DocumentID, owner).Scan(&active); err != nil {
			return ErrStorage
		}
		if active != 0 {
			return ErrConflict
		}
		deadline, ok := addMillis(now, retention)
		if !ok || deadline < documentExpiry {
			return ErrInvalid
		}
		if operation == "apply_correction" {
			var originalID, correctionID, transformJSON, riskJSON string
			var sourceType, mediaType string
			var originalSHA, correctionSHA []byte
			var width, height int
			err = tx.QueryRowContext(ctx, `SELECT pc.original_artifact_id,pc.correction_artifact_id,pc.source_sha256,pc.correction_sha256,pc.transform_json,pc.risk_flags,p.pixel_width,p.pixel_height,p.source_type,original.media_type
 FROM page_corrections pc JOIN pages p ON p.id=pc.page_id AND p.document_id=pc.document_id AND p.owner_id=pc.owner_id
 JOIN artifacts original ON original.id=pc.original_artifact_id AND original.document_id=pc.document_id AND original.owner_id=pc.owner_id
 JOIN artifacts corrected ON corrected.id=pc.correction_artifact_id AND corrected.document_id=pc.document_id AND corrected.owner_id=pc.owner_id
 WHERE pc.owner_id=? AND pc.document_id=? AND pc.page_id=? AND pc.suggestion_id=? AND pc.state='suggested'
 AND p.original_artifact_id=pc.original_artifact_id AND p.source_type IN ('scan','photo')
 AND original.kind='original' AND original.state='ready'
 AND original.sha256=pc.source_sha256 AND corrected.sha256=pc.correction_sha256
 AND corrected.kind='intermediate' AND corrected.state='ready' AND corrected.media_type='image/png'`,
				owner, request.DocumentID, request.PageID, request.SuggestionID).Scan(&originalID, &correctionID, &originalSHA, &correctionSHA, &transformJSON, &riskJSON, &width, &height, &sourceType, &mediaType)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrConflict
			}
			if err != nil {
				return ErrStorage
			}
			if !riskFreeCorrection(riskJSON) {
				return ErrConflict
			}
			if len(originalSHA) != 32 || len(correctionSHA) != 32 || !sameHash(originalSHA, source) || width < 1 || height < 1 {
				return ErrConflict
			}
			baseline, err := loadCorrectionBaseline(ctx, tx, owner, request.DocumentID, CapturedPage{ID: request.PageID, SourceType: sourceType}, ArtifactRecord{ID: originalID, SHA256: source, PixelWidth: width, PixelHeight: height, MediaType: mediaType})
			if err != nil {
				return err
			}
			var transform correction.Transform
			if json.Unmarshal([]byte(transformJSON), &transform) != nil || correction.Validate(transform, image.Rect(0, 0, baseline.PixelWidth, baseline.PixelHeight)) != nil {
				return ErrInvalid
			}
			if _, err = tx.ExecContext(ctx, `UPDATE page_corrections SET state='discarded',applied_at_ms=NULL WHERE owner_id=? AND document_id=? AND page_id=? AND state='applied'`, owner, request.DocumentID, request.PageID); err != nil {
				return ErrStorage
			}
			result, err := tx.ExecContext(ctx, `UPDATE page_corrections SET state='applied',applied_at_ms=? WHERE owner_id=? AND document_id=? AND page_id=? AND suggestion_id=? AND state='suggested'`, now, owner, request.DocumentID, request.PageID, request.SuggestionID)
			if err != nil || result == nil {
				return ErrStorage
			}
			if count, err := result.RowsAffected(); err != nil || count != 1 {
				return ErrConflict
			}
		} else {
			result, err := tx.ExecContext(ctx, `UPDATE page_corrections SET state='discarded',applied_at_ms=NULL WHERE owner_id=? AND document_id=? AND page_id=? AND state='applied'`, owner, request.DocumentID, request.PageID)
			if err != nil || result == nil {
				return ErrStorage
			}
			if count, err := result.RowsAffected(); err != nil || count != 1 {
				return ErrConflict
			}
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM published_outputs WHERE document_id=? AND owner_id=?`, request.DocumentID, owner); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `UPDATE documents SET revision=revision+1,state='review' WHERE id=? AND owner_id=?`, request.DocumentID, owner); err != nil {
			return ErrStorage
		}
		out, err = s.project(ctx, tx, owner, request.DocumentID)
		if err != nil {
			return err
		}
		stored, err := json.Marshal(out)
		if err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,expires_at_ms,key_expires_at_ms) VALUES(?,?,?,?,?,?)`, owner, key[:], operation, digest[:], deadline, keyExpiry); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO edit_results(owner_id,key_hash,document_id,projection_json) VALUES(?,?,?,?)`, owner, key[:], request.DocumentID, string(stored)); err != nil {
			return ErrStorage
		}
		return nil
	})
	if err != nil {
		return Projection{}, err
	}
	return out, nil
}

func riskFreeCorrection(raw string) bool {
	if len(raw) > 4<<10 {
		return false
	}
	var flags []string
	return json.Unmarshal([]byte(raw), &flags) == nil && flags != nil && len(flags) == 0
}

func (s *Store) ReadCorrectionArtifact(ctx context.Context, owner, documentID, pageID string) (ArtifactReference, error) {
	var out ArtifactReference
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var digest []byte
		err := tx.QueryRowContext(ctx, `SELECT a.id,a.relative_path,a.media_type,a.byte_size,a.sha256,d.expires_at_ms
 FROM page_corrections pc JOIN documents d ON d.id=pc.document_id AND d.owner_id=pc.owner_id
 JOIN artifacts a ON a.id=pc.correction_artifact_id AND a.document_id=pc.document_id AND a.owner_id=pc.owner_id
 WHERE pc.owner_id=? AND pc.document_id=? AND pc.page_id=? AND pc.state IN ('suggested','applied')
 AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting') AND a.kind='intermediate' AND a.state='ready' AND a.media_type='image/png'
 ORDER BY CASE pc.state WHEN 'applied' THEN 0 ELSE 1 END,pc.created_at_ms DESC,pc.suggestion_id DESC LIMIT 1`, owner, documentID, pageID, s.now().UnixMilli()).Scan(&out.Artifact.ID, &out.Artifact.RelativePath, &out.Artifact.MediaType, &out.Artifact.Size, &digest, &out.ExpiresAtMS)
		if err != nil {
			return readError(err)
		}
		if len(digest) != 32 || out.Artifact.MediaType != "image/png" || out.Artifact.Size < 1 || out.Artifact.Size > 200<<20 {
			return ErrStorage
		}
		copy(out.Artifact.SHA256[:], digest)
		return nil
	})
	if err != nil {
		return ArtifactReference{}, err
	}
	return out, nil
}

func sha256Sum(body []byte) [32]byte {
	return sha256.Sum256(body)
}
