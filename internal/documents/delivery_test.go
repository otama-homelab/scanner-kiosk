package documents

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/storage"
)

func deliveryRequest() DeliveryRequest {
	return DeliveryRequest{DocumentID: docA, Revision: 2, IdempotencyKey: "request_delivery_0001", DestinationID: "paperless-local", ConfigFingerprint: strings.Repeat("a", 64)}
}

func readyDelivery(t *testing.T) (*fixture, Delivery) {
	t.Helper()
	f, b := completedOCRCandidate(t)
	if _, err := f.store.AdoptOCRResult(context.Background(), alice, ocrResultRequest(b), time.Hour); err != nil {
		t.Fatal(err)
	}
	d, err := f.store.AdmitDelivery(context.Background(), alice, deliveryRequest())
	if err != nil {
		t.Fatal(err)
	}
	return f, d
}

func reopenDelivery(t *testing.T, f *fixture) {
	t.Helper()
	if err := f.docs.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.docs, err = storage.Open(f.docsPath, storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	f.store, err = New(f.state, f.docs, func() time.Time { return time.UnixMilli(f.now) }, fixtureSpace)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeliverySemanticDedupNewKey(t *testing.T) {
	ctx := context.Background()
	f, d := readyDelivery(t)
	r := deliveryRequest()
	r.IdempotencyKey = "request_delivery_0002"
	again, err := f.store.AdmitDelivery(ctx, alice, r)
	if err != nil || again.ID != d.ID {
		t.Fatal("new key bypassed send fence", again, err)
	}
	render := admissionRequest()
	render.IdempotencyKey = deliveryRequest().IdempotencyKey
	if _, _, err = f.store.FindRender(ctx, alice, render); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("delivery key must conflict before normal render admission", err)
	}
	reopenDelivery(t, f)
	again, err = f.store.AdmitDelivery(ctx, alice, r)
	if err != nil || again.ID != d.ID {
		t.Fatal("reopen lost semantic evidence", err)
	}
}

func TestDeliveryAdmissionSnapshot(t *testing.T) {
	ctx := context.Background()
	t.Run("adopted current output and semantic fence", func(t *testing.T) {
		f, b := completedOCRCandidate(t)
		if _, err := f.store.AdoptOCRResult(ctx, alice, ocrResultRequest(b), time.Hour); err != nil {
			t.Fatal(err)
		}
		request := deliveryRequest()
		first, err := f.store.AdmitDelivery(ctx, alice, request)
		if err != nil {
			t.Fatal(err)
		}
		if first.Artifact.ID != b.Output.ID || first.Artifact.SHA256 != b.Output.SHA256 || first.Revision != 2 || first.ExpiresAtMS != 10000 || first.State != "queued" || first.Filename != first.ID+".pdf" {
			t.Fatal("snapshot drift", first)
		}
		again, err := f.store.AdmitDelivery(ctx, alice, request)
		if err != nil || again.ID != first.ID {
			t.Fatal("same intent replayed send", again, err)
		}
		request.IdempotencyKey = agedKey(f.now, 1)
		again, err = f.store.AdmitDelivery(ctx, alice, request)
		if err != nil || again.ID != first.ID {
			t.Fatal("new key bypassed semantic fence", again, err)
		}
		if scalar(t, f.docs, `SELECT count(*) FROM delivery_receipts WHERE key_expires_at_ms IS NOT NULL`) != 1 {
			t.Fatal("aged delivery key lost retirement evidence")
		}
		request.Revision = 3
		if _, err = f.store.AdmitDelivery(ctx, alice, request); !errors.Is(err, ErrIdempotencyConflict) {
			t.Fatal("key payload mismatch accepted", err)
		}
		if scalar(t, f.docs, `SELECT revision FROM documents WHERE id=?`, docA) != 2 || scalar(t, f.docs, `SELECT count(*) FROM ocr_results WHERE state='adopted' AND adopted_revision=2`) != 1 || scalar(t, f.docs, `SELECT count(*) FROM artifacts WHERE id='artifact_prior_output_001' AND state='ready'`) != 1 || scalar(t, f.docs, `SELECT count(*) FROM deliveries`) != 1 {
			t.Fatal("delivery changed output/source/OCR history")
		}
	})
	for _, name := range []string{"owner", "disabled", "expired", "revision", "unreviewed", "media", "missing output", "old key", "cross operation key"} {
		t.Run(name, func(t *testing.T) {
			f, b := completedOCRCandidate(t)
			if _, err := f.store.AdoptOCRResult(ctx, alice, ocrResultRequest(b), time.Hour); err != nil {
				t.Fatal(err)
			}
			r := deliveryRequest()
			owner := alice
			switch name {
			case "owner":
				owner = bob
			case "disabled":
				must(t, f.state, `UPDATE principals SET disabled_at_ms=2000 WHERE id=?`, alice)
			case "expired":
				f.now = 10000
			case "revision":
				r.Revision = 1
			case "unreviewed":
				must(t, f.docs, `UPDATE documents SET state='review' WHERE id=?`, docA)
			case "media":
				must(t, f.docs, `DELETE FROM published_outputs WHERE document_id=?`, docA)
				must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('delivery_image_output',?,?,'output','ready','delivery-image.png','image/png',100,zeroblob(32),2,2000)`, docA, alice)
				must(t, f.docs, `INSERT INTO published_outputs VALUES(?,?,2,'delivery_image_output')`, docA, alice)
			case "missing output":
				must(t, f.docs, `DELETE FROM published_outputs WHERE document_id=?`, docA)
			case "old key":
				r.IdempotencyKey = "rq1_0000000000001_bad"
			case "cross operation key":
				r.IdempotencyKey = ocrResultRequest(b).IdempotencyKey
			}
			if _, err := f.store.AdmitDelivery(ctx, owner, r); err == nil {
				t.Fatal("invalid delivery admitted")
			}
			if scalar(t, f.docs, `SELECT count(*) FROM deliveries`) != 0 {
				t.Fatal("rejected admission created ledger")
			}
		})
	}
}
