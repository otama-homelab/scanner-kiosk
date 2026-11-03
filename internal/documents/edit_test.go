package documents

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"
)

const pageOne = "page_edit_00000001"
const pageTwo = "page_edit_00000002"

func editable(t *testing.T) *fixture {
	f := setup(t)
	for i, page := range []string{pageOne, pageTwo} {
		artifact := page + "_original"
		must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready',?,'image/png',20,zeroblob(32),1,1000)`, artifact, docA, alice, artifact+".png")
		must(t, f.docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi) VALUES(?,?,?, ?,?,'a4','gray',300)`, page, docA, alice, i, artifact)
	}
	claim(t, f)
	if err := f.store.PublishExport(context.Background(), alice, jobA, runner, 1, output()); err != nil {
		t.Fatal(err)
	}
	return f
}
func rotation() EditRequest {
	return EditRequest{DocumentID: docA, Revision: 1, IdempotencyKey: "request_rotate_001", Operation: "rotate", PageID: pageOne, Rotation: 90}
}
func TestEditRotationReplayAndOutputInvalidation(t *testing.T) {
	f := editable(t)
	ctx := context.Background()
	request := rotation()
	result, err := f.store.Edit(ctx, alice, request, time.Hour)
	if err != nil || result.Revision != 2 || result.State != "review" || result.Output != nil || result.Pages[0].Rotation != 90 || result.ExpiresAt.UnixMilli() != 10000 {
		t.Fatal("rotation", result, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM artifacts WHERE kind='original'") != 2 {
		t.Fatal("original removed")
	}
	replay, err := f.store.Edit(ctx, alice, request, time.Hour)
	if err != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("exact replay", replay, err)
	}
	next := request
	next.Revision = 2
	next.Rotation = 180
	next.IdempotencyKey = "request_rotate_002"
	if _, err = f.store.Edit(ctx, alice, next, time.Hour); err != nil {
		t.Fatal(err)
	}
	replay, err = f.store.Edit(ctx, alice, request, time.Hour)
	if err != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("old replay changed after later edit", err)
	}
	changed := request
	changed.Rotation = 270
	if _, err = f.store.Edit(ctx, alice, changed, time.Hour); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("changed intent", err)
	}
	request.IdempotencyKey = "request_rotate_003"
	if _, err = f.store.Edit(ctx, alice, request, time.Hour); !errors.Is(err, ErrRevision) {
		t.Fatal("stale revision", err)
	}
}
func TestEditReorderDeleteAndPreserveOriginals(t *testing.T) {
	f := editable(t)
	ctx := context.Background()
	request := EditRequest{DocumentID: docA, Revision: 1, IdempotencyKey: "request_reorder_01", Operation: "reorder", PageIDs: []string{pageTwo, pageOne}}
	result, err := f.store.Edit(ctx, alice, request, time.Hour)
	if err != nil || result.Pages[0].ID != pageTwo || result.Pages[0].Ordinal != 0 || result.Pages[1].Ordinal != 1 {
		t.Fatal("reorder", result, err)
	}
	request = EditRequest{DocumentID: docA, Revision: 2, IdempotencyKey: "request_delete_01", Operation: "delete_page", PageID: pageTwo}
	result, err = f.store.Edit(ctx, alice, request, time.Hour)
	if err != nil || result.PageCount != 1 || result.Pages[0].ID != pageOne || result.Pages[0].Ordinal != 0 {
		t.Fatal("delete", result, err)
	}
	request.Revision = 3
	request.IdempotencyKey = "request_delete_02"
	request.PageID = pageOne
	result, err = f.store.Edit(ctx, alice, request, time.Hour)
	if err != nil || result.PageCount != 0 || len(result.Pages) != 0 || result.Revision != 4 {
		t.Fatal("last page", result, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM artifacts WHERE kind='original'") != 2 {
		t.Fatal("physical originals prematurely removed")
	}
}
func TestEditRejectsUnsafeAuthorityAndPageSets(t *testing.T) {
	for _, name := range []string{"owner", "disabled", "expired", "active", "state", "duplicates", "wrongset", "shortretention"} {
		t.Run(name, func(t *testing.T) {
			f := editable(t)
			request := rotation()
			owner := alice
			retention := time.Hour
			expected := ErrUnavailable
			switch name {
			case "owner":
				owner = bob
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
			case "expired":
				f.now = 10000
			case "active":
				must(t, f.docs, `UPDATE jobs SET state='queued' WHERE id=?`, jobA)
				expected = ErrConflict
			case "state":
				must(t, f.docs, `UPDATE documents SET state='processing' WHERE id=?`, docA)
				expected = ErrConflict
			case "duplicates":
				request.Operation = "reorder"
				request.PageID = ""
				request.Rotation = 0
				request.PageIDs = []string{pageOne, pageOne}
				expected = ErrInvalid
			case "wrongset":
				request.Operation = "reorder"
				request.PageID = ""
				request.Rotation = 0
				request.PageIDs = []string{pageOne, "page_foreign_0001"}
				expected = ErrInvalid
			case "shortretention":
				retention = time.Second
				expected = ErrInvalid
			}
			if _, err := f.store.Edit(context.Background(), owner, request, retention); !errors.Is(err, expected) {
				t.Fatal("unsafe edit accepted", err)
			}
			if scalar(t, f.docs, "SELECT revision FROM documents WHERE id=?", docA) != 1 || scalar(t, f.docs, "SELECT rotation FROM pages WHERE id=?", pageOne) != 0 {
				t.Fatal("rejected edit mutated")
			}
		})
	}
}
func TestEditUncertainCommitReturnsNoProjectionAndReplays(t *testing.T) {
	f := editable(t)
	f.store.commit = func(tx *sql.Tx) error {
		if err := tx.Commit(); err != nil {
			return err
		}
		return errors.New("lost commit acknowledgement")
	}
	result, err := f.store.Edit(context.Background(), alice, rotation(), time.Hour)
	if !errors.Is(err, ErrCommitUncertain) || result.ID != "" {
		t.Fatal("uncertain result escaped", result, err)
	}
	f.store.commit = func(tx *sql.Tx) error { return tx.Commit() }
	result, err = f.store.Edit(context.Background(), alice, rotation(), time.Hour)
	if err != nil || result.Revision != 2 {
		t.Fatal("commit readback", result, err)
	}
	f.now = 10000
	if _, err = f.store.Edit(context.Background(), alice, rotation(), time.Hour); !errors.Is(err, ErrGone) {
		t.Fatal("expired replay", err)
	}
}

func TestConcurrentEditsHaveOneRevisionWinner(t *testing.T) {
	for _, same := range []bool{false, true} {
		t.Run(map[bool]string{false: "different-intent", true: "same-intent"}[same], func(t *testing.T) {
			f := editable(t)
			start := make(chan struct{})
			results := make(chan error, 2)
			for i := 0; i < 2; i++ {
				go func(index int) {
					<-start
					request := rotation()
					if index == 1 && !same {
						request.IdempotencyKey = "request_rotate_002"
						request.Rotation = 180
					}
					_, err := f.store.Edit(context.Background(), alice, request, time.Hour)
					results <- err
				}(i)
			}
			close(start)
			success, stale := 0, 0
			for i := 0; i < 2; i++ {
				err := <-results
				switch {
				case err == nil:
					success++
				case errors.Is(err, ErrRevision):
					stale++
				default:
					t.Fatal(err)
				}
			}
			if same && success != 2 || !same && (success != 1 || stale != 1) {
				t.Fatal("concurrent edit outcome", success, stale)
			}
			if scalar(t, f.docs, "SELECT revision FROM documents WHERE id=?", docA) != 2 {
				t.Fatal("duplicate revision advancement")
			}
			if _, err := f.docs.Exec("UPDATE edit_results SET projection_json='{}'"); err == nil {
				t.Fatal("receipt projection mutable")
			}
		})
	}
}

func TestEditExpiresAfterProjectionRollsBackMutationAndReceipt(t *testing.T) {
	f := editable(t)
	calls := 0
	f.store.now = func() time.Time {
		calls++
		if calls >= 3 {
			return time.UnixMilli(10000)
		}
		return time.UnixMilli(2000)
	}
	result, err := f.store.Edit(context.Background(), alice, rotation(), time.Hour)
	if !errors.Is(err, ErrUnavailable) || result.ID != "" {
		t.Fatal("expired edit returned success", result, err)
	}
	if scalar(t, f.docs, "SELECT revision FROM documents WHERE id=?", docA) != 1 || scalar(t, f.docs, "SELECT rotation FROM pages WHERE id=?", pageOne) != 0 || scalar(t, f.docs, "SELECT count(*) FROM edit_results") != 0 || scalar(t, f.docs, "SELECT count(*) FROM published_outputs") != 1 {
		t.Fatal("expired edit partially committed")
	}
}
