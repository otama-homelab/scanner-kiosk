package documents

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"scanner-kiosk/internal/correction"
)

// Projection contains public metadata only. Storage paths, owners and provider
// configuration are deliberately absent. All fields are read in one transaction.
type Projection struct {
	OCRResults    []OCRResultSummary `json:"ocr_results"`
	ID            string             `json:"id"`
	Revision      int64              `json:"revision"`
	ActiveJobID   *string            `json:"active_job_id"`
	PhotoFormat   *string            `json:"photo_format"`
	Purpose       string             `json:"purpose"`
	State         string             `json:"state"`
	CreatedAt     time.Time          `json:"created_at"`
	ExpiresAt     time.Time          `json:"expires_at"`
	PageCount     int                `json:"page_count"`
	Pages         []PageProjection   `json:"pages"`
	Output        *OutputProjection  `json:"output"`
	ReadyRevision *int64             `json:"ready_revision"`
}
type CaptureProjection struct {
	Size  string `json:"size"`
	Color string `json:"color"`
	DPI   int    `json:"dpi"`
}
type PDFPageProjection struct {
	WidthPoints  float64 `json:"width_points"`
	HeightPoints float64 `json:"height_points"`
}
type PageProjection struct {
	Photo        *PhotoProjection      `json:"photo,omitempty"`
	SourceType   string                `json:"source_type"`
	PDF          *PDFPageProjection    `json:"pdf,omitempty"`
	ID           string                `json:"id"`
	Ordinal      int                   `json:"ordinal"`
	Rotation     int                   `json:"rotation"`
	Capture      *CaptureProjection    `json:"capture"`
	ThumbnailURL string                `json:"thumbnail_url"`
	PreviewURL   string                `json:"preview_url"`
	Correction   *CorrectionProjection `json:"correction,omitempty"`
}
type PhotoProjection struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	SHA256 string `json:"sha256"`
}
type CorrectionProjection struct {
	SuggestionID         string               `json:"suggestion_id"`
	CorrectionArtifactID string               `json:"correction_artifact_id"`
	SourceSHA256         string               `json:"source_sha256"`
	CorrectionSHA256     string               `json:"correction_sha256"`
	PreviewURL           string               `json:"preview_url"`
	Transform            correction.Transform `json:"transform"`
	ConfidenceKind       string               `json:"confidence_kind"`
	ConfidenceValue      *float64             `json:"confidence_value,omitempty"`
	RiskFlags            []string             `json:"risk_flags"`
	State                string               `json:"state"`
	CanUndo              bool                 `json:"can_undo"`
}
type OutputProjection struct {
	Revision    int64  `json:"revision"`
	Format      string `json:"format"`
	ByteSize    int64  `json:"byte_size"`
	DownloadURL string `json:"download_url"`
}

// Position is an ordering hint, never an authorization capability.
type Position struct {
	ExpiresAtMS int64
	ID          string
}

func (s *Store) ReadDocument(ctx context.Context, owner, documentID string) (Projection, error) {
	var out Projection
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error { var err error; out, err = s.project(ctx, tx, owner, documentID); return err })
	if err != nil {
		return Projection{}, err
	}
	return out, nil
}

// projectionColumns is shared by detail and paginated root selection. Correlated
// scalar metadata stays inside this statement, never one API query per document.
const projectionColumns = `d.id,d.revision,d.purpose,d.state,d.created_at_ms,d.expires_at_ms,
 CASE WHEN d.purpose='photo' THEN COALESCE((SELECT COALESCE(json_extract(j.configuration_json,'$.settings.format'),json_extract(j.configuration_json,'$.photo_format')) FROM jobs j WHERE j.document_id=d.id AND j.owner_id=d.owner_id AND j.kind IN ('scan','render') AND COALESCE(json_extract(j.configuration_json,'$.settings.format'),json_extract(j.configuration_json,'$.photo_format')) IN ('jpeg','png') ORDER BY j.input_revision DESC,j.created_at_ms DESC,j.id DESC LIMIT 1),'jpeg') END,
 (SELECT j.id FROM jobs j WHERE j.document_id=d.id AND j.owner_id=d.owner_id AND j.kind!='cleanup' AND j.state IN ('queued','running','cancelling','reconciling') LIMIT 1),
 (SELECT json_group_array(json_object('id',r.id,'state',r.state,'input_revision',r.input_revision)) FROM ocr_results r WHERE r.document_id=d.id AND r.owner_id=d.owner_id AND (r.state='candidate' OR r.state='adopted' AND r.id=(SELECT r2.id FROM ocr_results r2 WHERE r2.document_id=d.id AND r2.owner_id=d.owner_id AND r2.state='adopted' ORDER BY r2.adopted_revision DESC LIMIT 1)))`

