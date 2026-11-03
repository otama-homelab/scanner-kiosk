//go:build linux

// Package cleanupjob coordinates one confirmed claimed cleanup. It neither
// claims work nor invents stop evidence; the runtime must retain/join its guard.
package cleanupjob

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"scanner-kiosk/internal/artifactgc"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/workwatch"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{16,96}$`)

var ErrPending = errors.New("cleanup requires reconciliation")
var ErrInvalid = errors.New("invalid cleanup coordinator")

type Records interface {
	LoadCleanupWork(context.Context, string, string, string, int) (documents.CleanupWork, error)
	RecordCleanupRemoval(context.Context, string, string, string, documents.CleanupRemoval) error
	CleanupRemovalRecorded(context.Context, string, string, string, string) (bool, error)
	FinishCleanup(context.Context, string, string, string) error
	CleanupFinishedRecorded(context.Context, string, string, string) (bool, error)
}
type Remover interface {
	Remove(context.Context, string, artifactgc.Input) (artifactgc.Removal, error)
}
type CheckAuthority func(context.Context) error

// ExecuteClaimed loads at most one pending file at a time. A monitor checks the
// runtime authority and same manifest while local file work runs, and joins before
// return. The caller must never close the worker guard/root before this returns.
// File IO uses the job deadline. Reports of already completed IO use a separate
// bounded context so cancellation cannot erase evidence of an unlink.
func ExecuteClaimed(ctx context.Context, records Records, remover Remover, check CheckAuthority, owner, job, runner string, interval time.Duration) error {
	if records == nil || remover == nil || check == nil || !identifier.MatchString(owner) || !identifier.MatchString(job) || !identifier.MatchString(runner) || interval < time.Millisecond || interval > time.Minute {
		return ErrInvalid
	}
	for {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrPending, err)
		}
		if err := check(ctx); err != nil {
			return errors.Join(ErrPending, err)
		}
		work, err := records.LoadCleanupWork(ctx, owner, job, runner, 1)
		if err != nil {
			return errors.Join(ErrPending, fmt.Errorf("load cleanup manifest: %w", err))
		}
		if work.Job.ID != job || work.Job.Kind != "cleanup" || work.Job.Configuration != `{"version":1}` || !identifier.MatchString(work.Job.DocumentID) || work.Job.Revision < 1 || work.Job.DeadlineAtMS <= 0 || work.Remaining < 0 || len(work.Files) > 1 || work.Remaining == 0 && len(work.Files) != 0 || work.Remaining > 0 && len(work.Files) != 1 {
			return errors.Join(ErrPending, ErrInvalid)
		}
		if work.Remaining == 0 {
			if err = check(ctx); err != nil {
				return errors.Join(ErrPending, err)
			}
			report, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			err = records.FinishCleanup(report, owner, job, runner)
			if errors.Is(err, documents.ErrCommitUncertain) {
				recorded, readErr := records.CleanupFinishedRecorded(report, owner, job, runner)
				if readErr == nil && recorded {
					err = nil
				} else {
					err = errors.Join(err, readErr)
				}
			}
			stop()
			if err != nil {
				return errors.Join(ErrPending, fmt.Errorf("finish cleanup: %w", err))
			}
			return nil
		}
		file := work.Files[0]
		input := artifactgc.Input{ArtifactID: file.ArtifactID, RelativePath: file.RelativePath, Size: file.Size, SHA256: file.SHA256}
		execution, stop := context.WithDeadline(ctx, time.UnixMilli(work.Job.DeadlineAtMS))
		var removed artifactgc.Removal
		pulse := func(current context.Context) error {
			if err := check(current); err != nil {
				return err
			}
			latest, err := records.LoadCleanupWork(current, owner, job, runner, 1)
			if err != nil {
				return err
			}
			if latest.Job != work.Job || latest.Remaining != work.Remaining || len(latest.Files) != 1 || latest.Files[0] != file {
				return ErrInvalid
			}
			return nil
		}
		err = workwatch.Run(execution, interval, pulse, func(current context.Context) error {
			var err error
			removed, err = remover.Remove(current, job, input)
			return err
		})
		stop()
		// Preserve a successful physical outcome even when the monitor/caller cancelled
		// concurrently. DB runner/manifest fences still bind this one-way report.
		if removed.RemovedAtMS > 0 {
			if removed.JobID != job || removed.ArtifactID != file.ArtifactID || removed.Size != file.Size || removed.SHA256 != file.SHA256 {
				return errors.Join(ErrPending, ErrInvalid, err)
			}
			report, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			recordErr := records.RecordCleanupRemoval(report, owner, job, runner, documents.CleanupRemoval{ArtifactID: removed.ArtifactID, Size: removed.Size, SHA256: removed.SHA256, RemovedAtMS: removed.RemovedAtMS})
			if errors.Is(recordErr, documents.ErrCommitUncertain) {
				recorded, readErr := records.CleanupRemovalRecorded(report, owner, job, runner, file.ArtifactID)
				if readErr == nil && recorded {
					recordErr = nil
				} else {
					recordErr = errors.Join(recordErr, readErr)
				}
			}
			cancel()
			if recordErr != nil {
				return errors.Join(ErrPending, err, fmt.Errorf("record cleanup removal: %w", recordErr))
			}
		} else if err == nil {
			return errors.Join(ErrPending, ErrInvalid)
		}
		if err != nil {
			return errors.Join(ErrPending, fmt.Errorf("supervise cleanup removal: %w", err))
		}
	}
}
