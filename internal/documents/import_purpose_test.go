package documents

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestImportPurposeIdentityAndLegacy(t *testing.T) {
	for _, media := range []string{"application/pdf", "image/jpeg", "image/png"} {
		r := photoStoreRequest()
		r.MediaType = media
		_, _, old, err := importIdentity(r)
		if err != nil {
			t.Fatal(err)
		}
		r.Purpose = "document"
		_, _, explicit, err := importIdentity(r)
		if err != nil || old != explicit {
			t.Fatalf("explicit document changed legacy identity: %s %v", media, err)
		}
	}
	r := photoStoreRequest()
	r.Purpose = "photo"
	_, _, jpeg, err := importIdentity(r)
	if err != nil {
		t.Fatal(err)
	}
	r.PhotoFormat = "jpeg"
	_, _, explicit, err := importIdentity(r)
	if err != nil || jpeg != explicit {
		t.Fatal("default photo JPEG identity", err)
	}
	r.PhotoFormat = "png"
	_, _, png, err := importIdentity(r)
	if err != nil || png == jpeg {
		t.Fatal("format not bound to identity", err)
	}
	for _, bad := range []ImportRequest{
		{MediaType: "application/pdf", Purpose: "photo"},
		{MediaType: "image/jpeg", Purpose: "invalid"},
		{MediaType: "image/jpeg", Purpose: "document", PhotoFormat: "jpeg"},
		{MediaType: "image/jpeg", Purpose: "photo", PhotoFormat: "gif"},
	} {
		bad.IdempotencyKey = r.IdempotencyKey
		bad.InputBytes = r.InputBytes
		bad.SHA256 = r.SHA256
		if _, _, _, e := importIdentity(bad); !errors.Is(e, ErrInvalid) {
			t.Fatalf("invalid purpose accepted: %+v %v", bad, e)
		}
	}
}

func TestPhotoPurposeImportCropUndoAndOutput(t *testing.T) {
	for _, format := range []string{"jpeg", "png"} {
		t.Run(format, func(t *testing.T) {
			f := scanFixture(t)
			ctx := context.Background()
			r := photoStoreRequest()
			r.Purpose = "photo"
			r.PhotoFormat = format
			a, e := f.store.AdmitImport(ctx, alice, r, importLimits())
			if e != nil {
				t.Fatal(e)
			}
			changed := r
			changed.PhotoFormat = map[string]string{"jpeg": "png", "png": "jpeg"}[format]
			if _, e = f.store.ReadImportReceipt(ctx, alice, changed); !errors.Is(e, ErrIdempotencyConflict) {
				t.Fatal("changed format receipt replay", e)
			}
			changed = r
			changed.Purpose = "document"
			changed.PhotoFormat = ""
			if _, e = f.store.AdmitImport(ctx, alice, changed, importLimits()); !errors.Is(e, ErrIdempotencyConflict) {
				t.Fatal("changed purpose admitted", e)
			}
			sealPhoto(t, f, a, r)
			claim, e := f.store.ClaimNextImport(ctx, runner)
			if e != nil {
				t.Fatal(e)
			}
			work, e := f.store.LoadImportWork(ctx, alice, claim.Job.ID, runner)
			if e != nil || work.Snapshot.Purpose != "photo" || work.Snapshot.PhotoFormat != format {
				t.Fatal("purpose snapshot", work.Snapshot, e)
			}
			if e = f.store.CommitImportPages(ctx, alice, claim.Job.ID, ImportStop{RunnerID: runner, ProcessStoppedAtMS: f.now, WorkspaceCleanedAtMS: f.now}, []ImportedPage{photoImported()}); e != nil {
				t.Fatal(e)
			}
			p, e := f.store.ReadDocument(ctx, alice, a.DocumentID)
			if e != nil || p.Purpose != "photo" || p.PhotoFormat == nil || *p.PhotoFormat != format {
				t.Fatal("photo projection", p, e)
			}
			var raw string
			if e = f.docs.QueryRow("SELECT original_artifact_id FROM pages WHERE id=?", p.Pages[0].ID).Scan(&raw); e != nil {
				t.Fatal(e)
			}
			crop := RenderAdmission{DocumentID: p.ID, Revision: p.Revision, IdempotencyKey: "request_import_crop001", Settings: RenderSettings{Format: "pdf", PDFEncoding: "lossless"}, ManualCrop: &PhotoCrop{PageID: p.Pages[0].ID, SourceSHA256: p.Pages[0].Photo.SHA256, Width: 1, Height: 2}, ReserveBytes: 100}
			for _, settings := range []RenderSettings{{Format: format}, {Format: "pdf", OCR: RenderOCR{Enabled: true}}} {
				invalid := crop
				invalid.Settings = settings
				if _, err := f.store.AdmitRender(ctx, alice, invalid, admissionLimits()); !errors.Is(err, ErrInvalid) {
					t.Fatal("invalid manual settings admitted", settings, err)
				}
			}
			edit, e := f.store.AdmitRender(ctx, alice, crop, admissionLimits())
			if e != nil {
				t.Fatal("crop admission", e)
			}
			if _, e = f.store.ClaimExport(ctx, alice, edit.JobID, runner); e != nil {
				t.Fatal(e)
			}
			if _, e = f.store.LoadRenderWork(ctx, alice, edit.JobID, runner); e != nil {
				t.Fatal("crop work", e)
			}
			if e = f.store.CompletePhotoCrop(ctx, alice, edit.JobID, runner, p.Revision, manualArtifact()); e != nil {
				t.Fatal("crop complete", e)
			}
			p, e = f.store.ReadDocument(ctx, alice, a.DocumentID)
			if e != nil || p.PhotoFormat == nil || *p.PhotoFormat != format {
				t.Fatal("crop changed format/original", p, e)
			}
			var after string
			if e = f.docs.QueryRow("SELECT original_artifact_id FROM pages WHERE id=?", p.Pages[0].ID).Scan(&after); e != nil || after != raw {
				t.Fatal("original changed", e)
			}
			_, e = f.store.UndoCorrection(ctx, alice, CorrectionRequest{DocumentID: p.ID, PageID: p.Pages[0].ID, Revision: p.Revision, IdempotencyKey: "request_import_undo001"}, 72*time.Hour)
			if e != nil {
				t.Fatal("undo", e)
			}
			p, e = f.store.ReadDocument(ctx, alice, a.DocumentID)
			if e != nil {
				t.Fatal(e)
			}
			for _, bad := range []RenderSettings{{Format: "pdf"}, {Format: format, OCR: RenderOCR{Enabled: true}}} {
				_, e = f.store.AdmitRender(ctx, alice, RenderAdmission{DocumentID: p.ID, Revision: p.Revision, IdempotencyKey: "request_import_bad001", Settings: bad, ReserveBytes: 100}, admissionLimits())
				if !errors.Is(e, ErrInvalid) {
					t.Fatal("photo invalid output accepted", bad, e)
				}
			}
			render, e := f.store.AdmitRender(ctx, alice, RenderAdmission{DocumentID: p.ID, Revision: p.Revision, IdempotencyKey: "request_import_output1", Settings: RenderSettings{Format: format}, ReserveBytes: 100}, admissionLimits())
			if e != nil {
				t.Fatal("image output", e)
			}
			var snap RenderSnapshot
			var configuration string
			if e = f.docs.QueryRow("SELECT configuration_json FROM jobs WHERE id=?", render.JobID).Scan(&configuration); e != nil {
				t.Fatal(e)
			}
			if json.Unmarshal([]byte(configuration), &snap) != nil || snap.Settings.Format != format {
				t.Fatal("output format lost")
			}
			if _, e = f.store.ClaimExport(ctx, alice, render.JobID, runner); e != nil {
				t.Fatal(e)
			}
			if _, e = f.store.LoadRenderWork(ctx, alice, render.JobID, runner); e != nil {
				t.Fatal("image work", e)
			}
			media := "image/" + format
			exported := ArtifactRecord{ID: "artifact_import_output1", RelativePath: "work_import_output1/image." + format, MediaType: media, Size: 10, SHA256: sha256.Sum256([]byte("image output")), PixelWidth: 3, PixelHeight: 2}
			if e = f.store.PublishExport(ctx, alice, render.JobID, runner, render.Revision, exported); e != nil {
				t.Fatal("image publication", e)
			}
			p, e = f.store.ReadDocument(ctx, alice, a.DocumentID)
			if e != nil || p.Output == nil || p.Output.Format != format || p.Purpose != "photo" {
				t.Fatal("image output projection", p, e)
			}
		})
	}
}

