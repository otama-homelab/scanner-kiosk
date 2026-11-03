package apiruntime

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"scanner-kiosk/internal/documents"
)

func TestDeleteDocumentHTTPRevokesContentAndReplays(t *testing.T) {
	app, guest, _, thumbnail, file := thumbnailHTTPFixture(t)
	response := previewRequest(app, "DELETE", "/api/documents/document_preview_01", guest)
	if response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}
	var job documents.JobProjection
	if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil || job.Kind != "cleanup" || job.State != "queued" || response.Header().Get("Location") != "/api/jobs/"+job.ID {
		t.Fatal(job, err)
	}
	for _, path := range []string{"/api/documents/document_preview_01", thumbnail, strings.TrimSuffix(thumbnail, "thumbnail") + "preview", "/api/documents/document_preview_01/download"} {
		response = previewRequest(app, "GET", path, guest)
		if response.Code != 404 {
			t.Fatal(path, response.Code, response.Body.String())
		}
	}
	response = previewRequest(app, "GET", "/api/documents", guest)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatal("history exposed deleting document", response.Code, response.Body.String())
	}
	response = previewRequest(app, "GET", "/api/jobs/"+job.ID, guest)
	if response.Code != 200 {
		t.Fatal("cleanup status lost", response.Code)
	}
	response = previewRequest(app, "DELETE", "/api/documents/document_preview_01", guest)
	var replay documents.JobProjection
	if err := json.Unmarshal(response.Body.Bytes(), &replay); err != nil || response.Code != 202 || replay.ID != job.ID {
		t.Fatal("repeat created new job", response.Code, replay, err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("HTTP intent removed worker-owned file", err)
	}
}
func TestDeleteDocumentHTTPStrictInput(t *testing.T) {
	for _, mode := range []string{"anonymous", "origin", "revision", "weak-revision", "duplicate-revision", "missing-key", "duplicate-key", "body", "query"} {
		t.Run(mode, func(t *testing.T) {
			app, guest, _, _, _ := thumbnailHTTPFixture(t)
			path := "/api/documents/document_preview_01"
			body := ""
			if mode == "body" {
				body = "{}"
			}
			if mode == "query" {
				path += "?force=true"
			}
			request := httptest.NewRequest("DELETE", "https://scanner.example.test"+path, strings.NewReader(body))
			request.Header.Set("Origin", "https://scanner.example.test")
			request.Header.Set("If-Match", `"1"`)
			request.Header.Set("Idempotency-Key", "request_delete_http")
			if mode != "anonymous" {
				request.AddCookie(guest)
			}
			expected := 400
			switch mode {
			case "anonymous":
				expected = 401
			case "origin":
				request.Header.Set("Origin", "https://other.example.test")
				expected = 403
			case "revision":
				request.Header.Set("If-Match", `"2"`)
				expected = 412
			case "weak-revision":
				request.Header.Set("If-Match", `W/"1"`)
			case "duplicate-revision":
				request.Header.Add("If-Match", `"1"`)
			case "missing-key":
				request.Header.Del("Idempotency-Key")
			case "duplicate-key":
				request.Header.Add("Idempotency-Key", "request_delete_http")
			}
			response := httptest.NewRecorder()
			app.Handler().ServeHTTP(response, request)
			if response.Code != expected {
				t.Fatal(mode, response.Code, response.Body.String())
			}
			var count int
			if err := app.docs.QueryRow("SELECT count(*) FROM jobs WHERE kind='cleanup'").Scan(&count); err != nil || count != 0 {
				t.Fatal("rejected request queued work", count, err)
			}
		})
	}
}
