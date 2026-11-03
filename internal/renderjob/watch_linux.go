//go:build linux

package renderjob

import (
	"context"
	"errors"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/workwatch"
	"time"
)

type PulseRecords interface {
	PulseRender(context.Context, string, string, string) error
}

// PrepareWatched observes DB cancellation/expiry/ownership while generating PDF.
// It waits for synchronous work to return before cleanup. Native subprocess
// isolation and preparation-failure terminalization remain worker concerns.
func PrepareWatched(ctx context.Context, records Records, root, owner, job, runner string, options pdfassemble.Options, interval time.Duration) (*Prepared, error) {
	pulses, ok := records.(PulseRecords)
	if !ok {
		return nil, ErrUnsupported
	}
	var prepared *Prepared
	err := workwatch.Run(ctx, interval, func(ctx context.Context) error { return pulses.PulseRender(ctx, owner, job, runner) }, func(ctx context.Context) error {
		var err error
		prepared, err = Prepare(ctx, records, root, owner, job, runner, options)
		return err
	})
	if err != nil && prepared != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return nil, errors.Join(err, prepared.Finish(cleanup, "interrupted"))
	}
	return prepared, err
}