func TestImportLegacyDigestExact(t *testing.T) {
	for _, media := range []string{"", "application/pdf", "image/jpeg", "image/png"} {
		r := importRequest()
		r.MediaType = media
		_, _, digest, e := importIdentity(r)
		if e != nil {
			t.Fatal(e)
		}
		legacy := struct {
			MediaType  string   `json:"media_type,omitempty"`
			InputBytes int64    `json:"input_bytes"`
			SHA256     [32]byte `json:"sha256"`
		}{MediaType: media, InputBytes: r.InputBytes, SHA256: r.SHA256}
		prefix := "photo-import-new-v1\x00"
		if media == "" || media == "application/pdf" {
			legacy.MediaType = ""
			prefix = "pdf-import-new-v1\x00"
		}
		raw, e := canonicalCommand(legacy)
		if e != nil || digest != sha256.Sum256(append([]byte(prefix), raw...)) {
			t.Fatal("legacy digest changed", media, e)
		}
	}
}

func TestPhotoImportSnapshotPurposeCannotDrift(t *testing.T) {
	for _, mutate := range []string{"purpose", "photo_format"} {
		t.Run(mutate, func(t *testing.T) {
			f := scanFixture(t)
			ctx := context.Background()
			r := photoStoreRequest()
			r.Purpose = "photo"
			r.PhotoFormat = "png"
			a, e := f.store.AdmitImport(ctx, alice, r, importLimits())
			if e != nil {
				t.Fatal(e)
			}
			sealPhoto(t, f, a, r)
			claim, e := f.store.ClaimNextImport(ctx, runner)
			if e != nil {
				t.Fatal(e)
			}
			value := "document"
			if mutate == "photo_format" {
				value = "webp"
			}
			if _, e = f.docs.Exec("UPDATE jobs SET configuration_json=json_set(configuration_json,?,?) WHERE id=?", "$."+mutate, value, claim.Job.ID); e == nil {
				t.Fatal("immutable import selection changed")
			}
			work, e := f.store.LoadImportWork(ctx, alice, claim.Job.ID, runner)
			if e != nil || work.Snapshot.Purpose != "photo" || work.Snapshot.PhotoFormat != "png" {
				t.Fatal("frozen selection changed", e)
			}
		})
	}
}
