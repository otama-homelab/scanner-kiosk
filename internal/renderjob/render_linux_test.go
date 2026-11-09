//go:build linux

package renderjob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/storage"
)

const owner = "principal_render_001"
const document = "document_render_001"
const runner = "worker_render_00001"

type faultRecords struct {
	*documents.Store
	mode         string
	calls, reads int
	cleanupCalls int
}

func (f *faultRecords) PublishExport(ctx context.Context, o, j, r string, v int64, a documents.Export) error {
	f.calls++
	if f.mode == "rollback" || f.mode == "unreadable-rollback" {
		return documents.ErrCommitUncertain
	}
	err := f.Store.PublishExport(ctx, o, j, r, v, a)
	if err == nil && (f.mode == "committed-error" || f.mode == "unreadable-commit") {
		return documents.ErrCommitUncertain
	}
	return err
}
func (f *faultRecords) ExportRecorded(ctx context.Context, o, j, a string) (bool, error) {
	f.reads++
	if f.mode == "unreadable-commit" || f.mode == "unreadable-rollback" {
		return false, documents.ErrStorage
	}
	return f.Store.ExportRecorded(ctx, o, j, a)
}
func sqlExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T, requested ...documents.RenderSettings) (*faultRecords, *sql.DB, string, string, pdfassemble.Options) {
	return fixtureWithPDF(t, false, requested...)
}

func fixtureWithPDF(t *testing.T, imported bool, requested ...documents.RenderSettings) (*faultRecords, *sql.DB, string, string, pdfassemble.Options) {
	return fixtureWithPDFData(t, imported, invisibleLayer(), requested...)
}

