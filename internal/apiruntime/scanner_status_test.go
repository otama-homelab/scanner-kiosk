package apiruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/scannerreadiness"
	"scanner-kiosk/pkg/ocrplugin"
)

type publicScannerStates struct {
	Revision string `json:"revision"`
	Scanners []struct {
		ID    string `json:"id"`
		State string `json:"state"`
	} `json:"scanners"`
}

func scannerStates(t *testing.T, f *scanHTTPSetup) (publicScannerStates, string) {
	t.Helper()
	w := previewRequest(f.app, "GET", "/api/capabilities", f.cookie)
	var out publicScannerStates
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	return out, w.Body.String()
}
func scannerStatusCatalog(t *testing.T, f *scanHTTPSetup) {
	t.Helper()
	if err := f.catalog.Publish(context.Background(), f.lease, renderadmission.CatalogSnapshot{Providers: []ocrplugin.Capabilities{}}); err != nil {
		t.Fatal(err)
	}
}
func TestScannerStatesFollowEvidenceAndNeverExposeWorkerOrOwner(t *testing.T) {
	f := scanHTTPFixture(t)
	scannerStatusCatalog(t, f)
	states, _ := scannerStates(t, f)
	if states.Scanners[0].State != "unknown" {
		t.Fatal("metadata became readiness", states)
	}
	f.ready(t, 0)
	states, _ = scannerStates(t, f)
	if states.Scanners[0].State != "idle" || states.Scanners[1].State != "unknown" {
		t.Fatal(states)
	}
	revision := states.Revision
	f.ready(t, 1)
	states, _ = scannerStates(t, f)
	if states.Revision != revision {
		t.Fatal("proof timestamp changed content revision")
	}
	job := scanHTTPJob(t, scanHTTPCall(f.app, f.cookie, "/api/documents", scanBody, "request_status_scan_01", "", nil))
	states, body := scannerStates(t, f)
	if states.Scanners[0].State != "busy" || states.Revision == revision {
		t.Fatal("held lease still looked ready", states)
	}
	for _, private := range []string{f.guard.ID(), f.owner, job.ID, job.DocumentID, "handoff_http_fixture", "physical_http_fixture", "ownership_confirmed", "lease_epoch", "worker_boot", "192.0.2.", "checked_at_ms"} {
		if strings.Contains(body, private) {
			t.Fatal("private readiness/job field exposed", private)
		}
	}
	f.completePage(t, job)
	states, _ = scannerStates(t, f)
	if states.Scanners[0].State != "unknown" {
		t.Fatal("completion revived consumed readiness", states)
	}
	f.ready(t, 2)
	states, _ = scannerStates(t, f)
	if states.Scanners[0].State != "idle" {
		t.Fatal(states)
	}
	if _, err := f.app.docs.Exec("UPDATE scanner_readiness SET expires_at_ms=published_at_ms+1"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	states, _ = scannerStates(t, f)
	if states.Scanners[0].State != "unknown" {
		t.Fatal("expired proof still looked ready", states)
	}
}
func TestScannerStatesIncludeSharedCapacityAndRecoveryBarriers(t *testing.T) {
	f := scanHTTPFixture(t)
	scannerStatusCatalog(t, f)
	var payload string
	if err := f.app.docs.QueryRow("SELECT payload FROM scanner_metadata_snapshot").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	// Expand the local fixture's native metadata to its second registered device.
	if _, err := f.app.docs.Exec("UPDATE scanner_metadata_snapshot SET payload=json_insert(payload,'$.devices[1]',json_set(json_extract(payload,'$.devices[0]'),'$.id',?))", f.c.Scanners[1].ID); err != nil {
		t.Fatal(err)
	}
	for i, device := range f.c.Scanners {
		now := time.Now().UnixMilli()
		if _, err := f.catalog.PublishScannerReadiness(context.Background(), f.lease, scannerreadiness.Evidence{DeviceID: device.ID, PhysicalID: fmt.Sprintf("physical_status_%04d", i), HandoffID: "handoff_status_fixture", ScanConcurrency: 1, OwnershipConfirmedAtMS: now, LocalStoppedAtMS: now, IdleObservedAtMS: now}); err != nil {
			t.Fatal(err)
		}
	}
	states, _ := scannerStates(t, f)
	if states.Scanners[0].State != "idle" || states.Scanners[1].State != "idle" {
		t.Fatal(states)
	}
	job := scanHTTPJob(t, scanHTTPCall(f.app, f.cookie, "/api/documents", scanBody, "request_status_scan_01", "", nil))
	states, _ = scannerStates(t, f)
	if states.Scanners[0].State != "busy" || states.Scanners[1].State != "busy" {
		t.Fatal("shared full slot looked available", states)
	}
	if _, err := f.app.docs.Exec("UPDATE jobs SET state='failed' WHERE id=?", job.ID); err != nil {
		t.Fatal(err)
	}
	states, _ = scannerStates(t, f)
	if states.Scanners[0].State != "unavailable" || states.Scanners[1].State != "unavailable" {
		t.Fatal("terminal-held recovery barrier looked available", states)
	}
}

func TestScannerListDoesNotRequireOCRCatalogOrGrantAcquisition(t *testing.T) {
	f := scanHTTPFixture(t)
	if w := previewRequest(f.app, "GET", "/api/capabilities", f.cookie); w.Code != 503 {
		t.Fatal("fixture unexpectedly had OCR catalog")
	}
	w := previewRequest(f.app, "GET", "/api/scanners", f.cookie)
	var list publicScannerStates
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Scanners) != 2 || list.Scanners[0].State != "unknown" {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "ocr_plugins") {
		t.Fatal("scanner route coupled to OCR")
	}
	f.ready(t, 0)
	w = previewRequest(f.app, "GET", "/api/scanners", f.cookie)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || list.Scanners[0].State != "idle" {
		t.Fatal("valid readiness required OCR", w.Code, w.Body.String())
	}
	if w := previewRequest(f.app, "GET", "/api/scanners", nil); w.Code != 401 {
		t.Fatal("anonymous scanner state", w.Code)
	}
	if w := previewRequest(f.app, "GET", "/api/scanners?ignored=1", f.cookie); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if _, err := f.app.docs.Exec("UPDATE scanner_readiness SET expires_at_ms=published_at_ms+1"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	w = previewRequest(f.app, "GET", "/api/scanners", f.cookie)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || list.Scanners[0].State != "unknown" {
		t.Fatal("expired proof survived independent endpoint", w.Code, w.Body.String())
	}
}
