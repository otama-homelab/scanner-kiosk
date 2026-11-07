//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannerreadiness"
	"sync/atomic"
	"testing"
	"time"
)

type readinessAuthority struct {
	catalogAuthoritySpy
	published   chan scannerreadiness.Evidence
	withdrawn   chan string
	withdrawErr error
}

func (s *readinessAuthority) WithdrawReadiness(ctx context.Context, id string) error {
	if s.withdrawErr != nil {
		return s.withdrawErr
	}
	if s.withdrawn == nil {
		return nil
	}
	select {
	case s.withdrawn <- id:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestFailedReadinessWithdrawalStopsPublisher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	authority := &readinessAuthority{withdrawErr: errors.New("storage unavailable")}
	failures := make(chan error, 1)
	p := &catalogPublisher{ctx: ctx, cancel: cancel, store: authority, fail: func(err error) { failures <- err; cancel() }}
	observer := readinessObserverFunc(func(context.Context, string, int64, int) (scannerreadiness.Evidence, error) {
		return scannerreadiness.Evidence{}, errors.New("device unavailable")
	})
	p.startScannerReadiness(observer, []string{"scanner_readiness_001"}, 1, time.Hour)
	select {
	case err := <-failures:
		if !errors.Is(err, ErrCatalog) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Error("failed withdrawal did not stop publisher")
	}
	cancel()
	p.wg.Wait()
}

func (s *readinessAuthority) BeginScannerObservation(context.Context, string) (int64, error) {
	return 7, nil
}
func (s *readinessAuthority) PublishReadiness(ctx context.Context, p scannerreadiness.Evidence) error {
	select {
	case s.published <- p:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type readinessObserverFunc func(context.Context, string, int64, int) (scannerreadiness.Evidence, error)

func (f readinessObserverFunc) ObserveApplicationReadiness(ctx context.Context, id string, epoch int64, capacity int) (scannerreadiness.Evidence, error) {
	return f(ctx, id, epoch, capacity)
}

func TestFailedReadinessObservationWithdrawsPreviousEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	authority := &readinessAuthority{withdrawn: make(chan string, 1)}
	p := &catalogPublisher{ctx: ctx, cancel: cancel, store: authority, fail: func(error) { cancel() }}
	observer := readinessObserverFunc(func(context.Context, string, int64, int) (scannerreadiness.Evidence, error) {
		return scannerreadiness.Evidence{}, errors.New("device unavailable")
	})
	p.startScannerReadiness(observer, []string{"scanner_readiness_001"}, 1, time.Hour)
	select {
	case id := <-authority.withdrawn:
		if id != "scanner_readiness_001" {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Error("failed observation retained previous readiness")
	}
	cancel()
	p.wg.Wait()
}
func TestReadinessRetriesExternalBusyButStopsOnLocalUncertainty(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "busy", true: "uncertain"}[uncertain], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			authority := &readinessAuthority{published: make(chan scannerreadiness.Evidence, 1)}
			failures := make(chan error, 1)
			p := &catalogPublisher{ctx: ctx, cancel: cancel, store: authority, fail: func(err error) { failures <- err; cancel() }}
			var calls atomic.Int64
			observer := readinessObserverFunc(func(_ context.Context, id string, epoch int64, capacity int) (scannerreadiness.Evidence, error) {
				if calls.Add(1) == 1 {
					if uncertain {
						return scannerreadiness.Evidence{}, scanner.ErrUnresolved
					}
					return scannerreadiness.Evidence{}, errors.New("external busy")
				}
				return scannerreadiness.Evidence{DeviceID: id, LeaseEpoch: epoch, ScanConcurrency: capacity}, nil
			})
			p.startScannerReadiness(observer, []string{"scanner_readiness_001"}, 2, 30*time.Millisecond)
			select {
			case proof := <-authority.published:
				if uncertain || proof.LeaseEpoch != 7 || proof.ScanConcurrency != 2 || calls.Load() < 2 {
					t.Fatal("invalid renewal", proof)
				}
			case err := <-failures:
				if !uncertain || !errors.Is(err, ErrReconciliation) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("readiness did not advance")
			}
			cancel()
			p.wg.Wait()
		})
	}
}
