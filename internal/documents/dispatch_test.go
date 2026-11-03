package documents

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestClaimNextRenderIsExclusive(t *testing.T) {
	f := renderFixture(t)
	admission, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for _, worker := range []string{runner, "worker_competing_001"} {
		wait.Add(1)
		go func(worker string) {
			defer wait.Done()
			claimed, err := f.store.ClaimNextRender(context.Background(), worker)
			if err == nil && (claimed.OwnerID != alice || claimed.Job.ID != admission.JobID) {
				results <- errors.New("wrong claimed work")
				return
			}
			results <- err
		}(worker)
	}
	wait.Wait()
	close(results)
	successes, idle := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrNoWork) {
			idle++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || idle != 1 {
		t.Fatal("job dispatched twice")
	}
}
func TestDispatchSkipsUnavailableAndPreservesUncertainWork(t *testing.T) {
	for _, mode := range []string{"disabled", "expired", "cancelled", "old-running", "old-reconciling", "old-requeued", "quota"} {
		t.Run(mode, func(t *testing.T) {
			f := renderFixture(t)
			admission, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
			case "expired":
				f.now = 10000
			case "cancelled":
				if err = f.store.RequestCancel(context.Background(), alice, admission.JobID); err != nil {
					t.Fatal(err)
				}
			case "old-running", "old-reconciling", "old-requeued":
				if _, err = f.store.ClaimExport(context.Background(), alice, admission.JobID, "worker_previous_001"); err != nil {
					t.Fatal(err)
				}
				if mode == "old-requeued" {
					must(t, f.docs, "UPDATE jobs SET state='queued',runner_id=NULL WHERE id=?", admission.JobID)
				}
				if mode == "old-reconciling" {
					must(t, f.docs, "UPDATE jobs SET state='reconciling' WHERE id=?", admission.JobID)
				}
			case "quota":
				f.store.space = func(context.Context) (int64, error) { return 0, nil }
			}
			claimed, err := f.store.ClaimNextRender(context.Background(), runner)
			if err == nil || claimed.Job.ID != "" {
				t.Fatal("unavailable work claimed")
			}
			if mode == "quota" && !errors.Is(err, ErrQuota) {
				t.Fatalf("wrong capacity error: %v", err)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", admission.JobID) != 1 {
				t.Fatal("selection freed reservation")
			}
		})
	}
}
func TestDispatchSkipsDisabledOwnerAndFindsEnabledWork(t *testing.T) {
	f := renderFixture(t)
	ctx := context.Background()
	if _, err := f.store.AdmitRender(ctx, alice, admissionRequest(), admissionLimits()); err != nil {
		t.Fatal(err)
	}
	must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
	must(t, f.docs, "UPDATE documents SET state='review' WHERE id=?", docB)
	seedPage(t, f, docB, bob, "original_bob_00001", "page_bob_00000001")
	request := admissionRequest()
	request.DocumentID = docB
	expected, err := f.store.AdmitRender(ctx, bob, request, admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := f.store.ClaimNextRender(ctx, runner)
	if err != nil || claimed.OwnerID != bob || claimed.Job.ID != expected.JobID {
		t.Fatalf("enabled work starved: %v", err)
	}
}
