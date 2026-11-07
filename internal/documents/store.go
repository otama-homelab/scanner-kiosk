// Package documents enforces owner/expiry/revision checks for document records.
// Callers provide authenticated opaque principal IDs; HTTP authentication and
// file/content validation are separate boundaries.
package documents

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io/fs"
	"strings"
	"time"
)

var (
	ErrUnavailable     = errors.New("document or principal unavailable")
	ErrConflict        = errors.New("document operation conflicts with current state")
	ErrInvalid         = errors.New("invalid document operation")
	ErrStorage         = errors.New("document storage operation failed")
	ErrCommitUncertain = errors.New("document commit requires reconciliation")
)

type SpaceProbe func(context.Context) (int64, error)

type Store struct {
	space            SpaceProbe
	state, documents *sql.DB
	now              func() time.Time
	commit           func(*sql.Tx) error
}
type Document struct {
	ID, Purpose, State                 string
	Revision, CreatedAtMS, ExpiresAtMS int64
}
type Job struct {
	ID, DocumentID, Kind, Configuration string
	Revision, DeadlineAtMS              int64
}
type ArtifactRecord struct {
	// Validated source dimensions, used only for scan-page publication. Zero means unknown.
	PixelWidth, PixelHeight     int
	ID, RelativePath, MediaType string
	Size                        int64
	SHA256                      [32]byte
}

// Export is the same sealed file metadata used by final-output publication.
type Export = ArtifactRecord

func New(state, documents *sql.DB, now func() time.Time, space SpaceProbe) (*Store, error) {
	if state == nil || documents == nil || state == documents || now == nil || space == nil {
		return nil, ErrInvalid
	}
	return &Store{state: state, documents: documents, now: now, space: space, commit: func(tx *sql.Tx) error { return tx.Commit() }}, nil
}

// withEnabled holds the state database's IMMEDIATE transaction while accessing
// documents. Principal-disable writes cannot interleave publication. Lock order
// is always state then documents; no network/file processing belongs in this scope.
// The two stores are not committed atomically: this guard never mutates state.
func (s *Store) withEnabled(ctx context.Context, owner string, fn func(*sql.Tx) error) error {
	if !id(owner) {
		return ErrUnavailable
	}

	return s.withPrincipalSnapshot(ctx, func(guard *sql.Conn, tx *sql.Tx) error {
		var active int
		if err := guard.QueryRowContext(ctx, "SELECT 1 FROM principals WHERE id=? AND disabled_at_ms IS NULL", owner).Scan(&active); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrUnavailable
			}
			return ErrStorage
		}
		return fn(tx)
	})
}

// withPrincipalSnapshot pins the enabled-principal set while a worker selects
// work. It shares the same state→documents lock order as public operations.
func (s *Store) withPrincipalSnapshot(ctx context.Context, fn func(*sql.Conn, *sql.Tx) error) error {
	guard, err := s.state.Conn(ctx)
	if err != nil {
		return storageOperationError(err)
	}
	defer guard.Close()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := guard.ExecContext(cleanup, "ROLLBACK"); err != nil {
			_ = guard.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	if _, err = guard.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return storageOperationError(err)
	}
	tx, err := s.documents.BeginTx(ctx, nil)
	if err != nil {
		return storageOperationError(err)
	}
	defer tx.Rollback()
	if err = fn(guard, tx); err != nil {
		return err
	}
	if err = s.commit(tx); err != nil {
		return ErrCommitUncertain
	}
	return nil
}
func (s *Store) Get(ctx context.Context, owner, documentID string) (Document, error) {
	var out Document
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT id,purpose,state,revision,created_at_ms,expires_at_ms FROM documents WHERE id=? AND owner_id=? AND expires_at_ms>? AND state NOT IN ('expired','deleting')`, documentID, owner, s.now().UnixMilli()).Scan(&out.ID, &out.Purpose, &out.State, &out.Revision, &out.CreatedAtMS, &out.ExpiresAtMS)
		return readError(err)
	})
	if err != nil {
		return Document{}, err
	}
	return out, nil
}
func (s *Store) List(ctx context.Context, owner string, limit int) ([]Document, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	out := []Document{}
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,purpose,state,revision,created_at_ms,expires_at_ms FROM documents WHERE owner_id=? AND expires_at_ms>? AND state NOT IN ('expired','deleting') AND NOT (state='failed' AND NOT EXISTS(SELECT 1 FROM pages p WHERE p.document_id=documents.id AND p.owner_id=documents.owner_id)) ORDER BY expires_at_ms,id LIMIT ?`, owner, s.now().UnixMilli(), limit)
		if err != nil {
			return ErrStorage
		}
		defer rows.Close()
		for rows.Next() {
			var d Document
			if err = rows.Scan(&d.ID, &d.Purpose, &d.State, &d.Revision, &d.CreatedAtMS, &d.ExpiresAtMS); err != nil {
				return ErrStorage
			}
			out = append(out, d)
		}
		if rows.Err() != nil {
			return ErrStorage
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ClaimExport claims an already-admitted render job. Admission/quota/device
// acquisition are separate; this method never inserts or retries a scan.
func (s *Store) ClaimExport(ctx context.Context, owner, jobID, runnerID string) (Job, error) {
	var out Job
	if !id(jobID) || !id(runnerID) {
		return out, ErrInvalid
	}
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var err error
		out, err = s.claimRenderTx(ctx, tx, owner, jobID, runnerID)
		return err
	})
	if err != nil {
		return Job{}, err
	}
	return out, nil
}

