//go:build linux

// Package workerruntime starts the local worker. Initial dispatch supports plain
// PDF, photo, OCR renders and boot-pinned admitted scans; readiness and restart recovery are
// unfinished and this runtime must not be deployed as a scanner replacement yet.
package workerruntime

import (
	"context"
	"errors"
	"time"

	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/paperless"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/renderjob"
	"scanner-kiosk/internal/scannerdriver"
	"scanner-kiosk/internal/scanthumbnail"
	"scanner-kiosk/internal/workercatalog"
)

var ErrReconciliation = errors.New("worker recovery required before accepting more work")
var ErrRoot = errors.New("worker storage roots must be existing private directories")

// Run acquires one boot identity for the whole lifecycle. Once processes at most
// one eligible job and one independent delivery step; otherwise it polls until cancellation. Scans require explicit
// acquisition opt-in and a never-started admission pinned to this fresh boot.
func Run(ctx context.Context, c config.Config, once bool) (result error) {
	resources, err := openWorkerStorage(c)
	if err != nil {
		return err
	}
	defer resources.close()
	guard, docs, store := resources.guard, resources.docs, resources.store
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
	if err = sweepStartupDirectories(ctx, store, c.DocumentDir, c.Limits["SCANNER_JOB_MAX_BYTES"], c.Durations["SCANNER_CLEANUP_TIMEOUT_SECONDS"], checkStartupDirectory); err != nil {
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
		if c.ScannerDiscovery {
			publisher.startScannerDiscovery(scannerSession)
		}
		publisher.startScannerMetadata(scannerSession, ids, c.Durations["SCANNER_OCR_CATALOG_LEASE_SECONDS"])
		publisher.startScannerReadiness(scannerSession, ids, c.EffectiveScanConcurrency(), c.Durations["SCANNER_OCR_CATALOG_LEASE_SECONDS"])
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
	deliveryClient, err := c.Paperless.NewClient()
	if err != nil {
		return configError()
	}
	if deliveryClient != nil {
		defer deliveryClient.Close()
		root, err := artifactread.OpenRoot(c.DocumentDir, paperless.MaxPDFBytes)
		if err != nil {
			return err
		}
		defer root.Close()
		deliveryCtx, stopDelivery := context.WithCancel(ctx)
		deliveryDone := make(chan error, 1)
		go func() {
			err := runDeliveries(deliveryCtx, store, root, deliveryClient, once)
			if err != nil && deliveryCtx.Err() == nil {
				cancelWorker()
			}
			deliveryDone <- err
		}()
		defer func() {
			if !once || ctx.Err() != nil {
				stopDelivery()
			}
			err := <-deliveryDone
			stopDelivery()
			if err != nil && !errors.Is(err, context.Canceled) {
				result = errors.Join(result, err)
			}
		}()
	}
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
			// Cancellation can precede claim even while another device occupies a slot.
			maintenance, stopMaintenance := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_, abortErr := store.AbortWithdrawnUnstartedScans(maintenance, 100)
			stopMaintenance()
			if abortErr != nil {
				return abortErr
			}
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
			imported, importErr := store.ClaimNextImport(ctx, guard.ID())
			if importErr != nil && !errors.Is(importErr, documents.ErrNoWork) {
				if importErr == ctx.Err() {
					return importErr
				}
				return errors.Join(importErr, ctx.Err())
			}
			if importErr == nil {
				if err := executeImport(ctx, store, c.DocumentDir, imported.OwnerID, imported.Job.ID, guard.ID()); err != nil {
					return err
				}
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

// One sequential delivery lane runs independently of native scan/render work.
// A durable attempted marker, not a tick or restart, decides upload authority.
func runDeliveries(ctx context.Context, store *documents.Store, root *artifactread.Root, client *paperless.Client, once bool) error {
	for {
		candidate, err := store.NextDelivery(ctx, client.DestinationID(), client.ConfigFingerprint())
		if err != nil && !errors.Is(err, documents.ErrNoWork) {
			return err
		}
		if err == nil {
			err = ProcessDelivery(ctx, store, root, client, candidate.OwnerID, candidate.DeliveryID)
			// Optional remote failure remains in the durable ledger, never a normal job failure.
			if errors.Is(err, documents.ErrStorage) || errors.Is(err, documents.ErrCommitUncertain) {
				return err
			}
		}
		if once {
			return ctx.Err()
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
