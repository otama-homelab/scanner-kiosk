//go:build linux

package workerruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanner/escl"
	"scanner-kiosk/internal/workeridentity"
)

func TestOfflineRecoveryPreservesPagesAndAllowsNewPDF(t *testing.T) {
	c, store, db, root := fixture(t)
	job := admit(t, store)
	if _, err := store.ClaimExport(context.Background(), owner, job, "worker_previous_001"); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(root, "original.png"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.ReadDocument(context.Background(), owner, document)
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(root, "work_"+strings.Repeat("a", 48))
	if err = os.Mkdir(scratch, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(scratch, "unfinished-output"), []byte("not published"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = Recover(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	status, err := store.ReadJob(context.Background(), owner, job)
	if err != nil || status.State != "interrupted" {
		t.Fatal(status, err)
	}
	after, err := store.ReadDocument(context.Background(), owner, document)
	if err != nil || after.PageCount != 1 || after.Pages[0].ID != before.Pages[0].ID || after.Revision != before.Revision || !after.ExpiresAt.Equal(before.ExpiresAt) || after.State != "review" {
		t.Fatal("document changed", after, err)
	}
	actual, err := os.ReadFile(filepath.Join(root, "original.png"))
	if err != nil || sha256.Sum256(actual) != sha256.Sum256(original) {
		t.Fatal("original changed", err)
	}
	if _, err = os.Stat(scratch); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unpublished output retained", err)
	}
	if err = Recover(context.Background(), c, true); err != nil {
		t.Fatal("recovery retry", err)
	}
	admitted, err := store.AdmitRender(context.Background(), owner, documents.RenderAdmission{DocumentID: document, Revision: after.Revision, IdempotencyKey: "request_after_recovery_001", Settings: documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless"}, ReserveBytes: 1 << 20}, documents.AdmissionLimits{UserMaxBytes: 2 << 20, GlobalMaxBytes: 4 << 20, JobMaxBytes: 1 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour})
	if err != nil {
		t.Fatal("new work refused", err)
	}
	if err = Run(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	status, err = store.ReadJob(context.Background(), owner, admitted.JobID)
	if err != nil || status.State != "succeeded" {
		t.Fatal(status, err)
	}
	var reservations int
	if err = db.QueryRow("SELECT count(*) FROM reservations").Scan(&reservations); err != nil || reservations != 0 {
		t.Fatal("reservation retained", reservations, err)
	}
}
func TestOfflineRecoveryRequiresOperatorStopAndExclusivePrivateRoot(t *testing.T) {
	for _, mode := range []string{"unconfirmed", "active-worker", "scratch-symlink", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			c, store, _, root := fixture(t)
			job := admit(t, store)
			if _, err := store.ClaimExport(context.Background(), owner, job, "worker_previous_001"); err != nil {
				t.Fatal(err)
			}
			confirmed := mode != "unconfirmed"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			if mode == "active-worker" {
				guard, err := workeridentity.Acquire(root)
				if err != nil {
					t.Fatal(err)
				}
				defer guard.Close()
			}
			if mode == "scratch-symlink" {
				dir := filepath.Join(root, "capture_"+strings.Repeat("b", 48))
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, "original.png"), filepath.Join(dir, "link")); err != nil {
					t.Fatal(err)
				}
			}
			if err := Recover(ctx, c, confirmed); err == nil {
				t.Fatal("unsafe recovery accepted")
			}
			status, err := store.ReadJob(context.Background(), owner, job)
			if err != nil || status.State != "running" {
				t.Fatal("job changed without proof", status, err)
			}
			if _, err = os.Stat(filepath.Join(root, "original.png")); err != nil {
				t.Fatal("published input removed", err)
			}
		})
	}
}

func TestOfflineScanRecoveryBindsIdentityAndIdleBeforeReleasingOldLease(t *testing.T) {
	for _, mode := range []string{"idle", "busy", "identity-changed", "identity-after-idle"} {
		t.Run(mode, func(t *testing.T) {
			var capabilityCalls atomic.Int32
			const uuid = "12abcdef-3456-789a-bcde-1234567890ab"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("recovery attempted device mutation")
				}
				if r.URL.Path == "/eSCL/ScannerCapabilities" {
					call := capabilityCalls.Add(1)
					current := uuid
					if mode == "identity-changed" && call > 1 || mode == "identity-after-idle" && call > 2 {
						current = "22abcdef-3456-789a-bcde-1234567890ab"
					}
					fmt.Fprintf(w, `<s:ScannerCapabilities xmlns:s="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:p="http://www.pwg.org/schemas/2010/12/sm"><p:Version>2.0</p:Version><s:UUID>%s</s:UUID></s:ScannerCapabilities>`, current)
				} else if r.URL.Path == "/eSCL/ScannerStatus" {
					state := "Idle"
					if mode == "busy" {
						state = "Processing"
					}
					fmt.Fprintf(w, `<s:ScannerStatus xmlns:s="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:p="http://www.pwg.org/schemas/2010/12/sm"><p:Version>2.0</p:Version><p:State>%s</p:State><s:Jobs/></s:ScannerStatus>`, state)
				} else {
					t.Error("unexpected device path", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			c, _, db, root := fixture(t)
			c.Scanners[0].Endpoint = server.URL + "/eSCL"
			device := c.Scanners[0].ID
			observer, err := escl.New([]escl.Device{{ID: device, Endpoint: c.Scanners[0].Endpoint}}, escl.Options{Timeout: time.Second, MaxResponseBytes: 4096})
			if err != nil {
				t.Fatal(err)
			}
			identity, err := observer.ObserveIdentity(context.Background(), device)
			if err != nil {
				t.Fatal(err)
			}
			const previous = "worker_previous_001"
			const job = "job_recovery_scan_001"
			now := time.Now().UnixMilli()
			snapshot, err := json.Marshal(documents.ScanSnapshot{Version: 1, DeviceID: device, WorkerBootID: previous, LeaseEpoch: 2, Capture: documents.Capture{Size: "a4", Color: "gray", DPI: 300}, PhysicalID: identity.PhysicalID, HandoffID: "handoff_previous_001"})
			if err != nil {
				t.Fatal(err)
			}
			execute(t, db, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id,started_at_ms) VALUES(?,?,?,'scan','running',1,?,?,?,?,?)`, job, document, owner, string(snapshot), now, now+60000, previous, now)
			execute(t, db, `INSERT INTO device_leases(device_id,epoch,state,job_id) VALUES(?,2,'held',?)`, device, job)
			execute(t, db, `INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,1024)`, job, owner)
			execute(t, db, `UPDATE documents SET state='scanning' WHERE id=?`, document)
			err = Recover(context.Background(), c, true)
			if mode == "idle" && err != nil || mode != "idle" && err == nil {
				t.Fatal("incorrect recovery result", mode, err)
			}
			var state, lease string
			var epoch, reservations, pages int
			if err = db.QueryRow("SELECT state FROM jobs WHERE id=?", job).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow("SELECT state,epoch FROM device_leases WHERE device_id=?", device).Scan(&lease, &epoch); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow("SELECT count(*) FROM reservations WHERE job_id=?", job).Scan(&reservations); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow("SELECT count(*) FROM pages WHERE document_id=?", document).Scan(&pages); err != nil {
				t.Fatal(err)
			}
			if pages != 1 || epoch != 2 {
				t.Fatal("original pages or epoch changed")
			}
			if mode == "idle" {
				if state != "interrupted" || lease != "idle" || reservations != 0 {
					t.Fatal(state, lease, reservations)
				}
			} else if state != "running" || lease != "held" || reservations != 1 {
				t.Fatal("unsafe work released", state, lease, reservations)
			}
			if _, err = os.Stat(filepath.Join(root, "original.png")); err != nil {
				t.Fatal("original removed", err)
			}
		})
	}
}
