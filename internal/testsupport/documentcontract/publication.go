package documentcontract

import (
	"context"
	"errors"
	"testing"

	"scanner-kiosk/internal/documents"
)

// RenderPublicationRecords is the worker publication/read-back surface.
type RenderPublicationRecords interface {
	PublishExport(context.Context, string, string, string, int64, documents.Export) error
	PublishExportWithThumbnails(context.Context, string, string, string, int64, documents.Export, []documents.ThumbnailRecord) error
	ExportRecorded(context.Context, string, string, string) (bool, error)
}

type RenderPublicationFixture struct {
	DispatchFixture
	Publications         RenderPublicationRecords
	OpenPeerPublications func() RenderPublicationRecords
	Revision             int64
	Artifact             documents.Export
	Thumbnails           []documents.ThumbnailRecord
}

// RunUncertainRenderPublication checks atomic metadata and reconciliation, not
// actual PDF files, worker cleanup or storage durability.
func RunUncertainRenderPublication(t *testing.T, newFixture func(*testing.T) RenderPublicationFixture) {
	t.Helper()
	ctx := context.Background()
	for _, committed := range []bool{false, true} {
		name := "rollback"
		if committed {
			name = "lost acknowledgement"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			peer := f.OpenPeerPublications()
			reader := f.OpenPeerRecords()
			restore := f.FailCommit(committed)
			var err error
			if len(f.Thumbnails) == 0 {
				err = f.Publications.PublishExport(ctx, f.Owner, f.Job, f.Boot, f.Revision, f.Artifact)
			} else {
				err = f.Publications.PublishExportWithThumbnails(ctx, f.Owner, f.Job, f.Boot, f.Revision, f.Artifact, f.Thumbnails)
			}
			restore()
			if !errors.Is(err, documents.ErrCommitUncertain) {
				t.Fatal("uncertain publication reported success", err)
			}
			recorded, err := peer.ExportRecorded(ctx, f.Owner, f.Job, f.Artifact.ID)
			if err != nil || recorded != committed {
				t.Fatal("incorrect durable publication read-back", recorded, err)
			}
			foreign, err := peer.ExportRecorded(ctx, f.OtherOwner, f.Job, f.Artifact.ID)
			if err != nil || foreign {
				t.Fatal("foreign publication disclosed", foreign, err)
			}
			job, err := reader.ReadJob(ctx, f.Owner, f.Job)
			expected := "running"
			if committed {
				expected = "succeeded"
			}
			if err != nil || job.State != expected {
				t.Fatal("job/publication diverged", job, err)
			}
			output, err := reader.ReadCurrentOutput(ctx, f.Owner, f.Document)
			if !committed {
				if !errors.Is(err, documents.ErrUnavailable) {
					t.Fatal("rolled-back output visible", output, err)
				}
			} else if err != nil || output.Artifact.ID != f.Artifact.ID || output.Artifact.SHA256 != f.Artifact.SHA256 || output.Artifact.Size != f.Artifact.Size {
				t.Fatal("committed output pointer lost or changed", output, err)
			}
			if len(f.Thumbnails) > 0 {
				preview, err := reader.ReadPageThumbnail(ctx, f.Owner, f.Document, f.Pages[0])
				thumbnail := f.Thumbnails[0].Artifact
				if committed {
					if err != nil || preview.Artifact.ID != thumbnail.ID || preview.Artifact.SHA256 != thumbnail.SHA256 || preview.Artifact.Size != thumbnail.Size {
						t.Fatal("committed render thumbnail missing or changed", preview, err)
					}
				} else if !errors.Is(err, documents.ErrUnavailable) {
					t.Fatal("rollback exposed render thumbnail", preview, err)
				}
			}
			again, err := f.Publications.ExportRecorded(ctx, f.Owner, f.Job, f.Artifact.ID)
			if err != nil || again != recorded {
				t.Fatal("read-back disagrees across connections", again, err)
			}
		})
	}
}

// ScanPublicationRecords is the worker original-publication/read-back surface.
type ScanPublicationRecords interface {
	CommitScanPage(context.Context, string, string, int64, documents.ArtifactRecord, documents.ScanStop) (documents.ScanPage, error)
	CommitScanPageWithThumbnails(context.Context, string, string, int64, documents.ArtifactRecord, documents.ScanStop, []documents.ThumbnailRecord) (documents.ScanPage, error)
	CaptureRecorded(context.Context, string, string, string) (bool, error)
	CaptureBundleRecorded(context.Context, string, string, documents.ArtifactRecord, []documents.ThumbnailRecord) (bool, error)
}