type projectionRow interface{ Scan(...any) error }

func scanProjection(row projectionRow) (Projection, error) {
	var p Projection
	var created, expires int64
	var photo, active sql.NullString
	var results string
	if err := row.Scan(&p.ID, &p.Revision, &p.Purpose, &p.State, &created, &expires, &photo, &active, &results); err != nil {
		return Projection{}, readError(err)
	}
	p.CreatedAt = time.UnixMilli(created).UTC()
	p.ExpiresAt = time.UnixMilli(expires).UTC()
	p.Pages = []PageProjection{}
	if json.Unmarshal([]byte(results), &p.OCRResults) != nil || len(p.OCRResults) > 2 {
		return Projection{}, ErrStorage
	}
	for i := range p.OCRResults {
		p.OCRResults[i].URL = "/api/documents/" + p.ID + "/ocr-results/" + p.OCRResults[i].ID
	}
	if photo.Valid {
		p.PhotoFormat = &photo.String
	}
	if active.Valid {
		p.ActiveJobID = &active.String
	}
	return p, nil
}
func (s *Store) ReadHistory(ctx context.Context, owner string, limit int, after *Position) ([]Projection, *Position, error) {
	if limit < 1 || limit > 100 || after != nil && (after.ExpiresAtMS < 0 || !id(after.ID)) {
		return nil, nil, ErrInvalid
	}
	out := []Projection{}
	var next *Position
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		expiry := int64(0)
		last := ""
		if after != nil {
			expiry, last = after.ExpiresAtMS, after.ID
		}
		rows, err := tx.QueryContext(ctx, `SELECT `+projectionColumns+` FROM documents d WHERE d.owner_id=? AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting') AND NOT `+discardableEmptyDocumentSQL+` AND (d.expires_at_ms>? OR (d.expires_at_ms=? AND d.id>?)) ORDER BY d.expires_at_ms,d.id LIMIT ?`, owner, s.now().UnixMilli(), expiry, expiry, last, limit+1)
		if err != nil {
			return ErrStorage
		}
		for rows.Next() {
			p, err := scanProjection(rows)
			if err != nil {
				rows.Close()
				return err
			}
			out = append(out, p)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ErrStorage
		}
		if len(out) > limit {
			p := out[limit-1]
			next = &Position{ExpiresAtMS: p.ExpiresAt.UnixMilli(), ID: p.ID}
			out = out[:limit]
		}
		if err = s.projectContent(ctx, tx, owner, out); err != nil {
			return err
		}
		current := s.now()
		filtered := out[:0]
		for _, p := range out {
			if p.ExpiresAt.After(current) {
				filtered = append(filtered, p)
			}
		}
		out = filtered
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return out, next, nil
}
func (s *Store) project(ctx context.Context, tx *sql.Tx, owner, documentID string) (Projection, error) {
	p, err := scanProjection(tx.QueryRowContext(ctx, `SELECT `+projectionColumns+` FROM documents d WHERE d.id=? AND d.owner_id=? AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting')`, documentID, owner, s.now().UnixMilli()))
	if err != nil {
		return Projection{}, err
	}
	values := []Projection{p}
	if err = s.projectContent(ctx, tx, owner, values); err != nil {
		return Projection{}, err
	}
	if !values[0].ExpiresAt.After(s.now()) {
		return Projection{}, ErrUnavailable
	}
	return values[0], nil
}

