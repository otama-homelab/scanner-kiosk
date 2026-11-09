package documents

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type JobProjection struct {
	ID            string  `json:"id"`
	DocumentID    string  `json:"document_id"`
	Kind          string  `json:"kind"`
	State         string  `json:"state"`
	InputRevision int64   `json:"input_revision"`
	ErrorCode     *string `json:"error_code"`
}

func (s *Store) ReadJob(ctx context.Context, owner, jobID string) (JobProjection, error) {
	var out JobProjection
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error { var err error; out, _, err = s.projectJob(ctx, tx, owner, jobID); return err })
	if err != nil {
		return JobProjection{}, err
	}
	return out, nil
}
func (s *Store) projectJob(ctx context.Context, tx *sql.Tx, owner, jobID string) (JobProjection, int64, error) {
	var out JobProjection
	var code sql.NullString
	var expiry int64
	err := tx.QueryRowContext(ctx, `SELECT j.id,j.document_id,j.kind,j.state,j.input_revision,j.error_code,d.expires_at_ms FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND (j.kind='cleanup' OR (d.expires_at_ms>? AND d.state NOT IN ('expired','deleting')))`, jobID, owner, s.now().UnixMilli()).Scan(&out.ID, &out.DocumentID, &out.Kind, &out.State, &out.InputRevision, &code, &expiry)
	if err != nil {
		return JobProjection{}, 0, readError(err)
	}
	if code.Valid {
		safe := code.String
		switch safe {
		case "failed", "interrupted", "cancelled", "not_started":
		default:
			if out.Kind != "import" || out.State != "failed" || !validImportFailure(safe) {
				safe = "processing_failed"
			}
		}
		out.ErrorCode = &safe
	}
	if out.Kind != "cleanup" && s.now().UnixMilli() >= expiry {
		return JobProjection{}, 0, ErrUnavailable
	}
	return out, expiry, nil
}

// CancelJob routes policy through Lifecycle; release remains worker-owned.
func (s *Store) CancelJob(ctx context.Context, owner, jobID, key string, retention time.Duration) (JobProjection, error) {
	return (&Lifecycle{records: s}).CancelJob(ctx, owner, jobID, key, retention)
}

// ApplyCancel records only a fenced cancellation intent, never a stop proof.
func (s *Store) ApplyCancel(ctx context.Context, owner string, command CancelCommand) (JobProjection, error) {
	jobID, key, retention, err := command.Request()
	if err != nil {
		return JobProjection{}, err
	}
	keyHash, digest := command.Hashes()
	var out JobProjection
	err = s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		if now < 0 {
			return ErrInvalid
		}
		var operation string
		var hash []byte
		var result sql.NullString
		var expiry int64
		err := tx.QueryRowContext(ctx, `SELECT operation,request_hash,result_job_id,expires_at_ms FROM request_receipts WHERE owner_id=? AND key_hash=?`, owner, keyHash[:]).Scan(&operation, &hash, &result, &expiry)
		if err == nil {
			if operation != "cancel" || !sameHash(hash, digest) {
				return ErrIdempotencyConflict
			}
			if !result.Valid || expiry <= now {
				return ErrGone
			}
			out, _, err = s.projectJob(ctx, tx, owner, result.String)
			if errors.Is(err, ErrUnavailable) {
				return ErrGone
			}
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ErrStorage
		}
		keyExpiry, err := s.freshRequestKey(ctx, tx, owner, key, now)
		if err != nil {
			return err
		}
		var documentExpiry int64
		out, documentExpiry, err = s.projectJob(ctx, tx, owner, jobID)
		if err != nil {
			return err
		}
		if out.Kind == "cleanup" || out.State != "queued" && out.State != "running" && out.State != "cancelling" {
			return ErrConflict
		}
		deadline, ok := addMillis(now, retention)
		if !ok || deadline < documentExpiry {
			return ErrInvalid
		}
		if _, err = tx.ExecContext(ctx, `UPDATE jobs SET state='cancelling' WHERE id=? AND owner_id=?`, jobID, owner); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,result_job_id,expires_at_ms,key_expires_at_ms) VALUES(?,?,'cancel',?,?,?,?)`, owner, keyHash[:], digest[:], jobID, deadline, keyExpiry); err != nil {
			return ErrStorage
		}
		out.State = "cancelling"
		if s.now().UnixMilli() >= documentExpiry {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return JobProjection{}, err
	}
	return out, nil
}
