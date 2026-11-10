package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type ImportRequest struct {
	MediaType      string   `json:"media_type,omitempty"`
	IdempotencyKey string   `json:"-"`
	InputBytes     int64    `json:"input_bytes"`
	SHA256         [32]byte `json:"sha256"`
}
type ImportLimits struct {
	AdmissionLimits
	MaxDocuments     int
	DocumentLifetime time.Duration
}
type ImportSnapshot struct {
	MediaType       string   `json:"media_type,omitempty"`
	Version         int      `json:"version"`
	InputArtifactID string   `json:"input_artifact_id"`
	InputBytes      int64    `json:"input_bytes"`
	SHA256          [32]byte `json:"sha256"`
	MaxPages        int      `json:"max_pages"`
}

// Producer authority is internal and is never returned on receipt replay.
type ImportAdmission struct {
	Admission
	ProducerID   string         `json:"-"`
	Input        ArtifactRecord `json:"-"`
	DeadlineAtMS int64          `json:"-"`
}

func (s *Store) AdmitImport(ctx context.Context, owner string, request ImportRequest, limits ImportLimits) (ImportAdmission, error) {
	media, operationName, digest, err := importIdentity(request)
	if err != nil || !validAdmissionLimits(limits.AdmissionLimits) || limits.MaxDocuments < 1 || limits.DocumentLifetime < time.Millisecond || limits.DocumentLifetime > 365*24*time.Hour {
		return ImportAdmission{}, ErrInvalid
	}
	request.MediaType = media
	if media == "application/pdf" {
		request.MediaType = ""
	}
	key := sha256.Sum256([]byte(request.IdempotencyKey))

	var out ImportAdmission
	err = s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		if now < 0 {
			return ErrInvalid
		}
		var operation string
		var priorHash []byte
		var result sql.NullString
		var receiptExpiry int64
		err := tx.QueryRowContext(ctx, "SELECT operation,request_hash,result_job_id,expires_at_ms FROM request_receipts WHERE owner_id=? AND key_hash=?", owner, key[:]).Scan(&operation, &priorHash, &result, &receiptExpiry)
		if err == nil {
			if operation != operationName || !sameHash(priorHash, digest) {
				return ErrIdempotencyConflict
			}
			if !result.Valid || receiptExpiry <= now {
				return ErrGone
			}
			err = tx.QueryRowContext(ctx, `SELECT j.id,j.document_id,j.input_revision FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.kind='import' AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting')`, result.String, owner, now).Scan(&out.JobID, &out.DocumentID, &out.Revision)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrGone
			}
			if err != nil {
				return ErrStorage
			}
			out.Replay = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ErrStorage
		}
		keyExpiry, err := s.freshRequestKey(ctx, tx, owner, request.IdempotencyKey, now)
		if err != nil {
			return err
		}
		if request.InputBytes >= limits.JobMaxBytes {
			return ErrQuota
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM documents d WHERE d.owner_id=? AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.document_id=d.id AND j.owner_id=d.owner_id AND `+completedCleanupPredicate+`)`, owner).Scan(&count); err != nil {
			return ErrStorage
		}
		if count >= limits.MaxDocuments {
			return ErrQuota
		}
		if err = s.checkCapacity(ctx, tx, owner, limits.JobMaxBytes, limits.AdmissionLimits); err != nil {
			return err
		}
		expiry, ok := addMillis(now, limits.DocumentLifetime)
		if !ok {
			return ErrInvalid
		}
		deadline, ok := addMillis(now, limits.JobTimeout)
		if !ok {
			return ErrInvalid
		}
		deadline = min(deadline, expiry)
		receiptDeadline, ok := addMillis(now, limits.ReceiptRetention)
		if !ok || receiptDeadline < expiry {
			return ErrInvalid
		}
		document, err := opaqueID("document_")
		if err != nil {
			return ErrStorage
		}
		job, err := opaqueID("job_")
		if err != nil {
			return ErrStorage
		}
		artifact, err := opaqueID("artifact_")
		if err != nil {
			return ErrStorage
		}
		directory, err := opaqueID("work_")
		if err != nil {
			return ErrStorage
		}
		producer, err := opaqueID("producer_")
		if err != nil {
			return ErrStorage
		}
		snapshot, err := json.Marshal(ImportSnapshot{Version: 1, InputArtifactID: artifact, InputBytes: request.InputBytes, SHA256: request.SHA256, MaxPages: limits.MaxPages, MediaType: request.MediaType})
		if err != nil {
			return ErrStorage
		}
		path := directory + "/" + artifact
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{"INSERT INTO owners(id) VALUES(?) ON CONFLICT(id) DO NOTHING", []any{owner}},
			{`INSERT INTO documents(id,owner_id,purpose,state,revision,created_at_ms,expires_at_ms) VALUES(?,?,'document','processing',1,?,?)`, []any{document, owner, now, expiry}},
			{`INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES(?,?,?,'import','queued',1,?,?,?)`, []any{job, document, owner, string(snapshot), now, deadline}},
			{`INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,input_revision,created_at_ms) VALUES(?,?,?,'original','staging',?,?,0,1,?)`, []any{artifact, document, owner, path, media, now}},
			{"INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,?)", []any{job, owner, limits.JobMaxBytes}},
			{"INSERT INTO import_intakes(job_id,document_id,owner_id,artifact_id,producer_id) VALUES(?,?,?,?,?)", []any{job, document, owner, artifact, producer}},
			{`INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,result_job_id,expires_at_ms,key_expires_at_ms) VALUES(?,?,?,?,?,?,?)`, []any{owner, key[:], operationName, digest[:], job, receiptDeadline, keyExpiry}},
		} {
			if _, err = tx.ExecContext(ctx, q.sql, q.args...); err != nil {
				return ErrStorage
			}
		}
		if current := s.now().UnixMilli(); current < now || current >= deadline {
			return ErrConflict
		}
		out = ImportAdmission{Admission: Admission{JobID: job, DocumentID: document, Revision: 1}, ProducerID: producer, Input: ArtifactRecord{ID: artifact, RelativePath: path, MediaType: media}, DeadlineAtMS: deadline}
		return nil
	})
	if err != nil {
		return ImportAdmission{}, err
	}
	return out, nil
}

// StartImportUpload fences producer creation before any filesystem operation.
func (s *Store) StartImportUpload(ctx context.Context, owner, job, producer string) error {
	if !id(job) || !id(producer) {
		return ErrInvalid
	}
	return s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		result, err := tx.ExecContext(ctx, `UPDATE import_intakes SET state='receiving',started_at_ms=? WHERE job_id=? AND owner_id=? AND producer_id=? AND state='pending' AND EXISTS(SELECT 1 FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE j.id=import_intakes.job_id AND j.kind='import' AND j.state='queued' AND j.runner_id IS NULL AND j.started_at_ms IS NULL AND j.created_at_ms<=? AND d.state='processing' AND d.revision=j.input_revision AND j.deadline_at_ms>? AND d.expires_at_ms>?)`, now, job, owner, producer, now, now, now)
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
		return nil
	})
}

// SealStoppedImportUpload requires the coordinator to verify, sync and close the
// complete input first. It seals intake only; no PDF page is published here.
func (s *Store) SealStoppedImportUpload(ctx context.Context, owner, job, producer string, artifact ArtifactRecord, closedAt int64) error {
	return s.finishImportUpload(ctx, owner, job, producer, artifact, closedAt, "ready")
}

// FinishStoppedImportUpload requires the producer and every file writer stopped.
// It preserves actual partial bytes for owned cleanup, including expired/disabled
// owners. These methods and producer tokens must never become browser authority.
func (s *Store) FinishStoppedImportUpload(ctx context.Context, owner, job, producer string, artifact ArtifactRecord, closedAt int64, outcome string) error {
	if outcome != "failed" && outcome != "interrupted" {
		return ErrInvalid
	}
	return s.finishImportUpload(ctx, owner, job, producer, artifact, closedAt, outcome)
}
func (s *Store) finishImportUpload(ctx context.Context, owner, job, producer string, artifact ArtifactRecord, closedAt int64, outcome string) error {
	if !id(owner) || !id(job) || !id(producer) || !id(artifact.ID) || !localPath(artifact.RelativePath) || !validImportMedia(artifact.MediaType) || artifact.Size < 0 || artifact.SHA256 == ([32]byte{}) || artifact.PixelWidth != 0 || artifact.PixelHeight != 0 || closedAt <= 0 {
		return ErrInvalid
	}
	return s.withPrincipalSnapshot(ctx, func(guard *sql.Conn, tx *sql.Tx) error {
		now := s.now().UnixMilli()
		if closedAt > now {
			return ErrConflict
		}
		var document, state, intake, path, configuration, artifactID string
		var start sql.NullInt64
		var reserved, revision, docRevision, deadline, expiry, created int64
		err := tx.QueryRowContext(ctx, `SELECT j.document_id,j.state,i.state,i.started_at_ms,a.relative_path,j.configuration_json,a.id,r.byte_size,j.input_revision,d.revision,j.deadline_at_ms,d.expires_at_ms,j.created_at_ms FROM import_intakes i JOIN jobs j ON j.id=i.job_id AND j.owner_id=i.owner_id JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id JOIN artifacts a ON a.id=i.artifact_id AND a.document_id=i.document_id AND a.owner_id=i.owner_id JOIN reservations r ON r.job_id=j.id AND r.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND i.producer_id=? AND j.kind='import' AND j.state IN ('queued','cancelling') AND j.runner_id IS NULL AND j.started_at_ms IS NULL AND i.state IN ('pending','receiving') AND a.state='staging'`, job, owner, producer).Scan(&document, &state, &intake, &start, &path, &configuration, &artifactID, &reserved, &revision, &docRevision, &deadline, &expiry, &created)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return ErrStorage
		}
		var snapshot ImportSnapshot
		if json.Unmarshal([]byte(configuration), &snapshot) != nil || snapshot.Version != 1 || snapshot.InputArtifactID != artifactID || snapshot.InputBytes < 1 || snapshot.InputBytes > 1<<30 || snapshot.SHA256 == ([32]byte{}) {
			return ErrStorage
		}
		if importMedia(snapshot.MediaType) != artifact.MediaType {
			return ErrConflict
		}
		if closedAt < created || artifact.ID != artifactID || artifact.RelativePath != path || artifact.Size > snapshot.InputBytes || artifact.Size > reserved || start.Valid && closedAt < start.Int64 {
			return ErrConflict
		}
		if outcome == "ready" {
			var enabled int
			if err = guard.QueryRowContext(ctx, "SELECT 1 FROM principals WHERE id=? AND disabled_at_ms IS NULL", owner).Scan(&enabled); err != nil {
				return readError(err)
			}
			var active int
			if err = tx.QueryRowContext(ctx, "SELECT 1 FROM documents WHERE id=? AND owner_id=? AND state='processing'", document, owner).Scan(&active); err != nil {
				return readError(err)
			}
			if intake != "receiving" || state != "queued" || revision != docRevision || now >= deadline || now >= expiry || artifact.Size != snapshot.InputBytes || artifact.SHA256 != snapshot.SHA256 || reserved <= artifact.Size {
				return ErrConflict
			}
		} else if intake == "pending" && (artifact.Size != 0 || artifact.SHA256 != sha256.Sum256(nil)) {
			return ErrConflict
		}
		if _, err = tx.ExecContext(ctx, "UPDATE artifacts SET state='ready',byte_size=?,sha256=? WHERE id=? AND document_id=? AND owner_id=?", artifact.Size, artifact.SHA256[:], artifactID, document, owner); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO job_artifacts(job_id,document_id,owner_id,artifact_id) VALUES(?,?,?,?)", job, document, owner, artifactID); err != nil {
			return ErrStorage
		}
		target := "ready"
		if outcome != "ready" {
			target = "aborted"
		}
		if _, err = tx.ExecContext(ctx, "UPDATE import_intakes SET state=?,closed_at_ms=? WHERE job_id=? AND owner_id=?", target, closedAt, job, owner); err != nil {
			return ErrStorage
		}
		if outcome == "ready" {
			if _, err = tx.ExecContext(ctx, "UPDATE reservations SET byte_size=byte_size-? WHERE job_id=? AND owner_id=?", artifact.Size, job, owner); err != nil {
				return ErrStorage
			}
			if current := s.now().UnixMilli(); current >= deadline || current >= expiry {
				return ErrConflict
			}
		} else {
			if state == "cancelling" {
				outcome = "cancelled"
			}
			if _, err = tx.ExecContext(ctx, "UPDATE jobs SET state=? WHERE id=? AND owner_id=?", outcome, job, owner); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, "DELETE FROM reservations WHERE job_id=? AND owner_id=?", job, owner); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, "UPDATE documents SET state=CASE WHEN expires_at_ms<=? THEN 'expired' ELSE 'failed' END WHERE id=? AND owner_id=? AND state='processing'", now, document, owner); err != nil {
				return ErrStorage
			}
		}
		return nil
	})
}

// ImportUploadRecorded is internal uncertain-commit reconciliation. It never
// authorizes another write and remains usable after cancellation/owner disabling.
func (s *Store) ImportUploadRecorded(ctx context.Context, owner, job, producer string, artifact ArtifactRecord) (string, error) {
	if !id(owner) || !id(job) || !id(producer) {
		return "", ErrInvalid
	}
	var state, path, media, artifactID string
	var size int64
	var digest []byte
	err := s.documents.QueryRowContext(ctx, `SELECT i.state,a.id,a.relative_path,a.media_type,a.byte_size,a.sha256 FROM import_intakes i JOIN artifacts a ON a.id=i.artifact_id AND a.document_id=i.document_id AND a.owner_id=i.owner_id WHERE i.job_id=? AND i.owner_id=? AND i.producer_id=? AND i.state IN ('ready','aborted') AND a.state IN ('ready','deleting')`, job, owner, producer).Scan(&state, &artifactID, &path, &media, &size, &digest)
	if err != nil {
		return "", readError(err)
	}
	if artifactID != artifact.ID || path != artifact.RelativePath || media != artifact.MediaType || size != artifact.Size || !sameHash(digest, artifact.SHA256) {
		return "", ErrConflict
	}
	return state, nil
}

func validImportMedia(media string) bool {
	return media == "application/pdf" || media == "image/jpeg" || media == "image/png"
}
func importMedia(media string) string {
	if media == "" {
		return "application/pdf"
	}
	return media
}

// importIdentity retains the historical PDF digest while separating camera intake.
func importIdentity(request ImportRequest) (string, string, [32]byte, error) {
	media := importMedia(request.MediaType)
	if !id(request.IdempotencyKey) || !validImportMedia(media) || request.InputBytes < 1 || request.InputBytes > 1<<30 || request.SHA256 == ([32]byte{}) || (media != "application/pdf" && request.InputBytes > 32<<20) {
		return "", "", [32]byte{}, ErrInvalid
	}
	operation, prefix := "pdf_import", "pdf-import-new-v1\x00"
	request.MediaType = media
	if media == "application/pdf" {
		request.MediaType = ""
	} else {
		operation, prefix = "photo_import", "photo-import-new-v1\x00"
	}
	canonical, err := canonicalCommand(request)
	if err != nil {
		return "", "", [32]byte{}, ErrInvalid
	}
	return media, operation, sha256.Sum256(append([]byte(prefix), canonical...)), nil
}

// ReadImportReceipt never admits work or returns producer/file authority.
func (s *Store) ReadImportReceipt(ctx context.Context, owner string, request ImportRequest) (Admission, error) {
	_, operation, digest, err := importIdentity(request)
	if err != nil {
		return Admission{}, err
	}
	key := sha256.Sum256([]byte(request.IdempotencyKey))
	var out Admission
	err = s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var actual string
		var hash []byte
		var job sql.NullString
		var expiry int64
		if err := tx.QueryRowContext(ctx, `SELECT operation,request_hash,result_job_id,expires_at_ms FROM request_receipts WHERE owner_id=? AND key_hash=?`, owner, key[:]).Scan(&actual, &hash, &job, &expiry); err != nil {
			return readError(err)
		}
		if actual != operation || !sameHash(hash, digest) {
			return ErrIdempotencyConflict
		}
		now := s.now().UnixMilli()
		if expiry <= now || !job.Valid {
			return ErrGone
		}
		if err := tx.QueryRowContext(ctx, `SELECT j.id,j.document_id,j.input_revision FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.kind='import' AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting')`, job.String, owner, now).Scan(&out.JobID, &out.DocumentID, &out.Revision); err != nil {
			return readError(err)
		}
		out.Replay = true
		return nil
	})
	if err != nil {
		return Admission{}, err
	}
	return out, nil
}
