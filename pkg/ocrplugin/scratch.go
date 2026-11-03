package ocrplugin

import (
	"context"
	"errors"
)

// ErrReconciliation means owned execution or cleanup could not be confirmed.
// Callers must retain resources rather than treat cancellation as proof of stop.
var ErrReconciliation = errors.New("OCR execution or cleanup requires reconciliation")

// ScratchAccess is an optional coordinator resource port for native providers.
// A scope derives input size from its own allowlist, never provider file paths.
type ScratchAccess interface {
	ReserveScratch(context.Context, string, int64, int64) (ScratchReservation, error)
}

// ScratchReservation is a planned native input/result/diagnostic allocation.
// ReleaseAfterCleanup is trusted adapter evidence: call only after owned work
// has stopped and its workspace was removed/synced. Implementations must make
// release idempotent; adapters should release once. It is not a filesystem quota.
type ScratchReservation interface {
	InputBytes() int64
	ReleaseAfterCleanup()
}
