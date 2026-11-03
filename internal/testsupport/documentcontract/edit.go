// Package documentcontract checks backend-independent document command behavior.
package documentcontract

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"scanner-kiosk/internal/documenthttp"
	"scanner-kiosk/internal/documents"
)

// Fixture starts with two enabled owners and one review document owned by Owner,
// revision 1, two unrotated pages, and no active work. Callbacks affect the actual
// backend authority/clock, not a wrapper that fabricates authorization failures.
type Fixture struct {
	Records                     documenthttp.Records
	OpenPeerRecords             func() documenthttp.Records
	ReopenRecords               func() documenthttp.Records
	Owner, OtherOwner, Document string
	Pages                       [2]string
	Expire                      func()
	Disable                     func()
	FailCommit                  func(committed bool) (restore func())
}

// RunEdits covers the public edit/read contract, not worker leases, migration,
// file durability or uncertain commits. Each case needs a fresh isolated backend.
func RunEdits(t *testing.T, newFixture func(*testing.T) Fixture) {
	t.Helper()
	ctx := context.Background()
	request := func(f Fixture, key string, revision int64, rotation int) documents.EditRequest {
		return documents.EditRequest{DocumentID: f.Document, Revision: revision, IdempotencyKey: key, Operation: "rotate", PageID: f.Pages[0], Rotation: rotation}
	}
	t.Run("owner isolation", func(t *testing.T) {
		f := newFixture(t)
		before, err := f.Records.ReadDocument(ctx, f.Owner, f.Document)
		if err != nil || before.Revision != 1 || before.PageCount != 2 {
			t.Fatal("invalid fixture", before, err)
		}
		if _, err = f.Records.ReadDocument(ctx, f.OtherOwner, f.Document); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("foreign read", err)
		}
		if _, err = f.Records.Edit(ctx, f.OtherOwner, request(f, "contract_foreign_001", 1, 90), time.Hour); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("foreign edit", err)
		}
		rows, next, err := f.Records.ReadHistory(ctx, f.OtherOwner, 10, nil)
		if err != nil || len(rows) != 0 || next != nil {
			t.Fatal("foreign history", rows, next, err)
		}
		after, err := f.Records.ReadDocument(ctx, f.Owner, f.Document)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("foreign operation changed document", err)
		}
	})
	t.Run("replay revision and retention", func(t *testing.T) {
		f := newFixture(t)
		before, err := f.Records.ReadDocument(ctx, f.Owner, f.Document)
		if err != nil {
			t.Fatal(err)
		}
		first := request(f, "contract_rotate_001", 1, 90)
		result, err := f.Records.Edit(ctx, f.Owner, first, time.Hour)
		if err != nil || result.Revision != 2 || len(result.Pages) != 2 || result.Pages[0].Rotation != 90 || !result.ExpiresAt.Equal(before.ExpiresAt) {
			t.Fatal("edit contract", result, err)
		}
		if _, err = f.Records.Edit(ctx, f.Owner, request(f, "contract_rotate_002", 2, 180), time.Hour); err != nil {
			t.Fatal(err)
		}
		replay, err := f.Records.Edit(ctx, f.Owner, first, time.Hour)
		if err != nil || !reflect.DeepEqual(result, replay) {
			t.Fatal("historical replay changed", replay, err)
		}
		changed := first
		changed.Rotation = 270
		if _, err = f.Records.Edit(ctx, f.Owner, changed, time.Hour); !errors.Is(err, documents.ErrIdempotencyConflict) {
			t.Fatal("changed intent reused receipt", err)
		}
		changed.IdempotencyKey = "contract_stale_001"
		if _, err = f.Records.Edit(ctx, f.Owner, changed, time.Hour); !errors.Is(err, documents.ErrRevision) {
			t.Fatal("stale revision accepted", err)
		}
	})
	t.Run("concurrent revision", func(t *testing.T) {
		f := newFixture(t)
		peer := f.OpenPeerRecords()
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for i, key := range []string{"contract_race_001", "contract_race_002"} {
			records := f.Records
			if i == 1 {
				records = peer
			}
			wg.Add(1)
			go func(key string, rotation int) {
				defer wg.Done()
				<-start
				_, err := records.Edit(ctx, f.Owner, request(f, key, 1, rotation), time.Hour)
				results <- err
			}(key, (i+1)*90)
		}
		close(start)
		wg.Wait()
		close(results)
		success, conflict := 0, 0
		for err := range results {
			if err == nil {
				success++
			} else if errors.Is(err, documents.ErrRevision) {
				conflict++
			} else {
				t.Fatal("unexpected concurrent result", err)
			}
		}
		current, err := f.Records.ReadDocument(ctx, f.Owner, f.Document)
		if err != nil || success != 1 || conflict != 1 || current.Revision != 2 || current.PageCount != 2 {
			t.Fatal("revision was not atomic", success, conflict, current, err)
		}
	})
	for _, mode := range []string{"expired", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			intent := request(f, "contract_revoke_001", 1, 90)
			if _, err := f.Records.Edit(ctx, f.Owner, intent, time.Hour); err != nil {
				t.Fatal(err)
			}
			if mode == "expired" {
				f.Expire()
			} else {
				f.Disable()
			}
			if _, err := f.Records.ReadDocument(ctx, f.Owner, f.Document); !errors.Is(err, documents.ErrUnavailable) {
				t.Fatal("revoked read", err)
			}
			rows, next, err := f.Records.ReadHistory(ctx, f.Owner, 10, nil)
			if mode == "expired" {
				if err != nil || len(rows) != 0 || next != nil {
					t.Fatal("expired history", rows, next, err)
				}
			} else if !errors.Is(err, documents.ErrUnavailable) {
				t.Fatal("disabled history", err)
			}
			if _, err := f.Records.Edit(ctx, f.Owner, intent, time.Hour); !errors.Is(err, documents.ErrGone) && !errors.Is(err, documents.ErrUnavailable) {
				t.Fatal("revoked replay", err)
			}
			if _, err := f.Records.Edit(ctx, f.Owner, request(f, "contract_revoke_002", 2, 180), time.Hour); !errors.Is(err, documents.ErrUnavailable) {
				t.Fatal("revoked new edit", err)
			}
		})
	}
}
