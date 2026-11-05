package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"
)

func readyImport(t *testing.T) (*fixture, ImportAdmission) {
	t.Helper()
	f := scanFixture(t)
	ctx := context.Background()
	request := importRequest()
	a, err := f.store.AdmitImport(ctx, alice, request, importLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimNextImport(ctx, runner); !errors.Is(err, ErrNoWork) {
		t.Fatal("unreceived input claim", err)
	}
	if err = f.store.StartImportUpload(ctx, alice, a.JobID, a.ProducerID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimNextImport(ctx, runner); !errors.Is(err, ErrNoWork) {
		t.Fatal("live producer claimed", err)
	}
	if err = f.store.SealStoppedImportUpload(ctx, alice, a.JobID, a.ProducerID, intakeArtifact(a, request.InputBytes, request.SHA256), f.now); err != nil {
		t.Fatal(err)
	}
	return f, a
}
func claimedImport(t *testing.T) (*fixture, ImportAdmission, ImportStop) {
	t.Helper()
	f, a := readyImport(t)
	ctx := context.Background()
	f.now += 10
	claimed, err := f.store.ClaimNextImport(ctx, runner)
	if err != nil || claimed.OwnerID != alice || claimed.Job.ID != a.JobID || claimed.Job.Kind != "import" {
		t.Fatal("ready import claim", claimed, err)
	}
	if _, err = f.store.ClaimNextImport(ctx, "worker_competing_01"); !errors.Is(err, ErrNoWork) {
		t.Fatal("duplicate import claim", err)
	}
	work, err := f.store.LoadImportWork(ctx, alice, a.JobID, runner)
	if err != nil || work.Input.ID != a.Input.ID || work.ReserveBytes != 80 || work.Snapshot.InputBytes != 20 {
		t.Fatal("import snapshot", work, err)
	}
	f.now += 100
	return f, a, ImportStop{RunnerID: runner, ProcessStoppedAtMS: f.now - 20, WorkspaceCleanedAtMS: f.now - 10}
}
func importPages() []ImportedPage {
	return []ImportedPage{
		{Artifact: ArtifactRecord{ID: "artifact_import_page1", RelativePath: "work_import/page1", MediaType: "application/pdf", Size: 3, SHA256: sha256.Sum256([]byte("one"))}, WidthPoints: 200, HeightPoints: 300},
		{Artifact: ArtifactRecord{ID: "artifact_import_page2", RelativePath: "work_import/page2", MediaType: "application/pdf", Size: 3, SHA256: sha256.Sum256([]byte("two"))}, WidthPoints: 400, HeightPoints: 250},
	}
}
func TestImportPagesPublishAtomicallyAndPreserveSource(t *testing.T) {
	f, a, stop := claimedImport(t)
	pages := importPages()
	ctx := context.Background()
	if err := f.store.CommitImportPages(ctx, alice, a.JobID, stop, pages); err != nil {
		t.Fatal(err)
	}
	projection, err := f.store.ReadDocument(ctx, alice, a.DocumentID)
	if err != nil || projection.State != "review" || projection.Revision != 1 || len(projection.Pages) != 2 {
		t.Fatal("import projection", projection, err)
	}
	for i, p := range projection.Pages {
		if p.Ordinal != i || p.SourceType != "pdf" || p.Capture != nil || p.PDF == nil || p.PDF.WidthPoints != pages[i].WidthPoints {
			t.Fatal("PDF geometry/order", p)
		}
	}
	if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != 0 || scalar(t, f.docs, "SELECT count(*) FROM job_artifacts WHERE job_id=?", a.JobID) != 3 || scalar(t, f.docs, "SELECT count(*) FROM artifacts WHERE id=? AND state='ready' AND byte_size=20", a.Input.ID) != 1 {
		t.Fatal("source/accounting lost")
	}
	if recorded, err := f.store.ImportPagesRecorded(ctx, alice, a.JobID, runner, pages); err != nil || !recorded {
		t.Fatal("publication readback", recorded, err)
	}
	wrong := append([]ImportedPage(nil), pages...)
	wrong[1].Artifact = intakeArtifact(a, 20, importRequest().SHA256)
	if recorded, err := f.store.ImportPagesRecorded(ctx, alice, a.JobID, runner, wrong); err != nil || recorded {
		t.Fatal("raw input mistaken for a published page", recorded, err)
	}
}
func TestImportPagePublicationRefusalsKeepReservationAndSource(t *testing.T) {
	for _, mode := range []string{"owner", "runner", "cancel", "expired", "geometry", "budget", "storage-rollback"} {
		t.Run(mode, func(t *testing.T) {
			f, a, stop := claimedImport(t)
			pages := importPages()
			who := alice
			switch mode {
			case "owner":
				who = bob
			case "runner":
				stop.RunnerID = "worker_foreign_0001"
			case "cancel":
				if err := f.store.RequestCancel(context.Background(), alice, a.JobID); err != nil {
					t.Fatal(err)
				}
			case "expired":
				f.now = a.DeadlineAtMS
			case "geometry":
				pages[1].WidthPoints = 0
			case "budget":
				pages[1].Artifact.Size = 100
			case "storage-rollback":
				seedPage(t, f, docB, bob, "artifact_import_collision", "page_import_collision")
				pages[1].Artifact.ID = "artifact_import_collision"
			}
			if err := f.store.CommitImportPages(context.Background(), who, a.JobID, stop, pages); err == nil {
				t.Fatal("invalid page publication")
			}
			if scalar(t, f.docs, "SELECT count(*) FROM pages WHERE document_id=?", a.DocumentID) != 0 || scalar(t, f.docs, "SELECT count(*) FROM job_artifacts WHERE job_id=?", a.JobID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=? AND byte_size=80", a.JobID) != 1 {
				t.Fatal("partial publication or released reservation")
			}
		})
	}
}
func TestImportStoppedFailureAndLostPublicationAck(t *testing.T) {
	t.Run("stopped", func(t *testing.T) {
		f, a, stop := claimedImport(t)
		if err := f.store.FinishStoppedImport(context.Background(), alice, a.JobID, stop, "failed"); err != nil {
			t.Fatal(err)
		}
		if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != 0 || scalar(t, f.docs, "SELECT count(*) FROM artifacts WHERE id=? AND state='ready'", a.Input.ID) != 1 {
			t.Fatal("stopped import lost source")
		}
	})
	t.Run("lost-ack", func(t *testing.T) {
		f, a, stop := claimedImport(t)
		pages := importPages()
		f.store.commit = func(tx *sql.Tx) error {
			if err := tx.Commit(); err != nil {
				return err
			}
			return errors.New("lost acknowledgement")
		}
		if err := f.store.CommitImportPages(context.Background(), alice, a.JobID, stop, pages); !errors.Is(err, ErrCommitUncertain) {
			t.Fatal(err)
		}
		if recorded, err := f.store.ImportPagesRecorded(context.Background(), alice, a.JobID, runner, pages); err != nil || !recorded {
			t.Fatal("lost publication unreconciled", err)
		}
	})
}

func TestRunningImportBlocksOtherHeavyWorkAndInvalidStop(t *testing.T) {
	f, a, stop := claimedImport(t)
	ctx := context.Background()
	if recorded, err := f.store.ImportStoppedRecorded(ctx, alice, a.JobID, runner); err != nil || recorded {
		t.Fatal("running import claimed stopped", recorded, err)
	}
	must(t, f.docs, "UPDATE documents SET state='review' WHERE id=?", docA)
	seedPage(t, f, docA, alice, "original_render_other1", "page_render_other01")
	if _, err := f.store.AdmitRender(ctx, alice, admissionRequest(), admissionLimits()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ClaimNextRender(ctx, "worker_other_render1"); !errors.Is(err, ErrNoWork) {
		t.Fatal("render ran beside import", err)
	}
	bad := stop
	bad.WorkspaceCleanedAtMS = bad.ProcessStoppedAtMS - 1
	if err := f.store.FinishStoppedImport(ctx, alice, a.JobID, bad, "failed"); !errors.Is(err, ErrConflict) {
		t.Fatal("unconfirmed cleanup released import", err)
	}
	if err := f.store.RequestCancel(ctx, alice, a.JobID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.PulseImport(ctx, alice, a.JobID, runner); err == nil {
		t.Fatal("cancelled import remained authorized")
	}
	if err := f.store.FinishStoppedImport(ctx, alice, a.JobID, stop, "failed"); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='cancelled'", a.JobID) != 1 {
		t.Fatal("cancellation not terminalized")
	}
	if recorded, err := f.store.ImportStoppedRecorded(ctx, alice, a.JobID, runner); err != nil || !recorded {
		t.Fatal("stop acknowledgement not recorded", recorded, err)
	}
	if recorded, err := f.store.ImportStoppedRecorded(ctx, bob, a.JobID, runner); err != nil || recorded {
		t.Fatal("wrong owner accepted", recorded, err)
	}
	if recorded, err := f.store.ImportStoppedRecorded(ctx, alice, a.JobID, "worker_different01"); err != nil || recorded {
		t.Fatal("wrong runner accepted", recorded, err)
	}
}