type ScanPublicationFixture struct {
	DispatchFixture
	Publications         ScanPublicationRecords
	OpenPeerPublications func() ScanPublicationRecords
	Revision             int64
	Artifact             documents.ArtifactRecord
	Thumbnails           []documents.ThumbnailRecord
	Stop                 documents.ScanStop
}

// RunUncertainScanPublication starts with a running first-page scan. Stop is
// synthetic trusted evidence; no native process, scanner or real file is used.
func RunUncertainScanPublication(t *testing.T, newFixture func(*testing.T) ScanPublicationFixture) {
	t.Helper()
	ctx := context.Background()
	for _, committed := range []bool{false, true} {
		name := "rollback"
		if committed {
			name = "lost acknowledgement"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			peer := f.OpenPeerPublications()
			reader := f.OpenPeerRecords()
			before, err := reader.ReadDocument(ctx, f.Owner, f.Document)
			if err != nil || before.PageCount != 0 || before.State != "scanning" {
				t.Fatal("invalid publication fixture", before, err)
			}
			restore := f.FailCommit(committed)
			var page documents.ScanPage
			if len(f.Thumbnails) == 0 {
				page, err = f.Publications.CommitScanPage(ctx, f.Owner, f.Job, f.Revision, f.Artifact, f.Stop)
			} else {
				page, err = f.Publications.CommitScanPageWithThumbnails(ctx, f.Owner, f.Job, f.Revision, f.Artifact, f.Stop, f.Thumbnails)
			}
			restore()
			if !errors.Is(err, documents.ErrCommitUncertain) || page != (documents.ScanPage{}) {
				t.Fatal("uncertain scan publication exposed success", page, err)
			}
			readBack := func(records ScanPublicationRecords, owner string) (bool, error) {
				if len(f.Thumbnails) > 0 {
					return records.CaptureBundleRecorded(ctx, owner, f.Job, f.Artifact, f.Thumbnails)
				}
				return records.CaptureRecorded(ctx, owner, f.Job, f.Artifact.ID)
			}
			recorded, err := readBack(peer, f.Owner)
			if err != nil || recorded != committed {
				t.Fatal("incorrect durable scan read-back", recorded, err)
			}
			foreign, err := readBack(peer, f.OtherOwner)
			if err != nil || foreign {
				t.Fatal("foreign scan publication disclosed", foreign, err)
			}
			job, err := reader.ReadJob(ctx, f.Owner, f.Job)
			expected := "running"
			if committed {
				expected = "succeeded"
			}
			if err != nil || job.State != expected {
				t.Fatal("job/scan publication diverged", job, err)
			}
			current, err := reader.ReadDocument(ctx, f.Owner, f.Document)
			count, state := 0, "scanning"
			if committed {
				count, state = 1, "review"
			}
			if err != nil || current.PageCount != count || len(current.Pages) != count || current.State != state || current.Revision != before.Revision || !current.CreatedAt.Equal(before.CreatedAt) || !current.ExpiresAt.Equal(before.ExpiresAt) {
				t.Fatal("page/retention publication diverged", current, err)
			}
			if committed {
				original, err := reader.ReadPageOriginal(ctx, f.Owner, f.Document, current.Pages[0].ID)
				if err != nil || original.Artifact.ID != f.Artifact.ID || original.Artifact.SHA256 != f.Artifact.SHA256 || original.Artifact.Size != f.Artifact.Size {
					t.Fatal("committed original lost or changed", original, err)
				}
			}
			if committed && len(f.Thumbnails) > 0 {
				mismatched := append([]documents.ThumbnailRecord(nil), f.Thumbnails...)
				mismatched[0].Artifact.Size++
				found, readErr := peer.CaptureBundleRecorded(ctx, f.Owner, f.Job, f.Artifact, mismatched)
				if found || !errors.Is(readErr, documents.ErrConflict) {
					t.Fatal("mismatched bundle became safe negative read-back", found, readErr)
				}

				preview, err := reader.ReadPageThumbnail(ctx, f.Owner, f.Document, current.Pages[0].ID)
				thumbnail := f.Thumbnails[0].Artifact
				if err != nil || preview.Artifact.ID != thumbnail.ID || preview.Artifact.SHA256 != thumbnail.SHA256 || preview.Artifact.Size != thumbnail.Size {
					t.Fatal("committed capture thumbnail missing or changed", preview, err)
				}
			}
			again, err := readBack(f.Publications, f.Owner)
			if err != nil || again != recorded {
				t.Fatal("scan read-back disagrees across connections", again, err)
			}
		})
	}
}
