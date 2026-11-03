package documents_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
)

type lifecycleContextKey struct{}

type lifecycleRecords struct {
	delete func(context.Context, string, documents.DeleteCommand) (documents.JobProjection, error)
	cancel func(context.Context, string, documents.CancelCommand) (documents.JobProjection, error)
}

func (r lifecycleRecords) ApplyDelete(ctx context.Context, owner string, c documents.DeleteCommand) (documents.JobProjection, error) {
	return r.delete(ctx, owner, c)
}
func (r lifecycleRecords) ApplyCancel(ctx context.Context, owner string, c documents.CancelCommand) (documents.JobProjection, error) {
	return r.cancel(ctx, owner, c)
}

func TestLifecycleValidatesBeforeBackend(t *testing.T) {
	if _, err := documents.NewLifecycle(nil); !errors.Is(err, documents.ErrInvalid) {
		t.Fatal(err)
	}
	deletes, cancels := 0, 0
	service, err := documents.NewLifecycle(lifecycleRecords{
		delete: func(context.Context, string, documents.DeleteCommand) (documents.JobProjection, error) {
			deletes++
			return documents.JobProjection{}, nil
		},
		cancel: func(context.Context, string, documents.CancelCommand) (documents.JobProjection, error) {
			cancels++
			return documents.JobProjection{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	valid := documents.DeleteRequest{DocumentID: "document_lifecycle_001", Revision: 1, IdempotencyKey: "request_lifecycle_001"}
	if _, err = service.RequestDelete(ctx, "principal_lifecycle_001", valid, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CancelJob(ctx, "principal_lifecycle_001", "job_lifecycle_00001", valid.IdempotencyKey, time.Hour); err != nil {
		t.Fatal(err)
	}
	if deletes != 1 || cancels != 1 {
		t.Fatal("valid controls did not reach backend", deletes, cancels)
	}
	deletes, cancels = 0, 0
	for _, mutate := range []func(*documents.DeleteRequest){func(r *documents.DeleteRequest) { r.DocumentID = "" }, func(r *documents.DeleteRequest) { r.Revision = 0 }, func(r *documents.DeleteRequest) { r.IdempotencyKey = "" }} {
		r := valid
		mutate(&r)
		if _, err = service.RequestDelete(ctx, "principal_lifecycle_001", r, time.Hour); !errors.Is(err, documents.ErrInvalid) {
			t.Fatal("invalid delete", r, err)
		}
	}
	for _, v := range [][2]string{{"", "request_lifecycle_001"}, {"job_lifecycle_00001", ""}} {
		if _, err = service.CancelJob(ctx, "principal_lifecycle_001", v[0], v[1], time.Hour); !errors.Is(err, documents.ErrInvalid) {
			t.Fatal("invalid cancel", err)
		}
	}
	for _, retention := range []time.Duration{0, 366 * 24 * time.Hour} {
		if _, err = service.RequestDelete(ctx, "principal_lifecycle_001", valid, retention); !errors.Is(err, documents.ErrInvalid) {
			t.Fatal("invalid delete retention", err)
		}
		if _, err = service.CancelJob(ctx, "principal_lifecycle_001", "job_lifecycle_00001", valid.IdempotencyKey, retention); !errors.Is(err, documents.ErrInvalid) {
			t.Fatal("invalid cancel retention", err)
		}
	}
	if deletes != 0 || cancels != 0 {
		t.Fatal("invalid command performed storage work", deletes, cancels)
	}
	valid.Revision = math.MaxInt64
	if _, err = service.RequestDelete(ctx, "principal_lifecycle_001", valid, time.Hour); err != nil || deletes != 1 {
		t.Fatal("deletion cannot advance revision; maximum revision remains valid", err)
	}
}

func TestLifecycleKeepsCommandIdentityAndBackendErrors(t *testing.T) {
	var deletion documents.DeleteCommand
	var cancellation documents.CancelCommand
	backendErr := errors.New("backend commit unknown")
	deletes, cancels := 0, 0
	check := func(ctx context.Context, owner string) {
		t.Helper()
		if ctx.Value(lifecycleContextKey{}) != "caller" || owner != "principal_lifecycle_001" {
			t.Fatal("authority context changed")
		}
	}
	service, err := documents.NewLifecycle(lifecycleRecords{
		delete: func(ctx context.Context, owner string, c documents.DeleteCommand) (documents.JobProjection, error) {
			check(ctx, owner)
			deletes++
			deletion = c
			return documents.JobProjection{}, backendErr
		},
		cancel: func(ctx context.Context, owner string, c documents.CancelCommand) (documents.JobProjection, error) {
			check(ctx, owner)
			cancels++
			cancellation = c
			return documents.JobProjection{}, backendErr
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), lifecycleContextKey{}, "caller")
	r := documents.DeleteRequest{DocumentID: "document_lifecycle_001", Revision: 7, IdempotencyKey: "request_lifecycle_001"}
	if _, err = service.RequestDelete(ctx, "principal_lifecycle_001", r, time.Hour); err != backendErr || deletes != 1 || cancels != 0 {
		t.Fatal("delete dispatch", err, deletes, cancels)
	}
	if _, err = service.CancelJob(ctx, "principal_lifecycle_001", "job_lifecycle_00001", r.IdempotencyKey, time.Hour); err != backendErr || deletes != 1 || cancels != 1 {
		t.Fatal("cancel dispatch", err, deletes, cancels)
	}
	r.DocumentID = "document_mutated_001"
	copy, retention, err := deletion.Request()
	if err != nil || copy.DocumentID != "document_lifecycle_001" || copy.Revision != 7 || retention != time.Hour {
		t.Fatal("delete command changed", copy, err)
	}
	job, key, retention, err := cancellation.Request()
	if err != nil || job != "job_lifecycle_00001" || key != "request_lifecycle_001" || retention != time.Hour {
		t.Fatal("cancel command changed", job, key, err)
	}
	deleteKey, deleteHash := deletion.Hashes()
	cancelKey, cancelHash := cancellation.Hashes()
	if deleteKey != sha256.Sum256([]byte("request_lifecycle_001")) || cancelKey != deleteKey || deleteHash != sha256.Sum256([]byte(`{"document":"document_lifecycle_001","revision":7,"version":1}`)) || cancelHash != sha256.Sum256([]byte("cancel-v1\x00job_lifecycle_00001")) {
		t.Fatal("established receipt identities changed")
	}
}

func TestSQLiteRejectsUnpreparedLifecycleCommands(t *testing.T) {
	var store *documents.Store
	if _, err := store.ApplyDelete(context.Background(), "principal_lifecycle_001", documents.DeleteCommand{}); !errors.Is(err, documents.ErrInvalid) {
		t.Fatal("zero delete reached storage", err)
	}
	if _, err := store.ApplyCancel(context.Background(), "principal_lifecycle_001", documents.CancelCommand{}); !errors.Is(err, documents.ErrInvalid) {
		t.Fatal("zero cancel reached storage", err)
	}
}
