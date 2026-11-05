package documents

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestWithdrawnScanMaintenanceReleasesOnlyNeverStartedWork(t *testing.T) {
	for _, mode := range []string{"deleted", "expired", "disabled", "live", "started", "unknown-lease", "heartbeat"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := scanFixture(t)
			a, err := f.store.AdmitScan(ctx, alice, scanRequest(), scanLimits())
			if err != nil {
				t.Fatal(err)
			}
			if mode == "started" {
				if _, err = f.store.ClaimScan(ctx, alice, a.JobID, runner); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "live" {
				if _, err = f.store.RequestDelete(ctx, alice, DeleteRequest{DocumentID: a.DocumentID, Revision: a.Revision, IdempotencyKey: "withdrawn_scan_delete_01"}, 72*time.Hour); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "expired":
				must(t, f.docs, "UPDATE documents SET state='expired' WHERE id=?", a.DocumentID)
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
			case "unknown-lease":
				must(t, f.docs, "UPDATE device_leases SET state='reconciling' WHERE job_id=?", a.JobID)
			case "heartbeat":
				must(t, f.docs, "UPDATE jobs SET heartbeat_at_ms=2000 WHERE id=?", a.JobID)
			}
			count, err := f.store.AbortWithdrawnUnstartedScans(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			expected := 0
			if mode == "deleted" || mode == "expired" || mode == "disabled" {
				expected = 1
			}
			if count != expected {
				t.Fatal(count, expected)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != 1-expected {
				t.Fatal("reservation changed without proof")
			}
			if expected == 1 {
				if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='cancelled' AND started_at_ms IS NULL AND error_code='not_started' AND runner_id=?", a.JobID, runner) != 1 {
					t.Fatal("old boot identity/start not retained")
				}
				if scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=?", a.JobID) != 0 {
					t.Fatal("held lease not released")
				}
				candidate, err := f.store.NextCleanup(ctx)
				if err != nil || candidate.OwnerID != alice {
					t.Fatal("cleanup still blocked", candidate, err)
				}
				if again, err := f.store.AbortWithdrawnUnstartedScans(ctx, 1); err != nil || again != 0 {
					t.Fatal("replayed", again, err)
				}
			}
		})
	}
}

func TestWithdrawnScanMaintenancePreservesCommitUncertainty(t *testing.T) {
	ctx := context.Background()
	f := scanFixture(t)
	a, err := f.store.AdmitScan(ctx, alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.RequestDelete(ctx, alice, DeleteRequest{DocumentID: a.DocumentID, Revision: a.Revision, IdempotencyKey: "withdrawn_scan_delete_01"}, 72*time.Hour); err != nil {
		t.Fatal(err)
	}
	f.store.commit = func(tx *sql.Tx) error {
		if err := tx.Commit(); err != nil {
			return err
		}
		return errors.New("lost acknowledgement")
	}
	if _, err = f.store.AbortWithdrawnUnstartedScans(ctx, 100); !errors.Is(err, ErrCommitUncertain) {
		t.Fatal(err)
	}
	if recorded, err := f.store.ScanStoppedRecorded(ctx, alice, a.JobID); err != nil || !recorded {
		t.Fatal(recorded, err)
	}
	for _, limit := range []int{0, 101} {
		if _, err = f.store.AbortWithdrawnUnstartedScans(ctx, limit); !errors.Is(err, ErrInvalid) {
			t.Fatal(limit, err)
		}
	}
}

func TestCancelledNeverStartedScanIsCompletedWithoutDeletingDocument(t *testing.T) {
	for _, mode := range []string{"unstarted", "started", "heartbeat", "uncertain"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := scanFixture(t)
			a, err := f.store.AdmitScan(ctx, alice, scanRequest(), scanLimits())
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "started":
				if _, err = f.store.ClaimScan(ctx, alice, a.JobID, runner); err != nil {
					t.Fatal(err)
				}
			case "heartbeat":
				must(t, f.docs, "UPDATE jobs SET heartbeat_at_ms=2000 WHERE id=?", a.JobID)
			case "uncertain":
				must(t, f.docs, "UPDATE device_leases SET state='reconciling' WHERE job_id=?", a.JobID)
			}
			key := "request_prelaunch_cancel_001"
			cancelled, err := f.store.CancelJob(ctx, alice, a.JobID, key, 72*time.Hour)
			if err != nil || cancelled.State != "cancelling" {
				t.Fatal(cancelled, err)
			}
			count, err := f.store.AbortWithdrawnUnstartedScans(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			expected := 0
			if mode == "unstarted" {
				expected = 1
			}
			if count != expected {
				t.Fatal("cancelled scan was not handled safely", count, expected)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != 1-expected {
				t.Fatal("wrong reservation release")
			}
			if mode == "unstarted" {
				job, err := f.store.CancelJob(ctx, alice, a.JobID, key, 72*time.Hour)
				if err != nil || job.State != "cancelled" || job.ID != a.JobID {
					t.Fatal("cancel replay did not retain terminal result", job, err)
				}
				if scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='idle' AND job_id IS NULL AND epoch=2") != 1 {
					t.Fatal("device epoch changed")
				}
				document, err := f.store.ReadDocument(ctx, alice, a.DocumentID)
				if err != nil || document.State != "failed" || document.PageCount != 0 {
					t.Fatal("cancel removed or changed document", document, err)
				}
			}
		})
	}
}
