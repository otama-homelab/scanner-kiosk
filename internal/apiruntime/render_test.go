package apiruntime

import (
	"encoding/json"
	"net/http/httptest"
	"scanner-kiosk/internal/documents"
	"strings"
	"testing"
	"time"
)

func TestRenderHTTPAdmissionReplayAndStrictCommands(t *testing.T) {
	app, guest, _, _, _ := previewFixture(t, time.Hour)
	call := func(body, key, revision string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://scanner.example.test/api/documents/document_preview_01/render", strings.NewReader(body))
		r.AddCookie(guest)
		r.Header.Set("Origin", "https://scanner.example.test")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("If-Match", revision)
		w := httptest.NewRecorder()
		app.Handler().ServeHTTP(w, r)
		return w
	}
	for _, body := range []string{`{"FORMAT":"pdf","ocr":{"enabled":false}}`, `{"format":"pdf","ocr":{"Enabled":false}}`, `{"format":"pdf","ocr":{"enabled":false,"enabled":true}}`, `{"format":"pdf","ocr":{"enabled":null}}`, `{"format":"pdf","ocr":{"enabled":false,"model_version":"client"}}`, `{"format":"pdf","ocr":{"enabled":false},"reserve_bytes":1}`, `{"format":"pdf","ocr":{"enabled":false},"pdf_encoding":null}`} {
		if w := call(body, "request_render_01", `"1"`); w.Code != 400 {
			t.Fatal("invalid command accepted", body, w.Code, w.Body.String())
		}
	}
	w := call(`{"format":"pdf","ocr":{"enabled":true,"provider_id":"tesseract","model_id":"model","languages":["jpn","eng"]}}`, "request_ocr_00001", `"1"`)
	if w.Code != 503 {
		t.Fatal("OCR silently bypassed", w.Code, w.Body.String())
	}
	w = call(`{"format":"pdf","pdf_encoding":"lossless","ocr":{"enabled":false}}`, "request_render_01", `"1"`)
	var first documents.JobProjection
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || w.Code != 202 || first.State != "queued" || first.InputRevision != 2 || w.Header().Get("Location") != "/api/jobs/"+first.ID {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call(`{"format":"pdf","pdf_encoding":"lossless","ocr":{"enabled":false}}`, "request_render_01", `"1"`)
	var replay documents.JobProjection
	if err := json.Unmarshal(w.Body.Bytes(), &replay); err != nil || w.Code != 202 || replay.ID != first.ID {
		t.Fatal("render replay", w.Code, w.Body.String())
	}
	w = call(`{"format":"pdf","pdf_encoding":"jpeg","ocr":{"enabled":false}}`, "request_render_01", `"1"`)
	if w.Code != 409 {
		t.Fatal("changed intent", w.Code)
	}
	var jobs, reservations int
	if err := app.docs.QueryRow("SELECT count(*) FROM jobs").Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err := app.docs.QueryRow("SELECT count(*) FROM reservations").Scan(&reservations); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || reservations != 1 {
		t.Fatal("duplicate admission", jobs, reservations)
	}
}
