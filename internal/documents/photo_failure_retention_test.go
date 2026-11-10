package documents

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPhotoFailureReceiptSurvivesCleanupUntilTTL(t *testing.T) {
	for _, action := range []string{"expiry", "delete"} {
		t.Run(action, func(t *testing.T) {
			f := scanFixture(t)
			ctx := context.Background()
			r := photoStoreRequest()
			a, e := f.store.AdmitImport(ctx, alice, r, importLimits())
			if e != nil {
				t.Fatal(e)
			}
			sealPhoto(t, f, a, r)
			if _, e = f.store.ClaimNextImport(ctx, runner); e != nil {
				t.Fatal(e)
			}
			stop := ImportStop{RunnerID: runner, ProcessStoppedAtMS: f.now, WorkspaceCleanedAtMS: f.now, ErrorCode: "photo_invalid"}
			if e = f.store.FinishStoppedImport(ctx, alice, a.JobID, stop, "failed"); e != nil {
				t.Fatal(e)
			}
			if n, e := f.store.QueueExpiredCleanup(ctx, 100, 72*time.Hour); e != nil || n != 0 {
				t.Fatal("failure receipt destroyed before poll", n, e)
			}
			job, e := f.store.ReadJob(ctx, alice, a.JobID)
			if e != nil || job.ErrorCode == nil || *job.ErrorCode != "photo_invalid" {
				t.Fatal("localized reason unavailable", job, e)
			}
			if _, e = f.store.ReadJob(ctx, bob, a.JobID); !errors.Is(e, ErrUnavailable) {
				t.Fatal("cross-owner reason", e)
			}
			receipt, e := f.store.ReadImportReceipt(ctx, alice, r)
			if e != nil || receipt.JobID != a.JobID {
				t.Fatal("receipt lost", receipt, e)
			}
			if _, e = f.store.ReadImportReceipt(ctx, bob, r); e == nil {
				t.Fatal("cross-owner receipt")
			}
			history, _, e := f.store.ReadHistory(ctx, alice, 100, nil)
			if e != nil {
				t.Fatal(e)
			}
			for _, d := range history {
				if d.ID == a.DocumentID {
					t.Fatal("empty failure in history")
				}
			}
			if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != 0 {
				t.Fatal("failed job retained capacity reservation")
			}
			if action == "expiry" {
				f.now += int64(25 * time.Hour / time.Millisecond)
				if n, e := f.store.QueueExpiredCleanup(ctx, 100, 72*time.Hour); e != nil || n < 1 {
					t.Fatal("TTL cleanup missing", n, e)
				}
			} else {
				if _, e = f.store.RequestDelete(ctx, alice, DeleteRequest{DocumentID: a.DocumentID, Revision: 1, IdempotencyKey: "request_failed_photo_delete"}, 72*time.Hour); e != nil {
					t.Fatal("explicit delete blocked", e)
				}
			}
			if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE document_id=? AND kind='cleanup'", a.DocumentID) != 1 {
				t.Fatal("cleanup barrier missing")
			}
			if _, e = f.store.ReadJob(ctx, alice, a.JobID); !errors.Is(e, ErrUnavailable) {
				t.Fatal("retired failure remains readable", e)
			}
		})
	}
}
