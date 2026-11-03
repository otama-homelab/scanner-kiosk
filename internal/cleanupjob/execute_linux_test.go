//go:build linux

package cleanupjob

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/artifactgc"
	"scanner-kiosk/internal/documents"
)

const owner = "owner_cleanup_0001"
const job = "job_cleanup_000001"
const runner = "runner_cleanup_001"

type records struct {
	deadline                            int64
	mu                                  sync.Mutex
	file                                documents.CleanupFile
	recorded, finished                  bool
	mode                                string
	recordCalls, finishCalls, readbacks int
	loadErr                             error
}

func (r *records) LoadCleanupWork(ctx context.Context, o, j, w string, limit int) (documents.CleanupWork, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return documents.CleanupWork{}, err
	}
	if r.loadErr != nil {
		return documents.CleanupWork{}, r.loadErr
	}
	work := documents.CleanupWork{Job: documents.Job{ID: job, DocumentID: "document_cleanup_01", Kind: "cleanup", Configuration: `{"version":1}`, Revision: 1, DeadlineAtMS: r.deadline}}
	if !r.recorded {
		work.Files = []documents.CleanupFile{r.file}
		work.Remaining = 1
	}
	return work, nil
}
func (r *records) RecordCleanupRemoval(ctx context.Context, o, j, w string, proof documents.CleanupRemoval) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	r.recordCalls++
	if r.mode == "progress-rollback" {
		return documents.ErrCommitUncertain
	}
	r.recorded = true
	if strings.HasPrefix(r.mode, "progress-") {
		return documents.ErrCommitUncertain
	}
	return nil
}
func (r *records) CleanupRemovalRecorded(context.Context, string, string, string, string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readbacks++
	if r.mode == "progress-unreadable" {
		return false, documents.ErrStorage
	}
	return r.recorded, nil
}
func (r *records) FinishCleanup(ctx context.Context, o, j, w string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishCalls++
	if !r.recorded {
		return documents.ErrConflict
	}
	if r.mode == "finish-rollback" {
		return documents.ErrCommitUncertain
	}
	r.finished = true
	if r.mode == "finish-committed" {
		return documents.ErrCommitUncertain
	}
	return nil
}
func (r *records) CleanupFinishedRecorded(context.Context, string, string, string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readbacks++
	return r.finished, nil
}

type remover struct {
	root   *artifactgc.Root
	calls  int
	after  func(*artifactgc.Removal)
	block  bool
	joined atomic.Bool
}

