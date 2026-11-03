package documents

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

func deleteRequest() DeleteRequest {
	return DeleteRequest{DocumentID: docA, Revision: 1, IdempotencyKey: "request_delete_document"}
}
func TestDeleteRevokesContentPreservesResourcesAndReplays(t *testing.T) {
	f := thumbnailFixture(t)
	ctx := context.Background()
	if err := f.store.PublishExportWithThumbnails(ctx, alice, jobA, runner, 1, output(), []ThumbnailRecord{thumbnailRecord()}); err != nil {
		t.Fatal(err)
	}
	result, err := f.store.RequestDelete(ctx, alice, deleteRequest(), time.Hour)
	if err != nil || result.Kind != "cleanup" || result.State != "queued" {
		t.Fatal(result, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM artifacts") != 3 || scalar(t, f.docs, "SELECT count(*) FROM published_outputs") != 1 {
		t.Fatal("delete intent removed retained resources")
	}
	for _, read := range []func() error{func() error { _, err := f.store.ReadDocument(ctx, alice, docA); return err }, func() error { _, err := f.store.ReadPageOriginal(ctx, alice, docA, "page_alice_000001"); return err }, func() error { _, err := f.store.ReadPageThumbnail(ctx, alice, docA, "page_alice_000001"); return err }, func() error { _, err := f.store.ReadCurrentOutput(ctx, alice, docA); return err }} {
		if err = read(); !errors.Is(err, ErrUnavailable) {
			t.Fatal("deleted content exposed", err)
		}
	}
	repeat, err := f.store.RequestDelete(ctx, alice, deleteRequest(), time.Hour)
	if err != nil || repeat != result {
		t.Fatal("replay", repeat, err)
	}
	another := deleteRequest()
	another.IdempotencyKey = "request_delete_again"
	repeat, err = f.store.RequestDelete(ctx, alice, another, time.Hour)
	if err != nil || repeat.ID != result.ID {
		t.Fatal("duplicate intent created new job", repeat, err)
	}
	f.now = 10000
	repeat, err = f.store.RequestDelete(ctx, alice, deleteRequest(), time.Hour)
	if err != nil || repeat.ID != result.ID {
		t.Fatal("retained delete receipt after content TTL", repeat, err)
	}
	f.now = 3602000
	if _, err = f.store.RequestDelete(ctx, alice, deleteRequest(), time.Hour); !errors.Is(err, ErrGone) {
		t.Fatal("expired receipt retried intent", err)
	}
}
func TestDeleteRejectsInvalidOrConflictingRequests(t *testing.T) {
	for _, mode := range []string{"owner", "revision", "expired", "disabled", "key-conflict", "late-clock", "commit-rollback", "committed-error"} {
		t.Run(mode, func(t *testing.T) {
			f := thumbnailFixture(t)
			ctx := context.Background()
			owner := alice
			request := deleteRequest()
			expected := ErrUnavailable
			switch mode {
			case "owner":
				owner = bob
			case "revision":
				request.Revision = 2
				expected = ErrRevision
			case "expired":
				f.now = 10000
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
			case "key-conflict":
				if _, err := f.store.CancelJob(ctx, alice, jobA, request.IdempotencyKey, time.Hour); err != nil {
					t.Fatal(err)
				}
				expected = ErrIdempotencyConflict
			case "late-clock":
				calls := 0
				f.store.now = func() time.Time {
					calls++
					if calls >= 2 {
						return time.UnixMilli(10000)
					}
					return time.UnixMilli(2000)
				}
			case "commit-rollback":
				f.store.commit = func(*sql.Tx) error { return errors.New("lost commit") }
				expected = ErrCommitUncertain
			case "committed-error":
				f.store.commit = func(tx *sql.Tx) error {
					if err := tx.Commit(); err != nil {
						return err
					}
					return errors.New("lost acknowledgement")
				}
				expected = ErrCommitUncertain
			}
			if _, err := f.store.RequestDelete(ctx, owner, request, time.Hour); !errors.Is(err, expected) {
				t.Fatal(mode, err)
			}
			count := scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind='cleanup'")
			if mode == "committed-error" {
				if count != 1 {
					t.Fatal("commit not recorded")
				}
				f.store.commit = func(tx *sql.Tx) error { return tx.Commit() }
				if _, err := f.store.RequestDelete(ctx, owner, request, time.Hour); err != nil {
					t.Fatal("uncertain replay failed", err)
				}
			} else if count != 0 || scalar(t, f.docs, "SELECT count(*) FROM documents WHERE state='deleting'") != 0 {
				t.Fatal("partial deletion intent")
			}
		})
	}
}
func TestDeleteRunningScanRetainsLeaseReservationAndStopEvidence(t *testing.T) {
	f, a, stop := startedCapture(t)
	ctx := context.Background()
	request := DeleteRequest{DocumentID: a.DocumentID, Revision: a.Revision, IdempotencyKey: "request_delete_scan"}
	if _, err := f.store.RequestDelete(ctx, alice, request, 72*time.Hour); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='cancelling'", a.JobID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=? AND state='held'", a.JobID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != 1 {
		t.Fatal("delete intent released scan resources")
	}
	if _, err := f.store.CommitScanPage(ctx, alice, a.JobID, a.Revision, captureArtifact(), stop); err == nil {
		t.Fatal("deleted scan published")
	}
}
func TestConcurrentDeleteCreatesOneCleanupIdentity(t *testing.T) {
	f := thumbnailFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan JobProjection, 2)
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := f.store.RequestDelete(ctx, alice, deleteRequest(), time.Hour)
			results <- result
			failures <- err
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	id := ""
	for result := range results {
		if id != "" && id != result.ID {
			t.Fatal("duplicate job")
		}
		id = result.ID
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind='cleanup'") != 1 {
		t.Fatal("duplicate cleanup")
	}
}
func TestExpiredCleanupBoundedDisabledOwnersAndReplay(t *testing.T) {
	f := thumbnailFixture(t)
	must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", bob)
	f.now = 10000
	ctx := context.Background()
	count, err := f.store.QueueExpiredCleanup(ctx, 1, time.Hour)
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	count, err = f.store.QueueExpiredCleanup(ctx, 1, time.Hour)
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	count, err = f.store.QueueExpiredCleanup(ctx, 100, time.Hour)
	if err != nil || count != 0 {
		t.Fatal("expiry duplicated cleanup", count, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind='cleanup'") != 2 || scalar(t, f.docs, "SELECT count(*) FROM documents WHERE state='expired'") != 2 || scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", jobA) != 1 {
		t.Fatal("TTL cleanup changed reserved resources")
	}
}
