package documents

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func admitWithinOneSlot(f *fixture) (ScanAdmission, error) {
	request := scanRequest()
	limits := scanLimits()
	limits.MaxDocuments = 1
	return f.store.AdmitScan(context.Background(), alice, request, limits)
}
func addIdleScannerForSlots(t *testing.T, f *fixture) {
	t.Helper()
	must(t, f.docs, "INSERT INTO device_leases(device_id,epoch,state) VALUES(?,1,'idle')", scanner)
}
func TestDocumentSlotReturnsOnlyAfterCleanupCompletion(t *testing.T) {
	f, job, work := claimedCleanup(t)
	addIdleScannerForSlots(t, f)
	ctx := context.Background()
	if _, err := admitWithinOneSlot(f); !errors.Is(err, ErrQuota) {
		t.Fatal("running cleanup released slot", err)
	}
	acknowledgeAll(t, f, job, work)
	if _, err := admitWithinOneSlot(f); !errors.Is(err, ErrQuota) {
		t.Fatal("file progress released slot before completion", err)
	}
	if err := f.store.FinishCleanup(ctx, alice, job, runner); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM documents WHERE owner_id=?", alice) != 1 {
		t.Fatal("receipt tombstone removed")
	}
	admitted, err := admitWithinOneSlot(f)
	if err != nil || admitted.DocumentID == docA {
		t.Fatal("completed tombstone still consumed slot", admitted, err)
	}
	replay, err := f.store.RequestDelete(ctx, alice, deleteRequest(), 72*time.Hour)
	if err != nil || replay.ID != job || replay.State != "succeeded" {
		t.Fatal("new admission broke delete replay", replay, err)
	}
}
func TestIncompleteOrInconsistentCleanupStillConsumesDocumentSlot(t *testing.T) {
	for _, mode := range []string{"queued", "running", "expired-pending", "terminal-marker-with-payload", "failed", "late-artifact", "late-reservation"} {
		t.Run(mode, func(t *testing.T) {
			f, job := cleanupFixture(t)
			ctx := context.Background()
			addIdleScannerForSlots(t, f)
			if mode != "queued" {
				if _, err := f.store.ClaimCleanup(ctx, alice, job, runner); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "expired-pending":
				must(t, f.docs, "UPDATE documents SET state='expired' WHERE id=?", docA)
			case "terminal-marker-with-payload":
				must(t, f.docs, "UPDATE jobs SET state='succeeded' WHERE id=?", job)
			case "failed":
				must(t, f.docs, "UPDATE jobs SET state='failed' WHERE id=?", job)
			case "late-artifact", "late-reservation":
				work, err := f.store.LoadCleanupWork(ctx, alice, job, runner, 100)
				if err != nil {
					t.Fatal(err)
				}
				acknowledgeAll(t, f, job, work)
				if err = f.store.FinishCleanup(ctx, alice, job, runner); err != nil {
					t.Fatal(err)
				}
				if mode == "late-artifact" {
					must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('late_slot_artifact_01',?,?,'intermediate','ready','late-slot.bin','application/octet-stream',0,zeroblob(32),1,2000)`, docA, alice)
				} else {
					must(t, f.docs, "INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,1)", job, alice)
				}
			}
			if _, err := admitWithinOneSlot(f); !errors.Is(err, ErrQuota) {
				t.Fatal(mode, "freed document slot", err)
			}
			if mode == "late-artifact" || mode == "late-reservation" {
				if complete, err := f.store.CleanupFinishedRecorded(ctx, alice, job, runner); err != nil || complete {
					t.Fatal("reconciliation disagrees with admission", complete, err)
				}
			}
		})
	}
}

func TestConcurrentNewDocumentsShareOneReclaimedSlot(t *testing.T) {
	f, job, work := claimedCleanup(t)
	ctx := context.Background()
	acknowledgeAll(t, f, job, work)
	if err := f.store.FinishCleanup(ctx, alice, job, runner); err != nil {
		t.Fatal(err)
	}
	addIdleScannerForSlots(t, f)
	second := "scanner_slot_second_01"
	must(t, f.docs, "INSERT INTO device_leases(device_id,epoch,state) VALUES(?,1,'idle')", second)
	limits := scanLimits()
	limits.MaxDocuments = 1
	limits.MaxConcurrentScans = 2
	type outcome struct {
		request  ScanRequest
		admitted ScanAdmission
		err      error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	for index, device := range []string{scanner, second} {
		request := scanRequest()
		request.DeviceID = device
		request.IdempotencyKey = []string{"request_slot_concurrent_01", "request_slot_concurrent_02"}[index]
		wg.Add(1)
		go func() {
			defer wg.Done()
			admitted, err := f.store.AdmitScan(ctx, alice, request, limits)
			results <- outcome{request, admitted, err}
		}()
	}
	wg.Wait()
	close(results)
	accepted, rejected := 0, 0
	for result := range results {
		if result.err == nil {
			accepted++
			replay, err := f.store.AdmitScan(ctx, alice, result.request, limits)
			if err != nil || replay.JobID != result.admitted.JobID {
				t.Fatal("full slot broke receipt replay", replay, err)
			}
		} else if errors.Is(result.err, ErrQuota) {
			rejected++
		} else {
			t.Fatal(result.err)
		}
	}
	if accepted != 1 || rejected != 1 || scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind='scan'") != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 1 || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='held'") != 1 {
		t.Fatal("one reclaimed slot admitted multiple documents", accepted, rejected)
	}
}
func TestDocumentSlotCompletionArtifactLookupUsesIndex(t *testing.T) {
	f, _, _ := claimedCleanup(t)
	rows, err := f.docs.Query(`EXPLAIN QUERY PLAN SELECT count(*) FROM documents d WHERE d.owner_id=? AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.document_id=d.id AND j.owner_id=d.owner_id AND `+completedCleanupPredicate+`)`, alice)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	indexed := false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "artifacts_document_owner") {
			indexed = true
		}
		if strings.Contains(detail, "SCAN artifacts") {
			t.Fatal("per-tombstone artifact scan", detail)
		}
	}
	if err = rows.Err(); err != nil || !indexed {
		t.Fatal("artifact index unused", err)
	}
}
