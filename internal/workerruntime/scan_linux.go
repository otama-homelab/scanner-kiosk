//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanjob"
)

type scanDispatchRecords interface {
	scanjob.Records
	ClaimNextScan(context.Context, string) (documents.ClaimedScan, error)
}

// dispatchScan claims at most one never-started job pinned to this live boot.
// An ordinary terminalized failure counts as handled; an unresolved outcome
// stops the worker. No driver invocation follows an uncertain claim commit.
func dispatchScan(ctx context.Context, store scanDispatchRecords, driver scanjob.Driver, boot string) (bool, error) {
	if driver == nil {
		return false, nil
	}
	claimed, err := store.ClaimNextScan(ctx, boot)
	if errors.Is(err, documents.ErrNoWork) {
		return false, nil
	}
	if err != nil {
		return false, errors.Join(err, ctx.Err())
	}
	return true, executeScan(ctx, store, driver, boot, claimed)
}

func executeScan(ctx context.Context, store scanjob.Records, driver scanjob.Driver, boot string, claimed documents.ClaimedScan) error {
	recovery, executionErr := scanjob.ExecuteClaimed(ctx, store, driver, claimed.OwnerID, claimed.Job.ID, boot, time.Second)
	if recovery != nil {
		return errors.Join(ErrReconciliation, executionErr)
	}
	if executionErr != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		recorded, readErr := store.ScanStoppedRecorded(cleanup, claimed.OwnerID, claimed.Job.ID)
		if readErr != nil || !recorded {
			return errors.Join(ErrReconciliation, executionErr, readErr)
		}
	}
	return nil
}
