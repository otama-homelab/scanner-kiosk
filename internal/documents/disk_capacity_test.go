package documents

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestPhysicalCapacityFailureRollsBackScanAndRender(t *testing.T) {
	for _, operation := range []string{"scan", "render"} {
		for _, mode := range []string{"low", "error", "negative"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				var f *fixture
				if operation == "scan" {
					f = scanFixture(t)
				} else {
					f = renderFixture(t)
				}
				f.store.space = func(context.Context) (int64, error) {
					switch mode {
					case "error":
						return 0, errors.New("private-path-canary")
					case "negative":
						return -1, nil
					}
					return 49, nil
				}
				before := scalar(t, f.docs, "SELECT count(*) FROM documents")
				var err error
				if operation == "scan" {
					_, err = f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
				} else {
					_, err = f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
				}
				if err == nil || strings.Contains(err.Error(), "private-path-canary") {
					t.Fatal("unavailable physical capacity admitted or leaked detail")
				}
				if scalar(t, f.docs, "SELECT count(*) FROM jobs") != 0 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 || scalar(t, f.docs, "SELECT count(*) FROM request_receipts") != 0 || scalar(t, f.docs, "SELECT count(*) FROM documents") != before {
					t.Fatal("capacity failure partially committed")
				}
			})
		}
	}
}
func TestReplayDoesNotRequireNewPhysicalCapacity(t *testing.T) {
	f := renderFixture(t)
	first, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	f.store.space = func(context.Context) (int64, error) { t.Fatal("replay probed filesystem capacity"); return 0, nil }
	again, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
	if err != nil || !again.Replay || again.JobID != first.JobID {
		t.Fatal("replay created or rejected prior work")
	}
}
func TestConcurrentReservationsSharePhysicalCapacity(t *testing.T) {
	f := renderFixture(t)
	must(t, f.docs, "UPDATE documents SET state='review' WHERE id=?", docB)
	seedPage(t, f, docB, bob, "original_bob_000001", "page_bob_00000001")
	other := secondStore(t, f)
	probe := func(context.Context) (int64, error) { return 90, nil }
	f.store.space = probe
	other.space = probe
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, store := range []*Store{f.store, other} {
		wg.Add(1)
		go func(i int, store *Store) {
			defer wg.Done()
			request := admissionRequest()
			owner := alice
			if i == 1 {
				request.DocumentID = docB
				owner = bob
			}
			_, err := store.AdmitRender(context.Background(), owner, request, admissionLimits())
			results <- err
		}(i, store)
	}
	wg.Wait()
	close(results)
	accepted, rejected := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrQuota) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || rejected != 1 || scalar(t, f.docs, "SELECT COALESCE(SUM(byte_size),0) FROM reservations") != 50 {
		t.Fatal("physical budget reserved twice")
	}
}