func (s *Store) claimRenderTx(ctx context.Context, tx *sql.Tx, owner, jobID, runnerID string) (Job, error) {
	var out Job
	now := s.now().UnixMilli()
	err := tx.QueryRowContext(ctx, `UPDATE jobs SET state='running',runner_id=?,heartbeat_at_ms=?,started_at_ms=?
   WHERE id=? AND owner_id=? AND kind='render' AND state='queued' AND started_at_ms IS NULL AND deadline_at_ms>?
   AND EXISTS(SELECT 1 FROM documents d WHERE d.id=jobs.document_id AND d.owner_id=jobs.owner_id AND d.revision=jobs.input_revision AND d.expires_at_ms>? AND d.state='processing')
   AND EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=jobs.id AND r.owner_id=jobs.owner_id AND r.byte_size>0)
   AND NOT EXISTS(SELECT 1 FROM jobs b WHERE b.id!=jobs.id AND b.runner_id=? AND b.kind!='cleanup' AND b.state IN ('queued','running','cancelling','reconciling'))
   RETURNING id,document_id,kind,configuration_json,input_revision,deadline_at_ms`, runnerID, now, now, jobID, owner, now, now, runnerID).Scan(&out.ID, &out.DocumentID, &out.Kind, &out.Configuration, &out.Revision, &out.DeadlineAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrConflict
	}
	if err != nil {
		return Job{}, readError(err)
	}
	if err = s.checkExecutionCapacity(ctx, tx, owner, jobID); err != nil {
		return Job{}, err
	}
	return out, nil
}

// PublishExport accepts only a validated, closed/synced final output. Its database
// transaction is the publication point. An uncertain commit must be read back
// with ExportRecorded before the caller decides whether to delete the files.
func (s *Store) PublishExport(ctx context.Context, owner, jobID, runnerID string, revision int64, artifact Export) error {
	return s.PublishExportWithThumbnails(ctx, owner, jobID, runnerID, revision, artifact, nil)
}

