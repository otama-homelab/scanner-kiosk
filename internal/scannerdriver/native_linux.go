//go:build linux

package scannerdriver

import (
	"context"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scanner/sane"
	"time"
)

type native struct{ adapter *sane.Adapter }

func Native(adapter *sane.Adapter) (Backend, error) {
	if adapter == nil {
		return nil, ErrInvalid
	}
	return &native{adapter}, nil
}
func (n *native) DeviceID() string { return n.adapter.DeviceID() }
func inspectionResult(attempt *sane.ProbeAttempt, err error) Inspection {
	if attempt != nil {
		return attempt
	}
	// The concrete adapter returns nil only before allocating/starting native work.
	if err != nil {
		return &noStart{at: time.Now().UnixMilli()}
	}
	return nil
}
func (n *native) InspectSelection(ctx context.Context, selection sane.Selection) (sane.Capabilities, Inspection, error) {
	caps, attempt, err := n.adapter.ProbeSelection(ctx, selection)
	return caps, inspectionResult(attempt, err), err
}
func (n *native) InspectParameters(ctx context.Context, selection sane.Selection) (sane.Parameters, Inspection, error) {
	params, attempt, err := n.adapter.ProbeParameters(ctx, selection)
	return params, inspectionResult(attempt, err), err
}
func (n *native) Capture(ctx context.Context, plan scanner.Plan) (scanner.Attempt, error) {
	attempt, err := n.adapter.Capture(ctx, plan)
	if attempt == nil && err != nil {
		return &noStart{at: time.Now().UnixMilli()}, err
	}
	return attempt, err
}
