//go:build linux

package workercatalog

import (
	"context"
	"database/sql"
	"errors"

	"scanner-kiosk/internal/workeridentity"
)

// WithdrawPrior closes the predecessor's metadata admission window before
// startup maintenance/recovery. The caller holds the successor's active guard
// on this document volume. This publishes no successor authority and neither
// stops native work nor changes jobs, leases, reservations or generation.
// It cannot withdraw a coordinator already begun by this same guard.
func (s *Store) WithdrawPrior(ctx context.Context, guard *workeridentity.Guard) error {
	if guard == nil {
		return ErrUnavailable
	}
	err := guard.WithActive(func(boot string) error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrUnavailable
		}
		defer tx.Rollback()
		var previous string
		err = tx.QueryRowContext(ctx, "SELECT boot_id FROM processing_coordinator WHERE singleton=1").Scan(&previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) || previous == boot {
			return ErrUnavailable
		}
		if _, err = tx.ExecContext(ctx, "UPDATE processing_coordinator SET state='stopped' WHERE singleton=1; DELETE FROM ocr_catalog_snapshot; DELETE FROM scanner_metadata_snapshot; DELETE FROM scanner_readiness"); err != nil {
			return ErrUnavailable
		}
		if err = tx.Commit(); err != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return ErrUnavailable
	}
	return nil
}
