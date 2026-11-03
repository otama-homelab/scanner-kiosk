//go:build linux

package scanjob

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scanner/escl"
	"scanner-kiosk/internal/scanner/sane"
	"scanner-kiosk/internal/storage"
)

const (
	owner  = "principal_capture_01"
	boot   = "worker_capture_00001"
	device = "scanner_capture_0001"
)

type faultRecords struct {
	*documents.Store
	mode             string
	writes, reads    int
	stops, stopReads int
}

func (f *faultRecords) LoadScanWork(ctx context.Context, o, j, b string) (documents.ScanWork, error) {
	w, err := f.Store.LoadScanWork(ctx, o, j, b)
	if err != nil {
		return w, fmt.Errorf("load scan work: %w", err)
	}
	return w, nil
}

func (f *faultRecords) PulseScan(ctx context.Context, o, j, b string) error {
	if err := f.Store.PulseScan(ctx, o, j, b); err != nil {
		return fmt.Errorf("pulse scan: %w", err)
	}
	return nil
}

func (f *faultRecords) CommitScanPage(ctx context.Context, o, j string, r int64, a documents.ArtifactRecord, s documents.ScanStop) (documents.ScanPage, error) {
	f.writes++
	if f.mode == "rollback" || f.mode == "unreadable-rollback" {
		return documents.ScanPage{}, documents.ErrCommitUncertain
	}
	page, err := f.Store.CommitScanPage(ctx, o, j, r, a, s)
	if err != nil {
		return page, fmt.Errorf("commit scan page: %w", err)
	}
	if err == nil && (f.mode == "committed-error" || f.mode == "unreadable-commit") {
		return documents.ScanPage{}, documents.ErrCommitUncertain
	}
	return page, err
}
func (f *faultRecords) CaptureRecorded(ctx context.Context, o, j, a string) (bool, error) {
	f.reads++
	if f.mode == "unreadable-commit" || f.mode == "unreadable-rollback" {
		return false, documents.ErrStorage
	}
	return f.Store.CaptureRecorded(ctx, o, j, a)
}

type driver struct {
	capturer            scanner.Capturer
	resolveErr, idleErr error
	plan                scanner.Plan
	idleCalls           int
	checkIdle           func()
}

func (d *driver) Resolve(context.Context, documents.ScanWork) (scanner.Capturer, scanner.Plan, error) {
	return d.capturer, d.plan, d.resolveErr
}
func (d *driver) ObserveIdle(_ context.Context, selected string, after int64) (int64, error) {
	d.idleCalls++
	if selected != device || after <= 0 {
		return 0, errors.New("wrong idle identity")
	}
	if d.checkIdle != nil {
		d.checkIdle()
	}
	return time.Now().UnixMilli(), d.idleErr
}

type captureFunc func(context.Context, scanner.Plan) (scanner.Attempt, error)

