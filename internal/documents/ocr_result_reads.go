package documents

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
)

// OCRResultSummary is bounded public metadata; content is fetched separately.
type OCRResultSummary struct {
	ID            string `json:"id"`
	State         string `json:"state"`
	InputRevision int64  `json:"input_revision"`
	URL           string `json:"url"`
}
type OCRResultProjection struct {
	OCRResultSummary
	Settings  RenderSettings            `json:"settings"`
	Pages     []OCRResultPageProjection `json:"pages"`
	OutputURL string                    `json:"output_url"`
}
type OCRResultPageProjection struct {
	PageID                string            `json:"page_id"`
	SourceType            string            `json:"source_type"`
	OriginalSHA256        string            `json:"original_sha256"`
	EffectiveSHA256       string            `json:"effective_sha256"`
	CorrectionID          string            `json:"correction_id,omitempty"`
	TransformJSON         string            `json:"transform_json,omitempty"`
	Rotation              int               `json:"rotation"`
	PixelWidth            int               `json:"pixel_width"`
	PixelHeight           int               `json:"pixel_height"`
	PDFWidthPoints        float64           `json:"pdf_width_points,omitempty"`
	PDFHeightPoints       float64           `json:"pdf_height_points,omitempty"`
	PDFTextOrigin         string            `json:"pdf_text_origin"`
	PDFRaster             *PDFRasterMapping `json:"pdf_raster,omitempty"`
	TranscriptUnavailable bool              `json:"transcript_unavailable"`
	Artifacts             map[string]string `json:"artifacts"`
}

func (s *Store) readOCRBundle(ctx context.Context, tx *sql.Tx, owner, documentID, resultID string) (OCRResultBundle, OCRResultSummary, int64, error) {
	var b OCRResultBundle
	var summary OCRResultSummary
	var raw string
	var digest []byte
	var expires int64
	var output string
	err := tx.QueryRowContext(ctx, `SELECT r.id,r.state,r.input_revision,r.bundle_json,r.bundle_sha256,r.output_artifact_id,d.expires_at_ms FROM ocr_results r JOIN documents d ON d.id=r.document_id AND d.owner_id=r.owner_id WHERE r.id=? AND d.id=? AND d.owner_id=? AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting')`, resultID, documentID, owner, s.now().UnixMilli()).Scan(&summary.ID, &summary.State, &summary.InputRevision, &raw, &digest, &output, &expires)
	if err != nil {
		return b, summary, 0, readError(err)
	}
	if len(raw) > 2<<20 || !sameHash(digest, sha256Sum([]byte(raw))) || json.Unmarshal([]byte(raw), &b) != nil || b.ID != resultID || b.InputRevision != summary.InputRevision || b.Output.ID != output {
		return b, summary, 0, ErrStorage
	}
	if _, _, _, err := normalizeOCRBundle(b); err != nil {
		return b, summary, 0, ErrStorage
	}
	summary.URL = "/api/documents/" + documentID + "/ocr-results/" + resultID
	return b, summary, expires, nil
}

func (s *Store) ReadOCRResult(ctx context.Context, owner, documentID, resultID string) (OCRResultProjection, error) {
	var out OCRResultProjection
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		b, summary, _, err := s.readOCRBundle(ctx, tx, owner, documentID, resultID)
		if err != nil {
			return err
		}
		out.OCRResultSummary = summary
		out.Settings = b.Settings
		out.OutputURL = summary.URL + "/output"
		for _, p := range b.Pages {
			page := OCRResultPageProjection{PageID: p.PageID, SourceType: p.SourceType, OriginalSHA256: hex.EncodeToString(p.OriginalSHA256[:]), EffectiveSHA256: hex.EncodeToString(p.EffectiveSHA256[:]), CorrectionID: p.CorrectionID, TransformJSON: p.TransformJSON, Rotation: p.Rotation, PixelWidth: p.PixelWidth, PixelHeight: p.PixelHeight, PDFWidthPoints: p.PDFWidthPoints, PDFHeightPoints: p.PDFHeightPoints, PDFTextOrigin: p.PDFTextOrigin, PDFRaster: p.PDFRaster, TranscriptUnavailable: p.TranscriptUnavailable, Artifacts: map[string]string{}}
			for kind := range ocrPageArtifacts(p) {
				if kind == "text" && p.TranscriptUnavailable {
					continue
				}
				page.Artifacts[kind] = summary.URL + "/pages/" + p.PageID + "/" + kind
			}
			out.Pages = append(out.Pages, page)
		}
		return nil
	})
	if err != nil {
		return OCRResultProjection{}, err
	}
	return out, nil
}

func ocrPageArtifacts(p OCRResultPage) map[string]ArtifactRecord {
	out := map[string]ArtifactRecord{"text": p.Text, "original": {ID: p.OriginalArtifactID, SHA256: p.OriginalSHA256}, "effective": {ID: p.EffectiveArtifactID, SHA256: p.EffectiveSHA256}}
	for kind, a := range map[string]*ArtifactRecord{"positions": p.Positions, "preview": p.Preview, "thumbnail": p.Thumbnail, "layer": p.Layer} {
		if a != nil {
			out[kind] = *a
		}
	}
	return out
}

// ReadOCRResultArtifact returns an internal capability, never public JSON.
func (s *Store) ReadOCRResultArtifact(ctx context.Context, owner, documentID, resultID, pageID, kind string) (ArtifactReference, error) {
	var out ArtifactReference
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		b, _, expires, err := s.readOCRBundle(ctx, tx, owner, documentID, resultID)
		if err != nil {
			return err
		}
		var expected ArtifactRecord
		if kind == "output" && pageID == "" {
			expected = b.Output
		} else {
			for _, p := range b.Pages {
				if p.PageID == pageID {
					if kind == "text" && p.TranscriptUnavailable {
						return ErrUnavailable
					}
					expected = ocrPageArtifacts(p)[kind]
					break
				}
			}
		}
		if expected.ID == "" {
			return ErrUnavailable
		}
		var digest []byte
		err = tx.QueryRowContext(ctx, `SELECT id,relative_path,media_type,byte_size,sha256 FROM artifacts WHERE id=? AND document_id=? AND owner_id=? AND state='ready'`, expected.ID, documentID, owner).Scan(&out.Artifact.ID, &out.Artifact.RelativePath, &out.Artifact.MediaType, &out.Artifact.Size, &digest)
		if err != nil {
			return readError(err)
		}
		if !sameHash(digest, expected.SHA256) || expected.RelativePath != "" && (out.Artifact.RelativePath != expected.RelativePath || out.Artifact.MediaType != expected.MediaType || out.Artifact.Size != expected.Size) {
			return ErrStorage
		}
		copy(out.Artifact.SHA256[:], digest)
		out.ExpiresAtMS = expires
		if s.now().UnixMilli() >= expires {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return ArtifactReference{}, err
	}
	return out, nil
}
