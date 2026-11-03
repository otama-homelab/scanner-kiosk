//go:build linux

package documents

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scanner-kiosk/internal/artifactgc"
)

func TestCleanupRealFilesProgressAndQuotaCompletion(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	must(t, f.docs, "DELETE FROM jobs")
	must(t, f.docs, "DELETE FROM documents")
	f.now = time.Now().UnixMilli()
	must(t, f.docs, `INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','processing',?,?)`, docA, alice, f.now, f.now+3600000)
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	files := []struct {
		id, path, media string
		data            []byte
	}{{"original_cleanup_001", "work_original/source.png", "image/png", []byte("synthetic immutable source bytes")}, {"output_cleanup_0001", "work_output/document.pdf", "application/pdf", []byte("synthetic immutable output bytes")}, {"thumbnail_cleanup_01", "work_thumbnail/image.jpg", "image/jpeg", []byte("synthetic immutable derivative bytes")}}
	artifacts := make([]ArtifactRecord, len(files))
	for i, file := range files {
		path := filepath.Join(root, file.path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, file.data, 0600); err != nil {
			t.Fatal(err)
		}
		artifacts[i] = ArtifactRecord{ID: file.id, RelativePath: file.path, MediaType: file.media, Size: int64(len(file.data)), SHA256: sha256.Sum256(file.data), PixelWidth: 16, PixelHeight: 8}
	}
	original := artifacts[0]
	must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready',?,?,?,?,1,?)`, original.ID, docA, alice, original.RelativePath, original.MediaType, original.Size, original.SHA256[:], f.now)
	must(t, f.docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi) VALUES('page_cleanup_00001',?,?,0,?,'a4','color',300)`, docA, alice, original.ID)
	must(t, f.docs, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES(?,?,?,'render','queued',1,'{}',?,?)`, jobA, docA, alice, f.now, f.now+60000)
	must(t, f.docs, "INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,1048576)", jobA, alice)
	claim(t, f)
	if err := f.store.PublishExportWithThumbnails(ctx, alice, jobA, runner, 1, artifacts[1], []ThumbnailRecord{{OriginalID: original.ID, Artifact: artifacts[2]}}); err != nil {
		t.Fatal(err)
	}
	job, err := f.store.RequestDelete(ctx, alice, deleteRequest(), 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimCleanup(ctx, alice, job.ID, runner); err != nil {
		t.Fatal(err)
	}
	work, err := f.store.LoadCleanupWork(ctx, alice, job.ID, runner, 100)
	if err != nil || len(work.Files) != 3 {
		t.Fatal(work, err)
	}
	remover, err := artifactgc.OpenRoot(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer remover.Close()
	for _, file := range work.Files {
		result, err := remover.Remove(ctx, job.ID, artifactgc.Input{ArtifactID: file.ArtifactID, RelativePath: file.RelativePath, Size: file.Size, SHA256: file.SHA256})
		if err != nil {
			t.Fatal(err)
		}
		if result.JobID != job.ID || result.ArtifactID != file.ArtifactID {
			t.Fatal("removal proof misbound")
		}
		f.now = max(f.now, result.RemovedAtMS)
		if err = f.store.RecordCleanupRemoval(ctx, alice, job.ID, runner, CleanupRemoval{ArtifactID: result.ArtifactID, Size: result.Size, SHA256: result.SHA256, RemovedAtMS: result.RemovedAtMS}); err != nil {
			t.Fatal(err)
		}
		if scalar(t, f.docs, "SELECT count(*) FROM artifacts") != 3 {
			t.Fatal("quota released before completion")
		}
	}
	if err = f.store.FinishCleanup(ctx, alice, job.ID, runner); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM artifacts") != 0 || scalar(t, f.docs, "SELECT count(*) FROM cleanup_files") != 0 {
		t.Fatal("metadata quota not completed")
	}
	for _, file := range files {
		if _, err = os.Stat(filepath.Join(root, file.path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("file survived", file.path, err)
		}
	}
	replay, err := f.store.RequestDelete(ctx, alice, deleteRequest(), 72*time.Hour)
	if err != nil || replay.ID != job.ID || replay.State != "succeeded" {
		t.Fatal("completed receipt lost", replay, err)
	}
}
