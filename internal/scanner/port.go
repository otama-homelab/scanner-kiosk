// Package scanner defines the capture boundary independently of a native driver.
// Admission/device leases and fresh physical-idle observation are coordinator
// responsibilities; local process exit alone must not release a scanner lease.
package scanner

import (
	"context"
	"errors"
	"scanner-kiosk/internal/captureimage"
)

// ErrUnresolved means local work/cleanup or a prior attempt is not reconciled.
// A caller must preserve the durable device lease and reservation.
var ErrUnresolved = errors.New("scanner work requires reconciliation")

// Capture identifies one complete setting combination. Independent lists of
// sizes/colors/DPI must not be treated as a supported Cartesian product.
type Capture struct {
	Size  string `json:"size"`
	Color string `json:"color"`
	DPI   int    `json:"dpi"`
}

// Profile is read-only native metadata, not permission to acquire or initialize
// a device lease. Pre-start dimensions remain best-effort; capture validates the
// actual image again and must never retry because geometry changed.
type Profile struct {
	Capture
	Width       int   `json:"width"`
	Height      int   `json:"height"`
	CheckedAtMS int64 `json:"checked_at_ms"`
}

// Plan is a capability-validated, immutable capture request, never raw CLI input.
// Width/height must come from the resolved backend geometry, not UI estimates.
type Plan struct {
	DeviceID, Size, Color string
	DPI, Width, Height    int
	ByteBudget            int64
}
type Artifact struct {
	ID, RelativePath string
	Image            captureimage.Info
}

// Attempt owns unpublished local output even on capture failure. Release follows
// confirmed publication/read-back; Close follows confirmed local stop and abort.
type Attempt interface {
	Artifact() (Artifact, error)
	StoppedAtMS() int64
	Release() error
	Close() error
}
type Capturer interface {
	Capture(context.Context, Plan) (Attempt, error)
}
