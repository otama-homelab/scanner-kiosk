//go:build linux

package artifactgc

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

const job = "cleanup_job_000001"
const artifact = "artifact_remove_001"

func fixture(t *testing.T) (*Root, string, Input) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "work_fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("immutable private original")
	input := Input{ArtifactID: artifact, RelativePath: "work_fixture/original.png", Size: int64(len(raw)), SHA256: sha256.Sum256(raw)}
	if err := os.WriteFile(filepath.Join(root, input.RelativePath), raw, 0600); err != nil {
		t.Fatal(err)
	}
	gc, err := OpenRoot(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gc.Close() })
	return gc, root, input
}
func staged(root string) string { return filepath.Join(root, ".cleanup", job, artifact) }
func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}
func TestRemoveAndAbsentRetryPreserveUnrelatedContent(t *testing.T) {
	gc, root, input := fixture(t)
	other := filepath.Join(root, "other.png")
	if err := os.WriteFile(other, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := gc.Remove(context.Background(), job, input)
	if err != nil || result.RemovedAtMS <= 0 {
		t.Fatal(result, err)
	}
	if exists(t, filepath.Join(root, input.RelativePath)) || exists(t, staged(root)) || !exists(t, other) {
		t.Fatal("wrong file lifecycle")
	}
	if _, err = gc.Remove(context.Background(), job, input); err != nil {
		t.Fatal("absent retry", err)
	}
}
func TestResumeQuarantineAfterCancellation(t *testing.T) {
	gc, root, input := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	gc.afterVerify = cancel
	if result, err := gc.Remove(ctx, job, input); !errors.Is(err, context.Canceled) || result.RemovedAtMS != 0 {
		t.Fatal(result, err)
	}
	if exists(t, filepath.Join(root, input.RelativePath)) || !exists(t, staged(root)) {
		t.Fatal("cancel did not retain quarantine")
	}
	gc.afterVerify = nil
	if _, err := gc.Remove(context.Background(), job, input); err != nil {
		t.Fatal("resume", err)
	}
	if exists(t, staged(root)) {
		t.Fatal("resumed file retained")
	}
}
func TestRejectsMismatchedBytesWithoutUnlink(t *testing.T) {
	gc, root, input := fixture(t)
	input.SHA256 = sha256.Sum256([]byte("different"))
	if result, err := gc.Remove(context.Background(), job, input); !errors.Is(err, ErrMismatch) || result.RemovedAtMS != 0 {
		t.Fatal(result, err)
	}
	if !exists(t, staged(root)) {
		t.Fatal("mismatched data deleted")
	}
}
func TestSourceReplacementDuringMoveRetained(t *testing.T) {
	gc, root, input := fixture(t)
	source := filepath.Join(root, input.RelativePath)
	gc.beforeMove = func() {
		if err := os.Rename(source, source+".saved"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, make([]byte, input.Size), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if result, err := gc.Remove(context.Background(), job, input); !errors.Is(err, ErrMismatch) || result.RemovedAtMS != 0 {
		t.Fatal(result, err)
	}
	if !exists(t, source+".saved") || !exists(t, staged(root)) {
		t.Fatal("replacement or original deleted")
	}
}
func TestQuarantineReplacementAfterHashRetained(t *testing.T) {
	gc, root, input := fixture(t)
	gc.afterVerify = func() {
		if err := os.Rename(staged(root), staged(root)+".saved"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(staged(root), []byte("replacement"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if result, err := gc.Remove(context.Background(), job, input); !errors.Is(err, ErrMismatch) || result.RemovedAtMS != 0 {
		t.Fatal(result, err)
	}
	if !exists(t, staged(root)) || !exists(t, staged(root)+".saved") {
		t.Fatal("unverified entry deleted")
	}
}
func TestNoOverwriteOfOccupiedQuarantine(t *testing.T) {
	gc, root, input := fixture(t)
	if err := os.MkdirAll(filepath.Dir(staged(root)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged(root), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := gc.Remove(context.Background(), job, input); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(staged(root))
	if err != nil || string(bytes) != "keep" || !exists(t, filepath.Join(root, input.RelativePath)) {
		t.Fatal("occupied quarantine overwritten", err)
	}
}
func TestRejectsUnsafePathsAndSpecialFiles(t *testing.T) {
	for _, mode := range []string{"traversal", "absolute", "control-db", "control-lock", "hidden-parent", "symlink-leaf", "symlink-parent", "hardlink", "fifo", "public-file", "public-parent"} {
		t.Run(mode, func(t *testing.T) {
			gc, root, input := fixture(t)
			source := filepath.Join(root, input.RelativePath)
			switch mode {
			case "traversal":
				input.RelativePath = "../original.png"
			case "absolute":
				input.RelativePath = source
			case "control-db":
				input.RelativePath = "documents.sqlite3-wal"
			case "control-lock":
				input.RelativePath = ".worker.lock"
			case "hidden-parent":
				input.RelativePath = "work_fixture/.native/file"
			case "symlink-leaf":
				if err := os.Rename(source, source+".saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(source+".saved", source); err != nil {
					t.Fatal(err)
				}
			case "symlink-parent":
				if err := os.Rename(filepath.Dir(source), filepath.Dir(source)+".saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Dir(source)+".saved", filepath.Dir(source)); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(source, source+".linked"); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(source, 0600); err != nil {
					t.Fatal(err)
				}
			case "public-file":
				os.Chmod(source, 0644)
			case "public-parent":
				os.Chmod(filepath.Dir(source), 0755)
			}
			if result, err := gc.Remove(context.Background(), job, input); err == nil || result.RemovedAtMS != 0 {
				t.Fatal("unsafe deletion accepted", mode, result, err)
			}
		})
	}
}
func TestSyncFailureRetainsOrAllowsSameIdentityRetry(t *testing.T) {
	for _, afterUnlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-unlink", true: "after-unlink"}[afterUnlink], func(t *testing.T) {
			gc, root, input := fixture(t)
			count := 0
			gc.syncDir = func(fd int) error {
				count++
				if !afterUnlink && count == 4 || afterUnlink && count == 5 {
					return unix.EIO
				}
				return unix.Fsync(fd)
			}
			if result, err := gc.Remove(context.Background(), job, input); !errors.Is(err, ErrStorage) || result.RemovedAtMS != 0 {
				t.Fatal(result, err)
			}
			if !afterUnlink && !exists(t, staged(root)) {
				t.Fatal("sync failure lost quarantine")
			}
			gc.syncDir = unix.Fsync
			if _, err := gc.Remove(context.Background(), job, input); err != nil {
				t.Fatal("retry", err)
			}
		})
	}
}
func TestPinnedRootDoesNotDeleteReplacementRoot(t *testing.T) {
	gc, root, input := fixture(t)
	old := root + "-old"
	if err := os.Rename(root, old); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(old)
	if err := os.MkdirAll(filepath.Join(root, "work_fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(root, input.RelativePath)
	if err := os.WriteFile(replacement, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := gc.Remove(context.Background(), job, input); err != nil {
		t.Fatal(err)
	}
	if !exists(t, replacement) || exists(t, filepath.Join(old, input.RelativePath)) {
		t.Fatal("replacement root touched")
	}
}

func TestEmptyArtifactAndUntrustedQuarantine(t *testing.T) {
	for _, mode := range []string{"empty", "symlink", "public"} {
		t.Run(mode, func(t *testing.T) {
			gc, root, input := fixture(t)
			source := filepath.Join(root, input.RelativePath)
			if mode == "empty" {
				if err := os.WriteFile(source, nil, 0600); err != nil {
					t.Fatal(err)
				}
				input.Size = 0
				input.SHA256 = sha256.Sum256(nil)
				if _, err := gc.Remove(context.Background(), job, input); err != nil {
					t.Fatal(err)
				}
				return
			}
			if mode == "symlink" {
				outside := t.TempDir()
				if err := os.Symlink(outside, filepath.Join(root, ".cleanup")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(filepath.Join(root, ".cleanup"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if result, err := gc.Remove(context.Background(), job, input); err == nil || result.RemovedAtMS != 0 || !exists(t, source) {
				t.Fatal("untrusted quarantine accepted", result, err)
			}
		})
	}
}
