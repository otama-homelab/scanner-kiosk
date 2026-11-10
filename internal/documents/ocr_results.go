package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"reflect"

)

// OCRResultBundle is immutable metadata, never inline provider text or geometry.
// Every produced file is owned by the existing job_artifacts cleanup boundary.
type OCRResultBundle struct {
	ID            string
	JobID         string
	InputRevision int64
	Settings      RenderSettings
	Pages         []OCRResultPage
	Output        ArtifactRecord
}

type OCRResultPage struct {
	PageID, OriginalArtifactID, EffectiveArtifactID string
	OriginalSHA256, EffectiveSHA256                 [32]byte
	CorrectionID, TransformJSON                     string
	SourceType                                      string
	PDFWidthPoints, PDFHeightPoints                 float64
	Rotation, PixelWidth, PixelHeight               int
	PDFTextOrigin                                   string            // recognition or retained-source
	PDFRaster                                       *PDFRasterMapping `json:",omitempty"`
	Text                                            ArtifactRecord
	TranscriptUnavailable                           bool // legacy layer-only output has no transcript
	Layer                                           *ArtifactRecord
	Positions                                       *ArtifactRecord // nil means positions unavailable, never synthesized
	Preview                                         *ArtifactRecord // optional generated source preview
	Thumbnail                                       *ArtifactRecord
}

// PDFRasterMapping binds recognition coordinates to the unrotated original
// CropBox. Display rotation is a separate page property and is applied once.
type PDFRasterMapping struct {
	DPI, SourceRotation int
	MediaBox, CropBox   [4]float64
	ScaleX, ScaleY      float64
}

type ocrResultArtifact struct {
	artifact ArtifactRecord
	kind     string
}

func normalizeOCRBundle(b OCRResultBundle) ([]byte, []ocrResultArtifact, int64, error) {
	if !id(b.ID) || !id(b.JobID) || b.InputRevision < 1 || len(b.Pages) < 1 || len(b.Pages) > 1000 || !b.Settings.OCR.Enabled || b.Settings.OCR.ProviderID == "" || b.Settings.OCR.ModelID == "" || b.Settings.OCR.ModelVersion == "" {
		return nil, nil, 0, ErrInvalid
	}
	all := []ocrResultArtifact{{b.Output, "output"}}
	seenPages := map[string]bool{}
	for _, p := range b.Pages {
		if !id(p.PageID) || seenPages[p.PageID] || !id(p.OriginalArtifactID) || !id(p.EffectiveArtifactID) || p.OriginalSHA256 == ([32]byte{}) || p.EffectiveSHA256 == ([32]byte{}) || p.PixelWidth < 1 || p.PixelWidth > 50000 || p.PixelHeight < 1 || p.PixelHeight > 50000 || int64(p.PixelWidth)*int64(p.PixelHeight) > 250000000 || (p.Rotation != 0 && p.Rotation != 90 && p.Rotation != 180 && p.Rotation != 270) || (p.PDFTextOrigin != "recognition" && p.PDFTextOrigin != "retained-source") {
			return nil, nil, 0, ErrInvalid
		}
		seenPages[p.PageID] = true
		if p.PDFRaster != nil {
			m := p.PDFRaster
			b, c := m.MediaBox, m.CropBox
			if p.SourceType != "pdf" || m.DPI != 300 || (m.SourceRotation != 0 && m.SourceRotation != 90 && m.SourceRotation != 180 && m.SourceRotation != 270) {
				return nil, nil, 0, ErrInvalid
			}
			for _, v := range append(append([]float64{}, b[:]...), c[:]...) {
				if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 20000 {
					return nil, nil, 0, ErrInvalid
				}
			}
			w, h := c[2]-c[0], c[3]-c[1]
			pointWidth, pointHeight := b[2]-b[0], b[3]-b[1]
			if m.SourceRotation%180 != 0 {
				pointWidth, pointHeight = pointHeight, pointWidth
			}
			if pointWidth != p.PDFWidthPoints || pointHeight != p.PDFHeightPoints {
				return nil, nil, 0, ErrInvalid
			}
			if w <= 0 || h <= 0 || c[0] < b[0] || c[1] < b[1] || c[2] > b[2] || c[3] > b[3] || p.PixelWidth != int(math.Ceil(w*300/72)) || p.PixelHeight != int(math.Ceil(h*300/72)) || m.ScaleX != float64(p.PixelWidth)/w || m.ScaleY != float64(p.PixelHeight)/h {
				return nil, nil, 0, ErrInvalid
			}
		}
		if p.Text.MediaType != "text/plain" || (p.TranscriptUnavailable && (p.Text.Size != 0 || p.Positions != nil || p.Layer == nil)) {
			return nil, nil, 0, ErrInvalid
		}
		all = append(all, ocrResultArtifact{p.Text, "ocr_text"})
		if p.Layer != nil {
			if p.Layer.MediaType != "application/pdf" || p.Layer.Size < 1 {
				return nil, nil, 0, ErrInvalid
			}
			all = append(all, ocrResultArtifact{*p.Layer, "intermediate"})
		}
		if p.Positions != nil {
			if p.Positions.MediaType != "application/json" || p.Positions.Size < 1 || p.Positions.PixelWidth != p.PixelWidth || p.Positions.PixelHeight != p.PixelHeight {
				return nil, nil, 0, ErrInvalid
			}
			all = append(all, ocrResultArtifact{*p.Positions, "intermediate"})
		}
		if p.Preview != nil {
			if p.Preview.Size < 1 || (p.Preview.MediaType != "image/png" && p.Preview.MediaType != "image/jpeg") {
				return nil, nil, 0, ErrInvalid
			}
			if p.Preview.PixelWidth != p.PixelWidth || p.Preview.PixelHeight != p.PixelHeight {
				return nil, nil, 0, ErrInvalid
			}
			all = append(all, ocrResultArtifact{*p.Preview, "intermediate"})
		}
		if p.Thumbnail != nil {
			a := p.Thumbnail
			if a.MediaType != "image/jpeg" || a.Size < 1 || a.Size > 1<<20 || a.PixelWidth < 1 || a.PixelWidth > 512 || a.PixelHeight < 1 || a.PixelHeight > 512 {
				return nil, nil, 0, ErrInvalid
			}
			all = append(all, ocrResultArtifact{*a, "thumbnail"})
		}
	}
	if b.Output.MediaType != "application/pdf" || b.Output.Size < 1 {
		return nil, nil, 0, ErrInvalid
	}
	seen := map[string]bool{}
	paths := map[string]bool{}
	var total int64
	for _, v := range all {
		a := v.artifact
		if !id(a.ID) || seen[a.ID] || !localPath(a.RelativePath) || len(a.RelativePath) > 1024 || paths[a.RelativePath] || a.Size < 0 || a.Size > 1<<30 || a.SHA256 == ([32]byte{}) {
			return nil, nil, 0, ErrInvalid
		}
		for _, p := range b.Pages {
			if a.ID == p.OriginalArtifactID || a.ID == p.EffectiveArtifactID {
				return nil, nil, 0, ErrInvalid
			}
		}
		seen[a.ID] = true
		paths[a.RelativePath] = true
		total += a.Size
	}
	raw, err := json.Marshal(b)
	if err != nil || len(raw) > 2<<20 {
		return nil, nil, 0, ErrInvalid
	}
	return raw, all, total, nil
}

