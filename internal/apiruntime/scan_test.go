package apiruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/scannerreadiness"
	"scanner-kiosk/internal/workercatalog"
	"scanner-kiosk/internal/workeridentity"
)

const scanBody = `{"scanner_id":"scanner_http_00001","purpose":"document","capture":{"size":"a4","color":"color","dpi":300}}`
const nextBody = `{"scanner_id":"scanner_http_00001","capture":{"size":"a4","color":"color","dpi":300}}`

type scanHTTPSetup struct {
	app               *App
	c                 config.Config
	cookie, anonymous *http.Cookie
	owner             string
	catalog           *workercatalog.Store
	lease             workercatalog.Lease
	guard             *workeridentity.Guard
}

func scanHTTPFixture(t *testing.T) *scanHTTPSetup {
	t.Helper()
	f := &scanHTTPSetup{c: fixture(t)}
	f.c.Scanners = []config.Scanner{{ID: "scanner_http_00001", Name: "First", Driver: "sane-airscan", Endpoint: "http://192.0.2.10/eSCL"}, {ID: "scanner_http_00002", Name: "Second", Driver: "sane-airscan", Endpoint: "http://192.0.2.11/eSCL"}}
	var err error
	f.app, err = New(context.Background(), f.c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.app.Close() })
	f.anonymous = previewRequest(f.app, "GET", "/api/session", nil).Result().Cookies()[0]
	f.cookie = previewRequest(f.app, "POST", "/auth/guest", f.anonymous).Result().Cookies()[0]
	if err = f.app.state.QueryRow("SELECT id FROM principals WHERE kind='guest'").Scan(&f.owner); err != nil {
		t.Fatal(err)
	}
	f.guard, err = workeridentity.Acquire(f.c.DocumentDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.guard.Close() })
	f.catalog, err = workercatalog.New(f.app.docs, time.Now, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	f.lease, err = f.catalog.Begin(context.Background(), f.guard)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.catalog.PublishScanners(context.Background(), f.lease, scannermetadata.Snapshot{Devices: []scannermetadata.Device{{ID: f.c.Scanners[0].ID, Profiles: []scanner.Profile{{Capture: scanner.Capture{Size: "a4", Color: "color", DPI: 300}, Width: 80, Height: 100, CheckedAtMS: time.Now().UnixMilli()}}}}}); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *scanHTTPSetup) ready(t *testing.T, epoch int64) {
	t.Helper()
	now := time.Now().UnixMilli()
	_, err := f.catalog.PublishScannerReadiness(context.Background(), f.lease, scannerreadiness.Evidence{DeviceID: f.c.Scanners[0].ID, HandoffID: "handoff_http_fixture", PhysicalID: "physical_http_fixture", LeaseEpoch: epoch, ScanConcurrency: 2, OwnershipConfirmedAtMS: now, LocalStoppedAtMS: now, IdleObservedAtMS: now})
	if err != nil {
		t.Fatal(err)
	}
}
func scanHTTPCall(app *App, cookie *http.Cookie, path, body, key, match string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "https://scanner.example.test"+path, strings.NewReader(body))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	r.Header.Set("Origin", "https://scanner.example.test")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	if match != "" {
		r.Header.Set("If-Match", match)
	}
	if mutate != nil {
		mutate(r)
	}
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, r)
	return w
}
func scanHTTPJob(t *testing.T, w *httptest.ResponseRecorder) documents.JobProjection {
	t.Helper()
	var job documents.JobProjection
	if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &job) != nil || job.ID == "" || job.Kind != "scan" || w.Header().Get("Location") != "/api/jobs/"+job.ID || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String(), w.Header())
	}
	return job
}
func TestScanHTTPCreateReadinessReplayAndRegistryChange(t *testing.T) {
	f := scanHTTPFixture(t)
	w := scanHTTPCall(f.app, f.cookie, "/api/documents", scanBody, "request_scan_http_001", "", nil)
	if w.Code != 503 || !strings.Contains(w.Body.String(), "scanner_unavailable") {
		t.Fatal("metadata alone admitted", w.Code, w.Body.String())
	}
	f.ready(t, 0)
	job := scanHTTPJob(t, scanHTTPCall(f.app, f.cookie, "/api/documents", scanBody, "request_scan_http_001", "", nil))
	var reserve int64
	var snapshot documents.ScanSnapshot
	var raw string
	if err := f.app.docs.QueryRow("SELECT j.configuration_json,r.byte_size FROM jobs j JOIN reservations r ON r.job_id=j.id WHERE j.id=?", job.ID).Scan(&raw, &reserve); err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal([]byte(raw), &snapshot) != nil || snapshot.WorkerBootID != f.guard.ID() || snapshot.LeaseEpoch != 2 || reserve != f.c.Limits["SCANNER_JOB_MAX_BYTES"] {
		t.Fatal("untrusted execution policy", reserve, snapshot)
	}
	if err := f.catalog.Stop(context.Background(), f.lease); err != nil {
		t.Fatal(err)
	}
	replay := scanHTTPJob(t, scanHTTPCall(f.app, f.cookie, "/api/documents", scanBody, "request_scan_http_001", "", nil))
	if replay.ID != job.ID {
		t.Fatal("replay created another scan")
	}
	c := f.c
	c.Scanners = nil
	other, err := New(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	replay = scanHTTPJob(t, scanHTTPCall(other, f.cookie, "/api/documents", scanBody, "request_scan_http_001", "", nil))
	if replay.ID != job.ID {
		t.Fatal("registry removal blocked replay")
	}
	if w := scanHTTPCall(other, f.cookie, "/api/documents", scanBody, "request_scan_http_new", "", nil); w.Code != 422 {
		t.Fatal("removed scanner accepted fresh intent", w.Code)
	}
	if w := scanHTTPCall(f.app, f.cookie, "/api/documents", strings.Replace(scanBody, `"document"`, `"photo"`, 1), "request_scan_http_001", "", nil); w.Code != 409 {
		t.Fatal("changed intent replayed", w.Code)
	}
	var jobs int
	if err = f.app.docs.QueryRow("SELECT count(*) FROM jobs").Scan(&jobs); err != nil || jobs != 1 {
		t.Fatal(jobs, err)
	}
}
func TestScanHTTPStrictCommandsAndAuthorization(t *testing.T) {
	f := scanHTTPFixture(t)
	f.ready(t, 0)
	bodies := []string{`null`, `{}`, `[]`, scanBody + ` {}`, strings.Replace(scanBody, `"scanner_id"`, `"SCANNER_ID"`, 1), strings.Replace(scanBody, `"dpi":300`, `"DPI":300`, 1), strings.Replace(scanBody, `"dpi":300`, `"dpi":300,"dpi":600`, 1), strings.Replace(scanBody, `"dpi":300`, `"dpi":null`, 1), strings.Replace(scanBody, `"dpi":300`, `"dpi":300.0`, 1), strings.Replace(scanBody, `"dpi":300`, `"dpi":1200`, 1), strings.Replace(scanBody, `"color":"color",`, ``, 1), strings.Replace(scanBody, `"capture":{`, `"capture":null,"ignored":{`, 1), strings.Replace(scanBody, `"purpose":"document"`, `"purpose":null`, 1), strings.Replace(scanBody, `"purpose":"document"`, `"purpose":"document","photo_format":"jpeg"`, 1), strings.TrimSuffix(scanBody, "}") + `,"worker_boot_id":"client_worker_0001"}`, strings.TrimSuffix(scanBody, "}") + `,"reserve_bytes":1}`, strings.TrimSuffix(scanBody, "}") + `,"owner_id":"client_owner_0001"}`, strings.TrimSuffix(scanBody, "}") + `,"photo_format":null}`}
	bodies = append(bodies, strings.TrimSuffix(scanBody, "}")+`,"physical_id":"client_physical_0001"}`, strings.TrimSuffix(scanBody, "}")+`,"handoff_id":"client_handoff_00001"}`)
	for _, body := range bodies {
		if w := scanHTTPCall(f.app, f.cookie, "/api/documents", body, "request_strict_scan_01", "", nil); w.Code != 400 {
			t.Fatal("invalid command accepted", body, w.Code, w.Body.String())
		}
	}
	for _, test := range []struct {
		name, path, match string
		cookie            *http.Cookie
		mutate            func(*http.Request)
		status            int
	}{
		{name: "anonymous", path: "/api/documents", cookie: f.anonymous, status: 401},
		{name: "no-cookie", path: "/api/documents", status: 401},
		{name: "wrong-origin", path: "/api/documents", cookie: f.cookie, mutate: func(r *http.Request) { r.Header.Set("Origin", "https://foreign.test") }, status: 403},
		{name: "query", path: "/api/documents?a=1", cookie: f.cookie, status: 400},
		{name: "empty-query", path: "/api/documents?", cookie: f.cookie, status: 400},
		{name: "create-revision", path: "/api/documents", cookie: f.cookie, match: `"1"`, status: 400},
		{name: "duplicate-key", path: "/api/documents", cookie: f.cookie, mutate: func(r *http.Request) { r.Header.Add("Idempotency-Key", "request_duplicate_01") }, status: 400},
		{name: "wrong-media", path: "/api/documents", cookie: f.cookie, mutate: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, status: 415},
		{name: "next-no-revision", path: "/api/documents/document_other_001/scans", cookie: f.cookie, status: 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := scanHTTPCall(f.app, test.cookie, test.path, scanBody, "request_strict_scan_01", test.match, test.mutate)
			if w.Code != test.status {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	if w := scanHTTPCall(f.app, f.cookie, "/api/documents", strings.Repeat(" ", 64<<10)+scanBody, "request_too_large_01", "", nil); w.Code != 413 {
		t.Fatal(w.Code)
	}
	var jobs int
	if err := f.app.docs.QueryRow("SELECT count(*) FROM jobs").Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal("rejected commands wrote work", jobs, err)
	}
}
func TestScanHTTPNextPagePreservesPagesTTLAndOwnership(t *testing.T) {
	f := scanHTTPFixture(t)
	f.ready(t, 0)
	first := scanHTTPJob(t, scanHTTPCall(f.app, f.cookie, "/api/documents", scanBody, "request_scan_http_001", "", nil))
	f.completePage(t, first)
	f.ready(t, 2)
	var expiry int64
	var err error
	if err = f.app.docs.QueryRow("SELECT expires_at_ms FROM documents WHERE id=?", first.DocumentID).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	path := "/api/documents/" + first.DocumentID + "/scans"
	anon := previewRequest(f.app, "GET", "/api/session", nil).Result().Cookies()[0]
	other := previewRequest(f.app, "POST", "/auth/guest", anon).Result().Cookies()[0]
	if w := scanHTTPCall(f.app, other, path, nextBody, "request_next_other_001", `"1"`, nil); w.Code != 404 {
		t.Fatal("foreign append", w.Code, w.Body.String())
	}
	if w := scanHTTPCall(f.app, f.cookie, path, nextBody, "request_next_stale_01", `"2"`, nil); w.Code != 412 {
		t.Fatal("stale revision", w.Code, w.Body.String())
	}
	next := scanHTTPJob(t, scanHTTPCall(f.app, f.cookie, path, nextBody, "request_next_valid_01", `"1"`, nil))
	if next.InputRevision != 2 || next.DocumentID != first.DocumentID {
		t.Fatal(next)
	}
	replay := scanHTTPJob(t, scanHTTPCall(f.app, f.cookie, path, nextBody, "request_next_valid_01", `"1"`, nil))
	if replay.ID != next.ID {
		t.Fatal("next-page replay")
	}
	var pages int
	var remaining int64
	if err = f.app.docs.QueryRow("SELECT (SELECT count(*) FROM pages WHERE document_id=d.id),expires_at_ms FROM documents d WHERE id=?", first.DocumentID).Scan(&pages, &remaining); err != nil || pages != 1 || remaining != expiry {
		t.Fatal("append discarded original or extended TTL", pages, remaining, err)
	}
}

func (f *scanHTTPSetup) completePage(t *testing.T, first documents.JobProjection) {
	t.Helper()
	records, err := documents.New(f.app.state, f.app.docs, time.Now, func(context.Context) (int64, error) { return 1 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = records.ClaimScanWithLimit(context.Background(), f.owner, first.ID, f.guard.ID(), 2); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	png.Encode(&buffer, image.NewGray(image.Rect(0, 0, 80, 100)))
	data := buffer.Bytes()
	if err = os.WriteFile(filepath.Join(f.c.DocumentDir, "scan-http-page.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if _, err = records.CommitScanPage(context.Background(), f.owner, first.ID, 1, documents.ArtifactRecord{ID: "artifact_http_original", RelativePath: "scan-http-page.png", MediaType: "image/png", Size: int64(len(data)), SHA256: sha256.Sum256(data), PixelWidth: 80, PixelHeight: 100}, documents.ScanStop{DeviceID: f.c.Scanners[0].ID, WorkerBootID: f.guard.ID(), LeaseEpoch: 2, ProcessStoppedAtMS: now, IdleObservedAtMS: now}); err != nil {
		t.Fatal(err)
	}
}

func TestScanHTTPPhotoEncodingAndSingleImageRule(t *testing.T) {
	for _, format := range []string{"default", "jpeg", "png"} {
		t.Run(format, func(t *testing.T) {
			f := scanHTTPFixture(t)
			f.ready(t, 0)
			body := strings.Replace(scanBody, `"document"`, `"photo"`, 1)
			expected := format
			if format == "default" {
				expected = "jpeg"
			} else {
				body = strings.TrimSuffix(body, "}") + `,"photo_format":"` + format + `"}`
			}
			job := scanHTTPJob(t, scanHTTPCall(f.app, f.cookie, "/api/documents", body, "request_photo_http_001", "", nil))
			var raw string
			if err := f.app.docs.QueryRow("SELECT configuration_json FROM jobs WHERE id=?", job.ID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var snapshot documents.ScanSnapshot
			if json.Unmarshal([]byte(raw), &snapshot) != nil || snapshot.PhotoFormat != expected {
				t.Fatal("lost photo encoding intent", raw)
			}
			f.completePage(t, job)
			projected := previewRequest(f.app, "GET", "/api/documents/"+job.DocumentID, f.cookie)
			var doc documents.Projection
			if projected.Code != 200 || json.Unmarshal(projected.Body.Bytes(), &doc) != nil || doc.PhotoFormat == nil || *doc.PhotoFormat != expected {
				t.Fatal("photo preference not persisted in projection", projected.Body.String())
			}
			f.ready(t, 2)
			w := scanHTTPCall(f.app, f.cookie, "/api/documents/"+job.DocumentID+"/scans", nextBody, "request_photo_next_001", `"1"`, nil)
			if w.Code != 409 {
				t.Fatal("photo appended another page", w.Code, w.Body.String())
			}
		})
	}
}
