package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

const scanner = "scanner_device_0001"

func scanRequest() ScanRequest {
	return ScanRequest{Purpose: "document", DeviceID: scanner, Capture: Capture{Size: "a4", Color: "color", DPI: 300}, IdempotencyKey: "request_scan_00001", ReserveBytes: 50, WorkerBootID: runner}
}
func scanLimits() ScanLimits {
	return ScanLimits{AdmissionLimits: admissionLimits(), MaxDocuments: 20, DocumentLifetime: 24 * time.Hour}
}
func scanFixture(t *testing.T) *fixture {
	t.Helper()
	f := setup(t)
	must(t, f.docs, "DELETE FROM jobs")
	must(t, f.docs, "INSERT INTO device_leases(device_id,epoch,state) VALUES(?,1,'idle')", scanner)
	return f
}
func TestFirstScanAdmissionReplayAndClaim(t *testing.T) {
	f := scanFixture(t)
	first, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	if first.Replay || first.DocumentID == "" || first.JobID == "" || first.Revision != 1 || first.LeaseEpoch != 2 {
		t.Fatal("incorrect scan admission")
	}
	request := scanRequest()
	request.WorkerBootID = "new_worker_boot_001"
	request.ReserveBytes = 10000
	again, err := f.store.AdmitScan(context.Background(), alice, request, scanLimits())
	if err != nil || !again.Replay || again.JobID != first.JobID || again.DocumentID != first.DocumentID || again.LeaseEpoch != 2 {
		t.Fatal("scan replay created new work")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs") != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 1 {
		t.Fatal("duplicate scan job")
	}
	if _, err = f.store.ClaimScan(context.Background(), alice, first.JobID, "new_worker_boot_001"); !errors.Is(err, ErrConflict) {
		t.Fatal("restarted worker auto-resumed old scan")
	}
	job, err := f.store.ClaimScan(context.Background(), alice, first.JobID, runner)
	if err != nil || job.Kind != "scan" {
		t.Fatalf("ready worker could not claim scan: %v", err)
	}
	var snapshot ScanSnapshot
	if json.Unmarshal([]byte(job.Configuration), &snapshot) != nil || snapshot.Capture.DPI != 300 || snapshot.WorkerBootID != runner || snapshot.LeaseEpoch != 2 {
		t.Fatal("invalid immutable scan snapshot")
	}
	if _, err = f.store.ClaimScan(context.Background(), alice, first.JobID, runner); !errors.Is(err, ErrConflict) {
		t.Fatal("scan claimed twice")
	}
	d, err := f.store.Get(context.Background(), alice, first.DocumentID)
	if err != nil || d.ExpiresAtMS-d.CreatedAtMS != (24*time.Hour).Milliseconds() {
		t.Fatal("incorrect initial retention")
	}
}
func TestUnknownBusyOrReconcilingDeviceDoesNotCreateDocument(t *testing.T) {
	for _, mode := range []string{"missing", "held", "reconciling"} {
		t.Run(mode, func(t *testing.T) {
			f := scanFixture(t)
			if mode == "missing" {
				must(t, f.docs, "DELETE FROM device_leases")
			} else {
				first, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
				if err != nil {
					t.Fatal(err)
				}
				if mode == "reconciling" {
					must(t, f.docs, "UPDATE device_leases SET state='reconciling' WHERE job_id=?", first.JobID)
				}
			}
			before := scalar(t, f.docs, "SELECT count(*) FROM documents")
			request := scanRequest()
			request.IdempotencyKey = "request_scan_00002"
			request.WorkerBootID = "worker_other_000001"
			if _, err := f.store.AdmitScan(context.Background(), bob, request, scanLimits()); !errors.Is(err, ErrDeviceBusy) {
				t.Fatalf("unavailable device accepted: %v", err)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM documents") != before {
				t.Fatal("failed admission created document")
			}
		})
	}
}
func TestTwoUsersCompeteForOneDeviceLease(t *testing.T) {
	f := scanFixture(t)
	other := secondStore(t, f)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, store := range []*Store{f.store, other} {
		wg.Add(1)
		go func(i int, store *Store) {
			defer wg.Done()
			owner := alice
			request := scanRequest()
			if i == 1 {
				owner = bob
				request.WorkerBootID = "worker_other_000001"
			}
			_, err := store.AdmitScan(context.Background(), owner, request, scanLimits())
			results <- err
		}(i, store)
	}
	wg.Wait()
	close(results)
	success, busy := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrDeviceBusy) {
			busy++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || busy != 1 || scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind='scan'") != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 1 || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='held' AND epoch=2") != 1 {
		t.Fatal("multiple physical writers admitted")
	}
}
func TestScanAdmissionRejectsCapacityCapabilityAndOwnership(t *testing.T) {
	for _, mode := range []string{"disabled", "dpi", "documents", "quota", "missing-boot", "owner", "revision", "photo"} {
		t.Run(mode, func(t *testing.T) {
			f := scanFixture(t)
			request := scanRequest()
			limits := scanLimits()
			owner := alice
			switch mode {
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
			case "dpi":
				request.Capture.DPI = 1200
			case "documents":
				limits.MaxDocuments = 1
			case "quota":
				limits.UserMaxBytes = 40
				limits.JobMaxBytes = 40
			case "missing-boot":
				request.WorkerBootID = ""
			case "owner":
				request.Purpose = ""
				request.DocumentID = docA
				request.Revision = 1
				owner = bob
			case "revision":
				request.Purpose = ""
				request.DocumentID = docA
				request.Revision = 2
			case "photo":
				must(t, f.docs, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('photo_document_0001',?,'photo','review',1000,10000)", alice)
				seedPage(t, f, "photo_document_0001", alice, "photo_original_0001", "photo_page_0000001")
				request.Purpose = ""
				request.DocumentID = "photo_document_0001"
				request.Revision = 1
			}
			before := scalar(t, f.docs, "SELECT count(*) FROM documents")
			if _, err := f.store.AdmitScan(context.Background(), owner, request, limits); err == nil {
				t.Fatal("invalid scan admitted")
			}
			if scalar(t, f.docs, "SELECT count(*) FROM jobs") != 0 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 || scalar(t, f.docs, "SELECT count(*) FROM request_receipts") != 0 || scalar(t, f.docs, "SELECT count(*) FROM documents") != before || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='idle' AND epoch=1") != 1 {
				t.Fatal("failed scan left partial state")
			}
		})
	}
}
func TestNextScanAdvancesRevisionRetainsPagesAndExpiry(t *testing.T) {
	f := renderFixture(t)
	must(t, f.docs, "INSERT INTO device_leases(device_id,epoch,state) VALUES(?,7,'idle')", scanner)
	request := scanRequest()
	request.Purpose = ""
	request.DocumentID = docA
	request.Revision = 1
	request.Capture = Capture{Size: "a5", Color: "gray", DPI: 600}
	admitted, err := f.store.AdmitScan(context.Background(), alice, request, scanLimits())
	if err != nil || admitted.Revision != 2 || admitted.LeaseEpoch != 8 {
		t.Fatalf("next page not admitted: %v", err)
	}
	d, err := f.store.Get(context.Background(), alice, docA)
	if err != nil || d.ExpiresAtMS != 10000 || d.CreatedAtMS != 1000 || d.State != "scanning" {
		t.Fatal("next scan changed retention")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM pages WHERE document_id=?", docA) != 1 {
		t.Fatal("existing page lost")
	}
	request.Capture.DPI = 300
	if _, err = f.store.AdmitScan(context.Background(), alice, request, scanLimits()); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("same key changed capture settings")
	}
}
func TestScanCommitUncertaintyReplaysOneLease(t *testing.T) {
	f := scanFixture(t)
	f.store.commit = func(tx *sql.Tx) error {
		if err := tx.Commit(); err != nil {
			return err
		}
		return errors.New("injected post-commit error")
	}
	if _, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits()); !errors.Is(err, ErrCommitUncertain) {
		t.Fatal("uncertainty not surfaced")
	}
	f.store.commit = func(tx *sql.Tx) error { return tx.Commit() }
	result, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
	if err != nil || !result.Replay || result.LeaseEpoch != 2 || scalar(t, f.docs, "SELECT count(*) FROM jobs") != 1 {
		t.Fatal("uncertain retry reacquired lease")
	}
}
func TestLeaseEpochOrCancellationPreventsClaim(t *testing.T) {
	for _, mode := range []string{"epoch", "cancel", "expired"} {
		t.Run(mode, func(t *testing.T) {
			f := scanFixture(t)
			a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "epoch":
				must(t, f.docs, "UPDATE device_leases SET epoch=epoch+1 WHERE device_id=?", scanner)
			case "cancel":
				if err = f.store.RequestCancel(context.Background(), alice, a.JobID); err != nil {
					t.Fatal(err)
				}
			case "expired":
				f.now = 2000 + time.Minute.Milliseconds()
			}
			if _, err = f.store.ClaimScan(context.Background(), alice, a.JobID, runner); !errors.Is(err, ErrConflict) {
				t.Fatal("stale capture claim accepted")
			}
		})
	}
}

