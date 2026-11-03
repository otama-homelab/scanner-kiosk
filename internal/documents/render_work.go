package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
)

// RenderWork is worker-internal metadata, never an HTTP response. Inputs follow
// the immutable snapshot order; paths are resolved only by the rooted file scope.
type RenderWork struct {
	Job                  Job
	Snapshot             RenderSnapshot
	Inputs               []ArtifactRecord
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
		rows, err := tx.QueryContext(ctx, `SELECT p.id,p.original_artifact_id,p.rotation,p.capture_size,p.capture_color,p.capture_dpi,a.relative_path,a.media_type,a.byte_size,a.sha256,EXISTS(SELECT 1 FROM page_thumbnails t JOIN artifacts thumb ON thumb.id=t.artifact_id AND thumb.document_id=t.document_id AND thumb.owner_id=t.owner_id WHERE t.original_artifact_id=a.id AND thumb.kind='thumbnail' AND thumb.state='ready')
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
			if err = rows.Scan(&page.ID, &page.OriginalArtifactID, &page.Rotation, &page.Size, &page.Color, &page.DPI, &input.RelativePath, &input.MediaType, &input.Size, &digest, &hasThumbnail); err != nil {
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
			if !id(input.ID) || seen[input.ID] || !localPath(input.RelativePath) || input.Size < 1 || len(digest) != 32 || (input.MediaType != "image/png" && input.MediaType != "image/jpeg") {
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
