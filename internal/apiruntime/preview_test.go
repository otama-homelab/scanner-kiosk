package apiruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func previewRequest(app *App, method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://scanner.example.test"+path, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if method != "GET" {
		r.Header.Set("Origin", "https://scanner.example.test")
		r.Header.Set("Idempotency-Key", "request_preview_01")
		r.Header.Set("If-Match", `"1"`)
	}
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, r)
	return w
}
func previewFixture(t *testing.T, ttl time.Duration) (*App, *http.Cookie, []byte, string, string) {
	t.Helper()
	c := fixture(t)
	app, err := New(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Close() })
	anonymous := previewRequest(app, "GET", "/api/session", nil).Result().Cookies()[0]
	w := previewRequest(app, "POST", "/auth/guest", anonymous)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	guest := w.Result().Cookies()[0]
	var owner string
	if err := app.state.QueryRow("SELECT id FROM principals WHERE kind='guest'").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, 32, 16))
	for alpha := 3; alpha < len(canvas.Pix); alpha += 4 {
		canvas.Pix[alpha] = 255
	}
	canvas.SetNRGBA(1, 2, color.NRGBA{R: 100, G: 20, B: 70, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, canvas); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	sum := sha256.Sum256(data)
	if err := os.WriteFile(filepath.Join(c.DocumentDir, "private-original.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	queries := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO owners(id) VALUES(?)", []any{owner}},
		{`INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('document_preview_01',?,'document','review',?,?)`, []any{owner, now, now + ttl.Milliseconds()}},
		{`INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_preview_01','document_preview_01',?,'original','ready','private-original.png','image/png',?,?,1,?)`, []any{owner, len(data), sum[:], now}},
		{`INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,rotation,capture_size,capture_color,capture_dpi,pixel_width,pixel_height) VALUES('page_preview_0001','document_preview_01',?,0,'original_preview_01',90,'a4','color',300,32,16)`, []any{owner}},
	}
	for _, q := range queries {
		if _, err := app.docs.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	return app, guest, data, "/api/documents/document_preview_01/pages/page_preview_0001/preview", filepath.Join(c.DocumentDir, "private-original.png")
}
func TestPreviewRealHTTPExactBytesAndOwnerChecks(t *testing.T) {
	app, guest, data, path, sourcePath := previewFixture(t, time.Hour)
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	call := func(method, path string, cookie *http.Cookie) (*http.Response, []byte) {
		t.Helper()
		r, err := http.NewRequest(method, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Host = "scanner.example.test"
		if cookie != nil {
			r.AddCookie(cookie)
		}
		client := &http.Client{Timeout: 5 * time.Second}
		response, err := client.Do(r)
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
	response, body := call("GET", path, guest)
	if response.StatusCode != 200 || !bytes.Equal(body, data) || response.Header.Get("Content-Type") != "image/png" || response.Header.Get("Cache-Control") != "no-store" || len(response.Header.Values("Cache-Control")) != 1 || response.Header.Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Fatal("preview bytes/headers", response.StatusCode, hex.EncodeToString(body))
	}
	// The source is returned unchanged; the existing page rotation is applied by UI.
	response, body = call("HEAD", path, guest)
	if response.StatusCode != 200 || len(body) != 0 || response.ContentLength != int64(len(data)) {
		t.Fatal("HEAD preview", response.StatusCode, len(body))
	}
	if response, _ = call("GET", path, nil); response.StatusCode != 401 {
		t.Fatal("anonymous preview", response.StatusCode)
	}
	anonymous := previewRequest(app, "GET", "/api/session", nil).Result().Cookies()[0]
	other := previewRequest(app, "POST", "/auth/guest", anonymous).Result().Cookies()[0]
	if response, _ = call("GET", path, other); response.StatusCode != 404 {
		t.Fatal("cross-owner preview", response.StatusCode)
	}
	if response, _ = call("GET", "/api/documents/document_missing_01/pages/page_preview_0001/preview", guest); response.StatusCode != 404 {
		t.Fatal("cross-document preview", response.StatusCode)
	}
	if response, _ = call("GET", path+"?path=/etc/passwd", guest); response.StatusCode != 400 {
		t.Fatal("caller path accepted", response.StatusCode)
	}
	// An adapter cannot serve a file whose bytes no longer match the sealed record.
	if _, err := app.docs.Exec("UPDATE artifacts SET sha256=zeroblob(32) WHERE id='original_preview_01'"); err == nil {
		t.Fatal("ready artifact unexpectedly mutable")
	}
	// Replace the file via the root's pinned directory path, leaving metadata intact.
	altered := append([]byte{}, data...)
	altered[0] ^= 1
	if err := os.WriteFile(sourcePath, altered, 0600); err != nil {
		t.Fatal(err)
	}
	response, body = call("GET", path, guest)
	if response.StatusCode != 503 || response.Header.Get("Content-Type") != "application/json" || bytes.Equal(body, altered) {
		t.Fatal("corrupt original served", response.StatusCode)
	}
}

// blockingPreviewWriter models a saturated client socket. The production server
// is tested above; this fixture lets the test observe cancellation deterministically.
type blockingPreviewWriter struct {
	header               http.Header
	started, interrupted chan struct{}
	once                 sync.Once
	mu                   sync.Mutex
	timer                *time.Timer
}

func (w *blockingPreviewWriter) Header() http.Header { return w.header }
func (w *blockingPreviewWriter) WriteHeader(int)     {}
func (w *blockingPreviewWriter) Write([]byte) (int, error) {
	close(w.started)
	<-w.interrupted
	return 0, errors.New("write deadline reached")
}
func (w *blockingPreviewWriter) SetWriteDeadline(deadline time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(max(0, time.Until(deadline)), func() { w.once.Do(func() { close(w.interrupted) }) })
	return nil
}
func (w *blockingPreviewWriter) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.once.Do(func() { close(w.interrupted) })
}
func TestPreviewAbortsBlockedTransferOnRevocationOrExpiry(t *testing.T) {
	for _, name := range []string{"page-delete", "guest-end", "session-revoked", "expiry"} {
		t.Run(name, func(t *testing.T) {
			ttl := time.Hour
			if name == "expiry" {
				ttl = 1500 * time.Millisecond
			}
			app, guest, _, path, _ := previewFixture(t, ttl)
			writer := &blockingPreviewWriter{header: make(http.Header), started: make(chan struct{}), interrupted: make(chan struct{})}
			defer writer.stop()
			r := httptest.NewRequest("GET", "https://scanner.example.test"+path, nil)
			r.AddCookie(guest)
			done := make(chan any, 1)
			go func() { defer func() { done <- recover() }(); app.Handler().ServeHTTP(writer, r) }()
			select {
			case <-writer.started:
			case result := <-done:
				t.Fatal("transfer did not start", result)
			case <-time.After(5 * time.Second):
				t.Fatal("transfer start timed out")
			}
			switch name {
			case "page-delete":
				w := previewRequest(app, "DELETE", "/api/documents/document_preview_01/pages/page_preview_0001", guest)
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			case "session-revoked":
				if _, err := app.state.Exec("DELETE FROM sessions"); err != nil {
					t.Fatal(err)
				}
			case "guest-end":
				w := previewRequest(app, "POST", "/auth/guest/end", guest)
				if w.Code != 202 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			select {
			case result := <-done:
				if result != http.ErrAbortHandler {
					t.Fatal("stream did not abort", result)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("revoked transfer remained blocked")
			}
		})
	}
}
