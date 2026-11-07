//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannerdiscovery"
	"scanner-kiosk/internal/scannerdriver"
	"time"
)

func (p *catalogPublisher) startScannerDiscovery(session *scannerdriver.Session) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for p.ctx.Err() == nil {
			candidates, err := scannerdiscovery.Browse(p.ctx)
			if err == nil {
				for _, candidate := range candidates {
					ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
					_, err = session.Discover(ctx, candidate)
					cancel()
					if errors.Is(err, scanner.ErrUnresolved) && p.ctx.Err() == nil {
						p.fail(ErrReconciliation)
						return
					}
					if p.ctx.Err() != nil {
						return
					}
				}
			}
			timer := time.NewTimer(15 * time.Second)
			select {
			case <-p.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}
