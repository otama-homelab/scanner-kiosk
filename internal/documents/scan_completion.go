package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// ScanStop is trusted coordinator evidence, never an HTTP payload. Timestamps
// must come from joined local work and a subsequent hardware-idle observation.
// The repository binds this report to the claimed job/boot/epoch; it cannot
// itself verify a physical device or replace the reconciler/adapter.
type ScanStop struct {
	DeviceID, WorkerBootID               string
	LeaseEpoch                           int64
	ProcessStoppedAtMS, IdleObservedAtMS int64
}
type ScanPage struct {
	ID, OriginalArtifactID string
	Ordinal                int
	Capture                Capture
}

// CommitScanPage accepts only a validated, closed/synced original and fresh stop
// evidence. Publication, page metadata, lease/reservation release and job completion
// commit together. Unknown commit outcomes require CaptureRecorded read-back.
func (s *Store) CommitScanPage(ctx context.Context, owner, jobID string, revision int64, artifact ArtifactRecord, stop ScanStop) (ScanPage, error) {
	return s.CommitScanPageWithThumbnails(ctx, owner, jobID, revision, artifact, stop, nil)
}

// CommitScanPageWithThumbnails publishes one captured original and its optional
// source-bound preview under the same reservation, revision and physical stop
// fences. No derivative can commit separately or consume another user's quota.
func (s *Store) CommitScanPageWithThumbnails(ctx context.Context, owner, jobID string, revision int64, artifact ArtifactRecord, stop ScanStop, thumbnails []ThumbnailRecord) (ScanPage, error) {
	var out ScanPage
	if !id(jobID) || revision < 1 || !validStopIdentity(stop) || !id(artifact.ID) || !localPath(artifact.RelativePath) || artifact.Size < 1 || (artifact.MediaType != "image/png" && artifact.MediaType != "image/jpeg") {
		return out, ErrInvalid
	}
	if artifact.PixelWidth < 0 || artifact.PixelHeight < 0 || artifact.PixelWidth > 1000000 || artifact.PixelHeight > 1000000 || (artifact.PixelWidth == 0) != (artifact.PixelHeight == 0) {
		return out, ErrInvalid
	}
	if len(thumbnails) > 1 {
		return out, ErrInvalid
	}
	total := artifact.Size
	for _, thumb := range thumbnails {
		a := thumb.Artifact
		if thumb.OriginalID != artifact.ID || !id(a.ID) || a.ID == artifact.ID || !localPath(a.RelativePath) || a.RelativePath == artifact.RelativePath || a.MediaType != "image/jpeg" || a.Size < 1 || a.Size > 1<<20 || a.PixelWidth < 1 || a.PixelWidth > 512 || a.PixelHeight < 1 || a.PixelHeight > 512 || a.SHA256 == ([32]byte{}) || total > int64(^uint64(0)>>1)-a.Size {
			return out, ErrInvalid
		}
		total += a.Size
	}
	var width, height any
	if artifact.PixelWidth > 0 {
		width, height = artifact.PixelWidth, artifact.PixelHeight
	}
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		var document, purpose, configuration string
		var reserved, claimedAt int64
		err := tx.QueryRowContext(ctx, `SELECT d.id,d.purpose,j.configuration_json,r.byte_size,j.started_at_ms FROM jobs j
   JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id
   JOIN reservations r ON r.job_id=j.id AND r.owner_id=j.owner_id
   JOIN device_leases l ON l.job_id=j.id
   WHERE j.id=? AND j.owner_id=? AND j.kind='scan' AND j.state='running' AND j.started_at_ms IS NOT NULL AND j.runner_id=?
   AND j.input_revision=? AND d.revision=j.input_revision AND d.state='scanning' AND j.deadline_at_ms>? AND d.expires_at_ms>?
   AND l.device_id=? AND l.epoch=? AND l.state='held'`, jobID, owner, stop.WorkerBootID, revision, now, now, stop.DeviceID, stop.LeaseEpoch).Scan(&document, &purpose, &configuration, &reserved, &claimedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return ErrStorage
		}
		var snapshot ScanSnapshot
		if json.Unmarshal([]byte(configuration), &snapshot) != nil || snapshot.Version != 1 || !validScanPlacement(snapshot.InsertBeforePageID, snapshot.ReplacePageID) || snapshot.DeviceID != stop.DeviceID || snapshot.WorkerBootID != stop.WorkerBootID || snapshot.LeaseEpoch != stop.LeaseEpoch || !validCapture(snapshot.Capture) {
			return ErrConflict
		}
		if !validStopTimes(stop, claimedAt, now) || total > reserved {
			return ErrConflict
		}
		var count, ordinal int
		if err = tx.QueryRowContext(ctx, "SELECT count(*),COALESCE(MAX(ordinal),-1)+1 FROM pages WHERE document_id=? AND owner_id=?", document, owner).Scan(&count, &ordinal); err != nil {
			return ErrStorage
		}
		if purpose == "photo" && count != 0 && snapshot.ReplacePageID == "" {
			return ErrConflict
		}
		ordinal, err = applyScanPlacement(ctx, tx, document, owner, snapshot, ordinal)
		if err != nil {
			return err
		}
		pageID, err := opaqueID("page_")
		if err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready',?,?,?,?,?,?)`, artifact.ID, document, owner, artifact.RelativePath, artifact.MediaType, artifact.Size, artifact.SHA256[:], revision, now); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO job_artifacts(job_id,document_id,owner_id,artifact_id) VALUES(?,?,?,?)", jobID, document, owner, artifact.ID); err != nil {
			return ErrStorage
		}
		capture := snapshot.Capture
		if _, err = tx.ExecContext(ctx, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,pixel_width,pixel_height) VALUES(?,?,?,?,?,?,?,?,?,?)`, pageID, document, owner, ordinal, artifact.ID, capture.Size, capture.Color, capture.DPI, width, height); err != nil {
			return ErrStorage
		}
		for _, thumb := range thumbnails {
			a := thumb.Artifact
			if _, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'thumbnail','ready',?,?,?,?,?,?)`, a.ID, document, owner, a.RelativePath, a.MediaType, a.Size, a.SHA256[:], revision, now); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO job_artifacts(job_id,document_id,owner_id,artifact_id) VALUES(?,?,?,?)`, jobID, document, owner, a.ID); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO page_thumbnails(original_artifact_id,document_id,owner_id,artifact_id,pixel_width,pixel_height) VALUES(?,?,?,?,?,?)`, artifact.ID, document, owner, a.ID, a.PixelWidth, a.PixelHeight); err != nil {
				return ErrStorage
			}
		}
		if err = releaseStoppedScan(ctx, tx, jobID, owner, stop, "succeeded"); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE documents SET state='review' WHERE id=? AND owner_id=?", document, owner); err != nil {
			return ErrStorage
		}
		current := s.now().UnixMilli()
		var active int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM documents d JOIN jobs j ON j.document_id=d.id AND j.owner_id=d.owner_id WHERE j.id=? AND j.owner_id=? AND d.revision=? AND d.expires_at_ms>? AND j.deadline_at_ms>?`, jobID, owner, revision, current, current).Scan(&active)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return ErrStorage
		}
		out = ScanPage{ID: pageID, OriginalArtifactID: artifact.ID, Ordinal: ordinal, Capture: capture}
		return nil
	})
	if err != nil {
		return ScanPage{}, err
	}
	return out, nil
}
func releaseStoppedScan(ctx context.Context, tx *sql.Tx, job, owner string, stop ScanStop, terminal string) error {
	result, err := tx.ExecContext(ctx, "UPDATE device_leases SET state='idle',job_id=NULL WHERE device_id=? AND epoch=? AND job_id=? AND state IN ('held','reconciling')", stop.DeviceID, stop.LeaseEpoch, job)
	if err != nil {
		return ErrStorage
	}
	n, err := result.RowsAffected()
	if err != nil {
		return ErrStorage
	}
	if n != 1 {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM reservations WHERE job_id=? AND owner_id=?", job, owner); err != nil {
		return ErrStorage
	}
	if _, err = tx.ExecContext(ctx, "UPDATE jobs SET state=? WHERE id=? AND owner_id=?", terminal, job, owner); err != nil {
		return ErrStorage
	}
	return nil
}
func (s *Store) CaptureRecorded(ctx context.Context, owner, jobID, artifactID string) (bool, error) {
	var found int
	err := s.documents.QueryRowContext(ctx, `SELECT 1 FROM job_artifacts x JOIN jobs j ON j.id=x.job_id AND j.owner_id=x.owner_id JOIN artifacts a ON a.id=x.artifact_id AND a.document_id=x.document_id AND a.owner_id=x.owner_id WHERE x.owner_id=? AND x.job_id=? AND x.artifact_id=? AND j.kind='scan' AND j.state='succeeded' AND a.kind='original' AND a.state IN ('ready','deleting')`, owner, jobID, artifactID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrStorage
	}
	return true, nil
}
func validStopIdentity(p ScanStop) bool {
	return id(p.DeviceID) && id(p.WorkerBootID) && p.LeaseEpoch > 0
}
func validStopTimes(p ScanStop, claimedAt, now int64) bool {
	return p.ProcessStoppedAtMS > 0 && p.ProcessStoppedAtMS >= claimedAt && p.IdleObservedAtMS >= p.ProcessStoppedAtMS && p.IdleObservedAtMS <= now
}

// FinishStoppedScan is worker-internal cleanup authority, including for disabled
// or expired owners. The caller must already have stopped/reaped owned work,
// cleaned unpublished files and confirmed device idle. It never stops hardware.
// It cannot be exposed as a normal-user endpoint or take evidence from a browser.
func (s *Store) FinishStoppedScan(ctx context.Context, owner, jobID string, stop ScanStop, workspaceCleanedAtMS int64, outcome string) error {
	if !id(owner) || !id(jobID) || !validStopIdentity(stop) || (outcome != "failed" && outcome != "interrupted") {
		return ErrInvalid
	}
	tx, err := s.documents.BeginTx(ctx, nil)
	if err != nil {
		return ErrStorage
	}
	defer tx.Rollback()
	now := s.now().UnixMilli()
	var configuration, jobState, document, documentState string
	var createdAt, jobRevision, docRevision, expiry int64
	var claimedAt sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT j.configuration_json,j.state,j.document_id,j.created_at_ms,j.started_at_ms,j.input_revision,d.revision,d.expires_at_ms,d.state
   FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id JOIN device_leases l ON l.job_id=j.id
   WHERE j.id=? AND j.owner_id=? AND j.kind='scan' AND j.runner_id=? AND j.state IN ('queued','running','cancelling','reconciling')
   AND l.device_id=? AND l.epoch=? AND l.state IN ('held','reconciling')`, jobID, owner, stop.WorkerBootID, stop.DeviceID, stop.LeaseEpoch).Scan(&configuration, &jobState, &document, &createdAt, &claimedAt, &jobRevision, &docRevision, &expiry, &documentState)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return ErrStorage
	}
	var snapshot ScanSnapshot
	if json.Unmarshal([]byte(configuration), &snapshot) != nil || snapshot.Version != 1 || snapshot.DeviceID != stop.DeviceID || snapshot.WorkerBootID != stop.WorkerBootID || snapshot.LeaseEpoch != stop.LeaseEpoch {
		return ErrConflict
	}
	reference := createdAt
	if claimedAt.Valid {
		reference = claimedAt.Int64
	}
	if !validStopTimes(stop, reference, now) || workspaceCleanedAtMS < stop.ProcessStoppedAtMS || workspaceCleanedAtMS > now {
		return ErrConflict
	}
	terminal := outcome
	if jobState == "cancelling" {
		terminal = "cancelled"
	}
	if err = releaseStoppedScan(ctx, tx, jobID, owner, stop, terminal); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE jobs SET error_code=? WHERE id=? AND owner_id=?", terminal, jobID, owner); err != nil {
		return ErrStorage
	}
	if documentState != "deleting" {
		if expiry <= now {
			if _, err = tx.ExecContext(ctx, "UPDATE documents SET state='expired' WHERE id=? AND owner_id=?", document, owner); err != nil {
				return ErrStorage
			}
		} else if docRevision == jobRevision && (documentState == "scanning" || documentState == "cancelling") {
			var pages int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE document_id=? AND owner_id=?", document, owner).Scan(&pages); err != nil {
				return ErrStorage
			}
			state := "failed"
			if pages > 0 {
				state = "review"
			}
			if _, err = tx.ExecContext(ctx, "UPDATE documents SET state=? WHERE id=? AND owner_id=?", state, document, owner); err != nil {
				return ErrStorage
			}
		}
	}
	if err = s.commit(tx); err != nil {
		return ErrCommitUncertain
	}
	return nil
}
func (s *Store) ScanStoppedRecorded(ctx context.Context, owner, jobID string) (bool, error) {
	var found int
	err := s.documents.QueryRowContext(ctx, `SELECT 1 FROM jobs j WHERE j.id=? AND j.owner_id=? AND j.kind='scan' AND j.state IN ('failed','cancelled','interrupted') AND NOT EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=j.id) AND NOT EXISTS(SELECT 1 FROM device_leases l WHERE l.job_id=j.id)`, jobID, owner).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrStorage
	}
	return true, nil
}

