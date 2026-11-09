package documents

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNextDeliveryEligibility(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"queued", "attempted", "accepted", "unknown-task", "unknown-no-task", "poll-limit", "cancelled", "disabled-owner", "expired", "deleting", "destination", "fingerprint", "stale-revision", "missing-output", "processing"} {
		t.Run(mode, func(t *testing.T) {
			f, b := completedOCRCandidate(t)
			if _, err := f.store.AdoptOCRResult(ctx, alice, ocrResultRequest(b), time.Hour); err != nil {
				t.Fatal(err)
			}
			r := deliveryRequest()
			d, err := f.store.AdmitDelivery(ctx, alice, r)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "attempted" || mode == "accepted" || mode == "unknown-task" || mode == "unknown-no-task" || mode == "poll-limit" {
				if _, err = f.store.ClaimDelivery(ctx, alice, d.ID); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "accepted" || mode == "unknown-task" || mode == "poll-limit" {
				if err = f.store.AcceptDelivery(ctx, alice, d.ID, "12345678-1234-4234-8234-123456789abc"); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "unknown-task" || mode == "unknown-no-task" {
				if err = f.store.MarkDeliveryUnknown(ctx, alice, d.ID); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "poll-limit":
				for range 20 {
					if reserved, err := f.store.ReserveDeliveryPoll(ctx, alice, d.ID); err != nil || !reserved {
						t.Fatal("poll reservation", err)
					}
				}
				if reserved, err := f.store.ReserveDeliveryPoll(ctx, alice, d.ID); err != nil || reserved {
					t.Fatal("poll cap", err)
				}
			case "cancelled":
				if _, err = f.store.CancelDelivery(ctx, alice, d.ID); err != nil {
					t.Fatal(err)
				}
			case "disabled-owner":
				must(t, f.state, `UPDATE principals SET disabled_at_ms=2000 WHERE id=?`, alice)
			case "expired":
				f.store.now = func() time.Time { return time.UnixMilli(d.ExpiresAtMS) }
			case "deleting":
				must(t, f.docs, `UPDATE documents SET state='deleting' WHERE id=?`, docA)
			case "stale-revision":
				must(t, f.docs, `DELETE FROM published_outputs WHERE document_id=?`, docA)
				must(t, f.docs, `UPDATE documents SET revision=revision+1 WHERE id=?`, docA)
			case "missing-output":
				must(t, f.docs, `DELETE FROM published_outputs WHERE document_id=?`, docA)
			case "processing":
				must(t, f.docs, `UPDATE documents SET state='processing' WHERE id=?`, docA)
			case "destination":
				r.DestinationID = "different-archive"
			case "fingerprint":
				r.ConfigFingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			}
			candidate, err := f.store.NextDelivery(ctx, r.DestinationID, r.ConfigFingerprint)
			want := mode == "queued" || mode == "attempted" || mode == "accepted" || mode == "unknown-task"
			if want {
				if err != nil || candidate.DeliveryID != d.ID || candidate.OwnerID != alice {
					t.Fatal("eligible delivery missing", candidate, err)
				}
			} else if !errors.Is(err, ErrNoWork) {
				t.Fatal("ineligible delivery scheduled", candidate, err)
			}
		})
	}
}
