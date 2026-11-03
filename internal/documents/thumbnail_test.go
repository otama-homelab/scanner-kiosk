package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func thumbnailRecord() ThumbnailRecord {
	return ThumbnailRecord{OriginalID: "original_alice_0001", Artifact: ArtifactRecord{ID: "thumbnail_alice_001", RelativePath: "work_thumb_001/image.jpg", MediaType: "image/jpeg", Size: 10, SHA256: sha256.Sum256([]byte("thumbnail")), PixelWidth: 32, PixelHeight: 16}}
}
func thumbnailFixture(t *testing.T) *fixture {
	f := setup(t)
	seedPage(t, f, docA, alice, "original_alice_0001", "page_alice_000001")
	claim(t, f)
	return f
}
func TestThumbnailBundleAtomicPublicationAndMetadataEdits(t *testing.T) {
	f := thumbnailFixture(t)
	ctx := context.Background()
	thumb := thumbnailRecord()
	if _, err := f.store.ReadPageThumbnail(ctx, alice, docA, "page_alice_000001"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing derivative", err)
	}
	if err := f.store.PublishExportWithThumbnails(ctx, alice, jobA, runner, 1, output(), []ThumbnailRecord{thumb}); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.ReadPageThumbnail(ctx, alice, docA, "page_alice_000001")
	if err != nil || got.Artifact.ID != thumb.Artifact.ID || got.ExpiresAtMS != 10000 {
		t.Fatal(got, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM job_artifacts WHERE job_id=?", jobA) != 2 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 {
		t.Fatal("bundle not atomic")
	}
	if recorded, err := f.store.ExportRecorded(ctx, alice, jobA, output().ID); err != nil || !recorded {
		t.Fatal(recorded, err)
	}
	edit := EditRequest{DocumentID: docA, Revision: 1, IdempotencyKey: "request_thumb_rotate", Operation: "rotate", PageID: "page_alice_000001", Rotation: 90}
	if _, err = f.store.Edit(ctx, alice, edit, time.Hour); err != nil {
		t.Fatal(err)
	}
	after, err := f.store.ReadPageThumbnail(ctx, alice, docA, "page_alice_000001")
	if err != nil || after != got {
		t.Fatal("metadata edit invalidated source derivative", err)
	}
	edit = EditRequest{DocumentID: docA, Revision: 2, IdempotencyKey: "request_thumb_delete", Operation: "delete_page", PageID: "page_alice_000001"}
	if _, err = f.store.Edit(ctx, alice, edit, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ReadPageThumbnail(ctx, alice, docA, "page_alice_000001"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("deleted page readable", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM artifacts WHERE kind='thumbnail'") != 1 {
		t.Fatal("metadata edit pretended to remove file")
	}
}
func TestThumbnailBundleRejectsWithoutPartialPublication(t *testing.T) {
	for _, mode := range []string{"aggregate-reservation", "foreign-source", "missing-page", "foreign-owner", "foreign-runner", "cancelled", "expired", "disabled", "geometry", "format", "duplicate-source", "late-bad-source"} {
		t.Run(mode, func(t *testing.T) {
			f := thumbnailFixture(t)
			ctx := context.Background()
			owner, worker := alice, runner
			thumb := thumbnailRecord()
			thumbs := []ThumbnailRecord{thumb}
			switch mode {
			case "aggregate-reservation":
				must(t, f.docs, "UPDATE reservations SET byte_size=12 WHERE job_id=?", jobA)
			case "foreign-source":
				seedPage(t, f, docB, bob, "original_bob_00001", "page_bob_00000001")
				thumbs[0].OriginalID = "original_bob_00001"
			case "missing-page":
				must(t, f.docs, "DELETE FROM pages WHERE document_id=?", docA)
			case "foreign-owner":
				owner = bob
			case "foreign-runner":
				worker = "other_worker_00001"
			case "cancelled":
				if err := f.store.RequestCancel(ctx, alice, jobA); err != nil {
					t.Fatal(err)
				}
			case "expired":
				f.now = 10000
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
			case "geometry":
				thumbs[0].Artifact.PixelWidth = 513
			case "format":
				thumbs[0].Artifact.MediaType = "image/png"
			case "duplicate-source":
				other := thumb
				other.Artifact.ID = "thumbnail_alice_002"
				other.Artifact.RelativePath = "other.jpg"
				thumbs = append(thumbs, other)
			case "late-bad-source":
				other := thumb
				other.Artifact.ID = "thumbnail_alice_002"
				other.Artifact.RelativePath = "other.jpg"
				other.OriginalID = "missing_original_01"
				thumbs = append(thumbs, other)
			}
			if err := f.store.PublishExportWithThumbnails(ctx, owner, jobA, worker, 1, output(), thumbs); err == nil {
				t.Fatal("accepted", mode)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM artifacts WHERE kind IN ('output','thumbnail')") != 0 || scalar(t, f.docs, "SELECT count(*) FROM page_thumbnails") != 0 || scalar(t, f.docs, "SELECT count(*) FROM job_artifacts") != 0 || scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", jobA) != 1 {
				t.Fatal("partial publication", mode)
			}
		})
	}
}
func TestThumbnailReadRevokesAuthority(t *testing.T) {
	for _, mode := range []string{"foreign-owner", "cross-document", "expired", "disabled", "deleting", "source-deleting", "thumbnail-deleting"} {
		t.Run(mode, func(t *testing.T) {
			f := thumbnailFixture(t)
			ctx := context.Background()
			if err := f.store.PublishExportWithThumbnails(ctx, alice, jobA, runner, 1, output(), []ThumbnailRecord{thumbnailRecord()}); err != nil {
				t.Fatal(err)
			}
			owner, document := alice, docA
			switch mode {
			case "foreign-owner":
				owner = bob
			case "cross-document":
				document = docB
			case "expired":
				f.now = 10000
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
			case "deleting":
				must(t, f.docs, "UPDATE documents SET state='deleting' WHERE id=?", docA)
			case "source-deleting":
				must(t, f.docs, "UPDATE artifacts SET state='deleting' WHERE id=?", "original_alice_0001")
			case "thumbnail-deleting":
				must(t, f.docs, "UPDATE artifacts SET state='deleting' WHERE id=?", thumbnailRecord().Artifact.ID)
			}
			if got, err := f.store.ReadPageThumbnail(ctx, owner, document, "page_alice_000001"); !errors.Is(err, ErrUnavailable) || got != (ArtifactReference{}) {
				t.Fatal(got, err)
			}
		})
	}
}
func TestThumbnailBundleUncertainCommitReconcilesTogether(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "committed"}[committed], func(t *testing.T) {
			f := thumbnailFixture(t)
			f.store.commit = func(tx *sql.Tx) error {
				if committed {
					if err := tx.Commit(); err != nil {
						return err
					}
				}
				return errors.New("lost acknowledgement")
			}
			err := f.store.PublishExportWithThumbnails(context.Background(), alice, jobA, runner, 1, output(), []ThumbnailRecord{thumbnailRecord()})
			if !errors.Is(err, ErrCommitUncertain) {
				t.Fatal(err)
			}
			recorded, err := f.store.ExportRecorded(context.Background(), alice, jobA, output().ID)
			if err != nil || recorded != committed {
				t.Fatal(recorded, err)
			}
			wanted := 0
			if committed {
				wanted = 1
			}
			if scalar(t, f.docs, "SELECT count(*) FROM page_thumbnails") != wanted {
				t.Fatal("derivative split from export")
			}
		})
	}
}
