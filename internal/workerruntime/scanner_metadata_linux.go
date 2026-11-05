//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"time"

	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannermetadata"
)

type scannerMetadataBuilder struct {
	inspector scannermetadata.Inspector
	ids       []string
	cached    scannermetadata.Snapshot
	checked   time.Time
}

func (b *scannerMetadataBuilder) catalog(ctx context.Context) (scannermetadata.Snapshot, error) {
	if b.cached.Devices != nil && time.Since(b.checked) < time.Minute {
		return b.cached, nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out := scannermetadata.Snapshot{Devices: []scannermetadata.Device{}}
	for _, id := range b.ids {
		device := scannermetadata.Device{ID: id, Profiles: []scanner.Profile{}}
		for _, size := range []string{"a4", "a5", "a6", "letter", "photo10x15"} {
			for _, color := range []string{"color", "gray"} {
				for _, dpi := range []int{300, 600, 200, 150, 100, 75} {
					capture := scanner.Capture{Size: size, Color: color, DPI: dpi}
					profile, err := b.inspector.InspectCapture(ctx, id, capture)
					if errors.Is(err, scanner.ErrUnresolved) {
						return scannermetadata.Snapshot{}, err
					}
					if ctx.Err() != nil {
						return scannermetadata.Snapshot{}, ctx.Err()
					}
					if err != nil {
						continue
					} // This tuple is not advertised; never infer others.
					if profile.Capture != capture {
						return scannermetadata.Snapshot{}, scannermetadata.ErrUnavailable
					}
					device.Profiles = append(device.Profiles, profile)
				}
			}
		}
		if len(device.Profiles) > 0 {
			out.Devices = append(out.Devices, device)
		}
	}
	b.cached, b.checked = out, time.Now()
	return out, nil
}

// metadataResult contains only a completed per-device inspection; unfinished
// batches never replace verified data for a different device.
type metadataResult struct {
	id       string
	snapshot scannermetadata.Snapshot
	err      error
}

// Native inspection uses a bounded pool. Publication/heartbeat do not wait for
// a slow device; the driver's per-device gate still serializes capture and probe.
func (p *catalogPublisher) startScannerMetadata(inspector scannermetadata.Inspector, ids []string, lifetime time.Duration) {
	if inspector == nil || len(ids) == 0 {
		return
	}
	ids = append([]string(nil), ids...)
	jobs := make(chan string, len(ids))
	results := make(chan metadataResult, len(ids))
	workers := min(4, len(ids))
	p.wg.Add(workers + 1)
	for range workers {
		go func() {
			defer p.wg.Done()
			for {
				select {
				case <-p.ctx.Done():
					return
				case id := <-jobs:
					if p.ctx.Err() != nil {
						return
					}
					builder := scannerMetadataBuilder{inspector: inspector, ids: []string{id}}
					snapshot, err := builder.catalog(p.ctx)
					// Retain failures discovered during shutdown before reporting completion.
					if errors.Is(err, scanner.ErrUnresolved) {
						p.fail(errors.Join(ErrReconciliation, err))
					}
					results <- metadataResult{id, snapshot, err}
				}
			}
		}()
	}
	go func() {
		defer p.wg.Done()
		cached := map[string]scannermetadata.Device{}
		next := map[string]time.Time{}
		pending := map[string]bool{}
		interval := lifetime / 3
		ticker := time.NewTicker(min(time.Second, interval))
		defer ticker.Stop()
		var published time.Time
		schedule := func(now time.Time) {
			for _, id := range ids {
				if !pending[id] && !now.Before(next[id]) {
					pending[id] = true
					jobs <- id
				}
			}
		}
		publish := func(now time.Time) error {
			out := scannerMetadataSnapshot(ids, cached, now, interval+2*time.Second)
			if err := p.store.PublishScanners(p.ctx, out); err != nil {
				return err
			}
			published = now
			return nil
		}
		schedule(time.Now())
		for {
			if p.ctx.Err() != nil {
				return
			}
			select {
			case <-p.ctx.Done():
				return
			case result := <-results:
				if p.ctx.Err() != nil {
					return
				}
				delete(pending, result.id)
				now := time.Now()
				next[result.id] = now.Add(interval)
				delete(cached, result.id)
				if result.err == nil {
					if len(result.snapshot.Devices) == 1 {
						cached[result.id] = result.snapshot.Devices[0]
					}
					next[result.id] = now.Add(time.Minute)
				}
				if err := publish(now); err != nil {
					if p.ctx.Err() == nil {
						p.fail(ErrCatalog)
					}
					return
				}
				schedule(now)
			case now := <-ticker.C:
				if published.IsZero() || now.Sub(published) >= interval {
					if err := publish(now); err != nil {
						if p.ctx.Err() == nil {
							p.fail(ErrCatalog)
						}
						return
					}
				}
				schedule(now)
			}
		}
	}()
}

// A device's oldest observation must outlive the next publication interval.
// Removing it never refreshes its timestamp or removes another device's data.
func scannerMetadataSnapshot(ids []string, cached map[string]scannermetadata.Device, now time.Time, horizon time.Duration) scannermetadata.Snapshot {
	out := scannermetadata.Snapshot{Devices: []scannermetadata.Device{}}
	for _, id := range ids {
		device, ok := cached[id]
		if !ok {
			continue
		}
		fresh := true
		for _, profile := range device.Profiles {
			if profile.CheckedAtMS <= now.Add(-scannermetadata.MaxAge+horizon).UnixMilli() {
				fresh = false
				break
			}
		}
		if !fresh {
			delete(cached, id)
			continue
		}
		out.Devices = append(out.Devices, device)
	}
	return out
}
