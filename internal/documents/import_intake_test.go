package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func importRequest() ImportRequest {
	return ImportRequest{IdempotencyKey: "request_import_0001", InputBytes: 20, SHA256: sha256.Sum256([]byte("fixture"))}
}
func importLimits() ImportLimits {
	return ImportLimits{AdmissionLimits: admissionLimits(), MaxDocuments: 20, DocumentLifetime: 24 * time.Hour}
}
func intakeArtifact(a ImportAdmission, size int64, digest [32]byte) ArtifactRecord {
	return ArtifactRecord{ID: a.Input.ID, RelativePath: a.Input.RelativePath, MediaType: "application/pdf", Size: size, SHA256: digest}
}
func TestImportAdmissionStartSealAndReplay(t *testing.T) {
	f := scanFixture(t)
	ctx := context.Background()
	request := importRequest()
	limits := importLimits()
	a, err := f.store.AdmitImport(ctx, alice, request, limits)
	if err != nil {
		t.Fatal(err)
	}
	if a.Replay || a.ProducerID == "" || a.Input.ID == "" || a.Revision != 1 || a.DeadlineAtMS <= f.now {
		t.Fatal("missing intake authority", a)
	}
	if free, err := f.store.RecoveryDirectoryUnreferenced(ctx, strings.Split(a.Input.RelativePath, "/")[0]); err != nil || free {
		t.Fatal("live staging directory unprotected", free, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=? AND byte_size=100", a.JobID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM artifacts WHERE id=? AND state='staging' AND byte_size=0 AND sha256 IS NULL", a.Input.ID) != 1 {
		t.Fatal("intake not reserved atomically")
	}
	smaller := limits
	smaller.JobMaxBytes = 1
	replay, err := f.store.AdmitImport(ctx, alice, request, smaller)
	if err != nil || !replay.Replay || replay.JobID != a.JobID || replay.ProducerID != "" || replay.Input.ID != "" {
		t.Fatal("replay recreated producer", replay, err)
	}
	changed := request
	changed.InputBytes++
	if _, err = f.store.AdmitImport(ctx, alice, changed, limits); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("digest mismatch", err)
	}
	record := intakeArtifact(a, request.InputBytes, request.SHA256)
	if err = f.store.SealStoppedImportUpload(ctx, alice, a.JobID, a.ProducerID, record, f.now); err == nil {
		t.Fatal("unstarted intake sealed")
	}
	if err = f.store.StartImportUpload(ctx, alice, a.JobID, a.ProducerID); err != nil {
		t.Fatal(err)
	}
	if err = f.store.StartImportUpload(ctx, alice, a.JobID, a.ProducerID); err == nil {
		t.Fatal("second producer started")
	}
	f.now++
	if err = f.store.SealStoppedImportUpload(ctx, alice, a.JobID, a.ProducerID, record, f.now); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=? AND byte_size=80", a.JobID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM artifacts WHERE id=? AND state='ready' AND byte_size=20", a.Input.ID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM import_intakes WHERE job_id=? AND state='ready'", a.JobID) != 1 {
		t.Fatal("seal accounting/publication not atomic")
	}
	if state, err := f.store.ImportUploadRecorded(ctx, alice, a.JobID, a.ProducerID, record); err != nil || state != "ready" {
		t.Fatal("seal reconciliation", state, err)
	}
	if err = f.store.StartImportUpload(ctx, alice, a.JobID, a.ProducerID); err == nil {
		t.Fatal("sealed upload restarted")
	}
}
func TestImportIntakeRefusalsPreserveAuthority(t *testing.T) {
	for _, mode := range []string{"owner", "producer", "digest", "size", "path", "cancel", "expired", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			f := scanFixture(t)
			ctx := context.Background()
			request := importRequest()
			a, err := f.store.AdmitImport(ctx, alice, request, importLimits())
			if err != nil {
				t.Fatal(err)
			}
			if err = f.store.StartImportUpload(ctx, alice, a.JobID, a.ProducerID); err != nil {
				t.Fatal(err)
			}
			owner, producer := alice, a.ProducerID
			record := intakeArtifact(a, request.InputBytes, request.SHA256)
			switch mode {
			case "owner":
				owner = bob
			case "producer":
				producer = "producer_foreign_001"
			case "digest":
				record.SHA256 = sha256.Sum256([]byte("changed"))
			case "size":
				record.Size--
			case "path":
				record.RelativePath = "foreign/file"
			case "cancel":
				if err = f.store.RequestCancel(ctx, alice, a.JobID); err != nil {
					t.Fatal(err)
				}
			case "expired":
				f.now = a.DeadlineAtMS
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=? WHERE id=?", f.now, alice)
			}
			if err = f.store.SealStoppedImportUpload(ctx, owner, a.JobID, producer, record, f.now); err == nil {
				t.Fatal("invalid intake sealed")
			}
			if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=? AND byte_size=100", a.JobID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM artifacts WHERE id=? AND state='staging'", a.Input.ID) != 1 {
				t.Fatal("refusal released producer authority")
			}
		})
	}
}
func TestStoppedImportAbortReleasesReservationButRetainsActualBytes(t *testing.T) {
	for _, mode := range []string{"pending", "failed", "cancelled", "disabled", "expired"} {
		t.Run(mode, func(t *testing.T) {
			f := scanFixture(t)
			ctx := context.Background()
			a, err := f.store.AdmitImport(ctx, alice, importRequest(), importLimits())
			if err != nil {
				t.Fatal(err)
			}
			size := int64(5)
			hash := sha256.Sum256([]byte("short"))
			if mode == "pending" {
				size = 0
				hash = sha256.Sum256(nil)
			} else if err = f.store.StartImportUpload(ctx, alice, a.JobID, a.ProducerID); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "cancelled":
				if err = f.store.RequestCancel(ctx, alice, a.JobID); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=? WHERE id=?", f.now, alice)
			case "expired":
				f.now = a.DeadlineAtMS
			}
			record := intakeArtifact(a, size, hash)
			if err = f.store.FinishStoppedImportUpload(ctx, alice, a.JobID, a.ProducerID, record, f.now, "failed"); err != nil {
				t.Fatal(err)
			}
			want := "failed"
			if mode == "cancelled" {
				want = "cancelled"
			}
			if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state=?", a.JobID, want) != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != 0 || scalar(t, f.docs, "SELECT byte_size FROM artifacts WHERE id=?", a.Input.ID) != int(size) {
				t.Fatal("abort lost actual accounting")
			}
			if state, err := f.store.ImportUploadRecorded(ctx, alice, a.JobID, a.ProducerID, record); err != nil || state != "aborted" {
				t.Fatal("abort readback", state, err)
			}
		})
	}
}
func TestImportQuotaAndOwnerAdmission(t *testing.T) {
	for _, mode := range []string{"owner", "input", "quota", "documents"} {
		t.Run(mode, func(t *testing.T) {
			f := scanFixture(t)
			request := importRequest()
			limits := importLimits()
			owner := alice
			switch mode {
			case "owner":
				owner = "principal_absent_001"
			case "input":
				request.InputBytes = limits.JobMaxBytes
			case "quota":
				limits.UserMaxBytes = limits.JobMaxBytes
				seedPage(t, f, docA, alice, "original_quota_import1", "page_quota_import01")
			case "documents":
				limits.MaxDocuments = 1
			}
			if _, err := f.store.AdmitImport(context.Background(), owner, request, limits); err == nil {
				t.Fatal("invalid admission")
			}
			if scalar(t, f.docs, "SELECT count(*) FROM jobs") != 0 || scalar(t, f.docs, "SELECT count(*) FROM artifacts WHERE media_type='application/pdf'") != 0 {
				t.Fatal("partial admission")
			}
		})
	}
}

