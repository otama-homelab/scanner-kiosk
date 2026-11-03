//go:build linux

package apiruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanthumbnail"
)

// Captured bytes/physical stop are explicit fixtures. The Publisher, private DB
// bundle, session owner checks and streamed JPEG route are product code.
func TestCapturedThumbnailHTTPBeforeAnyExport(t *testing.T) {
	app, guest, raw, _, source := previewFixture(t, time.Hour)
	ctx := context.Background()
	root := filepath.Dir(source)
	var owner string
	if err := app.docs.QueryRow("SELECT owner_id FROM documents WHERE id='document_preview_01'").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	const job = "job_capture_http_01"
	const boot = "worker_capture_http_01"
	const device = "scanner_capture_http_01"
	snapshot, _ := json.Marshal(documents.ScanSnapshot{Version: 1, DeviceID: device, WorkerBootID: boot, LeaseEpoch: 2, Capture: documents.Capture{Size: "a4", Color: "color", DPI: 300}})
	now := time.Now().UnixMilli()
	for _, q := range []struct {
		query string
		args  []any
	}{
		{"UPDATE documents SET state='scanning' WHERE id='document_preview_01'", nil},
		{`INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id,started_at_ms,heartbeat_at_ms) VALUES(?,'document_preview_01',?,'scan','running',1,?,?,?,?,?,?)`, []any{job, owner, string(snapshot), now, now + 60000, boot, now, now}},
		{`INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,?)`, []any{job, owner, 2 << 20}},
		{`INSERT INTO device_leases(device_id,epoch,state,job_id) VALUES(?,2,'held',?)`, []any{device, job}},
	} {
		if _, err := app.docs.Exec(q.query, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	original := documents.ArtifactRecord{ID: "original_capture_http_01", RelativePath: "captured-for-thumbnail.png", MediaType: "image/png", Size: int64(len(raw)), SHA256: sha256.Sum256(raw), PixelWidth: 32, PixelHeight: 16}
	if err := os.WriteFile(filepath.Join(root, original.RelativePath), raw, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := documents.New(app.state, app.docs, time.Now, func(context.Context) (int64, error) { return 1 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
	publisher := scanthumbnail.New(store, root, 2<<20, 10000)
	stop := documents.ScanStop{DeviceID: device, WorkerBootID: boot, LeaseEpoch: 2, ProcessStoppedAtMS: now, IdleObservedAtMS: time.Now().UnixMilli()}
	page, err := publisher.CommitScanPage(ctx, owner, job, 1, original, stop)
	if err != nil {
		t.Fatal(err)
	}
	if err = publisher.ReleaseScanArtifacts(); err != nil {
		t.Fatal(err)
	}
	path := "/api/documents/document_preview_01/pages/" + page.ID + "/thumbnail"
	response := thumbnailNetworkRequest(t, app, "GET", path, guest)
	if response.Code != 200 || response.Header().Get("Content-Type") != "image/jpeg" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(response.Code, response.Body.String())
	}
	bitmap, err := jpeg.Decode(bytes.NewReader(response.Body.Bytes()))
	if err != nil || bitmap.Bounds().Dx() != 32 || bitmap.Bounds().Dy() != 16 {
		t.Fatal("pre-export preview geometry", err)
	}
	if thumbnailNetworkRequest(t, app, "HEAD", path, guest).Code != 200 {
		t.Fatal("pre-export HEAD failed")
	}
	if thumbnailNetworkRequest(t, app, "GET", "/api/documents/document_preview_01/download", guest).Code != 404 {
		t.Fatal("capture implicitly created an export")
	}
	var renders int
	if err = app.docs.QueryRow("SELECT count(*) FROM jobs WHERE kind='render'").Scan(&renders); err != nil || renders != 0 {
		t.Fatal(renders, err)
	}
	if _, err = app.docs.Exec("DELETE FROM pages WHERE id=?", page.ID); err != nil {
		t.Fatal(err)
	}
	if thumbnailNetworkRequest(t, app, "GET", path, guest).Code != 404 {
		t.Fatal("deleted page preview still served")
	}
}
