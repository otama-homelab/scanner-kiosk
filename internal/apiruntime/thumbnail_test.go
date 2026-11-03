package apiruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/captureimage"
	"scanner-kiosk/internal/thumbnail"
)

func thumbnailHTTPFixture(t *testing.T, ttl ...time.Duration) (*App, *http.Cookie, []byte, string, string) {
	t.Helper()
	retention := time.Hour
	if len(ttl) > 0 {
		retention = ttl[0]
	}
	app, guest, original, preview, source := previewFixture(t, retention)
	result, err := thumbnail.Build(context.Background(), bytes.NewReader(original), thumbnail.Options{Input: captureimage.Options{MaxBytes: 1 << 20, MaxPixels: 10000, Width: 32, Height: 16, Color: "color"}, MaxEdge: 16, MaxOutputBytes: 1 << 20, Quality: 85})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(source), "private-thumbnail.jpg")
	if err = os.WriteFile(path, result.Data, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(result.Data)
	var owner string
	if err = app.docs.QueryRow("SELECT owner_id FROM documents WHERE id='document_preview_01'").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if _, err = app.docs.Exec(`INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('thumbnail_preview_01','document_preview_01',?,'thumbnail','ready','private-thumbnail.jpg','image/jpeg',?,?,1,?)`, owner, len(result.Data), digest[:], time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err = app.docs.Exec(`INSERT INTO page_thumbnails VALUES('original_preview_01','document_preview_01',?,'thumbnail_preview_01',16,8)`, owner); err != nil {
		t.Fatal(err)
	}
	return app, guest, result.Data, strings.TrimSuffix(preview, "preview") + "thumbnail", path
}
func TestThumbnailDeliveryVerifiedJPEGAndOwner(t *testing.T) {
	app, guest, data, path, _ := thumbnailHTTPFixture(t)
	response := thumbnailNetworkRequest(t, app, "GET", path, guest)
	if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), data) || response.Header().Get("Content-Type") != "image/jpeg" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(response.Code, response.Header(), response.Body.String())
	}
	bitmap, err := jpeg.Decode(bytes.NewReader(response.Body.Bytes()))
	if err != nil || bitmap.Bounds().Dx() != 16 || bitmap.Bounds().Dy() != 8 {
		t.Fatal(err)
	}
	if response = thumbnailNetworkRequest(t, app, "HEAD", path, guest); response.Code != 200 || response.Body.Len() != 0 {
		t.Fatal("HEAD", response.Code, response.Body.Len())
	}
	for _, test := range []struct {
		path   string
		cookie *http.Cookie
		status int
	}{{path, nil, 401}, {path + "?size=9999", guest, 400}, {strings.Replace(path, "document_preview_01", "other_document_0001", 1), guest, 404}, {strings.Replace(path, "page_preview_0001", "other_page_000001", 1), guest, 404}} {
		response = thumbnailNetworkRequest(t, app, "GET", test.path, test.cookie)
		if response.Code != test.status {
			t.Fatal(test.path, response.Code)
		}
	}
	anonymous := previewRequest(app, "GET", "/api/session", nil).Result().Cookies()[0]
	other := previewRequest(app, "POST", "/auth/guest", anonymous).Result().Cookies()[0]
	if response = thumbnailNetworkRequest(t, app, "GET", path, other); response.Code != 404 {
		t.Fatal("other owner", response.Code)
	}
}
func TestThumbnailDeliveryRevocationAndCorruption(t *testing.T) {
	for _, mode := range []string{"page-removed", "expired", "disabled", "revoked", "deleting", "corrupted", "original-deleting"} {
		t.Run(mode, func(t *testing.T) {
			retention := time.Hour
			if mode == "expired" {
				retention = time.Millisecond
			}
			app, guest, _, path, file := thumbnailHTTPFixture(t, retention)
			status := 404
			switch mode {
			case "page-removed":
				_, _ = app.docs.Exec("DELETE FROM pages WHERE id='page_preview_0001'")
			case "expired":
				time.Sleep(5 * time.Millisecond)
			case "disabled":
				_, _ = app.state.Exec("UPDATE principals SET disabled_at_ms=?", time.Now().UnixMilli())
				status = 401
			case "revoked":
				_, _ = app.state.Exec("DELETE FROM sessions")
				status = 401
			case "deleting":
				_, _ = app.docs.Exec("UPDATE documents SET state='deleting' WHERE id='document_preview_01'")
			case "corrupted":
				if err := os.WriteFile(file, []byte("corrupted"), 0600); err != nil {
					t.Fatal(err)
				}
				status = 503
			case "original-deleting":
				_, _ = app.docs.Exec("UPDATE artifacts SET state='deleting' WHERE id='original_preview_01'")
			}
			response := thumbnailNetworkRequest(t, app, "GET", path, guest)
			if response.Code != status {
				t.Fatal(mode, response.Code, response.Body.String())
			}
		})
	}
}

// Real net/http supports the stream write-deadline contract; Recorder does not.
func thumbnailNetworkRequest(t *testing.T, app *App, method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	request, err := http.NewRequest(method, server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "scanner.example.test"
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	recorder := httptest.NewRecorder()
	for name, values := range response.Header {
		for _, value := range values {
			recorder.Header().Add(name, value)
		}
	}
	recorder.WriteHeader(response.StatusCode)
	if _, err = io.Copy(recorder, response.Body); err != nil {
		t.Fatal(err)
	}
	return recorder
}
