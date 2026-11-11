package documents

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"reflect"
	"scanner-kiosk/internal/correction"
)

// PhotoCrop coordinates always refer to the immutable normalized baseline.
type PhotoCrop struct {
	PageID       string `json:"page_id"`
	SourceSHA256 string `json:"source_sha256"`
	X            int    `json:"x"`
	Y            int    `json:"y"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
}

func (c PhotoCrop) valid() bool {
	b, e := hex.DecodeString(c.SourceSHA256)
	return id(c.PageID) && len(c.PageID) <= 96 && e == nil && len(b) == 32 && hex.EncodeToString(b) == c.SourceSHA256 && c.X >= 0 && c.Y >= 0 && c.Width > 0 && c.Height > 0 && c.X <= 12000 && c.Y <= 12000 && c.Width <= 12000 && c.Height <= 12000
}
func photoCropBaseline(ctx context.Context, tx *sql.Tx, owner, doc string, c PhotoCrop, pages []CapturedPage, sources []RenderSource) (ArtifactRecord, error) {
	if !c.valid() || len(pages) != len(sources) {
		return ArtifactRecord{}, ErrInvalid
	}
	for i, p := range pages {
		if p.ID == c.PageID {
			if p.SourceType != "photo" {
				return ArtifactRecord{}, ErrInvalid
			}
			a, e := loadCorrectionBaseline(ctx, tx, owner, doc, p, sources[i].Original)
			if e != nil {
				return a, e
			}
			if hex.EncodeToString(a.SHA256[:]) != c.SourceSHA256 {
				return ArtifactRecord{}, ErrConflict
			}
			if c.X > a.PixelWidth-c.Width || c.Y > a.PixelHeight-c.Height {
				return ArtifactRecord{}, ErrInvalid
			}
			return a, nil
		}
	}
	return ArtifactRecord{}, ErrUnavailable
}
func (s *Store) ReadPhotoBaseline(ctx context.Context, owner, doc, page string) (ArtifactReference, error) {
	original, e := s.ReadPageOriginal(ctx, owner, doc, page)
	if e != nil {
		return ArtifactReference{}, e
	}
	out := ArtifactReference{ExpiresAtMS: original.ExpiresAtMS}
	e = s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var err error
		out.Artifact, err = loadCorrectionBaseline(ctx, tx, owner, doc, CapturedPage{ID: page, SourceType: "photo"}, original.Artifact)
		if err != nil {
			return err
		}
		var expiry int64
		err = tx.QueryRowContext(ctx, `SELECT expires_at_ms FROM documents WHERE id=? AND owner_id=? AND expires_at_ms>? AND state NOT IN ('expired','deleting')`, doc, owner, s.now().UnixMilli()).Scan(&expiry)
		return readError(err)
	})
	return out, e
}

// CompletePhotoCrop publishes only a claimed manual job. It never publishes a PDF or OCR result.
func (s *Store) CompletePhotoCrop(ctx context.Context, owner, job, runner string, revision int64, a ArtifactRecord) error {
	if !id(job) || !id(runner) || revision < 1 || revision == 1<<63-1 || !id(a.ID) || !localPath(a.RelativePath) || a.MediaType != "image/png" || a.Size < 1 || a.SHA256 == ([32]byte{}) {
		return ErrInvalid
	}
	return s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		var doc, raw string
		var reserved int64
		e := tx.QueryRowContext(ctx, `SELECT d.id,j.configuration_json,r.byte_size FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id JOIN reservations r ON r.job_id=j.id AND r.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.runner_id=? AND j.kind='render' AND j.state='running' AND j.input_revision=? AND d.revision=j.input_revision AND d.state='processing' AND d.purpose IN ('document','photo') AND d.expires_at_ms>? AND j.deadline_at_ms>?`, job, owner, runner, revision, now, now).Scan(&doc, &raw, &reserved)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrConflict
		}
		if e != nil {
			return ErrStorage
		}
		var snap RenderSnapshot
		if json.Unmarshal([]byte(raw), &snap) != nil || snap.Version != 1 || snap.ReOCR || snap.Settings.OCR.Enabled || snap.Settings.Format != "pdf" || snap.ManualCrop == nil || snap.ManualSource == nil || a.Size > reserved {
			return ErrConflict
		}
		sources, e := loadReOCRSources(ctx, tx, owner, doc, snap.Pages)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(sources, snap.Sources) {
			return ErrConflict
		}
		base, e := photoCropBaseline(ctx, tx, owner, doc, *snap.ManualCrop, snap.Pages, sources)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(base, *snap.ManualSource) || a.PixelWidth != snap.ManualCrop.Width || a.PixelHeight != snap.ManualCrop.Height {
			return ErrConflict
		}
		c := *snap.ManualCrop
		var original ArtifactRecord
		for i, p := range snap.Pages {
			if p.ID == c.PageID {
				original = sources[i].Original
			}
		}
		tr := correction.Transform{CropQuad: [4]correction.Point{{X: float64(c.X), Y: float64(c.Y)}, {X: float64(c.X + c.Width), Y: float64(c.Y)}, {X: float64(c.X + c.Width), Y: float64(c.Y + c.Height)}, {X: float64(c.X), Y: float64(c.Y + c.Height)}}, OutputWidth: c.Width, OutputHeight: c.Height, ConfidenceKind: "unavailable"}
		if correction.Validate(tr, image.Rect(0, 0, base.PixelWidth, base.PixelHeight)) != nil {
			return ErrInvalid
		}
		transform, _ := json.Marshal(tr)
		if _, e = tx.ExecContext(ctx, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'intermediate','ready',?,'image/png',?,?,?,?)`, a.ID, doc, owner, a.RelativePath, a.Size, a.SHA256[:], revision, now); e != nil {
			return ErrStorage
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO job_artifacts(job_id,document_id,owner_id,artifact_id) VALUES(?,?,?,?)`, job, doc, owner, a.ID); e != nil {
			return ErrStorage
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO page_corrections(suggestion_id,document_id,owner_id,page_id,original_artifact_id,correction_artifact_id,source_sha256,correction_sha256,transform_json,confidence_kind,risk_flags,state,created_at_ms) VALUES(?,?,?,?,?,?,?,?,?,'unavailable','[]','suggested',?)`, a.ID, doc, owner, c.PageID, original.ID, a.ID, original.SHA256[:], a.SHA256[:], string(transform), now); e != nil {
			return ErrStorage
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`UPDATE page_corrections SET state='discarded',applied_at_ms=NULL WHERE document_id=? AND owner_id=? AND page_id=? AND state='applied'`, []any{doc, owner, c.PageID}},
			{`UPDATE page_corrections SET state='applied',applied_at_ms=? WHERE suggestion_id=?`, []any{now, a.ID}},
			{`DELETE FROM published_outputs WHERE document_id=? AND owner_id=?`, []any{doc, owner}},
			{`UPDATE jobs SET state='succeeded',heartbeat_at_ms=? WHERE id=? AND owner_id=?`, []any{now, job, owner}},
			{`DELETE FROM reservations WHERE job_id=? AND owner_id=?`, []any{job, owner}},
			{`UPDATE documents SET revision=revision+1,state='review' WHERE id=? AND owner_id=?`, []any{doc, owner}},
		} {
			if _, e = tx.ExecContext(ctx, q.sql, q.args...); e != nil {
				return ErrStorage
			}
		}
		current := s.now().UnixMilli()
		var valid int
		e = tx.QueryRowContext(ctx, `SELECT 1 FROM documents d JOIN jobs j ON j.document_id=d.id AND j.owner_id=d.owner_id WHERE j.id=? AND d.expires_at_ms>? AND j.deadline_at_ms>?`, job, current, current).Scan(&valid)
		return readError(e)
	})
}

