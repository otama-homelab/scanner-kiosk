package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"
)

type OCRResultRequest struct {
	DocumentID, ResultID, IdempotencyKey string
	Revision                             int64
}

func (s *Store) AdoptOCRResult(ctx context.Context, owner string, request OCRResultRequest, retention time.Duration) (Projection, error) {
	return s.mutateOCRResult(ctx, owner, request, "adopt_ocr_result", retention)
}
func (s *Store) DiscardOCRResult(ctx context.Context, owner string, request OCRResultRequest, retention time.Duration) (Projection, error) {
	return s.mutateOCRResult(ctx, owner, request, "discard_ocr_result", retention)
}

func (s *Store) mutateOCRResult(ctx context.Context, owner string, request OCRResultRequest, operation string, retention time.Duration) (Projection, error) {
	if !id(request.DocumentID) || !id(request.ResultID) || !id(request.IdempotencyKey) || request.Revision < 1 || request.Revision == math.MaxInt64 || retention < time.Second || retention > 365*24*time.Hour {
		return Projection{}, ErrInvalid
	}
	canonical, err := canonicalCommand(struct {
		Version                     int
		Operation, Document, Result string
		Revision                    int64
	}{1, operation, request.DocumentID, request.ResultID, request.Revision})
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
		err = tx.QueryRowContext(ctx, `SELECT revision,state,expires_at_ms FROM documents WHERE id=? AND owner_id=? AND expires_at_ms>? AND state NOT IN ('expired','deleting')`, request.DocumentID, owner, now).Scan(&revision, &state, &documentExpiry)
		if err != nil {
			return readError(err)
		}
		if revision != request.Revision {
			return ErrRevision
		}
		if state != "review" && state != "ready" && state != "failed" {
			return ErrConflict
		}
		var active int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE document_id=? AND owner_id=? AND state IN ('queued','running','cancelling','reconciling')`, request.DocumentID, owner).Scan(&active)
		if err != nil {
			return ErrStorage
		}
		if active != 0 {
			return ErrConflict
		}
		deadline, ok := addMillis(now, retention)
		if !ok || deadline < documentExpiry {
			return ErrInvalid
		}
		var raw string
		var hash []byte
		var inputRevision int64
		err = tx.QueryRowContext(ctx, `SELECT r.bundle_json,r.bundle_sha256,r.input_revision FROM ocr_results r JOIN jobs j ON j.id=r.job_id AND j.owner_id=r.owner_id WHERE r.id=? AND r.document_id=? AND r.owner_id=? AND r.state='candidate' AND j.state='succeeded'`, request.ResultID, request.DocumentID, owner).Scan(&raw, &hash, &inputRevision)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return ErrStorage
		}
		if !sameHash(hash, sha256Sum([]byte(raw))) {
			return ErrStorage
		}
		var bundle OCRResultBundle
		if json.Unmarshal([]byte(raw), &bundle) != nil {
			return ErrStorage
		}
		_, all, _, err := normalizeOCRBundle(bundle)
		if err != nil || bundle.ID != request.ResultID || bundle.InputRevision != inputRevision {
			return ErrStorage
		}
		if operation == "adopt_ocr_result" {
			if inputRevision != revision {
				return ErrRevision
			}
			if err = validateOCRSources(ctx, tx, owner, request.DocumentID, bundle); err != nil {
				return err
			}
			// Recheck all produced artifacts; a cleanup-in-progress file cannot be adopted.
			for _, v := range all {
				a := v.artifact
				var n int
				err = tx.QueryRowContext(ctx, `SELECT count(*) FROM artifacts a JOIN job_artifacts x ON x.artifact_id=a.id AND x.document_id=a.document_id AND x.owner_id=a.owner_id WHERE a.id=? AND a.document_id=? AND a.owner_id=? AND a.state='ready' AND a.kind=? AND a.input_revision=? AND a.sha256=? AND a.byte_size=? AND a.media_type=? AND a.relative_path=? AND x.job_id=?`, a.ID, request.DocumentID, owner, v.kind, inputRevision, a.SHA256[:], a.Size, a.MediaType, a.RelativePath, bundle.JobID).Scan(&n)
				if err != nil {
					return ErrStorage
				}
				if n != 1 {
					return ErrConflict
				}
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM published_outputs WHERE document_id=? AND owner_id=?`, request.DocumentID, owner); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, `UPDATE documents SET revision=revision+1,state='ready' WHERE id=? AND owner_id=?`, request.DocumentID, owner); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, `UPDATE ocr_results SET state='adopted',adopted_revision=? WHERE id=? AND owner_id=? AND state='candidate'`, revision+1, request.ResultID, owner); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO published_outputs(document_id,owner_id,revision,artifact_id) VALUES(?,?,?,?)`, request.DocumentID, owner, revision+1, bundle.Output.ID); err != nil {
				return ErrStorage
			}
		} else {
			if _, err = tx.ExecContext(ctx, `UPDATE ocr_results SET state='discarded' WHERE id=? AND owner_id=? AND state='candidate'`, request.ResultID, owner); err != nil {
				return ErrStorage
			}
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
		if s.now().UnixMilli() >= documentExpiry {
			return ErrGone
		}
		return nil
	})
	if err != nil {
		return Projection{}, err
	}
	return out, nil
}
