//go:build linux && saneparams

package scanjob

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scanner/escl"
	"scanner-kiosk/internal/scanner/sane"
	"scanner-kiosk/internal/scannerdriver"
)

func TestInstalledRegisteredDriverScanPublication(t *testing.T) {
	if os.Getenv("SCANNER_TEST_SANE_PARAMETERS") == "" {
		t.Skip("requires official SANE and parameter helper")
	}
	for _, state := range []string{"Idle", "Processing"} {
		t.Run(state, func(t *testing.T) {
			records, db, root, job, _, started := fixture(t, false)
			config := filepath.Dir(started)
			if err := os.WriteFile(filepath.Join(config, "test.conf"), []byte("number_of_devices 1\ndepth 8\ngeometry_max 300\n"), 0600); err != nil {
				t.Fatal(err)
			}
			native, err := sane.New(sane.Options{Binary: os.Getenv("SCANNER_TEST_SANE_BINARY"), ParametersBinary: os.Getenv("SCANNER_TEST_SANE_PARAMETERS"), ConfigDir: config, WorkRoot: root, DeviceID: device, DeviceName: "test:0", AllowTestBackend: true, LibraryDirs: strings.Split(os.Getenv("SCANNER_TEST_SANE_LIBRARY_DIRS"), ":"), MaxBytes: 1 << 20, MaxPixels: 40000000, Timeout: 10 * time.Second, StopGrace: 100 * time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			var status atomic.Value
			status.Store(state)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/eSCL/ScannerStatus" {
					t.Error("unexpected status operation", r.Method, r.URL.Path)
					http.Error(w, "unexpected", 500)
					return
				}
				w.Header().Set("Content-Type", "text/xml")
				_, _ = w.Write([]byte(`<scan:ScannerStatus xmlns:scan="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:pwg="http://www.pwg.org/schemas/2010/12/sm"><pwg:Version>2.0</pwg:Version><pwg:State>` + status.Load().(string) + `</pwg:State><scan:Jobs/></scan:ScannerStatus>`))
			}))
			defer server.Close()
			observer, err := escl.New([]escl.Device{{ID: device, Endpoint: server.URL + "/eSCL"}}, escl.Options{Timeout: time.Second, MaxResponseBytes: 4096})
			if err != nil {
				t.Fatal(err)
			}
			backend, err := scannerdriver.Native(native)
			if err != nil {
				t.Fatal(err)
			}
			d, err := scannerdriver.New(scannerdriver.Options{BootID: boot, Devices: []scannerdriver.Device{{ID: device, Backend: backend}}, Observer: observer, MaxBytes: 1 << 20, MaxPixels: 40000000})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := d.Close(context.Background()); err != nil {
					t.Error("driver close", err)
				}
			}()
			profile, err := d.InspectCapture(context.Background(), device, scanner.Capture{Size: "a4", Color: "gray", DPI: 300})
			if err != nil || profile.Width != 2480 || profile.Height != 3507 || profile.CheckedAtMS <= 0 {
				t.Fatal("native metadata inspection", profile, err)
			}
			prepared, err := PrepareClaimed(context.Background(), records, d, owner, job, boot, 10*time.Millisecond)
			if err != nil || prepared == nil {
				t.Fatal("combined resolution/acquisition failed", err)
			}
			if count(t, db, "SELECT count(*) FROM device_leases WHERE job_id=? AND state='held'", job) != 1 || count(t, db, "SELECT count(*) FROM reservations WHERE job_id=?", job) != 1 {
				t.Fatal("lease released before idle/publication")
			}
			artifact, err := prepared.Publish(context.Background())
			if state == "Processing" {
				if !errors.Is(err, ErrPending) {
					t.Fatal("busy scanner output published", err)
				}
				if count(t, db, "SELECT count(*) FROM pages") != 0 || count(t, db, "SELECT count(*) FROM reservations WHERE job_id=?", job) != 1 {
					t.Fatal("busy status discarded capture authority")
				}
				// A second fresh observation reports Idle; this is fixture state, not hardware.
				status.Store("Idle")
				if err = prepared.Finish(context.Background(), "failed"); err != nil {
					t.Fatal("stopped failed capture cleanup", err)
				}
			} else {
				if err != nil || artifact.PixelWidth != 2480 || artifact.PixelHeight != 3507 {
					t.Fatal("native parameter/capture/publication mismatch", err)
				}
				if count(t, db, "SELECT count(*) FROM pages WHERE pixel_width=2480 AND pixel_height=3507") != 1 {
					t.Fatal("actual dimensions not stored")
				}
			}
			if count(t, db, "SELECT count(*) FROM device_leases WHERE job_id=?", job) != 0 || count(t, db, "SELECT count(*) FROM reservations WHERE job_id=?", job) != 0 {
				t.Fatal("confirmed terminal work retained capacity")
			}
		})
	}
}
