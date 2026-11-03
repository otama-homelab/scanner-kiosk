package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"scanner-kiosk/internal/scannermetadata"
	"time"
)

var ErrDeviceBusy = errors.New("scanner unavailable or not reconciled")
var ErrScannerSelection = errors.New("scanner is not registered")

type Capture struct {
	Size  string `json:"size"`
	Color string `json:"color"`
	DPI   int    `json:"dpi"`
}
type ScanRequest struct {
	DocumentID     string  `json:"document_id,omitempty"`
	Revision       int64   `json:"revision,omitempty"`
	Purpose        string  `json:"purpose,omitempty"`
	PhotoFormat    string  `json:"photo_format,omitempty"`
	DeviceID       string  `json:"device_id"`
	Capture        Capture `json:"capture"`
	IdempotencyKey string  `json:"-"`
	ReserveBytes   int64   `json:"-"`
	// Supplied by trusted ready-worker coordination, never by a browser or a stable env setting.
	WorkerBootID string `json:"-"`
}
type ScanLimits struct {
	AdmissionLimits
	MaxDocuments int
	// Zero retains the conservative one-slot policy for existing internal callers.
	MaxConcurrentScans int
	// Non-nil constrains new admissions; receipt replay precedes this policy.
	RegisteredDevices []string
	// Required by AdmitReadyScan; bounds native geometry in the admission transaction.
	MaxPagePixels    int64
	DocumentLifetime time.Duration
}
type ScanSnapshot struct {
	Version      int     `json:"version"`
	DeviceID     string  `json:"device_id"`
	WorkerBootID string  `json:"worker_boot_id"`
	LeaseEpoch   int64   `json:"lease_epoch"`
	Capture      Capture `json:"capture"`
	PhotoFormat  string  `json:"photo_format,omitempty"`
	// Set only from same-transaction readiness, never from a browser request.
	PhysicalID string `json:"physical_id,omitempty"`
	HandoffID  string `json:"handoff_id,omitempty"`
}
type ScanAdmission struct {
	Admission
	LeaseEpoch int64
}

// AdmitScan is trusted internal admission only, never the public HTTP path.
// A missing lease remains unavailable; it does not establish physical authority.
func (s *Store) AdmitScan(ctx context.Context, owner string, request ScanRequest, limits ScanLimits) (ScanAdmission, error) {
	command, err := prepareScan(request, limits)
	if err != nil {
		return ScanAdmission{}, err
	}
	return s.applyScan(ctx, owner, command, false)
}

