//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannerreadiness"
	"time"
)

type applicationReadinessObserver interface {
	ObserveApplicationReadiness(context.Context, string, int64, int) (scannerreadiness.Evidence, error)
}

func (p *catalogPublisher) startScannerReadiness(observer applicationReadinessObserver, ids []string, capacity int, lifetime time.Duration) {
	if inventory, ok := observer.(interface{ IDs() []string }); ok {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			seen := map[string]bool{}
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				for _, id := range inventory.IDs() {
					if !seen[id] {
						seen[id] = true
						p.startScannerReadinessDevice(observer, id, capacity, lifetime)
					}
				}
				select {
				case <-p.ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
		return
	}
	for _, id := range ids {
		p.startScannerReadinessDevice(observer, id, capacity, lifetime)
	}
}
func (p *catalogPublisher) startScannerReadinessDevice(observer applicationReadinessObserver, id string, capacity int, lifetime time.Duration) {

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for p.ctx.Err() == nil {
			ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
			epoch, err := p.store.BeginScannerObservation(ctx, id)
			if err == nil {
				var proof scannerreadiness.Evidence
				proof, err = observer.ObserveApplicationReadiness(ctx, id, epoch, capacity)
				if err == nil {
					err = p.store.PublishReadiness(ctx, proof)
				}
			}
			cancel()
			if errors.Is(err, scanner.ErrUnresolved) && p.ctx.Err() == nil {
				p.fail(ErrReconciliation)
				return
			}
			timer := time.NewTimer(min(5*time.Second, lifetime/3))
			select {
			case <-p.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}
