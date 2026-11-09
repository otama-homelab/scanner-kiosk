package documents

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"scanner-kiosk/internal/storage"
)

func deliveryRequest() DeliveryRequest {
	return DeliveryRequest{DocumentID: docA, Revision: 2, IdempotencyKey: "request_delivery_0001", DestinationID: "paperless-local", ConfigFingerprint: strings.Repeat("a", 64)}
}

func TestDeliveryOneAttemptCrashMatrix(t *testing.T) {
	ctx := context.Background()
	const task = "12345678-1234-4234-8234-123456789abc"
	for _, stage := range []string{"queued", "attempted", "accepted", "confirmed"} {
		for _, committed := range []bool{false, true} {
			name := stage + map[bool]string{false: "/rollback", true: "/ack-loss"}[committed]
			t.Run(name, func(t *testing.T) {
				f, b := completedOCRCandidate(t)
				if _, err := f.store.AdoptOCRResult(ctx, alice, ocrResultRequest(b), time.Hour); err != nil {
					t.Fatal(err)
				}
				var d Delivery
				var err error
				if stage != "queued" {
					d, err = f.store.AdmitDelivery(ctx, alice, deliveryRequest())
					if err != nil {
						t.Fatal(err)
					}
				}
				if stage == "accepted" || stage == "confirmed" {
					if _, err = f.store.ClaimDeliveryAttempt(ctx, alice, d.ID, runner); err != nil {
						t.Fatal(err)
					}
				}
				if stage == "confirmed" {
					if err = f.store.AcceptDelivery(ctx, alice, d.ID, task); err != nil {
						t.Fatal(err)
					}
				}
				f.store.commit = func(tx *sql.Tx) error {
					if committed {
						if err := tx.Commit(); err != nil {
							return err
						}
					}
					return errors.New("lost acknowledgement")
				}
				switch stage {
				case "queued":
					_, err = f.store.AdmitDelivery(ctx, alice, deliveryRequest())
				case "attempted":
					_, err = f.store.ClaimDeliveryAttempt(ctx, alice, d.ID, runner)
				case "accepted":
					err = f.store.AcceptDelivery(ctx, alice, d.ID, task)
				case "confirmed":
					err = f.store.ConfirmDelivery(ctx, alice, d.ID, task, 42, d.Artifact.SHA256)
				}
				if !errors.Is(err, ErrCommitUncertain) {
					t.Fatal("uncertain outcome guessed", err)
				}
				reopenDelivery(t, f)
				known, err := f.store.FindDelivery(ctx, alice, deliveryRequest().IdempotencyKey)
				if stage == "queued" && !committed {
					if !errors.Is(err, ErrUnavailable) {
						t.Fatal("uncommitted admission became work", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				want := stage
				if !committed {
					want = map[string]string{"attempted": "queued", "accepted": "attempted", "confirmed": "accepted"}[stage]
				}
				if known.State != want {
					t.Fatal("wrong persisted outcome", known.State, want)
				}
				if known.AttemptedAtMS != 0 {
					if _, err = f.store.LoadDeliveryAttempt(ctx, alice, known.ID, runner); err != nil {
						t.Fatal("exact attempt readback failed", err)
					}
					if _, err = f.store.LoadDeliveryAttempt(ctx, alice, known.ID, successor); !errors.Is(err, ErrConflict) {
						t.Fatal("other runner inherited attempt", err)
					}
					if _, err = f.store.ClaimDeliveryAttempt(ctx, alice, known.ID, successor); !errors.Is(err, ErrConflict) {
						t.Fatal("persisted attempt became retryable", err)
					}
				}
				t.Log("reopened exact receipt/runner; no network authority returned after uncertain commit", known.State)
			})
		}
	}
}

func TestDeliveryConcurrentClaimAtMostOneAttempt(t *testing.T) {
	f, d := readyDelivery(t)
	ctx := context.Background()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, who := range []string{runner, successor} {
		go func(who string) {
			<-start
			_, err := f.store.ClaimDeliveryAttempt(ctx, alice, d.ID, who)
			results <- err
		}(who)
	}
	close(start)
	winners := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatal("claim must have exactly one winner", winners)
	}
	reopenDelivery(t, f)
	if d, err := f.store.ReadDelivery(ctx, alice, d.ID); err != nil || d.State != "attempted" || d.RunnerID == "" {
		t.Fatal(d, err)
	}
}

func TestDeliveryDestinationDrift(t *testing.T) {
	for _, mode := range []string{"disabled", "expired", "revision", "output", "owner"} {
		t.Run(mode, func(t *testing.T) {
			f, d := readyDelivery(t)
			owner := alice
			switch mode {
			case "disabled":
				must(t, f.state, `UPDATE principals SET disabled_at_ms=2000 WHERE id=?`, alice)
			case "expired":
				f.now = d.ExpiresAtMS
			case "revision":
				must(t, f.docs, `DELETE FROM published_outputs WHERE document_id=?`, docA)
				must(t, f.docs, `UPDATE documents SET revision=revision+1 WHERE id=?`, docA)
			case "output":
				must(t, f.docs, `DELETE FROM published_outputs WHERE document_id=?`, docA)
			case "owner":
				owner = bob
			}
			if _, err := f.store.ClaimDeliveryAttempt(context.Background(), owner, d.ID, runner); err == nil {
				t.Fatal("drift authorized upload")
			}
			if scalar(t, f.docs, `SELECT count(*) FROM deliveries WHERE attempted_at_ms IS NOT NULL`) != 0 {
				t.Fatal("blocked upload acquired attempt")
			}
		})
	}
}

func TestDeliverySemanticAdmissionRace(t *testing.T) {
	f, d := readyDelivery(t)
	var wg sync.WaitGroup
	for _, key := range []string{"request_delivery_race1", "request_delivery_race2"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			r := deliveryRequest()
			r.IdempotencyKey = key
			got, err := f.store.AdmitDelivery(context.Background(), alice, r)
			if err != nil || got.ID != d.ID {
				t.Error("semantic race created second send", got.ID, err)
			}
		}(key)
	}
	wg.Wait()
	if scalar(t, f.docs, `SELECT count(*) FROM deliveries`) != 1 {
		t.Fatal("semantic fence lost race")
	}
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