// AdmitReadyScan retains the public readiness-gated facade.
func (s *Store) AdmitReadyScan(ctx context.Context, owner string, request ScanRequest, limits ScanLimits) (ScanAdmission, error) {
	return (&ReadyScanService{records: s}).AdmitReadyScan(ctx, owner, request, limits)
}
func (s *Store) ApplyReadyScan(ctx context.Context, owner string, command ReadyScanCommand) (ScanAdmission, error) {
	return s.applyScan(ctx, owner, command.command, true)
}
func (s *Store) applyScan(ctx context.Context, owner string, command scanCommand, ready bool) (ScanAdmission, error) {
	request, limits, err := command.requestValues()
	if err != nil {
		return ScanAdmission{}, err
	}
	first := request.DocumentID == ""
	operation := command.operation
	key, digest := command.key, command.digest
	var out ScanAdmission
	err = s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		if now < 0 {
			return ErrInvalid
		}
		var priorOperation string
		var priorHash []byte
		var priorJob sql.NullString
		var receiptExpiry int64
		err := tx.QueryRowContext(ctx, "SELECT operation,request_hash,result_job_id,expires_at_ms FROM request_receipts WHERE owner_id=? AND key_hash=?", owner, key[:]).Scan(&priorOperation, &priorHash, &priorJob, &receiptExpiry)
		if err == nil {
			if priorOperation != operation || !sameHash(priorHash, digest) {
				return ErrIdempotencyConflict
			}
			if !priorJob.Valid || receiptExpiry <= now {
				return ErrGone
			}
			var jobID, documentID, configuration string
			var revision int64
			err = tx.QueryRowContext(ctx, `SELECT j.id,j.document_id,j.input_revision,j.configuration_json FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.kind='scan' AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting')`, priorJob.String, owner, now).Scan(&jobID, &documentID, &revision, &configuration)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrGone
			}
			if err != nil {
				return ErrStorage
			}
			var snapshot ScanSnapshot
			if json.Unmarshal([]byte(configuration), &snapshot) != nil || snapshot.Version != 1 || !validScanBinding(snapshot) || snapshot.DeviceID != request.DeviceID || snapshot.LeaseEpoch < 1 {
				return ErrStorage
			}
			out = ScanAdmission{Admission: Admission{JobID: jobID, DocumentID: documentID, Revision: revision, Replay: true}, LeaseEpoch: snapshot.LeaseEpoch}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ErrStorage
		}
		keyExpiry, err := s.freshRequestKey(ctx, tx, request.IdempotencyKey, now)
		if err != nil {
			return err
		}
		if ready && limits.RegisteredDevices != nil {
			allowed := false
			for _, device := range limits.RegisteredDevices {
				allowed = allowed || device == request.DeviceID
			}
			if !allowed {
				return ErrScannerSelection
			}
		}

		var authority readyScanAuthority
		if ready {
			var readyErr error
			authority, readyErr = s.scanReadiness(ctx, tx, request, limits.MaxPagePixels, "", now)
			if readyErr != nil {
				return readyErr
			}
			request.WorkerBootID = authority.boot
			limits.MaxConcurrentScans = authority.capacity
		}

		if !id(request.WorkerBootID) {
			return ErrInvalid
		}
		if err := scanSlotsAvailable(ctx, tx, request.WorkerBootID, limits.MaxConcurrentScans, "", now); err != nil {
			return err
		}
		if request.ReserveBytes < 1 || request.ReserveBytes > limits.JobMaxBytes {
			return ErrQuota
		}
		var oldEpoch int64
		err = tx.QueryRowContext(ctx, "SELECT epoch FROM device_leases WHERE device_id=? AND state='idle' AND job_id IS NULL", request.DeviceID).Scan(&oldEpoch)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDeviceBusy
		}
		if err != nil {
			return ErrStorage
		}
		if oldEpoch == 1<<63-1 {
			return ErrDeviceBusy
		}
		documentID := request.DocumentID
		photoFormat := request.PhotoFormat
		revision := int64(1)
		var expiry int64
		if first {
			var count int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM documents d WHERE d.owner_id=? AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.document_id=d.id AND j.owner_id=d.owner_id AND `+completedCleanupPredicate+`)`, owner).Scan(&count); err != nil {
				return ErrStorage
			}
			if count >= limits.MaxDocuments {
				return ErrQuota
			}
			var ok bool
			expiry, ok = addMillis(now, limits.DocumentLifetime)
			if !ok {
				return ErrInvalid
			}
			documentID, err = opaqueID("document_")
			if err != nil {
				return ErrStorage
			}
		} else {
			var purpose, state string
			err = tx.QueryRowContext(ctx, "SELECT purpose,state,revision,expires_at_ms FROM documents WHERE id=? AND owner_id=? AND expires_at_ms>? AND state NOT IN ('expired','deleting')", documentID, owner, now).Scan(&purpose, &state, &revision, &expiry)
			if err != nil {
				return readError(err)
			}
			if revision != request.Revision {
				return ErrRevision
			}
			if revision == 1<<63-1 || (state != "review" && state != "ready" && state != "failed") {
				return ErrConflict
			}
			var pages, active int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE document_id=? AND owner_id=?", documentID, owner).Scan(&pages); err != nil {
				return ErrStorage
			}
			if pages >= limits.MaxPages || (purpose == "photo" && pages >= 1) {
				return ErrConflict
			}
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM jobs WHERE document_id=? AND owner_id=? AND kind!='cleanup' AND state IN ('queued','running','cancelling','reconciling')", documentID, owner).Scan(&active); err != nil {
				return ErrStorage
			}
			if active != 0 {
				return ErrConflict
			}
			if purpose == "photo" {
				// Keep the most recent committed output intent through an explicit retry
				// of an empty photo. This query carries metadata only, never file content.
				err = tx.QueryRowContext(ctx, `SELECT COALESCE(json_extract(configuration_json,'$.settings.format'),json_extract(configuration_json,'$.photo_format')) FROM jobs WHERE document_id=? AND owner_id=? AND kind IN ('scan','render') AND COALESCE(json_extract(configuration_json,'$.settings.format'),json_extract(configuration_json,'$.photo_format')) IN ('jpeg','png') ORDER BY input_revision DESC LIMIT 1`, documentID, owner).Scan(&photoFormat)
				if errors.Is(err, sql.ErrNoRows) {
					photoFormat = "jpeg"
				} else if err != nil {
					return ErrStorage
				}
			}
			revision++
		}
		if err = s.checkCapacity(ctx, tx, owner, request.ReserveBytes, limits.AdmissionLimits); err != nil {
			return err
		}
		deadline, ok := addMillis(now, limits.JobTimeout)
		if !ok {
			return ErrInvalid
		}
		if deadline > expiry {
			deadline = expiry
		}
		receiptDeadline, ok := addMillis(now, limits.ReceiptRetention)
		if !ok || receiptDeadline < expiry {
			return ErrInvalid
		}
		if first {
			if _, err = tx.ExecContext(ctx, "INSERT INTO owners(id) VALUES(?) ON CONFLICT(id) DO NOTHING", owner); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO documents(id,owner_id,purpose,state,revision,created_at_ms,expires_at_ms) VALUES(?,?,?,'scanning',?,?,?)`, documentID, owner, request.Purpose, revision, now, expiry); err != nil {
				return ErrStorage
			}
		} else {
			if _, err = tx.ExecContext(ctx, "DELETE FROM published_outputs WHERE document_id=? AND owner_id=?", documentID, owner); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, "UPDATE documents SET state='scanning',revision=? WHERE id=? AND owner_id=?", revision, documentID, owner); err != nil {
				return ErrStorage
			}
		}
		jobID, err := opaqueID("job_")
		if err != nil {
			return ErrStorage
		}
		epoch := oldEpoch + 1
		configuration, err := json.Marshal(ScanSnapshot{Version: 1, DeviceID: request.DeviceID, WorkerBootID: request.WorkerBootID, LeaseEpoch: epoch, Capture: request.Capture, PhotoFormat: photoFormat, PhysicalID: authority.physical, HandoffID: authority.handoff})
		if err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id) VALUES(?,?,?,'scan','queued',?,?,?,?,?)`, jobID, documentID, owner, revision, string(configuration), now, deadline, request.WorkerBootID); err != nil {
			return ErrStorage
		}
		result, err := tx.ExecContext(ctx, `UPDATE device_leases SET epoch=?,job_id=?,state='held' WHERE device_id=? AND epoch=? AND state='idle' AND job_id IS NULL`, epoch, jobID, request.DeviceID, oldEpoch)
		if err != nil {
			return ErrStorage
		}
		n, err := result.RowsAffected()
		if err != nil {
			return ErrStorage
		}
		if n != 1 {
			return ErrDeviceBusy
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,?)", jobID, owner, request.ReserveBytes); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,result_job_id,expires_at_ms,key_expires_at_ms) VALUES(?,?,?,?,?,?,?)`, owner, key[:], operation, digest[:], jobID, receiptDeadline, keyExpiry); err != nil {
			return ErrStorage
		}
		current := s.now().UnixMilli()
		if current >= deadline || current >= expiry {
			return ErrConflict
		}
		if err := scanSlotsAvailable(ctx, tx, request.WorkerBootID, limits.MaxConcurrentScans, jobID, current); err != nil {
			return err
		}
		if ready {
			confirmed, err := s.scanReadiness(ctx, tx, request, limits.MaxPagePixels, jobID, current)
			if err != nil {
				return err
			}
			if confirmed != authority {
				return ErrDeviceBusy
			}
		}

		out = ScanAdmission{Admission: Admission{JobID: jobID, DocumentID: documentID, Revision: revision}, LeaseEpoch: epoch}
		return nil
	})
	if err != nil {
		return ScanAdmission{}, err
	}
	return out, nil
}

