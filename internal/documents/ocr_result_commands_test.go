package documents

import (
	"context"
	"testing"
)

func TestReOCRAdoptWholeDocument(t *testing.T) {
	f, b := ocrCandidateFixture(t)
	if err := f.store.CompleteOCRResult(context.Background(), alice, jobA, runner, 1, b); err != nil {
		t.Fatal(err)
	}
	// A durable explicit whole-bundle disposition must authorize only this
	// output at the next revision, without mutating its input provenance.
	tx, err := f.docs.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM published_outputs WHERE document_id='document_alice_001'`,
		`UPDATE documents SET revision=2 WHERE id='document_alice_001'`,
		`UPDATE ocr_results SET state='adopted',adopted_revision=2 WHERE id='ocr_result_candidate_001'`,
		`INSERT INTO published_outputs(document_id,owner_id,revision,artifact_id) VALUES('document_alice_001','principal_alice_001',2,'artifact_export_0001')`,
	} {
		if _, err = tx.Exec(q); err != nil {
			t.Fatal("explicit whole-document adoption rejected", err)
		}
	}
}
