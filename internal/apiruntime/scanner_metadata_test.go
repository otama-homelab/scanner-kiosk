package apiruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/workercatalog"
	"scanner-kiosk/internal/workeridentity"
	"scanner-kiosk/pkg/ocrplugin"
)

func TestScannerMetadataProjectsCoupledSettingsWithoutReadiness(t *testing.T) {
	c := fixture(t)
	id := "scanner_metadata_001"
	c.Scanners = []config.Scanner{{ID: id, Name: "Registered scanner", Driver: "sane-airscan", Endpoint: "http://192.0.2.10/private-scanner"}, {ID: "scanner_not_probed_01", Name: "Unknown scanner", Driver: "sane-airscan", Endpoint: "http://192.0.2.20/eSCL"}}
	app, err := New(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	anonymous := previewRequest(app, "GET", "/api/session", nil).Result().Cookies()[0]
	guest := previewRequest(app, "POST", "/auth/guest", anonymous).Result().Cookies()[0]
	guard, err := workeridentity.Acquire(c.DocumentDir)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	store, err := workercatalog.New(app.docs, time.Now, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Begin(context.Background(), guard)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(context.Background(), lease, renderadmission.CatalogSnapshot{Providers: []ocrplugin.Capabilities{}}); err != nil {
		t.Fatal(err)
	}
	value := scannermetadata.Snapshot{Devices: []scannermetadata.Device{{ID: id, Profiles: []scanner.Profile{
		{Capture: scanner.Capture{Size: "a4", Color: "color", DPI: 300}, Width: 2480, Height: 3508, CheckedAtMS: time.Now().UnixMilli()},
		{Capture: scanner.Capture{Size: "a5", Color: "gray", DPI: 600}, Width: 3496, Height: 4960, CheckedAtMS: time.Now().UnixMilli()},
		{Capture: scanner.Capture{Size: "a6", Color: "color", DPI: 600}, Width: 1000000, Height: 1000000, CheckedAtMS: time.Now().UnixMilli()},
	}}}}
	if err := store.PublishScanners(context.Background(), lease, value); err != nil {
		t.Fatal(err)
	}
	read := func() string {
		w := previewRequest(app, "GET", "/api/capabilities", guest)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	body := read()
	for _, private := range []string{"private-scanner", "width", "height", "checked_at_ms", guard.ID(), "192.0.2."} {
		if strings.Contains(body, private) {
			t.Fatal("private scanner metadata leaked", private)
		}
	}
	var projected struct {
		Revision string `json:"revision"`
		Scanners []struct {
			State   string            `json:"state"`
			Options []scanner.Capture `json:"capture_options"`
			Default *scanner.Capture  `json:"default_capture"`
		} `json:"scanners"`
	}
	if err := json.Unmarshal([]byte(body), &projected); err != nil {
		t.Fatal(err)
	}
	if len(projected.Scanners) != 2 || projected.Scanners[0].State != "unknown" || len(projected.Scanners[0].Options) != 2 || projected.Scanners[0].Default == nil || projected.Scanners[1].Default != nil {
		t.Fatal("invalid coupled settings", body)
	}
	for _, capture := range projected.Scanners[0].Options {
		if capture.Size == "a4" && (capture.Color != "color" || capture.DPI != 300) {
			t.Fatal("fabricated Cartesian-product choice")
		}
	}
	before := projected.Revision
	value.Devices[0].Profiles[0].CheckedAtMS = time.Now().UnixMilli()
	if err := store.PublishScanners(context.Background(), lease, value); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(read()), &projected); err != nil || projected.Revision != before {
		t.Fatal("native refresh changed content revision", err)
	}
	if _, err := app.docs.Exec("UPDATE scanner_metadata_snapshot SET published_at_ms=?,expires_at_ms=?", time.Now().UnixMilli()-1000, time.Now().UnixMilli()-1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(read()), &projected); err != nil || len(projected.Scanners[0].Options) != 0 || projected.Scanners[0].Default != nil {
		t.Fatal("expired metadata remained selectable", err)
	}
	var leases int
	if err := app.docs.QueryRow("SELECT count(*) FROM device_leases").Scan(&leases); err != nil || leases != 0 {
		t.Fatal("capability publication initialized readiness", err)
	}
}

func TestDiscoveredScannerAppearsAndAdmitsOnlyWithFreshReadiness(t *testing.T) {
	f := scanHTTPFixture(t)
	c := f.c
	c.Scanners = nil
	c.ScannerDiscovery = true
	app, err := New(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	metadata := scannermetadata.Snapshot{Devices: []scannermetadata.Device{{ID: f.c.Scanners[0].ID, Name: "Discovered Canon", Profiles: []scanner.Profile{{Capture: scanner.Capture{Size: "a4", Color: "color", DPI: 300}, Width: 80, Height: 100, CheckedAtMS: time.Now().UnixMilli()}}}}}
	if err := f.catalog.PublishScanners(context.Background(), f.lease, metadata); err != nil {
		t.Fatal(err)
	}
	body := previewRequest(app, "GET", "/api/scanners", f.cookie)
	if body.Code != 200 || !strings.Contains(body.Body.String(), "Discovered Canon") || strings.Contains(body.Body.String(), "192.0.2.") {
		t.Fatal(body.Code, body.Body.String())
	}
	if w := scanHTTPCall(app, f.cookie, "/api/documents", scanBody, "request_discovered_001", "", nil); w.Code != 503 {
		t.Fatal("discovery alone admitted", w.Code)
	}
	f.ready(t, 0)
	scanHTTPJob(t, scanHTTPCall(app, f.cookie, "/api/documents", scanBody, "request_discovered_001", "", nil))
	c.ScannerDiscovery = false
	disabled, err := New(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer disabled.Close()
	body = previewRequest(disabled, "GET", "/api/scanners", f.cookie)
	if body.Code != 200 || strings.Contains(body.Body.String(), "Discovered Canon") {
		t.Fatal("disabled discovery displayed", body.Body.String())
	}
	if w := scanHTTPCall(disabled, f.cookie, "/api/documents", scanBody, "request_discovered_002", "", nil); w.Code != 422 {
		t.Fatal("disabled discovery admitted", w.Code)
	}
}
