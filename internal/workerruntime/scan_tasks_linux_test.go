//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"scanner-kiosk/internal/documents"
	"testing"
	"time"
)

func TestScanTasksCloseRetainsLateFailureAndJoinsAll(t *testing.T) {
	sentinel := errors.New("late cleanup failure")
	entered := make(chan struct{}, 2)
	tasks := newScanTasks(context.Background(), 2, func(ctx context.Context, claimed documents.ClaimedScan) error {
		entered <- struct{}{}
		<-ctx.Done()
		if claimed.Job.ID == "first" {
			return sentinel
		}
		return nil
	})
	tasks.start(documents.ClaimedScan{Job: documents.Job{ID: "first"}})
	tasks.start(documents.ClaimedScan{Job: documents.Job{ID: "second"}})
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("task did not start")
		}
	}
	if err := tasks.close(); !errors.Is(err, sentinel) || tasks.active != 0 {
		t.Fatal("lost late failure or unjoined task", err, tasks.active)
	}
}
func TestScanTaskFailureCancelsPeerWithoutLosingCause(t *testing.T) {
	sentinel := errors.New("capture reconciliation required")
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	tasks := newScanTasks(context.Background(), 2, func(ctx context.Context, claimed documents.ClaimedScan) error {
		entered <- struct{}{}
		if claimed.Job.ID == "first" {
			<-release
			return sentinel
		}
		<-ctx.Done()
		return nil
	})
	tasks.start(documents.ClaimedScan{Job: documents.Job{ID: "first"}})
	tasks.start(documents.ClaimedScan{Job: documents.Job{ID: "second"}})
	for range 2 {
		<-entered
	}
	close(release)
	select {
	case <-tasks.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("peer was not cancelled")
	}
	if err := tasks.close(); !errors.Is(err, sentinel) || tasks.active != 0 {
		t.Fatal(err, tasks.active)
	}
}
