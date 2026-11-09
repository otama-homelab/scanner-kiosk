package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// DeliveryRequest is an application-reviewed intent; destination fields come from operator configuration.
type DeliveryRequest struct {
	DocumentID        string
	Revision          int64
	IdempotencyKey    string
	DestinationID     string
	ConfigFingerprint string
}

type Delivery struct {
	ID, OwnerID, DocumentID                                   string
	Revision                                                  int64
	Artifact                                                  ArtifactRecord
	ExpiresAtMS                                               int64
	DestinationID, ConfigFingerprint, Filename, State, TaskID string
	CreatedAtMS, AttemptedAtMS, RemoteDocumentID              int64
}

const deliveryColumns = `id,owner_id,document_id,revision,artifact_id,relative_path,media_type,byte_size,payload_sha256,expires_at_ms,destination_id,config_fingerprint,filename,state,created_at_ms,COALESCE(attempted_at_ms,0),COALESCE(task_id,''),COALESCE(remote_document_id,0)`

func loadDelivery(ctx context.Context, tx *sql.Tx, owner, deliveryID string) (Delivery, error) {
	var d Delivery
	var digest []byte
	err := tx.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE id=? AND owner_id=?`, deliveryID, owner).Scan(&d.ID, &d.OwnerID, &d.DocumentID, &d.Revision, &d.Artifact.ID, &d.Artifact.RelativePath, &d.Artifact.MediaType, &d.Artifact.Size, &digest, &d.ExpiresAtMS, &d.DestinationID, &d.ConfigFingerprint, &d.Filename, &d.State, &d.CreatedAtMS, &d.AttemptedAtMS, &d.TaskID, &d.RemoteDocumentID)
	if err != nil {
		return Delivery{}, readError(err)
	}
	if len(digest) != 32 {
		return Delivery{}, ErrStorage
	}
	copy(d.Artifact.SHA256[:], digest)
	return d, nil
}

func (s *Store) ReadDelivery(ctx context.Context, owner, deliveryID string) (Delivery, error) {
	var d Delivery
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var err error
		d, err = loadDelivery(ctx, tx, owner, deliveryID)
		if err != nil {
			return err
		}
		if d.ExpiresAtMS <= s.now().UnixMilli() || d.State == "retired" {
			return ErrGone
		}
		var live int
		return readError(tx.QueryRowContext(ctx, `SELECT 1 FROM documents WHERE id=? AND owner_id=? AND expires_at_ms>? AND state NOT IN ('expired','deleting')`, d.DocumentID, owner, s.now().UnixMilli()).Scan(&live))
	})
	if err != nil {
		return Delivery{}, err
	}
	return d, nil
}

func (s *Store) AdmitDelivery(ctx context.Context, owner string, r DeliveryRequest) (Delivery, error) {
	fingerprint, e := hex.DecodeString(r.ConfigFingerprint)
	if !id(r.DocumentID) || r.Revision < 1 || !id(r.IdempotencyKey) || !label(r.DestinationID) || len(r.DestinationID) > 128 || e != nil || len(fingerprint) != 32 || strings.ToLower(r.ConfigFingerprint) != r.ConfigFingerprint {
		return Delivery{}, ErrInvalid
	}
	key := sha256.Sum256([]byte(r.IdempotencyKey))
	body, _ := json.Marshal(struct {
		DocumentID                       string
		Revision                         int64
		DestinationID, ConfigFingerprint string
	}{r.DocumentID, r.Revision, r.DestinationID, r.ConfigFingerprint})
	requestHash := sha256.Sum256(body)
	var out Delivery
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		var prior string
		var digest []byte
		var expiry int64
		err := tx.QueryRowContext(ctx, `SELECT delivery_id,request_hash,expires_at_ms FROM delivery_receipts WHERE owner_id=? AND key_hash=?`, owner, key[:]).Scan(&prior, &digest, &expiry)
		if err == nil {
			if !sameHash(digest, requestHash) {
				return ErrIdempotencyConflict
			}
			if expiry <= now {
				return ErrGone
			}
			out, err = loadDelivery(ctx, tx, owner, prior)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ErrStorage
		}
		// Keys are shared across operation kinds; delivery never fabricates a job receipt.
		var used int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM request_receipts WHERE owner_id=? AND key_hash=?`, owner, key[:]).Scan(&used); err != nil {
			return ErrStorage
		}
		if used != 0 {
			return ErrIdempotencyConflict
		}
		keyExpiry, err := s.freshRequestKey(ctx, tx, r.IdempotencyKey, now)
		if err != nil {
			return err
		}
		out = Delivery{OwnerID: owner, DocumentID: r.DocumentID, Revision: r.Revision, DestinationID: r.DestinationID, ConfigFingerprint: r.ConfigFingerprint, State: "queued", CreatedAtMS: now}
		var hash []byte
		err = tx.QueryRowContext(ctx, `SELECT a.id,a.relative_path,a.media_type,a.byte_size,a.sha256,d.expires_at_ms FROM published_outputs p
   JOIN documents d ON d.id=p.document_id AND d.owner_id=p.owner_id AND d.revision=p.revision
   JOIN artifacts a ON a.id=p.artifact_id AND a.document_id=d.id AND a.owner_id=d.owner_id
   WHERE d.id=? AND d.owner_id=? AND d.revision=? AND d.expires_at_ms>? AND d.state='ready'
   AND a.state='ready' AND a.kind='output' AND (a.input_revision=d.revision OR EXISTS(SELECT 1 FROM ocr_results r WHERE r.document_id=d.id AND r.owner_id=d.owner_id AND r.output_artifact_id=a.id AND r.input_revision=a.input_revision AND r.state='adopted' AND r.adopted_revision=d.revision))`, r.DocumentID, owner, r.Revision, now).Scan(&out.Artifact.ID, &out.Artifact.RelativePath, &out.Artifact.MediaType, &out.Artifact.Size, &hash, &out.ExpiresAtMS)
		if err != nil {
			return readError(err)
		}
		if len(hash) != 32 || out.Artifact.MediaType != "application/pdf" || out.Artifact.Size < 1 || out.Artifact.Size > 128<<20 {
			return ErrConflict
		}
		copy(out.Artifact.SHA256[:], hash)
		err = tx.QueryRowContext(ctx, `SELECT id FROM deliveries WHERE owner_id=? AND destination_id=? AND payload_sha256=?`, owner, r.DestinationID, hash).Scan(&prior)
		if err == nil {
			out, err = loadDelivery(ctx, tx, owner, prior)
			if err != nil {
				return err
			}
			if out.State == "retired" || out.ExpiresAtMS <= now {
				return ErrGone
			}
		} else if errors.Is(err, sql.ErrNoRows) {
			out.ID, err = opaqueID("delivery_")
			if err != nil {
				return ErrStorage
			}
			out.Filename = out.ID + ".pdf"
			_, err = tx.ExecContext(ctx, `INSERT INTO deliveries(id,owner_id,document_id,revision,artifact_id,relative_path,media_type,byte_size,payload_sha256,expires_at_ms,destination_id,config_fingerprint,filename,state,created_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'queued',?)`, out.ID, owner, out.DocumentID, out.Revision, out.Artifact.ID, out.Artifact.RelativePath, out.Artifact.MediaType, out.Artifact.Size, hash, out.ExpiresAtMS, out.DestinationID, out.ConfigFingerprint, out.Filename, now)
			if err != nil {
				return ErrStorage
			}
		} else {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO delivery_receipts(owner_id,key_hash,request_hash,delivery_id,expires_at_ms,key_expires_at_ms) VALUES(?,?,?,?,?,?)`, owner, key[:], requestHash[:], out.ID, out.ExpiresAtMS, keyExpiry); err != nil {
			return ErrStorage
		}
		if s.now().UnixMilli() >= out.ExpiresAtMS {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return Delivery{}, err
	}
	return out, nil
}

// ClaimDelivery returns upload authority only after a positively acknowledged commit.
func (s *Store) ClaimDelivery(ctx context.Context, owner, deliveryID string) (Delivery, error) {
	var d Delivery
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var err error
		d, err = loadDelivery(ctx, tx, owner, deliveryID)
		if err != nil {
			return err
		}
		now := s.now().UnixMilli()
		result, err := tx.ExecContext(ctx, `UPDATE deliveries SET state='attempted',attempted_at_ms=? WHERE id=? AND owner_id=? AND state='queued' AND attempted_at_ms IS NULL AND expires_at_ms>?
   AND EXISTS(SELECT 1 FROM published_outputs p JOIN documents d ON d.id=p.document_id AND d.owner_id=p.owner_id AND d.revision=p.revision JOIN artifacts a ON a.id=p.artifact_id AND a.document_id=d.id AND a.owner_id=d.owner_id
   WHERE d.id=deliveries.document_id AND d.owner_id=deliveries.owner_id AND d.revision=deliveries.revision AND d.state='ready' AND d.expires_at_ms>? AND a.id=deliveries.artifact_id AND a.state='ready' AND a.kind='output' AND a.relative_path=deliveries.relative_path AND a.media_type=deliveries.media_type AND a.byte_size=deliveries.byte_size AND a.sha256=deliveries.payload_sha256
   AND (a.input_revision=d.revision OR EXISTS(SELECT 1 FROM ocr_results r WHERE r.document_id=d.id AND r.owner_id=d.owner_id AND r.output_artifact_id=a.id AND r.input_revision=a.input_revision AND r.state='adopted' AND r.adopted_revision=d.revision)))`, now, deliveryID, owner, now, now)
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
		d.State = "attempted"
		d.AttemptedAtMS = now
		if s.now().UnixMilli() >= d.ExpiresAtMS {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return Delivery{}, err
	}
	return d, nil
}

func (s *Store) AcceptDelivery(ctx context.Context, owner, deliveryID, taskID string) error {
	if !deliveryUUID(taskID) {
		return ErrInvalid
	}
	return s.updateDelivery(ctx, owner, deliveryID, `UPDATE deliveries SET state='accepted',task_id=? WHERE id=? AND owner_id=? AND state IN ('attempted','unknown','accepted') AND (task_id IS NULL OR task_id=?)`, taskID, deliveryID, owner, taskID)
}
func (s *Store) ConfirmDelivery(ctx context.Context, owner, deliveryID, taskID string, remoteDocumentID int64, checksum [32]byte) error {
	if !deliveryUUID(taskID) || remoteDocumentID < 1 {
		return ErrInvalid
	}
	return s.updateDelivery(ctx, owner, deliveryID, `UPDATE deliveries SET state='confirmed',remote_document_id=?,confirmed_sha256=? WHERE id=? AND owner_id=? AND state IN ('accepted','unknown','confirmed') AND task_id=? AND payload_sha256=?`, remoteDocumentID, checksum[:], deliveryID, owner, taskID, checksum[:])
}
func (s *Store) MarkDeliveryUnknown(ctx context.Context, owner, deliveryID string) error {
	return s.updateDelivery(ctx, owner, deliveryID, `UPDATE deliveries SET state='unknown' WHERE id=? AND owner_id=? AND state IN ('attempted','accepted','unknown')`, deliveryID, owner)
}
func (s *Store) updateDelivery(ctx context.Context, owner, deliveryID, query string, args ...any) error {
	return s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		d, err := loadDelivery(ctx, tx, owner, deliveryID)
		if err != nil {
			return err
		}
		if d.ExpiresAtMS <= s.now().UnixMilli() {
			return ErrGone
		}
		var live int
		if err = tx.QueryRowContext(ctx, `SELECT 1 FROM documents WHERE id=? AND owner_id=? AND expires_at_ms>? AND state NOT IN ('expired','deleting')`, d.DocumentID, owner, s.now().UnixMilli()).Scan(&live); err != nil {
			return readError(err)
		}
		result, err := tx.ExecContext(ctx, query, args...)
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
func deliveryUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return s != "00000000-0000-0000-0000-000000000000"
}
