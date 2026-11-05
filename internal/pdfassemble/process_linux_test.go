//go:build linux

package pdfassemble

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/artifactscope"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__pdf-assemble" {
		if os.Getenv("SCANNER_ASSEMBLY_SECRET_SENTINEL") != "" {
			os.Exit(99)
		}
		os.Exit(AssemblyChild(os.Args[2:], os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

func TestIsolatedMixedAssemblyAndFailureCleanup(t *testing.T) {
	for _, mode := range []string{"mixed", "corrupt-input", "low-budget", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			scope, err := artifactscope.New(root, nil, artifactscope.Limits{MaxInputBytes: 1 << 20, MaxOutputBytes: 1 << 20, MaxTotalOutputBytes: 1 << 20, MaxOutputs: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer scope.Close()
			raw := importedTextPDF()
			if err = os.WriteFile(filepath.Join(root, "source.pdf"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			input := artifactread.Input{RelativePath: "source.pdf", Size: int64(len(raw)), SHA256: sha256.Sum256(raw)}
			image := source(t, "scanned", "png")
			reader, err := image.Open(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			imageBytes := new(bytes.Buffer)
			if _, err = imageBytes.ReadFrom(reader); err != nil {
				t.Fatal(err)
			}
			reader.Close()
			if err = os.WriteFile(filepath.Join(root, "source.png"), imageBytes.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			request := ProcessRequest{Encoding: "lossless", Options: opts(), Pages: []ProcessPage{{ID: "imported", Input: input, MediaType: "application/pdf", Rotation: 90}, {ID: "scanned", Input: artifactread.Input{RelativePath: "source.png", Size: int64(imageBytes.Len()), SHA256: sha256.Sum256(imageBytes.Bytes())}, MediaType: "image/png", Size: image.Size, Color: image.Color, Width: image.Width, Height: image.Height}}}
			if mode == "corrupt-input" {
				request.Pages[0].Input.SHA256[0] ^= 1
			}
			available := int64(1 << 20)
			if mode == "low-budget" {
				available = 1
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			t.Setenv("SCANNER_ASSEMBLY_SECRET_SENTINEL", "must-not-inherit")
			ref, err := GenerateIsolated(ctx, root, scope, request, available)
			if mode != "mixed" {
				if err == nil {
					t.Fatal("invalid execution accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			sealed, err := scope.Seal(ctx)
			if err != nil || len(sealed) != 1 || sealed[0].ID != ref {
				t.Fatal("not sealed", sealed, err)
			}
			data, err := os.ReadFile(filepath.Join(root, sealed[0].RelativePath))
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := api.ReadValidateAndOptimize(ctx, bytes.NewReader(data), model.NewStatelessConfiguration(), nil)
			if err != nil || parsed.PageCount != 2 {
				t.Fatal("invalid merged output", err)
			}
			dict, _, _, err := parsed.PageDict(ctx, 1, false)
			if err != nil {
				t.Fatal(err)
			}
			content, err := parsed.PageContent(dict, 1)
			if err != nil || !bytes.Contains(content, []byte("PRESERVED_TEXT")) || !bytes.Contains(content, []byte(" re f")) {
				t.Fatal("lost PDF text/vector", err)
			}
			if original, err := os.ReadFile(filepath.Join(root, "source.pdf")); err != nil || !bytes.Equal(original, raw) {
				t.Fatal("source changed", err)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 3 {
				t.Fatal("scratch retained", len(entries), err)
			}
		})
	}
}
