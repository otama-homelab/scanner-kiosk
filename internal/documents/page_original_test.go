package documents

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPageOriginalOwnershipLifetimeAndMembership(t *testing.T) {
	f := editable(t)
	ctx := context.Background()
	original, err := f.store.ReadPageOriginal(ctx, alice, docA, pageOne)
	if err != nil || original.Artifact.ID != pageOne+"_original" || original.Artifact.MediaType != "image/png" || original.ExpiresAtMS != 10000 {
		t.Fatal(original, err)
	}
	if _, err = f.store.ReadPageOriginal(ctx, bob, docA, pageOne); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cross-owner original", err)
	}
	if _, err = f.store.ReadPageOriginal(ctx, alice, docB, pageOne); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cross-document page", err)
	}
	request := EditRequest{DocumentID: docA, Revision: 1, IdempotencyKey: "request_delete_01", Operation: "delete_page", PageID: pageOne}
	if _, err = f.store.Edit(ctx, alice, request, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ReadPageOriginal(ctx, alice, docA, pageOne); !errors.Is(err, ErrUnavailable) {
		t.Fatal("deleted page original", err)
	}
	f.now = 10000
	if _, err = f.store.ReadPageOriginal(ctx, alice, docA, pageTwo); !errors.Is(err, ErrUnavailable) {
		t.Fatal("expired page", err)
	}
}
func TestPageOriginalDisabledOrDeletingDenied(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		f := editable(t)
		if disabled {
			must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
		} else {
			must(t, f.docs, "UPDATE documents SET state='deleting' WHERE id=?", docA)
		}
		if _, err := f.store.ReadPageOriginal(context.Background(), alice, docA, pageOne); !errors.Is(err, ErrUnavailable) {
			t.Fatal("revoked page readable", err)
		}
	}
}