// ClaimScan requires the exact ready-worker boot nonce pinned at admission.
// A restarted worker has a new nonce and cannot automatically execute an old
// queued physical scan. Startup reconciliation must interrupt old work first.
func (s *Store) ClaimScan(ctx context.Context, owner, jobID, workerBootID string) (Job, error) {
	return s.ClaimScanWithLimit(ctx, owner, jobID, workerBootID, 1)
}

// ClaimScanWithLimit claims within trusted deployment scan capacity.
func (s *Store) ClaimScanWithLimit(ctx context.Context, owner, jobID, workerBootID string, limit int) (Job, error) {
	var out Job
	if limit < 1 || limit > 32 || !id(jobID) || !id(workerBootID) {
		return out, ErrInvalid
	}
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var err error
		out, err = s.claimScanTx(ctx, tx, owner, jobID, workerBootID, limit)
		return err
	})
	if err != nil {
		return Job{}, err
	}
	return out, nil
}

func (s *Store) claimScanTx(ctx context.Context, tx *sql.Tx, owner, jobID, workerBootID string, limit int) (Job, error) {
	var out Job
	now := s.now().UnixMilli()
	if err := scanSlotsAvailable(ctx, tx, workerBootID, limit, jobID, now); err != nil {
		if err == ErrDeviceBusy {
			return out, ErrConflict
		}
		return out, err
	}
	err := tx.QueryRowContext(ctx, `UPDATE jobs SET state='running',heartbeat_at_ms=?,started_at_ms=?
   WHERE id=? AND owner_id=? AND kind='scan' AND state='queued' AND started_at_ms IS NULL AND runner_id=? AND deadline_at_ms>?
   AND json_extract(configuration_json,'$.version')=1 AND json_extract(configuration_json,'$.worker_boot_id')=?
   AND EXISTS(SELECT 1 FROM documents d WHERE d.id=jobs.document_id AND d.owner_id=jobs.owner_id AND d.revision=jobs.input_revision AND d.expires_at_ms>? AND d.state='scanning')
   AND EXISTS(SELECT 1 FROM device_leases l WHERE l.job_id=jobs.id AND l.state='held' AND l.device_id=json_extract(jobs.configuration_json,'$.device_id') AND l.epoch=json_extract(jobs.configuration_json,'$.lease_epoch'))
   AND EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=jobs.id AND r.owner_id=jobs.owner_id AND r.byte_size>0)
   RETURNING id,document_id,kind,configuration_json,input_revision,deadline_at_ms`, now, now, jobID, owner, workerBootID, now, workerBootID, now).Scan(&out.ID, &out.DocumentID, &out.Kind, &out.Configuration, &out.Revision, &out.DeadlineAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrConflict
	}
	if err != nil {
		return Job{}, readError(err)
	}
	if err := s.checkExecutionCapacity(ctx, tx, owner, jobID); err != nil {
		return Job{}, err
	}
	if err := scanSlotsAvailable(ctx, tx, workerBootID, limit, jobID, s.now().UnixMilli()); err != nil {
		if err == ErrDeviceBusy {
			return Job{}, ErrConflict
		}
		return Job{}, err
	}
	return out, nil
}

// Empty bindings are retained solely for legacy trusted AdmitScan callers.
func validScanBinding(s ScanSnapshot) bool {
	return s.PhysicalID == "" && s.HandoffID == "" || scannermetadata.ValidID(s.PhysicalID) && scannermetadata.ValidID(s.HandoffID)
}
