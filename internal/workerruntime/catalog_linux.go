//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/workercatalog"
	"scanner-kiosk/internal/workeridentity"
	"sync"
	"time"
)

var ErrCatalog = errors.New("worker catalog authority unavailable")

type catalogBuilder interface {
	catalog(context.Context) (renderadmission.CatalogSnapshot, error)
}
type catalogStarter interface {
	BeginPublisher(context.Context, *workeridentity.Guard) (workercatalog.Publisher, error)
}
type catalogPublisher struct {
	cancel context.CancelFunc
	ctx    context.Context
	fail   func(error)
	wg     sync.WaitGroup
	mu     sync.Mutex
	err    error
	store  workercatalog.Publisher
}

func startCatalogPublisher(parent context.Context, cancelWorker context.CancelFunc, starter catalogStarter, guard *workeridentity.Guard, builder catalogBuilder, lifetime time.Duration) (*catalogPublisher, error) {
	if starter == nil || cancelWorker == nil || builder == nil || lifetime < 3*time.Millisecond || lifetime > 5*time.Minute {
		return nil, ErrCatalog
	}
	store, err := starter.BeginPublisher(parent, guard)
	if err != nil || store == nil {
		return nil, ErrCatalog
	}
	ctx, cancel := context.WithCancel(parent)
	p := &catalogPublisher{cancel: cancel, ctx: ctx, store: store}
	fail := func(err error) {
		p.mu.Lock()
		if p.err == nil {
			p.err = err
		} else if !errors.Is(p.err, err) {
			p.err = errors.Join(p.err, err)
		}
		p.mu.Unlock()
		cancelWorker()
		cancel()
	}
	p.fail = fail
	interval := lifetime / 3
	p.wg.Add(2)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := store.Pulse(ctx); err != nil {
					if ctx.Err() == nil {
						fail(ErrCatalog)
					}
					return
				}
			}
		}
	}()
	go func() {
		defer p.wg.Done()
		for {
			if ctx.Err() != nil {
				return
			}
			snapshot, err := builder.catalog(ctx)
			if err == nil {
				if err = store.Publish(ctx, snapshot); err != nil {
					if ctx.Err() == nil {
						fail(ErrCatalog)
					}
					return
				}
			}
			// Optional provider initialization failure leaves OCR unavailable. It must
			// neither publish a fabricated model nor stop ordinary processing.
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return p, nil
}
func (p *catalogPublisher) failure() error { p.mu.Lock(); defer p.mu.Unlock(); return p.err }
func (p *catalogPublisher) close() error {
	p.cancel()
	p.wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.store.Stop(ctx); err != nil {
		return errors.Join(ErrCatalog, p.failure())
	}
	return p.failure()
}