// PublishExportWithThumbnails atomically consumes one render reservation for the
// output and its source-bound derivatives. ExportRecorded reconciles the entire
// bundle after an uncertain commit, never just a separately committed thumbnail.
func (s *Store) PublishExportWithThumbnails(ctx context.Context, owner, jobID, runnerID string, revision int64, artifact Export, thumbnails []ThumbnailRecord) error {
	if !id(jobID) || !id(runnerID) || revision < 1 || !id(artifact.ID) || !localPath(artifact.RelativePath) || artifact.Size < 1 {
		return ErrInvalid
	}
	if len(thumbnails) > 1000 {
		return ErrInvalid
	}
	total := artifact.Size
	seen := map[string]bool{artifact.ID: true}
	sources := map[string]bool{}
	for _, thumbnail := range thumbnails {
		a := thumbnail.Artifact
		if !id(thumbnail.OriginalID) || sources[thumbnail.OriginalID] || !id(a.ID) || seen[a.ID] || !localPath(a.RelativePath) || a.MediaType != "image/jpeg" || a.Size < 1 || a.Size > 1<<20 || a.PixelWidth < 1 || a.PixelWidth > 512 || a.PixelHeight < 1 || a.PixelHeight > 512 || a.SHA256 == ([32]byte{}) {
			return ErrInvalid
		}
		if total > int64(^uint64(0)>>1)-a.Size {
			return ErrInvalid
		}
		total += a.Size
		seen[a.ID] = true
		sources[thumbnail.OriginalID] = true
	}
	return s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		var documentID, purpose string
		var reserved int64
		err := tx.QueryRowContext(ctx, `SELECT d.id,d.purpose,r.byte_size FROM jobs j
   JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id
   JOIN reservations r ON r.job_id=j.id AND r.owner_id=j.owner_id
   WHERE j.id=? AND j.owner_id=? AND j.kind='render' AND j.state='running' AND j.runner_id=?
   AND j.input_revision=? AND d.revision=j.input_revision AND j.deadline_at_ms>?
   AND d.expires_at_ms>? AND d.state='processing'`, jobID, owner, runnerID, revision, now, now).Scan(&documentID, &purpose, &reserved)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return ErrStorage
		}
		if total > reserved {
			return ErrConflict
		}
		if (purpose == "document" && artifact.MediaType != "application/pdf") || (purpose == "photo" && artifact.MediaType != "image/jpeg" && artifact.MediaType != "image/png") {
			return ErrInvalid
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'output','ready',?,?,?,?,?,?)`, artifact.ID, documentID, owner, artifact.RelativePath, artifact.MediaType, artifact.Size, artifact.SHA256[:], revision, now); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO job_artifacts(job_id,document_id,owner_id,artifact_id) VALUES(?,?,?,?)`, jobID, documentID, owner, artifact.ID); err != nil {
			return ErrStorage
		}
		for _, thumbnail := range thumbnails {
			var found int
			err = tx.QueryRowContext(ctx, `SELECT 1 FROM pages p JOIN artifacts a ON a.id=p.original_artifact_id AND a.document_id=p.document_id AND a.owner_id=p.owner_id WHERE p.document_id=? AND p.owner_id=? AND a.id=? AND a.kind='original' AND a.state='ready' AND NOT EXISTS(SELECT 1 FROM page_thumbnails t WHERE t.original_artifact_id=a.id) LIMIT 1`, documentID, owner, thumbnail.OriginalID).Scan(&found)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrConflict
			}
			if err != nil {
				return ErrStorage
			}
			a := thumbnail.Artifact
			if _, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'thumbnail','ready',?,?,?,?,?,?)`, a.ID, documentID, owner, a.RelativePath, a.MediaType, a.Size, a.SHA256[:], revision, now); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO page_thumbnails(original_artifact_id,document_id,owner_id,artifact_id,pixel_width,pixel_height) VALUES(?,?,?,?,?,?)`, thumbnail.OriginalID, documentID, owner, a.ID, a.PixelWidth, a.PixelHeight); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO job_artifacts(job_id,document_id,owner_id,artifact_id) VALUES(?,?,?,?)`, jobID, documentID, owner, a.ID); err != nil {
				return ErrStorage
			}
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM published_outputs WHERE document_id=? AND owner_id=?", documentID, owner); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO published_outputs(document_id,owner_id,revision,artifact_id) VALUES(?,?,?,?)`, documentID, owner, revision, artifact.ID); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, "UPDATE jobs SET state='succeeded',heartbeat_at_ms=? WHERE id=? AND owner_id=?", now, jobID, owner); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM reservations WHERE job_id=? AND owner_id=?", jobID, owner); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, "UPDATE documents SET state='ready' WHERE id=? AND owner_id=?", documentID, owner); err != nil {
			return ErrStorage
		}
		// Recheck the clock immediately before commit: expensive work is never inside
		// this transaction, but lock contention must not extend an expired job.
		current := s.now().UnixMilli()
		var valid int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.deadline_at_ms>? AND d.expires_at_ms>?`, jobID, owner, current, current).Scan(&valid)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		return readError(err)
	})
}

// ExportRecorded is worker-internal boolean reconciliation metadata, including
// for disabled/expired owners. It returns no content/path and bypasses no public
// authorization: it must never be exposed as an unauthenticated endpoint.
func (s *Store) ExportRecorded(ctx context.Context, owner, jobID, artifactID string) (bool, error) {
	var exists int
	err := s.documents.QueryRowContext(ctx, `SELECT 1 FROM job_artifacts x JOIN jobs j ON j.id=x.job_id AND j.owner_id=x.owner_id WHERE x.owner_id=? AND x.job_id=? AND x.artifact_id=? AND j.state='succeeded'`, owner, jobID, artifactID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrStorage
	}
	return true, nil
}

// RequestCancel persists cancellation, not proof of process/hardware termination.
// The worker must reconcile and release resources before a terminal transition.
func (s *Store) RequestCancel(ctx context.Context, owner, jobID string) error {
	return s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE jobs SET state='cancelling' WHERE id=? AND owner_id=? AND state IN ('queued','running','cancelling') AND EXISTS(SELECT 1 FROM documents d WHERE d.id=jobs.document_id AND d.owner_id=jobs.owner_id AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting'))`, jobID, owner, s.now().UnixMilli())
		if err != nil {
			return ErrStorage
		}
		n, err := result.RowsAffected()
		if err != nil {
			return ErrStorage
		}
		if n != 1 {
			return ErrUnavailable
		}
		return nil
	})
}
func readError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnavailable
	}
	return storageOperationError(err)
}
func id(s string) bool {
	if len(s) < 16 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func localPath(s string) bool {
	return fs.ValidPath(s) && s != "." && !strings.ContainsAny(s, "\\\x00") && !strings.Contains(s, "..")
}

// GetExport returns internal authorized file metadata, never a path to expose
// directly in JSON. Streaming still needs deletion/expiry cancellation and checks
// on every new Range/reconnect request.
func (s *Store) GetExport(ctx context.Context, owner, documentID string) (Export, error) {
	reference, err := s.ReadCurrentOutput(ctx, owner, documentID)
	if err != nil {
		return Export{}, err
	}
	return reference.Artifact, nil
}

func (s *Store) ReadCurrentOutput(ctx context.Context, owner, documentID string) (ArtifactReference, error) {
	var reference ArtifactReference
	out := &reference.Artifact
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var digest []byte
		err := tx.QueryRowContext(ctx, `SELECT a.id,a.relative_path,a.media_type,a.byte_size,a.sha256,d.expires_at_ms FROM published_outputs p
   JOIN documents d ON d.id=p.document_id AND d.owner_id=p.owner_id AND d.revision=p.revision
   JOIN artifacts a ON a.id=p.artifact_id AND a.document_id=d.id AND a.owner_id=d.owner_id
   WHERE d.id=? AND d.owner_id=? AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting')
   AND a.state='ready' AND a.kind='output' AND a.input_revision=d.revision`, documentID, owner, s.now().UnixMilli()).Scan(&out.ID, &out.RelativePath, &out.MediaType, &out.Size, &digest, &reference.ExpiresAtMS)
		if err != nil {
			return readError(err)
		}
		if len(digest) != 32 {
			return ErrStorage
		}
		copy(out.SHA256[:], digest)
		if s.now().UnixMilli() >= reference.ExpiresAtMS {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return ArtifactReference{}, err
	}
	return reference, nil
}

// Preserve a driver's explicit cancellation before commit. Other storage errors
// remain failures even when cancellation happens concurrently. Commit uncertainty
// is deliberately classified separately by callers.
func storageOperationError(err error) error {
	if err == context.Canceled {
		return context.Canceled
	}
	if err == context.DeadlineExceeded {
		return context.DeadlineExceeded
	}
	return ErrStorage
}