func TestImportUploadUnknownCommitCanBeReadBack(t *testing.T) {
	for _, mode := range []string{"admission", "seal", "abort"} {
		t.Run(mode, func(t *testing.T) {
			f := scanFixture(t)
			ctx := context.Background()
			request := importRequest()
			lost := func(tx *sql.Tx) error {
				if err := tx.Commit(); err != nil {
					return err
				}
				return errors.New("post-commit acknowledgement lost")
			}
			if mode == "admission" {
				f.store.commit = lost
				if a, err := f.store.AdmitImport(ctx, alice, request, importLimits()); !errors.Is(err, ErrCommitUncertain) || a.ProducerID != "" {
					t.Fatal("uncertain producer escaped", a, err)
				}
				f.store.commit = func(tx *sql.Tx) error { return tx.Commit() }
				replay, err := f.store.AdmitImport(ctx, alice, request, importLimits())
				if err != nil || !replay.Replay || replay.ProducerID != "" || scalar(t, f.docs, "SELECT count(*) FROM jobs") != 1 {
					t.Fatal("uncertain admission duplicated", err)
				}
				return
			}
			a, err := f.store.AdmitImport(ctx, alice, request, importLimits())
			if err != nil {
				t.Fatal(err)
			}
			serialized, err := json.Marshal(a)
			if err != nil || strings.Contains(string(serialized), a.ProducerID) || strings.Contains(string(serialized), a.Input.RelativePath) {
				t.Fatal("internal producer capability serialized", err)
			}
			if err = f.store.StartImportUpload(ctx, alice, a.JobID, a.ProducerID); err != nil {
				t.Fatal(err)
			}
			record := intakeArtifact(a, request.InputBytes, request.SHA256)
			f.store.commit = lost
			want := "ready"
			if mode == "seal" {
				err = f.store.SealStoppedImportUpload(ctx, alice, a.JobID, a.ProducerID, record, f.now)
			} else {
				want = "aborted"
				err = f.store.FinishStoppedImportUpload(ctx, alice, a.JobID, a.ProducerID, record, f.now, "failed")
			}
			if !errors.Is(err, ErrCommitUncertain) {
				t.Fatal("lost acknowledgement hidden", err)
			}
			f.store.commit = func(tx *sql.Tx) error { return tx.Commit() }
			if state, err := f.store.ImportUploadRecorded(ctx, alice, a.JobID, a.ProducerID, record); err != nil || state != want {
				t.Fatal("lost result not reconcilable", state, err)
			}
		})
	}
}
func TestImportDeleteWaitsForProducerToStop(t *testing.T) {
	f := scanFixture(t)
	ctx := context.Background()
	a, err := f.store.AdmitImport(ctx, alice, importRequest(), importLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.StartImportUpload(ctx, alice, a.JobID, a.ProducerID); err != nil {
		t.Fatal(err)
	}
	cleanup, err := f.store.RequestDelete(ctx, alice, DeleteRequest{DocumentID: a.DocumentID, Revision: 1, IdempotencyKey: "delete_import_00001"}, 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimCleanup(ctx, alice, cleanup.ID, runner); !errors.Is(err, ErrConflict) {
		t.Fatal("cleanup bypassed receiving producer", err)
	}
	record := intakeArtifact(a, 5, sha256.Sum256([]byte("short")))
	if err = f.store.FinishStoppedImportUpload(ctx, alice, a.JobID, a.ProducerID, record, f.now, "failed"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimCleanup(ctx, alice, cleanup.ID, runner); err != nil {
		t.Fatal("stopped intake blocked owned cleanup", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM cleanup_files WHERE job_id=? AND byte_size=5", cleanup.ID) != 1 {
		t.Fatal("cleanup lost actual partial source")
	}
}

func TestImportProducerDoesNotStartAfterClockRollback(t *testing.T) {
	f := scanFixture(t)
	ctx := context.Background()
	a, err := f.store.AdmitImport(ctx, alice, importRequest(), importLimits())
	if err != nil {
		t.Fatal(err)
	}
	f.now--
	if err = f.store.StartImportUpload(ctx, alice, a.JobID, a.ProducerID); !errors.Is(err, ErrConflict) {
		t.Fatal("producer started before job creation", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM import_intakes WHERE job_id=? AND state='pending' AND started_at_ms IS NULL", a.JobID) != 1 {
		t.Fatal("rollback changed intake authority")
	}
}
