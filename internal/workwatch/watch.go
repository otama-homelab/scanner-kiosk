// Package workwatch propagates persisted job authorization loss to running work.
package workwatch

import (
	"context"
	"errors"
	"time"
)

var ErrInvalid = errors.New("invalid job watch configuration")

// Run validates before starting work, then periodically pulses until work returns.
// A failed pulse cancels the work context; Run waits for work and monitor exit.
// Work must honor cancellation. This is not a process kill/stop proof.
func Run(ctx context.Context, interval time.Duration, pulse func(context.Context) error, work func(context.Context) error) error {
	if interval < time.Millisecond || interval > time.Minute || pulse == nil || work == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := pulse(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	running, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	monitoring, stop := context.WithCancel(running)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-monitoring.Done():
				return
			case <-ticker.C:
				if err := pulse(monitoring); err != nil {
					// Completion-driven monitor cancellation is not authorization loss.
					if monitoring.Err() == nil {
						cancel(err)
					}
					return
				}
			}
		}
	}()
	err := work(running)
	stop()
	<-done
	return errors.Join(err, context.Cause(running))
}
