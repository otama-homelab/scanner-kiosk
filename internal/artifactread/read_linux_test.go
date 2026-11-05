//go:build linux

package artifactread

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func privateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestVerifiedPinnedReadAndBoundedGrowth(t *testing.T) {
	dir := privateDir(t)
	data := []byte("private immutable bytes")
	path := filepath.Join(dir, "original.png")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(dir, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := root.Open(context.Background(), Input{"original.png", int64(len(data)), sha256.Sum256(data)})
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := os.WriteFile(path, append(data, []byte("unexpected growth")...), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(file)
	if err != nil || string(got) != string(data) {
		t.Fatal("read exceeded authorized size", string(got), err)
	}
}
func TestArtifactReadRejectsInvalidInputs(t *testing.T) {
	for _, name := range []string{"traversal", "absolute", "unclean", "symlink", "parent-symlink", "public", "size", "hash", "over-budget", "fifo", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			dir := privateDir(t)
			data := []byte("private bytes")
			path := filepath.Join(dir, "input.png")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			root, err := OpenRoot(dir, 1024)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			input := Input{"input.png", int64(len(data)), sha256.Sum256(data)}
			ctx := context.Background()
			switch name {
			case "traversal":
				input.RelativePath = "../input.png"
			case "absolute":
				input.RelativePath = path
			case "unclean":
				input.RelativePath = "a/../input.png"
			case "symlink":
				if err := os.Symlink("input.png", filepath.Join(dir, "alias")); err != nil {
					t.Fatal(err)
				}
				input.RelativePath = "alias"
			case "parent-symlink":
				outside := t.TempDir()
				if err := os.WriteFile(filepath.Join(outside, "input.png"), data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(dir, "outside")); err != nil {
					t.Fatal(err)
				}
				input.RelativePath = "outside/input.png"
			case "public":
				os.Chmod(path, 0644)
			case "size":
				input.Size++
			case "hash":
				input.SHA256 = [32]byte{}
			case "over-budget":
				input.Size = 1025
			case "fifo":
				if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
					t.Fatal(err)
				}
				input.RelativePath = "pipe"
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			file, err := root.Open(ctx, input)
			if file != nil {
				file.Close()
				t.Fatal("invalid artifact opened")
			}
			if err == nil {
				t.Fatal("invalid artifact accepted")
			}
		})
	}
}
func TestArtifactRootAndReadCancellation(t *testing.T) {
	dir := privateDir(t)
	os.Chmod(dir, 0755)
	if root, err := OpenRoot(dir, 1024); err == nil {
		root.Close()
		t.Fatal("public root accepted")
	}
	os.Chmod(dir, 0700)
	alias := filepath.Join(t.TempDir(), "root")
	os.Symlink(dir, alias)
	if root, err := OpenRoot(alias, 1024); err == nil {
		root.Close()
		t.Fatal("symlink root accepted")
	}
	os.WriteFile(filepath.Join(dir, "input"), []byte("abc"), 0600)
	root, err := OpenRoot(dir, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	file, err := root.Open(ctx, Input{"input", 3, sha256.Sum256([]byte("abc"))})
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cancel()
	if _, err = file.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled read continued", err)
	}
}

func TestPinnedRootSurvivesPathReplacement(t *testing.T) {
	parent := privateDir(t)
	path := filepath.Join(parent, "documents")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte("owned artifact")
	if err := os.WriteFile(filepath.Join(path, "input"), data, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(path, filepath.Join(parent, "retained")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "input"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := root.Open(context.Background(), Input{"input", int64(len(data)), sha256.Sum256(data)})
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	got, err := io.ReadAll(file)
	if err != nil || string(got) != string(data) {
		t.Fatal("root path replacement changed file authority", string(got), err)
	}
}

func TestVerifiedRangeBoundsAndWholeFileHash(t *testing.T) {
	dir := privateDir(t)
	data := []byte("abcdefghij")
	path := filepath.Join(dir, "input")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(dir, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	input := Input{"input", int64(len(data)), sha256.Sum256(data)}
	file, err := root.OpenRange(context.Background(), input, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(file)
	file.Close()
	if err != nil || string(got) != "cde" {
		t.Fatal(string(got), err)
	}
	for _, interval := range [][2]int64{{-1, 3}, {2, -1}, {9, 2}, {11, 0}} {
		if f, err := root.OpenRange(context.Background(), input, interval[0], interval[1]); err == nil {
			f.Close()
			t.Fatal("invalid interval accepted", interval)
		}
	}
	// Corruption outside the selected interval must still prevent disclosure.
	altered := append([]byte{}, data...)
	altered[9] = 'X'
	if err := os.WriteFile(path, altered, 0600); err != nil {
		t.Fatal(err)
	}
	if f, err := root.OpenRange(context.Background(), input, 0, 3); err == nil {
		f.Close()
		t.Fatal("only selected bytes were verified")
	}
}

func TestVerifiedFileDescriptorForProcessingChild(t *testing.T) {
	rootPath := privateDir(t)
	raw := []byte("immutable input")
	if err := os.WriteFile(filepath.Join(rootPath, "input"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(rootPath, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := root.OpenFile(context.Background(), Input{"input", int64(len(raw)), sha256.Sum256(raw)})
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil || string(data) != string(raw) {
		t.Fatal("verified descriptor", err)
	}
}
