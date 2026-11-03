//go:build linux

// Package workerruntime starts the local worker. Initial dispatch supports plain
// PDF, photo, OCR renders and boot-pinned admitted scans; readiness and restart recovery are
// unfinished and this runtime must not be deployed as a scanner replacement yet.
package workerruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/diskspace"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/renderjob"
	"scanner-kiosk/internal/scannerdriver"
	"scanner-kiosk/internal/scanthumbnail"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/workercatalog"
	"scanner-kiosk/internal/workeridentity"
)

var ErrReconciliation = errors.New("worker recovery required before accepting more work")
var ErrRoot = errors.New("worker storage roots must be existing private directories")

// Run acquires one boot identity for the whole lifecycle. Once processes at most
// one eligible job; otherwise it polls until cancellation. Scans require explicit
// acquisition opt-in and a never-started admission pinned to this fresh boot.
func Run(ctx context.Context, c config.Config, once bool) (result error) {
	if c.Role != config.Worker {
		return configError()
	}
	for _, root := range []string{c.StateDir, c.DocumentDir} {
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return ErrRoot
		}
	}
	guard, err := workeridentity.Acquire(c.DocumentDir)
	if err != nil {
		return err
	}
	defer guard.Close()
	state, err := storage.Open(filepath.Join(c.StateDir, "state.sqlite3"), storage.State)
	if err != nil {
		return err
	}
	defer state.Close()
	docs, err := storage.Open(filepath.Join(c.DocumentDir, "documents.sqlite3"), storage.Documents)
	if err != nil {
		return err
	}
	defer docs.Close()
	meter, err := diskspace.Open(c.DocumentDir, c.Limits["SCANNER_MIN_FREE_BYTES"])
	if err != nil {
		return err
	}
	defer meter.Close()
	store, err := documents.New(state, docs, time.Now, meter.Available)
	if err != nil {
		return err
	}
	catalogStore, err := workercatalog.New(docs, time.Now, c.Durations["SCANNER_OCR_CATALOG_LEASE_SECONDS"])
	if err != nil {
		return err
	}
	// Revoke stale admission metadata before inspecting old work. A fresh local
	// guard does not invalidate the predecessor's still-unexpired DB publication.
	withdrawal, stopWithdrawal := context.WithTimeout(ctx, 5*time.Second)
	err = catalogStore.WithdrawPrior(withdrawal, guard)
	stopWithdrawal()
	if err != nil {
		return errors.Join(ErrCatalog, err, ctx.Err())
	}
	if err = reconcileStartup(ctx, store, c.Durations["SCANNER_RECEIPT_RETENTION_SECONDS"]); err != nil {
		return err
	}
	startupBoot := guard.ID()
	checkStartupDirectory := func(current context.Context) error {
		if err := current.Err(); err != nil {
			return err
		}
		if !guard.MatchesRoot(c.DocumentDir) {
			return ErrReconciliation
		}
		return guard.WithActive(func(boot string) error {
			if boot != startupBoot {
				return ErrReconciliation
			}
			return nil
		})
	}
	if err = sweepStartupDirectories(ctx, c.DocumentDir, c.Limits["SCANNER_JOB_MAX_BYTES"], c.Durations["SCANNER_CLEANUP_TIMEOUT_SECONDS"], checkStartupDirectory); err != nil {
		return err
	}
	// Explicit operator opt-in is required before wiring native scanner resources.
	// It does not itself prove exclusive hardware control or establish readiness.
	var scannerSession *scannerdriver.Session
	if c.ScannerTools.AcquisitionEnabled {
		session, configureErr := scannerdriver.Configure(ctx, c, guard)
		if session != nil {
			defer func() {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.Durations["SCANNER_PROCESS_STOP_GRACE_SECONDS"]+5*time.Second)
				defer cancel()
				if closeErr := session.Close(cleanup); closeErr != nil {
					result = errors.Join(result, ErrReconciliation, closeErr)
				}
			}()
		}
		if configureErr != nil {
			return configureErr
		}
		scannerSession = session
	}
	processing := &processingRecords{renderExecutionRecords: store, config: c}
	defer processing.close()
	ctx, cancelWorker := context.WithCancel(ctx)
	defer cancelWorker()
	publisher, err := startCatalogPublisher(ctx, cancelWorker, catalogStore, guard, processing, c.Durations["SCANNER_OCR_CATALOG_LEASE_SECONDS"])
	if err != nil {
		return err
	}
	if scannerSession != nil {
		ids := make([]string, 0, len(c.Scanners))
		for _, device := range c.Scanners {
			ids = append(ids, device.ID)
		}
		publisher.startScannerMetadata(scannerSession, ids, c.Durations["SCANNER_OCR_CATALOG_LEASE_SECONDS"])
	}
	defer func() {
		if err := publisher.close(); err != nil {
			if result == nil {
				result = err
			} else if !errors.Is(result, err) {
				result = errors.Join(result, err)
			}
		}
	}()
	options := pdfassemble.Options{MaxInputBytes: c.Limits["SCANNER_JOB_MAX_BYTES"], MaxOutputBytes: c.Limits["SCANNER_JOB_MAX_BYTES"], MaxPagePixels: c.Limits["SCANNER_MAX_PAGE_PIXELS"], MaxPages: int(c.Limits["SCANNER_JOB_MAX_PAGES"]), JPEGQuality: 92}
	scanLimit := c.EffectiveScanConcurrency()
	if scanLimit < 1 {
		return configError()
	}
	tasks := newScanTasks(ctx, scanLimit, func(taskCtx context.Context, claimed documents.ClaimedScan) error {
		return executeScan(taskCtx, scanthumbnail.New(store, c.DocumentDir, c.Limits["SCANNER_JOB_MAX_BYTES"], c.Limits["SCANNER_MAX_PAGE_PIXELS"]), scannerSession, guard.ID(), claimed)
	})
	defer func() {
		if err := tasks.close(); err != nil {
			if result == nil {
				result = err
			} else if !errors.Is(result, err) {
				result = errors.Join(result, err)
			}
		}
	}()
	cleanupBoot := guard.ID()
	checkCleanup := func(current context.Context) error {
		if err := current.Err(); err != nil {
			return err
		}
		if err := publisher.failure(); err != nil {
			return err
		}
		if !guard.MatchesRoot(c.DocumentDir) {
			return ErrReconciliation
		}
		return guard.WithActive(func(boot string) error {
			if boot != cleanupBoot {
				return ErrReconciliation
			}
			return nil
		})
	}
	for {
		if err := publisher.failure(); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := tasks.poll(); err != nil {
			return err
		}
		if tasks.ctx.Err() != nil {
			return tasks.ctx.Err()
		}
		scanned := false
		if scannerSession != nil {
			for tasks.active < tasks.limit {
				if err := tasks.poll(); err != nil {
					return err
				}
				if tasks.ctx.Err() != nil {
					return tasks.ctx.Err()
				}
				claimed, claimErr := store.ClaimNextScanWithLimit(tasks.ctx, guard.ID(), scanLimit)
				if errors.Is(claimErr, documents.ErrNoWork) {
					break
				}
				if claimErr != nil {
					if claimErr == tasks.ctx.Err() {
						return claimErr
					}
					return errors.Join(claimErr, tasks.ctx.Err())
				}
				tasks.start(claimed)
				scanned = true
				if once {
					break
				}
			}
		}
		if !scanned && tasks.active == 0 {
			if err := maintainCleanup(ctx, store, c.Durations["SCANNER_RECEIPT_RETENTION_SECONDS"]); err != nil {
				return err
			}
			cleaned, cleanupErr := dispatchCleanup(ctx, store, c.DocumentDir, cleanupBoot, c.Limits["SCANNER_JOB_MAX_BYTES"], c.Durations["SCANNER_CLEANUP_TIMEOUT_SECONDS"], checkCleanup)
			if cleanupErr != nil {
				return cleanupErr
			}
			if cleaned {
				if once {
					return tasks.poll()
				}
				continue
			}
			claimed, claimErr := store.ClaimNextRender(ctx, guard.ID())
			if claimErr != nil && !errors.Is(claimErr, documents.ErrNoWork) {
				// Preserve both cancellation and storage/claim uncertainty; never turn
				// an interrupted transaction into a clean cancellation.
				if claimErr == ctx.Err() {
					return claimErr
				}
				return errors.Join(claimErr, ctx.Err())
			}
			if claimErr == nil {
				recovery, executionErr := renderjob.ExecuteClaimed(ctx, processing, c.DocumentDir, claimed.OwnerID, claimed.Job.ID, guard.ID(), options, time.Second)
				if recovery != nil {
					return errors.Join(ErrReconciliation, executionErr)
				} // preserve unresolved files; never Close blindly.
				if executionErr != nil {
					cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					recorded, readErr := store.RenderStoppedRecorded(cleanup, claimed.OwnerID, claimed.Job.ID, guard.ID())
					cancel()
					if readErr != nil || !recorded {
						return errors.Join(ErrReconciliation, executionErr, readErr)
					}
				}
			}
		}
		if err := publisher.failure(); err != nil {
			return err
		}
		if once {
			if tasks.active > 0 {
				select {
				case err := <-tasks.results:
					tasks.record(err)
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return tasks.poll()
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			if err := publisher.failure(); err != nil {
				return err
			}
			return ctx.Err()
		case err := <-tasks.results:
			timer.Stop()
			tasks.record(err)
			if tasks.err != nil {
				return tasks.err
			}
		case <-timer.C:
		}
	}
}
func configError() error { return errors.New("worker role configuration required") }