// projectContent loads page/output metadata for a bounded selected root set in
// two queries. Owner predicates apply to both; stale outputs never become ready.
func (s *Store) projectContent(ctx context.Context, tx *sql.Tx, owner string, values []Projection) error {
	if len(values) == 0 {
		return nil
	}
	positions := map[string]int{}
	marks := make([]string, 0, len(values))
	args := []any{owner}
	for i, p := range values {
		positions[p.ID] = i
		marks = append(marks, "?")
		args = append(args, p.ID)
	}
	selected := strings.Join(marks, ",")
	rows, err := tx.QueryContext(ctx, `SELECT document_id,id,ordinal,rotation,COALESCE(capture_size,''),COALESCE(capture_color,''),COALESCE(capture_dpi,0),source_type,COALESCE(pdf_width_points,0),COALESCE(pdf_height_points,0) FROM pages WHERE owner_id=? AND document_id IN (`+selected+`) ORDER BY document_id,ordinal`, args...)
	if err != nil {
		return ErrStorage
	}
	for rows.Next() {
		var document string
		var page PageProjection
		var capture CaptureProjection
		var width, height float64
		if err = rows.Scan(&document, &page.ID, &page.Ordinal, &page.Rotation, &capture.Size, &capture.Color, &capture.DPI, &page.SourceType, &width, &height); err != nil {
			rows.Close()
			return ErrStorage
		}
		if page.SourceType == "pdf" {
			page.PDF = &PDFPageProjection{WidthPoints: width, HeightPoints: height}
		} else if page.SourceType == "scan" {
			page.Capture = &capture
		}
		i, ok := positions[document]
		if !ok {
			rows.Close()
			return ErrStorage
		}
		base := "/api/documents/" + document + "/pages/" + page.ID
		page.ThumbnailURL = base + "/thumbnail"
		page.PreviewURL = base + "/preview"
		values[i].Pages = append(values[i].Pages, page)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrStorage
	}
	if err = s.projectCorrections(ctx, tx, owner, values); err != nil {
		return err
	}
	for i := range values {
		for j := range values[i].Pages {
			p := &values[i].Pages[j]
			if p.SourceType != "photo" {
				continue
			}
			var a ArtifactRecord
			var digest []byte
			err := tx.QueryRowContext(ctx, `SELECT a.id,a.sha256 FROM pages p JOIN artifacts a ON a.id=p.original_artifact_id AND a.owner_id=p.owner_id AND a.document_id=p.document_id WHERE p.id=? AND p.owner_id=? AND p.document_id=? AND a.state='ready' AND a.kind='original'`, p.ID, owner, values[i].ID).Scan(&a.ID, &digest)
			if err != nil || len(digest) != 32 {
				return ErrConflict
			}
			copy(a.SHA256[:], digest)
			base, e := loadPhotoBaseline(ctx, tx, owner, values[i].ID, p.ID, a)
			if e != nil {
				return e
			}
			p.Photo = &PhotoProjection{Width: base.PixelWidth, Height: base.PixelHeight, SHA256: hex.EncodeToString(base.SHA256[:])}
		}
	}
	for i := range values {
		values[i].PageCount = len(values[i].Pages)
	}
	rows, err = tx.QueryContext(ctx, `SELECT o.document_id,o.revision,a.media_type,a.byte_size FROM published_outputs o JOIN artifacts a ON a.id=o.artifact_id AND a.document_id=o.document_id AND a.owner_id=o.owner_id WHERE o.owner_id=? AND o.document_id IN (`+selected+`) AND a.state='ready' AND a.kind='output' AND (a.input_revision=o.revision OR EXISTS(SELECT 1 FROM ocr_results r WHERE r.document_id=o.document_id AND r.owner_id=o.owner_id AND r.output_artifact_id=a.id AND r.input_revision=a.input_revision AND r.state='adopted' AND r.adopted_revision=o.revision))`, args...)
	if err != nil {
		return ErrStorage
	}
	defer rows.Close()
	for rows.Next() {
		var document, media string
		var output OutputProjection
		if err = rows.Scan(&document, &output.Revision, &media, &output.ByteSize); err != nil {
			return ErrStorage
		}
		i, ok := positions[document]
		if !ok {
			return ErrStorage
		}
		if output.Revision != values[i].Revision {
			continue
		}
		switch media {
		case "application/pdf":
			output.Format = "pdf"
		case "image/jpeg":
			output.Format = "jpeg"
		case "image/png":
			output.Format = "png"
		default:
			return ErrStorage
		}
		output.DownloadURL = "/api/documents/" + document + "/download"
		values[i].Output = &output
		values[i].ReadyRevision = &output.Revision
	}
	if rows.Err() != nil {
		return ErrStorage
	}
	return nil
}