func (f captureFunc) Capture(ctx context.Context, p scanner.Plan) (scanner.Attempt, error) {
	return f(ctx, p)
}
func execSQL(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func fixture(t *testing.T, slow bool) (*faultRecords, *sql.DB, string, string, *driver, string) {
	t.Helper()
	root := t.TempDir()
	config := t.TempDir()
	for _, path := range []string{root, config} {
		if err := os.Chmod(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	state, err := storage.Open(filepath.Join(t.TempDir(), "state.db"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	docs, err := storage.Open(filepath.Join(t.TempDir(), "docs.db"), storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { docs.Close() })
	var clockMu sync.Mutex
	var clockSamples [16]int64
	clockCount := 0
	clock := func() time.Time {
		now := time.Now()
		clockMu.Lock()
		clockSamples[clockCount%len(clockSamples)] = now.UnixMilli()
		clockCount++
		clockMu.Unlock()
		return now
	}
	store, err := documents.New(state, docs, clock, func(context.Context) (int64, error) { return 1 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, state, "INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'guest',?)", owner, time.Now().UnixMilli())
	execSQL(t, docs, "INSERT INTO device_leases(device_id,epoch,state) VALUES(?,1,'idle')", device)
	a, err := store.AdmitScan(context.Background(), owner, documents.ScanRequest{Purpose: "document", DeviceID: device, Capture: documents.Capture{Size: "a4", Color: "gray", DPI: 300}, IdempotencyKey: "request_capture_0001", ReserveBytes: 1 << 20, WorkerBootID: boot}, documents.ScanLimits{AdmissionLimits: documents.AdmissionLimits{UserMaxBytes: 2 << 20, GlobalMaxBytes: 4 << 20, JobMaxBytes: 1 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour}, MaxDocuments: 20, DocumentLifetime: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if claimed, claimErr := store.ClaimNextScan(context.Background(), boot); claimErr != nil || claimed.OwnerID != owner || claimed.Job.ID != a.JobID {
		clockMu.Lock()
		t.Logf("dispatch clock calls=%d ring=%v next=%d now=%d", clockCount, clockSamples, clockCount%len(clockSamples), time.Now().UnixMilli())
		clockMu.Unlock()
		var state string
		var created, deadline int64
		var started sql.NullInt64
		diagnosticErr := docs.QueryRow("SELECT state,created_at_ms,started_at_ms,deadline_at_ms FROM jobs WHERE id=?", a.JobID).Scan(&state, &created, &started, &deadline)
		t.Logf("dispatch job state=%s created=%d started=%v deadline=%d read=%v", state, created, started, deadline, diagnosticErr)
		t.Fatal("atomic scan dispatch failed", claimErr)
	}
	write := func(name string, data []byte, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(config, name), data, mode); err != nil {
			t.Fatal(err)
		}
	}
	write("dll.conf", []byte("test\n"), 0600)
	file, err := os.Create(filepath.Join(config, "fixture.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(file, image.NewGray(image.Rect(0, 0, 80, 100))); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf started > \"$SANE_CONFIG_DIR/started\"\n"
	if slow {
		script += "exec /bin/sleep 5\n"
	}
	script += "cat \"$SANE_CONFIG_DIR/fixture.png\"\n"
	write("scanimage", []byte(script), 0700)
	native, err := sane.New(sane.Options{Binary: filepath.Join(config, "scanimage"), ConfigDir: config, WorkRoot: root, DeviceID: device, DeviceName: "test:0", AllowTestBackend: true, MaxBytes: 1 << 20, MaxPixels: 100000, Timeout: 10 * time.Second, StopGrace: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	d := &driver{capturer: native, plan: scanner.Plan{DeviceID: device, Size: "a4", Color: "gray", DPI: 300, Width: 80, Height: 100, ByteBudget: 1 << 20}}
	d.checkIdle = func() {
		if count(t, docs, "SELECT count(*) FROM device_leases WHERE job_id=? AND state='held'", a.JobID) != 1 {
			t.Error("lease released before physical idle")
		}
	}
	return &faultRecords{Store: store}, docs, root, a.JobID, d, filepath.Join(config, "started")
}
func TestNativeScanPublicationAndReadback(t *testing.T) {
	for _, mode := range []string{"success", "committed-error", "rollback", "unreadable-commit", "unreadable-rollback"} {
		t.Run(mode, func(t *testing.T) {
			records, db, root, job, d, _ := fixture(t, false)
			records.mode = mode
			p, err := PrepareClaimed(context.Background(), records, d, owner, job, boot, time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := p.Publish(context.Background())
			if mode == "unreadable-commit" || mode == "unreadable-rollback" {
				if !errors.Is(err, ErrPending) || !errors.Is(p.Finish(context.Background(), "failed"), ErrPending) {
					t.Fatal("unknown publication allowed deletion", err)
				}
				entries, _ := os.ReadDir(root)
				if len(entries) != 1 {
					t.Fatal("unknown output removed")
				}
				records.mode = "success"
				artifact, err = p.Publish(context.Background())
			}
			if mode == "success" || mode == "committed-error" || mode == "unreadable-commit" {
				if err != nil {
					t.Fatal(err)
				}
				data, e := os.ReadFile(filepath.Join(root, artifact.RelativePath))
				if e != nil || sha256.Sum256(data) != artifact.SHA256 {
					t.Fatal("published original missing", e)
				}
				if count(t, db, "SELECT count(*) FROM pages") != 1 || count(t, db, "SELECT count(*) FROM reservations") != 0 || count(t, db, "SELECT count(*) FROM device_leases WHERE state='idle'") != 1 {
					t.Fatal("publication incomplete")
				}
				if _, err = ExecuteClaimed(context.Background(), records, d, owner, job, boot, time.Millisecond); err == nil {
					t.Fatal("completed scan executed again")
				}
			} else {
				if err == nil {
					t.Fatal("rollback accepted")
				}
				if err = p.Finish(context.Background(), "failed"); err != nil {
					t.Fatal(err)
				}
				entries, _ := os.ReadDir(root)
				if len(entries) != 0 || count(t, db, "SELECT count(*) FROM reservations") != 0 {
					t.Fatal("rollback cleanup incomplete")
				}
			}
			if records.writes != 1 {
				t.Fatal("publication retried instead of readback")
			}
		})
	}
}
func TestScanIdleUnknownRetainsOutputAndLease(t *testing.T) {
	records, db, root, job, d, _ := fixture(t, false)
	d.idleErr = errors.New("device status unavailable")
	p, err := ExecuteClaimed(context.Background(), records, d, owner, job, boot, time.Millisecond)
	if p == nil || !errors.Is(err, ErrPending) || records.writes != 0 {
		t.Fatal("missing idle fabricated publication", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 || count(t, db, "SELECT count(*) FROM reservations") != 1 || count(t, db, "SELECT count(*) FROM device_leases WHERE state='held'") != 1 {
		t.Fatal("uncertain device lost resources")
	}
	d.idleErr = nil
	if err = p.Finish(context.Background(), "interrupted"); err != nil {
		t.Fatal(err)
	}
	entries, _ = os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("reconciled cleanup left output")
	}
}
func TestPersistedCancellationStopsNativeScan(t *testing.T) {
	records, db, root, job, d, started := fixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-ticker.C:
				if _, err := os.Stat(started); err == nil {
					done <- records.RequestCancel(ctx, owner, job)
					return
				}
			}
		}
	}()
	p, err := ExecuteClaimed(ctx, records, d, owner, job, boot, time.Millisecond)
	if cancelErr := <-done; cancelErr != nil {
		t.Fatal(cancelErr)
	}
	if p != nil || err == nil {
		t.Fatal("cancelled native scan accepted", err)
	}
	if count(t, db, "SELECT count(*) FROM jobs WHERE id=? AND state='cancelled'", job) != 1 || count(t, db, "SELECT count(*) FROM reservations") != 0 || count(t, db, "SELECT count(*) FROM device_leases WHERE state='idle'") != 1 {
		t.Fatal("cancel cleanup incomplete")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("cancel left unpublished files")
	}
}
func TestResolveFailureNeverLaunchesScan(t *testing.T) {
	records, db, _, job, d, started := fixture(t, false)
	d.plan.DPI = 600
	p, err := ExecuteClaimed(context.Background(), records, d, owner, job, boot, time.Millisecond)
	if p != nil || !errors.Is(err, documents.ErrInvalid) {
		t.Fatal("mismatched plan accepted", err)
	}
	if _, err = os.Stat(started); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid plan launched hardware")
	}
	if count(t, db, "SELECT count(*) FROM reservations") != 0 {
		t.Fatal("prelaunch cleanup incomplete")
	}
}

type uncertainAttempt struct {
	scanner.Attempt
	closeCalls int
}

func (a *uncertainAttempt) StoppedAtMS() int64 { return 0 }
func (a *uncertainAttempt) Close() error {
	a.closeCalls++
	return errors.New("must not clean unknown execution")
}
func TestUnknownLocalStopNeverCleansOrObservesIdle(t *testing.T) {
	records, db, root, job, d, _ := fixture(t, false)
	native := d.capturer
	var attempt *uncertainAttempt
	d.capturer = captureFunc(func(ctx context.Context, plan scanner.Plan) (scanner.Attempt, error) {
		a, err := native.Capture(ctx, plan)
		if err != nil {
			return a, err
		}
		attempt = &uncertainAttempt{Attempt: a}
		return attempt, errors.New("local process stop unknown")
	})
	p, err := ExecuteClaimed(context.Background(), records, d, owner, job, boot, time.Millisecond)
	if p == nil || err == nil || attempt == nil || attempt.closeCalls != 0 || d.idleCalls != 0 || records.writes != 0 {
		t.Fatal("unknown local execution escaped reconciliation", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 || count(t, db, "SELECT count(*) FROM reservations") != 1 {
		t.Fatal("uncertain local work was deleted")
	}
	// Test-only teardown: the underlying adapter actually stopped; the coordinator
	// was deliberately denied that evidence and must not infer it itself.
	if err = attempt.Attempt.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestMissingAttemptConservativelyRetainsReservation(t *testing.T) {
	records, db, _, job, d, _ := fixture(t, false)
	d.capturer = captureFunc(func(context.Context, scanner.Plan) (scanner.Attempt, error) {
		return nil, errors.New("driver failed without ownership evidence")
	})
	p, err := ExecuteClaimed(context.Background(), records, d, owner, job, boot, time.Millisecond)
	if p == nil || err == nil || d.idleCalls != 0 || records.writes != 0 || count(t, db, "SELECT count(*) FROM reservations") != 1 {
		t.Fatal("missing capture outcome released resources", err)
	}
}

func TestInstalledSaneScanJob(t *testing.T) {
	binary := os.Getenv("SCANNER_TEST_SANE_BINARY")
	if binary == "" {
		t.Skip("official SANE fixture not requested")
	}
	records, db, root, job, d, started := fixture(t, false)
	config := filepath.Dir(started)
	if err := os.WriteFile(filepath.Join(config, "test.conf"), []byte("number_of_devices 1\ndepth 8\ngeometry_max 300\n"), 0600); err != nil {
		t.Fatal(err)
	}
	native, err := sane.New(sane.Options{Binary: binary, ConfigDir: config, WorkRoot: root, DeviceID: device, DeviceName: "test:0", AllowTestBackend: true, LibraryDirs: strings.Split(os.Getenv("SCANNER_TEST_SANE_LIBRARY_DIRS"), ":"), MaxBytes: 1 << 20, MaxPixels: 10000000, Timeout: 10 * time.Second, StopGrace: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	d.capturer = native
	d.plan.Width = 2480
	d.plan.Height = 3507
	p, err := ExecuteClaimed(context.Background(), records, d, owner, job, boot, 10*time.Millisecond)
	if p != nil || err != nil {
		t.Fatal("official virtual capture did not publish", err)
	}
	if count(t, db, "SELECT count(*) FROM pages WHERE capture_size='a4' AND capture_color='gray' AND capture_dpi=300") != 1 || count(t, db, "SELECT count(*) FROM reservations") != 0 {
		t.Fatal("virtual capture metadata/cleanup mismatch")
	}
}

type cleanupFailure struct {
	scanner.Attempt
	fail bool
}

func (a *cleanupFailure) Close() error {
	if a.fail {
		return errors.New("injected cleanup failure")
	}
	return a.Attempt.Close()
}
func TestCleanupFailureRetainsReservationUntilRetry(t *testing.T) {
	records, db, root, job, d, _ := fixture(t, false)
	native := d.capturer
	var attempt *cleanupFailure
	d.capturer = captureFunc(func(ctx context.Context, plan scanner.Plan) (scanner.Attempt, error) {
		a, err := native.Capture(ctx, plan)
		if err != nil {
			return a, err
		}
		attempt = &cleanupFailure{Attempt: a, fail: true}
		return attempt, errors.New("capture failed after local stop")
	})
	p, err := ExecuteClaimed(context.Background(), records, d, owner, job, boot, time.Millisecond)
	if p == nil || err == nil || count(t, db, "SELECT count(*) FROM reservations") != 1 {
		t.Fatal("failed cleanup released capacity", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatal("failed cleanup lost ownership")
	}
	attempt.fail = false
	if err = p.Finish(context.Background(), "failed"); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "SELECT count(*) FROM reservations") != 0 {
		t.Fatal("cleanup retry did not release capacity")
	}
}

func (f *faultRecords) FinishStoppedScan(ctx context.Context, o, j string, s documents.ScanStop, cleaned int64, outcome string) error {
	f.stops++
	if f.mode == "cleanup-rollback" {
		return documents.ErrCommitUncertain
	}
	err := f.Store.FinishStoppedScan(ctx, o, j, s, cleaned, outcome)
	if err == nil && (f.mode == "cleanup-commit" || f.mode == "cleanup-unreadable") {
		return documents.ErrCommitUncertain
	}
	return err
}
func (f *faultRecords) ScanStoppedRecorded(ctx context.Context, o, j string) (bool, error) {
	f.stopReads++
	if f.mode == "cleanup-unreadable" {
		return false, documents.ErrStorage
	}
	return f.Store.ScanStoppedRecorded(ctx, o, j)
}
func TestCleanupCommitReadbackNeverRepeatsCapture(t *testing.T) {
	for _, mode := range []string{"cleanup-commit", "cleanup-unreadable", "cleanup-rollback"} {
		t.Run(mode, func(t *testing.T) {
			records, db, root, job, d, _ := fixture(t, false)
			p, err := PrepareClaimed(context.Background(), records, d, owner, job, boot, time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			records.mode = mode
			err = p.Finish(context.Background(), "interrupted")
			if mode == "cleanup-unreadable" {
				if !errors.Is(err, ErrPending) {
					t.Fatal("uncertain cleanup not retained", err)
				}
				records.mode = "success"
				err = p.Finish(context.Background(), "interrupted")
			}
			if mode == "cleanup-rollback" {
				if !errors.Is(err, documents.ErrCommitUncertain) || count(t, db, "SELECT count(*) FROM reservations") != 1 {
					t.Fatal("cleanup rollback released resources", err)
				}
				records.mode = "success"
				err = p.Finish(context.Background(), "interrupted")
			}
			if err != nil {
				t.Fatal(err)
			}
			expected := 1
			if mode == "cleanup-rollback" {
				expected = 2
			}
			if records.stops != expected || records.writes != 0 || count(t, db, "SELECT count(*) FROM reservations") != 0 {
				t.Fatal("cleanup reconciliation repeated/retained incorrect work")
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 0 {
				t.Fatal("cleanup reconciliation left files")
			}
		})
	}
}
func TestCancellationBeforePublicationDiscardsOriginal(t *testing.T) {
	records, db, root, job, d, _ := fixture(t, false)
	p, err := PrepareClaimed(context.Background(), records, d, owner, job, boot, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err = records.RequestCancel(context.Background(), owner, job); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Publish(context.Background()); err == nil {
		t.Fatal("cancelled scan published")
	}
	if err = p.Finish(context.Background(), "interrupted"); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 || count(t, db, "SELECT count(*) FROM pages") != 0 || count(t, db, "SELECT count(*) FROM reservations") != 0 {
		t.Fatal("late cancellation lost cleanup or published image")
	}
}

type observedDriver struct {
	*driver
	observer *escl.Observer
}

func (d observedDriver) ObserveIdle(ctx context.Context, id string, after int64) (int64, error) {
	if d.checkIdle != nil {
		d.checkIdle()
	}
	return d.observer.ObserveIdle(ctx, id, after)
}
func TestESCLStatusControlsScanPublication(t *testing.T) {
	for _, state := range []string{"Idle", "Processing"} {
		t.Run(state, func(t *testing.T) {
			records, db, root, job, d, _ := fixture(t, false)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/eSCL/ScannerStatus" {
					t.Error("unexpected status request")
				}
				fmt.Fprintf(w, `<s:ScannerStatus xmlns:s="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:p="http://www.pwg.org/schemas/2010/12/sm"><p:Version>2.0</p:Version><p:State>%s</p:State><s:Jobs/></s:ScannerStatus>`, state)
			}))
			defer server.Close()
			observer, err := escl.New([]escl.Device{{ID: device, Endpoint: server.URL + "/eSCL"}}, escl.Options{Timeout: 5 * time.Second, MaxResponseBytes: 4096})
			if err != nil {
				t.Fatal(err)
			}
			p, err := ExecuteClaimed(context.Background(), records, observedDriver{driver: d, observer: observer}, owner, job, boot, time.Millisecond)
			if state == "Idle" {
				if p != nil || err != nil || count(t, db, "SELECT count(*) FROM pages") != 1 || count(t, db, "SELECT count(*) FROM reservations") != 0 {
					t.Fatalf("HTTP idle did not allow conditional publication: %v; recovery retained=%t", err, p != nil)
				}
			} else {
				if p == nil || !errors.Is(err, ErrPending) || count(t, db, "SELECT count(*) FROM pages") != 0 || count(t, db, "SELECT count(*) FROM reservations") != 1 || count(t, db, "SELECT count(*) FROM device_leases WHERE state='held'") != 1 {
					t.Fatal("HTTP busy allowed publication or lease release", err)
				}
				entries, _ := os.ReadDir(root)
				if len(entries) != 1 {
					t.Fatal("busy device lost owned bitmap")
				}
				// Local command actually stopped; test-only teardown leaves the simulated
				// DB lease held, as a real reconciler must still observe physical idle.
				if err = p.attempt.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestUnresolvedDriverInspectionKeepsLeaseAndReservation(t *testing.T) {
	records, db, _, job, d, _ := fixture(t, false)
	d.resolveErr = scanner.ErrUnresolved
	prepared, err := PrepareClaimed(context.Background(), records, d, owner, job, boot, time.Millisecond)
	if !errors.Is(err, scanner.ErrUnresolved) || prepared == nil {
		t.Fatal("unresolved inspection hidden", err)
	}
	if err = prepared.Finish(context.Background(), "failed"); !errors.Is(err, ErrPending) {
		t.Fatal("inspection uncertainty terminalized", err)
	}
	if d.idleCalls != 0 || records.stops != 0 || count(t, db, "SELECT count(*) FROM device_leases WHERE job_id=? AND state='held'", job) != 1 || count(t, db, "SELECT count(*) FROM reservations WHERE job_id=?", job) != 1 {
		t.Fatal("uncertain resolution released physical authority")
	}
}
