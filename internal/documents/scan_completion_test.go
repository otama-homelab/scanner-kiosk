package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func captureArtifact() Export {
	return Export{ID: "captured_original_01", RelativePath: "work_fixture/original_01", MediaType: "image/png", Size: 3, SHA256: sha256.Sum256([]byte("png"))}
}
func startedCapture(t *testing.T) (*fixture, ScanAdmission, ScanStop) {
	t.Helper()
	f := scanFixture(t)
	a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimScan(context.Background(), alice, a.JobID, runner); err != nil {
		t.Fatal(err)
	}
	f.now = 2300
	return f, a, ScanStop{DeviceID: scanner, WorkerBootID: runner, LeaseEpoch: a.LeaseEpoch, ProcessStoppedAtMS: 2100, IdleObservedAtMS: 2200}
}
func TestCaptureCompletionPublishesPageAndReleasesAtomically(t *testing.T) {
	f, a, stop := startedCapture(t)
	page, err := f.store.CommitScanPage(context.Background(), alice, a.JobID, a.Revision, captureArtifact(), stop)
	if err != nil {
		t.Fatal(err)
	}
	if page.ID == "" || page.Ordinal != 0 || page.Capture.DPI != 300 || page.OriginalArtifactID != captureArtifact().ID {
		t.Fatal("incorrect captured page")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM artifacts WHERE kind='original' AND state='ready'") != 1 || scalar(t, f.docs, "SELECT count(*) FROM pages") != 1 || scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='succeeded'", a.JobID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='idle' AND epoch=2 AND job_id IS NULL") != 1 {
		t.Fatal("capture completion not atomic")
	}
	d, err := f.store.Get(context.Background(), alice, a.DocumentID)
	if err != nil || d.State != "review" || d.Revision != 1 || d.CreatedAtMS != 2000 {
		t.Fatal("capture completion changed identity/TTL")
	}
	recorded, err := f.store.CaptureRecorded(context.Background(), alice, a.JobID, captureArtifact().ID)
	if err != nil || !recorded {
		t.Fatal("capture readback missing")
	}
}
func TestCapturePublicationRejectsStaleOrUnconfirmedResults(t *testing.T) {
	for _, mode := range []string{"owner", "revision", "boot", "epoch", "process", "idle", "future-idle", "cancel", "deadline", "disabled", "budget", "media"} {
		t.Run(mode, func(t *testing.T) {
			f, a, stop := startedCapture(t)
			owner := alice
			revision := a.Revision
			artifact := captureArtifact()
			switch mode {
			case "owner":
				owner = bob
			case "revision":
				revision++
			case "boot":
				stop.WorkerBootID = "other_worker_000001"
			case "epoch":
				stop.LeaseEpoch++
			case "process":
				stop.ProcessStoppedAtMS = 0
			case "idle":
				stop.IdleObservedAtMS = 1999
			case "future-idle":
				stop.IdleObservedAtMS = 2500
			case "cancel":
				if err := f.store.RequestCancel(context.Background(), alice, a.JobID); err != nil {
					t.Fatal(err)
				}
			case "deadline":
				f.now = 2000 + time.Minute.Milliseconds()
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2300 WHERE id=?", alice)
			case "budget":
				artifact.Size = 51
			case "media":
				artifact.MediaType = "application/pdf"
			}
			if _, err := f.store.CommitScanPage(context.Background(), owner, a.JobID, revision, artifact, stop); err == nil {
				t.Fatal("invalid capture published")
			}
			if scalar(t, f.docs, "SELECT count(*) FROM pages") != 0 || scalar(t, f.docs, "SELECT count(*) FROM artifacts") != 0 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 1 || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='held' AND epoch=2") != 1 {
				t.Fatal("rejected capture lost reservation/lease or partially published")
			}
		})
	}
}
func TestTwoCapturedPagesRetainIndependentSettingsAndInitialExpiry(t *testing.T) {
	f, a, stop := startedCapture(t)
	if _, err := f.store.CommitScanPage(context.Background(), alice, a.JobID, a.Revision, captureArtifact(), stop); err != nil {
		t.Fatal(err)
	}
	request := scanRequest()
	request.DocumentID = a.DocumentID
	request.Revision = a.Revision
	request.Purpose = ""
	request.IdempotencyKey = "request_scan_00002"
	request.Capture = Capture{Size: "a5", Color: "gray", DPI: 600}
	next, err := f.store.AdmitScan(context.Background(), alice, request, scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimScan(context.Background(), alice, next.JobID, runner); err != nil {
		t.Fatal(err)
	}
	artifact := captureArtifact()
	artifact.ID = "captured_original_02"
	artifact.RelativePath = "work_fixture/original_02"
	f.now = 2600
	proof := ScanStop{DeviceID: scanner, WorkerBootID: runner, LeaseEpoch: next.LeaseEpoch, ProcessStoppedAtMS: 2400, IdleObservedAtMS: 2500}
	page, err := f.store.CommitScanPage(context.Background(), alice, next.JobID, next.Revision, artifact, proof)
	if err != nil || page.Ordinal != 1 || page.Capture.DPI != 600 || page.Capture.Color != "gray" {
		t.Fatalf("second page incorrect: %v", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM pages WHERE capture_dpi=300 AND capture_color='color'") != 1 || scalar(t, f.docs, "SELECT count(*) FROM pages WHERE capture_dpi=600 AND capture_color='gray'") != 1 {
		t.Fatal("capture settings overwritten")
	}
	d, err := f.store.Get(context.Background(), alice, a.DocumentID)
	if err != nil || d.CreatedAtMS != 2000 || d.ExpiresAtMS != 2000+(24*time.Hour).Milliseconds() {
		t.Fatal("next page extended retention")
	}
	if _, err = f.store.CommitScanPage(context.Background(), alice, a.JobID, a.Revision, captureArtifact(), stop); !errors.Is(err, ErrConflict) {
		t.Fatal("old capture completed twice")
	}
}
func TestStoppedCleanupRequiresFreshProofAndWorkspaceCleanup(t *testing.T) {
	for _, mode := range []string{"missing-process", "stale-idle", "missing-cleanup", "wrong-epoch", "success"} {
		t.Run(mode, func(t *testing.T) {
			f, a, stop := startedCapture(t)
			cleaned := int64(2250)
			switch mode {
			case "missing-process":
				stop.ProcessStoppedAtMS = 0
			case "stale-idle":
				stop.IdleObservedAtMS = 1000
			case "missing-cleanup":
				cleaned = 0
			case "wrong-epoch":
				stop.LeaseEpoch++
			}
			err := f.store.FinishStoppedScan(context.Background(), alice, a.JobID, stop, cleaned, "failed")
			if mode != "success" {
				if err == nil || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='held'") != 1 {
					t.Fatal("unproven stop unlocked device")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			recorded, err := f.store.ScanStoppedRecorded(context.Background(), alice, a.JobID)
			if err != nil || !recorded || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 {
				t.Fatal("confirmed cleanup not recorded")
			}
		})
	}
}
func TestCancelledDisabledAndExpiredScansCanFinishInternalCleanup(t *testing.T) {
	for _, mode := range []string{"cancel", "disabled", "expired"} {
		t.Run(mode, func(t *testing.T) {
			f, a, stop := startedCapture(t)
			terminal := "failed"
			switch mode {
			case "cancel":
				if err := f.store.RequestCancel(context.Background(), alice, a.JobID); err != nil {
					t.Fatal(err)
				}
				terminal = "cancelled"
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2300 WHERE id=?", alice)
			case "expired":
				f.now = 2000 + (24 * time.Hour).Milliseconds()
			}
			if err := f.store.FinishStoppedScan(context.Background(), alice, a.JobID, stop, 2250, "failed"); err != nil {
				t.Fatal(err)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state=?", a.JobID, terminal) != 1 || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='idle'") != 1 {
				t.Fatal("terminal cleanup did not release resources")
			}
			if mode == "expired" && scalar(t, f.docs, "SELECT count(*) FROM documents WHERE id=? AND state='expired'", a.DocumentID) != 1 {
				t.Fatal("expired content revived")
			}
		})
	}
}
func TestCaptureCommitUncertaintyReadback(t *testing.T) {
	f, a, stop := startedCapture(t)
	f.store.commit = func(tx *sql.Tx) error {
		if err := tx.Commit(); err != nil {
			return err
		}
		return errors.New("injected post-commit failure")
	}
	if _, err := f.store.CommitScanPage(context.Background(), alice, a.JobID, a.Revision, captureArtifact(), stop); !errors.Is(err, ErrCommitUncertain) {
		t.Fatal("uncertain capture commit lost")
	}
	recorded, err := f.store.CaptureRecorded(context.Background(), alice, a.JobID, captureArtifact().ID)
	if err != nil || !recorded {
		t.Fatal("committed capture not reconciled")
	}
}

func TestHeartbeatAfterStopDoesNotInvalidateEvidence(t *testing.T) {
	f, a, stop := startedCapture(t)
	must(t, f.docs, "UPDATE jobs SET heartbeat_at_ms=2290 WHERE id=?", a.JobID)
	if _, err := f.store.CommitScanPage(context.Background(), alice, a.JobID, a.Revision, captureArtifact(), stop); err != nil {
		t.Fatalf("mutable heartbeat invalidated real stop interval: %v", err)
	}
}
func TestStartedScanCannotBeRequeuedOrItsStartTimeRewritten(t *testing.T) {
	f, a, _ := startedCapture(t)
	must(t, f.docs, "UPDATE jobs SET state='queued' WHERE id=?", a.JobID)
	if _, err := f.store.ClaimScan(context.Background(), alice, a.JobID, runner); !errors.Is(err, ErrConflict) {
		t.Fatal("started physical job executed twice")
	}
	if _, err := f.docs.Exec("UPDATE jobs SET started_at_ms=NULL WHERE id=?", a.JobID); err == nil {
		t.Fatal("start identity erased")
	}
	if _, err := f.docs.Exec("UPDATE jobs SET started_at_ms=2200 WHERE id=?", a.JobID); err == nil {
		t.Fatal("start identity rewritten")
	}
}

func TestCancelledAdditionalPagePreservesOriginalAndOldStopCannotUnlockNextJob(t *testing.T) {
	f, a, stop := startedCapture(t)
	if _, err := f.store.CommitScanPage(context.Background(), alice, a.JobID, a.Revision, captureArtifact(), stop); err != nil {
		t.Fatal(err)
	}
	request := scanRequest()
	request.DocumentID = a.DocumentID
	request.Revision = a.Revision
	request.Purpose = ""
	request.IdempotencyKey = "request_scan_00002"
	second, err := f.store.AdmitScan(context.Background(), alice, request, scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimScan(context.Background(), alice, second.JobID, runner); err != nil {
		t.Fatal(err)
	}
	if err = f.store.RequestCancel(context.Background(), alice, second.JobID); err != nil {
		t.Fatal(err)
	}
	f.now = 2600
	proof := ScanStop{DeviceID: scanner, WorkerBootID: runner, LeaseEpoch: second.LeaseEpoch, ProcessStoppedAtMS: 2400, IdleObservedAtMS: 2500}
	if err = f.store.FinishStoppedScan(context.Background(), alice, second.JobID, proof, 2550, "interrupted"); err != nil {
		t.Fatal(err)
	}
	d, err := f.store.Get(context.Background(), alice, a.DocumentID)
	if err != nil || d.State != "review" || scalar(t, f.docs, "SELECT count(*) FROM pages WHERE document_id=?", a.DocumentID) != 1 {
		t.Fatal("cancelled page damaged existing content")
	}
	request.Revision = d.Revision
	request.IdempotencyKey = "request_scan_00003"
	third, err := f.store.AdmitScan(context.Background(), alice, request, scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.FinishStoppedScan(context.Background(), alice, second.JobID, proof, 2550, "interrupted"); !errors.Is(err, ErrConflict) {
		t.Fatal("old stop replay accepted")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=? AND epoch=? AND state='held'", third.JobID, third.LeaseEpoch) != 1 {
		t.Fatal("stale cleanup unlocked another capture")
	}
	recorded, err := f.store.ScanStoppedRecorded(context.Background(), alice, second.JobID)
	if err != nil || !recorded {
		t.Fatal("old cleanup readback lost")
	}
}

// Regression for the manual-batch loss reported in scanservjs issue #766.
// Stop/idle evidence is a repository fixture, not physical scanner acceptance.
func TestFailedTwentyFirstScanPreservesPublishedPagesAndAllowsNextPage(t *testing.T) {
	ctx := context.Background()
	f := scanFixture(t)
	request := scanRequest()
	var document string
	start := func(index int) ScanAdmission {
		t.Helper()
		request.IdempotencyKey = fmt.Sprintf("request_batch_%04d", index)
		if document != "" {
			current, err := f.store.Get(ctx, alice, document)
			if err != nil {
				t.Fatal(err)
			}
			request.DocumentID, request.Revision, request.Purpose = document, current.Revision, ""
		}
		a, err := f.store.AdmitScan(ctx, alice, request, scanLimits())
		if err != nil {
			t.Fatal(err)
		}
		document = a.DocumentID
		if _, err = f.store.ClaimScan(ctx, alice, a.JobID, runner); err != nil {
			t.Fatal(err)
		}
		return a
	}
	stop := func(a ScanAdmission) ScanStop {
		f.now += 300
		return ScanStop{DeviceID: scanner, WorkerBootID: runner, LeaseEpoch: a.LeaseEpoch, ProcessStoppedAtMS: f.now - 200, IdleObservedAtMS: f.now - 100}
	}
	for index := 1; index <= 20; index++ {
		a := start(index)
		artifact := captureArtifact()
		artifact.ID = fmt.Sprintf("batch_original_%04d", index)
		artifact.RelativePath = fmt.Sprintf("batch_fixture/original_%04d", index)
		artifact.SHA256 = sha256.Sum256([]byte(artifact.ID))
		if _, err := f.store.CommitScanPage(ctx, alice, a.JobID, a.Revision, artifact, stop(a)); err != nil {
			t.Fatal(err)
		}
	}
	before, err := f.store.ReadDocument(ctx, alice, document)
	if err != nil || len(before.Pages) != 20 {
		t.Fatal("initial batch not published", err)
	}
	failed := start(21)
	proof := stop(failed)
	if err = f.store.FinishStoppedScan(ctx, alice, failed.JobID, proof, f.now-50, "failed"); err != nil {
		t.Fatal(err)
	}
	after, err := f.store.ReadDocument(ctx, alice, document)
	if err != nil || after.State != "review" || after.ActiveJobID != nil || !reflect.DeepEqual(before.Pages, after.Pages) || before.CreatedAt != after.CreatedAt || before.ExpiresAt != after.ExpiresAt {
		t.Fatal("failed additional page lost the batch or changed retention", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM artifacts WHERE document_id=? AND state='ready'", document) != 20 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 {
		t.Fatal("failed page damaged originals or leaked its reservation")
	}
	if _, err = f.store.ReadDocument(ctx, bob, document); !errors.Is(err, ErrUnavailable) {
		t.Fatal("recovered batch became visible to another owner")
	}
	next := start(22)
	if scalar(t, f.docs, "SELECT count(*) FROM pages WHERE document_id=?", document) != 20 || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=? AND state='held'", next.JobID) != 1 {
		t.Fatal("next page could not resume the same retained document")
	}
}
func TestStoppedCommitUncertaintyIsMetadataOnlyReadBack(t *testing.T) {
	f, a, stop := startedCapture(t)
	f.store.commit = func(tx *sql.Tx) error {
		if err := tx.Commit(); err != nil {
			return err
		}
		return errors.New("injected post-commit failure")
	}
	if err := f.store.FinishStoppedScan(context.Background(), alice, a.JobID, stop, 2250, "failed"); !errors.Is(err, ErrCommitUncertain) {
		t.Fatal("stop uncertainty hidden")
	}
	recorded, err := f.store.ScanStoppedRecorded(context.Background(), alice, a.JobID)
	if err != nil || !recorded {
		t.Fatal("stop readback failed")
	}
}
