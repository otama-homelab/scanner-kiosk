//go:build linux

package workerruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanner/escl"
	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/workeridentity"
)

func TestRuntimeScansTwoDevicesConcurrentlyAndDrains(t *testing.T) {
	for _, mode := range []string{"success", "cancel", "authority-loss"} {
		t.Run(mode, func(t *testing.T) {
			c, store, db, root := fixture(t)
			tools := t.TempDir()
			release := filepath.Join(tools, "release")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("unexpected method")
					w.WriteHeader(500)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/ScannerCapabilities") {
					uuid := "12abcdef-3456-789a-bcde-1234567890ab"
					if strings.HasPrefix(r.URL.Path, "/second/") {
						uuid = "abcdef12-3456-789a-bcde-1234567890ab"
					}
					fmt.Fprintf(w, `<s:ScannerCapabilities xmlns:s="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:p="http://www.pwg.org/schemas/2010/12/sm"><p:Version>2.0</p:Version><s:UUID>%s</s:UUID></s:ScannerCapabilities>`, uuid)
					return
				}
				fmt.Fprint(w, `<scan:ScannerStatus xmlns:scan="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:pwg="http://www.pwg.org/schemas/2010/12/sm"><pwg:Version>2.0</pwg:Version><pwg:State>Idle</pwg:State></scan:ScannerStatus>`)
			}))
			defer server.Close()
			c.Scanners = []config.Scanner{{ID: "scanner_parallel_001", Name: "First", Driver: "sane-airscan", Endpoint: server.URL + "/first/eSCL"}, {ID: "scanner_parallel_002", Name: "Second", Driver: "sane-airscan", Endpoint: server.URL + "/second/eSCL"}}
			c.ScannerTools.AcquisitionEnabled = true
			c.ScannerTools.Concurrency = 2
			c.Durations["SCANNER_PROCESS_STOP_GRACE_SECONDS"] = 100 * time.Millisecond
			c.Durations["SCANNER_OCR_CATALOG_LEASE_SECONDS"] = 3 * time.Second
			c.ScannerTools.ScanimagePath = filepath.Join(tools, "scanimage")
			c.ScannerTools.ParametersPath = filepath.Join(tools, "parameters")
			script := `#!/usr/bin/python3
import sys,pathlib,time
args=sys.argv[1:]
device=next(a.split("airscan:e0:",1)[1] for a in args if a.startswith("--device-name="))
if "--all-options" in args:
 print("All options specific to device "+chr(96)+"airscan:e0:"+device+"':\n    --source Flatbed [Flatbed]\n    --mode Gray [Gray]\n    --resolution 300dpi [300]\n    -l 0..216mm [0]\n    -t 0..300mm [0]\n    -x 0..216mm [210]\n    -y 0..300mm [297]\n")
 sys.exit(0)
with open(` + strconv.Quote(tools) + `+"/"+device,"ab") as marker: marker.write(b"x")
while not pathlib.Path(` + strconv.Quote(release) + `).exists(): time.sleep(0.02)
sys.stdout.buffer.write(pathlib.Path(` + strconv.Quote(filepath.Join(root, "original.png")) + `).read_bytes())
`

			params := `{"protocol_version":1,"size":"a4","color":"gray","dpi":300,"width":80,"height":100,"bytes_per_line":80,"depth":8,"channels":1,"last_frame":true,"sane_version":16908289}`
			for path, data := range map[string]string{c.ScannerTools.ScanimagePath: script, c.ScannerTools.ParametersPath: "#!/bin/sh\nprintf '%s' " + shellLiteral(params) + "\n"} {
				if err := os.WriteFile(path, []byte(data), 0700); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- Run(ctx, c, false) }()
			joined := false
			defer func() {
				cancel()
				if !joined {
					select {
					case <-done:
					case <-time.After(10 * time.Second):
						t.Error("parallel worker failed to drain")
					}
				}
			}()
			wait := func(check func() bool) {
				t.Helper()
				deadline := time.Now().Add(12 * time.Second)
				for !check() {
					select {
					case err := <-done:
						joined = true
						t.Fatalf("worker ended: %v", err)
					default:
					}
					if time.Now().After(deadline) {
						t.Fatal("parallel scan condition timed out")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			var boot string
			wait(func() bool {
				return db.QueryRow("SELECT boot_id FROM processing_coordinator WHERE state='active'").Scan(&boot) == nil
			})
			wait(func() bool {
				var payload string
				if db.QueryRow("SELECT payload FROM scanner_metadata_snapshot WHERE boot_id=?", boot).Scan(&payload) != nil {
					return false
				}
				var snapshot scannermetadata.Snapshot
				if json.Unmarshal([]byte(payload), &snapshot) != nil || len(snapshot.Devices) != 2 {
					return false
				}
				return len(snapshot.Devices[0].Profiles) > 0 && len(snapshot.Devices[1].Profiles) > 0
			})
			var jobs []string
			for i, device := range c.Scanners {
				execute(t, db, "INSERT INTO device_leases(device_id,epoch,state) VALUES(?,1,'idle')", device.ID)
				observer, err := escl.New([]escl.Device{{ID: device.ID, Endpoint: device.Endpoint}}, escl.Options{Timeout: 5 * time.Second, MaxResponseBytes: 4096})
				if err != nil {
					t.Fatal(err)
				}
				identity, err := observer.ObserveIdentity(ctx, device.ID)
				if err != nil {
					t.Fatal(err)
				}
				// Ownership/stop/idle timestamps below are explicit fixtures,
				// while metadata, advertised identity and native IO are real.
				now := time.Now().UnixMilli()
				execute(t, db, `INSERT INTO scanner_readiness(device_id,boot_id,generation,lease_epoch,handoff_id,physical_id,scan_concurrency,ownership_confirmed_at_ms,local_stopped_at_ms,idle_observed_at_ms,published_at_ms,expires_at_ms) SELECT ?,boot_id,generation,1,'handoff_parallel_fixture',?,2,?,?,?,?,min(valid_until_ms,?) FROM processing_coordinator WHERE state='active'`, device.ID, identity.PhysicalID, now, now, now, now, now+30000)
				a, err := store.AdmitReadyScan(ctx, owner, documents.ScanRequest{Purpose: "document", DeviceID: device.ID, Capture: documents.Capture{Size: "a4", Color: "gray", DPI: 300}, IdempotencyKey: fmt.Sprintf("request_parallel_%04d", i), ReserveBytes: 2 << 20}, documents.ScanLimits{AdmissionLimits: documents.AdmissionLimits{UserMaxBytes: 8 << 20, GlobalMaxBytes: 16 << 20, JobMaxBytes: 2 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour}, MaxDocuments: 20, MaxConcurrentScans: 2, MaxPagePixels: c.Limits["SCANNER_MAX_PAGE_PIXELS"], RegisteredDevices: []string{device.ID}, DocumentLifetime: 24 * time.Hour})
				if err != nil {
					t.Fatal(err)
				}
				jobs = append(jobs, a.JobID)
			}
			// Both native processes must enter before either can finish. Serial execution
			// cannot satisfy this barrier, even with a successful final PDF.
			wait(func() bool {
				for _, device := range c.Scanners {
					info, err := os.Stat(filepath.Join(tools, device.ID))
					if err != nil || info.Size() != 1 {
						return false
					}
				}
				return true
			})
			if guard, err := workeridentity.Acquire(root); err == nil {
				guard.Close()
				t.Fatal("worker released guard during capture")
			}
			switch mode {
			case "success":
				if err := os.WriteFile(release, nil, 0600); err != nil {
					t.Fatal(err)
				}
				wait(func() bool {
					var n int
					return db.QueryRow("SELECT count(*) FROM jobs WHERE kind='scan' AND state='succeeded'").Scan(&n) == nil && n == 2
				})
				cancel()
			case "cancel":
				cancel()
			case "authority-loss":
				execute(t, db, "UPDATE processing_coordinator SET state='stopped'")
			}
			select {
			case err := <-done:
				joined = true
				if mode == "authority-loss" {
					if !errors.Is(err, ErrCatalog) {
						t.Fatal("lost authority failure", err)
					}
				} else if !errors.Is(err, context.Canceled) || errors.Is(err, ErrReconciliation) {
					t.Fatal("unclean cancellation", err)
				}
			case <-time.After(12 * time.Second):
				t.Fatal("worker did not join captures")
			}
			for i, device := range c.Scanners {
				data, err := os.ReadFile(filepath.Join(tools, device.ID))
				if err != nil || string(data) != "x" {
					t.Fatal("native capture replayed", err)
				}
				var state, lease string
				var reservations, pages int
				if err := db.QueryRow("SELECT state FROM jobs WHERE id=?", jobs[i]).Scan(&state); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRow("SELECT state FROM device_leases WHERE device_id=?", device.ID).Scan(&lease); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRow("SELECT count(*) FROM reservations WHERE job_id=?", jobs[i]).Scan(&reservations); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRow("SELECT count(*) FROM pages WHERE document_id=(SELECT document_id FROM jobs WHERE id=?)", jobs[i]).Scan(&pages); err != nil {
					t.Fatal(err)
				}
				if lease != "idle" || reservations != 0 {
					t.Fatal("joined scan failed to release proven resources", lease, reservations)
				}
				if mode == "success" {
					var previews int
					if err := db.QueryRow("SELECT count(*) FROM page_thumbnails WHERE document_id=(SELECT document_id FROM jobs WHERE id=?)", jobs[i]).Scan(&previews); err != nil || previews != 1 {
						t.Fatal("independent capture preview missing", previews, err)
					}
					if state != "succeeded" || pages != 1 {
						t.Fatal(state, pages)
					}
				} else if state != "interrupted" || pages != 0 {
					t.Fatal("cancel published or replayed scan", state, pages)
				}
			}
			guard, err := workeridentity.Acquire(root)
			if err != nil {
				t.Fatal("joined worker retained guard", err)
			}
			guard.Close()
		})
	}
}
