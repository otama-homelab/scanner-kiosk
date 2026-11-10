//go:build linux

package workerruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scanner-kiosk/internal/importupload"
)

func TestPhotoNormalizeRejectsInvalidAndBounded(t *testing.T) {
	for _, mode := range []string{"invalid-child-exit", "wrong-actual-format", "output-limit", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			_, store, db, root := fixture(t)
			raw := photoJPEG(t)
			if mode == "invalid-child-exit" {
				raw = []byte("corrupt compressed image")
			}
			request := photoRequest(t, raw)
			if mode == "wrong-actual-format" {
				request.MediaType = "image/png"
			}
			service, err := importupload.New(root, store, photoLimits())
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close()
			admission, err := service.Receive(context.Background(), owner, request, bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := store.ClaimNextImport(context.Background(), "worker_photo_bounds001")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "output-limit" {
				execute(t, db, "UPDATE reservations SET byte_size=1 WHERE job_id=?", admission.JobID)
			}
			ctx := context.Background()
			var cancel context.CancelFunc
			if mode == "deadline" {
				ctx, cancel = context.WithTimeout(ctx, time.Millisecond)
				defer cancel()
			}
			if err = executeImport(ctx, store, root, owner, claimed.Job.ID, "worker_photo_bounds001"); err != nil {
				t.Fatal("confirmed child failure did not reconcile", err)
			}
			job, err := store.ReadJob(context.Background(), owner, admission.JobID)
			if err != nil || job.State != "failed" && job.State != "interrupted" {
				t.Fatal("hostile input published", job, err)
			}
			for query, want := range map[string]int{"SELECT count(*) FROM pages WHERE document_id=?": 0, "SELECT count(*) FROM artifacts WHERE document_id=?": 1, "SELECT count(*) FROM photo_sources WHERE document_id=?": 0, "SELECT count(*) FROM reservations WHERE job_id=?": 0} {
				target := admission.DocumentID
				if query == "SELECT count(*) FROM reservations WHERE job_id=?" {
					target = admission.JobID
				}
				var n int
				if err = db.QueryRow(query, target).Scan(&n); err != nil || n != want {
					t.Fatal("child failure leaked publication/reservation", query, n, err)
				}
			}
			var path string
			var digest []byte
			if err = db.QueryRow("SELECT a.relative_path,a.sha256 FROM import_intakes i JOIN artifacts a ON a.id=i.artifact_id WHERE i.job_id=?", admission.JobID).Scan(&path, &digest); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, path))
			if err != nil || sha256.Sum256(data) != request.SHA256 {
				t.Fatal("failure changed raw", err)
			}
			directories, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range directories {
				if entry.IsDir() && entry.Name() != filepath.Dir(path) {
					t.Fatal("unpublished scratch leaked", entry.Name())
				}
			}
			replay, err := store.AdmitImport(context.Background(), owner, request, photoLimits())
			if err != nil || !replay.Replay || replay.JobID != admission.JobID {
				t.Fatal("stopped child admitted a duplicate", err)
			}
		})
	}
}
