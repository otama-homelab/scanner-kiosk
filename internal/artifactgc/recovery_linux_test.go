//go:build linux

package artifactgc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoverUnpublishedKeepsReferencedAndUnrecognizedDirectories(t *testing.T) {
	gc, root, _ := fixture(t)
	keep := "capture_" + strings.Repeat("a", 48)
	orphan := "capture_" + strings.Repeat("b", 48)
	probe := "probe_" + strings.Repeat("c", 48)
	for _, name := range []string{keep, orphan, probe, "unknown-directory"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, "image"), []byte("private source"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	err := gc.RecoverUnpublished(context.Background(), func(_ context.Context, name string) (bool, error) { return name != keep, nil }, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{keep, "unknown-directory"} {
		if !exists(t, filepath.Join(root, name, "image")) {
			t.Fatal("preserved content removed", name)
		}
	}
	for _, name := range []string{orphan, probe} {
		if exists(t, filepath.Join(root, name)) {
			t.Fatal("orphan retained", name)
		}
	}
	if err = gc.RecoverUnpublished(context.Background(), func(context.Context, string) (bool, error) { return false, nil }, func(context.Context) error { return nil }); err != nil {
		t.Fatal("retry", err)
	}
}
func TestRecoverUnpublishedRefusesUnknownFilesystemOrAuthority(t *testing.T) {
	for _, mode := range []string{"symlink", "nested", "public-file", "reference-error", "guard-error", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			gc, root, _ := fixture(t)
			name := "work_" + strings.Repeat("d", 48)
			dir := filepath.Join(root, name)
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "symlink":
				if err := os.Symlink(filepath.Join(root, "original.png"), filepath.Join(dir, "link")); err != nil {
					t.Fatal(err)
				}
			case "nested":
				if err := os.Mkdir(filepath.Join(dir, "nested"), 0700); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(filepath.Join(dir, "image"), []byte("private"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "public-file" {
				if err := os.Chmod(filepath.Join(dir, "image"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			check := func(context.Context) error {
				if mode == "guard-error" {
					return errors.New("guard lost")
				}
				return nil
			}
			allow := func(context.Context, string) (bool, error) {
				if mode == "reference-error" {
					return false, errors.New("database unavailable")
				}
				return true, nil
			}
			if err := gc.RecoverUnpublished(ctx, allow, check); err == nil {
				t.Fatal("uncertainty reported clean")
			}
			if !exists(t, dir) {
				t.Fatal("unknown workspace removed")
			}
		})
	}
}