// AbortUnstartedScan is internal cleanup for queued/pre-launch-cancelled scans.
// The immutable null start marker proves no conforming executor claimed it. The
// conditional transaction races safely against claim: only one can succeed.
// Executors must allocate job files and launch children only after confirmed claim.
// No physical stop report is fabricated for work that never started.
func (s *Store) AbortUnstartedScan(ctx context.Context, owner, jobID, workerBootID string) error {
	if !id(owner) || !id(jobID) || !id(workerBootID) {
		return ErrInvalid
	}
	tx, err := s.documents.BeginTx(ctx, nil)
	if err != nil {
		return ErrStorage
	}
	defer tx.Rollback()
	var configuration, jobState, document, documentState string
	var jobRevision, docRevision, expiry int64
	err = tx.QueryRowContext(ctx, `SELECT j.configuration_json,j.state,j.document_id,j.input_revision,d.revision,d.expires_at_ms,d.state
  FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id
  JOIN device_leases l ON l.job_id=j.id AND l.state='held' AND l.device_id=json_extract(j.configuration_json,'$.device_id') AND l.epoch=json_extract(j.configuration_json,'$.lease_epoch')
  WHERE j.id=? AND j.owner_id=? AND j.kind='scan' AND j.runner_id=? AND j.state IN ('queued','cancelling')
  AND j.started_at_ms IS NULL AND j.heartbeat_at_ms IS NULL
  AND NOT EXISTS(SELECT 1 FROM job_artifacts a WHERE a.job_id=j.id)`, jobID, owner, workerBootID).Scan(&configuration, &jobState, &document, &jobRevision, &docRevision, &expiry, &documentState)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return ErrStorage
	}
	var snapshot ScanSnapshot
	if json.Unmarshal([]byte(configuration), &snapshot) != nil || snapshot.Version != 1 || snapshot.WorkerBootID != workerBootID || !id(snapshot.DeviceID) || snapshot.LeaseEpoch < 1 {
		return ErrConflict
	}
	terminal := "interrupted"
	if jobState == "cancelling" {
		terminal = "cancelled"
	}
	identity := ScanStop{DeviceID: snapshot.DeviceID, WorkerBootID: workerBootID, LeaseEpoch: snapshot.LeaseEpoch}
	if err = releaseStoppedScan(ctx, tx, jobID, owner, identity, terminal); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE jobs SET error_code='not_started' WHERE id=? AND owner_id=?", jobID, owner); err != nil {
		return ErrStorage
	}
	now := s.now().UnixMilli()
	if documentState != "deleting" {
		if expiry <= now {
			if _, err = tx.ExecContext(ctx, "UPDATE documents SET state='expired' WHERE id=? AND owner_id=?", document, owner); err != nil {
				return ErrStorage
			}
		} else if docRevision == jobRevision && (documentState == "scanning" || documentState == "cancelling") {
			var pages int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE document_id=? AND owner_id=?", document, owner).Scan(&pages); err != nil {
				return ErrStorage
			}
			state := "failed"
			if pages > 0 {
				state = "review"
			}
			if _, err = tx.ExecContext(ctx, "UPDATE documents SET state=? WHERE id=? AND owner_id=?", state, document, owner); err != nil {
				return ErrStorage
			}
		}
	}
	if err = s.commit(tx); err != nil {
		return ErrCommitUncertain
	}
	return nil
}
