package documents

import (
	"context"
	"crypto/sha256"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"scanner-kiosk/internal/captureimage"
)

// This connects real PNG validation to the record service. Capture/device-stop
// reports are still simulated; it does not acquire or reconcile Canon hardware.
func TestRealBitmapValidationBeforeCaptureRecordPublication(t *testing.T) {
	f := scanFixture(t)
	request := scanRequest()
	request.Capture = Capture{Size: "a4", Color: "gray", DPI: 75}
	request.ReserveBytes = 100000
	limits := scanLimits()
	limits.JobMaxBytes = 200000
	limits.UserMaxBytes = 1000000
	limits.GlobalMaxBytes = 2000000
	admitted, err := f.store.AdmitScan(context.Background(), alice, request, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimScan(context.Background(), alice, admitted.JobID, runner); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "captured.png")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	bitmap := image.NewGray(image.Rect(0, 0, 620, 877))
	for i := range bitmap.Pix {
		bitmap.Pix[i] = byte(i % 251)
	}
	if err = png.Encode(file, bitmap); err != nil {
		t.Fatal(err)
	}
	info, err := captureimage.Validate(context.Background(), file, captureimage.Options{MaxBytes: request.ReserveBytes, MaxPixels: 1000000, Width: 620, Height: 877, Color: "gray"})
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Sync(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	artifact := ArtifactRecord{ID: "actual_png_capture_01", RelativePath: "captured.png", MediaType: info.MediaType, Size: info.Size, SHA256: info.SHA256, PixelWidth: info.Width, PixelHeight: info.Height}
	f.now = 2300
	report := ScanStop{DeviceID: scanner, WorkerBootID: runner, LeaseEpoch: admitted.LeaseEpoch, ProcessStoppedAtMS: 2100, IdleObservedAtMS: 2200}
	page, err := f.store.CommitScanPage(context.Background(), alice, admitted.JobID, admitted.Revision, artifact, report)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(bytes) != info.SHA256 || int64(len(bytes)) != info.Size || page.Capture.DPI != 75 || page.Capture.Color != "gray" {
		t.Fatal("validated image and stored capture disagree")
	}
	var width, height int
	if err := f.docs.QueryRow("SELECT pixel_width,pixel_height FROM pages WHERE id=?", page.ID).Scan(&width, &height); err != nil || width != info.Width || height != info.Height {
		t.Fatal("validated dimensions not stored", err)
	}
	var stored []byte
	var size int64
	if err = f.docs.QueryRow("SELECT sha256,byte_size FROM artifacts WHERE id=? AND kind='original' AND state='ready'", artifact.ID).Scan(&stored, &size); err != nil {
		t.Fatal(err)
	}
	if !sameHash(stored, info.SHA256) || size != info.Size {
		t.Fatal("database did not store verified image metadata")
	}
}
