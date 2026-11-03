//go:build linux

package scanthumbnail

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/thumbnail"
)

const owner = "principal_preview_01"
const job = "job_preview_scan_001"
const boot = "worker_preview_boot_01"
const device = "scanner_preview_01"
const document = "document_preview_01"

type faults struct {
	*documents.Store
	db            *sql.DB
	mode          string
	writes, reads int
}

func (r *faults) CommitScanPageWithThumbnails(ctx context.Context, o, j string, revision int64, a documents.ArtifactRecord, s documents.ScanStop, thumbs []documents.ThumbnailRecord) (documents.ScanPage, error) {
	r.writes++
	if r.mode == "rollback" || r.mode == "unreadable-rollback" {
		return documents.ScanPage{}, documents.ErrCommitUncertain
	}
	if r.mode == "cancel-before-commit" {
		if err := r.Store.RequestCancel(ctx, o, j); err != nil {
			return documents.ScanPage{}, err
		}
	}
	page, err := r.Store.CommitScanPageWithThumbnails(ctx, o, j, revision, a, s, thumbs)
	if err != nil {
		return page, err
	}
	switch r.mode {
	case "edited-commit":
		if _, err = r.db.Exec("DELETE FROM pages"); err != nil {
			return page, err
		}
	case "inconsistent-commit":
		if _, err = r.db.Exec("DELETE FROM page_thumbnails"); err != nil {
			return page, err
		}
	}
	if r.mode != "" {
		return documents.ScanPage{}, documents.ErrCommitUncertain
	}
	return page, nil
}
func (r *faults) CaptureBundleRecorded(ctx context.Context, o, j string, a documents.ArtifactRecord, thumbs []documents.ThumbnailRecord) (bool, error) {
	r.reads++
	if r.mode == "unreadable-commit" || r.mode == "unreadable-rollback" {
		return false, documents.ErrStorage
	}
	return r.Store.CaptureBundleRecorded(ctx, o, j, a, thumbs)
}
func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T, budget int64) (*Publisher, *faults, *sql.DB, documents.ArtifactRecord, documents.ScanStop) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	state, err := storage.Open(filepath.Join(t.TempDir(), "state.sqlite3"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	db, err := storage.Open(filepath.Join(t.TempDir(), "documents.sqlite3"), storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := documents.New(state, db, time.Now, func(context.Context) (int64, error) { return 1 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
	bitmap := image.NewGray(image.Rect(0, 0, 80, 100))
	for i := range bitmap.Pix {
		bitmap.Pix[i] = 200
	}
	var data bytes.Buffer
	if err = png.Encode(&data, bitmap); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(root, "capture_preview_01"), 0700); err != nil {
		t.Fatal(err)
	}
	a := documents.ArtifactRecord{ID: "original_preview_01", RelativePath: "capture_preview_01/original.png", MediaType: "image/png", Size: int64(data.Len()), SHA256: sha256.Sum256(data.Bytes()), PixelWidth: 80, PixelHeight: 100}
	if err = os.WriteFile(filepath.Join(root, a.RelativePath), data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	snapshot, _ := json.Marshal(documents.ScanSnapshot{Version: 1, DeviceID: device, WorkerBootID: boot, LeaseEpoch: 2, Capture: documents.Capture{Size: "a4", Color: "gray", DPI: 300}})
	exec(t, state, `INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'guest',?)`, owner, now)
	exec(t, db, `INSERT INTO owners(id) VALUES(?)`, owner)
	exec(t, db, `INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','scanning',?,?)`, document, owner, now, now+3600000)
	exec(t, db, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id,started_at_ms,heartbeat_at_ms) VALUES(?,?,?,'scan','running',1,?,?,?,?,?,?)`, job, document, owner, string(snapshot), now, now+60000, boot, now, now)
	exec(t, db, `INSERT INTO device_leases(device_id,epoch,state,job_id) VALUES(?,2,'held',?)`, device, job)
	exec(t, db, `INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,?)`, job, owner, budget)
	records := &faults{Store: store, db: db}
	p := New(records, root, 2<<20, 100000)
	return p, records, db, a, documents.ScanStop{DeviceID: device, WorkerBootID: boot, LeaseEpoch: 2, ProcessStoppedAtMS: now, IdleObservedAtMS: time.Now().UnixMilli()}
}

func TestCapturePreviewLifecycleAndCommitReadback(t *testing.T) {
	for _, mode := range []string{"", "commit-error", "rollback", "unreadable-commit", "unreadable-rollback", "edited-commit", "inconsistent-commit", "cancel-before-commit"} {
		t.Run("mode-"+mode, func(t *testing.T) {
			p, records, db, a, stop := fixture(t, 2<<20)
			records.mode = mode
			ctx := context.Background()
			_, err := p.CommitScanPage(ctx, owner, job, 1, a, stop)
			if mode == "cancel-before-commit" {
				if !errors.Is(err, documents.ErrConflict) {
					t.Fatal(err)
				}
			} else if mode == "" && err != nil || mode != "" && !errors.Is(err, documents.ErrCommitUncertain) {
				var state string
				var started, deadline, heartbeat int64
				diagnosticErr := db.QueryRow("SELECT state,started_at_ms,deadline_at_ms,heartbeat_at_ms FROM jobs WHERE id=?", job).Scan(&state, &started, &deadline, &heartbeat)
				t.Fatalf("publication: %v; writes=%d previews=%d job=%s started=%d deadline=%d heartbeat=%d now=%d stop=%+v diagnostic=%v", err, records.writes, len(p.thumbnails), state, started, deadline, heartbeat, time.Now().UnixMilli(), stop, diagnosticErr)
			}
			if len(p.thumbnails) != 1 {
				t.Fatal("preview not sealed")
			}
			thumb := p.thumbnails[0].Artifact
			raw, e := os.ReadFile(filepath.Join(p.root, thumb.RelativePath))
			if e != nil || sha256.Sum256(raw) != thumb.SHA256 || int64(len(raw)) != thumb.Size {
				t.Fatal("sealed preview mismatch", e)
			}
			bitmap, e := jpeg.Decode(bytes.NewReader(raw))
			if e != nil || bitmap.Bounds().Dx() != 80 || bitmap.Bounds().Dy() != 100 {
				t.Fatal("preview orientation/geometry", e)
			}
			r, g, b, _ := bitmap.At(40, 50).RGBA()
			if r < 198*257 || r > 202*257 || g != r || b != r {
				t.Fatal("preview gray pixels", r, g, b)
			}
			recorded, e := p.CaptureRecorded(ctx, owner, job, a.ID)
			if mode == "inconsistent-commit" {
				if !errors.Is(e, documents.ErrConflict) || recorded {
					t.Fatal(recorded, e)
				}
				return
			}
			if mode == "unreadable-commit" || mode == "unreadable-rollback" {
				if !errors.Is(e, documents.ErrStorage) || recorded {
					t.Fatal(recorded, e)
				}
				if _, e = os.Stat(filepath.Join(p.root, thumb.RelativePath)); e != nil {
					t.Fatal("uncertainty erased preview", e)
				}
				records.mode = ""
				recorded, e = p.CaptureRecorded(ctx, owner, job, a.ID)
			}
			committed := mode != "rollback" && mode != "unreadable-rollback" && mode != "cancel-before-commit"
			if e != nil || recorded != committed {
				t.Fatal(recorded, e)
			}
			if committed {
				if e = p.ReleaseScanArtifacts(); e != nil {
					t.Fatal(e)
				}
				if e = p.AbortScanArtifacts(); e != nil {
					t.Fatal(e)
				} // released files cannot be aborted
				if _, e = os.Stat(filepath.Join(p.root, thumb.RelativePath)); e != nil {
					t.Fatal("committed preview erased", e)
				}
			} else {
				if e = p.AbortScanArtifacts(); e != nil {
					t.Fatal(e)
				}
				if _, e = os.Stat(filepath.Join(p.root, thumb.RelativePath)); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("unpublished preview retained", e)
				}
				var reservations int
				if e = db.QueryRow("SELECT count(*) FROM reservations").Scan(&reservations); e != nil || reservations != 1 {
					t.Fatal("thumbnail abort released native reservation", reservations, e)
				}
			}
			original, e := os.ReadFile(filepath.Join(p.root, a.RelativePath))
			if e != nil || sha256.Sum256(original) != a.SHA256 {
				t.Fatal("preview processing altered original", e)
			}
			if records.writes != 1 {
				t.Fatal("publication re-executed", records.writes)
			}
		})
	}
}

func TestTightCaptureReservationKeepsOriginalWithoutPreview(t *testing.T) {
	p, _, db, a, stop := fixture(t, 4096)
	if _, err := p.CommitScanPage(context.Background(), owner, job, 1, a, stop); err != nil {
		t.Fatal(err)
	}
	if p.scope != nil || len(p.thumbnails) != 0 {
		t.Fatal("reservation grew for optional preview")
	}
	var originals, thumbs int
	if err := db.QueryRow("SELECT (SELECT count(*) FROM artifacts WHERE kind='original'),(SELECT count(*) FROM artifacts WHERE kind='thumbnail')").Scan(&originals, &thumbs); err != nil || originals != 1 || thumbs != 0 {
		t.Fatal(originals, thumbs, err)
	}
}

func TestPreviewLimitFallbackNeverHidesIndependentFailure(t *testing.T) {
	if !onlyPreviewLimit(errors.Join(thumbnail.ErrLimit)) || !onlyPreviewLimit(errors.Join(thumbnail.ErrLimit, artifactscope.ErrLimit)) {
		t.Fatal("pure optional limit not recognized")
	}
	for _, err := range []error{nil, documents.ErrCommitUncertain, errors.Join(thumbnail.ErrLimit, documents.ErrStorage), errors.Join(thumbnail.ErrLimit, context.Canceled)} {
		if onlyPreviewLimit(err) {
			t.Fatal("independent failure hidden", err)
		}
	}
}

func TestLargeReservationDoesNotEnlargePreviewInputBufferLimit(t *testing.T) {
	p, _, _, a, stop := fixture(t, 2<<30)
	p.maxBytes = 4 << 30
	if _, err := p.CommitScanPage(context.Background(), owner, job, 1, a, stop); err != nil {
		t.Fatal(err)
	}
	if len(p.thumbnails) != 1 {
		t.Fatal("large job configuration disabled a small source preview")
	}
	if err := p.ReleaseScanArtifacts(); err != nil {
		t.Fatal(err)
	}
}
