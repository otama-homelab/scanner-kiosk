//go:build linux

package pdfassemble

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestIsolatedImportedOCR(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		root := t.TempDir()
		os.Chmod(root, 0700)
		scope, err := artifactscope.New(root, nil, artifactscope.Limits{MaxInputBytes: 1 << 20, MaxOutputBytes: 1 << 20, MaxTotalOutputBytes: 1 << 20, MaxOutputs: 1})
		if err != nil {
			t.Fatal(err)
		}
		defer scope.Close()
		original := importedOCRFixture(200, 300, "10 10 30 40 re f", "")
		l := pdfLayer("page", 200, 300, "ISOLATED OCR")
		r, err := l.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		layer, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		capability := func(name string, raw []byte) artifactread.Input {
			if err := os.WriteFile(filepath.Join(root, name), raw, 0600); err != nil {
				t.Fatal(err)
			}
			return artifactread.Input{RelativePath: name, Size: int64(len(raw)), SHA256: sha256.Sum256(raw)}
		}
		input, overlay := capability("source.pdf", original), capability("layer.pdf", layer)
		if corrupt {
			overlay.SHA256[0] ^= 1
		}
		request := ProcessRequest{OCR: true, Encoding: "lossless", Options: opts(), Pages: []ProcessPage{{ID: "page", MediaType: "application/pdf", Input: input, Layer: &overlay}}}
		_, err = GenerateIsolated(context.Background(), root, scope, request, 1<<20)
		if corrupt {
			if err == nil {
				t.Fatal("foreign layer capability accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := scope.Seal(context.Background())
		if err != nil || len(sealed) != 1 {
			t.Fatal(sealed, err)
		}
		output, err := os.ReadFile(filepath.Join(root, sealed[0].RelativePath))
		if err != nil {
			t.Fatal(err)
		}
		if text := extractedOCRText(t, output); !strings.Contains(text, "ISOLATED OCR") {
			t.Fatal(text)
		}
	}
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

// A single output must use its admitted bytes once, including above the old
// staging-plus-copy half ceiling. The final file keeps its shared-budget charge.
func TestIsolatedOutputUsesSingleReservation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	raw := importedTextPDF()
	if err := os.WriteFile(filepath.Join(root, "input.pdf"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	expected, err := Build(ctx, []Page{{ID: "source", MediaType: "application/pdf", Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }}}, "lossless", opts())
	if err != nil {
		t.Fatal(err)
	}
	request := ProcessRequest{Encoding: "lossless", Options: opts(), Pages: []ProcessPage{{ID: "source", MediaType: "application/pdf", Input: artifactread.Input{RelativePath: "input.pdf", Size: int64(len(raw)), SHA256: sha256.Sum256(raw)}}}}
	manifest, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	available := int64(len(manifest) + len(expected) + 128)
	budget, err := artifactscope.NewBudget(available)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := artifactscope.NewWithBudget(root, nil, artifactscope.Limits{MaxInputBytes: 1 << 20, MaxOutputBytes: available, MaxTotalOutputBytes: available, MaxOutputs: 1}, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	ref, err := GenerateIsolated(ctx, root, scope, request, available)
	if err != nil {
		t.Fatal("output fitting one admitted reservation rejected", err)
	}
	sealed, err := scope.Seal(ctx)
	if err != nil || len(sealed) != 1 || sealed[0].ID != ref {
		t.Fatal("not sealed", sealed, err)
	}
	output := sealed[0]
	if output.Size <= (available-int64(len(manifest)))/2 || output.Size > available || budget.Used() != output.Size {
		t.Fatal("duplicate or invalid output charge", output.Size, budget.Used(), available)
	}
	data, err := os.ReadFile(filepath.Join(root, output.RelativePath))
	if err != nil || sha256.Sum256(data) != output.SHA256 {
		t.Fatal("invalid output hash", err)
	}
	if err = scope.Close(); err != nil || budget.Used() != 0 {
		t.Fatal("output cleanup did not release actual charge", err, budget.Used())
	}
}
