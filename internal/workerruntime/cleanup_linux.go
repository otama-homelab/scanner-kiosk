//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"time"

	"scanner-kiosk/internal/artifactgc"
	"scanner-kiosk/internal/cleanupjob"
	"scanner-kiosk/internal/documents"
)

// Atomic maintenance commands retain backend transaction/owner/expiry fences.
// Counts report confirmed affected rows in [0, limit]; uncertainty returns an
// error and must never be converted into an empty batch or permission to scan.
type maintenanceRecords interface {
	QueueExpiredCleanup(context.Context, int, time.Duration) (int, error)
	AbortWithdrawnUnstartedScans(context.Context, int) (int, error)
	AbortUnstartedRenders(context.Context, int) (int, error)
	AbortUnstartedImports(context.Context, int) (int, error)
	PurgeCompletedCleanup(context.Context, int, time.Duration) (int, error)
	PurgeExpiredRequestReceipts(context.Context, int) (int, error)
}
type startupRecords interface {
	maintenanceRecords
	RecoveryRequired(context.Context) (bool, error)
}
type cleanupDispatchRecords interface {
	cleanupjob.Records
	NextCleanup(context.Context) (documents.CleanupCandidate, error)
	ClaimCleanupAttempt(context.Context, string, string, string, string, time.Duration) (documents.Job, error)
}

func reconcileStartup(ctx context.Context, records startupRecords, retention time.Duration) error {
	if err := maintainStartupCleanup(ctx, records, retention); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	readback, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	recovery, err := records.RecoveryRequired(readback)
	if err != nil {
		return err
	}
	if err = readback.Err(); err != nil {
		return err
	}
	if recovery {
		return ErrReconciliation
	}
	return nil
}

func dispatchCleanup(ctx context.Context, store cleanupDispatchRecords, root, boot string, maxBytes int64, budget time.Duration, check cleanupjob.CheckAuthority) (handled bool, result error) {
	if err := check(ctx); err != nil {
		return false, err
	}
	candidate, err := store.NextCleanup(ctx)
	if errors.Is(err, documents.ErrNoWork) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = store.ClaimCleanupAttempt(ctx, candidate.OwnerID, candidate.JobID, candidate.PreviousRunner, boot, budget)
	if errors.Is(err, documents.ErrCommitUncertain) {
		readback, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_, readErr := store.LoadCleanupWork(readback, candidate.OwnerID, candidate.JobID, boot, 1)
		cancel()
		if readErr != nil {
			return false, errors.Join(ErrReconciliation, err, readErr)
		}
	} else if err != nil {
		return false, err
	}
	if err = check(ctx); err != nil {
		return false, errors.Join(ErrReconciliation, err)
	}
	files, err := artifactgc.OpenRoot(root, maxBytes)
	if err != nil {
		return false, errors.Join(ErrReconciliation, err)
	}
	defer func() { result = errors.Join(result, files.Close()) }()
	if err = cleanupjob.ExecuteClaimed(ctx, store, files, check, candidate.OwnerID, candidate.JobID, boot, time.Second); err != nil {
		return true, errors.Join(ErrReconciliation, err)
	}
	return true, nil
}

// Finish bounded metadata maintenance under the still-owned guard even if stop
// arrives mid-transaction. Only confirmed success becomes exact cancellation;
// timeout/storage/commit uncertainty remains a failure. No file IO occurs here.
func maintainCleanup(ctx context.Context, store maintenanceRecords, retention time.Duration) error {
	_, err := maintainCleanupBatch(ctx, store, retention)
	return err
}

// Before native reconciliation, drain bounded metadata batches. A full batch is
// not evidence that all old unstarted scans were handled. No native/file work is
// performed here; confirmed commits survive cancellation between batches.
func maintainStartupCleanup(ctx context.Context, store maintenanceRecords, retention time.Duration) error {
	for {
		more, err := maintainCleanupBatch(ctx, store, retention)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
}

func maintainCleanupBatch(ctx context.Context, store maintenanceRecords, retention time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	maintenance, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	expired, err := store.QueueExpiredCleanup(maintenance, 20, retention)
	if err != nil {
		return false, err
	}
	if expired < 0 || expired > 20 {
		return false, documents.ErrStorage
	}
	scans, err := store.AbortWithdrawnUnstartedScans(maintenance, 100)
	if err != nil {
		return false, err
	}
	if scans < 0 || scans > 100 {
		return false, documents.ErrStorage
	}
	renders, err := store.AbortUnstartedRenders(maintenance, 100)
	if err != nil {
		return false, err
	}
	if renders < 0 || renders > 100 {
		return false, documents.ErrStorage
	}
	imports, err := store.AbortUnstartedImports(maintenance, 100)
	if err != nil {
		return false, err
	}
	if imports < 0 || imports > 100 {
		return false, documents.ErrStorage
	}
	retired, err := store.PurgeCompletedCleanup(maintenance, 100, retention)
	if err != nil {
		return false, err
	}
	if retired < 0 || retired > 100 {
		return false, documents.ErrStorage
	}
	requests, err := store.PurgeExpiredRequestReceipts(maintenance, 100)
	if err != nil {
		return false, err
	}
	if requests < 0 || requests > 100 {
		return false, documents.ErrStorage
	}
	return requests == 100 || expired == 20 || scans == 100 || renders == 100 || imports == 100 || retired == 100, ctx.Err()
}
