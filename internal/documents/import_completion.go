package documents

import (
	"context"
	"database/sql"
	"errors"
)

type ImportedPage struct {
	Orientation               int
	Artifact                  ArtifactRecord
	WidthPoints, HeightPoints float64
}

// ImportStop is trusted coordinator evidence after joining all process/file work.
// Prepared page files may remain sealed; unpublished scratch must be gone.
type ImportStop struct {
	RunnerID                                 string
	ErrorCode                                string
	ProcessStoppedAtMS, WorkspaceCleanedAtMS int64
}

func validImportFailure(code string) bool {
	switch code {
	case "pdf_invalid", "pdf_encrypted", "pdf_unsupported", "pdf_limit", "photo_invalid", "photo_limit":
		return true
	}
	return false
}

func validImportStop(stop ImportStop, started, now int64) bool {
	return id(stop.RunnerID) && stop.ProcessStoppedAtMS > 0 && stop.ProcessStoppedAtMS >= started && stop.WorkspaceCleanedAtMS >= stop.ProcessStoppedAtMS && stop.WorkspaceCleanedAtMS <= now
}

func (s *Store) CommitImportPages(ctx context.Context, owner, job string, stop ImportStop, pages []ImportedPage) error {
	if !id(job) || !id(stop.RunnerID) || stop.ErrorCode != "" || len(pages) < 1 || len(pages) > 10000 {
		return ErrInvalid
	}
	seenIDs, seenPaths := map[string]bool{}, map[string]bool{}
	var total int64
	for _, p := range pages {
		a := p.Artifact
		if !id(a.ID) || !localPath(a.RelativePath) || seenIDs[a.ID] || seenPaths[a.RelativePath] || (a.MediaType != "application/pdf" && a.MediaType != "image/png") || a.Size < 1 || a.Size > 1<<30 || a.SHA256 == ([32]byte{}) || !validImportedGeometry(p) {
			return ErrInvalid
		}
		seenIDs[a.ID] = true
		seenPaths[a.RelativePath] = true
		total += a.Size
	}
	return s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		work, err := s.loadImportWork(ctx, tx, owner, job, stop.RunnerID)
		if err != nil {
			return err
		}
		now := s.now().UnixMilli()
		var started int64
		if err = tx.QueryRowContext(ctx, "SELECT started_at_ms FROM jobs WHERE id=? AND owner_id=?", job, owner).Scan(&started); err != nil {
			return ErrStorage
		}
		if !validImportStop(stop, started, now) || total > work.ReserveBytes || len(pages) > work.Snapshot.MaxPages {
			return ErrConflict
		}
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE document_id=? AND owner_id=?", work.Job.DocumentID, owner).Scan(&count); err != nil {
			return ErrStorage
		}
		if count != 0 {
			return ErrConflict
		}
		for ordinal, p := range pages {
			a := p.Artifact
			photo := work.Input.MediaType != "application/pdf"
			if photo != (p.Orientation != 0) || (photo && len(pages) != 1) {
				return ErrConflict
			}
			kind := "original"
			if photo {
				kind = "intermediate"
			}
			page, err := opaqueID("page_")
			if err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,?,'ready',?,?,?,?,?,?)`, a.ID, work.Job.DocumentID, owner, kind, a.RelativePath, a.MediaType, a.Size, a.SHA256[:], work.Job.Revision, now); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO job_artifacts(job_id,document_id,owner_id,artifact_id) VALUES(?,?,?,?)", job, work.Job.DocumentID, owner, a.ID); err != nil {
				return ErrStorage
			}
			if photo {
				if _, err = tx.ExecContext(ctx, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pixel_width,pixel_height) VALUES(?,?,?,?,?,'photo',?,?)`, page, work.Job.DocumentID, owner, ordinal, work.Input.ID, a.PixelWidth, a.PixelHeight); err != nil {
					return ErrStorage
				}
				if _, err = tx.ExecContext(ctx, `INSERT INTO photo_sources(page_id,document_id,owner_id,raw_artifact_id,normalized_artifact_id,raw_sha256,normalized_sha256,pixel_width,pixel_height,orientation) VALUES(?,?,?,?,?,?,?,?,?,?)`, page, work.Job.DocumentID, owner, work.Input.ID, a.ID, work.Input.SHA256[:], a.SHA256[:], a.PixelWidth, a.PixelHeight, p.Orientation); err != nil {
					return ErrStorage
				}
				continue
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pdf_width_points,pdf_height_points) VALUES(?,?,?,?,?,'pdf',?,?)`, page, work.Job.DocumentID, owner, ordinal, a.ID, p.WidthPoints, p.HeightPoints); err != nil {
				return ErrStorage
			}
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM published_outputs WHERE document_id=? AND owner_id=?", work.Job.DocumentID, owner); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM reservations WHERE job_id=? AND owner_id=?", job, owner); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, "UPDATE jobs SET state='succeeded',heartbeat_at_ms=? WHERE id=? AND owner_id=?", now, job, owner); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, "UPDATE documents SET state='review' WHERE id=? AND owner_id=?", work.Job.DocumentID, owner); err != nil {
			return ErrStorage
		}
		current := s.now().UnixMilli()
		if current < now || current >= work.Job.DeadlineAtMS {
			return ErrConflict
		}
		return nil
	})
}
func (s *Store) ImportPagesRecorded(ctx context.Context, owner, job, runner string, pages []ImportedPage) (bool, error) {
	if !id(owner) || !id(job) || !id(runner) || len(pages) < 1 || len(pages) > 10000 {
		return false, ErrInvalid
	}
	tx, err := s.documents.BeginTx(ctx, nil)
	if err != nil {
		return false, ErrStorage
	}
	defer tx.Rollback()
	var count int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM job_artifacts x JOIN jobs j ON j.id=x.job_id AND j.owner_id=x.owner_id JOIN import_intakes i ON i.job_id=j.id WHERE j.id=? AND j.owner_id=? AND j.runner_id=? AND j.kind='import' AND j.state='succeeded' AND i.state='ready' AND x.artifact_id!=i.artifact_id`, job, owner, runner).Scan(&count)
	if err != nil {
		return false, ErrStorage
	}
	if count != len(pages) {
		return false, nil
	}
	seen := map[string]bool{}
	for _, p := range pages {
		a := p.Artifact
		if seen[a.ID] {
			return false, ErrInvalid
		}
		seen[a.ID] = true
		var exists int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM job_artifacts x JOIN artifacts a ON a.id=x.artifact_id AND a.document_id=x.document_id AND a.owner_id=x.owner_id JOIN import_intakes i ON i.job_id=x.job_id AND i.owner_id=x.owner_id WHERE a.id!=i.artifact_id AND x.job_id=? AND x.owner_id=? AND a.id=? AND a.relative_path=? AND a.byte_size=? AND a.sha256=? AND ((a.media_type='application/pdf' AND a.kind='original') OR (a.media_type='image/png' AND a.kind='intermediate' AND EXISTS(SELECT 1 FROM photo_sources ps WHERE ps.normalized_artifact_id=a.id AND ps.document_id=a.document_id AND ps.owner_id=a.owner_id))) AND a.state IN ('ready','deleting')`, job, owner, a.ID, a.RelativePath, a.Size, a.SHA256[:]).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, ErrStorage
		}
	}
	return true, nil
}
func (s *Store) FinishStoppedImport(ctx context.Context, owner, job string, stop ImportStop, outcome string) error {
	if !id(owner) || !id(job) || !id(stop.RunnerID) || (outcome != "failed" && outcome != "interrupted") || stop.ErrorCode != "" && (outcome != "failed" || !validImportFailure(stop.ErrorCode)) {
		return ErrInvalid
	}
	tx, err := s.documents.BeginTx(ctx, nil)
	if err != nil {
		return ErrStorage
	}
	defer tx.Rollback()
	var state, document, documentState string
	var started, revision, currentRevision, expiry int64
	err = tx.QueryRowContext(ctx, `SELECT j.state,j.document_id,d.state,j.started_at_ms,j.input_revision,d.revision,d.expires_at_ms FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id JOIN import_intakes i ON i.job_id=j.id AND i.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.kind='import' AND j.runner_id=? AND j.started_at_ms IS NOT NULL AND j.state IN ('running','cancelling','reconciling') AND i.state='ready' AND EXISTS(SELECT 1 FROM job_artifacts x WHERE x.job_id=j.id AND x.owner_id=j.owner_id AND x.artifact_id=i.artifact_id) AND EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=j.id AND r.owner_id=j.owner_id) AND NOT EXISTS(SELECT 1 FROM job_artifacts x WHERE x.job_id=j.id AND x.artifact_id!=i.artifact_id) AND NOT EXISTS(SELECT 1 FROM device_leases l WHERE l.job_id=j.id)`, job, owner, stop.RunnerID).Scan(&state, &document, &documentState, &started, &revision, &currentRevision, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return ErrStorage
	}
	now := s.now().UnixMilli()
	if !validImportStop(stop, started, now) {
		return ErrConflict
	}
	if state == "cancelling" {
		outcome = "cancelled"
	}
	code := outcome
	if outcome == "failed" && stop.ErrorCode != "" {
		code = stop.ErrorCode
	}
	if _, err = tx.ExecContext(ctx, "UPDATE jobs SET state=?,error_code=?,heartbeat_at_ms=? WHERE id=? AND owner_id=?", outcome, code, now, job, owner); err != nil {
		return ErrStorage
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM reservations WHERE job_id=? AND owner_id=?", job, owner); err != nil {
		return ErrStorage
	}
	if revision == currentRevision && documentState == "processing" {
		target := "failed"
		if expiry <= now {
			target = "expired"
		}
		if _, err = tx.ExecContext(ctx, "UPDATE documents SET state=? WHERE id=? AND owner_id=?", target, document, owner); err != nil {
			return ErrStorage
		}
	}
	if err = s.commit(tx); err != nil {
		return ErrCommitUncertain
	}
	return nil
}

// ImportStoppedRecorded reconciles a stop acknowledgement; it is not stop proof.
func (s *Store) ImportStoppedRecorded(ctx context.Context, owner, job, runner string) (bool, error) {
	if !id(owner) || !id(job) || !id(runner) {
		return false, ErrInvalid
	}
	var found int
	err := s.documents.QueryRowContext(ctx, `SELECT 1 FROM jobs j JOIN import_intakes i ON i.job_id=j.id AND i.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.runner_id=? AND j.kind='import' AND j.state IN ('failed','cancelled','interrupted') AND i.state='ready' AND NOT EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=j.id) AND NOT EXISTS(SELECT 1 FROM job_artifacts a WHERE a.job_id=j.id AND a.artifact_id!=i.artifact_id) AND NOT EXISTS(SELECT 1 FROM device_leases l WHERE l.job_id=j.id)`, job, owner, runner).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrStorage
	}
	return true, nil
}

func validImportedGeometry(p ImportedPage) bool {
	a := p.Artifact
	if p.Orientation == 0 {
		return a.MediaType == "application/pdf" && a.PixelWidth == 0 && a.PixelHeight == 0 && p.WidthPoints > 0 && p.WidthPoints <= 14400 && p.HeightPoints > 0 && p.HeightPoints <= 14400
	}
	return a.MediaType == "image/png" && p.Orientation >= 1 && p.Orientation <= 8 && p.WidthPoints == 0 && p.HeightPoints == 0 && a.PixelWidth > 0 && a.PixelWidth <= 12000 && a.PixelHeight > 0 && a.PixelHeight <= 12000 && int64(a.PixelWidth)*int64(a.PixelHeight) <= 40000000
}
