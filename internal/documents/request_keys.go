package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"

	"scanner-kiosk/internal/requestkey"
)

func (s *Store) freshRequestKey(ctx context.Context, tx *sql.Tx, owner, key string, now int64) (any, error) {
	hash := sha256.Sum256([]byte(key))
	var used int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM delivery_receipts WHERE owner_id=? AND key_hash=?)`, owner, hash[:]).Scan(&used); err != nil {
		return nil, ErrStorage
	}
	if used != 0 {
		return nil, ErrIdempotencyConflict
	}
	_, aged, err := requestkey.Expiry(key)
	if err != nil {
		return nil, ErrInvalid
	}
	if !aged {
		return nil, nil
	}
	var floor int64
	if err = tx.QueryRowContext(ctx, "SELECT retired_at_ms FROM request_retirement_fence WHERE singleton=1").Scan(&floor); err != nil {
		return nil, ErrStorage
	}
	expiry, _, err := requestkey.ValidateNew(key, now, floor)
	switch {
	case errors.Is(err, requestkey.ErrExpired):
		return nil, ErrGone
	case errors.Is(err, requestkey.ErrClock):
		return nil, ErrStorage
	case err != nil:
		return nil, ErrInvalid
	}
	return expiry, nil
}

// PurgeExpiredRequestReceipts retires only proven-aged keys after both the key
// and receipt deadlines. The same transaction advances a durable time floor so
// clock rollback can never re-admit a deleted key. Legacy hashes remain intact.
func (s *Store) PurgeExpiredRequestReceipts(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, ErrInvalid
	}
	now := s.now().UnixMilli()
	if now < 0 {
		return 0, ErrInvalid
	}
	tx, err := s.documents.BeginTx(ctx, nil)
	if err != nil {
		return 0, storageOperationError(err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT owner_id,key_hash FROM request_receipts INDEXED BY request_receipts_aged_retirement WHERE key_expires_at_ms IS NOT NULL AND expires_at_ms<=? AND key_expires_at_ms<=? ORDER BY expires_at_ms,key_expires_at_ms,owner_id,key_hash LIMIT ?`, now, now, limit)
	if err != nil {
		return 0, storageOperationError(err)
	}
	type target struct {
		owner string
		hash  []byte
	}
	var targets []target
	for rows.Next() {
		var r target
		if err = rows.Scan(&r.owner, &r.hash); err != nil {
			rows.Close()
			return 0, ErrStorage
		}
		if len(r.hash) != 32 {
			rows.Close()
			return 0, ErrStorage
		}
		targets = append(targets, r)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil || closeErr != nil {
		return 0, errors.Join(ErrStorage, err, closeErr)
	}
	if len(targets) == 0 {
		return 0, nil
	}
	// Retain the maximum fence even if this cleanup clock moved backwards.
	result, err := tx.ExecContext(ctx, "UPDATE request_retirement_fence SET retired_at_ms=max(retired_at_ms,?) WHERE singleton=1", now)
	if err != nil {
		return 0, ErrStorage
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return 0, ErrStorage
	}
	for _, r := range targets {
		if _, err = tx.ExecContext(ctx, "DELETE FROM request_receipts WHERE owner_id=? AND key_hash=?", r.owner, r.hash); err != nil {
			return 0, ErrStorage
		}
	}
	if err = s.commit(tx); err != nil {
		return 0, ErrCommitUncertain
	}
	return len(targets), nil
}
