package documents

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func cleanupFixture(t *testing.T) (*fixture, string) {
	t.Helper()
	f := thumbnailFixture(t)
	ctx := context.Background()
	if err := f.store.PublishExportWithThumbnails(ctx, alice, jobA, runner, 1, output(), []ThumbnailRecord{thumbnailRecord()}); err != nil {
		t.Fatal(err)
	}
	job, err := f.store.RequestDelete(ctx, alice, deleteRequest(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return f, job.ID
}
func TestCleanupClaimFreezesBoundedManifestForDisabledOwner(t *testing.T) {
	f, job := cleanupFixture(t)
	ctx := context.Background()
	must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
	claimed, err := f.store.ClaimCleanup(ctx, alice, job, runner)
	if err != nil || claimed.ID != job || claimed.Kind != "cleanup" {
		t.Fatal(claimed, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM cleanup_files WHERE job_id=? AND state='pending'", job) != 3 || scalar(t, f.docs, "SELECT count(*) FROM artifacts WHERE state='deleting'") != 3 || scalar(t, f.docs, "SELECT count(*) FROM artifacts") != 3 {
		t.Fatal("manifest does not retain complete charged files")
	}
	work, err := f.store.LoadCleanupWork(ctx, alice, job, runner, 2)
	if err != nil || len(work.Files) != 2 || work.Remaining != 3 {
		t.Fatal(work, err)
	}
	for _, file := range work.Files {
		var path string
		var size int64
		var digest []byte
		if err = f.docs.QueryRow("SELECT relative_path,byte_size,sha256 FROM artifacts WHERE id=?", file.ArtifactID).Scan(&path, &size, &digest); err != nil || path != file.RelativePath || size != file.Size || !sameHash(digest, file.SHA256) {
			t.Fatal("manifest not source-bound", file, err)
		}
	}
	if _, err = f.store.ClaimCleanup(ctx, alice, job, runner); !errors.Is(err, ErrConflict) {
		t.Fatal("claim repeated execution", err)
	}
	if _, err = f.store.LoadCleanupWork(ctx, bob, job, runner, 2); !errors.Is(err, ErrConflict) {
		t.Fatal("foreign owner", err)
	}
	if _, err = f.store.LoadCleanupWork(ctx, alice, job, "foreign_runner_001", 2); !errors.Is(err, ErrConflict) {
		t.Fatal("foreign runner", err)
	}
}
func TestCleanupBlockedByUnresolvedProducerResources(t *testing.T) {
	for _, mode := range []string{"running", "terminal-reservation", "reconciling", "staging", "live-document", "held-scan"} {
		t.Run(mode, func(t *testing.T) {
			var f *fixture
			var cleanup string
			ctx := context.Background()
			if mode == "held-scan" {
				var admission ScanAdmission
				f, admission, _ = startedCapture(t)
				job, err := f.store.RequestDelete(ctx, alice, DeleteRequest{DocumentID: admission.DocumentID, Revision: admission.Revision, IdempotencyKey: "request_cleanup_scan"}, 72*time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				cleanup = job.ID
			} else if mode == "running" || mode == "terminal-reservation" || mode == "reconciling" {
				f = thumbnailFixture(t)
				job, err := f.store.RequestDelete(ctx, alice, deleteRequest(), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				cleanup = job.ID
				if mode == "terminal-reservation" {
					must(t, f.docs, "UPDATE jobs SET state='failed' WHERE id=?", jobA)
				}
				if mode == "reconciling" {
					must(t, f.docs, "UPDATE jobs SET state='reconciling' WHERE id=?", jobA)
				}
			} else {
				f, cleanup = cleanupFixture(t)
				if mode == "staging" {
					must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,input_revision,created_at_ms) VALUES('artifact_staging_01',?,?,'intermediate','staging','staging.bin','application/octet-stream',0,1,2000)`, docA, alice)
				}
				if mode == "live-document" {
					must(t, f.docs, "UPDATE documents SET state='review' WHERE id=?", docA)
				}
			}
			if _, err := f.store.ClaimCleanup(ctx, alice, cleanup, runner); !errors.Is(err, ErrConflict) {
				t.Fatal(mode, err)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM cleanup_files") != 0 || scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='queued'", cleanup) != 1 {
				t.Fatal("blocked cleanup partially claimed")
			}
		})
	}
}
func TestCleanupClaimUncertainReadbackDoesNotRepeat(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[committed], func(t *testing.T) {
			f, job := cleanupFixture(t)
			ctx := context.Background()
			f.store.commit = func(tx *sql.Tx) error {
				if committed {
					if err := tx.Commit(); err != nil {
						return err
					}
				}
				return errors.New("lost acknowledgement")
			}
			if _, err := f.store.ClaimCleanup(ctx, alice, job, runner); !errors.Is(err, ErrCommitUncertain) {
				t.Fatal(err)
			}
			f.store.commit = func(tx *sql.Tx) error { return tx.Commit() }
			work, err := f.store.LoadCleanupWork(ctx, alice, job, runner, 100)
			if committed {
				if err != nil || work.Remaining != 3 {
					t.Fatal(work, err)
				}
			} else if !errors.Is(err, ErrConflict) || len(work.Files) != 0 {
				t.Fatal("uncommitted manifest escaped", work, err)
			}
		})
	}
}
func TestCleanupLoadRechecksManifestAndProducerBarrier(t *testing.T) {
	for _, mode := range []string{"late-reservation", "late-artifact", "deadline", "manifest-removed"} {
		t.Run(mode, func(t *testing.T) {
			f, job := cleanupFixture(t)
			ctx := context.Background()
			claimed, err := f.store.ClaimCleanup(ctx, alice, job, runner)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "late-reservation":
				must(t, f.docs, "INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,1)", jobA, alice)
			case "late-artifact":
				must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('late_artifact_00001',?,?,'intermediate','ready','late.bin','application/octet-stream',0,zeroblob(32),1,2000)`, docA, alice)
			case "deadline":
				f.now = claimed.DeadlineAtMS
			case "manifest-removed":
				must(t, f.docs, "DELETE FROM cleanup_files WHERE artifact_id=?", thumbnailRecord().Artifact.ID)
			}
			if work, err := f.store.LoadCleanupWork(ctx, alice, job, runner, 100); !errors.Is(err, ErrConflict) || len(work.Files) != 0 {
				t.Fatal(mode, work, err)
			}
		})
	}
}
func TestAbortUnstartedRenderOnlyAfterWithdrawal(t *testing.T) {
	for _, mode := range []string{"never-started", "claimed", "live-document"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			if mode == "claimed" {
				claim(t, f)
			}
			var cleanup string
			if mode != "live-document" {
				job, err := f.store.RequestDelete(ctx, alice, deleteRequest(), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				cleanup = job.ID
			}
			count, err := f.store.AbortUnstartedRenders(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "never-started" {
				if count != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 || scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='cancelled' AND started_at_ms IS NULL AND error_code='not_started'", jobA) != 1 {
					t.Fatal("unclaimed cancellation failed")
				}
				if _, err = f.store.ClaimCleanup(ctx, alice, cleanup, runner); err != nil {
					t.Fatal(err)
				}
			} else if count != 0 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 1 {
				t.Fatal("claimed/live work released")
			}
		})
	}
}

func TestCleanupManifestIdentityAndInitialStateConstrained(t *testing.T) {
	f, job := cleanupFixture(t)
	ctx := context.Background()
	if _, err := f.store.ClaimCleanup(ctx, alice, job, runner); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`UPDATE cleanup_files SET relative_path='replacement' WHERE artifact_id='thumbnail_alice_001'`, `UPDATE cleanup_files SET byte_size=1 WHERE artifact_id='thumbnail_alice_001'`, `UPDATE cleanup_files SET sha256=zeroblob(32) WHERE artifact_id='thumbnail_alice_001'`} {
		if _, err := f.docs.Exec(query); err == nil {
			t.Fatal("manifest changed", query)
		}
	}
	must(t, f.docs, "DELETE FROM cleanup_files WHERE artifact_id=?", thumbnailRecord().Artifact.ID)
	if _, err := f.docs.Exec(`INSERT INTO cleanup_files(job_id,owner_id,artifact_id,relative_path,byte_size,sha256,state,deleted_at_ms) SELECT ?,owner_id,id,relative_path,byte_size,sha256,'deleted',2000 FROM artifacts WHERE id=?`, job, thumbnailRecord().Artifact.ID); err == nil {
		t.Fatal("manifest inserted pre-deleted")
	}
}

func TestExplicitPrecommitCancellationDoesNotHideCombinedFailure(t *testing.T) {
	if storageOperationError(context.Canceled) != context.Canceled || storageOperationError(context.DeadlineExceeded) != context.DeadlineExceeded {
		t.Fatal("explicit cancellation lost")
	}
	combined := errors.Join(errors.New("independent storage failure"), context.Canceled)
	if readError(context.Canceled) != context.Canceled || readError(context.DeadlineExceeded) != context.DeadlineExceeded || readError(combined) != ErrStorage {
		t.Fatal("row error classification hid cancellation or a combined failure")
	}
	if storageOperationError(combined) != ErrStorage {
		t.Fatal("combined failure was treated as clean cancellation")
	}
}
