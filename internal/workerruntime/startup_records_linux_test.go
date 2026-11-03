//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
)

type startupContextKey struct{}
type startupMemoryRecords struct {
	afterRecovery func()
	t             *testing.T
	counts        [][5]int
	calls         []string
	round         int
	failAt        int
	failure       error
	recovery      bool
	cancel        context.CancelFunc
}

func (m *startupMemoryRecords) command(ctx context.Context, name string, stage, limit int) (int, error) {
	m.t.Helper()
	m.calls = append(m.calls, name)
	if ctx.Err() != nil || ctx.Value(startupContextKey{}) != "startup" {
		m.t.Fatal("lost bounded maintenance context")
	}
	deadline, ok := ctx.Deadline()
	remaining := time.Until(deadline)
	if !ok || remaining <= 0 || remaining > 5*time.Second {
		m.t.Fatal("unbounded maintenance")
	}
	if len(m.calls) == 1 && m.cancel != nil {
		m.cancel()
	}
	if m.failAt == len(m.calls) {
		return limit, m.failure
	}
	result := 0
	if m.round < len(m.counts) {
		result = m.counts[m.round][stage]
	}
	if stage == 4 {
		m.round++
	}
	return result, nil
}
func (m *startupMemoryRecords) QueueExpiredCleanup(ctx context.Context, limit int, retention time.Duration) (int, error) {
	if limit != 20 || retention != time.Hour {
		m.t.Fatal("expired policy changed")
	}
	return m.command(ctx, "expired", 0, limit)
}
func (m *startupMemoryRecords) AbortWithdrawnUnstartedScans(ctx context.Context, limit int) (int, error) {
	if limit != 100 {
		m.t.Fatal("scan batch changed")
	}
	return m.command(ctx, "scans", 1, limit)
}
func (m *startupMemoryRecords) AbortUnstartedRenders(ctx context.Context, limit int) (int, error) {
	if limit != 100 {
		m.t.Fatal("render batch changed")
	}
	return m.command(ctx, "renders", 2, limit)
}
func (m *startupMemoryRecords) PurgeCompletedCleanup(ctx context.Context, limit int, retention time.Duration) (int, error) {
	if limit != 100 || retention != time.Hour {
		m.t.Fatal("status policy changed")
	}
	return m.command(ctx, "retired", 3, limit)
}
func (m *startupMemoryRecords) PurgeExpiredRequestReceipts(ctx context.Context, limit int) (int, error) {
	if limit != 100 {
		m.t.Fatal("request batch changed")
	}
	return m.command(ctx, "requests", 4, limit)
}
func (m *startupMemoryRecords) RecoveryRequired(ctx context.Context) (bool, error) {
	m.calls = append(m.calls, "recovery")
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second {
		m.t.Fatal("unbounded recovery inspection")
	}
	if m.afterRecovery != nil {
		m.afterRecovery()
	}
	if m.failAt == len(m.calls) {
		return false, m.failure
	}
	return m.recovery, nil
}
func startupContext() context.Context {
	return context.WithValue(context.Background(), startupContextKey{}, "startup")
}

func TestStartupRecordsDrainEveryFullBatchBeforeRecovery(t *testing.T) {
	for stage, limit := range []int{20, 100, 100, 100, 100} {
		t.Run(fmt.Sprint(stage), func(t *testing.T) {
			first := [5]int{}
			first[stage] = limit
			m := &startupMemoryRecords{t: t, counts: [][5]int{first, {}}}
			if err := reconcileStartup(startupContext(), m, time.Hour); err != nil {
				t.Fatal(err)
			}
			expected := []string{"expired", "scans", "renders", "retired", "requests", "expired", "scans", "renders", "retired", "requests", "recovery"}
			if !reflect.DeepEqual(m.calls, expected) {
				t.Fatal("recovery checked before drain", m.calls)
			}
		})
	}
}
func TestStartupRecordsFailClosedBeforeNativeAuthority(t *testing.T) {
	for stage := 1; stage <= 6; stage++ {
		for _, failure := range []error{documents.ErrCommitUncertain, documents.ErrStorage, context.DeadlineExceeded} {
			t.Run(fmt.Sprintf("%d/%v", stage, failure), func(t *testing.T) {
				m := &startupMemoryRecords{t: t, failAt: stage, failure: failure}
				if err := reconcileStartup(startupContext(), m, time.Hour); !errors.Is(err, failure) {
					t.Fatal("storage failure hidden", err)
				}
				if len(m.calls) != stage {
					t.Fatal("continued after unconfirmed command", m.calls)
				}
			})
		}
	}
	for _, required := range []bool{false, true} {
		t.Run(fmt.Sprint(required), func(t *testing.T) {
			m := &startupMemoryRecords{t: t, recovery: required}
			err := reconcileStartup(startupContext(), m, time.Hour)
			if required && !errors.Is(err, ErrReconciliation) || !required && err != nil {
				t.Fatal("recovery barrier", err)
			}
		})
	}
}
func TestStartupRecordsRejectInvalidBatchCounts(t *testing.T) {
	for stage, limit := range []int{20, 100, 100, 100, 100} {
		for _, invalid := range []int{-1, limit + 1} {
			t.Run(fmt.Sprintf("%d/%d", stage, invalid), func(t *testing.T) {
				counts := [5]int{}
				counts[stage] = invalid
				m := &startupMemoryRecords{t: t, counts: [][5]int{counts}}
				if err := reconcileStartup(startupContext(), m, time.Hour); !errors.Is(err, documents.ErrStorage) {
					t.Fatal("invalid count accepted", err)
				}
				if len(m.calls) != stage+1 {
					t.Fatal("continued after invalid backend count", m.calls)
				}
			})
		}
	}
}
func TestStartupRecordsFinishBoundedBatchThenHonorCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(startupContext())
	defer cancel()
	m := &startupMemoryRecords{t: t, cancel: cancel, counts: [][5]int{{20, 100, 100, 100, 100}}}
	if err := reconcileStartup(ctx, m, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	if !reflect.DeepEqual(m.calls, []string{"expired", "scans", "renders", "retired", "requests"}) {
		t.Fatal("partial batch or work after canceled batch", m.calls)
	}
	m = &startupMemoryRecords{t: t}
	if err := reconcileStartup(ctx, m, time.Hour); !errors.Is(err, context.Canceled) || len(m.calls) != 0 {
		t.Fatal("already canceled startup touched backend", err, m.calls)
	}
}

func TestStartupRecordsCancellationAfterRecoveryReadDoesNotGrant(t *testing.T) {
	ctx, cancel := context.WithCancel(startupContext())
	defer cancel()
	records := &startupMemoryRecords{t: t, afterRecovery: cancel}
	if err := reconcileStartup(ctx, records, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled recovery read granted startup", err)
	}
	if len(records.calls) != 6 {
		t.Fatal(records.calls)
	}
}
