//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"time"

	"scanner-kiosk/internal/artifactgc"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/importupload"
	"scanner-kiosk/internal/scanner/escl"
	"scanner-kiosk/internal/workercatalog"
)

var ErrRecoveryConfirmation = errors.New("offline recovery requires confirmation that the previous runtime and all native/provider child processes have been terminated")

// Recover retains existing documents and never repeats an interrupted scan.
// previousRuntimeStopped is an operator assertion at the administrative CLI,
// NOT a proof inferred from acquiring flock, a dead worker, or printer Idle.
// Stop the whole old container/pod/process namespace before invoking it.
func Recover(ctx context.Context, c config.Config, previousRuntimeStopped bool) (result error) {
	if !previousRuntimeStopped {
		return ErrRecoveryConfirmation
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	resources, err := openWorkerStorage(c)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, resources.close()) }()
	guard, store := resources.guard, resources.store
	boot := guard.ID()
	stoppedAt := time.Now().UnixMilli()
	check := func(current context.Context) error {
		if err := current.Err(); err != nil {
			return err
		}
		if !guard.MatchesRoot(c.DocumentDir) {
			return ErrReconciliation
		}
		return guard.WithActive(func(id string) error {
			if id != boot {
				return ErrReconciliation
			}
			return nil
		})
	}
	catalog, err := workercatalog.New(resources.docs, time.Now, c.Durations["SCANNER_OCR_CATALOG_LEASE_SECONDS"])
	if err != nil {
		return err
	}
	if err = catalog.WithdrawPrior(ctx, guard); err != nil {
		return err
	}
	if err = maintainStartupCleanup(ctx, store, c.Durations["SCANNER_RECEIPT_RETENTION_SECONDS"]); err != nil {
		return err
	}
	scans, err := store.RecoveryScans(ctx)
	if err != nil {
		return err
	}
	renders, err := store.RecoveryRenders(ctx)
	if err != nil {
		return err
	}
	imports, err := store.RecoveryImports(ctx)
	if err != nil {
		return err
	}
	for _, item := range imports {
		if item.RunnerID == boot || item.StartedAtMS > stoppedAt || item.CreatedAtMS > stoppedAt {
			return ErrReconciliation
		}
	}
	for _, scan := range scans {
		if scan.Snapshot.PhysicalID == "" || scan.Snapshot.WorkerBootID == boot || scan.StartedAtMS > stoppedAt || scan.CreatedAtMS > stoppedAt {
			return ErrReconciliation
		}
	}
	for _, render := range renders {
		if render.RunnerID == boot || render.StartedAtMS > stoppedAt {
			return ErrReconciliation
		}
	}
	cleanup, cancel := context.WithTimeout(ctx, c.Durations["SCANNER_CLEANUP_TIMEOUT_SECONDS"])
	files, err := artifactgc.OpenRoot(c.DocumentDir, c.Limits["SCANNER_JOB_MAX_BYTES"])
	if err != nil {
		cancel()
		return err
	}
	err = files.RecoverUnpublished(cleanup, store.RecoveryDirectoryUnreferenced, check)
	err = errors.Join(err, files.Close())
	cancel()
	if err != nil {
		return err
	}
	cleanedAt := time.Now().UnixMilli()
	if cleanedAt < stoppedAt {
		return ErrReconciliation
	}
	var observer *escl.Observer
	if len(scans) > 0 {
		devices := make([]escl.Device, 0, len(c.Scanners))
		for _, device := range c.Scanners {
			devices = append(devices, escl.Device{ID: device.ID, Endpoint: device.Endpoint})
		}
		observer, err = escl.New(devices, escl.Options{Timeout: 5 * time.Second, MaxResponseBytes: 256 << 10})
		if err != nil {
			return err
		}
	}
	for _, scan := range scans {
		if err = check(ctx); err != nil {
			return err
		}
		identity, observeErr := observer.ObserveIdentity(ctx, scan.Snapshot.DeviceID)
		if observeErr != nil || identity.PhysicalID != scan.Snapshot.PhysicalID {
			return errors.Join(ErrReconciliation, observeErr)
		}
		idle, observeErr := observer.ObserveIdle(ctx, scan.Snapshot.DeviceID, stoppedAt)
		if observeErr != nil {
			return errors.Join(ErrReconciliation, observeErr)
		}
		identity, observeErr = observer.ObserveIdentity(ctx, scan.Snapshot.DeviceID)
		if observeErr != nil || identity.PhysicalID != scan.Snapshot.PhysicalID {
			return errors.Join(ErrReconciliation, observeErr)
		}
		if err = check(ctx); err != nil {
			return err
		}
		stop := documents.ScanStop{DeviceID: scan.Snapshot.DeviceID, WorkerBootID: scan.Snapshot.WorkerBootID, LeaseEpoch: scan.Snapshot.LeaseEpoch, ProcessStoppedAtMS: stoppedAt, IdleObservedAtMS: idle}
		if err = store.FinishStoppedScan(ctx, scan.OwnerID, scan.JobID, stop, cleanedAt, "interrupted"); err != nil {
			return err
		}
	}
	for _, render := range renders {
		if err = check(ctx); err != nil {
			return err
		}
		stop := documents.RenderStop{RunnerID: render.RunnerID, ProcessStoppedAtMS: stoppedAt, WorkspaceCleanedAtMS: cleanedAt}
		if err = store.FinishStoppedRender(ctx, render.OwnerID, render.JobID, stop, "interrupted"); err != nil {
			return err
		}
	}
	for _, item := range imports {
		if err = check(ctx); err != nil {
			return err
		}
		if item.IntakeState == "ready" {
			stop := documents.ImportStop{RunnerID: item.RunnerID, ProcessStoppedAtMS: stoppedAt, WorkspaceCleanedAtMS: cleanedAt}
			if err = store.FinishStoppedImport(ctx, item.OwnerID, item.JobID, stop, "interrupted"); err != nil {
				return err
			}
		} else if err = importupload.RecoverStopped(ctx, c.DocumentDir, store, item, stoppedAt); err != nil {
			return err
		}
	}
	if required, err := store.RecoveryRequired(ctx); err != nil {
		return err
	} else if required {
		return ErrReconciliation
	}
	return check(ctx)
}
