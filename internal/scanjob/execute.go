// Package scanjob coordinates exactly one already-claimed physical scan. It
// never claims/retries hardware work or equates local exit with physical idle.
package scanjob

import (
	"context"
	"errors"
	"sync"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/workwatch"
)

var (
	ErrPending = errors.New("scan requires reconciliation")
	ErrClosed  = errors.New("scan output unavailable for publication")
)

type Records interface {
	LoadScanWork(context.Context, string, string, string) (documents.ScanWork, error)
	PulseScan(context.Context, string, string, string) error
	CommitScanPage(context.Context, string, string, int64, documents.ArtifactRecord, documents.ScanStop) (documents.ScanPage, error)
	CaptureRecorded(context.Context, string, string, string) (bool, error)
	FinishStoppedScan(context.Context, string, string, documents.ScanStop, int64, string) error
	ScanStoppedRecorded(context.Context, string, string) (bool, error)
}

// ArtifactLifecycle is an optional, per-scan publication extension. Extra
// prepared files are released only after confirmed DB publication; otherwise
// their cleanup must finish before the original and native lease are released.
type ArtifactLifecycle interface {
	ReleaseScanArtifacts() error
	AbortScanArtifacts() error
}

// Driver resolves the exact registered device and authoritative backend geometry.
// ObserveIdle must perform a fresh physical observation after the supplied local
// stop, bound to this registered device. Cached health or CLI exit is insufficient.
type Driver interface {
	Resolve(context.Context, documents.ScanWork) (scanner.Capturer, scanner.Plan, error)
	ObserveIdle(context.Context, string, int64) (int64, error)
}

type Prepared struct {
	mu                                                                            sync.Mutex
	records                                                                       Records
	driver                                                                        Driver
	owner, job                                                                    string
	work                                                                          documents.ScanWork
	attempt                                                                       scanner.Attempt
	artifact                                                                      documents.ArtifactRecord
	stop                                                                          documents.ScanStop
	launchUnknown, valid, pending, published, discarded, finished, cleanupPending bool
	publicationError                                                              error
}

