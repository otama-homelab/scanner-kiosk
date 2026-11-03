package apiruntime

import (
	"encoding/json"
	"net/http/httptest"
	"scanner-kiosk/internal/documents"
	"strings"
	"testing"
	"time"
)

func TestJobHTTPStatusCancellationAndNoProviderMaterial(t *testing.T) {
	app, guest, _, _, _ := previewFixture(t, time.Hour)
	var owner string
	if err := app.state.QueryRow("SELECT id FROM principals WHERE kind='guest'").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if _, err := app.docs.Exec(`INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,error_code) VALUES('job_http_00000001','document_preview_01',?,'render','queued',1,'{"secret":"do-not-return"}',?,?,'provider-private-material')`, owner, time.Now().UnixMilli(), time.Now().Add(time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	w := previewRequest(app, "GET", "/api/jobs/job_http_00000001", guest)
	var job documents.JobProjection
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil || w.Code != 200 || job.State != "queued" || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "do-not-return") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = previewRequest(app, "POST", "/api/jobs/job_http_00000001/cancel", guest)
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil || w.Code != 202 || job.State != "cancelling" || w.Header().Get("Location") != "/api/jobs/job_http_00000001" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = previewRequest(app, "POST", "/api/jobs/job_http_00000001/cancel", guest)
	if w.Code != 202 {
		t.Fatal("replay", w.Code, w.Body.String())
	}
	if w = previewRequest(app, "GET", "/api/jobs/job_http_00000001", nil); w.Code != 401 {
		t.Fatal("anonymous job", w.Code)
	}
	anonymous := previewRequest(app, "GET", "/api/session", nil).Result().Cookies()[0]
	other := previewRequest(app, "POST", "/auth/guest", anonymous).Result().Cookies()[0]
	if w = previewRequest(app, "GET", "/api/jobs/job_http_00000001", other); w.Code != 404 {
		t.Fatal("foreign job", w.Code)
	}
	r := httptest.NewRequest("POST", "https://scanner.example.test/api/jobs/job_http_00000001/cancel", nil)
	r.AddCookie(guest)
	r.Header.Set("Idempotency-Key", "request_cancel_02")
	w = httptest.NewRecorder()
	app.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("missing Origin", w.Code)
	}
}
