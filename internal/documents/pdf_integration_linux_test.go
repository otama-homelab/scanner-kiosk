//go:build linux

package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/pdfassemble"
)

// Synthetic component integration resolves the original allowlist from a claimed
// DB job. Geometry is fixture-supplied; a production controller remains pending.
func TestImagePDFScopedPublication(t *testing.T) {
	f := renderFixture(t)
	ctx := context.Background()
	request, limits := admissionRequest(), admissionLimits()
	request.ReserveBytes = 1 << 20
	limits.JobMaxBytes, limits.UserMaxBytes, limits.GlobalMaxBytes = 1<<20, 2<<20, 4<<20
	var err error
	root := t.TempDir()
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	var input bytes.Buffer
	bitmap := image.NewRGBA(image.Rect(0, 0, 80, 100))
	draw.Draw(bitmap, bitmap.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	if err = png.Encode(&input, bitmap); err != nil {
		t.Fatal(err)
	}
	original := input.Bytes()
	if err = os.WriteFile(filepath.Join(root, "original.png"), original, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(original)

	must(t, f.docs, "DELETE FROM pages WHERE document_id=?", docA)
	must(t, f.docs, "DELETE FROM artifacts WHERE id=?", "original_alice_0001")
	must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready','original.png','image/png',?,?,1,1000)`, "original_alice_0001", docA, alice, len(original), digest[:])
	must(t, f.docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi) VALUES(?,?,?,0,?,'a4','color',300)`, "page_alice_000001", docA, alice, "original_alice_0001")
	admitted, err := f.store.AdmitRender(ctx, alice, request, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimExport(ctx, alice, admitted.JobID, runner); err != nil {
		t.Fatal(err)
	}

	work, err := f.store.LoadRenderWork(ctx, alice, admitted.JobID, runner)
	if err != nil {
		t.Fatal(err)
	}
	resolved := work.Inputs[0]
	page := work.Snapshot.Pages[0]
	id := resolved.ID
	scope, err := artifactscope.New(root, map[string]artifactscope.Input{id: {RelativePath: resolved.RelativePath, Size: resolved.Size, SHA256: resolved.SHA256}}, artifactscope.Limits{MaxInputBytes: 1 << 20, MaxOutputBytes: 1 << 20, MaxTotalOutputBytes: 1 << 20, MaxOutputs: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	reference, err := pdfassemble.GenerateScoped(ctx, scope, []pdfassemble.ScopedPage{{ID: id, Size: page.Size, Color: page.Color, Rotation: page.Rotation, Width: 80, Height: 100}}, "lossless", pdfassemble.Options{MaxInputBytes: 1 << 20, MaxOutputBytes: 1 << 20, MaxPagePixels: 10000, MaxPages: 10, JPEGQuality: 92})
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := scope.Seal(ctx)
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("seal: %v", err)
	}
	a := artifacts[0]
	if a.ID != reference || a.Kind != artifactscope.PDF {
		t.Fatal("ordinary PDF mislabeled")
	}
	export := Export{ID: a.ID, RelativePath: a.RelativePath, MediaType: "application/pdf", Size: a.Size, SHA256: a.SHA256}
	if err = f.store.PublishExport(ctx, alice, admitted.JobID, runner, admitted.Revision, export); err != nil {
		t.Fatal(err)
	}
	if err = scope.Release(); err != nil {
		t.Fatal(err)
	}
	if err = scope.Close(); err != nil {
		t.Fatal(err)
	}
	published, err := f.store.GetExport(ctx, alice, docA)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, published.RelativePath))
	if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) || sha256.Sum256(data) != published.SHA256 {
		t.Fatal("generated PDF not retained")
	}
	unchanged, err := os.ReadFile(filepath.Join(root, "original.png"))
	if err != nil || !bytes.Equal(unchanged, original) {
		t.Fatal("original changed")
	}
}