func (s *Store) projectCorrections(ctx context.Context, tx *sql.Tx, owner string, values []Projection) error {
	if len(values) == 0 {
		return nil
	}
	positions := map[string]int{}
	marks := make([]string, 0, len(values))
	args := []any{owner}
	for index, value := range values {
		positions[value.ID] = index
		marks = append(marks, "?")
		args = append(args, value.ID)
	}
	rows, err := tx.QueryContext(ctx, `SELECT pc.document_id,pc.page_id,pc.suggestion_id,pc.correction_artifact_id,pc.source_sha256,pc.correction_sha256,pc.transform_json,pc.confidence_kind,pc.confidence_value,pc.risk_flags,pc.state
 FROM page_corrections pc JOIN artifacts a ON a.id=pc.correction_artifact_id AND a.document_id=pc.document_id AND a.owner_id=pc.owner_id
 WHERE pc.owner_id=? AND pc.document_id IN (`+strings.Join(marks, ",")+`) AND pc.state IN ('suggested','applied')
 AND a.kind='intermediate' AND a.state='ready' AND a.media_type='image/png'
 ORDER BY pc.document_id,pc.page_id,CASE pc.state WHEN 'applied' THEN 0 ELSE 1 END,pc.created_at_ms DESC,pc.suggestion_id DESC`, args...)
	if err != nil {
		return ErrStorage
	}
	defer rows.Close()
	type selected struct {
		document, page string
		value          CorrectionProjection
	}
	var chosen []selected
	for rows.Next() {
		var document, page string
		var source, correctionDigest []byte
		var transformJSON, riskJSON string
		var confidence sql.NullFloat64
		var value CorrectionProjection
		if err = rows.Scan(&document, &page, &value.SuggestionID, &value.CorrectionArtifactID, &source, &correctionDigest, &transformJSON, &value.ConfidenceKind, &confidence, &riskJSON, &value.State); err != nil {
			return ErrStorage
		}
		if len(source) != 32 || len(correctionDigest) != 32 || json.Unmarshal([]byte(transformJSON), &value.Transform) != nil || json.Unmarshal([]byte(riskJSON), &value.RiskFlags) != nil {
			return ErrStorage
		}
		value.SourceSHA256 = hex.EncodeToString(source)
		value.CorrectionSHA256 = hex.EncodeToString(correctionDigest)
		if confidence.Valid {
			value.ConfidenceValue = &confidence.Float64
		}
		value.PreviewURL = "/api/documents/" + document + "/pages/" + page + "/correction/preview"
		value.CanUndo = value.State == "applied"
		chosen = append(chosen, selected{document: document, page: page, value: value})
	}
	if rows.Err() != nil {
		return ErrStorage
	}
	for _, item := range chosen {
		index, ok := positions[item.document]
		if !ok {
			return ErrStorage
		}
		found := false
		for pageIndex := range values[index].Pages {
			page := &values[index].Pages[pageIndex]
			if page.ID != item.page {
				continue
			}
			if page.Correction == nil || item.value.State == "applied" {
				value := item.value
				page.Correction = &value
			}
			found = true
			break
		}
		if !found {
			return ErrStorage
		}
	}
	return nil
}
