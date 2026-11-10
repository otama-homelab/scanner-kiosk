package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	"io"
	"reflect"
	"strings"

	"scanner-kiosk/internal/correction"
)

// RenderSource freezes both immutable provenance and the exact effective input.
// Correction metadata never replaces CapturedPage.OriginalArtifactID.
type RenderSource struct {
	Original      ArtifactRecord `json:"original"`
	Effective     ArtifactRecord `json:"effective"`
	CorrectionID  string         `json:"correction_id,omitempty"`
	TransformJSON string         `json:"transform_json,omitempty"`
}

// RenderWork is worker-internal metadata, never an HTTP response. Inputs follow
// the immutable snapshot order; paths are resolved only by the rooted file scope.
type RenderWork struct {
	Job                  Job
	Snapshot             RenderSnapshot
	Inputs               []ArtifactRecord
	Originals            []ArtifactRecord
	ReserveBytes         int64
	ThumbnailOriginalIDs map[string]bool
}

// LoadRenderWork resolves only this runner's current, running render. It does not
// claim/retry work or open files. Publication must recheck all mutable predicates.
func (s *Store) LoadRenderWork(ctx context.Context, owner, jobID, runnerID string) (RenderWork, error) {
	var out RenderWork
	if !id(jobID) || !id(runnerID) {
		return out, ErrInvalid
	}
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		var expiry int64
		err := tx.QueryRowContext(ctx, `SELECT j.id,j.document_id,j.kind,j.configuration_json,j.input_revision,j.deadline_at_ms,r.byte_size,d.expires_at_ms
 FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id
 JOIN reservations r ON r.job_id=j.id AND r.owner_id=j.owner_id
 WHERE j.id=? AND j.owner_id=? AND j.runner_id=? AND j.kind='render' AND j.state='running' AND j.started_at_ms IS NOT NULL
 AND d.revision=j.input_revision AND d.state='processing' AND j.deadline_at_ms>? AND d.expires_at_ms>?`, jobID, owner, runnerID, now, now).Scan(&out.Job.ID, &out.Job.DocumentID, &out.Job.Kind, &out.Job.Configuration, &out.Job.Revision, &out.Job.DeadlineAtMS, &out.ReserveBytes, &expiry)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return ErrStorage
		}
		if len(out.Job.Configuration) > 1<<20 || out.ReserveBytes < 1 {
			return ErrInvalid
		}
		decoder := json.NewDecoder(strings.NewReader(out.Job.Configuration))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&out.Snapshot) != nil || decoder.Decode(new(any)) != io.EOF || out.Snapshot.Version != 1 || len(out.Snapshot.Pages) == 0 || len(out.Snapshot.Pages) > 10000 {
			return ErrInvalid
		}
		settings, err := normalizeRender(out.Snapshot.Settings)
		if err != nil || !reflect.DeepEqual(settings, out.Snapshot.Settings) {
			return ErrInvalid
		}
		rows, err := tx.QueryContext(ctx, `SELECT p.id,p.original_artifact_id,p.rotation,COALESCE(p.capture_size,''),COALESCE(p.capture_color,''),COALESCE(p.capture_dpi,0),CASE p.source_type WHEN 'scan' THEN '' ELSE p.source_type END,COALESCE(p.pdf_width_points,0),COALESCE(p.pdf_height_points,0),a.relative_path,a.media_type,a.byte_size,a.sha256,EXISTS(SELECT 1 FROM page_thumbnails t JOIN artifacts thumb ON thumb.id=t.artifact_id AND thumb.document_id=t.document_id AND thumb.owner_id=t.owner_id WHERE t.original_artifact_id=(CASE WHEN p.source_type='photo' THEN COALESCE((SELECT c.correction_artifact_id FROM page_corrections c WHERE c.page_id=p.id AND c.document_id=p.document_id AND c.owner_id=p.owner_id AND c.state='applied'),(SELECT ps.normalized_artifact_id FROM photo_sources ps WHERE ps.page_id=p.id AND ps.document_id=p.document_id AND ps.owner_id=p.owner_id)) ELSE p.original_artifact_id END) AND thumb.kind='thumbnail' AND thumb.state='ready')
 FROM pages p JOIN artifacts a ON a.id=p.original_artifact_id AND a.document_id=p.document_id AND a.owner_id=p.owner_id
 WHERE p.document_id=? AND p.owner_id=? AND a.kind='original' AND a.state='ready' ORDER BY p.ordinal`, out.Job.DocumentID, owner)
		if err != nil {
			return ErrStorage
		}
		defer rows.Close()
		seen := map[string]bool{}
		out.ThumbnailOriginalIDs = map[string]bool{}
		for rows.Next() {
			var page CapturedPage
			var input ArtifactRecord
			var digest []byte
			var hasThumbnail bool
			if err = rows.Scan(&page.ID, &page.OriginalArtifactID, &page.Rotation, &page.Size, &page.Color, &page.DPI, &page.SourceType, &page.PDFWidthPoints, &page.PDFHeightPoints, &input.RelativePath, &input.MediaType, &input.Size, &digest, &hasThumbnail); err != nil {
				return ErrStorage
			}
			index := len(out.Inputs)
			if index >= len(out.Snapshot.Pages) || page != out.Snapshot.Pages[index] {
				return ErrConflict
			}
			input.ID = page.OriginalArtifactID
			if hasThumbnail {
				out.ThumbnailOriginalIDs[input.ID] = true
			}
			if !id(input.ID) || seen[input.ID] || !localPath(input.RelativePath) || input.Size < 1 || len(digest) != 32 || !validPageSource(page, input.MediaType) {
				return ErrInvalid
			}
			seen[input.ID] = true
			copy(input.SHA256[:], digest)
			out.Inputs = append(out.Inputs, input)
		}
		if rows.Err() != nil {
			return ErrStorage
		}
		if len(out.Inputs) != len(out.Snapshot.Pages) {
			return ErrConflict
		}
		out.Originals = append([]ArtifactRecord(nil), out.Inputs...)
		if out.Snapshot.ReOCR || hasPhotoPages(out.Snapshot.Pages) {
			// Close the original cursor before issuing the source lookup.
			rows.Close()
			sources, err := loadReOCRSources(ctx, tx, owner, out.Job.DocumentID, out.Snapshot.Pages)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(sources, out.Snapshot.Sources) {
				return ErrConflict
			}
			oldThumbs := out.ThumbnailOriginalIDs
			out.ThumbnailOriginalIDs = map[string]bool{}
			out.Inputs = nil
			out.Originals = nil
			for _, source := range sources {
				effective := source.Effective
				if !out.Snapshot.ReOCR && out.Snapshot.Pages[len(out.Inputs)].SourceType != "photo" {
					effective = source.Original
				}
				if oldThumbs[source.Original.ID] {
					out.ThumbnailOriginalIDs[effective.ID] = true
				}
				out.Inputs = append(out.Inputs, effective)
				out.Originals = append(out.Originals, source.Original)
			}
		} else if len(out.Snapshot.Sources) != 0 {
			return ErrInvalid
		}
		current := s.now().UnixMilli()
		if current >= expiry || current >= out.Job.DeadlineAtMS {
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		return RenderWork{}, err
	}
	return out, nil
}

