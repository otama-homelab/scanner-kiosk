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

type editContextKey struct{}

type editRecordsFunc func(context.Context, string, documents.EditCommand) (documents.Projection, error)

func (f editRecordsFunc) ApplyEdit(ctx context.Context, owner string, c documents.EditCommand) (documents.Projection, error) {
	return f(ctx, owner, c)
}

func TestEditorRejectsInvalidCommandsBeforeBackend(t *testing.T) {
	if _, err := documents.NewEditor(nil); !errors.Is(err, documents.ErrInvalid) {
		t.Fatal(err)
	}
	calls := 0
	editor, err := documents.NewEditor(editRecordsFunc(func(context.Context, string, documents.EditCommand) (documents.Projection, error) {
		calls++
		return documents.Projection{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	valid := documents.EditRequest{DocumentID: "document_editor_001", Revision: 1, IdempotencyKey: "request_editor_001", Operation: "rotate", PageID: "page_editor_0001", Rotation: 90}
	if _, err = editor.Edit(context.Background(), "principal_editor_001", valid, time.Hour); err != nil || calls != 1 {
		t.Fatal("valid control did not reach backend", err, calls)
	}
	calls = 0
	for _, mutate := range []func(*documents.EditRequest){
		func(r *documents.EditRequest) { r.DocumentID = "" },
		func(r *documents.EditRequest) { r.IdempotencyKey = "" },
		func(r *documents.EditRequest) { r.Revision = 0 },
		func(r *documents.EditRequest) { r.Revision = math.MaxInt64 },
		func(r *documents.EditRequest) { r.Operation = "unknown" },
		func(r *documents.EditRequest) { r.PageID = "" },
		func(r *documents.EditRequest) { r.Rotation = 45 },
		func(r *documents.EditRequest) { r.PageIDs = []string{} },
		func(r *documents.EditRequest) { r.Operation = "delete_page"; r.Rotation = 90 },
		func(r *documents.EditRequest) {
			r.Operation = "reorder"
			r.PageID = ""
			r.Rotation = 0
			r.PageIDs = []string{"page_editor_0001", "page_editor_0001"}
		},
	} {
		r := valid
		mutate(&r)
		if _, err = editor.Edit(context.Background(), "principal_editor_001", r, time.Hour); !errors.Is(err, documents.ErrInvalid) {
			t.Fatal("invalid command reached backend", r, err)
		}
	}
	r := documents.EditRequest{DocumentID: "document_editor_001", Revision: 1, IdempotencyKey: "request_editor_001", Operation: "delete_page", PageID: "page_editor_0001"}
	for _, retention := range []time.Duration{0, 366 * 24 * time.Hour} {
		if _, err = editor.Edit(context.Background(), "principal_editor_001", r, retention); !errors.Is(err, documents.ErrInvalid) {
			t.Fatal("invalid retention", err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid request performed storage work", calls)
	}
}

func TestEditorBackendCommandIsDetachedAndKeepsV1Hashes(t *testing.T) {
	var saved documents.EditCommand
	calls := 0
	backendErr := errors.New("backend uncertainty")
	editor, err := documents.NewEditor(editRecordsFunc(func(ctx context.Context, owner string, command documents.EditCommand) (documents.Projection, error) {
		calls++
		if owner != "principal_editor_001" || ctx.Value(editContextKey{}) != "context" {
			t.Fatal("command context changed")
		}
		saved = command
		return documents.Projection{}, backendErr
	}))
	if err != nil {
		t.Fatal(err)
	}
	r := documents.EditRequest{DocumentID: "document_editor_001", Revision: 7, IdempotencyKey: "request_editor_001", Operation: "reorder", PageIDs: []string{"page_editor_0002", "page_editor_0001"}}
	ctx := context.WithValue(context.Background(), editContextKey{}, "context")
	if _, err = editor.Edit(ctx, "principal_editor_001", r, time.Hour); err != backendErr || calls != 1 {
		t.Fatal("backend error or invocation changed", err, calls)
	}
	r.PageIDs[0] = "page_mutated_001"
	copy, retention, err := saved.Request()
	if err != nil || retention != time.Hour || copy.PageIDs[0] != "page_editor_0002" {
		t.Fatal("caller changed prepared command", copy, err)
	}
	copy.PageIDs[0] = "page_mutated_002"
	again, _, err := saved.Request()
	if err != nil || again.PageIDs[0] != "page_editor_0002" {
		t.Fatal("adapter changed prepared command", again, err)
	}
	key, digest := saved.Hashes()
	wire := `{"document":"document_editor_001","operation":"reorder","page":"","pages":["page_editor_0002","page_editor_0001"],"revision":7,"rotation":0,"version":1}`
	if key != sha256.Sum256([]byte("request_editor_001")) || digest != sha256.Sum256([]byte(wire)) {
		t.Fatal("existing receipt hash changed")
	}
}

func TestSQLiteRejectsUnpreparedEditWithoutStorageAccess(t *testing.T) {
	var store *documents.Store
	if _, err := store.ApplyEdit(context.Background(), "principal_editor_001", documents.EditCommand{}); !errors.Is(err, documents.ErrInvalid) {
		t.Fatal("zero command accepted", err)
	}
}
