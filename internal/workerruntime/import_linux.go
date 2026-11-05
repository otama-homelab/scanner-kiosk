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
		budget := min((work.ReserveBytes-overhead)/2, int64(1<<30))
		if overhead < 0 || budget < 1 {
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
		prepared, err := pdfimport.Prepare(ctx, input, output, opts)
		if errors.Is(err, pdfimport.ErrUnstopped) {
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
			writer, err := scope.CreateArtifact(ctx, artifactscope.PDF)
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
			pages = append(pages, documents.ImportedPage{Artifact: documents.ArtifactRecord{ID: artifact.ID, RelativePath: artifact.RelativePath, MediaType: "application/pdf", Size: artifact.Size, SHA256: artifact.SHA256}, WidthPoints: page.WidthPoints, HeightPoints: page.HeightPoints})
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
