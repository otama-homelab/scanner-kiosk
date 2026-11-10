package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

type photoReceiptReader interface {
	ReadImportReceipt(context.Context, string, ImportRequest) (Admission, error)
}

func photoStoreRequest() ImportRequest {
	return ImportRequest{MediaType: "image/jpeg", IdempotencyKey: "request_photo_recover01", InputBytes: 20, SHA256: sha256.Sum256([]byte("synthetic photo original"))}
}
func photoImported() ImportedPage {
	return ImportedPage{Orientation: 6, Artifact: ArtifactRecord{ID: "normalized_photo_001", RelativePath: "work_photo_result1/normalized.png", MediaType: "image/png", Size: 30, SHA256: sha256.Sum256([]byte("synthetic normalized photo")), PixelWidth: 3, PixelHeight: 2}}
}
func sealPhoto(t *testing.T, f *fixture, a ImportAdmission, request ImportRequest) {
	t.Helper()
	if err := f.store.StartImportUpload(context.Background(), alice, a.JobID, a.ProducerID); err != nil {
		t.Fatal(err)
	}
	record := a.Input
	record.Size = request.InputBytes
	record.SHA256 = request.SHA256
	if err := f.store.SealStoppedImportUpload(context.Background(), alice, a.JobID, a.ProducerID, record, f.now); err != nil {
		t.Fatal(err)
	}
}

