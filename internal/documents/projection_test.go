package documents

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProjectionPagesAndCurrentOutput(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_page_0001',?,?,'original','ready','private-original.png','image/png',20,zeroblob(32),1,1000)`, docA, alice)
	must(t, f.docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,rotation,capture_size,capture_color,capture_dpi) VALUES('page_alice_000001',?,?,0,'original_page_0001',90,'a4','gray',300)`, docA, alice)
	p, err := f.store.ReadDocument(ctx, alice, docA)
	if err != nil || p.PageCount != 1 || p.Pages[0].Rotation != 90 || p.Pages[0].Capture.DPI != 300 || p.Output != nil || p.ReadyRevision != nil {
		t.Fatal("invalid projection", p, err)
	}
	claim(t, f)
	if err := f.store.PublishExport(ctx, alice, jobA, runner, 1, output()); err != nil {
		t.Fatal(err)
	}
	p, err = f.store.ReadDocument(ctx, alice, docA)
	if err != nil || p.Output == nil || p.Output.Format != "pdf" || *p.ReadyRevision != 1 {
		t.Fatal("missing current export", p, err)
	}
	if _, err = f.store.ReadDocument(ctx, bob, docA); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cross-owner projection", err)
	}
	f.now = 10000
	if _, err = f.store.ReadDocument(ctx, alice, docA); !errors.Is(err, ErrUnavailable) {
		t.Fatal("expired projection", err)
	}
}
func TestHistoryPaginationOwnerExpiryAndDisable(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	must(t, f.docs, `INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('document_alice_002',?,'photo','review',1000,10000)`, alice)
	values, next, err := f.store.ReadHistory(ctx, alice, 1, nil)
	if err != nil || len(values) != 1 || values[0].ID != docA || next == nil {
		t.Fatal("first history page", values, next, err)
	}
	values, last, err := f.store.ReadHistory(ctx, alice, 1, next)
	if err != nil || len(values) != 1 || values[0].ID != "document_alice_002" || last != nil {
		t.Fatal("second history page", values, last, err)
	}
	// A cursor from another owner only affects ordering, never authority.
	values, _, err = f.store.ReadHistory(ctx, bob, 100, next)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range values {
		if p.ID != docB {
			t.Fatal("cursor leaked owner")
		}
	}
	must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
	if _, _, err = f.store.ReadHistory(ctx, alice, 10, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("disabled owner history", err)
	}
	f.now = 10000
	values, _, err = f.store.ReadHistory(ctx, bob, 10, nil)
	if err != nil || len(values) != 0 {
		t.Fatal("expired history", values, err)
	}
}

func TestProjectionDiscoversOnlyOwnedActiveNonCleanupWork(t *testing.T) {
	for _, state := range []string{"queued", "running", "cancelling", "reconciling", "succeeded", "failed", "cancelled", "interrupted"} {
		t.Run(state, func(t *testing.T) {
			f := setup(t)
			must(t, f.docs, "UPDATE jobs SET state=? WHERE id=?", state, jobA)
			p, err := f.store.ReadDocument(context.Background(), alice, docA)
			if err != nil {
				t.Fatal(err)
			}
			active := state == "queued" || state == "running" || state == "cancelling" || state == "reconciling"
			if active && (p.ActiveJobID == nil || *p.ActiveJobID != jobA) || !active && p.ActiveJobID != nil {
				t.Fatal("wrong active job projection", state, p.ActiveJobID)
			}
			if _, err = f.store.ReadDocument(context.Background(), bob, docA); !errors.Is(err, ErrUnavailable) {
				t.Fatal("cross-owner job discovery", err)
			}
			values, _, err := f.store.ReadHistory(context.Background(), alice, 20, nil)
			if err != nil || len(values) != 1 {
				t.Fatal(err)
			}
			if active && (values[0].ActiveJobID == nil || *values[0].ActiveJobID != jobA) || !active && values[0].ActiveJobID != nil {
				t.Fatal("history/detail active-job mismatch")
			}
		})
	}
	f := setup(t)
	must(t, f.docs, "DELETE FROM jobs")
	must(t, f.docs, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES('cleanup_active_0001',?,?,'cleanup','queued',1,'{}',1000,9000)`, docA, alice)
	p, err := f.store.ReadDocument(context.Background(), alice, docA)
	if err != nil || p.ActiveJobID != nil {
		t.Fatal("cleanup exposed as resumable content job", p, err)
	}
}

func TestProjectionRechecksRetentionAfterBatchReads(t *testing.T) {
	for _, mode := range []string{"detail", "history"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			calls := 0
			f.store.now = func() time.Time {
				calls++
				if calls == 1 {
					return time.UnixMilli(2000)
				}
				return time.UnixMilli(10000)
			}
			if mode == "detail" {
				p, err := f.store.ReadDocument(context.Background(), alice, docA)
				if !errors.Is(err, ErrUnavailable) || p.ID != "" || p.ActiveJobID != nil {
					t.Fatal("expired content returned", p, err)
				}
			} else {
				values, _, err := f.store.ReadHistory(context.Background(), alice, 20, nil)
				if err != nil || len(values) != 0 {
					t.Fatal("expired history returned", values, err)
				}
			}
		})
	}
}
