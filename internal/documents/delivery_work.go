package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
)

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
