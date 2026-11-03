//go:build linux

package renderjob

import (
	"context"
	"errors"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/thumbnail"
)

type thumbnailRecords struct{ *faultRecords }

func (*thumbnailRecords) ThumbnailOptions() thumbnail.Options {
	return thumbnail.Options{MaxEdge: 32, MaxOutputBytes: 1 << 20, Quality: 82}
}
func (r *thumbnailRecords) PublishExportWithThumbnails(ctx context.Context, o, j, runner string, v int64, a documents.Export, thumbs []documents.ThumbnailRecord) error {
	r.calls++
	if r.mode == "rollback" || r.mode == "unreadable-rollback" {
		return documents.ErrCommitUncertain
	}
	err := r.Store.PublishExportWithThumbnails(ctx, o, j, runner, v, a, thumbs)
	if err == nil && (r.mode == "committed-error" || r.mode == "unreadable-commit") {
		return documents.ErrCommitUncertain
	}
	return err
}
func TestRenderThumbnailBundleLifecycleAndReconciliation(t *testing.T) {
	for _, mode := range []string{"success", "committed-error", "rollback", "unreadable-commit", "unreadable-rollback", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			base, _, root, job, options := fixture(t)
			records := &thumbnailRecords{base}
			records.mode = mode
			prepared, err := Prepare(context.Background(), records, root, owner, job, runner, options)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if len(prepared.thumbnails) != 1 {
				t.Fatal("thumbnail not prepared")
			}
			thumb := prepared.thumbnails[0]
			path := filepath.Join(root, thumb.Artifact.RelativePath)
			if mode == "cancelled" {
				if err = records.RequestCancel(context.Background(), owner, job); err != nil {
					t.Fatal(err)
				}
			}
			_, err = prepared.Publish(context.Background())
			if mode == "unreadable-commit" || mode == "unreadable-rollback" {
				if !errors.Is(err, ErrPending) || !errors.Is(prepared.Close(), ErrPending) {
					t.Fatal("uncertain bundle discarded", err)
				}
				if _, e := os.Stat(path); e != nil {
					t.Fatal(e)
				}
				records.mode = "success"
				_, err = prepared.Publish(context.Background())
			}
			success := mode == "success" || mode == "committed-error" || mode == "unreadable-commit"
			if success {
				if err != nil {
					t.Fatal(err)
				}
				reference, e := records.ReadPageThumbnail(context.Background(), owner, document, "page_render_00001")
				if e != nil || reference.Artifact.ID != thumb.Artifact.ID {
					t.Fatal(reference, e)
				}
				if e = prepared.Close(); e != nil {
					t.Fatal(e)
				}
				file, e := os.Open(path)
				if e != nil {
					t.Fatal(e)
				}
				bitmap, e := jpeg.Decode(file)
				file.Close()
				if e != nil || bitmap.Bounds().Dx() != 25 || bitmap.Bounds().Dy() != 32 {
					t.Fatal("source rotation changed or file lost", e)
				}
			} else {
				if err == nil {
					t.Fatal("bad publication succeeded")
				}
				if _, e := os.Stat(path); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("unpublished thumbnail retained", e)
				}
			}
			if records.calls != 1 {
				t.Fatal("uncertain bundle write repeated", records.calls)
			}
		})
	}
}

func TestRerenderReusesSourceThumbnail(t *testing.T) {
	base, db, root, job, options := fixture(t)
	records := &thumbnailRecords{base}
	ctx := context.Background()
	first, err := Prepare(ctx, records, root, owner, job, runner, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = first.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	doc, err := records.ReadDocument(ctx, owner, document)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := records.AdmitRender(ctx, owner, documents.RenderAdmission{DocumentID: document, Revision: doc.Revision, IdempotencyKey: "request_thumb_again", Settings: documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless"}, ReserveBytes: 1 << 20}, documents.AdmissionLimits{UserMaxBytes: 2 << 20, GlobalMaxBytes: 4 << 20, JobMaxBytes: 1 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = records.ClaimExport(ctx, owner, admitted.JobID, runner); err != nil {
		t.Fatal(err)
	}
	second, err := Prepare(ctx, records, root, owner, admitted.JobID, runner, options)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if len(second.thumbnails) != 0 {
		t.Fatal("existing source thumbnail regenerated")
	}
	if _, err = second.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM artifacts WHERE kind='thumbnail'").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
