package documents

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"scanner-kiosk/pkg/ocrplugin"
)

var (
	ErrIdempotencyConflict = errors.New("idempotency key used for another operation")
	ErrGone                = errors.New("previous operation is no longer available")
	ErrOCRLimits           = errors.New("OCR model cannot process these pages")
	ErrQuota               = errors.New("document capacity unavailable")
)

// These limits are deployment policy, never browser-controlled parameters.
// GlobalMaxBytes is a logical metadata/reservation budget, not a disk-free probe.
type AdmissionLimits struct {
	// Optional trusted model bounds; never part of a browser command or receipt hash.
	OCRMaxPages                               int
	OCRMaxPagePixels                          int64
	UserMaxBytes, GlobalMaxBytes, JobMaxBytes int64
	MaxPages                                  int
	JobTimeout, ReceiptRetention              time.Duration
}
type RenderOCR struct {
	Enabled       bool     `json:"enabled"`
	ProviderID    string   `json:"provider_id,omitempty"`
	ModelID       string   `json:"model_id,omitempty"`
	ModelVersion  string   `json:"model_version,omitempty"`
	Languages     []string `json:"languages,omitempty"`
	QualityPreset string   `json:"quality_preset,omitempty"`
}
type RenderSettings struct {
	Format      string    `json:"format"`
	PDFEncoding string    `json:"pdf_encoding,omitempty"`
	OCR         RenderOCR `json:"ocr"`
}
type RenderAdmission struct {
	DocumentID     string
	Revision       int64
	IdempotencyKey string
	Settings       RenderSettings
	// Server operation intent, never part of browser-controlled RenderSettings.
	ReOCR bool
	// Resolved by server-side capability validation, never accepted as a user quota.
	ReserveBytes int64
	// Used only when the client omitted a quality preset. It is not part of the client-command hash.
	ResolvedQualityPreset string
	ResolvedOutput        ocrplugin.OutputKind
}
type Admission struct {
	JobID, DocumentID string
	Revision          int64
	Replay            bool
}
type CapturedPage struct {
	SourceType         string  `json:"source_type,omitempty"`
	PDFWidthPoints     float64 `json:"pdf_width_points,omitempty"`
	PDFHeightPoints    float64 `json:"pdf_height_points,omitempty"`
	ID                 string  `json:"id"`
	OriginalArtifactID string  `json:"original_artifact_id"`
	Rotation           int     `json:"rotation"`
	Size               string  `json:"size"`
	Color              string  `json:"color"`
	DPI                int     `json:"dpi"`
}
type RenderSnapshot struct {
	Sources   []RenderSource       `json:"sources,omitempty"`
	ReOCR     bool                 `json:"reocr,omitempty"`
	Version   int                  `json:"version"`
	Settings  RenderSettings       `json:"settings"`
	Pages     []CapturedPage       `json:"pages"`
	OCROutput ocrplugin.OutputKind `json:"ocr_output,omitempty"`
}

// AdmitRender retains the trusted entry point through the policy service.
func (s *Store) AdmitRender(ctx context.Context, owner string, request RenderAdmission, limits AdmissionLimits) (Admission, error) {
	return (&RenderAdmissionService{records: s}).AdmitRender(ctx, owner, request, limits)
}

