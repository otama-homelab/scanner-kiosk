//go:build linux

package workercatalog

import (
	"context"

	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/scannerreadiness"
	"scanner-kiosk/internal/workeridentity"
)

// Publisher is one backend-owned authority, bound to its guard/boot/generation.
// Every operation rechecks that authority atomically; Stop withdraws metadata,
// never device leases or reservations. No SQL handle or forgeable lease escapes.
type Publisher interface {
	Pulse(context.Context) error
	Publish(context.Context, renderadmission.CatalogSnapshot) error
	PublishScanners(context.Context, scannermetadata.Snapshot) error
	BeginScannerObservation(context.Context, string) (int64, error)
	PublishReadiness(context.Context, scannerreadiness.Evidence) error
	WithdrawReadiness(context.Context, string) error
	Stop(context.Context) error
}

func (s *Store) BeginPublisher(ctx context.Context, guard *workeridentity.Guard) (Publisher, error) {
	lease, err := s.Begin(ctx, guard)
	if err != nil {
		return nil, err
	}
	return &sqlitePublisher{store: s, lease: lease}, nil
}

type sqlitePublisher struct {
	store *Store
	lease Lease
}

func (p *sqlitePublisher) Pulse(ctx context.Context) error { return p.store.Pulse(ctx, p.lease) }
func (p *sqlitePublisher) Publish(ctx context.Context, snapshot renderadmission.CatalogSnapshot) error {
	return p.store.Publish(ctx, p.lease, snapshot)
}
func (p *sqlitePublisher) PublishScanners(ctx context.Context, snapshot scannermetadata.Snapshot) error {
	return p.store.PublishScanners(ctx, p.lease, snapshot)
}
func (p *sqlitePublisher) Stop(ctx context.Context) error { return p.store.Stop(ctx, p.lease) }

func (p *sqlitePublisher) PublishReadiness(ctx context.Context, proof scannerreadiness.Evidence) error {
	_, err := p.store.PublishScannerReadiness(ctx, p.lease, proof)
	return err
}
