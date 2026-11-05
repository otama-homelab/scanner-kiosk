package apiruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scanner-kiosk/internal/pdfassemble"
)

func TestImportedPDFPreviewStreamsOnlyOwnedPreparedPage(t *testing.T) {
	app, guest, png, _, original := previewFixture(t, time.Hour)
	data, err := pdfassemble.Build(context.Background(), []pdfassemble.Page{{ID: "preview", Size: "a4", Color: "color", Width: 32, Height: 16, Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(png)), nil }}}, "lossless", pdfassemble.Options{MaxInputBytes: 1 << 20, MaxOutputBytes: 1 << 20, MaxPagePixels: 10000, MaxPages: 1, JPEGQuality: 92})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(original), "prepared.pdf"), data, 0600); err != nil {
		t.Fatal(err)
	}
	var owner string
	if err = app.docs.QueryRow("SELECT owner_id FROM documents WHERE id='document_preview_01'").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if _, err = app.docs.Exec(`INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_pdf_preview1','document_preview_01',?,'original','ready','prepared.pdf','application/pdf',?,?,1,?)`, owner, len(data), digest[:], time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err = app.docs.Exec(`INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pdf_width_points,pdf_height_points) VALUES('page_pdf_preview001','document_preview_01',?,1,'original_pdf_preview1','pdf',595.28,841.89)`, owner); err != nil {
		t.Fatal(err)
	}
	path := "/api/documents/document_preview_01/pages/page_pdf_preview001/preview"
	response := thumbnailNetworkRequest(t, app, "GET", path, guest)
	if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), data) || response.Header().Get("Content-Type") != "application/pdf" || response.Header().Get("Content-Disposition") != "inline" {
		t.Fatal("PDF not previewed", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Security-Policy") == "" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("PDF browser policy absent")
	}
	if response = thumbnailNetworkRequest(t, app, "HEAD", path, guest); response.Code != 200 || response.Body.Len() != 0 {
		t.Fatal("HEAD", response.Code)
	}
	if response = thumbnailNetworkRequest(t, app, "GET", path, nil); response.Code != 401 {
		t.Fatal("unauthenticated preview", response.Code)
	}
	anonymous := previewRequest(app, "GET", "/api/session", nil).Result().Cookies()[0]
	other := previewRequest(app, "POST", "/auth/guest", anonymous).Result().Cookies()[0]
	if response = thumbnailNetworkRequest(t, app, "GET", path, other); response.Code != 404 {
		t.Fatal("other owner preview", response.Code)
	}
	if _, err = app.docs.Exec("DELETE FROM pages WHERE id='page_pdf_preview001'"); err != nil {
		t.Fatal(err)
	}
	if response = thumbnailNetworkRequest(t, app, "GET", path, guest); response.Code != 404 {
		t.Fatal("deleted page preview", response.Code)
	}
}
