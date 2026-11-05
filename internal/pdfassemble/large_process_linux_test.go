//go:build linux && !race

package pdfassemble

import (
	"context"
	"crypto/sha256"
	"fmt"
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
	t.Run("one-a4", func(t *testing.T) {
		testIsolatedLargeColorScan(t, "a4", 4960, 7016, 1)
	})
	t.Run("two-a5", func(t *testing.T) {
		testIsolatedLargeColorScan(t, "a5", 3496, 4960, 2)
	})
}

func testIsolatedLargeColorScan(t *testing.T, size string, width, height, pageCount int) {
	t.Helper()
	// A high-entropy 600dpi page exercises simultaneous bitmap/input/PDF allocations.
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	pages := make([]ProcessPage, 0, pageCount)
	for index := 0; index < pageCount; index++ {
		input := filepath.Join(root, fmt.Sprintf("scan-%d.png", index))
		func() {
			bitmap := image.NewNRGBA(image.Rect(0, 0, width, height))
			_, _ = rand.New(rand.NewSource(int64(43000 + index))).Read(bitmap.Pix)
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
		pages = append(pages, ProcessPage{ID: fmt.Sprintf("artifact_large_color_scan_%d", index), Size: size, Color: "color", MediaType: "image/png", Width: width, Height: height, Input: artifactread.Input{RelativePath: filepath.Base(input), Size: info.Size(), SHA256: sum}})
	}
	const budget = 200 << 20
	scope, err := artifactscope.New(root, nil, artifactscope.Limits{MaxInputBytes: budget, MaxOutputBytes: budget, MaxTotalOutputBytes: budget, MaxOutputs: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	request := ProcessRequest{Encoding: "lossless", Options: Options{MaxInputBytes: budget, MaxOutputBytes: budget, MaxPagePixels: 40000000, MaxPages: pageCount, JPEGQuality: 92}, Pages: pages}
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
	if err != nil || count != pageCount {
		t.Fatal("invalid output", count, err)
	}
}