func TestPhotoImportRecoveryMatrix(t *testing.T) {
	f := scanFixture(t)
	reader, ok := any(f.store).(photoReceiptReader)
	if !ok {
		t.Fatal("photo unknown-outcome recovery needs a read-only receipt lookup; admission must not be called twice")
	}
	request := photoStoreRequest()
	ctx := context.Background()
	if _, err := reader.ReadImportReceipt(ctx, alice, request); !errors.Is(err, ErrUnavailable) {
		t.Fatal("unknown receipt admitted work", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs") != 0 {
		t.Fatal("lookup wrote a job")
	}
	for _, mode := range []string{"concurrent", "admission-commit-lost", "admission-rollback", "producer-seal-commit-lost", "publication-commit-lost", "publication-rollback"} {
		t.Run(mode, func(t *testing.T) {
			f := scanFixture(t)
			reader := any(f.store).(photoReceiptReader)
			request := photoStoreRequest()
			var a ImportAdmission
			var err error
			committed := mode != "admission-rollback" && mode != "publication-rollback"
			lost := func(tx *sql.Tx) error {
				if committed {
					if err := tx.Commit(); err != nil {
						return err
					}
				}
				return errors.New("lost commit acknowledgement")
			}
			if mode == "admission-commit-lost" || mode == "admission-rollback" {
				f.store.commit = lost
			}
			a, err = f.store.AdmitImport(ctx, alice, request, importLimits())
			if mode == "admission-commit-lost" || mode == "admission-rollback" {
				if !errors.Is(err, ErrCommitUncertain) || a.ProducerID != "" {
					t.Fatal("unknown admission leaked producer", a, err)
				}
				f.store.commit = func(tx *sql.Tx) error { return tx.Commit() }
				receipt, err := reader.ReadImportReceipt(ctx, alice, request)
				if !committed {
					if !errors.Is(err, ErrUnavailable) || scalar(t, f.docs, "SELECT count(*) FROM jobs") != 0 {
						t.Fatal("rollback fabricated receipt", err)
					}
					return
				}
				if err != nil || !receipt.Replay || scalar(t, f.docs, "SELECT count(*) FROM jobs") != 1 {
					t.Fatal("lost admission duplicated", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "concurrent" {
				var wg sync.WaitGroup
				fail := make(chan error, 8)
				for range 8 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						replay, err := f.store.AdmitImport(ctx, alice, request, importLimits())
						if err != nil {
							fail <- err
							return
						}
						if !replay.Replay || replay.JobID != a.JobID || replay.ProducerID != "" {
							fail <- ErrConflict
						}
					}()
				}
				wg.Wait()
				close(fail)
				for err := range fail {
					t.Fatal(err)
				}
			}
			if mode == "producer-seal-commit-lost" {
				if err = f.store.StartImportUpload(ctx, alice, a.JobID, a.ProducerID); err != nil {
					t.Fatal(err)
				}
				record := a.Input
				record.Size = request.InputBytes
				record.SHA256 = request.SHA256
				f.store.commit = lost
				err = f.store.SealStoppedImportUpload(ctx, alice, a.JobID, a.ProducerID, record, f.now)
				if !errors.Is(err, ErrCommitUncertain) {
					t.Fatal(err)
				}
				f.store.commit = func(tx *sql.Tx) error { return tx.Commit() }
				state, err := f.store.ImportUploadRecorded(ctx, alice, a.JobID, a.ProducerID, record)
				if err != nil || state != "ready" {
					t.Fatal("seal not reconciled", state, err)
				}
			} else {
				sealPhoto(t, f, a, request)
			}
			claim, err := f.store.ClaimNextImport(ctx, "worker_photo_recovery1")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "publication-commit-lost" || mode == "publication-rollback" {
				f.store.commit = lost
			}
			pages := []ImportedPage{photoImported()}
			stop := ImportStop{RunnerID: "worker_photo_recovery1", ProcessStoppedAtMS: f.now, WorkspaceCleanedAtMS: f.now}
			err = f.store.CommitImportPages(ctx, alice, claim.Job.ID, stop, pages)
			if mode == "publication-commit-lost" || mode == "publication-rollback" {
				if !errors.Is(err, ErrCommitUncertain) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			f.store.commit = func(tx *sql.Tx) error { return tx.Commit() }
			recorded, err := f.store.ImportPagesRecorded(ctx, alice, a.JobID, stop.RunnerID, pages)
			if err != nil || recorded != committed {
				t.Fatal("publication readback", recorded, err)
			}
			if !committed {
				if scalar(t, f.docs, "SELECT count(*) FROM photo_sources") != 0 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 1 {
					t.Fatal("rollback lost reservation")
				}
				if err = f.store.CommitImportPages(ctx, alice, a.JobID, stop, pages); err != nil {
					t.Fatal("same frozen publication retry", err)
				}
			}
			for query, want := range map[string]int{"SELECT count(*) FROM import_intakes": 1, "SELECT count(*) FROM request_receipts": 1, "SELECT count(*) FROM photo_sources": 1, "SELECT count(*) FROM pages": 1, "SELECT count(*) FROM artifacts": 2, "SELECT count(*) FROM jobs": 1, "SELECT count(*) FROM reservations": 0} {
				if n := scalar(t, f.docs, query); n != want {
					t.Fatal("duplicate or leaked photo graph", query, n, want)
				}
			}
			receipt, err := reader.ReadImportReceipt(ctx, alice, request)
			if err != nil || receipt.JobID != a.JobID {
				t.Fatal("read receipt after publication", err)
			}
		})
	}
}

func TestPhotoRoutesOwnerRevisionTTL(t *testing.T) {
	f := scanFixture(t)
	reader, ok := any(f.store).(photoReceiptReader)
	if !ok {
		t.Fatal("owner/TTL receipt reader absent")
	}
	ctx := context.Background()
	request := photoStoreRequest()
	a, err := f.store.AdmitImport(ctx, alice, request, importLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"hash", "size", "media", "key", "owner"} {
		t.Run(mode, func(t *testing.T) {
			changed := request
			owner := alice
			want := ErrIdempotencyConflict
			switch mode {
			case "hash":
				changed.SHA256[0] ^= 1
			case "size":
				changed.InputBytes++
			case "media":
				changed.MediaType = "image/png"
			case "key":
				changed.IdempotencyKey = "request_unknown_photo01"
				want = ErrUnavailable
			case "owner":
				owner = bob
				want = ErrUnavailable
			}
			if _, err := reader.ReadImportReceipt(ctx, owner, changed); !errors.Is(err, want) {
				t.Fatal("identity boundary", mode, err)
			}
		})
	}
	sealPhoto(t, f, a, request)
	claim, err := f.store.ClaimNextImport(ctx, "worker_photo_ttl0001")
	if err != nil {
		t.Fatal(err)
	}
	stop := ImportStop{RunnerID: "worker_photo_ttl0001", ProcessStoppedAtMS: f.now, WorkspaceCleanedAtMS: f.now}
	if err = f.store.CommitImportPages(ctx, alice, claim.Job.ID, stop, []ImportedPage{photoImported()}); err != nil {
		t.Fatal(err)
	}
	p, err := f.store.ReadDocument(ctx, alice, a.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	page := p.Pages[0].ID
	for _, owner := range []string{alice, bob} {
		_, err := f.store.ReadPageEffective(ctx, owner, a.DocumentID, page)
		if (owner == alice) != (err == nil) {
			t.Fatal("effective owner boundary", owner, err)
		}
	}
	_, err = f.store.AdmitRender(ctx, alice, RenderAdmission{DocumentID: a.DocumentID, Revision: 2, IdempotencyKey: "request_photo_stale001", Settings: RenderSettings{Format: "pdf", PDFEncoding: "lossless"}, ReserveBytes: 50}, admissionLimits())
	if !errors.Is(err, ErrRevision) {
		t.Fatal("stale revision accepted", err)
	}
	f.now += int64((25 * time.Hour) / time.Millisecond)
	if _, err = reader.ReadImportReceipt(ctx, alice, request); err == nil {
		t.Fatal("expired receipt read")
	}
	if _, err = f.store.ReadPageEffective(ctx, alice, a.DocumentID, page); err == nil {
		t.Fatal("expired effective read")
	}
	if _, err = f.store.ReadPageOriginal(ctx, alice, a.DocumentID, page); err == nil {
		t.Fatal("expired raw read")
	}
}