// validateOCRSources binds order, originals and the currently applied correction.
func validateOCRSources(ctx context.Context, tx *sql.Tx, owner, doc string, b OCRResultBundle) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,original_artifact_id,CASE source_type WHEN 'scan' THEN '' ELSE source_type END,rotation,COALESCE(capture_size,''),COALESCE(capture_color,''),COALESCE(capture_dpi,0),COALESCE(pdf_width_points,0),COALESCE(pdf_height_points,0) FROM pages WHERE document_id=? AND owner_id=? ORDER BY ordinal LIMIT 1001`, doc, owner)
	if err != nil {
		return ErrStorage
	}
	var pages []CapturedPage
	for rows.Next() {
		var p CapturedPage
		if err = rows.Scan(&p.ID, &p.OriginalArtifactID, &p.SourceType, &p.Rotation, &p.Size, &p.Color, &p.DPI, &p.PDFWidthPoints, &p.PDFHeightPoints); err != nil {
			rows.Close()
			return ErrStorage
		}
		pages = append(pages, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrStorage
	}
	if len(pages) != len(b.Pages) {
		return ErrConflict
	}
	sources, err := loadReOCRSources(ctx, tx, owner, doc, pages)
	if err != nil {
		return err
	}
	for i, page := range pages {
		p, source := b.Pages[i], sources[i]
		kind := page.SourceType
		if kind == "" {
			kind = "scan"
		}
		if p.PageID != page.ID || p.OriginalArtifactID != source.Original.ID || p.OriginalSHA256 != source.Original.SHA256 || p.EffectiveArtifactID != source.Effective.ID || p.EffectiveSHA256 != source.Effective.SHA256 || p.SourceType != kind || p.Rotation != page.Rotation || p.CorrectionID != source.CorrectionID || p.TransformJSON != source.TransformJSON || p.PDFWidthPoints != page.PDFWidthPoints || p.PDFHeightPoints != page.PDFHeightPoints {
			return ErrConflict
		}
		if kind != "pdf" && (p.PixelWidth != source.Effective.PixelWidth || p.PixelHeight != source.Effective.PixelHeight) {
			return ErrConflict
		}
		if kind != "pdf" && p.PDFTextOrigin == "retained-source" {
			return ErrInvalid
		}
	}
	return nil
}

func (s *Store) CompleteOCRResult(ctx context.Context, owner, job, runner string, revision int64, b OCRResultBundle) error {
	raw, all, total, err := normalizeOCRBundle(b)
	if err != nil {
		return err
	}
	if !id(runner) || job != b.JobID || revision != b.InputRevision {
		return ErrInvalid
	}
	return s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		var doc, config string
		var reserved int64
		err := tx.QueryRowContext(ctx, `SELECT d.id,j.configuration_json,r.byte_size FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id JOIN reservations r ON r.job_id=j.id AND r.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.kind='render' AND j.state='running' AND j.runner_id=? AND j.input_revision=? AND d.revision=j.input_revision AND j.deadline_at_ms>? AND d.expires_at_ms>? AND d.state='processing' AND d.purpose='document'`, job, owner, runner, revision, now, now).Scan(&doc, &config, &reserved)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return ErrStorage
		}
		if total > reserved {
			return ErrConflict
		}
		var snapshot RenderSnapshot
		if json.Unmarshal([]byte(config), &snapshot) != nil || snapshot.Version != 1 || !reflect.DeepEqual(snapshot.Settings, b.Settings) || len(snapshot.Pages) != len(b.Pages) {
			return ErrConflict
		}
		for i, p := range snapshot.Pages {
			v := b.Pages[i]
			if p.ID != v.PageID || p.OriginalArtifactID != v.OriginalArtifactID || p.Rotation != v.Rotation {
				return ErrConflict
			}
		}
		if snapshot.ReOCR {
			sources, err := loadReOCRSources(ctx, tx, owner, doc, snapshot.Pages)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(sources, snapshot.Sources) {
				return ErrConflict
			}
		}
		if err = validateOCRSources(ctx, tx, owner, doc, b); err != nil {
			return err
		}
		for _, v := range all {
			a := v.artifact
			if _, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,?,'ready',?,?,?,?,?,?)`, a.ID, doc, owner, v.kind, a.RelativePath, a.MediaType, a.Size, a.SHA256[:], revision, now); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO job_artifacts(job_id,document_id,owner_id,artifact_id) VALUES(?,?,?,?)`, job, doc, owner, a.ID); err != nil {
				return ErrStorage
			}
		}
		digest := sha256Sum(raw)
		if _, err = tx.ExecContext(ctx, `INSERT INTO ocr_results(id,job_id,document_id,owner_id,input_revision,output_artifact_id,bundle_json,bundle_sha256,created_at_ms) VALUES(?,?,?,?,?,?,?,?,?)`, b.ID, job, doc, owner, revision, b.Output.ID, string(raw), digest[:], now); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `UPDATE jobs SET state='succeeded',heartbeat_at_ms=? WHERE id=? AND owner_id=?`, now, job, owner); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM reservations WHERE job_id=? AND owner_id=?`, job, owner); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `UPDATE documents SET state='ready' WHERE id=? AND owner_id=?`, doc, owner); err != nil {
			return ErrStorage
		}
		current := s.now().UnixMilli()
		var valid int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.deadline_at_ms>? AND d.expires_at_ms>?`, job, owner, current, current).Scan(&valid)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		return readError(err)
	})
}

// OCRResultRecorded is worker-internal full-bundle reconciliation, including
// disabled/expired owners. A contradictory succeeded job never authorizes unlink.
func (s *Store) OCRResultRecorded(ctx context.Context, owner, job string, b OCRResultBundle) (bool, error) {
	raw, all, _, err := normalizeOCRBundle(b)
	if err != nil || !id(owner) || job != b.JobID {
		return false, ErrInvalid
	}
	tx, err := s.documents.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, ErrStorage
	}
	defer tx.Rollback()
	var state string
	err = tx.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=? AND owner_id=? AND kind='render'`, job, owner).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrStorage
	}
	if state != "succeeded" {
		return false, nil
	}
	var stored string
	var hash []byte
	err = tx.QueryRowContext(ctx, `SELECT bundle_json,bundle_sha256 FROM ocr_results WHERE job_id=? AND owner_id=? AND id=?`, job, owner, b.ID).Scan(&stored, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrConflict
	}
	if err != nil {
		return false, ErrStorage
	}
	if stored != string(raw) || !sameHash(hash, sha256Sum(raw)) {
		return false, ErrConflict
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM job_artifacts WHERE job_id=? AND owner_id=?`, job, owner).Scan(&count); err != nil {
		return false, ErrStorage
	}
	if count != len(all) {
		return false, ErrConflict
	}
	for _, v := range all {
		a := v.artifact
		var n int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM artifacts a JOIN job_artifacts x ON x.artifact_id=a.id AND x.document_id=a.document_id AND x.owner_id=a.owner_id WHERE x.job_id=? AND x.owner_id=? AND a.id=? AND a.kind=? AND a.relative_path=? AND a.media_type=? AND a.byte_size=? AND a.sha256=? AND a.input_revision=? AND a.state IN ('ready','deleting')`, job, owner, a.ID, v.kind, a.RelativePath, a.MediaType, a.Size, a.SHA256[:], b.InputRevision).Scan(&n)
		if err != nil {
			return false, ErrStorage
		}
		if n != 1 {
			return false, ErrConflict
		}
	}
	return true, nil
}
