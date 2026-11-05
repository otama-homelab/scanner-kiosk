package documents

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRecoveryScansEnumeratesOldAuthorityWithoutResumingWork(t *testing.T) {
	for _, mode := range []string{"unstarted", "running", "cancelling", "reconciling", "disabled-expired", "lease-mismatch", "missing-reservation", "unknown-snapshot-field"} {
		t.Run(mode, func(t *testing.T) {
			f := scanFixture(t)
			a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
			if err != nil {
				t.Fatal(err)
			}
			if mode != "unstarted" {
				if _, err = f.store.ClaimScan(context.Background(), alice, a.JobID, runner); err != nil {
					t.Fatal(err)
				}
			}
			wantError := false
			switch mode {
			case "cancelling":
				if err = f.store.RequestCancel(context.Background(), alice, a.JobID); err != nil {
					t.Fatal(err)
				}
			case "reconciling":
				must(t, f.docs, "UPDATE device_leases SET state='reconciling' WHERE job_id=?", a.JobID)
			case "disabled-expired":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
				f.now += (25 * time.Hour).Milliseconds()
			case "lease-mismatch":
				must(t, f.docs, "UPDATE device_leases SET epoch=epoch+1 WHERE job_id=?", a.JobID)
				wantError = true
			case "missing-reservation":
				must(t, f.docs, "DELETE FROM reservations WHERE job_id=?", a.JobID)
				wantError = true
			case "unknown-snapshot-field":
				must(t, f.docs, "DROP TRIGGER job_snapshot_immutable")
				must(t, f.docs, "UPDATE jobs SET configuration_json=json_set(configuration_json,'$.unexpected',true) WHERE id=?", a.JobID)
				wantError = true
			}
			jobs := scalar(t, f.docs, "SELECT count(*) FROM jobs")
			reserved := scalar(t, f.docs, "SELECT COALESCE(sum(byte_size),0) FROM reservations")
			items, err := f.store.RecoveryScans(context.Background())
			if wantError {
				if !errors.Is(err, ErrStorage) || len(items) != 0 {
					t.Fatalf("corrupt authority exposed: %+v %v", items, err)
				}
			} else {
				if err != nil || len(items) != 1 {
					t.Fatalf("recovery candidate: %+v %v", items, err)
				}
				item := items[0]
				if item.JobID != a.JobID || item.OwnerID != alice || item.DocumentID != a.DocumentID || item.Snapshot.WorkerBootID != runner || item.Snapshot.LeaseEpoch != 2 || item.Snapshot.DeviceID != scanner {
					t.Fatalf("wrong old authority: %+v", item)
				}
			}
			if scalar(t, f.docs, "SELECT count(*) FROM jobs") != jobs || scalar(t, f.docs, "SELECT COALESCE(sum(byte_size),0) FROM reservations") != reserved || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=?", a.JobID) != 1 {
				t.Fatal("read-only recovery discovery changed work")
			}
		})
	}
}

func TestRecoveryScansDoesNotReturnCompletedWorkAndHonorsCancellation(t *testing.T) {
	f, a, stop := startedCapture(t)
	if _, err := f.store.CommitScanPage(context.Background(), alice, a.JobID, a.Revision, captureArtifact(), stop); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.RecoveryScans(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM pages WHERE document_id=?", a.DocumentID) != 1 {
		t.Fatal("completed page removed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = f.store.RecoveryScans(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled discovery", err)
	}
}
