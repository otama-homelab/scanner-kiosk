//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"time"

	"scanner-kiosk/internal/artifactgc"
	"scanner-kiosk/internal/cleanupjob"
)

// Called only between confirmed startup reconciliation and creation of new
// native/provider/scan/render producers, under the exclusive worker guard.
func sweepStartupDirectories(ctx context.Context, root string, maxBytes int64, budget time.Duration, check cleanupjob.CheckAuthority) (result error) {
	if err := check(ctx); err != nil {
		return err
	}
	maintenance, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	files, err := artifactgc.OpenRoot(root, maxBytes)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, files.Close()) }()
	sweep, err := files.StartEmptySweep(maintenance)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, sweep.Close()) }()
	for {
		progress, err := sweep.Step(maintenance, 64, check)
		if err != nil {
			return err
		}
		if progress.Done {
			return nil
		}
	}
}
