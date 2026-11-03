package documents

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func claimedCleanup(t *testing.T) (*fixture, string, CleanupWork) {
	t.Helper()
	f, job := cleanupFixture(t)
	ctx := context.Background()
	if _, err := f.store.ClaimCleanup(ctx, alice, job, runner); err != nil {
		t.Fatal(err)
	}
	work, err := f.store.LoadCleanupWork(ctx, alice, job, runner, 100)
	if err != nil {
		t.Fatal(err)
	}
	return f, job, work
}
func removal(file CleanupFile, at int64) CleanupRemoval {
	return CleanupRemoval{ArtifactID: file.ArtifactID, Size: file.Size, SHA256: file.SHA256, RemovedAtMS: at}
}
func acknowledgeAll(t *testing.T, f *fixture, job string, work CleanupWork) {
	t.Helper()
	for _, file := range work.Files {
		if err := f.store.RecordCleanupRemoval(context.Background(), alice, job, runner, removal(file, f.now)); err != nil {
			t.Fatal(err)
		}
	}
}
func TestCleanupProgressAndFinalQuotaRelease(t *testing.T) {
	f, job, work := claimedCleanup(t)
	ctx := context.Background()
	if err := f.store.FinishCleanup(ctx, alice, job, runner); !errors.Is(err, ErrConflict) {
		t.Fatal("unremoved files freed", err)
	}
	first := work.Files[0]
	proof := removal(first, f.now)
	if err := f.store.RecordCleanupRemoval(ctx, alice, job, runner, proof); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecordCleanupRemoval(ctx, alice, job, runner, proof); err != nil {
		t.Fatal("progress retry", err)
	}
	if recorded, err := f.store.CleanupRemovalRecorded(ctx, alice, job, runner, first.ArtifactID); err != nil || !recorded {
		t.Fatal(recorded, err)
	}
	remaining, err := f.store.LoadCleanupWork(ctx, alice, job, runner, 100)
	if err != nil || remaining.Remaining != 2 || len(remaining.Files) != 2 {
		t.Fatal(remaining, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM artifacts") != 3 {
		t.Fatal("quota released before all file proof")
	}
	acknowledgeAll(t, f, job, remaining)
	must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
	f.now = work.Job.DeadlineAtMS + 1
	if err = f.store.FinishCleanup(ctx, alice, job, runner); err != nil {
		t.Fatal("finished physical work must be recordable after deadline", err)
	}
	for _, table := range []string{"artifacts", "pages", "page_thumbnails", "published_outputs", "cleanup_files", "job_artifacts"} {
		if scalar(t, f.docs, "SELECT count(*) FROM "+table) != 0 {
			t.Fatal("payload metadata survived", table)
		}
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind!='cleanup'") != 0 {
		t.Fatal("processing configuration retained")
	}
	if recorded, err := f.store.CleanupFinishedRecorded(ctx, alice, job, runner); err != nil || !recorded {
		t.Fatal(recorded, err)
	}
}
func TestCleanupRejectsMisboundRemovalProof(t *testing.T) {
	for _, mode := range []string{"owner", "runner", "hash", "size", "before-start", "future", "artifact"} {
		t.Run(mode, func(t *testing.T) {
			f, job, work := claimedCleanup(t)
			proof := removal(work.Files[0], f.now)
			owner, worker := alice, runner
			switch mode {
			case "owner":
				owner = bob
			case "runner":
				worker = "foreign_runner_001"
			case "hash":
				proof.SHA256[0] ^= 1
			case "size":
				proof.Size++
			case "before-start":
				proof.RemovedAtMS = f.now - 1
			case "future":
				proof.RemovedAtMS = f.now + 1
			case "artifact":
				proof.ArtifactID = "foreign_artifact_001"
			}
			if err := f.store.RecordCleanupRemoval(context.Background(), owner, job, worker, proof); !errors.Is(err, ErrConflict) {
				t.Fatal(mode, err)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM cleanup_files WHERE state='deleted'") != 0 {
				t.Fatal("bad proof acknowledged")
			}
		})
	}
}
func TestCleanupProgressCommitUncertaintyAndCompletionReadback(t *testing.T) {
	for _, phase := range []string{"progress", "finish"} {
		for _, committed := range []bool{false, true} {
			name := phase + map[bool]string{false: "-rollback", true: "-commit"}[committed]
			t.Run(name, func(t *testing.T) {
				f, job, work := claimedCleanup(t)
				ctx := context.Background()
				if phase == "finish" {
					acknowledgeAll(t, f, job, work)
				}
				f.store.commit = func(tx *sql.Tx) error {
					if committed {
						if err := tx.Commit(); err != nil {
							return err
						}
					}
					return errors.New("lost acknowledgement")
				}
				var err error
				if phase == "progress" {
					err = f.store.RecordCleanupRemoval(ctx, alice, job, runner, removal(work.Files[0], f.now))
				} else {
					err = f.store.FinishCleanup(ctx, alice, job, runner)
				}
				if !errors.Is(err, ErrCommitUncertain) {
					t.Fatal(err)
				}
				var recorded bool
				if phase == "progress" {
					recorded, err = f.store.CleanupRemovalRecorded(ctx, alice, job, runner, work.Files[0].ArtifactID)
				} else {
					recorded, err = f.store.CleanupFinishedRecorded(ctx, alice, job, runner)
				}
				if err != nil || recorded != committed {
					t.Fatal(recorded, err)
				}
				if phase == "finish" {
					count := 3
					if committed {
						count = 0
					}
					if scalar(t, f.docs, "SELECT count(*) FROM artifacts") != count {
						t.Fatal("partial quota completion")
					}
				}
			})
		}
	}
}
