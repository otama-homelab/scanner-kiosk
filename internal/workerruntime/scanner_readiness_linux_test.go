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
	published chan scannerreadiness.Evidence
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
