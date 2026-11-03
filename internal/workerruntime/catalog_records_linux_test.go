//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/workercatalog"
	"scanner-kiosk/internal/workeridentity"
)

type catalogStarterFunc func(context.Context, *workeridentity.Guard) (workercatalog.Publisher, error)

func (f catalogStarterFunc) BeginPublisher(ctx context.Context, g *workeridentity.Guard) (workercatalog.Publisher, error) {
	return f(ctx, g)
}

// This is a lifecycle spy, not a production backend or a lease/fencing proof.
type catalogAuthoritySpy struct {
	fail                        string
	pulses, publications, stops atomic.Int64
}

func (s *catalogAuthoritySpy) result(operation string) error {
	if s.fail == operation {
		return errors.New("injected backend failure")
	}
	return nil
}
func (s *catalogAuthoritySpy) Pulse(context.Context) error {
	s.pulses.Add(1)
	return s.result("pulse")
}
func (s *catalogAuthoritySpy) Publish(context.Context, renderadmission.CatalogSnapshot) error {
	s.publications.Add(1)
	return s.result("publish")
}
func (s *catalogAuthoritySpy) PublishScanners(context.Context, scannermetadata.Snapshot) error {
	return s.result("scanners")
}
func (s *catalogAuthoritySpy) Stop(context.Context) error {
	s.stops.Add(1)
	return s.result("stop")
}

func TestCatalogPublisherAcceptsBackendOwnedAuthorityWithoutSQL(t *testing.T) {
	for _, mode := range []string{"healthy", "optional", "pulse", "publish", "stop", "begin", "nil-authority"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			guard, err := workeridentity.Acquire(root)
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			spy := &catalogAuthoritySpy{fail: mode}
			starter := catalogStarterFunc(func(_ context.Context, received *workeridentity.Guard) (workercatalog.Publisher, error) {
				if received != guard {
					t.Fatal("guard replaced at backend boundary")
				}
				if mode == "begin" {
					return nil, errors.New("injected begin failure")
				}
				if mode == "nil-authority" {
					return nil, nil
				}
				return spy, nil
			})
			builder := catalogBuilderFunc(func(context.Context) (renderadmission.CatalogSnapshot, error) {
				if mode == "optional" {
					return renderadmission.CatalogSnapshot{}, errors.New("optional provider unavailable")
				}
				return renderadmission.CatalogSnapshot{}, nil
			})
			publisher, err := startCatalogPublisher(ctx, cancel, starter, guard, builder, 30*time.Millisecond)
			if mode == "begin" || mode == "nil-authority" {
				if !errors.Is(err, ErrCatalog) || publisher != nil || spy.stops.Load() != 0 {
					t.Fatal("invalid begin accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "pulse" || mode == "publish" {
				<-ctx.Done()
				if !errors.Is(publisher.failure(), ErrCatalog) {
					t.Fatal("backend authority loss did not stop worker")
				}
			} else {
				for spy.pulses.Load() == 0 || (mode != "optional" && spy.publications.Load() == 0) {
					if ctx.Err() != nil {
						t.Fatal("backend commands did not run")
					}
					time.Sleep(time.Millisecond)
				}
				if ctx.Err() != nil || publisher.failure() != nil {
					t.Fatal("healthy authority or optional provider stopped worker")
				}
				if mode == "optional" && spy.publications.Load() != 0 {
					t.Fatal("unavailable provider was published")
				}
			}
			err = publisher.close()
			if (mode == "pulse" || mode == "publish" || mode == "stop") != errors.Is(err, ErrCatalog) || spy.stops.Load() != 1 {
				t.Fatal("backend shutdown failure or withdrawal lost", err)
			}
			before := spy.pulses.Load()
			time.Sleep(20 * time.Millisecond)
			if spy.pulses.Load() != before {
				t.Fatal("heartbeat outlived joined close")
			}
		})
	}
}
