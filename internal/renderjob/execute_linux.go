//go:build linux

package renderjob

import (
	"context"
	"errors"
	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/workwatch"
	"time"
)

// preparationFailure records only synchronous work owned by this invocation.
// Failure to create a scope is conservatively not treated as confirmed cleanup.
type preparationFailure struct {
	cause       error
	clean       bool
	stoppedAtMS int64
	scope       *artifactscope.Scope
	extras      []*artifactscope.Scope
	uncertain   bool
}

func (e *preparationFailure) Error() string { return e.cause.Error() }
func (e *preparationFailure) Unwrap() error { return e.cause }

// ExecuteClaimed runs exactly one already-confirmed claimed render. It neither
// claims/requeues jobs nor retries generation. A nonnil Prepared must be retained
// for publication/cleanup reconciliation; a nil result does not imply success.
func ExecuteClaimed(ctx context.Context, records Records, root, owner, job, runner string, options pdfassemble.Options, interval time.Duration) (*Prepared, error) {
	pulses, ok := records.(PulseRecords)
	if !ok {
		return nil, ErrUnsupported
	}
	_, ok = records.(StopRecords)
	if !ok {
		return nil, ErrUnsupported
	}
	var prepared *Prepared
	began := false
	err := workwatch.Run(ctx, interval, func(ctx context.Context) error { return pulses.PulseRender(ctx, owner, job, runner) }, func(ctx context.Context) error {
		began = true
		var err error
		prepared, err = Prepare(ctx, records, root, owner, job, runner, options)
		return err
	})
	if prepared == nil {
		if err == nil {
			return nil, documents.ErrInvalid
		}
		var failure *preparationFailure
		if !began {
			now := time.Now().UnixMilli()
			failure = &preparationFailure{clean: true, stoppedAtMS: now}
		}
		if began && !errors.As(err, &failure) {
			return nil, err
		}
		if !failure.clean {
			if failure.scope != nil {
				return &Prepared{records: records, scope: failure.scope, extraScopes: failure.extras, executionUncertain: failure.uncertain, owner: owner, job: job, runner: runner, discarded: true, stoppedAtMS: failure.stoppedAtMS}, err
			}
			return nil, err // unknown scope creation outcome: reserve remains for reconciliation.
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		recovery := &Prepared{records: records, scope: failure.scope, extraScopes: failure.extras, executionUncertain: failure.uncertain, owner: owner, job: job, runner: runner, discarded: true, stoppedAtMS: failure.stoppedAtMS}
		if finishErr := recovery.Finish(cleanup, stopOutcome(err)); finishErr != nil {
			return recovery, errors.Join(err, finishErr)
		}
		return nil, err
	}
	if err == nil {
		_, err = prepared.Publish(ctx)
		if err == nil {
			return nil, nil
		}
		if prepared.pending || prepared.published {
			return prepared, err
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if finishErr := prepared.Finish(cleanup, stopOutcome(err)); finishErr != nil {
		return prepared, errors.Join(err, finishErr)
	}
	return nil, err
}

func stopOutcome(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "interrupted"
	}
	return "failed"
}
