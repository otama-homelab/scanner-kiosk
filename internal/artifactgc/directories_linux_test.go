//go:build linux

package artifactgc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRemovePrunesOnlyEmptyPinnedAncestors(t *testing.T) {
	gc, root, input := fixture(t)
	if err := os.MkdirAll(filepath.Join(root, "work_fixture", "nested", "deep"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, input.RelativePath), filepath.Join(root, "work_fixture/nested/deep/original.png")); err != nil {
		t.Fatal(err)
	}
	input.RelativePath = "work_fixture/nested/deep/original.png"
	if _, err := gc.Remove(context.Background(), job, input); err != nil {
		t.Fatal(err)
	}
	if exists(t, filepath.Join(root, "work_fixture")) || exists(t, filepath.Join(root, ".cleanup")) {
		t.Fatal("empty ancestors retained")
	}
	if _, err := gc.Remove(context.Background(), job, input); err != nil {
		t.Fatal("missing-ancestor retry", err)
	}
	if !exists(t, root) {
		t.Fatal("root removed")
	}
}

func TestNonemptyAncestorPreserved(t *testing.T) {
	gc, root, input := fixture(t)
	sibling := filepath.Join(root, "work_fixture/keep.png")
	if err := os.WriteFile(sibling, []byte("unrelated private document"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := gc.Remove(context.Background(), job, input); err != nil {
		t.Fatal(err)
	}
	if !exists(t, sibling) || !exists(t, filepath.Join(root, "work_fixture")) || exists(t, filepath.Join(root, ".cleanup")) {
		t.Fatal("wrong directory retirement")
	}
}

func TestMissingAncestorDoesNotTreatNearestAncestorAsSource(t *testing.T) {
	gc, root, input := fixture(t)
	if err := os.Remove(filepath.Join(root, input.RelativePath)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "work_fixture")); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "original.png")
	if err := os.WriteFile(other, []byte("must never hash or delete this"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := gc.Remove(context.Background(), job, input); err != nil {
		t.Fatal(err)
	}
	if !exists(t, other) {
		t.Fatal("source path shortened across absent ancestor")
	}
}

func TestDirectorySyncFailureAfterRmdirCanResume(t *testing.T) {
	for _, phase := range []string{"source", "quarantine", "cleanup"} {
		t.Run(phase, func(t *testing.T) {
			gc, root, input := fixture(t)
			failed := false
			gc.syncDir = func(fd int) error {
				sourceGone := !exists(t, filepath.Join(root, "work_fixture"))
				hit := sourceGone
				if phase == "quarantine" {
					hit = sourceGone && !exists(t, filepath.Join(root, ".cleanup", job))
				}
				if phase == "cleanup" {
					hit = sourceGone && !exists(t, filepath.Join(root, ".cleanup"))
				}
				if !failed && hit {
					failed = true
					return errors.New("injected directory sync failure")
				}
				return unix.Fsync(fd)
			}
			if proof, err := gc.Remove(context.Background(), job, input); !errors.Is(err, ErrStorage) || proof.RemovedAtMS != 0 || !failed {
				t.Fatal(proof, err, failed)
			}
			gc.syncDir = unix.Fsync
			if _, err := gc.Remove(context.Background(), job, input); err != nil {
				t.Fatal("resume after durable/unconfirmed directory removal", err)
			}
			if exists(t, filepath.Join(root, "work_fixture")) || exists(t, filepath.Join(root, ".cleanup")) {
				t.Fatal("resume retained directories")
			}
		})
	}
}

func TestPruningRejectsReplacedSourceDirectory(t *testing.T) {
	gc, root, input := fixture(t)
	original := filepath.Join(root, "work_fixture")
	saved := filepath.Join(root, "saved_private_directory")
	gc.afterVerify = func() {
		if err := os.Rename(original, saved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(original, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if proof, err := gc.Remove(context.Background(), job, input); !errors.Is(err, ErrMismatch) || proof.RemovedAtMS != 0 {
		t.Fatal(proof, err)
	}
	if !exists(t, original) || !exists(t, saved) {
		t.Fatal("replacement directory was retired")
	}
}
