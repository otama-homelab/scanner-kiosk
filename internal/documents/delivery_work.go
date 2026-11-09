package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
)

type DeliveryCandidate struct{ OwnerID, DeliveryID string }

// ReadCurrentDelivery restores the semantic ledger for the current reviewed payload.
func (s *Store) ReadCurrentDelivery(ctx context.Context, owner, documentID, destination string) (Delivery, error) {
	var out Delivery
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var deliveryID string
		err := tx.QueryRowContext(ctx, `SELECT x.id FROM documents d JOIN published_outputs p ON p.document_id=d.id AND p.owner_id=d.owner_id AND p.revision=d.revision JOIN artifacts a ON a.id=p.artifact_id AND a.owner_id=d.owner_id AND a.document_id=d.id JOIN deliveries x ON x.owner_id=d.owner_id AND x.destination_id=? AND x.payload_sha256=a.sha256 WHERE d.id=? AND d.owner_id=? AND d.expires_at_ms>? AND d.state='ready' AND a.state='ready' AND x.expires_at_ms>? AND x.state!='retired'`, destination, documentID, owner, s.now().UnixMilli(), s.now().UnixMilli()).Scan(&deliveryID)
		if err != nil {
			return readError(err)
		}
		out, err = loadDelivery(ctx, tx, owner, deliveryID)
		return err
	})
	if err != nil {
		return Delivery{}, err
	}
	return out, nil
}

// NextDelivery observes fixed-destination work; ProcessDelivery rechecks authority.
// No normal job/active index is involved. Poll reservations survive worker restarts.
func (s *Store) NextDelivery(ctx context.Context, destination, fingerprint string) (DeliveryCandidate, error) {
	var out DeliveryCandidate
	err := s.withPrincipalSnapshot(ctx, func(principals *sql.Conn, tx *sql.Tx) error {
		now := s.now().UnixMilli()
		rows, err := tx.QueryContext(ctx, `SELECT x.owner_id,x.id FROM deliveries x JOIN documents d ON d.id=x.document_id AND d.owner_id=x.owner_id
 WHERE x.destination_id=? AND x.config_fingerprint=? AND x.expires_at_ms>? AND d.expires_at_ms>? AND d.state NOT IN ('deleting','expired')
 AND ((x.state='queued' AND d.state='ready' AND d.revision=x.revision AND EXISTS(
 SELECT 1 FROM published_outputs p JOIN artifacts a ON a.id=p.artifact_id AND a.document_id=p.document_id AND a.owner_id=p.owner_id
 WHERE p.document_id=d.id AND p.owner_id=d.owner_id AND p.revision=x.revision AND a.id=x.artifact_id AND a.state='ready' AND a.sha256=x.payload_sha256))
 OR x.state='attempted' OR (x.state='accepted' AND x.task_id IS NOT NULL AND x.poll_count<=20) OR (x.state='unknown' AND x.task_id IS NOT NULL AND x.poll_count<20))
 ORDER BY CASE x.state WHEN 'queued' THEN 0 WHEN 'attempted' THEN 1 ELSE 2 END,x.poll_count,x.created_at_ms,x.id`, destination, fingerprint, now, now)
		if err != nil {
			return storageOperationError(err)
		}
		defer rows.Close()
		for rows.Next() {
			if err = rows.Scan(&out.OwnerID, &out.DeliveryID); err != nil {
				return ErrStorage
			}
			var enabled int
			err = principals.QueryRowContext(ctx, "SELECT 1 FROM principals WHERE id=? AND disabled_at_ms IS NULL", out.OwnerID).Scan(&enabled)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return storageOperationError(err)
			}
			return nil
		}
		if err = rows.Err(); err != nil {
			return storageOperationError(err)
		}
		return ErrNoWork
	})
	if err != nil {
		return DeliveryCandidate{}, err
	}
	return out, nil
}

// ClaimDelivery is a fresh, single attempt. Readback never grants upload authority.
func (s *Store) ClaimDelivery(ctx context.Context, owner, deliveryID string) (Delivery, error) {
	runner, err := opaqueID("delivery_runner_")
	if err != nil {
		return Delivery{}, ErrStorage
	}
	return s.ClaimDeliveryAttempt(ctx, owner, deliveryID, runner)
}

// LoadDeliveryAttempt reconciles only the exact known attempt after commit loss.
func (s *Store) LoadDeliveryAttempt(ctx context.Context, owner, deliveryID, runner string) (Delivery, error) {
	d, err := s.ReadDelivery(ctx, owner, deliveryID)
	if err != nil {
		return Delivery{}, err
	}
	if d.RunnerID != runner || d.AttemptedAtMS == 0 {
		return Delivery{}, ErrConflict
	}
	return d, nil
}

