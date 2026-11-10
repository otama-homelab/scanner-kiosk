package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func manualPhotoFixture(t *testing.T) (*fixture, RenderAdmission) {
	t.Helper()
	f := scanFixture(t)
	ctx := context.Background()
	r := photoStoreRequest()
	a, e := f.store.AdmitImport(ctx, alice, r, importLimits())
	if e != nil {
		t.Fatal(e)
	}
	sealPhoto(t, f, a, r)
	claim, e := f.store.ClaimNextImport(ctx, runner)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.store.CommitImportPages(ctx, alice, claim.Job.ID, ImportStop{RunnerID: runner, ProcessStoppedAtMS: f.now, WorkspaceCleanedAtMS: f.now}, []ImportedPage{photoImported()}); e != nil {
		t.Fatal(e)
	}
	p, e := f.store.ReadDocument(ctx, alice, a.DocumentID)
	if e != nil {
		t.Fatal(e)
	}
	return f, RenderAdmission{DocumentID: p.ID, Revision: p.Revision, IdempotencyKey: "request_photo_crop001", Settings: RenderSettings{Format: "pdf", PDFEncoding: "lossless"}, ManualCrop: &PhotoCrop{PageID: p.Pages[0].ID, SourceSHA256: p.Pages[0].Photo.SHA256, Width: 1, Height: 2}, ReserveBytes: 100}
}
func manualArtifact() ArtifactRecord {
	return ArtifactRecord{ID: "artifact_manual_photo001", RelativePath: "work_photo_crop001/cropped.png", MediaType: "image/png", Size: 20, SHA256: sha256.Sum256([]byte("cropped photo")), PixelWidth: 1, PixelHeight: 2}
}

func TestPhotoManualEditRevisionAndUndo(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"owner", "expired", "revision", "hash", "bounds", "negative", "overflow", "wrong-page", "reocr", "ocr", "scan", "quota"} {
		t.Run(mode, func(t *testing.T) {
			f, r := manualPhotoFixture(t)
			owner := alice
			want := ErrInvalid
			switch mode {
			case "owner":
				owner = bob
				want = ErrUnavailable
			case "expired":
				f.now += int64(25 * time.Hour / time.Millisecond)
				want = ErrUnavailable
			case "revision":
				r.Revision++
				want = ErrRevision
			case "hash":
				r.ManualCrop.SourceSHA256 = strings.Repeat("0", 64)
				want = ErrConflict
			case "bounds":
				r.ManualCrop.Width = 4
			case "negative":
				r.ManualCrop.X = -1
			case "overflow":
				r.ManualCrop.X = int(^uint(0) >> 1)
			case "wrong-page":
				r.ManualCrop.PageID = "missing_photo_page001"
				want = ErrUnavailable
			case "reocr":
				r.ReOCR = true
			case "ocr":
				r.Settings.OCR.Enabled = true
			case "scan":
				r.ManualCrop.PageID = correctionPage
				want = ErrUnavailable
			case "quota":
				r.ReserveBytes = 101
				want = ErrQuota
			}
			if _, e := f.store.AdmitRender(ctx, owner, r, admissionLimits()); !errors.Is(e, want) {
				t.Fatalf("%s: %v want %v", mode, e, want)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM page_corrections") != 0 || scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind='render'") != 0 {
				t.Fatal("rejected edit mutated state")
			}
		})
	}
	f, r := manualPhotoFixture(t)
	a, e := f.store.AdmitRender(ctx, alice, r, admissionLimits())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.ClaimExport(ctx, alice, a.JobID, runner); e != nil {
		t.Fatal(e)
	}
	if e = f.store.PublishExport(ctx, alice, a.JobID, runner, 1, ArtifactRecord{ID: "wrong_pdf_output001", RelativePath: "wrong.pdf", MediaType: "application/pdf", Size: 20}); !errors.Is(e, ErrConflict) {
		t.Fatal("manual job accepted plain output", e)
	}
	if e = f.store.CompleteOCRResult(ctx, alice, a.JobID, runner, 1, OCRResultBundle{}); e == nil {
		t.Fatal("manual job accepted OCR bundle")
	}
	if e = f.store.CompletePhotoCrop(ctx, alice, a.JobID, runner, 1, manualArtifact()); e != nil {
		t.Fatal(e)
	}
	p, e := f.store.ReadDocument(ctx, alice, r.DocumentID)
	if e != nil || p.Revision != 2 || p.Pages[0].Correction == nil || p.Pages[0].Correction.State != "applied" {
		t.Fatal("manual publication", p, e)
	}
	base, e := f.store.ReadPhotoBaseline(ctx, alice, r.DocumentID, r.ManualCrop.PageID)
	if e != nil || base.Artifact.ID != photoImported().Artifact.ID {
		t.Fatal("baseline changed", e)
	}
	if _, e = f.store.ReadPhotoBaseline(ctx, bob, r.DocumentID, r.ManualCrop.PageID); !errors.Is(e, ErrUnavailable) {
		t.Fatal("foreign baseline", e)
	}
	replay, e := f.store.AdmitRender(ctx, alice, r, admissionLimits())
	if e != nil || !replay.Replay || replay.JobID != a.JobID {
		t.Fatal("replay readmitted", replay, e)
	}
	undo := CorrectionRequest{DocumentID: r.DocumentID, PageID: r.ManualCrop.PageID, Revision: 1, IdempotencyKey: "request_photo_undo001"}
	if _, e = f.store.UndoCorrection(ctx, alice, undo, time.Hour*72); !errors.Is(e, ErrRevision) {
		t.Fatal("stale undo", e)
	}
	undo.Revision = 2
	p, e = f.store.UndoCorrection(ctx, alice, undo, time.Hour*72)
	if e != nil || p.Revision != 3 || p.Pages[0].Correction != nil {
		t.Fatal("undo", p, e)
	}
	effective, e := f.store.ReadPageEffective(ctx, alice, r.DocumentID, r.ManualCrop.PageID)
	if e != nil || effective.Artifact.ID != base.Artifact.ID {
		t.Fatal("undo did not restore normalized baseline", e)
	}
}

