package documents

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"

	"modernc.org/sqlite"
)

func TestHistoryProjectionQueryCountIsIndependentOfDocumentCount(t *testing.T) {
	for _, count := range []int{1, 32} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := setup(t)
			for i := 1; i < count; i++ {
				must(t, f.docs, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'photo','review',1000,10000)", fmt.Sprintf("document_history_%04d", i), alice)
				seedPage(t, f, fmt.Sprintf("document_history_%04d", i), alice, fmt.Sprintf("original_history_%04d", i), fmt.Sprintf("page_history_%04d", i))
			}
			var calls atomic.Int64
			name := fmt.Sprintf("history_projection_count_%d", statusDriverSequence.Add(1))
			sql.Register(name, statusCountDriver{base: &sqlite.Driver{}, calls: &calls})
			db, err := sql.Open(name, f.docsPath+"?_txlock=immediate&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			records, err := New(f.state, db, f.store.now, fixtureSpace)
			if err != nil {
				t.Fatal(err)
			}
			values, next, err := records.ReadHistory(context.Background(), alice, 100, nil)
			if err != nil || next != nil || len(values) != count {
				t.Fatal(len(values), next, err)
			}
			if calls.Load() != 3 {
				t.Fatal("history queries grew with document count", count, calls.Load())
			}
			if values[0].ID != docA || values[0].ActiveJobID == nil || *values[0].ActiveJobID != jobA {
				t.Fatal("batch lost active work")
			}
			for _, p := range values[1:] {
				if p.PhotoFormat == nil || *p.PhotoFormat != "jpeg" || p.ActiveJobID != nil {
					t.Fatal("batch crossed document preferences", p)
				}
			}
		})
	}
}
func TestHistoryBatchKeepsPageOrderOwnerAndOutputRevision(t *testing.T) {
	f := setup(t)
	must(t, f.docs, "DELETE FROM jobs")
	second := "document_alice_002"
	must(t, f.docs, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',1000,10000)", second, alice)
	for i, doc := range []string{docA, second, docB} {
		owner := alice
		if doc == docB {
			owner = bob
		}
		artifact := fmt.Sprintf("original_batch_%04d", i)
		seedPage(t, f, doc, owner, artifact, fmt.Sprintf("page_history_batch_%04d", i))
		output := fmt.Sprintf("output_batch_%04d", i)
		must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'output','ready',?,'application/pdf',10,zeroblob(32),1,1000)`, output, doc, owner, output)
		must(t, f.docs, "INSERT INTO published_outputs(document_id,owner_id,revision,artifact_id) VALUES(?,?,1,?)", doc, owner, output)
	}
	must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_history_second',?,?,'original','ready','history-second.png','image/png',10,zeroblob(32),1,1000)`, docA, alice)
	must(t, f.docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi) VALUES('page_history_second',?,?,1,'original_history_second','a4','color',300)`, docA, alice)
	must(t, f.docs, "DELETE FROM published_outputs WHERE document_id=?", second)
	must(t, f.docs, "UPDATE documents SET revision=2 WHERE id=?", second)
	values, _, err := f.store.ReadHistory(context.Background(), alice, 100, nil)
	if err != nil || len(values) != 2 {
		t.Fatal(values, err)
	}
	if values[0].ID != docA || values[0].PageCount != 2 || values[0].Pages[0].Ordinal != 0 || values[0].Pages[1].Ordinal != 1 || values[0].Output == nil || values[0].Output.Revision != 1 {
		t.Fatal("current output lost", values[0])
	}
	if values[1].ID != second || values[1].Output != nil || values[1].ReadyRevision != nil || values[1].PageCount != 1 {
		t.Fatal("stale output became ready", values[1])
	}
}
