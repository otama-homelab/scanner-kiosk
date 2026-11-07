package documents

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// RequestDelete routes policy through Lifecycle while retaining the existing API.
func (s *Store) RequestDelete(ctx context.Context, owner string, request DeleteRequest, retention time.Duration) (JobProjection, error) {
	return (&Lifecycle{records: s}).RequestDelete(ctx, owner, request, retention)
}

// ApplyDelete atomically revokes content reads and records cleanup intent. The
// worker must prove stop/publication outcome before any physical cleanup.
func (s *Store) ApplyDelete(ctx context.Context, owner string, command DeleteCommand) (JobProjection, error) {
	request, retention, err := command.Request()
	if err != nil {
		return JobProjection{}, err
	}
	key, digest := command.Hashes()
	var out JobProjection
	err = s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		deadline, ok := addMillis(now, retention)
		if now < 0 || !ok {
			return ErrInvalid
		}
		var operation string
		var hash []byte
		var result sql.NullString
		var receiptExpiry int64
		err := tx.QueryRowContext(ctx, `SELECT operation,request_hash,result_job_id,expires_at_ms FROM request_receipts WHERE owner_id=? AND key_hash=?`, owner, key[:]).Scan(&operation, &hash, &result, &receiptExpiry)
		if err == nil {
			if operation != "delete_document" || !sameHash(hash, digest) {
				return ErrIdempotencyConflict
			}
			if !result.Valid || receiptExpiry <= now {
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
		keyExpiry, err := s.freshRequestKey(ctx, tx, request.IdempotencyKey, now)
		if err != nil {
			return err
		}
		var revision, expiry int64
		var state string
		err = tx.QueryRowContext(ctx, `SELECT revision,expires_at_ms,state FROM documents WHERE id=? AND owner_id=? AND expires_at_ms>? AND state!='expired'`, request.DocumentID, owner, now).Scan(&revision, &expiry, &state)
		if err != nil {
			return readError(err)
		}
		if revision != request.Revision {
			return ErrRevision
		}
		if deadline < expiry {
			return ErrInvalid
		}
		var cleanupID string
		err = tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE document_id=? AND owner_id=? AND kind='cleanup'`, request.DocumentID, owner).Scan(&cleanupID)
		if errors.Is(err, sql.ErrNoRows) {
			cleanupID, err = enqueueCleanup(ctx, tx, owner, request.DocumentID, revision, now, deadline, "deleting")
		} else if err == nil {
			err = markForCleanup(ctx, tx, owner, request.DocumentID, "deleting")
		}
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,result_job_id,expires_at_ms,key_expires_at_ms) VALUES(?,?,'delete_document',?,?,?,?)`, owner, key[:], digest[:], cleanupID, deadline, keyExpiry); err != nil {
			return ErrStorage
		}
		if s.now().UnixMilli() >= expiry {
			return ErrUnavailable
		}
		out, _, err = s.projectJob(ctx, tx, owner, cleanupID)
		return err
	})
	if err != nil {
		return JobProjection{}, err
	}
	return out, nil
}

func enqueueCleanup(ctx context.Context, tx *sql.Tx, owner, document string, revision, now, deadline int64, state string) (string, error) {
	job, err := opaqueID("job_")
	if err != nil {
		return "", ErrStorage
	}
	if err = markForCleanup(ctx, tx, owner, document, state); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES(?,?,?,'cleanup','queued',?,'{"version":1}',?,?)`, job, document, owner, revision, now, deadline); err != nil {
		return "", ErrStorage
	}
	return job, nil
}

func markForCleanup(ctx context.Context, tx *sql.Tx, owner, document, state string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE documents SET state=? WHERE id=? AND owner_id=?`, state, document, owner); err != nil {
		return ErrStorage
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET state='cancelling' WHERE document_id=? AND owner_id=? AND kind!='cleanup' AND state IN ('queued','running')`, document, owner); err != nil {
		return ErrStorage
	}
	return nil
}

// Keep active acquisitions; failed or idle empty documents use the cleanup barrier.
const discardableEmptyDocumentSQL = `(d.state IN ('review','ready','failed')
 AND NOT EXISTS(SELECT 1 FROM pages p WHERE p.document_id=d.id AND p.owner_id=d.owner_id)
 AND (d.state='failed' OR NOT EXISTS(SELECT 1 FROM jobs j WHERE j.document_id=d.id AND j.owner_id=d.owner_id AND j.state IN ('queued','running','cancelling','reconciling'))))`

// QueueExpiredCleanup is worker-internal bounded discovery. Expired and disabled
// principals require no browser session to expire their content. The principal
// snapshot keeps the same state→documents lock order as public operations.
// Idle empty documents retire through the same stop barrier.
func (s *Store) QueueExpiredCleanup(ctx context.Context, limit int, retention time.Duration) (int, error) {
	if limit < 1 || limit > 100 || retention < time.Second || retention > 365*24*time.Hour {
		return 0, ErrInvalid
	}
	count := 0
	err := s.withPrincipalSnapshot(ctx, func(_ *sql.Conn, tx *sql.Tx) error {
		now := s.now().UnixMilli()
		deadline, ok := addMillis(now, retention)
		if now < 0 || !ok {
			return ErrInvalid
		}
		rows, err := tx.QueryContext(ctx, `SELECT d.id,d.owner_id,d.revision,CASE WHEN d.expires_at_ms<=? THEN 'expired' ELSE 'deleting' END FROM documents d WHERE (d.expires_at_ms<=? OR `+discardableEmptyDocumentSQL+`) AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.document_id=d.id AND j.kind='cleanup') ORDER BY d.expires_at_ms,d.id LIMIT ?`, now, now, limit)
		if err != nil {
			return storageOperationError(err)
		}
		type candidate struct {
			state           string
			document, owner string
			revision        int64
		}
		var candidates []candidate
		for rows.Next() {
			var c candidate
			if err = rows.Scan(&c.document, &c.owner, &c.revision, &c.state); err != nil {
				rows.Close()
				return storageOperationError(err)
			}
			candidates = append(candidates, c)
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil && closeErr != nil {
			return errors.Join(ErrStorage, err, closeErr)
		}
		if err != nil {
			return storageOperationError(err)
		}
		if closeErr != nil {
			return storageOperationError(closeErr)
		}
		for _, c := range candidates {
			if _, err = enqueueCleanup(ctx, tx, c.owner, c.document, c.revision, now, deadline, c.state); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
