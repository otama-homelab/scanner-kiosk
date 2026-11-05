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

func TestRecoveryRenderDiscoveryAndArtifactReferences(t *testing.T) {
	f := renderFixture(t)
	a, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimExport(context.Background(), alice, a.JobID, runner); err != nil {
		t.Fatal(err)
	}
	must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
	f.now += (25 * time.Hour).Milliseconds()
	items, err := f.store.RecoveryRenders(context.Background())
	if err != nil || len(items) != 1 || items[0].JobID != a.JobID || items[0].OwnerID != alice || items[0].RunnerID != runner {
		t.Fatal(items, err)
	}
	// A directory containing a retained artifact is never disposable, even for a disabled/expired owner.
	raw := "capture_recovery_original_0001"
	must(t, f.docs, "DROP TRIGGER artifact_identity_immutable")
	must(t, f.docs, "UPDATE artifacts SET relative_path=? WHERE document_id=?", raw+"/original.png", docA)
	disposable, err := f.store.RecoveryDirectoryUnreferenced(context.Background(), raw)
	if err != nil || disposable {
		t.Fatal("registered directory exposed to cleanup", disposable, err)
	}
	disposable, err = f.store.RecoveryDirectoryUnreferenced(context.Background(), raw+"_different")
	if err != nil || !disposable {
		t.Fatal("unreferenced directory not found", disposable, err)
	}
	if _, err = f.store.RecoveryDirectoryUnreferenced(context.Background(), "../outside"); !errors.Is(err, ErrInvalid) {
		t.Fatal("path traversal accepted", err)
	}
}

func TestCancelledUnstartedRenderRestoresReviewWithoutLosingPages(t *testing.T) {
	for _, mode := range []string{"unstarted", "started", "heartbeat", "produced"} {
		t.Run(mode, func(t *testing.T) {
			f := renderFixture(t)
			a, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
			if err != nil {
				t.Fatal(err)
			}
			if mode == "started" {
				if _, err = f.store.ClaimExport(context.Background(), alice, a.JobID, runner); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "heartbeat" {
				must(t, f.docs, "UPDATE jobs SET heartbeat_at_ms=2000 WHERE id=?", a.JobID)
			}
			if mode == "produced" {
				must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('artifact_recovery_output_001',?,?,'output','ready','work_recovery_001/output','application/pdf',1,zeroblob(32),?,2000)`, docA, alice, a.Revision)
				must(t, f.docs, `INSERT INTO job_artifacts(job_id,document_id,owner_id,artifact_id) VALUES(?,?,?,'artifact_recovery_output_001')`, a.JobID, docA, alice)
			}
			if err = f.store.RequestCancel(context.Background(), alice, a.JobID); err != nil {
				t.Fatal(err)
			}
			count, err := f.store.AbortUnstartedRenders(context.Background(), 100)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "unstarted" {
				if count != 0 {
					t.Fatal("uncertain work cleared")
				}
				return
			}
			if count != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != 0 {
				t.Fatal("never-started cancellation not finished", count)
			}
			document, err := f.store.ReadDocument(context.Background(), alice, docA)
			if err != nil || document.State != "review" || document.PageCount != 1 {
				t.Fatal("prior pages not editable", document, err)
			}
		})
	}
}
