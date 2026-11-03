//go:build linux

package workercatalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scannerreadiness"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/workeridentity"
)

const readyOwner = "principal_ready_001"

func readyFixture(t *testing.T, probes ...documents.SpaceProbe) (*fixture, *documents.Store, Lease, scannerreadiness.Evidence, documents.ScanRequest, documents.ScanLimits) {
	t.Helper()
	f := setup(t)
	ctx := context.Background()
	lease, err := f.store.Begin(ctx, f.guard)
	if err != nil {
		t.Fatal(err)
	}
	f.statePath = filepath.Join(t.TempDir(), "state.sqlite3")
	state, err := storage.Open(f.statePath, storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	if _, err = state.Exec("INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'oidc',1000)", readyOwner); err != nil {
		t.Fatal(err)
	}
	space := documents.SpaceProbe(func(context.Context) (int64, error) { return 1 << 30, nil })
	if len(probes) > 0 {
		space = probes[0]
	}
	records, err := documents.New(state, f.db, func() time.Time { return time.UnixMilli(f.now) }, space)
	if err != nil {
		t.Fatal(err)
	}
	proof := scannerreadiness.Evidence{DeviceID: "scanner_metadata_001", HandoffID: "handoff_ready_0001", PhysicalID: "physical_scanner_0001", ScanConcurrency: 2, OwnershipConfirmedAtMS: 1000, LocalStoppedAtMS: 1000, IdleObservedAtMS: 1000}
	if epoch, err := f.store.PublishScannerReadiness(ctx, lease, proof); err != nil || epoch != 1 {
		t.Fatal(epoch, err)
	}
	proof.LeaseEpoch = 1
	if err = f.store.PublishScanners(ctx, lease, scannerSnapshot(1000)); err != nil {
		t.Fatal(err)
	}
	request := documents.ScanRequest{Purpose: "document", DeviceID: proof.DeviceID, Capture: documents.Capture{Size: "a4", Color: "color", DPI: 300}, IdempotencyKey: "request_ready_0001", ReserveBytes: 50}
	limits := documents.ScanLimits{AdmissionLimits: documents.AdmissionLimits{UserMaxBytes: 1000, GlobalMaxBytes: 2000, JobMaxBytes: 100, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour}, MaxDocuments: 20, DocumentLifetime: 24 * time.Hour, MaxPagePixels: 40000000}
	return f, records, lease, proof, request, limits
}
func readyExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func readyCount(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestReadyScanAdmissionBindsWorkerEvidenceAndReplaysWithoutReadiness(t *testing.T) {
	f, records, _, proof, request, limits := readyFixture(t)
	a, err := records.AdmitReadyScan(context.Background(), readyOwner, request, limits)
	if err != nil || a.LeaseEpoch != 2 {
		t.Fatal(a, err)
	}
	if _, err = records.ClaimScanWithLimit(context.Background(), readyOwner, a.JobID, f.guard.ID(), 2); err != nil {
		t.Fatal("ready scan did not pin actual worker", err)
	}
	readyExec(t, f.db, "DELETE FROM scanner_readiness; DELETE FROM scanner_metadata_snapshot")
	work, loadErr := records.LoadScanWork(context.Background(), readyOwner, a.JobID, f.guard.ID())
	if loadErr != nil || work.Snapshot.PhysicalID != proof.PhysicalID || work.Snapshot.HandoffID != proof.HandoffID {
		t.Fatal("admission lost immutable physical/handoff binding", loadErr)
	}
	var raw map[string]any
	if json.Unmarshal([]byte(work.Job.Configuration), &raw) != nil || raw["physical_id"] != proof.PhysicalID || raw["handoff_id"] != proof.HandoffID {
		t.Fatal("binding absent from job snapshot")
	}
	f.now = 12000
	replay, err := records.AdmitReadyScan(context.Background(), readyOwner, request, limits)
	if err != nil || !replay.Replay || replay.JobID != a.JobID {
		t.Fatal("replay reacquired readiness", replay, err)
	}
	request.Capture.DPI = 600
	if _, err = records.AdmitReadyScan(context.Background(), readyOwner, request, limits); !errors.Is(err, documents.ErrIdempotencyConflict) {
		t.Fatal(err)
	}
	if readyCount(t, f.db, "SELECT count(*) FROM jobs") != 1 || readyCount(t, f.db, "SELECT count(*) FROM device_leases WHERE state='held' AND epoch=2") != 1 {
		t.Fatal("replay changed durable work")
	}
}
func TestReadyScanRejectsMissingStaleOrIncompatibleAuthority(t *testing.T) {
	for _, mode := range []string{"metadata-only", "readiness-only", "stopped", "expired-ready", "epoch", "boot", "generation", "future-ready", "tuple", "pixel-cap", "metadata-unknown-field", "before-boot-observation", "caller-boot", "missing-pixel-cap"} {
		t.Run(mode, func(t *testing.T) {
			f, records, lease, _, request, limits := readyFixture(t)
			switch mode {
			case "metadata-only":
				readyExec(t, f.db, "DELETE FROM scanner_readiness")
			case "readiness-only":
				readyExec(t, f.db, "DELETE FROM scanner_metadata_snapshot")
			case "stopped":
				readyExec(t, f.db, "UPDATE processing_coordinator SET state='stopped'")
			case "expired-ready":
				f.now = 5000
				if err := f.store.Pulse(context.Background(), lease); err != nil {
					t.Fatal(err)
				}
				if err := f.store.PublishScanners(context.Background(), lease, scannerSnapshot(5000)); err != nil {
					t.Fatal(err)
				}
				f.now = 11000
			case "epoch":
				readyExec(t, f.db, "UPDATE device_leases SET epoch=2")
			case "boot":
				readyExec(t, f.db, "UPDATE scanner_readiness SET boot_id='worker_foreign_0001'")
			case "generation":
				readyExec(t, f.db, "UPDATE scanner_readiness SET generation=generation+1")
			case "future-ready":
				readyExec(t, f.db, "UPDATE scanner_readiness SET published_at_ms=2000")
			case "tuple":
				request.Capture.DPI = 600
			case "pixel-cap":
				limits.MaxPagePixels = 1000
			case "metadata-unknown-field":
				readyExec(t, f.db, "UPDATE scanner_metadata_snapshot SET payload=json_set(payload,'$.unknown',true)")
			case "before-boot-observation":
				readyExec(t, f.db, "UPDATE scanner_metadata_snapshot SET payload=json_set(payload,'$.devices[0].profiles[0].checked_at_ms',999)")
			case "caller-boot":
				request.WorkerBootID = f.guard.ID()
			case "missing-pixel-cap":
				limits.MaxPagePixels = 0
			}
			a, err := records.AdmitReadyScan(context.Background(), readyOwner, request, limits)
			if err == nil || a.JobID != "" || a.DocumentID != "" {
				t.Fatal("unverified authority admitted", mode, a, err)
			}
			if readyCount(t, f.db, "SELECT count(*) FROM jobs") != 0 || readyCount(t, f.db, "SELECT count(*) FROM documents") != 0 || readyCount(t, f.db, "SELECT count(*) FROM reservations") != 0 {
				t.Fatal("failed readiness partially committed")
			}
		})
	}
}
func TestReadinessPublicationRejectsInvalidProofAndCannotClearHeldWork(t *testing.T) {
	for _, mode := range []string{"handoff", "physical-missing", "physical-change", "ownership-old", "stop-order", "idle-order", "idle-future", "wrong-epoch", "capacity-change", "foreign-lease", "held"} {
		t.Run(mode, func(t *testing.T) {
			f, records, lease, proof, request, limits := readyFixture(t)
			switch mode {
			case "handoff":
				proof.HandoffID = "short"
			case "physical-missing":
				proof.PhysicalID = ""
			case "physical-change":
				proof.PhysicalID = "physical_scanner_0002"
			case "ownership-old":
				proof.OwnershipConfirmedAtMS = 999
			case "stop-order":
				proof.LocalStoppedAtMS = 999
			case "idle-order":
				proof.IdleObservedAtMS = 999
			case "idle-future":
				proof.IdleObservedAtMS = 1001
			case "wrong-epoch":
				proof.LeaseEpoch = 2
			case "capacity-change":
				proof.ScanConcurrency = 1
			case "foreign-lease":
				lease.boot = "worker_foreign_0001"
			case "held":
				if _, err := records.AdmitReadyScan(context.Background(), readyOwner, request, limits); err != nil {
					t.Fatal(err)
				}
				proof.LeaseEpoch = 2
			}
			epoch, err := f.store.PublishScannerReadiness(context.Background(), lease, proof)
			if !errors.Is(err, ErrUnavailable) || epoch != 0 {
				t.Fatal("unverified proof accepted", mode, epoch, err)
			}
			if mode == "held" && readyCount(t, f.db, "SELECT count(*) FROM device_leases WHERE state='held' AND epoch=2") != 1 {
				t.Fatal("readiness stole held device")
			}
		})
	}
}
func TestReadinessStopAndNewBootWithdrawWithoutReleasingDevices(t *testing.T) {
	f, records, lease, _, request, limits := readyFixture(t)
	if _, err := records.AdmitReadyScan(context.Background(), readyOwner, request, limits); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Stop(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	if readyCount(t, f.db, "SELECT count(*) FROM scanner_readiness") != 0 || readyCount(t, f.db, "SELECT count(*) FROM device_leases WHERE state='held'") != 1 {
		t.Fatal("stop retained readiness or released hardware")
	}
	f.guard.Close()
	guard, err := workeridentity.Acquire(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if _, err = f.store.Begin(context.Background(), guard); err != nil {
		t.Fatal(err)
	}
	request.IdempotencyKey = "request_after_boot_001"
	if _, err = records.AdmitReadyScan(context.Background(), readyOwner, request, limits); !errors.Is(err, documents.ErrDeviceBusy) {
		t.Fatal("new boot reused old idle authority", err)
	}
}
func TestTwoReadyAdmissionsConsumeOneIdleEpoch(t *testing.T) {
	f, records, _, _, request, limits := readyFixture(t)
	state, err := storage.Open(f.statePath, storage.State)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	db, err := storage.Open(filepath.Join(f.root, "documents.sqlite3"), storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	other, err := documents.New(state, db, func() time.Time { return time.UnixMilli(f.now) }, func(context.Context) (int64, error) { return 1 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
	stores := []*documents.Store{records, other}
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for i := range 2 {
		wait.Go(func() {
			q := request
			if i == 1 {
				q.IdempotencyKey = "request_compete_0001"
			}
			_, err := stores[i].AdmitReadyScan(context.Background(), readyOwner, q, limits)
			results <- err
		})
	}
	wait.Wait()
	close(results)
	success, busy := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, documents.ErrDeviceBusy) {
			busy++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || busy != 1 || readyCount(t, f.db, "SELECT count(*) FROM jobs") != 1 {
		t.Fatal(success, busy)
	}
}

func TestReadyAdmissionUsesLiveSharedCapacityAcrossDevices(t *testing.T) {
	f, records, lease, proof, request, limits := readyFixture(t)
	second := proof
	second.DeviceID = "scanner_metadata_002"
	second.PhysicalID = "physical_scanner_0002"
	second.LeaseEpoch = 0
	if _, err := f.store.PublishScannerReadiness(context.Background(), lease, second); err != nil {
		t.Fatal(err)
	}
	metadata := scannerSnapshot(1000)
	device := metadata.Devices[0]
	device.ID = second.DeviceID
	metadata.Devices = append(metadata.Devices, device)
	if err := f.store.PublishScanners(context.Background(), lease, metadata); err != nil {
		t.Fatal(err)
	}
	limits.MaxConcurrentScans = 1 // Stale caller policy cannot replace live worker capacity.
	if _, err := records.AdmitReadyScan(context.Background(), readyOwner, request, limits); err != nil {
		t.Fatal(err)
	}
	request.DeviceID = second.DeviceID
	request.IdempotencyKey = "request_ready_0002"
	if _, err := records.AdmitReadyScan(context.Background(), readyOwner, request, limits); err != nil {
		t.Fatal("different ready device blocked by stale policy", err)
	}
	if readyCount(t, f.db, "SELECT count(*) FROM device_leases WHERE state='held'") != 2 {
		t.Fatal("live capacity not respected")
	}
}
func TestCompletedScanRequiresNewEpochAndPostScanEvidence(t *testing.T) {
	f, records, lease, proof, request, limits := readyFixture(t)
	a, err := records.AdmitReadyScan(context.Background(), readyOwner, request, limits)
	if err != nil {
		t.Fatal(err)
	}
	f.now = 2000
	if _, err = records.ClaimScanWithLimit(context.Background(), readyOwner, a.JobID, f.guard.ID(), 2); err != nil {
		t.Fatal(err)
	}
	f.now = 2300
	stop := documents.ScanStop{DeviceID: proof.DeviceID, WorkerBootID: f.guard.ID(), LeaseEpoch: a.LeaseEpoch, ProcessStoppedAtMS: 2100, IdleObservedAtMS: 2200}
	if err = records.FinishStoppedScan(context.Background(), readyOwner, a.JobID, stop, 2300, "failed"); err != nil {
		t.Fatal(err)
	}
	request.IdempotencyKey = "request_after_scan_001"
	if _, err = records.AdmitReadyScan(context.Background(), readyOwner, request, limits); !errors.Is(err, documents.ErrDeviceBusy) {
		t.Fatal("old idle evidence reused after capture", err)
	}
	proof.LeaseEpoch = a.LeaseEpoch
	if _, err = f.store.PublishScannerReadiness(context.Background(), lease, proof); !errors.Is(err, ErrUnavailable) {
		t.Fatal("pre-scan stop reused at new epoch", err)
	}
	proof.LocalStoppedAtMS = 2100
	proof.IdleObservedAtMS = 2200
	if _, err = f.store.PublishScannerReadiness(context.Background(), lease, proof); err != nil {
		t.Fatal(err)
	}
	if _, err = records.AdmitReadyScan(context.Background(), readyOwner, request, limits); err != nil {
		t.Fatal("fresh post-scan evidence refused", err)
	}
}
func TestReadyAdmissionRechecksAuthorityAfterCapacityProbe(t *testing.T) {
	var advance func()
	f, records, _, _, request, limits := readyFixture(t, func(context.Context) (int64, error) {
		if advance != nil {
			advance()
		}
		return 1 << 30, nil
	})
	advance = func() { f.now = 11000 }
	if _, err := records.AdmitReadyScan(context.Background(), readyOwner, request, limits); !errors.Is(err, documents.ErrDeviceBusy) {
		t.Fatal("readiness expired during capacity probe", err)
	}
	if readyCount(t, f.db, "SELECT count(*) FROM jobs") != 0 || readyCount(t, f.db, "SELECT count(*) FROM documents") != 0 || readyCount(t, f.db, "SELECT count(*) FROM device_leases WHERE state='held'") != 0 {
		t.Fatal("expired readiness partially committed")
	}
}

func TestReadinessRejectsPhysicalAliasesWithoutInitializingAnotherLease(t *testing.T) {
	f, _, lease, proof, _, _ := readyFixture(t)
	proof.DeviceID = "scanner_metadata_alias"
	proof.LeaseEpoch = 0
	if _, err := f.store.PublishScannerReadiness(context.Background(), lease, proof); !errors.Is(err, ErrUnavailable) {
		t.Fatal("same physical scanner registered as concurrent device", err)
	}
	if readyCount(t, f.db, "SELECT count(*) FROM device_leases") != 1 || readyCount(t, f.db, "SELECT count(*) FROM scanner_readiness") != 1 {
		t.Fatal("alias rejection initialized another physical slot")
	}
}
