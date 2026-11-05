//go:build linux

package workerruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannermetadata"
)

func shellLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

// The native executables and eSCL server below are explicit local fixtures.
// Admission, fresh worker identity, Session, executor, image validation, SQLite
// publication, native process supervision and shutdown are product code.
func TestRuntimeDispatchesAdmittedScanThroughSession(t *testing.T) {
	for _, mode := range []string{"success", "small-budget", "busy", "cancel", "identity-bound", "identity-changed", "identity-postcapture"} {
		t.Run(mode, func(t *testing.T) {
			c, store, docs, root := fixture(t)
			successful := mode == "success" || mode == "small-budget" || mode == "identity-bound"
			bound := true // all native Session admissions require a physical binding
			var identityCalls atomic.Int32
			reserve := int64(2 << 20)
			if mode == "small-budget" {
				reserve = 1 << 20
			}
			tools := t.TempDir()
			marker := filepath.Join(tools, "capture-count")
			options := fmt.Sprintf("All options specific to device `airscan:e0:%s':\n    --source Flatbed [Flatbed]\n    --mode Color|Gray [Gray]\n    --resolution 75|100|150|200|300|600dpi [300]\n    -l 0..216mm [0]\n    -t 0..300mm [0]\n    -x 0..216mm [210]\n    -y 0..300mm [297]\n", c.Scanners[0].ID)
			capture := "#!/bin/sh\nfor argument; do\n if [ \"$argument\" = --all-options ]; then\n  printf '%s' " + shellLiteral(options) + "\n  exit 0\n fi\ndone\nprintf x >> " + shellLiteral(marker) + "\n"
			if mode == "cancel" {
				capture += "exec /bin/sleep 30\n"
			}
			capture += "/bin/cat " + shellLiteral(filepath.Join(root, "original.png")) + "\n"
			c.ScannerTools.ScanimagePath = filepath.Join(tools, "scanimage")
			c.ScannerTools.ParametersPath = filepath.Join(tools, "parameters")
			params := `{"protocol_version":1,"size":"a4","color":"gray","dpi":300,"width":80,"height":100,"bytes_per_line":80,"depth":8,"channels":1,"last_frame":true,"sane_version":16908289}`
			for name, data := range map[string]string{c.ScannerTools.ScanimagePath: capture, c.ScannerTools.ParametersPath: "#!/bin/sh\nprintf '%s' " + shellLiteral(params) + "\n"} {
				if err := os.WriteFile(name, []byte(data), 0700); err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" && r.URL.Path == "/eSCL/ScannerCapabilities" {
					identityCalls.Add(1)
					var admittedJobs int
					_ = docs.QueryRow("SELECT count(*) FROM jobs WHERE kind='scan'").Scan(&admittedJobs)
					_, captured := os.Stat(marker)
					uuid := "12abcdef-3456-789a-bcde-1234567890ab"
					if mode == "identity-changed" && admittedJobs > 0 || mode == "identity-postcapture" && captured == nil {
						uuid = "abcdef12-3456-789a-bcde-1234567890ab"
					}
					fmt.Fprintf(w, `<s:ScannerCapabilities xmlns:s="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:p="http://www.pwg.org/schemas/2010/12/sm"><p:Version>2.0</p:Version><s:UUID>%s</s:UUID></s:ScannerCapabilities>`, uuid)
					return
				}
				if r.Method != "GET" || r.URL.Path != "/eSCL/ScannerStatus" {
					t.Error("unexpected device request", r.Method, r.URL.Path)
					w.WriteHeader(500)
					return
				}
				state := "Idle"
				if _, captured := os.Stat(marker); mode == "busy" && captured == nil {
					state = "Processing"
				}
				fmt.Fprintf(w, `<scan:ScannerStatus xmlns:scan="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:pwg="http://www.pwg.org/schemas/2010/12/sm"><pwg:Version>2.0</pwg:Version><pwg:State>%s</pwg:State></scan:ScannerStatus>`, state)
			}))
			defer server.Close()
			c.Scanners[0].Endpoint, c.ScannerTools.AcquisitionEnabled = server.URL+"/eSCL", true
			c.Durations["SCANNER_PROCESS_STOP_GRACE_SECONDS"] = 100 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- Run(ctx, c, false) }()
			joined := false
			defer func() {
				cancel()
				if !joined {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("worker did not drain")
					}
				}
			}()
			wait := func(check func() bool) {
				t.Helper()
				deadline := time.Now().Add(8 * time.Second)
				for !check() {
					select {
					case err := <-done:
						joined = true
						var state, configuration string
						var started sql.NullInt64
						var deadline, heartbeat int64
						diagnosticErr := docs.QueryRow("SELECT state,configuration_json,started_at_ms,deadline_at_ms,heartbeat_at_ms FROM jobs WHERE kind='scan' ORDER BY created_at_ms DESC LIMIT 1").Scan(&state, &configuration, &started, &deadline, &heartbeat)
						t.Logf("scan fixture: state=%s started=%v deadline=%d heartbeat=%d now=%d configuration=%s diagnostic=%v", state, started, deadline, heartbeat, time.Now().UnixMilli(), configuration, diagnosticErr)
						var coordinatorState string
						var created, pulse, until int64
						var ocrRows, scannerRows int
						catalogErr := docs.QueryRow(`SELECT state,created_at_ms,heartbeat_at_ms,valid_until_ms,
 (SELECT count(*) FROM ocr_catalog_snapshot), (SELECT count(*) FROM scanner_metadata_snapshot)
 FROM processing_coordinator WHERE singleton=1`).Scan(&coordinatorState, &created, &pulse, &until, &ocrRows, &scannerRows)
						t.Logf("catalog fixture: state=%s created=%d heartbeat=%d until=%d now=%d OCR=%d scanners=%d read=%v", coordinatorState, created, pulse, until, time.Now().UnixMilli(), ocrRows, scannerRows, catalogErr)
						t.Fatalf("worker ended early: %v", err)
					default:
					}
					if time.Now().After(deadline) {
						t.Fatal("worker condition timed out")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			var boot string
			wait(func() bool {
				return docs.QueryRow("SELECT boot_id FROM processing_coordinator WHERE state='active'").Scan(&boot) == nil
			})
			request := documents.ScanRequest{Purpose: "document", DeviceID: c.Scanners[0].ID, Capture: documents.Capture{Size: "a4", Color: "gray", DPI: 300}, WorkerBootID: boot, IdempotencyKey: "request_runtime_scan_01", ReserveBytes: reserve}
			scanLimits := documents.ScanLimits{AdmissionLimits: documents.AdmissionLimits{UserMaxBytes: 8 << 20, GlobalMaxBytes: 16 << 20, JobMaxBytes: 2 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour}, MaxDocuments: 20, DocumentLifetime: 24 * time.Hour}
			var a documents.ScanAdmission
			var err error
			if bound {
				// Readiness is produced by the runtime against the local eSCL fixture.
				wait(func() bool {
					var payload string
					if docs.QueryRow("SELECT payload FROM scanner_metadata_snapshot WHERE boot_id=?", boot).Scan(&payload) != nil {
						return false
					}
					var snapshot scannermetadata.Snapshot
					if json.Unmarshal([]byte(payload), &snapshot) != nil {
						return false
					}
					return len(snapshot.Devices) == 1 && len(snapshot.Devices[0].Profiles) > 0
				})
				request.WorkerBootID = ""
				scanLimits.MaxPagePixels = c.Limits["SCANNER_MAX_PAGE_PIXELS"]
				scanLimits.RegisteredDevices = []string{c.Scanners[0].ID}
				wait(func() bool {
					a, err = store.AdmitReadyScan(ctx, owner, request, scanLimits)
					return err == nil
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode != "identity-changed" {
				wait(func() bool { info, err := os.Stat(marker); return err == nil && info.Size() == 1 })
			}
			if mode == "cancel" {
				cancel()
			}
			if successful {
				wait(func() bool {
					var state string
					return docs.QueryRow("SELECT state FROM jobs WHERE id=?", a.JobID).Scan(&state) == nil && state == "succeeded"
				})
				var metadata scannermetadata.Snapshot
				wait(func() bool {
					var payload string
					if docs.QueryRow("SELECT payload FROM scanner_metadata_snapshot").Scan(&payload) != nil {
						return false
					}
					return json.Unmarshal([]byte(payload), &metadata) == nil
				})
				if len(metadata.Devices) != 1 || metadata.Devices[0].ID != c.Scanners[0].ID || len(metadata.Devices[0].Profiles) != 1 || metadata.Devices[0].Profiles[0].Capture != (scanner.Capture{Size: "a4", Color: "gray", DPI: 300}) {
					t.Fatal("worker published unverified setting tuples", metadata)
				}

				if mode == "identity-bound" {
					wait(func() bool {
						var epoch int64
						return docs.QueryRow("SELECT lease_epoch FROM scanner_readiness WHERE device_id=? AND boot_id=? AND handoff_id=?", c.Scanners[0].ID, boot, boot).Scan(&epoch) == nil && epoch == a.LeaseEpoch
					})
				}
				cancel()
			}
			select {
			case err := <-done:
				joined = true
				if mode == "busy" || mode == "identity-changed" || mode == "identity-postcapture" {
					if !errors.Is(err, ErrReconciliation) {
						t.Fatal("busy status did not preserve recovery", err)
					}
				} else if !errors.Is(err, context.Canceled) {
					t.Fatal("worker cancellation lost", err)
				}
			case <-time.After(8 * time.Second):
				t.Fatal("worker did not stop")
			}
			if mode == "identity-changed" {
				if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("replacement hardware triggered native acquisition")
				}
			}
			if mode == "identity-bound" && identityCalls.Load() < 5 {
				t.Fatal("missing admission/resolve/capture/idle identity checks", identityCalls.Load())
			}
			var state, lease string
			var reservations, pages int
			if err := docs.QueryRow("SELECT state FROM jobs WHERE id=?", a.JobID).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if err := docs.QueryRow("SELECT state FROM device_leases WHERE device_id=?", c.Scanners[0].ID).Scan(&lease); err != nil {
				t.Fatal(err)
			}
			if err := docs.QueryRow("SELECT count(*) FROM reservations WHERE job_id=?", a.JobID).Scan(&reservations); err != nil {
				t.Fatal(err)
			}
			if err := docs.QueryRow("SELECT count(*) FROM pages WHERE document_id=?", a.DocumentID).Scan(&pages); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "success", "small-budget", "identity-bound":
				if state != "succeeded" || lease != "idle" || reservations != 0 || pages != 1 {
					t.Fatal("publication incomplete", state, lease, reservations, pages)
				}
				var capturedPath string
				var width, height int
				if err := docs.QueryRow(`SELECT a.relative_path,p.pixel_width,p.pixel_height FROM pages p JOIN artifacts a ON a.id=p.original_artifact_id WHERE p.document_id=?`, a.DocumentID).Scan(&capturedPath, &width, &height); err != nil {
					t.Fatal(err)
				}
				captured, err := os.ReadFile(filepath.Join(root, capturedPath))
				if err != nil {
					t.Fatal(err)
				}
				original, err := os.ReadFile(filepath.Join(root, "original.png"))
				if err != nil || !bytes.Equal(captured, original) || width != 80 || height != 100 {
					t.Fatal("published original differs from native fixture", err, width, height)
				}
				var pageID string
				if err := docs.QueryRow("SELECT id FROM pages WHERE document_id=?", a.DocumentID).Scan(&pageID); err != nil {
					t.Fatal(err)
				}
				reference, err := store.ReadPageThumbnail(context.Background(), owner, a.DocumentID, pageID)
				if mode == "small-budget" {
					if !errors.Is(err, documents.ErrUnavailable) {
						t.Fatal("full reservation manufactured a derivative", reference, err)
					}
				} else {
					if err != nil {
						t.Fatal("pre-export thumbnail missing", err)
					}
					raw, err := os.ReadFile(filepath.Join(root, reference.Artifact.RelativePath))
					if err != nil || sha256.Sum256(raw) != reference.Artifact.SHA256 || int64(len(raw)) != reference.Artifact.Size {
						t.Fatal("preview content not bound", err)
					}
					bitmap, err := jpeg.Decode(bytes.NewReader(raw))
					if err != nil || bitmap.Bounds().Dx() != 80 || bitmap.Bounds().Dy() != 100 {
						t.Fatal("preview dimensions/orientation", err)
					}
					var exports, links int
					if err = docs.QueryRow("SELECT (SELECT count(*) FROM jobs WHERE document_id=? AND kind='render'),(SELECT count(*) FROM job_artifacts WHERE job_id=?)", a.DocumentID, a.JobID).Scan(&exports, &links); err != nil || exports != 0 || links != 2 {
						t.Fatal("thumbnail required a user export", exports, links, err)
					}
					admitted, err := store.AdmitRender(context.Background(), owner, documents.RenderAdmission{DocumentID: a.DocumentID, Revision: a.Revision, IdempotencyKey: "request_capture_export_01", Settings: documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless"}, ReserveBytes: 2 << 20}, documents.AdmissionLimits{UserMaxBytes: 8 << 20, GlobalMaxBytes: 16 << 20, JobMaxBytes: 2 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour})
					if err != nil {
						t.Fatal(err)
					}
					if err = Run(context.Background(), c, true); err != nil {
						t.Fatal("capture-to-PDF fresh boot", err)
					}
					output, err := store.GetExport(context.Background(), owner, a.DocumentID)
					if err != nil || output.MediaType != "application/pdf" {
						t.Fatal(output, err)
					}
					same, err := store.ReadPageThumbnail(context.Background(), owner, a.DocumentID, pageID)
					if err != nil || same.Artifact != reference.Artifact {
						t.Fatal("export regenerated capture preview", same, err)
					}
					var terminal string
					if err = docs.QueryRow("SELECT state FROM jobs WHERE id=?", admitted.JobID).Scan(&terminal); err != nil || terminal != "succeeded" {
						t.Fatal(terminal, err)
					}

				}
			case "cancel":
				if state != "interrupted" || lease != "idle" || reservations != 0 || pages != 0 {
					t.Fatal("cancel cleanup incomplete", state, lease, reservations, pages)
				}
			case "busy", "identity-changed", "identity-postcapture":
				if state != "running" || lease != "held" || reservations != 1 || pages != 0 {
					t.Fatal("busy status lost resources", state, lease, reservations, pages)
				}
			}
			data, err := os.ReadFile(marker)
			if mode != "identity-changed" && (err != nil || string(data) != "x") {
				t.Fatal("native acquisition repeated", err)
			}
		})
	}
}

type uncertainScanDispatch struct{ scanDispatchRecords }

func (uncertainScanDispatch) ClaimNextScan(context.Context, string) (documents.ClaimedScan, error) {
	return documents.ClaimedScan{}, documents.ErrCommitUncertain
}

type forbiddenScanDriver struct{ t *testing.T }

func (d forbiddenScanDriver) Resolve(context.Context, documents.ScanWork) (scanner.Capturer, scanner.Plan, error) {
	d.t.Error("uncertain claim invoked native resolution")
	return nil, scanner.Plan{}, errors.New("forbidden")
}
func (d forbiddenScanDriver) ObserveIdle(context.Context, string, int64) (int64, error) {
	d.t.Error("uncertain claim observed device")
	return 0, errors.New("forbidden")
}
func TestRuntimeScanDispatchRejectsUncertainClaimBeforeDriver(t *testing.T) {
	worked, err := dispatchScan(context.Background(), uncertainScanDispatch{}, forbiddenScanDriver{t}, "worker_runtime_0001")
	if worked || !errors.Is(err, documents.ErrCommitUncertain) {
		t.Fatal("uncertain claim dispatched", worked, err)
	}
}
