//go:build linux

package pdfassemble

import (
	"context"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/pkg/ocrplugin"
	"testing"
)

func TestScopedPDFFailurePreservesOriginal(t *testing.T) {
	for _, mode := range []string{"foreign-input", "output-limit"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			page := source(t, "original_page_0001", "png")
			input, _ := page.Open(context.Background())
			defer input.Close()
			data, err := io.ReadAll(input)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(root, "original"), data, 0600); err != nil {
				t.Fatal(err)
			}
			scope, err := artifactscope.New(root, map[string]artifactscope.Input{page.ID: {RelativePath: "original", Size: int64(len(data)), SHA256: sha256.Sum256(data)}}, artifactscope.Limits{MaxInputBytes: 1 << 20, MaxOutputBytes: 1, MaxTotalOutputBytes: 1, MaxOutputs: 1})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = scope.CreateOutput(context.Background(), ocrplugin.OutputKind(artifactscope.PDF)); err == nil {
				t.Fatal("OCR port accepted non-OCR output")
			}
			id := page.ID
			if mode == "foreign-input" {
				id = "foreign_page_0001"
			}
			ref, err := GenerateScoped(context.Background(), scope, []ScopedPage{{ID: id, Size: "a4", Color: "color", Width: 80, Height: 100}}, "lossless", opts())
			if err == nil || ref != "" {
				t.Fatal("invalid generation returned output")
			}
			if err = scope.Close(); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 1 || entries[0].Name() != "original" {
				t.Fatal("failed output not cleaned")
			}
		})
	}
}
