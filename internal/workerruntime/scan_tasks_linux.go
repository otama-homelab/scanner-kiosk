//go:build linux

package workerruntime

import (
	"context"
	"errors"

	"scanner-kiosk/internal/documents"
)

// scanTasks is owned by the runtime loop. Only task completion crosses a
// goroutine boundary; claim, active-count and error aggregation stay serialized.
// Close cancels and joins every task before stores/configuration/guard can close.
type scanTasks struct {
	ctx           context.Context
	cancel        context.CancelFunc
	results       chan error
	active, limit int
	err           error
	execute       func(context.Context, documents.ClaimedScan) error
}

func newScanTasks(ctx context.Context, limit int, execute func(context.Context, documents.ClaimedScan) error) *scanTasks {
	taskCtx, cancel := context.WithCancel(ctx)
	return &scanTasks{ctx: taskCtx, cancel: cancel, results: make(chan error, limit), limit: limit, execute: execute}
}
func (s *scanTasks) record(err error) {
	s.active--
	if err != nil {
		if s.err == nil {
			s.err = err
		} else if !errors.Is(s.err, err) {
			s.err = errors.Join(s.err, err)
		}
	}
}
func (s *scanTasks) poll() error {
	for {
		select {
		case err := <-s.results:
			s.record(err)
		default:
			return s.err
		}
	}
}
func (s *scanTasks) start(claimed documents.ClaimedScan) {
	s.active++
	go func() {
		err := s.execute(s.ctx, claimed)
		if err != nil {
			s.cancel()
		}
		s.results <- err
	}()
}
func (s *scanTasks) close() error {
	s.cancel()
	for s.active > 0 {
		s.record(<-s.results)
	}
	return s.err
}
