// Package scannerreadiness describes trusted worker-side handoff evidence.
// Production evidence covers the shared-root application writer and observed
// external busy state; it does not establish LAN-wide physical exclusion.
package scannerreadiness

import (
	"context"
	"scanner-kiosk/internal/scannermetadata"
	"time"
)

const MaxIdleAge = 30 * time.Second

// Evidence must come from the guarded session after joining local operations
// and observing the pinned physical identity and fresh physical Idle.
// PhysicalID is the canonical identity of the verified hardware, not its URL
// or display name. HandoffID identifies the ongoing application worker guard.
// Epoch zero means initialize a missing lease; otherwise it must match idle.
// There is deliberately no public HTTP/configuration decoder for this type.
type Evidence struct {
	DeviceID, HandoffID, PhysicalID                            string
	LeaseEpoch                                                 int64
	ScanConcurrency                                            int
	OwnershipConfirmedAtMS, LocalStoppedAtMS, IdleObservedAtMS int64
}

// State is an advisory operational projection, not acquisition permission.
type State string

const (
	Unknown     State = "unknown"
	Idle        State = "idle"
	Busy        State = "busy"
	Unavailable State = "unavailable"
)

type DeviceStatus struct {
	ID          string
	State       State
	ExpiresAtMS int64
}

// Snapshot binds native metadata and status to one repository transaction.
// Internal expiry/metadata are projected explicitly; never serialize this type.
type Snapshot struct {
	Metadata    scannermetadata.Snapshot
	Devices     []DeviceStatus
	ExpiresAtMS int64
}
type Source interface {
	ReadScannerReadiness(context.Context, string, []string, int64) (Snapshot, error)
}
