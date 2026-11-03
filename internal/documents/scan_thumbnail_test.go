package documents

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func scanThumbnail() ThumbnailRecord {
	a := thumbnailRecord()
	a.OriginalID = captureArtifact().ID
	return a
}

func TestScanThumbnailPublicationIsAtomicAndSourceBound(t *testing.T) {
	f, scan, stop := startedCapture(t)
	ctx := context.Background()
	thumb := scanThumbnail()
	page, err := f.store.CommitScanPageWithThumbnails(ctx, alice, scan.JobID, scan.Revision, captureArtifact(), stop, []ThumbnailRecord{thumb})
	if err != nil {
		t.Fatal(err)
	}
	reference, err := f.store.ReadPageThumbnail(ctx, alice, scan.DocumentID, page.ID)
	if err != nil || reference.Artifact.ID != thumb.Artifact.ID {
		t.Fatal(reference, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM job_artifacts WHERE job_id=?", scan.JobID) != 2 || scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", scan.JobID) != 0 || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=?", scan.JobID) != 0 {
		t.Fatal("bundle completion not atomic")
	}
	for _, edit := range []string{"UPDATE pages SET rotation=90", "UPDATE documents SET revision=revision+1 WHERE id='" + scan.DocumentID + "'", "DELETE FROM pages", "UPDATE documents SET state='deleting' WHERE id='" + scan.DocumentID + "'", "UPDATE artifacts SET state='deleting'"} {
		must(t, f.docs, edit)
		if recorded, err := f.store.CaptureBundleRecorded(ctx, alice, scan.JobID, captureArtifact(), []ThumbnailRecord{thumb}); err != nil || !recorded {
			t.Fatal("committed bundle lost after metadata edit", edit, recorded, err)
		}
	}
	must(t, f.state, "UPDATE principals SET disabled_at_ms=2300 WHERE id=?", alice)
	f.now += 48 * time.Hour.Milliseconds()
	if recorded, err := f.store.CaptureBundleRecorded(ctx, alice, scan.JobID, captureArtifact(), []ThumbnailRecord{thumb}); err != nil || !recorded {
		t.Fatal(recorded, err)
	}
}

func TestScanThumbnailRejectsWithoutPublishingOriginal(t *testing.T) {
	for _, mode := range []string{"aggregate-budget", "foreign-source", "same-id", "oversize", "geometry", "duplicate", "cancel", "expired", "disabled", "foreign-owner", "runner"} {
		t.Run(mode, func(t *testing.T) {
			f, a, stop := startedCapture(t)
			thumb := scanThumbnail()
			thumbs := []ThumbnailRecord{thumb}
			owner := alice
			switch mode {
			case "aggregate-budget":
				thumbs[0].Artifact.Size = 49
			case "foreign-source":
				thumbs[0].OriginalID = "original_someone_else"
			case "same-id":
				thumbs[0].Artifact.ID = captureArtifact().ID
			case "oversize":
				thumbs[0].Artifact.Size = (1 << 20) + 1
			case "geometry":
				thumbs[0].Artifact.PixelWidth = 513
			case "duplicate":
				thumbs = append(thumbs, thumb)
			case "cancel":
				if err := f.store.RequestCancel(context.Background(), alice, a.JobID); err != nil {
					t.Fatal(err)
				}
			case "expired":
				f.now += 24 * time.Hour.Milliseconds()
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2300 WHERE id=?", alice)
			case "foreign-owner":
				owner = bob
			case "runner":
				stop.WorkerBootID = "different_worker_01"
			}
			if _, err := f.store.CommitScanPageWithThumbnails(context.Background(), owner, a.JobID, a.Revision, captureArtifact(), stop, thumbs); err == nil {
				t.Fatal("invalid bundle accepted")
			}
			if scalar(t, f.docs, "SELECT count(*) FROM artifacts") != 0 || scalar(t, f.docs, "SELECT count(*) FROM pages") != 0 || scalar(t, f.docs, "SELECT count(*) FROM page_thumbnails") != 0 || scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != 1 {
				t.Fatal("partial publication or reservation release")
			}
		})
	}
}

func TestScanBundleUncertaintyAndMismatchRemainSafe(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[committed], func(t *testing.T) {
			f, a, stop := startedCapture(t)
			ctx := context.Background()
			thumbs := []ThumbnailRecord{scanThumbnail()}
			f.store.commit = func(tx *sql.Tx) error {
				if committed {
					if err := tx.Commit(); err != nil {
						return err
					}
				}
				return errors.New("lost commit result")
			}
			if _, err := f.store.CommitScanPageWithThumbnails(ctx, alice, a.JobID, a.Revision, captureArtifact(), stop, thumbs); !errors.Is(err, ErrCommitUncertain) {
				t.Fatal(err)
			}
			if yes, err := f.store.CaptureBundleRecorded(ctx, alice, a.JobID, captureArtifact(), thumbs); err != nil || yes != committed {
				t.Fatal(yes, err)
			}
			if committed {
				bad := append([]ThumbnailRecord(nil), thumbs...)
				bad[0].Artifact.Size++
				if yes, err := f.store.CaptureBundleRecorded(ctx, alice, a.JobID, captureArtifact(), bad); yes || !errors.Is(err, ErrConflict) {
					t.Fatal("mismatched committed bundle became rollback", yes, err)
				}
				must(t, f.docs, "DELETE FROM page_thumbnails")
				if yes, err := f.store.CaptureBundleRecorded(ctx, alice, a.JobID, captureArtifact(), thumbs); yes || !errors.Is(err, ErrConflict) {
					t.Fatal("incomplete committed bundle became rollback", yes, err)
				}
			}
		})
	}
}

func TestCaptureRecordedSurvivesPageRemoval(t *testing.T) {
	f, a, stop := startedCapture(t)
	if _, err := f.store.CommitScanPage(context.Background(), alice, a.JobID, a.Revision, captureArtifact(), stop); err != nil {
		t.Fatal(err)
	}
	must(t, f.docs, "DELETE FROM pages")
	if yes, err := f.store.CaptureRecorded(context.Background(), alice, a.JobID, captureArtifact().ID); err != nil || !yes {
		t.Fatal("UI page liveness invalidated native commit evidence", yes, err)
	}
}
