//go:build linux

package workerruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"time"

	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/pdfimport"
	"scanner-kiosk/internal/photonormalize"
	"scanner-kiosk/internal/workwatch"
)

// executeImport keeps the raw source immutable and publishes all prepared pages
// together. Unknown process, filesystem or commit outcomes retain the reservation.
func executeImport(ctx context.Context, store *documents.Store, root, owner, job, runner string) error {
	var scope *artifactscope.Scope
	var pages []documents.ImportedPage
	clean := true
	uncertainScope := false
	err := workwatch.Run(ctx, time.Second, func(ctx context.Context) error {
		return store.PulseImport(ctx, owner, job, runner)
	}, func(ctx context.Context) (result error) {
		work, err := store.LoadImportWork(ctx, owner, job, runner)
		if err != nil {
			return err
		}
		opts := pdfimport.Options{MaxInputBytes: work.Input.Size, MaxOutputBytes: 1, MaxPages: work.Snapshot.MaxPages}
		overhead := pdfimport.MaxResultBytes(opts) - 1
		if work.Input.MediaType != "application/pdf" {
			opts.MaxPages = 1
			overhead = 64
		}
		budget := min((work.ReserveBytes-overhead)/2, int64(1<<30))
		if overhead < 0 || budget < 1 {
			if work.Input.MediaType != "application/pdf" {
				return photonormalize.ErrLimit
			}
			return pdfimport.ErrLimit
		}
		opts.MaxOutputBytes = budget
		inputRoot, err := artifactread.OpenRoot(root, work.Input.Size)
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, inputRoot.Close()) }()
		input, err := inputRoot.OpenFile(ctx, artifactread.Input{RelativePath: work.Input.RelativePath, Size: work.Input.Size, SHA256: work.Input.SHA256})
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, input.Close()) }()
		files, err := os.OpenRoot(root)
		if err != nil {
			return err
		}
		defer files.Close()
		var nonce [24]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return err
		}
		dir := "work_" + hex.EncodeToString(nonce[:])
		clean = false
		if err = files.Mkdir(dir, 0700); err != nil {
			return err
		}
		output, err := files.OpenFile(dir+"/prepared", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		unstopped := false
		defer func() {
			closeErr := output.Close()
			if unstopped {
				result = errors.Join(result, closeErr)
				return
			}
			removeErr := files.RemoveAll(dir)
			parent, openErr := files.Open(".")
			var syncErr error
			if openErr == nil {
				syncErr = errors.Join(parent.Sync(), parent.Close())
			}
			cleanupErr := errors.Join(closeErr, removeErr, openErr, syncErr)
			clean = cleanupErr == nil && !uncertainScope
			result = errors.Join(result, cleanupErr)
		}()
		var prepared []pdfimport.PreparedPage
		var normalized photonormalize.Prepared
		if work.Input.MediaType == "application/pdf" {
			prepared, err = pdfimport.Prepare(ctx, input, output, opts)
		} else {
			normalized, err = photonormalize.Prepare(ctx, input, output, work.Input.MediaType, budget)
			if err == nil {
				prepared = []pdfimport.PreparedPage{{Offset: normalized.Offset, Size: normalized.Size, SHA256: normalized.SHA256}}
			}
		}

		if errors.Is(err, pdfimport.ErrUnstopped) || errors.Is(err, photonormalize.ErrUnstopped) {
			unstopped = true
		}
		if err != nil {
			return err
		}
		scope, err = artifactscope.New(root, nil, artifactscope.Limits{MaxInputBytes: 1, MaxOutputBytes: budget, MaxTotalOutputBytes: budget, MaxOutputs: opts.MaxPages})
		if err != nil {
			uncertainScope = true
			return err
		}
		for _, page := range prepared {
			kind := artifactscope.PDF
			if work.Input.MediaType != "application/pdf" {
				kind = artifactscope.PNG
			}
			writer, err := scope.CreateArtifact(ctx, kind)
			if err != nil {
				return err
			}
			n, copyErr := io.CopyBuffer(writer, io.NewSectionReader(output, page.Offset, page.Size), make([]byte, 64<<10))
			closeErr := writer.Close()
			if err = errors.Join(copyErr, closeErr); err != nil {
				return err
			}
			if n != page.Size {
				return io.ErrUnexpectedEOF
			}
		}
		sealed, err := scope.Seal(ctx)
		if err != nil {
			return err
		}
		for i, artifact := range sealed {
			page := prepared[i]
			if artifact.Size != page.Size || artifact.SHA256 != page.SHA256 {
				return pdfimport.ErrProcess
			}
			media := "application/pdf"
			if work.Input.MediaType != "application/pdf" {
				media = "image/png"
			}
			pages = append(pages, documents.ImportedPage{Orientation: normalized.Orientation, Artifact: documents.ArtifactRecord{ID: artifact.ID, RelativePath: artifact.RelativePath, MediaType: media, Size: artifact.Size, SHA256: artifact.SHA256, PixelWidth: normalized.Width, PixelHeight: normalized.Height}, WidthPoints: page.WidthPoints, HeightPoints: page.HeightPoints})
		}
		return nil
	})
	stopped := time.Now().UnixMilli()
	if !clean {
		return errors.Join(ErrReconciliation, err)
	}
	stop := documents.ImportStop{RunnerID: runner, ProcessStoppedAtMS: stopped, WorkspaceCleanedAtMS: stopped}
	if err == nil {
		err = store.CommitImportPages(ctx, owner, job, stop, pages)
		if err == nil {
			return scope.Release()
		}
		if errors.Is(err, documents.ErrCommitUncertain) {
			readback, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			recorded, readErr := store.ImportPagesRecorded(readback, owner, job, runner, pages)
			cancel()
			if readErr == nil && recorded {
				return scope.Release()
			}
			return errors.Join(ErrReconciliation, err, readErr)
		}
	}
	if scope != nil {
		if closeErr := scope.Close(); closeErr != nil {
			return errors.Join(ErrReconciliation, err, closeErr)
		}
	}
	stop.WorkspaceCleanedAtMS = time.Now().UnixMilli()
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	outcome := "failed"
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		outcome = "interrupted"
	}
	if outcome == "failed" {
		switch {
		case errors.Is(err, photonormalize.ErrInvalid):
			stop.ErrorCode = "photo_invalid"
		case errors.Is(err, photonormalize.ErrLimit):
			stop.ErrorCode = "photo_limit"
		case errors.Is(err, pdfimport.ErrEncrypted):
			stop.ErrorCode = "pdf_encrypted"
		case errors.Is(err, pdfimport.ErrUnsafe):
			stop.ErrorCode = "pdf_unsupported"
		case errors.Is(err, pdfimport.ErrLimit):
			stop.ErrorCode = "pdf_limit"
		case errors.Is(err, pdfimport.ErrInvalid):
			stop.ErrorCode = "pdf_invalid"
		}
	}
	finishErr := store.FinishStoppedImport(cleanup, owner, job, stop, outcome)
	if finishErr == nil {
		return nil
	}
	recorded, readErr := store.ImportStoppedRecorded(cleanup, owner, job, runner)
	if readErr == nil && recorded {
		return nil
	}
	return errors.Join(ErrReconciliation, err, finishErr, readErr)
}
