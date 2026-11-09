package documents

import (
	"context"
	"database/sql"
	"errors"
)

// FindRender reconciles before current capabilities or space reservation.
func (s *Store) FindRender(ctx context.Context, owner string, request RenderAdmission) (Admission, bool, error) {
	return (&RenderAdmissionService{records: s}).FindRender(ctx, owner, request)
}

// LookupRender keeps receipt/fresh-key reconciliation under the enabled-owner
// guard. It does not depend on the current provider or reservation policy.
func (s *Store) LookupRender(ctx context.Context, owner string, intent RenderIntent) (Admission, bool, error) {
	request, err := intent.Request()
	if err != nil {
		return Admission{}, false, err
	}
	key, digest := intent.Hashes()
	var out Admission
	var found bool
	err = s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var err error
		out, found, err = s.renderReceipt(ctx, tx, owner, s.now().UnixMilli(), key, digest)
		if err == nil && !found {
			_, err = s.freshRequestKey(ctx, tx, owner, request.IdempotencyKey, s.now().UnixMilli())
		}
		return err
	})
	if err != nil {
		return Admission{}, false, err
	}
	return out, found, nil
}
func (s *Store) renderReceipt(ctx context.Context, tx *sql.Tx, owner string, now int64, keyHash, requestHash [32]byte) (Admission, bool, error) {
	var operation string
	var hash []byte
	var result sql.NullString
	var expiry int64
	err := tx.QueryRowContext(ctx, `SELECT operation,request_hash,result_job_id,expires_at_ms FROM request_receipts WHERE owner_id=? AND key_hash=?`, owner, keyHash[:]).Scan(&operation, &hash, &result, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return Admission{}, false, nil
	}
	if err != nil {
		return Admission{}, false, ErrStorage
	}
	if operation != "render" || !sameHash(hash, requestHash) {
		return Admission{}, true, ErrIdempotencyConflict
	}
	if !result.Valid || expiry <= now {
		return Admission{}, true, ErrGone
	}
	var out Admission
	err = tx.QueryRowContext(ctx, `SELECT j.id,j.document_id,j.input_revision FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.kind='render' AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting')`, result.String, owner, now).Scan(&out.JobID, &out.DocumentID, &out.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return Admission{}, true, ErrGone
	}
	if err != nil {
		return Admission{}, true, ErrStorage
	}
	out.Replay = true
	return out, true, nil
}