func TestPhotoManualCommitRecoveryAndCancellation(t *testing.T) {
	for _, mode := range []string{"commit-lost", "rollback", "cancelled", "expired", "ordinary-job", "stale-source", "wrong-output"} {
		t.Run(mode, func(t *testing.T) {
			f, r := manualPhotoFixture(t)
			ctx := context.Background()
			if mode == "ordinary-job" {
				r.ManualCrop = nil
			}
			a, e := f.store.AdmitRender(ctx, alice, r, admissionLimits())
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.store.ClaimExport(ctx, alice, a.JobID, runner); e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "cancelled":
				if _, e = f.store.CancelJob(ctx, alice, a.JobID, "request_photo_cancel001", 72*time.Hour); e != nil {
					t.Fatal(e)
				}
			case "expired":
				f.now += int64(25 * time.Hour / time.Millisecond)
			case "stale-source":
				must(t, f.docs, "UPDATE artifacts SET state='deleting' WHERE id=?", photoImported().Artifact.ID)
			}
			artifact := manualArtifact()
			if mode == "wrong-output" {
				artifact.PixelWidth = 2
			}
			if mode == "commit-lost" || mode == "rollback" {
				f.store.commit = func(tx *sql.Tx) error {
					if mode == "commit-lost" {
						if e := tx.Commit(); e != nil {
							return e
						}
					}
					return errors.New("commit acknowledgement lost")
				}
			}
			revision := int64(1)
			if mode == "ordinary-job" {
				revision = 2
			}
			e = f.store.CompletePhotoCrop(ctx, alice, a.JobID, runner, revision, artifact)
			if mode == "commit-lost" || mode == "rollback" {
				if !errors.Is(e, ErrCommitUncertain) {
					t.Fatal(e)
				}
				f.store.commit = func(tx *sql.Tx) error { return tx.Commit() }
				recorded, e := f.store.PhotoCropRecorded(ctx, alice, a.JobID, artifact)
				if e != nil || recorded != (mode == "commit-lost") {
					t.Fatal("readback", recorded, e)
				}
			} else if e == nil {
				t.Fatal("invalid publication succeeded")
			}
			want := 0
			if mode == "commit-lost" {
				want = 1
			}
			if scalar(t, f.docs, "SELECT count(*) FROM page_corrections WHERE state='applied'") != want {
				t.Fatal("publication crossed boundary")
			}
			if mode == "cancelled" {
				if e = f.store.FinishStoppedRender(ctx, alice, a.JobID, RenderStop{RunnerID: runner, ProcessStoppedAtMS: f.now, WorkspaceCleanedAtMS: f.now}, "failed"); e != nil {
					t.Fatal(e)
				}
				if scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 {
					t.Fatal("cancelled reservation leaked")
				}
			}
		})
	}
}
