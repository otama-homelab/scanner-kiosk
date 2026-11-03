// Package scannerreadiness describes trusted worker-side handoff evidence.
// It provides no producer: neither a browser field, an environment opt-in,
// native capability nor an Idle response is exclusive physical writer fencing.
package scannerreadiness

import (
	"context"
	"scanner-kiosk/internal/scannermetadata"
	"time"
)

const MaxIdleAge = 30 * time.Second

// Evidence must come from a reconciler that has verified continuing exclusive
// ownership, local termination and a subsequent fresh physical idle observation.
// PhysicalID is the canonical identity of the verified hardware, not its URL
// or display name. HandoffID refers to the verified ongoing writer fence.
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
