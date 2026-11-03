package workwatch

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPulseFailureCancelsAndJoinsWork(t *testing.T) {
	rejected := errors.New("job cancelled")
	var calls atomic.Int32
	completed := false
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := Run(ctx, time.Millisecond, func(context.Context) error {
		if calls.Add(1) == 1 {
			return nil
		}
		return rejected
	}, func(ctx context.Context) error { <-ctx.Done(); completed = true; return ctx.Err() })
	if !errors.Is(err, rejected) || !completed || calls.Load() != 2 {
		t.Fatal("failed pulse did not stop/join work")
	}
}
func TestCompletionCancelsAndJoinsBlockedPulse(t *testing.T) {
	var calls atomic.Int32
	entered, exited := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := Run(ctx, time.Millisecond, func(ctx context.Context) error {
		if calls.Add(1) == 1 {
			return nil
		}
		close(entered)
		<-ctx.Done()
		close(exited)
		return ctx.Err()
	}, func(context.Context) error { <-entered; return nil })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("monitor not joined")
	}
}
func TestInitialFailureNeverStartsWork(t *testing.T) {
	rejected := errors.New("disabled principal")
	err := Run(context.Background(), time.Second, func(context.Context) error { return rejected }, func(context.Context) error { t.Fatal("unauthorized work started"); return nil })
	if !errors.Is(err, rejected) {
		t.Fatal("initial failure lost")
	}
}
func TestWorkErrorAndParentCancellation(t *testing.T) {
	workErr := errors.New("processing failed")
	if err := Run(context.Background(), time.Second, func(context.Context) error { return nil }, func(context.Context) error { return workErr }); !errors.Is(err, workErr) {
		t.Fatal("work error lost")
	}
	ctx, cancel := context.WithCancel(context.Background())
	err := Run(ctx, time.Second, func(context.Context) error { return nil }, func(ctx context.Context) error { cancel(); <-ctx.Done(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal("parent cancellation lost")
	}
}