// PhotoCropRecorded is internal reconciliation authority, including after expiry/disable.
func (s *Store) PhotoCropRecorded(ctx context.Context, owner, job string, a ArtifactRecord) (bool, error) {
	var id string
	var hash []byte
	var size int64
	var path string
	e := s.documents.QueryRowContext(ctx, `SELECT a.id,a.sha256,a.byte_size,a.relative_path FROM jobs j JOIN job_artifacts ja ON ja.job_id=j.id AND ja.owner_id=j.owner_id JOIN artifacts a ON a.id=ja.artifact_id AND a.document_id=ja.document_id AND a.owner_id=ja.owner_id JOIN page_corrections c ON c.correction_artifact_id=a.id AND c.owner_id=a.owner_id AND c.document_id=a.document_id WHERE j.id=? AND j.owner_id=? AND j.state='succeeded' AND json_type(j.configuration_json,'$.manual_crop')='object' AND a.id=? AND a.kind='intermediate' AND a.state='ready' AND a.media_type='image/png'`, job, owner, a.ID).Scan(&id, &hash, &size, &path)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, ErrStorage
	}
	if id != a.ID || size != a.Size || path != a.RelativePath || !sameHash(hash, a.SHA256) {
		return false, ErrConflict
	}
	return true, nil
}
