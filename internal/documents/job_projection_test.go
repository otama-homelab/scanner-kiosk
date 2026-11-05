package documents

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestJobProjectionOwnerExpiryAndSafeErrors(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	must(t, f.docs, "UPDATE jobs SET error_code='private-provider-token/path' WHERE id=?", jobA)
	job, err := f.store.ReadJob(ctx, alice, jobA)
	if err != nil || job.ID != jobA || job.State != "queued" || job.ErrorCode == nil || *job.ErrorCode != "processing_failed" {
		t.Fatal(job, err)
	}
	must(t, f.docs, "UPDATE jobs SET error_code='pdf_encrypted' WHERE id=?", jobA)
	if job, err = f.store.ReadJob(ctx, alice, jobA); err != nil || job.ErrorCode == nil || *job.ErrorCode != "processing_failed" {
		t.Fatal("PDF reason escaped import failure scope", job, err)
	}
	if _, err = f.store.ReadJob(ctx, bob, jobA); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cross-owner job", err)
	}
	f.now = 10000
	if _, err = f.store.ReadJob(ctx, alice, jobA); !errors.Is(err, ErrUnavailable) {
		t.Fatal("expired job", err)
	}
	must(t, f.docs, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES('cleanup_job_000001',?,?,'cleanup','queued',1,'{}',1500,9000)`, docA, alice)
	if job, err = f.store.ReadJob(ctx, alice, "cleanup_job_000001"); err != nil || job.Kind != "cleanup" {
		t.Fatal("minimal cleanup status unavailable", job, err)
	}
	must(t, f.state, "UPDATE principals SET disabled_at_ms=10000 WHERE id=?", alice)
	if _, err = f.store.ReadJob(ctx, alice, "cleanup_job_000001"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("disabled cleanup access", err)
	}
}
func TestJobCancellationReceiptKeepsResourcesAndLatestStatus(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	job, err := f.store.CancelJob(ctx, alice, jobA, "request_cancel_01", time.Hour)
	if err != nil || job.State != "cancelling" {
		t.Fatal(job, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", jobA) != 1 {
		t.Fatal("cancellation released reservation")
	}
	if _, err = f.store.CancelJob(ctx, alice, jobA, "request_cancel_01", time.Hour); err != nil {
		t.Fatal("cancel replay", err)
	}
	must(t, f.docs, "UPDATE jobs SET state='cancelled' WHERE id=?", jobA)
	job, err = f.store.CancelJob(ctx, alice, jobA, "request_cancel_01", time.Hour)
	if err != nil || job.State != "cancelled" {
		t.Fatal("cancel replay did not expose latest status", job, err)
	}
	if _, err = f.store.CancelJob(ctx, alice, jobA, "request_cancel_02", time.Hour); !errors.Is(err, ErrConflict) {
		t.Fatal("terminal job cancellation accepted", err)
	}
	if _, err = f.store.CancelJob(ctx, bob, jobA, "request_cancel_01", time.Hour); !errors.Is(err, ErrUnavailable) {
		t.Fatal("foreign cancellation", err)
	}
	if _, err = f.store.CancelJob(ctx, alice, "another_job_000001", "request_cancel_01", time.Hour); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("changed target replay accepted", err)
	}
	f.now = 10000
	if _, err = f.store.CancelJob(ctx, alice, jobA, "request_cancel_01", time.Hour); !errors.Is(err, ErrGone) {
		t.Fatal("expired cancellation replay", err)
	}
}
