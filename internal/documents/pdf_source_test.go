package documents

import (
	"context"
	"encoding/json"
	"testing"
)

func TestImportedPDFProjectionAndRenderSnapshot(t *testing.T) {
	f := renderFixture(t)
	seedImportedPDFPage(t, f)
	projection, err := f.store.ReadDocument(context.Background(), alice, docA)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Pages) != 2 || projection.Pages[0].Capture == nil || projection.Pages[1].Capture != nil || projection.Pages[1].SourceType != "pdf" || projection.Pages[1].PDF == nil || projection.Pages[1].PDF.WidthPoints != 200 {
		t.Fatal("false capture metadata", projection)
	}
	raw, err := json.Marshal(projection.Pages[1])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil || fields["capture"] != nil {
		t.Fatal("PDF represented as raster", string(raw))
	}
	admitted, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimExport(context.Background(), alice, admitted.JobID, runner); err != nil {
		t.Fatal(err)
	}
	work, err := f.store.LoadRenderWork(context.Background(), alice, admitted.JobID, runner)
	if err != nil {
		t.Fatal(err)
	}
	page := work.Snapshot.Pages[1]
	if page.SourceType != "pdf" || page.PDFWidthPoints != 200 || page.PDFHeightPoints != 300 || page.DPI != 0 || page.Size != "" || work.Inputs[1].MediaType != "application/pdf" {
		t.Fatal("lost real source metadata", page)
	}
	if _, err = f.store.ReadDocument(context.Background(), bob, docA); err == nil {
		t.Fatal("foreign owner read PDF metadata")
	}
}

func seedImportedPDFPage(t *testing.T, f *fixture) {
	t.Helper()
	must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_pdf_source1',?,?,'original','ready','pdf/source','application/pdf',100,zeroblob(32),1,1001)`, docA, alice)
	must(t, f.docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pdf_width_points,pdf_height_points) VALUES('page_pdf_source_001',?,?,1,'original_pdf_source1','pdf',200,300)`, docA, alice)
}