func (r *remover) Remove(ctx context.Context, j string, input artifactgc.Input) (artifactgc.Removal, error) {
	r.calls++
	if r.block {
		<-ctx.Done()
		r.joined.Store(true)
		return artifactgc.Removal{}, ctx.Err()
	}
	proof, err := r.root.Remove(ctx, j, input)
	if r.after != nil {
		r.after(&proof)
	}
	return proof, err
}
func fixture(t *testing.T) (*records, *remover, string) {
	t.Helper()
	root := t.TempDir()
	os.Chmod(root, 0700)
	raw := []byte("immutable fixture")
	if err := os.WriteFile(filepath.Join(root, "original.png"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	gc, err := artifactgc.OpenRoot(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gc.Close() })
	return &records{deadline: time.Now().Add(time.Hour).UnixMilli(), file: documents.CleanupFile{ArtifactID: "artifact_cleanup_01", RelativePath: "original.png", Size: int64(len(raw)), SHA256: sha256.Sum256(raw)}}, &remover{root: gc}, filepath.Join(root, "original.png")
}
func active(context.Context) error { return nil }
func execute(ctx context.Context, r *records, files *remover, check CheckAuthority) error {
	return ExecuteClaimed(ctx, r, files, check, owner, job, runner, time.Millisecond)
}
func TestSuccessAndCommittedAcknowledgementLoss(t *testing.T) {
	for _, mode := range []string{"success", "progress-committed", "finish-committed"} {
		t.Run(mode, func(t *testing.T) {
			r, files, path := fixture(t)
			r.mode = mode
			if err := execute(context.Background(), r, files, active); err != nil {
				t.Fatal(err)
			}
			if !r.finished || r.recordCalls != 1 || r.finishCalls != 1 || files.calls != 1 {
				t.Fatal("work or write repeated", r, files.calls)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		})
	}
}
func TestUncertainWritesStopAndSameJobCanResume(t *testing.T) {
	for _, mode := range []string{"progress-rollback", "progress-unreadable", "finish-rollback"} {
		t.Run(mode, func(t *testing.T) {
			r, files, _ := fixture(t)
			r.mode = mode
			if err := execute(context.Background(), r, files, active); !errors.Is(err, ErrPending) {
				t.Fatal(err)
			}
			if r.recordCalls != 1 || files.calls != 1 || r.finished {
				t.Fatal("uncertain work retried", r, files.calls)
			}
			r.mode = ""
			if err := execute(context.Background(), r, files, active); err != nil || !r.finished {
				t.Fatal("resume", err)
			}
			if mode != "progress-rollback" && files.calls != 1 {
				t.Fatal("confirmed IO repeated")
			}
		})
	}
}
func TestAuthorizationFailureAndGuardLossJoin(t *testing.T) {
	r, files, path := fixture(t)
	r.loadErr = documents.ErrConflict
	if err := execute(context.Background(), r, files, active); !errors.Is(err, ErrPending) || files.calls != 0 {
		t.Fatal(err)
	}
	r.loadErr = nil
	files.block = true
	var checks atomic.Int32
	lost := errors.New("guard lost")
	check := func(context.Context) error {
		if checks.Add(1) >= 3 {
			return lost
		}
		return nil
	}
	if err := execute(context.Background(), r, files, check); !errors.Is(err, lost) || !files.joined.Load() || r.recordCalls != 0 {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("authority loss removed file", err)
	}
}
func TestCancellationAfterUnlinkPersistsProgressWithoutFinishing(t *testing.T) {
	r, files, _ := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	files.after = func(*artifactgc.Removal) { cancel() }
	if err := execute(ctx, r, files, active); !errors.Is(err, context.Canceled) || !r.recorded || r.finished {
		t.Fatal(err, r)
	}
	files.after = nil
	if err := execute(context.Background(), r, files, active); err != nil || !r.finished || files.calls != 1 {
		t.Fatal(err, r)
	}
}
func TestMisboundProofDoesNotReleaseMetadata(t *testing.T) {
	r, files, _ := fixture(t)
	files.after = func(proof *artifactgc.Removal) { proof.ArtifactID = "foreign_artifact_01" }
	if err := execute(context.Background(), r, files, active); !errors.Is(err, ErrInvalid) || r.recorded || r.finished {
		t.Fatal(err)
	}
}

func TestFileMismatchDoesNotAcknowledgeOrFinish(t *testing.T) {
	r, files, path := fixture(t)
	r.file.SHA256[0] ^= 1
	if err := execute(context.Background(), r, files, active); !errors.Is(err, artifactgc.ErrMismatch) || r.recorded || r.finished || files.calls != 1 {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), ".cleanup", job, r.file.ArtifactID)); err != nil {
		t.Fatal("mismatched bytes lost", err)
	}
}

func TestCleanupFailureIdentifiesPhaseAndPreservesCause(t *testing.T) {
	for _, phase := range []string{"load cleanup manifest", "record cleanup removal", "finish cleanup", "supervise cleanup removal"} {
		t.Run(phase, func(t *testing.T) {
			r, files, _ := fixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := documents.ErrCommitUncertain
			switch phase {
			case "load cleanup manifest":
				r.loadErr = documents.ErrConflict
				cause = documents.ErrConflict
			case "record cleanup removal":
				r.mode = "progress-rollback"
			case "finish cleanup":
				r.mode = "finish-rollback"
			case "supervise cleanup removal":
				files.after = func(*artifactgc.Removal) { cancel() }
				cause = context.Canceled
			}
			err := execute(ctx, r, files, active)
			if !errors.Is(err, ErrPending) || !errors.Is(err, cause) || !strings.Contains(err.Error(), phase) {
				t.Fatal("cleanup phase or underlying cause lost", err)
			}
		})
	}
}