// ApplyRender atomically records the immutable render job, reservation and
// receipt after enabled-owner, expiry, revision and capacity checks.
func (s *Store) ApplyRender(ctx context.Context, owner string, command RenderCommand) (Admission, error) {
	request, limits, err := command.Request()
	if err != nil {
		return Admission{}, err
	}
	settings := request.Settings
	keyHash, requestHash := command.Hashes()
	var out Admission
	err = s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		if now < 0 {
			return ErrInvalid
		}
		replay, found, err := s.renderReceipt(ctx, tx, owner, now, keyHash, requestHash)
		if err != nil {
			return err
		}
		if found {
			out = replay
			return nil
		}
		keyExpiry, err := s.freshRequestKey(ctx, tx, owner, request.IdempotencyKey, now)
		if err != nil {
			return err
		}
		// A replay does not depend on current provider installation or policy. New
		// work requires a pinned resolved model and a valid server reservation.
		if settings.OCR.Enabled && !label(settings.OCR.ModelVersion) {
			return ErrInvalid
		}
		snapshotSettings := settings
		if request.ResolvedQualityPreset != "" {
			if !settings.OCR.Enabled || !label(request.ResolvedQualityPreset) {
				return ErrInvalid
			}
			if settings.OCR.QualityPreset != "" && settings.OCR.QualityPreset != request.ResolvedQualityPreset {
				return ErrInvalid
			}
			snapshotSettings.OCR.QualityPreset = request.ResolvedQualityPreset
		}
		if request.ReserveBytes < 1 || request.ReserveBytes > limits.JobMaxBytes {
			return ErrQuota
		}
		var purpose, state string
		var revision, expiry int64
		err = tx.QueryRowContext(ctx, `SELECT purpose,state,revision,expires_at_ms FROM documents WHERE id=? AND owner_id=? AND expires_at_ms>? AND state NOT IN ('expired','deleting')`, request.DocumentID, owner, now).Scan(&purpose, &state, &revision, &expiry)
		if err != nil {
			return readError(err)
		}
		if revision != request.Revision {
			return ErrRevision
		}
		if state != "review" && state != "ready" && state != "failed" {
			return ErrConflict
		}
		if (purpose == "document" && settings.Format != "pdf") || (purpose == "photo" && (settings.Format == "pdf" || settings.OCR.Enabled)) {
			return ErrInvalid
		}
		var active int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE document_id=? AND owner_id=? AND kind!='cleanup' AND state IN ('queued','running','cancelling','reconciling')`, request.DocumentID, owner).Scan(&active); err != nil {
			return ErrStorage
		}
		if active != 0 {
			return ErrConflict
		}
		if request.ReOCR {
			if purpose != "document" || (request.ResolvedOutput != ocrplugin.PositionedText && request.ResolvedOutput != ocrplugin.Text && request.ResolvedOutput != ocrplugin.PDFTextLayer && request.ResolvedOutput != ocrplugin.SearchablePDF) {
				return ErrInvalid
			}
			var candidates int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM ocr_results WHERE document_id=? AND owner_id=? AND state='candidate'`, request.DocumentID, owner).Scan(&candidates); err != nil {
				return ErrStorage
			}
			if candidates != 0 {
				return ErrConflict
			}
		}
		var pageCount int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE document_id=? AND owner_id=?", request.DocumentID, owner).Scan(&pageCount); err != nil {
			return ErrStorage
		}
		if pageCount < 1 || pageCount > limits.MaxPages || (purpose == "photo" && pageCount != 1) {
			return ErrConflict
		}
		if settings.OCR.Enabled && (limits.OCRMaxPages != 0 || limits.OCRMaxPagePixels != 0) {
			if limits.OCRMaxPages < 1 || limits.OCRMaxPagePixels < 1 {
				return ErrInvalid
			}
			var imageCount int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE document_id=? AND owner_id=? AND source_type='scan'", request.DocumentID, owner).Scan(&imageCount); err != nil {
				return ErrStorage
			}
			if imageCount > limits.OCRMaxPages {
				return ErrOCRLimits
			}
			var incompatible int
			// Division avoids overflow; NULL dimensions cannot establish compatibility.
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pages WHERE document_id=? AND owner_id=? AND source_type='scan' AND (pixel_width IS NULL OR pixel_height IS NULL OR pixel_width > ? / pixel_height)`, request.DocumentID, owner, limits.OCRMaxPagePixels).Scan(&incompatible); err != nil {
				return ErrStorage
			}
			if incompatible != 0 {
				return ErrOCRLimits
			}
		}
		pages := []CapturedPage{}
		rows, err := tx.QueryContext(ctx, `SELECT p.id,p.original_artifact_id,p.rotation,COALESCE(p.capture_size,''),COALESCE(p.capture_color,''),COALESCE(p.capture_dpi,0),CASE p.source_type WHEN 'pdf' THEN 'pdf' ELSE '' END,COALESCE(p.pdf_width_points,0),COALESCE(p.pdf_height_points,0) FROM pages p JOIN artifacts a ON a.id=p.original_artifact_id AND a.document_id=p.document_id AND a.owner_id=p.owner_id WHERE p.document_id=? AND p.owner_id=? AND a.kind='original' AND a.state='ready' ORDER BY p.ordinal`, request.DocumentID, owner)
		if err != nil {
			return ErrStorage
		}
		for rows.Next() {
			var page CapturedPage
			if err = rows.Scan(&page.ID, &page.OriginalArtifactID, &page.Rotation, &page.Size, &page.Color, &page.DPI, &page.SourceType, &page.PDFWidthPoints, &page.PDFHeightPoints); err != nil {
				rows.Close()
				return ErrStorage
			}
			pages = append(pages, page)
		}
		readErr := rows.Err()
		rows.Close()
		if readErr != nil {
			return ErrStorage
		}
		if len(pages) != pageCount {
			return ErrConflict
		}
		var sources []RenderSource
		if request.ReOCR {
			sources, err = loadReOCRSources(ctx, tx, owner, request.DocumentID, pages)
			if err != nil {
				return err
			}
		}
		if err = s.checkCapacity(ctx, tx, owner, request.ReserveBytes, limits); err != nil {
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
		// Every newly requested output is a new document revision. Invalidate the
		// old pointer before advancing the revision (its FK intentionally restricts
		// updates). Replay returns earlier, without incrementing again.
		if !request.ReOCR {
			if revision == 1<<63-1 {
				return ErrConflict
			}
			revision++
			if _, err = tx.ExecContext(ctx, "DELETE FROM published_outputs WHERE document_id=? AND owner_id=?", request.DocumentID, owner); err != nil {
				return ErrStorage
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE documents SET revision=?,state='processing' WHERE id=? AND owner_id=?", revision, request.DocumentID, owner); err != nil {
			return ErrStorage
		}
		jobID, err := opaqueID("job_")
		if err != nil {
			return ErrStorage
		}
		snapshot, err := json.Marshal(RenderSnapshot{Version: 1, ReOCR: request.ReOCR, Sources: sources, Settings: snapshotSettings, Pages: pages, OCROutput: request.ResolvedOutput})
		if err != nil || len(snapshot) > 1<<20 {
			return ErrInvalid
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES(?,?,?,'render','queued',?,?,?,?)`, jobID, request.DocumentID, owner, revision, string(snapshot), now, deadline); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,?)", jobID, owner, request.ReserveBytes); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,result_job_id,expires_at_ms,key_expires_at_ms) VALUES(?,?,'render',?,?,?,?)`, owner, keyHash[:], requestHash[:], jobID, receiptDeadline, keyExpiry); err != nil {
			return ErrStorage
		}
		current := s.now().UnixMilli()
		if current >= deadline || current >= expiry {
			return ErrConflict
		}
		out = Admission{JobID: jobID, DocumentID: request.DocumentID, Revision: revision}
		return nil
	})
	if err != nil {
		return Admission{}, err
	}
	return out, nil
}
func validAdmissionLimits(l AdmissionLimits) bool {
	return l.UserMaxBytes > 0 && l.UserMaxBytes <= 1<<60 && l.GlobalMaxBytes > 0 && l.GlobalMaxBytes <= 1<<60 && l.JobMaxBytes > 0 && l.JobMaxBytes <= l.UserMaxBytes && l.JobMaxBytes <= l.GlobalMaxBytes && l.MaxPages > 0 && l.MaxPages <= 10000 && l.JobTimeout >= time.Millisecond && l.JobTimeout <= 365*24*time.Hour && l.ReceiptRetention >= time.Millisecond && l.ReceiptRetention <= 365*24*time.Hour
}
func fits(used, reserved, extra, cap int64) bool {
	return used >= 0 && reserved >= 0 && extra > 0 && used <= cap && reserved <= cap-used && extra <= cap-used-reserved
}
func addMillis(now int64, d time.Duration) (int64, bool) {
	delta := d.Milliseconds()
	if now < 0 || delta < 1 || now > (1<<63-1)-delta {
		return 0, false
	}
	return now + delta, true
}
func sameHash(got []byte, want [32]byte) bool {
	if len(got) != 32 {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
func opaqueID(prefix string) (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:]), nil
}
func label(s string) bool {
	if len(s) == 0 || len(s) > 256 || !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// checkCapacity is shared by scan and render admission so outstanding reservations
// and cleanup-pending artifacts are charged identically across both operations.
func (s *Store) checkCapacity(ctx context.Context, tx *sql.Tx, owner string, extra int64, limits AdmissionLimits) error {
	var used, reserved, allUsed, allReserved int64
	for _, q := range []struct {
		query  string
		args   []any
		target *int64
	}{
		{"SELECT COALESCE(SUM(byte_size),0) FROM artifacts WHERE owner_id=?", []any{owner}, &used},
		{"SELECT COALESCE(SUM(byte_size),0) FROM reservations WHERE owner_id=?", []any{owner}, &reserved},
		{"SELECT COALESCE(SUM(byte_size),0) FROM artifacts", nil, &allUsed},
		{"SELECT COALESCE(SUM(byte_size),0) FROM reservations", nil, &allReserved},
	} {
		if err := tx.QueryRowContext(ctx, q.query, q.args...).Scan(q.target); err != nil {
			return ErrStorage
		}
	}
	if !fits(used, reserved, extra, limits.UserMaxBytes) || !fits(allUsed, allReserved, extra, limits.GlobalMaxBytes) {
		return ErrQuota
	}
	available, err := s.space(ctx)
	if err != nil || available < 0 {
		return ErrStorage
	}
	// Existing files are already deducted by the filesystem. Charge outstanding
	// reservations again conservatively, including partially-written scratch data.
	if !fits(0, allReserved, extra, available) {
		return ErrQuota
	}
	return nil
}

// Normalize object key order while preserving exact integer spelling and array
// order. Struct-field refactors must not change existing 72-hour receipt digests.
func canonicalCommand(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var normalized any
	if err = decoder.Decode(&normalized); err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

// checkExecutionCapacity re-observes physical capacity for already-reserved work.
// It adds no reservation and never releases resources on failure. The caller
// rolls back its tentative claim before a native process can be started.
func (s *Store) checkExecutionCapacity(ctx context.Context, tx *sql.Tx, owner, jobID string) error {
	var reserved int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(byte_size),0) FROM reservations").Scan(&reserved); err != nil {
		return storageOperationError(err)
	}
	available, err := s.space(ctx)
	if err != nil {
		return storageOperationError(err)
	}
	if available < 0 {
		return ErrStorage
	}
	if reserved > available {
		return ErrQuota
	}
	var valid int
	now := s.now().UnixMilli()
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.state='running' AND d.revision=j.input_revision AND j.deadline_at_ms>? AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting')`, jobID, owner, now, now).Scan(&valid)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	return readError(err)
}
