//go:build linux

package workerruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"scanner-kiosk/internal/scannerdriver"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/workeridentity"
)

const owner = "principal_runtime_001"
const document = "document_runtime_001"

func execute(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T, purpose ...string) (config.Config, *documents.Store, *sql.DB, string) {
	t.Helper()
	return fixtureWithLifetime(t, time.Hour, purpose...)
}
func fixtureWithLifetime(t *testing.T, lifetime time.Duration, purpose ...string) (config.Config, *documents.Store, *sql.DB, string) {
	t.Helper()
	parent := t.TempDir()
	stateRoot, docRoot := filepath.Join(parent, "state"), filepath.Join(parent, "documents")
	for _, p := range []string{stateRoot, docRoot} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	settings := map[string]string{"SCANNER_STATE_DIR": stateRoot, "SCANNER_DOCUMENT_DIR": docRoot, "SCANNER_SCANNERS_JSON": `[{"id":"scanner_fixture_001","name":"Fixture","driver":"sane-airscan","endpoint":"http://192.0.2.10/eSCL"}]`}
	c, err := config.LoadWorker(func(k string) (string, bool) { v, ok := settings[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	state, err := storage.Open(filepath.Join(stateRoot, "state.sqlite3"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	docs, err := storage.Open(filepath.Join(docRoot, "documents.sqlite3"), storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { docs.Close() })
	store, err := documents.New(state, docs, time.Now, func(context.Context) (int64, error) { return 1 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	execute(t, state, "INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'guest',?)", owner, now)
	execute(t, docs, "INSERT INTO owners(id) VALUES(?)", owner)
	docPurpose := "document"
	if len(purpose) > 0 {
		docPurpose = purpose[0]
	}
	execute(t, docs, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,?,'review',?,?)", document, owner, docPurpose, now, now+lifetime.Milliseconds())
	var buffer bytes.Buffer
	if err = png.Encode(&buffer, image.NewGray(image.Rect(0, 0, 80, 100))); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	digest := sha256.Sum256(data)
	if err = os.WriteFile(filepath.Join(docRoot, "original.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	execute(t, docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_runtime_001',?,?,'original','ready','original.png','image/png',?,?,1,?)`, document, owner, len(data), digest[:], now)
	execute(t, docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi) VALUES('page_runtime_00001',?,?,0,'original_runtime_001','a4','gray',300)`, document, owner)
	return c, store, docs, docRoot
}
func admit(t *testing.T, store *documents.Store, format ...string) string {
	settings := documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless"}
	if len(format) > 0 {
		settings = documents.RenderSettings{Format: format[0]}
	}
	t.Helper()
	a, err := store.AdmitRender(context.Background(), owner, documents.RenderAdmission{DocumentID: document, Revision: 1, IdempotencyKey: "request_runtime_001", Settings: settings, ReserveBytes: 1 << 20}, documents.AdmissionLimits{UserMaxBytes: 2 << 20, GlobalMaxBytes: 4 << 20, JobMaxBytes: 1 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return a.JobID
}
func TestRuntimeDispatchesActualPDF(t *testing.T) {
	c, store, docs, root := fixture(t)
	job := admit(t, store)
	if err := Run(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	output, err := store.GetExport(context.Background(), owner, document)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, output.RelativePath))
	if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatal("runtime did not publish PDF")
	}
	var state string
	if err = docs.QueryRow("SELECT state FROM jobs WHERE id=?", job).Scan(&state); err != nil || state != "succeeded" {
		t.Fatal("job not completed")
	}
	if _, err = os.Stat(filepath.Join(root, "original.png")); err != nil {
		t.Fatal("original removed")
	}
}
func TestRuntimeRejectsUncertainPriorBoot(t *testing.T) {
	c, store, docs, _ := fixture(t)
	job := admit(t, store)
	if _, err := store.ClaimExport(context.Background(), owner, job, "worker_previous_001"); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), c, true); !errors.Is(err, ErrReconciliation) {
		t.Fatal("prior work automatically resumed")
	}
	var runner string
	if err := docs.QueryRow("SELECT runner_id FROM jobs WHERE id=?", job).Scan(&runner); err != nil || runner != "worker_previous_001" {
		t.Fatal("old identity overwritten")
	}
}
func TestRuntimeIdleAndLocalExclusion(t *testing.T) {
	c, _, _, root := fixture(t)
	if err := Run(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	guard, err := workeridentity.Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err = Run(context.Background(), c, true); !errors.Is(err, workeridentity.ErrActive) {
		t.Fatal("overlapping local worker started")
	}
}

func TestRuntimeRejectsRequeuedStartedJob(t *testing.T) {
	c, store, docs, _ := fixture(t)
	job := admit(t, store)
	if _, err := store.ClaimExport(context.Background(), owner, job, "worker_previous_001"); err != nil {
		t.Fatal(err)
	}
	execute(t, docs, "UPDATE jobs SET state='queued',runner_id=NULL WHERE id=?", job)
	if err := Run(context.Background(), c, true); !errors.Is(err, ErrReconciliation) {
		t.Fatal("requeued started job bypassed startup recovery")
	}
}

func TestRuntimeExportsPhotoJPEGAndPNG(t *testing.T) {
	for _, format := range []string{"jpeg", "png"} {
		t.Run(format, func(t *testing.T) {
			c, store, docs, root := fixture(t, "photo")
			execute(t, docs, "UPDATE pages SET rotation=90 WHERE document_id=?", document)
			admit(t, store, format)
			original, err := os.ReadFile(filepath.Join(root, "original.png"))
			if err != nil {
				t.Fatal(err)
			}
			if err = Run(context.Background(), c, true); err != nil {
				t.Fatal(err)
			}
			output, err := store.GetExport(context.Background(), owner, document)
			if err != nil {
				t.Fatal(err)
			}
			if output.MediaType != "image/"+format {
				t.Fatal("wrong export media type")
			}
			data, err := os.ReadFile(filepath.Join(root, output.RelativePath))
			if err != nil {
				t.Fatal(err)
			}
			bitmap, actual, err := image.Decode(bytes.NewReader(data))
			if err != nil || actual != format || bitmap.Bounds().Dx() != 100 || bitmap.Bounds().Dy() != 80 {
				t.Fatalf("photo format/rotation: %v", err)
			}
			retained, err := os.ReadFile(filepath.Join(root, "original.png"))
			if err != nil || !bytes.Equal(retained, original) {
				t.Fatal("original changed")
			}
		})
	}
}

func TestBadOptionalOCRDoesNotPreventOrdinaryPDF(t *testing.T) {
	c, store, docs, root := fixture(t)
	t.Setenv("SCANNER_OCR_PLUGINS", "tesseract")
	t.Setenv("SCANNER_OCR_DEFAULT_PLUGIN", "tesseract")
	t.Setenv("SCANNER_OCR_TESSERACT_PATH", filepath.Join(root, "not-installed"))
	request := documents.RenderAdmission{DocumentID: document, Revision: 1, IdempotencyKey: "request_bad_ocr_001", Settings: documents.RenderSettings{Format: "pdf", OCR: documents.RenderOCR{Enabled: true, ProviderID: "tesseract", ModelID: "installed", ModelVersion: "test-version", Languages: []string{"ja", "en"}, QualityPreset: "auto"}}, ReserveBytes: 1 << 20}
	limits := documents.AdmissionLimits{UserMaxBytes: 2 << 20, GlobalMaxBytes: 4 << 20, JobMaxBytes: 1 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour}
	failed, err := store.AdmitRender(context.Background(), owner, request, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err = Run(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	var state string
	if err = docs.QueryRow("SELECT state FROM jobs WHERE id=?", failed.JobID).Scan(&state); err != nil || state != "failed" {
		t.Fatal("unavailable OCR did not finish safely")
	}
	current, err := store.Get(context.Background(), owner, document)
	if err != nil {
		t.Fatal(err)
	}
	request.Revision = current.Revision
	request.IdempotencyKey = "request_after_bad_ocr"
	request.Settings = documents.RenderSettings{Format: "pdf"}
	if _, err = store.AdmitRender(context.Background(), owner, request, limits); err != nil {
		t.Fatal(err)
	}
	if err = Run(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetExport(context.Background(), owner, document); err != nil {
		t.Fatal("bad optional OCR disabled ordinary PDF")
	}
}

// A terminal job row does not prove that native acquisition stopped or that the
// hardware lease was reconciled. Preserve the lease and all reserved resources.
func TestRuntimeRejectsTerminalScanWithUnresolvedLease(t *testing.T) {
	for _, leaseState := range []string{"held", "reconciling"} {
		t.Run(leaseState, func(t *testing.T) {
			c, store, docs, _ := fixture(t)
			execute(t, docs, "INSERT INTO device_leases(device_id,epoch,state) VALUES('scanner_fixture_001',1,'idle')")
			admitted, err := store.AdmitScan(context.Background(), owner, documents.ScanRequest{
				Purpose: "document", DeviceID: "scanner_fixture_001",
				Capture:        documents.Capture{Size: "a4", Color: "gray", DPI: 300},
				IdempotencyKey: "request_scan_runtime_001", ReserveBytes: 1 << 20,
				WorkerBootID: "worker_previous_001",
			}, documents.ScanLimits{AdmissionLimits: documents.AdmissionLimits{
				UserMaxBytes: 2 << 20, GlobalMaxBytes: 4 << 20, JobMaxBytes: 1 << 20,
				MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour,
			}, MaxDocuments: 20, DocumentLifetime: 24 * time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			if leaseState == "reconciling" {
				execute(t, docs, "UPDATE device_leases SET state='reconciling' WHERE job_id=?", admitted.JobID)
			}
			execute(t, docs, "UPDATE jobs SET state='failed' WHERE id=?", admitted.JobID)
			if err := Run(context.Background(), c, true); !errors.Is(err, ErrReconciliation) {
				t.Fatalf("unresolved lease bypassed startup recovery: %v", err)
			}
			var state string
			var epoch, reservations int
			if err := docs.QueryRow("SELECT state,epoch FROM device_leases WHERE job_id=?", admitted.JobID).Scan(&state, &epoch); err != nil || state != leaseState || epoch != 2 {
				t.Fatalf("lease changed: %s %d %v", state, epoch, err)
			}
			if err := docs.QueryRow("SELECT count(*) FROM reservations WHERE job_id=?", admitted.JobID).Scan(&reservations); err != nil || reservations != 1 {
				t.Fatalf("reservation changed: %d %v", reservations, err)
			}
		})
	}
}

func TestRuntimeScannerSessionOptInAndConstructorFailure(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			c, store, docs, root := fixture(t)
			c.ScannerTools.AcquisitionEnabled = enabled
			// Missing native tools must be irrelevant unless explicitly enabled.
			c.ScannerTools.ScanimagePath = filepath.Join(root, "missing-native-tool")
			c.ScannerTools.ParametersPath = c.ScannerTools.ScanimagePath
			job := admit(t, store)
			err := Run(context.Background(), c, true)
			if enabled && !errors.Is(err, scannerdriver.ErrConfiguration) || !enabled && err != nil {
				t.Fatalf("startup result: %v", err)
			}
			var state string
			if err := docs.QueryRow("SELECT state FROM jobs WHERE id=?", job).Scan(&state); err != nil {
				t.Fatal(err)
			}
			expected := "succeeded"
			if enabled {
				expected = "queued"
			}
			if state != expected {
				t.Fatalf("unexpected dispatch: %s", state)
			}
			paths, err := filepath.Glob(filepath.Join(root, "scanner_config_*"))
			if err != nil || len(paths) != 0 {
				t.Fatal("constructor failure leaked owned config", paths, err)
			}
			guard, err := workeridentity.Acquire(root)
			if err != nil {
				t.Fatal("guard not released after cleanup", err)
			}
			guard.Close()
		})
	}
}

func TestRuntimeScannerSessionCancellationAndRetainedForeignInput(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(map[bool]string{false: "clean", true: "foreign"}[foreign], func(t *testing.T) {
			c, _, docs, root := fixture(t)
			c.ScannerTools.AcquisitionEnabled = true
			c.ScannerTools.ScanimagePath, c.ScannerTools.ParametersPath = "/bin/false", "/bin/false"
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
			defer server.Close()
			c.Scanners[0].Endpoint = server.URL + "/eSCL"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- Run(ctx, c, false) }()
			// Active metadata proves Run passed construction. It is not scanner readiness.
			deadline := time.Now().Add(5 * time.Second)
			for {
				var count int
				if err := docs.QueryRow("SELECT count(*) FROM processing_coordinator WHERE state='active'").Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count == 1 {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("worker ended early: %v", err)
				default:
				}
				if time.Now().After(deadline) {
					t.Fatal("worker did not reach running loop")
				}
				time.Sleep(10 * time.Millisecond)
			}
			paths, err := filepath.Glob(filepath.Join(root, "scanner_config_*"))
			if err != nil || len(paths) != 1 {
				t.Fatal("missing session inputs", paths, err)
			}
			if foreign {
				if err := os.WriteFile(filepath.Join(paths[0], "foreign.txt"), []byte("retain"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cancel()
			select {
			case err := <-done:
				if foreign {
					// Metadata inspection can discover changed inputs before cancellation.
					// A retained unknown inspection must not be forced into config cleanup.
					if !errors.Is(err, ErrReconciliation) {
						t.Fatal("changed input lost reconciliation", err)
					}
				} else if !errors.Is(err, context.Canceled) || errors.Is(err, ErrReconciliation) {
					t.Fatal("clean cancellation did not drain", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not stop")
			}
			_, statErr := os.Stat(paths[0])
			if foreign && statErr != nil || !foreign && !errors.Is(statErr, os.ErrNotExist) {
				t.Fatal("unexpected config cleanup", statErr)
			}
			if foreign {
				data, err := os.ReadFile(filepath.Join(paths[0], "foreign.txt"))
				if err != nil || string(data) != "retain" {
					t.Fatal("changed input was removed", err)
				}
			}
			var snapshots int
			if err := docs.QueryRow("SELECT count(*) FROM scanner_metadata_snapshot").Scan(&snapshots); err != nil || snapshots != 0 {
				t.Fatal("shutdown left published scanner metadata", err)
			}
			// Failed readiness observations may contact the fixture, never acquire.
			var leases int
			if err := docs.QueryRow("SELECT count(*) FROM device_leases").Scan(&leases); err != nil || leases != 0 {
				t.Fatal("lifecycle created readiness", err)
			}
			guard, err := workeridentity.Acquire(root)
			if err != nil {
				t.Fatal("guard retained after runtime exit", err)
			}
			guard.Close()
		})
	}
}