func fixtureWithPDFData(t *testing.T, imported bool, pdf []byte, requested ...documents.RenderSettings) (*faultRecords, *sql.DB, string, string, pdfassemble.Options) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	state, err := storage.Open(filepath.Join(t.TempDir(), "state.db"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	docs, err := storage.Open(filepath.Join(t.TempDir(), "docs.db"), storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { docs.Close() })
	store, err := documents.New(state, docs, time.Now, func(context.Context) (int64, error) { return 1 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	sqlExec(t, state, "INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'guest',?)", owner, now)
	sqlExec(t, docs, "INSERT INTO owners(id) VALUES(?)", owner)
	sqlExec(t, docs, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',?,?)", document, owner, now, now+3600000)
	var buffer bytes.Buffer
	if err = png.Encode(&buffer, image.NewGray(image.Rect(0, 0, 80, 100))); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	digest := sha256.Sum256(data)
	if err = os.WriteFile(filepath.Join(root, "original.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	sqlExec(t, docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_render_001',?,?,'original','ready','original.png','image/png',?,?,1,?)`, document, owner, len(data), digest[:], now)
	sqlExec(t, docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,rotation,pixel_width,pixel_height) VALUES('page_render_00001',?,?,0,'original_render_001','a5','gray',300,90,80,100)`, document, owner)
	if imported {
		data := pdf
		digest := sha256.Sum256(data)
		if err = os.WriteFile(filepath.Join(root, "imported.pdf"), data, 0600); err != nil {
			t.Fatal(err)
		}
		sqlExec(t, docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_import_001',?,?,'original','ready','imported.pdf','application/pdf',?,?,1,?)`, document, owner, len(data), digest[:], now)
		dimensions, e := api.PageDims(context.Background(), bytes.NewReader(data), model.NewStatelessConfiguration())
		if e != nil || len(dimensions) != 1 {
			t.Fatal("fixture geometry", e)
		}
		sqlExec(t, docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pdf_width_points,pdf_height_points) VALUES('page_import_00001',?,?,1,'original_import_001','pdf',?,?)`, document, owner, dimensions[0].Width, dimensions[0].Height)
	}
	settings := documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless"}
	if len(requested) > 0 {
		settings = requested[0]
	}
	admission, err := store.AdmitRender(context.Background(), owner, documents.RenderAdmission{DocumentID: document, Revision: 1, IdempotencyKey: "request_render_0001", Settings: settings, ReserveBytes: 1 << 20}, documents.AdmissionLimits{UserMaxBytes: 2 << 20, GlobalMaxBytes: 4 << 20, JobMaxBytes: 1 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ClaimExport(context.Background(), owner, admission.JobID, runner); err != nil {
		t.Fatal(err)
	}
	return &faultRecords{Store: store}, docs, root, admission.JobID, pdfassemble.Options{MaxInputBytes: 1 << 20, MaxOutputBytes: 1 << 20, MaxPagePixels: 100000, MaxPages: 50, JPEGQuality: 92}
}
func TestPreparePublishAndCommitReconciliation(t *testing.T) {
	for _, mode := range []string{"success", "committed-error", "rollback", "unreadable-commit", "unreadable-rollback", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			records, _, root, job, options := fixture(t)
			records.mode = mode
			prepared, err := Prepare(context.Background(), records, root, owner, job, runner, options)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if mode == "cancelled" {
				if err = records.RequestCancel(context.Background(), owner, job); err != nil {
					t.Fatal(err)
				}
			}
			artifact, err := prepared.Publish(context.Background())
			pending := mode == "unreadable-commit" || mode == "unreadable-rollback"
			if pending {
				if !errors.Is(err, ErrPending) || !errors.Is(prepared.Close(), ErrPending) || !errors.Is(prepared.Finish(context.Background(), "failed"), ErrPending) {
					t.Fatal("uncertain output allowed deletion")
				}
				entries, _ := os.ReadDir(root)
				if len(entries) != 2 {
					t.Fatal("uncertain output removed")
				}
				records.mode = "success"
				artifact, err = prepared.Publish(context.Background())
			}
			success := mode == "success" || mode == "committed-error" || mode == "unreadable-commit"
			if success {
				if err != nil {
					t.Fatal(err)
				}
				current, e := records.GetExport(context.Background(), owner, document)
				if e != nil || current.ID != artifact.ID {
					t.Fatal("publication mismatch")
				}
				data, e := os.ReadFile(filepath.Join(root, current.RelativePath))
				if e != nil || !bytes.HasPrefix(data, []byte("%PDF-")) || sha256.Sum256(data) != current.SHA256 {
					t.Fatal("published PDF missing")
				}
			} else {
				if err == nil {
					t.Fatal("failed publication accepted")
				}
				entries, _ := os.ReadDir(root)
				if len(entries) != 1 {
					t.Fatal("unpublished output retained")
				}
			}
			if records.calls != 1 {
				t.Fatal("publication was repeated after uncertainty")
			}
			if _, err = os.Stat(filepath.Join(root, "original.png")); err != nil {
				t.Fatal("original removed")
			}
		})
	}
}
func TestPrepareRejectsCorruptOriginal(t *testing.T) {
	records, _, root, job, options := fixture(t)
	if err := os.WriteFile(filepath.Join(root, "original.png"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if prepared, err := Prepare(context.Background(), records, root, owner, job, runner, options); err == nil || prepared != nil {
		t.Fatal("corrupt original accepted")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatal("failed preparation workspace remains")
	}
	if records.calls != 0 {
		t.Fatal("failed preparation published")
	}
}

func TestPrepareRejectsUnsupportedOCR(t *testing.T) {
	records, _, root, job, options := fixture(t, documents.RenderSettings{Format: "pdf", OCR: documents.RenderOCR{Enabled: true, ProviderID: "tesseract", ModelID: "installed", ModelVersion: "test-version", Languages: []string{"ja", "en"}, QualityPreset: "auto"}})
	if prepared, err := Prepare(context.Background(), records, root, owner, job, runner, options); !errors.Is(err, ErrUnsupported) || prepared != nil {
		t.Fatal("OCR silently omitted")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 || records.calls != 0 {
		t.Fatal("unsupported pipeline created output")
	}
}
func TestPreparationBoundsAndUnpublishedClose(t *testing.T) {
	for _, mode := range []string{"pixel-limit", "cancelled", "abandoned"} {
		t.Run(mode, func(t *testing.T) {
			records, _, root, job, options := fixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "pixel-limit" {
				options.MaxPagePixels = 1
			}
			if mode == "cancelled" {
				cancel()
			}
			prepared, err := Prepare(ctx, records, root, owner, job, runner, options)
			if mode == "abandoned" {
				if err != nil {
					t.Fatal(err)
				}
				if err = prepared.Close(); err != nil {
					t.Fatal(err)
				}
				if _, err = prepared.Publish(context.Background()); !errors.Is(err, ErrClosed) {
					t.Fatal("closed preparation published")
				}
			} else if err == nil || prepared != nil {
				t.Fatal("invalid preparation accepted")
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 1 || records.calls != 0 {
				t.Fatal("unpublished preparation retained output")
			}
		})
	}
}

func (f *faultRecords) FinishStoppedRender(ctx context.Context, o, j string, stop documents.RenderStop, outcome string) error {
	f.cleanupCalls++
	if f.mode == "cleanup-rollback" {
		return documents.ErrCommitUncertain
	}
	err := f.Store.FinishStoppedRender(ctx, o, j, stop, outcome)
	if err == nil && (f.mode == "cleanup-committed-error" || f.mode == "cleanup-unreadable") {
		return documents.ErrCommitUncertain
	}
	return err
}
func (f *faultRecords) RenderStoppedRecorded(ctx context.Context, o, j, r string) (bool, error) {
	if f.mode == "cleanup-unreadable" {
		return false, documents.ErrStorage
	}
	return f.Store.RenderStoppedRecorded(ctx, o, j, r)
}
func TestFinishPreparedRenderAndReconcile(t *testing.T) {
	for _, mode := range []string{"failed", "cancelled", "cleanup-committed-error", "cleanup-unreadable", "cleanup-rollback"} {
		t.Run(mode, func(t *testing.T) {
			records, docs, root, job, options := fixture(t)
			records.mode = mode
			prepared, err := Prepare(context.Background(), records, root, owner, job, runner, options)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if mode == "cancelled" {
				if err = records.RequestCancel(context.Background(), owner, job); err != nil {
					t.Fatal(err)
				}
			}
			err = prepared.Finish(context.Background(), "failed")
			if mode == "cleanup-unreadable" {
				if !errors.Is(err, ErrPending) {
					t.Fatal("cleanup uncertainty lost")
				}
				records.mode = "success"
				err = prepared.Finish(context.Background(), "failed")
			}
			if mode == "cleanup-rollback" {
				if !errors.Is(err, documents.ErrCommitUncertain) {
					t.Fatal("cleanup rollback hidden")
				}
				records.mode = "success"
				err = prepared.Finish(context.Background(), "failed")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = prepared.Finish(context.Background(), "failed"); err != nil {
				t.Fatal("confirmed cleanup not idempotent")
			}
			expectedCalls := 1
			if mode == "cleanup-rollback" {
				expectedCalls = 2
			}
			if records.cleanupCalls != expectedCalls {
				t.Fatal("cleanup repeated before read-back")
			}
			var count int
			if err = docs.QueryRow("SELECT count(*) FROM reservations WHERE job_id=?", job).Scan(&count); err != nil || count != 0 {
				t.Fatal("reservation not released")
			}
			if _, err = prepared.Publish(context.Background()); !errors.Is(err, ErrClosed) {
				t.Fatal("stopped preparation published")
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 1 {
				t.Fatal("cleanup removed original or retained output")
			}
		})
	}
}

func TestWatchedPreparationUsesDatabasePulse(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "already-cancelled"}[cancelled], func(t *testing.T) {
			records, _, root, job, options := fixture(t)
			if cancelled {
				if err := records.RequestCancel(context.Background(), owner, job); err != nil {
					t.Fatal(err)
				}
			}
			prepared, err := PrepareWatched(context.Background(), records, root, owner, job, runner, options, time.Millisecond)
			if cancelled {
				if err == nil || prepared != nil {
					t.Fatal("cancelled job started")
				}
				entries, _ := os.ReadDir(root)
				if len(entries) != 1 {
					t.Fatal("cancelled job made workspace")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err = prepared.Finish(context.Background(), "interrupted"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestExecuteClaimedRenderLifecycle(t *testing.T) {
	for _, mode := range []string{"success", "corrupt", "pixel-limit", "cancelled", "parent-cancelled", "committed-error", "rollback", "unreadable-commit", "cleanup-unreadable", "unknown-root"} {
		t.Run(mode, func(t *testing.T) {
			records, docs, root, job, options := fixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			records.mode = mode
			switch mode {
			case "corrupt":
				if err := os.WriteFile(filepath.Join(root, "original.png"), []byte("corrupt"), 0600); err != nil {
					t.Fatal(err)
				}
			case "pixel-limit", "cleanup-unreadable":
				options.MaxPagePixels = 1
			case "cancelled":
				if err := records.RequestCancel(ctx, owner, job); err != nil {
					t.Fatal(err)
				}
			case "parent-cancelled":
				cancel()
			}
			inputRoot := root
			if mode == "unknown-root" {
				inputRoot = filepath.Join(root, "unavailable")
			}
			recovery, err := ExecuteClaimed(ctx, records, inputRoot, owner, job, runner, options, time.Millisecond)
			if mode == "unreadable-commit" {
				if !errors.Is(err, ErrPending) || recovery == nil {
					t.Fatal("publication recovery handle missing")
				}
				records.mode = "success"
				if _, e := recovery.Publish(context.Background()); e != nil {
					t.Fatal(e)
				}
			} else if mode == "cleanup-unreadable" {
				if !errors.Is(err, ErrPending) || recovery == nil {
					t.Fatal("cleanup recovery handle missing")
				}
				records.mode = "success"
				if e := recovery.Finish(context.Background(), "failed"); e != nil {
					t.Fatal(e)
				}
			} else if mode == "success" || mode == "committed-error" {
				if err != nil || recovery != nil {
					t.Fatalf("successful execution: %v", err)
				}
			} else if err == nil {
				t.Fatal("failed execution accepted")
			}
			if recovery != nil {
				defer recovery.Close()
			}
			var state string
			if e := docs.QueryRow("SELECT state FROM jobs WHERE id=?", job).Scan(&state); e != nil {
				t.Fatal(e)
			}
			expected := "failed"
			switch mode {
			case "success", "committed-error", "unreadable-commit":
				expected = "succeeded"
			case "cancelled":
				expected = "cancelled"
			case "parent-cancelled":
				expected = "interrupted"
			case "unknown-root":
				expected = "running"
			}
			if state != expected {
				t.Fatalf("state %s, expected %s; execute error=%v; recovery retained=%t", state, expected, err, recovery != nil)
			}
			var reserved int
			if e := docs.QueryRow("SELECT count(*) FROM reservations WHERE job_id=?", job).Scan(&reserved); e != nil {
				t.Fatal(e)
			}
			expectedReserved := 0
			if mode == "unknown-root" {
				expectedReserved = 1
			}
			if reserved != expectedReserved {
				t.Fatal("incorrect reservation release")
			}
			if _, e := os.Stat(filepath.Join(root, "original.png")); e != nil {
				t.Fatal("original removed")
			}
			if records.calls > 1 {
				t.Fatal("publication/generation retried")
			}
		})
	}
}
