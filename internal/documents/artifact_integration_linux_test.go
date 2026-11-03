//go:build linux

package documents

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/pkg/ocrplugin"
)

// This verifies metadata/file lifecycle, not PDF content validity. In production
// the coordinator validates the final PDF/image before invoking PublishExport.
func TestScopedOutputPublicationAndCASCleanup(t *testing.T) {
	for _, mode := range []string{"success", "stale", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			f := renderFixture(t)
			admitted, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.store.ClaimExport(context.Background(), alice, admitted.JobID, runner); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			original := []byte("immutable capture")
			if err := os.WriteFile(filepath.Join(root, "original"), original, 0600); err != nil {
				t.Fatal(err)
			}
			scope, err := artifactscope.New(root, nil, artifactscope.Limits{MaxInputBytes: 100, MaxOutputBytes: 50, MaxTotalOutputBytes: 50, MaxOutputs: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer scope.Close()
			writer, err := scope.CreateOutput(context.Background(), ocrplugin.SearchablePDF)
			if err != nil {
				t.Fatal(err)
			}
			data := []byte("opaque validated-export fixture")
			if _, err = writer.Write(data); err != nil {
				t.Fatal(err)
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			artifacts, err := scope.Seal(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			a := artifacts[0]
			artifact := Export{ID: a.ID, RelativePath: a.RelativePath, MediaType: "application/pdf", Size: a.Size, SHA256: a.SHA256}
			if mode == "stale" {
				must(t, f.docs, "UPDATE documents SET revision=revision+1 WHERE id=?", docA)
			}
			if mode == "cancelled" {
				if err = f.store.RequestCancel(context.Background(), alice, admitted.JobID); err != nil {
					t.Fatal(err)
				}
			}
			err = f.store.PublishExport(context.Background(), alice, admitted.JobID, runner, admitted.Revision, artifact)
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if err = scope.Release(); err != nil {
					t.Fatal(err)
				}
				scope.Close()
				authorized, e := f.store.GetExport(context.Background(), alice, docA)
				if e != nil || authorized.ID != a.ID || authorized.SHA256 != sha256.Sum256(data) {
					t.Fatal("published artifact differs")
				}
				persisted, e := os.ReadFile(filepath.Join(root, authorized.RelativePath))
				if e != nil || string(persisted) != string(data) {
					t.Fatal("published output removed")
				}
			} else {
				if !errors.Is(err, ErrConflict) {
					t.Fatalf("CAS did not reject output: %v", err)
				}
				if err = scope.Close(); err != nil {
					t.Fatal(err)
				}
				if _, err = os.Stat(filepath.Join(root, a.RelativePath)); !os.IsNotExist(err) {
					t.Fatal("rejected output retained")
				}
				if scalar(t, f.docs, "SELECT count(*) FROM published_outputs") != 0 {
					t.Fatal("rejected output published")
				}
			}
			persisted, e := os.ReadFile(filepath.Join(root, "original"))
			if e != nil || string(persisted) != string(original) {
				t.Fatal("original damaged")
			}
		})
	}
}
