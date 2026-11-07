//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"scanner-kiosk/internal/config"
	"time"

	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannermetadata"
)

type scannerMetadataBuilder struct {
	inspector scannermetadata.Inspector
	ids       []string
	cached    scannermetadata.Snapshot
	checked   time.Time
	cursor    int
}

func (b *scannerMetadataBuilder) catalog(ctx context.Context) (scannermetadata.Snapshot, error) {
	if b.cursor == 0 && b.cached.Devices != nil && time.Since(b.checked) < time.Minute {
		return b.cached, nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	const perDevice = 60
	index := b.cursor
	id := b.ids[index/perDevice]
	tuple := index % perDevice
	capture := scanner.Capture{
		Size:  []string{"a4", "a5", "a6", "letter", "photo10x15"}[tuple/12],
		Color: []string{"color", "gray"}[tuple/6%2],
		DPI:   []int{300, 600, 200, 150, 100, 75}[tuple%6],
	}
	profile, err := b.inspector.InspectCapture(ctx, id, capture)
	if errors.Is(err, scanner.ErrUnresolved) {
		return scannermetadata.Snapshot{}, err
	}
	if ctx.Err() != nil {
		return scannermetadata.Snapshot{}, ctx.Err()
	}
	if err == nil && profile.Capture != capture {
		return scannermetadata.Snapshot{}, scannermetadata.ErrUnavailable
	}
	// Copy before updating: the publisher may still be reading the previous result.
	out := scannermetadata.Snapshot{Devices: []scannermetadata.Device{}}
	device := scannermetadata.Device{ID: id, Profiles: []scanner.Profile{}}
	for _, old := range b.cached.Devices {
		if old.ID != id {
			out.Devices = append(out.Devices, old)
			continue
		}
		for _, previous := range old.Profiles {
			if previous.Capture != capture {
				device.Profiles = append(device.Profiles, previous)
			}
		}
	}
	if err == nil {
		device.Profiles = append(device.Profiles, profile)
	}
	if len(device.Profiles) > 0 {
		out.Devices = append(out.Devices, device)
	}
	b.cursor = (index + 1) % (len(b.ids) * perDevice)
	if b.cursor == 0 {
		b.checked = time.Now()
	}
	b.cached = out
	return out, nil
}

// metadataResult carries verified progress for one device.
type metadataResult struct {
	id       string
	complete bool
	snapshot scannermetadata.Snapshot
	err      error
}

// Native inspection uses a bounded pool. Publication/heartbeat do not wait for
// a slow device; the driver's per-device gate still serializes capture and probe.
func (p *catalogPublisher) startScannerMetadata(inspector scannermetadata.Inspector, ids []string, lifetime time.Duration) {
	if inspector == nil {
		return
	}
	ids = append([]string(nil), ids...)
	jobs := make(chan *scannerMetadataBuilder, 32)
	results := make(chan metadataResult, 32)
	builders := make(map[string]*scannerMetadataBuilder, len(ids))
	for _, id := range ids {
		builders[id] = &scannerMetadataBuilder{inspector: inspector, ids: []string{id}}
	}
	workers := min(4, max(1, len(ids)))
	if _, ok := inspector.(interface{ IDs() []string }); ok {
		workers = 4
	}
	p.wg.Add(workers + 1)
	for range workers {
		go func() {
			defer p.wg.Done()
			for {
				select {
				case <-p.ctx.Done():
					return
				case builder := <-jobs:
					if p.ctx.Err() != nil {
						return
					}
					id := builder.ids[0]
					snapshot, err := builder.catalog(p.ctx)
					// Retain failures discovered during shutdown before reporting completion.
					if errors.Is(err, scanner.ErrUnresolved) {
						p.fail(errors.Join(ErrReconciliation, err))
					}
					results <- metadataResult{id: id, complete: builder.cursor == 0, snapshot: snapshot, err: err}
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
			if inventory, ok := inspector.(interface{ IDs() []string }); ok {
				ids = inventory.IDs()
			}
			for _, id := range ids {
				if !pending[id] && !now.Before(next[id]) {
					pending[id] = true
					builder := builders[id]
					if builder == nil {
						builder = &scannerMetadataBuilder{inspector: inspector, ids: []string{id}}
						builders[id] = builder
					}
					jobs <- builder
				}
			}
		}
		publish := func(now time.Time) error {
			out := scannerMetadataSnapshot(ids, cached, now, interval+2*time.Second)
			if inventory, ok := inspector.(interface{ Devices() []config.Scanner }); ok {
				for _, device := range inventory.Devices() {
					for index := range out.Devices {
						if out.Devices[index].ID == device.ID {
							out.Devices[index].Name = device.Name
						}
					}
				}
			}
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
					next[result.id] = now
					if result.complete {
						next[result.id] = now.Add(time.Minute)
					}
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
