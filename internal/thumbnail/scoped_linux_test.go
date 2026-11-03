//go:build linux

package thumbnail

import (
	"context"
	"crypto/sha256"
	"errors"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"scanner-kiosk/internal/artifactscope"
)

func TestScopedDerivativeSealBudgetAndAbort(t *testing.T) {
	for _, abort := range []bool{false, true} {
		name := "publish"
		if abort {
			name = "abort"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			raw := fixture(t, 160, 80, false, "png")
			if err := os.WriteFile(filepath.Join(root, "original.png"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			scope, err := artifactscope.New(root, map[string]artifactscope.Input{"original_thumb_01": {RelativePath: "original.png", Size: int64(len(raw)), SHA256: sha256.Sum256(raw)}}, artifactscope.Limits{MaxInputBytes: 1 << 20, MaxOutputBytes: 1 << 20, MaxTotalOutputBytes: 1 << 20, MaxOutputs: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer scope.Close()
			if _, err = GenerateScoped(context.Background(), scope, "unlisted_thumb_01", options(160, 80, false)); err == nil {
				t.Fatal("unlisted input accepted")
			}
			reference, err := GenerateScoped(context.Background(), scope, "original_thumb_01", options(160, 80, false))
			if err != nil {
				t.Fatal(err)
			}
			sealed, err := scope.Seal(context.Background())
			if err != nil || len(sealed) != 1 || sealed[0].ID != reference || sealed[0].Kind != artifactscope.JPEG {
				t.Fatal(sealed, err)
			}
			path := filepath.Join(root, sealed[0].RelativePath)
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			bitmap, decodeErr := jpeg.Decode(file)
			file.Close()
			if decodeErr != nil || bitmap.Bounds().Dx() != 64 || bitmap.Bounds().Dy() != 32 {
				t.Fatal(decodeErr)
			}
			if !abort {
				if err = scope.Release(); err != nil {
					t.Fatal(err)
				}
			}
			if err = scope.Close(); err != nil {
				t.Fatal(err)
			}
			_, err = os.Stat(path)
			if abort && !errors.Is(err, os.ErrNotExist) || !abort && err != nil {
				t.Fatal("output lifecycle", err)
			}
			bytes, err := os.ReadFile(filepath.Join(root, "original.png"))
			if err != nil || sha256.Sum256(bytes) != sha256.Sum256(raw) {
				t.Fatal("original lost", err)
			}
		})
	}
}
func TestScopedOutputBudgetRejectsDerivative(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	raw := fixture(t, 16, 8, false, "png")
	if err := os.WriteFile(filepath.Join(root, "original.png"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	scope, err := artifactscope.New(root, map[string]artifactscope.Input{"original_thumb_01": {RelativePath: "original.png", Size: int64(len(raw)), SHA256: sha256.Sum256(raw)}}, artifactscope.Limits{MaxInputBytes: 1 << 20, MaxOutputBytes: 1, MaxTotalOutputBytes: 1, MaxOutputs: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	if reference, err := GenerateScoped(context.Background(), scope, "original_thumb_01", options(16, 8, false)); !errors.Is(err, artifactscope.ErrLimit) || reference != "" {
		t.Fatal(reference, err)
	}
}
