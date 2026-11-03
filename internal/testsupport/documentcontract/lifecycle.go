package documentcontract

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
)

// LifecycleFixture has the base document plus one queued render job/reservation.
// Expire reaches the document TTL while its one-hour receipt is still retained.
type LifecycleFixture struct {
	Fixture
	Job      string
	Revision int64
}

// RunLifecycle checks public cancellation/deletion semantics through Records.
// Resource retention/physical stop and uncertain commits need separate tests.
func RunLifecycle(t *testing.T, newFixture func(*testing.T) LifecycleFixture) {
	t.Helper()
	ctx := context.Background()
	request := func(f LifecycleFixture, key string) documents.DeleteRequest {
		return documents.DeleteRequest{DocumentID: f.Document, Revision: f.Revision, IdempotencyKey: key}
	}
	t.Run("cancel owner and replay", func(t *testing.T) {
		f := newFixture(t)
		before, err := f.Records.ReadJob(ctx, f.Owner, f.Job)
		if err != nil || before.Kind != "render" || before.State != "queued" || before.InputRevision != f.Revision {
			t.Fatal("invalid fixture", before, err)
		}
		if _, err = f.Records.CancelJob(ctx, f.OtherOwner, f.Job, "contract_cancel_001", time.Hour); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("foreign cancellation", err)
		}
		result, err := f.Records.CancelJob(ctx, f.Owner, f.Job, "contract_cancel_001", time.Hour)
		if err != nil || result.ID != f.Job || result.State != "cancelling" {
			t.Fatal("cancellation intent", result, err)
		}
		replay, err := f.Records.CancelJob(ctx, f.Owner, f.Job, "contract_cancel_001", time.Hour)
		if err != nil || !reflect.DeepEqual(replay, result) {
			t.Fatal("cancellation replay", replay, err)
		}
		current, err := f.Records.ReadDocument(ctx, f.Owner, f.Document)
		if err != nil || current.PageCount != 2 || current.Revision != f.Revision {
			t.Fatal("cancel intent removed content", current, err)
		}
	})
	t.Run("delete revokes reads and reuses cleanup", func(t *testing.T) {
		f := newFixture(t)
		if _, err := f.Records.RequestDelete(ctx, f.OtherOwner, request(f, "contract_delete_001"), time.Hour); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("foreign deletion", err)
		}
		result, err := f.Records.RequestDelete(ctx, f.Owner, request(f, "contract_delete_001"), time.Hour)
		if err != nil || result.Kind != "cleanup" || result.State != "queued" {
			t.Fatal("cleanup intent", result, err)
		}
		if _, err = f.Records.ReadPageOriginal(ctx, f.Owner, f.Document, f.Pages[0]); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("deleted original exposed", err)
		}
		if _, err = f.Records.ReadJob(ctx, f.Owner, f.Job); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("deleted document exposed old work status", err)
		}
		replay, err := f.Records.RequestDelete(ctx, f.Owner, request(f, "contract_delete_001"), time.Hour)
		if err != nil || !reflect.DeepEqual(result, replay) {
			t.Fatal("delete replay", replay, err)
		}
		again, err := f.Records.RequestDelete(ctx, f.Owner, request(f, "contract_delete_002"), time.Hour)
		if err != nil || again.ID != result.ID {
			t.Fatal("second key duplicated cleanup", again, err)
		}
		if _, err = f.Records.ReadJob(ctx, f.OtherOwner, result.ID); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("foreign cleanup status exposed", err)
		}
	})
	t.Run("retained delete replay after content expiry", func(t *testing.T) {
		f := newFixture(t)
		intent := request(f, "contract_delete_001")
		result, err := f.Records.RequestDelete(ctx, f.Owner, intent, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		f.Expire()
		replay, err := f.Records.RequestDelete(ctx, f.Owner, intent, time.Hour)
		if err != nil || replay.ID != result.ID {
			t.Fatal("retained cleanup status lost at content TTL", replay, err)
		}
		if _, err = f.Records.RequestDelete(ctx, f.Owner, request(f, "contract_delete_002"), time.Hour); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("new delete accepted after content TTL", err)
		}
	})
	t.Run("disabled owner cannot replay", func(t *testing.T) {
		f := newFixture(t)
		intent := request(f, "contract_delete_001")
		result, err := f.Records.RequestDelete(ctx, f.Owner, intent, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		f.Disable()
		if _, err = f.Records.RequestDelete(ctx, f.Owner, intent, time.Hour); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("disabled delete replay", err)
		}
		if _, err = f.Records.ReadJob(ctx, f.Owner, result.ID); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("disabled cleanup status", err)
		}
	})
}