// loadReOCRSources uses the same transaction as admission/execution. Missing or
// invalid applied corrections fail closed; they never select the original.
func loadReOCRSources(ctx context.Context, tx *sql.Tx, owner, doc string, pages []CapturedPage) ([]RenderSource, error) {
	if len(pages) == 0 || len(pages) > 1000 {
		return nil, ErrInvalid
	}
	out := make([]RenderSource, 0, len(pages))
	for _, page := range pages {
		var source RenderSource
		var originalHash []byte
		err := tx.QueryRowContext(ctx, `SELECT a.id,a.relative_path,a.media_type,a.byte_size,a.sha256,COALESCE(p.pixel_width,0),COALESCE(p.pixel_height,0)
 FROM pages p JOIN artifacts a ON a.id=p.original_artifact_id AND a.owner_id=p.owner_id AND a.document_id=p.document_id
 WHERE p.id=? AND p.document_id=? AND p.owner_id=? AND a.id=? AND a.kind='original' AND a.state='ready'`, page.ID, doc, owner, page.OriginalArtifactID).Scan(&source.Original.ID, &source.Original.RelativePath, &source.Original.MediaType, &source.Original.Size, &originalHash, &source.Original.PixelWidth, &source.Original.PixelHeight)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrConflict
		}
		if err != nil {
			return nil, ErrStorage
		}
		if !localPath(source.Original.RelativePath) || source.Original.Size < 1 || len(originalHash) != 32 || !validPageSource(page, source.Original.MediaType) {
			return nil, ErrInvalid
		}
		copy(source.Original.SHA256[:], originalHash)
		source.Effective = source.Original
		if page.SourceType == "photo" {
			source.Effective, err = loadPhotoBaseline(ctx, tx, owner, doc, page.ID, source.Original)
			if err != nil {
				return nil, err
			}
		}
		var originalID, effectiveID string
		var sourceHash, effectiveHash []byte
		err = tx.QueryRowContext(ctx, `SELECT suggestion_id,original_artifact_id,correction_artifact_id,source_sha256,correction_sha256,transform_json FROM page_corrections WHERE page_id=? AND document_id=? AND owner_id=? AND state='applied'`, page.ID, doc, owner).Scan(&source.CorrectionID, &originalID, &effectiveID, &sourceHash, &effectiveHash, &source.TransformJSON)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStorage
		}
		if err == nil {
			if (page.SourceType != "" && page.SourceType != "photo") || originalID != source.Original.ID || !sameHash(sourceHash, source.Original.SHA256) {
				return nil, ErrConflict
			}
			var tr correction.Transform
			if json.Unmarshal([]byte(source.TransformJSON), &tr) != nil || correction.Validate(tr, image.Rect(0, 0, source.Effective.PixelWidth, source.Effective.PixelHeight)) != nil {
				return nil, ErrConflict
			}
			var digest []byte
			err = tx.QueryRowContext(ctx, `SELECT id,relative_path,media_type,byte_size,sha256 FROM artifacts WHERE id=? AND document_id=? AND owner_id=? AND kind='intermediate' AND state='ready'`, effectiveID, doc, owner).Scan(&source.Effective.ID, &source.Effective.RelativePath, &source.Effective.MediaType, &source.Effective.Size, &digest)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrConflict
			}
			if err != nil {
				return nil, ErrStorage
			}
			if len(digest) != 32 || len(effectiveHash) != 32 || !localPath(source.Effective.RelativePath) || source.Effective.Size < 1 || (source.Effective.MediaType != "image/png" && source.Effective.MediaType != "image/jpeg") {
				return nil, ErrConflict
			}
			copy(source.Effective.SHA256[:], digest)
			if !sameHash(effectiveHash, source.Effective.SHA256) {
				return nil, ErrConflict
			}
			source.Effective.PixelWidth, source.Effective.PixelHeight = tr.OutputWidth, tr.OutputHeight
		}
		out = append(out, source)
	}
	return out, nil
}

func validPageSource(page CapturedPage, media string) bool {
	if page.SourceType == "photo" {
		return (media == "image/png" || media == "image/jpeg") && page.Size == "" && page.Color == "" && page.DPI == 0 && page.PDFWidthPoints == 0 && page.PDFHeightPoints == 0
	}
	if page.SourceType == "pdf" {
		return media == "application/pdf" && page.Size == "" && page.Color == "" && page.DPI == 0 && page.PDFWidthPoints > 0 && page.PDFWidthPoints <= 20000 && page.PDFHeightPoints > 0 && page.PDFHeightPoints <= 20000
	}
	return page.SourceType == "" && page.PDFWidthPoints == 0 && page.PDFHeightPoints == 0 && (media == "image/png" || media == "image/jpeg")
}
