package apiruntime

import (
	"bytes"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Transport fixture only: PDF parsing/rendering has separate assembler coverage.
func downloadFixture(t *testing.T, ttl time.Duration) (*App, *http.Cookie, []byte, string) {
	t.Helper()
	app, guest, _, _, original := previewFixture(t, ttl)
	data := []byte("%PDF-1.4\nSynthetic binary transport fixture\n")
	digest := sha256.Sum256(data)
	if err := os.WriteFile(filepath.Join(filepath.Dir(original), "output.pdf"), data, 0600); err != nil {
		t.Fatal(err)
	}
	var owner string
	if err := app.state.QueryRow("SELECT id FROM principals WHERE kind='guest'").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if _, err := app.docs.Exec(`INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('output_download_01','document_preview_01',?,'output','ready','output.pdf','application/pdf',?,?,1,?)`, owner, len(data), digest[:], time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := app.docs.Exec(`INSERT INTO published_outputs(document_id,owner_id,revision,artifact_id) VALUES('document_preview_01',?,1,'output_download_01')`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := app.docs.Exec("UPDATE documents SET state='ready' WHERE id='document_preview_01'"); err != nil {
		t.Fatal(err)
	}
	return app, guest, data, "/api/documents/document_preview_01/download"
}
func TestDownloadRealHTTPFullRangeHeadAndRevokedOutput(t *testing.T) {
	app, guest, data, path := downloadFixture(t, time.Hour)
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	call := func(method, raw, ifRange string, cookie *http.Cookie) (*http.Response, []byte) {
		t.Helper()
		r, err := http.NewRequest(method, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Host = "scanner.example.test"
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if raw != "" {
			r.Header.Set("Range", raw)
		}
		if ifRange != "" {
			r.Header.Set("If-Range", ifRange)
		}
		response, err := (&http.Client{Timeout: 5 * time.Second}).Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response, body
	}
	for _, test := range []struct {
		name, method, raw, ifRange string
		status                     int
		expected                   []byte
	}{
		{"full", "GET", "", "", 200, data}, {"closed", "GET", "bytes=0-3", "", 206, data[:4]},
		{"open", "GET", "bytes=5-", "", 206, data[5:]}, {"suffix", "GET", "bytes=-5", "", 206, data[len(data)-5:]},
		{"clamp", "GET", "bytes=0-9999", "", 206, data}, {"if-range", "GET", "bytes=0-3", `"unknown"`, 200, data},
		{"head", "HEAD", "bytes=0-3", "", 200, nil}, {"unsatisfiable", "GET", "bytes=9999-", "", 416, nil},
		{"multiple", "GET", "bytes=0-1,3-4", "", 416, nil}, {"unit", "GET", "items=0-3", "", 416, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, body := call(test.method, test.raw, test.ifRange, guest)
			if response.StatusCode != test.status || !bytes.Equal(body, test.expected) {
				t.Fatal(response.StatusCode, string(body))
			}
			if response.Header.Get("Cache-Control") != "no-store" || len(response.Header.Values("Cache-Control")) != 1 {
				t.Fatal("cache policy")
			}
			if test.status == 206 && response.Header.Get("Content-Range") == "" {
				t.Fatal("missing range projection")
			}
			if test.status == 416 && !strings.HasPrefix(response.Header.Get("Content-Range"), "bytes */") {
				t.Fatal("missing unsatisfied size")
			}
			if test.status != 416 && (response.Header.Get("Content-Type") != "application/pdf" || response.Header.Get("Content-Disposition") != `attachment; filename="scan.pdf"`) {
				t.Fatal("download media/disposition", response.Header)
			}
		})
	}
	if response, _ := call("GET", "bytes=0-3", "", nil); response.StatusCode != 401 {
		t.Fatal("anonymous range", response.StatusCode)
	}
	anonymous := previewRequest(app, "GET", "/api/session", nil).Result().Cookies()[0]
	other := previewRequest(app, "POST", "/auth/guest", anonymous).Result().Cookies()[0]
	if response, _ := call("GET", "bytes=0-3", "", other); response.StatusCode != 404 {
		t.Fatal("cross-owner range", response.StatusCode)
	}
	// A real page edit invalidates the output, including subsequent range requests.
	r := httptest.NewRequest("PATCH", "https://scanner.example.test/api/documents/document_preview_01/pages/page_preview_0001", strings.NewReader(`{"rotation":180}`))
	r.AddCookie(guest)
	r.Header.Set("Origin", "https://scanner.example.test")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "request_rotate_001")
	r.Header.Set("If-Match", `"1"`)
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if response, _ := call("GET", "bytes=0-3", "", guest); response.StatusCode != 404 {
		t.Fatal("invalidated current output served", response.StatusCode)
	}
}
func TestDownloadAbortsBlockedTransferOnOutputInvalidation(t *testing.T) {
	app, guest, _, path := downloadFixture(t, time.Hour)
	writer := &blockingPreviewWriter{header: make(http.Header), started: make(chan struct{}), interrupted: make(chan struct{})}
	defer writer.stop()
	r := httptest.NewRequest("GET", "https://scanner.example.test"+path, nil)
	r.AddCookie(guest)
	r.Header.Set("Range", "bytes=0-3")
	done := make(chan any, 1)
	go func() { defer func() { done <- recover() }(); app.Handler().ServeHTTP(writer, r) }()
	select {
	case <-writer.started:
	case result := <-done:
		t.Fatal("download did not start", result)
	case <-time.After(5 * time.Second):
		t.Fatal("start timeout")
	}
	if _, err := app.docs.Exec("DELETE FROM published_outputs WHERE document_id='document_preview_01'"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result != http.ErrAbortHandler {
			t.Fatal("invalidated download did not abort", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("invalidated transfer remained blocked")
	}
}
