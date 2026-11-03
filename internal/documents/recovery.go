package documents

import "context"

// RecoveryRequired is an internal startup barrier, never physical stop/idle
// evidence. The caller holds the exclusive worker guard and must finish metadata
// maintenance first. It neither resumes work nor releases leases/reservations.
// All owners (including disabled/expired) participate in this safety check.
func (s *Store) RecoveryRequired(ctx context.Context) (bool, error) {
	var uncertain int
	err := s.documents.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE kind!='cleanup' AND (state IN ('running','cancelling','reconciling') OR (state='queued' AND (runner_id IS NOT NULL OR started_at_ms IS NOT NULL)))) OR EXISTS(SELECT 1 FROM device_leases WHERE state!='idle' OR job_id IS NOT NULL)`).Scan(&uncertain)
	if err != nil {
		return false, storageOperationError(err)
	}
	return uncertain != 0, nil
}
