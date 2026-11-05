//go:build linux && !race

package pdfassemble

import (
	"context"
	"crypto/sha256"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"image"
	"image/png"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/artifactscope"
	"testing"
)

// Resource limits are tested without race instrumentation, as in release builds.
func TestIsolatedLargeColorScan(t *testing.T) {
	// A high-entropy 600dpi page exercises simultaneous bitmap/input/PDF allocations.
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(root, "scan.png")
	func() {
		bitmap := image.NewNRGBA(image.Rect(0, 0, 4960, 7016))
		_, _ = rand.New(rand.NewSource(43000)).Read(bitmap.Pix)
		for i := 3; i < len(bitmap.Pix); i += 4 {
			bitmap.Pix[i] = 255
		}
		f, err := os.OpenFile(input, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(f, bitmap); err != nil {
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	f, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.New()
	if _, err = io.Copy(digest, f); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	info, err := os.Stat(input)
	if err != nil {
		t.Fatal(err)
	}
	var sum [32]byte
	copy(sum[:], digest.Sum(nil))
	const budget = 200 << 20
	scope, err := artifactscope.New(root, nil, artifactscope.Limits{MaxInputBytes: budget, MaxOutputBytes: budget, MaxTotalOutputBytes: budget, MaxOutputs: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	request := ProcessRequest{Encoding: "lossless", Options: Options{MaxInputBytes: budget, MaxOutputBytes: budget, MaxPagePixels: 40000000, MaxPages: 1, JPEGQuality: 92}, Pages: []ProcessPage{{ID: "artifact_large_color_scan", Size: "a4", Color: "color", MediaType: "image/png", Width: 4960, Height: 7016, Input: artifactread.Input{RelativePath: "scan.png", Size: info.Size(), SHA256: sum}}}}
	reference, err := GenerateIsolated(context.Background(), root, scope, request, budget)
	if err != nil {
		t.Fatal("600dpi color page rejected", err)
	}
	sealed, err := scope.Seal(context.Background())
	if err != nil || len(sealed) != 1 || sealed[0].ID != reference {
		t.Fatal("unsealed output", sealed, err)
	}
	output, err := os.Open(filepath.Join(root, sealed[0].RelativePath))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	count, err := api.PageCount(context.Background(), output, model.NewStatelessConfiguration())
	if err != nil || count != 1 {
		t.Fatal("invalid output", count, err)
	}
}
