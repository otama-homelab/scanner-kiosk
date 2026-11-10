//go:build linux

package renderjob

import (
	"context"
	"errors"
	"image"
	"io"
	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/photonormalize"
	"scanner-kiosk/pkg/ocrplugin"
	"time"
)

type PhotoCropRecords interface {
	CompletePhotoCrop(context.Context, string, string, string, int64, documents.ArtifactRecord) error
	PhotoCropRecorded(context.Context, string, string, documents.ArtifactRecord) (bool, error)
}

func preparePhotoCrop(ctx context.Context, records Records, root, owner, job, runner string, work documents.RenderWork) (prepared *Prepared, err error) {
	crop, source := work.Snapshot.ManualCrop, work.Snapshot.ManualSource
	if crop == nil || source == nil || work.Snapshot.ReOCR || work.Snapshot.Settings.OCR.Enabled || source.MediaType != "image/png" || crop.Width < 1 || crop.Height < 1 || crop.X < 0 || crop.Y < 0 || crop.X > source.PixelWidth-crop.Width || crop.Y > source.PixelHeight-crop.Height || work.ReserveBytes < 65 {
		return nil, documents.ErrInvalid
	}
	if _, ok := records.(PhotoCropRecords); !ok {
		return nil, ErrUnsupported
	}
	ctx, cancel := context.WithDeadline(ctx, time.UnixMilli(work.Job.DeadlineAtMS))
	defer cancel()
	reader, e := artifactread.OpenRoot(root, photonormalize.MaxNormalizedBytes)
	if e != nil {
		return nil, e
	}
	defer reader.Close()
	input, e := reader.OpenFile(ctx, artifactread.Input{RelativePath: source.RelativePath, Size: source.Size, SHA256: source.SHA256})
	if e != nil {
		return nil, e
	}
	defer input.Close()
	limit := min(work.ReserveBytes-64, photonormalize.MaxNormalizedBytes)
	scope, e := artifactscope.New(root, nil, artifactscope.Limits{MaxInputBytes: photonormalize.MaxNormalizedBytes, MaxOutputBytes: limit + 64, MaxTotalOutputBytes: work.ReserveBytes, MaxOutputs: 1})
	if e != nil {
		return nil, e
	}
	uncertain := false
	defer func() {
		if err != nil {
			clean := false
			if !uncertain {
				clean = scope.Close() == nil
			}
			err = &preparationFailure{cause: err, clean: clean, stoppedAtMS: time.Now().UnixMilli(), scope: scope, uncertain: uncertain}
		}
	}()
	output, e := scope.CreateFileArtifact(ctx, artifactscope.PNG, limit+64)
	if e != nil {
		return nil, e
	}
	p, e := photonormalize.PrepareCrop(ctx, input, output.File(), image.Rect(crop.X, crop.Y, crop.X+crop.Width, crop.Y+crop.Height), limit)
	if errors.Is(e, photonormalize.ErrUnstopped) {
		uncertain = true
		return nil, ocrplugin.ErrReconciliation
	}
	if e != nil {
		closeErr := output.CloseAfterStop()
		return nil, errors.Join(e, closeErr)
	}
	// Strip the validated framing in place; peak disk remains within the job reservation.
	buffer := make([]byte, 64<<10)
	for offset := int64(0); offset < p.Size; {
		n := min(int64(len(buffer)), p.Size-offset)
		if _, e = output.File().ReadAt(buffer[:n], p.Offset+offset); e != nil {
			return nil, errors.Join(e, output.CloseAfterStop())
		}
		if _, e = output.File().WriteAt(buffer[:n], offset); e != nil {
			return nil, errors.Join(e, output.CloseAfterStop())
		}
		offset += n
	}
	if e = output.File().Truncate(p.Size); e != nil {
		return nil, errors.Join(e, output.CloseAfterStop())
	}
	if e = output.CloseAfterStop(); e != nil {
		return nil, e
	}
	sealed, e := scope.Seal(ctx)
	if e != nil {
		return nil, e
	}
	if len(sealed) != 1 || sealed[0].SHA256 != p.SHA256 || sealed[0].Size != p.Size {
		return nil, io.ErrUnexpectedEOF
	}
	a := sealed[0]
	return &Prepared{manualCrop: true, records: records, scope: scope, owner: owner, job: job, runner: runner, revision: work.Job.Revision, stoppedAtMS: time.Now().UnixMilli(), artifact: documents.ArtifactRecord{ID: a.ID, RelativePath: a.RelativePath, Size: a.Size, SHA256: a.SHA256, MediaType: "image/png", PixelWidth: p.Width, PixelHeight: p.Height}}, nil
}