func TestQueuedScanCannotBeReassignedAcrossWorkerBoots(t *testing.T) {
	f := scanFixture(t)
	a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	// Even a generic recovery routine changing runner_id must not bypass the
	// immutable boot binding in the scan's configuration snapshot.
	nextBoot := "new_worker_boot_001"
	must(t, f.docs, "UPDATE jobs SET runner_id=? WHERE id=?", nextBoot, a.JobID)
	if _, err = f.store.ClaimScan(context.Background(), alice, a.JobID, nextBoot); !errors.Is(err, ErrConflict) {
		t.Fatal("new boot executed a stale physical request")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='held' AND epoch=2") != 1 {
		t.Fatal("failed claim released hardware without reconciliation")
	}
}
func TestExpiredScanReceiptDoesNotReacquireOrUnlockDevice(t *testing.T) {
	f := scanFixture(t)
	a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	f.now += 24 * time.Hour.Milliseconds()
	if _, err = f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits()); !errors.Is(err, ErrGone) {
		t.Fatal("expired scan retried")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=?", a.JobID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='held' AND epoch=2") != 1 {
		t.Fatal("expiry inferred hardware termination")
	}
}

func TestFirstScanCreatesOnlyTemporaryOwnerMirror(t *testing.T) {
	f := scanFixture(t)
	owner := "principal_guest_001"
	must(t, f.state, "INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'guest',1000)", owner)
	if _, err := f.store.AdmitScan(context.Background(), owner, scanRequest(), scanLimits()); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM owners WHERE id=?", owner) != 1 || scalar(t, f.state, "SELECT count(*) FROM principals WHERE id=?", owner) != 1 {
		t.Fatal("principal was recreated instead of mirrored")
	}
}
func TestScanReplayDoesNotReclaimDeviceNowHeldByAnotherJob(t *testing.T) {
	f := scanFixture(t)
	a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the trusted completion/reconciliation boundary. These updates are
	// test setup, not evidence that actual hardware has stopped.
	must(t, f.docs, "UPDATE device_leases SET job_id=NULL,state='idle' WHERE job_id=?", a.JobID)
	must(t, f.docs, "UPDATE jobs SET state='interrupted' WHERE id=?", a.JobID)
	request := scanRequest()
	request.IdempotencyKey = "request_scan_00002"
	b, err := f.store.AdmitScan(context.Background(), bob, request, scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
	if err != nil || !replay.Replay || replay.JobID != a.JobID {
		t.Fatal("old scan did not replay")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=? AND epoch=3 AND state='held'", b.JobID) != 1 {
		t.Fatal("replay disturbed the current writer")
	}
}

func TestPhotoFormatIntentSurvivesEmptyPhotoRetry(t *testing.T) {
	f := scanFixture(t)
	request := scanRequest()
	request.Purpose = "photo"
	request.PhotoFormat = "png"
	first, err := f.store.AdmitScan(context.Background(), alice, request, scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err = f.docs.QueryRow("SELECT configuration_json FROM jobs WHERE id=?", first.JobID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var snapshot ScanSnapshot
	if json.Unmarshal([]byte(raw), &snapshot) != nil || snapshot.PhotoFormat != "png" {
		t.Fatal("photo format choice lost")
	}
	request.PhotoFormat = "jpeg"
	if _, err = f.store.AdmitScan(context.Background(), alice, request, scanLimits()); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("replay changed photo format")
	}
	// Test setup simulates a confirmed stopped failed capture with zero pages.
	must(t, f.docs, "UPDATE device_leases SET job_id=NULL,state='idle' WHERE job_id=?", first.JobID)
	must(t, f.docs, "UPDATE jobs SET state='interrupted' WHERE id=?", first.JobID)
	must(t, f.docs, "DELETE FROM reservations WHERE job_id=?", first.JobID)
	must(t, f.docs, "UPDATE documents SET state='failed' WHERE id=?", first.DocumentID)
	retry := scanRequest()
	retry.DocumentID = first.DocumentID
	retry.Revision = first.Revision
	retry.Purpose = ""
	retry.IdempotencyKey = "request_scan_retry1"
	again, err := f.store.AdmitScan(context.Background(), alice, retry, scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.docs.QueryRow("SELECT configuration_json FROM jobs WHERE id=?", again.JobID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	snapshot = ScanSnapshot{}
	if json.Unmarshal([]byte(raw), &snapshot) != nil || snapshot.PhotoFormat != "png" {
		t.Fatal("retry reset photo intent")
	}
}

func TestScanAndRenderCompeteForReadyWorkerWithoutDelayedScanQueue(t *testing.T) {
	f := setup(t)
	must(t, f.docs, "INSERT INTO device_leases(device_id,epoch,state) VALUES(?,1,'idle')", scanner)
	other := secondStore(t, f)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := f.store.ClaimExport(context.Background(), alice, jobA, runner)
		results <- err
	}()
	go func() {
		defer wg.Done()
		_, err := other.AdmitScan(context.Background(), bob, scanRequest(), scanLimits())
		results <- err
	}()
	wg.Wait()
	close(results)
	success, rejected := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrDeviceBusy) || errors.Is(err, ErrConflict) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || rejected != 1 || scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE runner_id=? AND state IN ('queued','running')", runner) != 1 {
		t.Fatal("scan queued behind a busy single worker")
	}
}
