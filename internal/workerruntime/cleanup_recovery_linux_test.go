//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
)

func TestRuntimeResumesInterruptedCleanupWithFreshRunner(t *testing.T) {
	for _, mode := range []string{"claimed", "quarantined", "unlinked", "acknowledged", "expired-attempt", "expired-job", "bad-manifest"} {
		t.Run(mode, func(t *testing.T) {
			lifetime := time.Hour
			if mode == "expired-job" {
				lifetime = 2 * time.Second
			}
			c, store, db, root := fixtureWithLifetime(t, lifetime)
			ctx := context.Background()
			retention := 72 * time.Hour
			if mode == "expired-job" {
				retention = 3 * time.Second
			}
			cleanup, err := store.RequestDelete(ctx, owner, documents.DeleteRequest{DocumentID: document, Revision: 1, IdempotencyKey: "request_restart_cleanup_01"}, retention)
			if err != nil {
				t.Fatal(err)
			}
			const old = "worker_cleanup_previous_01"
			if mode == "expired-attempt" {
				_, err = store.ClaimCleanupAttempt(ctx, owner, cleanup.ID, "", old, time.Second)
			} else {
				_, err = store.ClaimCleanup(ctx, owner, cleanup.ID, old)
			}
			if err != nil {
				t.Fatal(err)
			}
			work, err := store.LoadCleanupWork(ctx, owner, cleanup.ID, old, 1)
			if err != nil || len(work.Files) != 1 {
				t.Fatal(work, err)
			}
			var start, deadline int64
			if err = db.QueryRow("SELECT started_at_ms,deadline_at_ms FROM jobs WHERE id=?", cleanup.ID).Scan(&start, &deadline); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "quarantined":
				target := filepath.Join(root, ".cleanup", cleanup.ID)
				if err = os.MkdirAll(target, 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.Rename(filepath.Join(root, work.Files[0].RelativePath), filepath.Join(target, work.Files[0].ArtifactID)); err != nil {
					t.Fatal(err)
				}
			case "unlinked", "acknowledged":
				if err = os.Remove(filepath.Join(root, work.Files[0].RelativePath)); err != nil {
					t.Fatal(err)
				}
				if mode == "acknowledged" {
					file := work.Files[0]
					if err = store.RecordCleanupRemoval(ctx, owner, cleanup.ID, old, documents.CleanupRemoval{ArtifactID: file.ArtifactID, Size: file.Size, SHA256: file.SHA256, RemovedAtMS: time.Now().UnixMilli()}); err != nil {
						t.Fatal(err)
					}
				}
			case "bad-manifest":
				execute(t, db, "DELETE FROM cleanup_files WHERE job_id=?", cleanup.ID)
			case "expired-attempt":
				time.Sleep(1100 * time.Millisecond)
			case "expired-job":
				time.Sleep(3100 * time.Millisecond)
			}
			err = Run(ctx, c, true)
			if mode == "bad-manifest" {
				if !errors.Is(err, documents.ErrConflict) {
					t.Fatal("bad manifest resumed", err)
				}
				if _, err = os.Stat(filepath.Join(root, "original.png")); err != nil {
					t.Fatal("manifest mismatch removed original", err)
				}
				return
			}
			if err != nil {
				var state string
				var jobStart, jobDeadline, attemptStart, attemptDeadline int64
				var pending, deleted int
				diagnosticErr := db.QueryRow(`SELECT j.state,j.started_at_ms,j.deadline_at_ms,
 COALESCE(a.started_at_ms,0),COALESCE(a.deadline_at_ms,0),
 (SELECT count(*) FROM cleanup_files WHERE job_id=j.id AND state='pending'),
 (SELECT count(*) FROM cleanup_files WHERE job_id=j.id AND state='deleted')
 FROM jobs j LEFT JOIN cleanup_attempts a ON a.job_id=j.id WHERE j.id=?`, cleanup.ID).Scan(&state, &jobStart, &jobDeadline, &attemptStart, &attemptDeadline, &pending, &deleted)
				_, originalErr := os.Stat(filepath.Join(root, "original.png"))
				t.Logf("cleanup fixture: state=%s job_start=%d job_deadline=%d attempt_start=%d attempt_deadline=%d pending=%d deleted=%d original_missing=%t now=%d read=%v", state, jobStart, jobDeadline, attemptStart, attemptDeadline, pending, deleted, errors.Is(originalErr, os.ErrNotExist), time.Now().UnixMilli(), diagnosticErr)
				t.Fatal(err)
			}
			status, err := store.ReadJob(ctx, owner, cleanup.ID)
			if err != nil || status.State != "succeeded" {
				t.Fatal(status, err)
			}
			var newRunner string
			var sameStart, sameDeadline int64
			if err = db.QueryRow("SELECT runner_id,started_at_ms,deadline_at_ms FROM jobs WHERE id=?", cleanup.ID).Scan(&newRunner, &sameStart, &sameDeadline); err != nil || newRunner == old || sameStart != start || sameDeadline != deadline {
				t.Fatal("immutable history or runner", newRunner, sameStart, sameDeadline, err)
			}
			if _, err = os.Stat(filepath.Join(root, "original.png")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("original retained", err)
			}
			var charged, attempts int
			if err = db.QueryRow("SELECT (SELECT count(*) FROM artifacts),(SELECT count(*) FROM cleanup_attempts)").Scan(&charged, &attempts); err != nil || charged != 0 || attempts != 0 {
				t.Fatal(charged, attempts, err)
			}
			if err = Run(ctx, c, true); err != nil {
				t.Fatal("repeat boot", err)
			}
		})
	}
}
