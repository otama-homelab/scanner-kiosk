package documents

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestReOCRCleanupHistory(t *testing.T) {
	for _, state := range []string{"candidate", "adopted", "discarded"} {
		for _, committed := range []bool{false, true} {
			t.Run(state+map[bool]string{false: " rollback", true: " commit"}[committed], func(t *testing.T) {
				f, b := completedOCRCandidate(t)
				ctx := context.Background()
				revision := int64(1)
				var err error
				if state == "adopted" {
					_, err = f.store.AdoptOCRResult(ctx, alice, ocrResultRequest(b), time.Hour)
					revision = 2
				} else if state == "discarded" {
					_, err = f.store.DiscardOCRResult(ctx, alice, ocrResultRequest(b), time.Hour)
				}
				if err != nil {
					t.Fatal(err)
				}
				count := scalar(t, f.docs, `SELECT count(*) FROM artifacts WHERE document_id=?`, docA)
				cleanup, err := f.store.RequestDelete(ctx, alice, DeleteRequest{DocumentID: docA, Revision: revision, IdempotencyKey: "request_reocr_cleanup_001"}, time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.store.ReadOCRResult(ctx, alice, docA, b.ID); err == nil {
					t.Fatal("deletion did not revoke metadata")
				}
				if _, err = f.store.ReadOCRResultArtifact(ctx, alice, docA, b.ID, b.Pages[0].PageID, "text"); err == nil {
					t.Fatal("deletion did not revoke content")
				}
				if _, err = f.store.ClaimCleanup(ctx, alice, cleanup.ID, runner); err != nil {
					t.Fatal(err)
				}
				work, err := f.store.LoadCleanupWork(ctx, alice, cleanup.ID, runner, 100)
				if err != nil || len(work.Files) != count {
					t.Fatal("history missing from physical cleanup manifest", len(work.Files), count, err)
				}
				acknowledgeAll(t, f, cleanup.ID, work)
				original := f.store.commit
				f.store.commit = func(tx *sql.Tx) error {
					if committed {
						if err := tx.Commit(); err != nil {
							return err
						}
					}
					return errors.New("lost cleanup response")
				}
				if err = f.store.FinishCleanup(ctx, alice, cleanup.ID, runner); !errors.Is(err, ErrCommitUncertain) {
					t.Fatal(err)
				}
				recorded, err := f.store.CleanupFinishedRecorded(ctx, alice, cleanup.ID, runner)
				if err != nil || recorded != committed {
					t.Fatal(recorded, err)
				}
				f.store.commit = original
				if !committed {
					if err = f.store.FinishCleanup(ctx, alice, cleanup.ID, runner); err != nil {
						t.Fatal(err)
					}
				}
				if scalar(t, f.docs, `SELECT count(*) FROM ocr_results WHERE document_id=?`, docA) != 0 || scalar(t, f.docs, `SELECT count(*) FROM artifacts WHERE document_id=?`, docA) != 0 {
					t.Fatal("result history leaked after cleanup")
				}
				rows, err := f.docs.Query(`PRAGMA foreign_key_check`)
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				if rows.Next() {
					t.Fatal("foreign key integrity lost")
				}
			})
		}
	}
}

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
