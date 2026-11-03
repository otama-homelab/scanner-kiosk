//go:build linux

// Package scanthumbnail adds bounded source previews to one confirmed scan.
// It owns no device, admission or HTTP state. A fresh Publisher belongs to exactly
// one scan coordinator, including while uncertain publication is reconciled.
package scanthumbnail

import (
	"context"
	"errors"
	"time"

	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/captureimage"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanjob"
	"scanner-kiosk/internal/thumbnail"
	"scanner-kiosk/internal/workwatch"
)

const previewBudget int64 = 1 << 20

type Records interface {
	scanjob.Records
	CommitScanPageWithThumbnails(context.Context, string, string, int64, documents.ArtifactRecord, documents.ScanStop, []documents.ThumbnailRecord) (documents.ScanPage, error)
	CaptureBundleRecorded(context.Context, string, string, documents.ArtifactRecord, []documents.ThumbnailRecord) (bool, error)
}

type Publisher struct {
	Records
	root                string
	maxBytes, maxPixels int64
	scope               *artifactscope.Scope
	original            documents.ArtifactRecord
	thumbnails          []documents.ThumbnailRecord
	owner, job          string
	called              bool
}

func New(records Records, root string, maxBytes, maxPixels int64) *Publisher {
	return &Publisher{Records: records, root: root, maxBytes: maxBytes, maxPixels: maxPixels}
}

func (p *Publisher) CommitScanPage(ctx context.Context, owner, job string, revision int64, original documents.ArtifactRecord, stop documents.ScanStop) (documents.ScanPage, error) {
	if p.called || p.Records == nil || p.maxBytes < 1 || p.maxPixels < 1 {
		return documents.ScanPage{}, documents.ErrInvalid
	}
	p.called = true
	p.original = original
	p.owner = owner
	p.job = job
	work, err := p.Records.LoadScanWork(ctx, owner, job, stop.WorkerBootID)
	if err != nil {
		return documents.ScanPage{}, err
	}
	if work.Job.ID != job || work.Job.Revision != revision || original.Size < 1 || original.Size > work.ReserveBytes {
		return documents.ScanPage{}, documents.ErrInvalid
	}
	execution, cancel := context.WithDeadline(ctx, time.UnixMilli(work.Job.DeadlineAtMS))
	defer cancel()
	// Preview is optional when the immutable scan reservation is already full.
	// Never enlarge that reservation or fail a valid original only for headroom.
	inputCap := min(p.maxBytes, work.ReserveBytes, int64(1<<30))
	if work.ReserveBytes-original.Size >= previewBudget && original.Size <= inputCap {
		err = workwatch.Run(execution, time.Second, func(current context.Context) error {
			return p.Records.PulseScan(current, owner, job, stop.WorkerBootID)
		}, func(current context.Context) error {
			input := artifactscope.Input{RelativePath: original.RelativePath, Size: original.Size, SHA256: original.SHA256}
			var err error
			p.scope, err = artifactscope.New(p.root, map[string]artifactscope.Input{original.ID: input}, artifactscope.Limits{MaxInputBytes: inputCap, MaxOutputBytes: previewBudget, MaxTotalOutputBytes: previewBudget, MaxOutputs: 1})
			if err != nil {
				return err
			}
			policy := thumbnail.Options{Input: captureimage.Options{MaxBytes: inputCap, MaxPixels: p.maxPixels, Width: original.PixelWidth, Height: original.PixelHeight, Color: work.Snapshot.Capture.Color}, MaxEdge: 512, MaxOutputBytes: previewBudget, Quality: 82}
			reference, err := thumbnail.GenerateScoped(current, p.scope, original.ID, policy)
			if err != nil {
				return err
			}
			sealed, err := p.scope.Seal(current)
			if err != nil {
				return err
			}
			if len(sealed) != 1 || sealed[0].ID != reference || sealed[0].Kind != artifactscope.JPEG {
				return documents.ErrInvalid
			}
			width, height := original.PixelWidth, original.PixelHeight
			if width > 512 || height > 512 {
				if width >= height {
					height = max(1, int(int64(height)*512/int64(width)))
					width = 512
				} else {
					width = max(1, int(int64(width)*512/int64(height)))
					height = 512
				}
			}
			a := sealed[0]
			p.thumbnails = []documents.ThumbnailRecord{{OriginalID: original.ID, Artifact: documents.ArtifactRecord{ID: a.ID, RelativePath: a.RelativePath, MediaType: "image/jpeg", Size: a.Size, SHA256: a.SHA256, PixelWidth: width, PixelHeight: height}}}
			return nil
		})
		if onlyPreviewLimit(err) {
			if closeErr := p.AbortScanArtifacts(); closeErr != nil {
				return documents.ScanPage{}, errors.Join(err, closeErr)
			}
			p.scope = nil
			p.thumbnails = nil
			// Recheck authority after confirmed optional-output cleanup.
			err = p.Records.PulseScan(execution, owner, job, stop.WorkerBootID)
		}
		if err != nil {
			return documents.ScanPage{}, err
		}
	}
	return p.Records.CommitScanPageWithThumbnails(execution, owner, job, revision, original, stop, p.thumbnails)
}

func (p *Publisher) CaptureRecorded(ctx context.Context, owner, job, originalID string) (bool, error) {
	if !p.called || p.owner != owner || p.job != job || p.original.ID != originalID {
		return false, documents.ErrInvalid
	}
	return p.Records.CaptureBundleRecorded(ctx, owner, job, p.original, p.thumbnails)
}
func (p *Publisher) ReleaseScanArtifacts() error {
	if p.scope == nil {
		return nil
	}
	return p.scope.Release()
}
func (p *Publisher) AbortScanArtifacts() error {
	if p.scope == nil {
		return nil
	}
	return p.scope.Close()
}

var _ scanjob.Records = (*Publisher)(nil)
var _ scanjob.ArtifactLifecycle = (*Publisher)(nil)

// A monitor/storage/cancellation error joined with a preview limit must never be
// downgraded to optional-preview fallback. workwatch returns joined causes.
func onlyPreviewLimit(err error) bool {
	if err == thumbnail.ErrLimit || err == artifactscope.ErrLimit {
		return true
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return false
	}
	causes := joined.Unwrap()
	if len(causes) == 0 {
		return false
	}
	for _, cause := range causes {
		if !onlyPreviewLimit(cause) {
			return false
		}
	}
	return true
}