// FindDelivery reads an existing receipt and never admits work after uncertainty.
func (s *Store) FindDelivery(ctx context.Context, owner, key string) (Delivery, error) {
	if !id(key) {
		return Delivery{}, ErrInvalid
	}
	hash := sha256.Sum256([]byte(key))
	var d Delivery
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var deliveryID string
		var expiry int64
		if err := tx.QueryRowContext(ctx, `SELECT delivery_id,expires_at_ms FROM delivery_receipts WHERE owner_id=? AND key_hash=?`, owner, hash[:]).Scan(&deliveryID, &expiry); err != nil {
			return readError(err)
		}
		if expiry <= s.now().UnixMilli() {
			return ErrGone
		}
		var err error
		d, err = loadDelivery(ctx, tx, owner, deliveryID)
		if err != nil {
			return err
		}
		if d.State == "retired" || d.ExpiresAtMS <= s.now().UnixMilli() {
			return ErrGone
		}
		var live int
		return readError(tx.QueryRowContext(ctx, `SELECT 1 FROM documents WHERE id=? AND owner_id=? AND expires_at_ms>? AND state NOT IN ('deleting','expired')`, d.DocumentID, owner, s.now().UnixMilli()).Scan(&live))
	})
	if err != nil {
		return Delivery{}, err
	}
	return d, nil
}

func (s *Store) FailDelivery(ctx context.Context, owner, deliveryID, taskID string) error {
	if !deliveryUUID(taskID) {
		return ErrInvalid
	}
	return s.updateDelivery(ctx, owner, deliveryID, `UPDATE deliveries SET state='failed' WHERE id=? AND owner_id=? AND task_id=? AND state IN ('accepted','unknown','failed')`, deliveryID, owner, taskID)
}

// ReserveDeliveryPoll bounds automatic remote reads across restarts and workers.
func (s *Store) ReserveDeliveryPoll(ctx context.Context, owner, deliveryID string) (bool, error) {
	var reserved bool
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		d, err := loadDelivery(ctx, tx, owner, deliveryID)
		if err != nil {
			return err
		}
		if d.ExpiresAtMS <= s.now().UnixMilli() {
			return ErrGone
		}
		var live int
		if err = tx.QueryRowContext(ctx, `SELECT 1 FROM documents WHERE id=? AND owner_id=? AND expires_at_ms>? AND state NOT IN ('deleting','expired')`, d.DocumentID, owner, s.now().UnixMilli()).Scan(&live); err != nil {
			return readError(err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE deliveries SET poll_count=poll_count+1 WHERE id=? AND owner_id=? AND state IN ('accepted','unknown') AND task_id IS NOT NULL AND poll_count<20`, deliveryID, owner)
		if err != nil {
			return ErrStorage
		}
		n, err := result.RowsAffected()
		if err != nil {
			return ErrStorage
		}
		reserved = n == 1
		if !reserved && (d.State == "accepted" || d.State == "unknown") {
			_, err = tx.ExecContext(ctx, `UPDATE deliveries SET state='unknown' WHERE id=? AND owner_id=?`, deliveryID, owner)
			if err != nil {
				return ErrStorage
			}
		}
		return nil
	})
	return reserved && err == nil, err
}

// CancelDelivery serializes local cancellation with the attempted marker.
// An attempted delivery is never rolled back or presented as remotely cancelled.
func (s *Store) CancelDelivery(ctx context.Context, owner, deliveryID string) (Delivery, error) {
	var d Delivery
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var err error
		d, err = loadDelivery(ctx, tx, owner, deliveryID)
		if err != nil {
			return err
		}
		if d.ExpiresAtMS <= s.now().UnixMilli() || d.State == "retired" {
			return ErrGone
		}
		var live int
		if err = tx.QueryRowContext(ctx, `SELECT 1 FROM documents WHERE id=? AND owner_id=? AND expires_at_ms>? AND state NOT IN ('deleting','expired')`, d.DocumentID, owner, s.now().UnixMilli()).Scan(&live); err != nil {
			return readError(err)
		}
		if d.State == "queued" {
			if _, err = tx.ExecContext(ctx, `UPDATE deliveries SET state='cancelled' WHERE id=? AND owner_id=? AND state='queued'`, deliveryID, owner); err != nil {
				return ErrStorage
			}
			d.State = "cancelled"
		}
		return nil
	})
	if err != nil {
		return Delivery{}, err
	}
	return d, nil
}