// PrepareClaimed monitors persisted authorization throughout resolution and
// capture. A nonnil result owns all returned files/evidence even on failure and
// must be retained until Publish or Finish resolves it. No automatic retry.
func PrepareClaimed(ctx context.Context, records Records, driver Driver, owner, job, boot string, interval time.Duration) (*Prepared, error) {
	if records == nil || driver == nil {
		return nil, documents.ErrInvalid
	}
	work, err := records.LoadScanWork(ctx, owner, job, boot)
	if err != nil {
		return nil, err
	}
	p := &Prepared{records: records, driver: driver, owner: owner, job: job, work: work, stop: documents.ScanStop{DeviceID: work.Snapshot.DeviceID, WorkerBootID: boot, LeaseEpoch: work.Snapshot.LeaseEpoch}}
	if work.Job.ID != job || work.Job.Kind != "scan" || work.Snapshot.WorkerBootID != boot || work.ReserveBytes < 1 || work.Job.DeadlineAtMS < 1 {
		return p, documents.ErrInvalid
	}
	execution, cancel := context.WithDeadline(ctx, time.UnixMilli(work.Job.DeadlineAtMS))
	defer cancel()
	err = workwatch.Run(execution, interval, func(ctx context.Context) error { return records.PulseScan(ctx, owner, job, boot) }, func(ctx context.Context) error {
		capturer, plan, err := driver.Resolve(ctx, work)
		if err != nil {
			if errors.Is(err, scanner.ErrUnresolved) {
				p.launchUnknown = true
			}
			return err
		}
		capture := work.Snapshot.Capture
		if capturer == nil || plan.DeviceID != work.Snapshot.DeviceID || plan.Size != capture.Size || plan.Color != capture.Color || plan.DPI != capture.DPI || plan.Width < 1 || plan.Height < 1 || plan.ByteBudget < 1 || plan.ByteBudget > work.ReserveBytes {
			return documents.ErrInvalid
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		// A nil attempt after invoking a driver has no cleanup proof; retain the
		// durable reservation until an external reconciler resolves that outcome.
		p.launchUnknown = true
		p.attempt, err = capturer.Capture(ctx, plan)
		if p.attempt == nil {
			if err == nil {
				err = documents.ErrInvalid
			}
			return err
		}
		p.launchUnknown = false
		p.stop.ProcessStoppedAtMS = p.attempt.StoppedAtMS()
		if err != nil {
			return err
		}
		if p.stop.ProcessStoppedAtMS <= 0 {
			return ErrPending
		}
		artifact, err := p.attempt.Artifact()
		if err != nil {
			return err
		}
		image := artifact.Image
		if image.Size < 1 || image.Size > plan.ByteBudget || image.Width != plan.Width || image.Height != plan.Height || (image.MediaType != "image/png" && image.MediaType != "image/jpeg") {
			return documents.ErrInvalid
		}
		p.artifact = documents.ArtifactRecord{ID: artifact.ID, RelativePath: artifact.RelativePath, MediaType: image.MediaType, Size: image.Size, SHA256: image.SHA256, PixelWidth: image.Width, PixelHeight: image.Height}
		p.valid = true
		return nil
	})
	if p.attempt == nil && !p.launchUnknown {
		p.stop.ProcessStoppedAtMS = time.Now().UnixMilli()
	}
	if err != nil {
		p.valid = false
		return p, err
	}
	return p, nil
}

func (p *Prepared) observeIdle(ctx context.Context) error {
	if p.launchUnknown || p.stop.ProcessStoppedAtMS <= 0 {
		return ErrPending
	}
	began := time.Now().UnixMilli()
	observed, err := p.driver.ObserveIdle(ctx, p.stop.DeviceID, p.stop.ProcessStoppedAtMS)
	if err != nil {
		return errors.Join(ErrPending, err)
	}
	if observed < p.stop.ProcessStoppedAtMS || observed < began || observed > time.Now().UnixMilli() {
		return ErrPending
	}
	p.stop.IdleObservedAtMS = observed
	return nil
}

// Publish requires fresh physical idle after local stop. On uncertain DB commit,
// only read-back is retried; native capture and the publication write are not.
func (p *Prepared) Publish(ctx context.Context) (documents.ArtifactRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.published {
		return p.artifact, p.releaseArtifacts()
	}
	if p.discarded || !p.valid {
		return documents.ArtifactRecord{}, ErrClosed
	}
	if !p.pending {
		if err := p.observeIdle(ctx); err != nil {
			return documents.ArtifactRecord{}, err
		}
		_, err := p.records.CommitScanPage(ctx, p.owner, p.job, p.work.Job.Revision, p.artifact, p.stop)
		if err == nil {
			p.published = true
			return p.artifact, p.releaseArtifacts()
		}
		p.publicationError = err
		if !errors.Is(err, documents.ErrCommitUncertain) {
			p.discarded = true
			return documents.ArtifactRecord{}, err
		}
		p.pending = true
	}
	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	recorded, err := p.records.CaptureRecorded(recovery, p.owner, p.job, p.artifact.ID)
	if err != nil {
		return documents.ArtifactRecord{}, errors.Join(ErrPending, err)
	}
	p.pending = false
	if recorded {
		p.published = true
		return p.artifact, p.releaseArtifacts()
	}
	p.discarded = true
	return documents.ArtifactRecord{}, p.publicationError
}

func (p *Prepared) releaseArtifacts() error {
	if lifecycle, ok := p.records.(ArtifactLifecycle); ok {
		if err := lifecycle.ReleaseScanArtifacts(); err != nil {
			return err
		}
	}
	return p.attempt.Release()
}

// Finish cleans only known-stopped, confirmed-unpublished local work. The lease
// remains held until cleanup and fresh physical idle have both been confirmed.
// Disabled/expired owners use the internal cleanup authority in Records.
func (p *Prepared) Finish(ctx context.Context, outcome string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if outcome != "failed" && outcome != "interrupted" {
		return documents.ErrInvalid
	}
	if p.pending || p.launchUnknown {
		return ErrPending
	}
	if p.published {
		return ErrClosed
	}
	if p.finished {
		return nil
	}
	if !p.cleanupPending {
		if err := p.observeIdle(ctx); err != nil {
			return err
		}
		p.discarded = true
		if lifecycle, ok := p.records.(ArtifactLifecycle); ok {
			if err := lifecycle.AbortScanArtifacts(); err != nil {
				return err
			}
		}
		if p.attempt != nil {
			if err := p.attempt.Close(); err != nil {
				return err
			}
		}
		err := p.records.FinishStoppedScan(ctx, p.owner, p.job, p.stop, time.Now().UnixMilli(), outcome)
		if err == nil {
			p.finished = true
			return nil
		}
		if !errors.Is(err, documents.ErrCommitUncertain) {
			return err
		}
		p.cleanupPending = true
	}
	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	recorded, err := p.records.ScanStoppedRecorded(recovery, p.owner, p.job)
	if err != nil {
		return errors.Join(ErrPending, err)
	}
	p.cleanupPending = false
	if recorded {
		p.finished = true
		return nil
	}
	return documents.ErrCommitUncertain
}

// ExecuteClaimed connects monitoring, capture, publication and failed-work
// cleanup. A nonnil result must be retained for reconciliation; it never means
// the device is safe to reassign. The caller must have confirmed ClaimScan.
func ExecuteClaimed(ctx context.Context, records Records, driver Driver, owner, job, boot string, interval time.Duration) (*Prepared, error) {
	p, err := PrepareClaimed(ctx, records, driver, owner, job, boot, interval)
	if p == nil {
		return nil, err
	}
	if err == nil {
		_, err = p.Publish(ctx)
		if err == nil {
			return nil, nil
		}
		if p.pending || p.published {
			return p, err
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	outcome := "failed"
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		outcome = "interrupted"
	}
	if finishErr := p.Finish(cleanup, outcome); finishErr != nil {
		return p, errors.Join(err, finishErr)
	}
	return nil, err
}
