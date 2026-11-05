//go:build linux

package artifactgc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
	"scanner-kiosk/internal/artifactscope"
)

func sweepFixture(t *testing.T) (*Root, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	gc, err := OpenRoot(root, 1024)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gc.Close() })
	return gc, root
}
func sweepDirectory(t *testing.T, root, name string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
		t.Fatal(err)
	}
}
func sweepAll(t *testing.T, sweep *EmptySweep, limit int) SweepProgress {
	t.Helper()
	var total SweepProgress
	for calls := 0; calls < 1000; calls++ {
		progress, err := sweep.Step(context.Background(), limit, func(context.Context) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		if progress.Examined > limit {
			t.Fatal("unbounded batch")
		}
		total.Examined += progress.Examined
		total.Removed += progress.Removed
		if progress.Done {
			total.Done = true
			return total
		}
	}
	t.Fatal("sweep did not terminate")
	return total
}
func TestEmptySweepStreamsLegacyProducerAndQuarantineDirectories(t *testing.T) {
	gc, root := sweepFixture(t)
	for i := 0; i < 205; i++ {
		sweepDirectory(t, root, fmt.Sprintf("work_%048x", i))
	}
	for i := 0; i < 7; i++ {
		sweepDirectory(t, root, fmt.Sprintf("capture_%048x", i))
	}
	sweepDirectory(t, root, ".cleanup")
	for i := 0; i < 103; i++ {
		sweepDirectory(t, filepath.Join(root, ".cleanup"), fmt.Sprintf("job_cleanup_%04d", i))
	}
	// These namespace/control paths are outside the sweep's allowlist.
	for _, name := range []string{".native", "airscan-config", "work_fixture", "work_ABC", "capture_abc", fmt.Sprintf("probe_%048x", 0)} {
		sweepDirectory(t, root, name)
	}
	keep := fmt.Sprintf("work_%048x", 999)
	sweepDirectory(t, root, keep)
	if err := os.WriteFile(filepath.Join(root, keep, "original.png"), []byte("retained original"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"documents.sqlite3", "documents.sqlite3-wal", ".worker.lock"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("control"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sweep, err := gc.StartEmptySweep(context.Background(), unreferencedTestDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer sweep.Close()
	result := sweepAll(t, sweep, 64)
	if result.Removed != 205+7+103+1 || result.Examined < 315 {
		t.Fatal("incomplete bounded sweep", result)
	}
	for _, name := range []string{".native", "airscan-config", "work_fixture", "work_ABC", "capture_abc", fmt.Sprintf("probe_%048x", 0), "documents.sqlite3", "documents.sqlite3-wal", ".worker.lock", keep} {
		if !exists(t, filepath.Join(root, name)) {
			t.Fatal("unrelated path removed", name)
		}
	}
	if exists(t, filepath.Join(root, ".cleanup")) {
		t.Fatal("empty quarantine remained")
	}
	bytes, err := os.ReadFile(filepath.Join(root, keep, "original.png"))
	if err != nil || string(bytes) != "retained original" {
		t.Fatal("retained file changed", err)
	}
	again, err := gc.StartEmptySweep(context.Background(), unreferencedTestDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if result := sweepAll(t, again, 1); result.Removed != 0 {
		t.Fatal("repeat sweep changed retained data", result)
	}
}
func TestEmptySweepPreservesLinksPublicDirectoriesAndNestedContent(t *testing.T) {
	gc, root := sweepFixture(t)
	outside := t.TempDir()
	link := fmt.Sprintf("work_%048x", 1)
	if err := os.Symlink(outside, filepath.Join(root, link)); err != nil {
		t.Fatal(err)
	}
	public := fmt.Sprintf("capture_%048x", 2)
	sweepDirectory(t, root, public)
	if err := os.Chmod(filepath.Join(root, public), 0755); err != nil {
		t.Fatal(err)
	}
	nested := fmt.Sprintf("work_%048x", 3)
	sweepDirectory(t, root, nested)
	sweepDirectory(t, filepath.Join(root, nested), "nested")
	ordinary := fmt.Sprintf("work_%048x", 4)
	if err := os.WriteFile(filepath.Join(root, ordinary), []byte("ordinary private file"), 0600); err != nil {
		t.Fatal(err)
	}
	sweepDirectory(t, root, ".cleanup")
	if err := os.Symlink(outside, filepath.Join(root, ".cleanup", "job_symlink_0001")); err != nil {
		t.Fatal(err)
	}
	sweepDirectory(t, filepath.Join(root, ".cleanup"), "unknown")
	if err := os.WriteFile(filepath.Join(root, ".cleanup", "job_file_0000001"), []byte("untracked quarantine file"), 0600); err != nil {
		t.Fatal(err)
	}
	sweep, err := gc.StartEmptySweep(context.Background(), unreferencedTestDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer sweep.Close()
	if result := sweepAll(t, sweep, 2); result.Removed != 0 {
		t.Fatal("unsafe directory removed", result)
	}
	for _, name := range []string{link, public, nested, ordinary, ".cleanup/job_symlink_0001", ".cleanup/unknown", ".cleanup/job_file_0000001"} {
		if !exists(t, filepath.Join(root, name)) {
			t.Fatal("protected entry removed", name)
		}
	}
	if !exists(t, outside) {
		t.Fatal("link target removed")
	}
}
func TestEmptySweepRejectsPinnedDirectoryReplacement(t *testing.T) {
	for _, cleanup := range []bool{false, true} {
		t.Run(fmt.Sprint(cleanup), func(t *testing.T) {
			gc, root := sweepFixture(t)
			name := fmt.Sprintf("work_%048x", 1)
			if cleanup {
				name = ".cleanup"
				sweepDirectory(t, root, name)
				sweepDirectory(t, filepath.Join(root, name), "job_cleanup_0001")
			} else {
				sweepDirectory(t, root, name)
			}
			sweep, err := gc.StartEmptySweep(context.Background(), unreferencedTestDirectory)
			if err != nil {
				t.Fatal(err)
			}
			defer sweep.Close()
			changed := false
			sweep.beforeRemove = func() {
				if changed {
					return
				}
				changed = true
				if err := os.Rename(filepath.Join(root, name), filepath.Join(root, "retained")); err != nil {
					t.Fatal(err)
				}
				sweepDirectory(t, root, name)
			}
			_, err = sweep.Step(context.Background(), 64, func(context.Context) error { return nil })
			if !errors.Is(err, ErrMismatch) || !changed {
				t.Fatal("replacement accepted", err)
			}
			if !exists(t, filepath.Join(root, name)) || !exists(t, filepath.Join(root, "retained")) {
				t.Fatal("replaced directory removed")
			}
			if cleanup && !exists(t, filepath.Join(root, "retained/job_cleanup_0001")) {
				t.Fatal("old cleanup parent mutated after replacement")
			}
		})
	}
}
func TestEmptySweepCancellationAuthorityAndSyncFailureResume(t *testing.T) {
	for _, mode := range []string{"cancel", "authority", "sync-root", "sync-cleanup"} {
		t.Run(mode, func(t *testing.T) {
			gc, root := sweepFixture(t)
			name := fmt.Sprintf("work_%048x", 1)
			if mode == "sync-cleanup" {
				sweepDirectory(t, root, ".cleanup")
				name = ".cleanup/job_cleanup_0001"
				sweepDirectory(t, filepath.Join(root, ".cleanup"), "job_cleanup_0001")
			} else {
				sweepDirectory(t, root, name)
			}
			sweep, err := gc.StartEmptySweep(context.Background(), unreferencedTestDirectory)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			check := func(context.Context) error { return nil }
			if mode == "cancel" {
				cancel()
			}
			if mode == "authority" {
				check = func(context.Context) error { return ErrConflict }
			}
			if mode == "sync-root" || mode == "sync-cleanup" {
				gc.syncDir = func(fd int) error {
					if !exists(t, filepath.Join(root, name)) {
						return unix.EIO
					}
					return unix.Fsync(fd)
				}
			}
			_, err = sweep.Step(ctx, 64, check)
			if err == nil {
				t.Fatal("failed sweep acknowledged")
			}
			if (mode == "cancel" || mode == "authority") && !exists(t, filepath.Join(root, name)) {
				t.Fatal("lost authority removed directory")
			}
			if err = sweep.Close(); err != nil {
				t.Fatal(err)
			}
			gc.syncDir = unix.Fsync
			resumed, err := gc.StartEmptySweep(context.Background(), unreferencedTestDirectory)
			if err != nil {
				t.Fatal(err)
			}
			defer resumed.Close()
			sweepAll(t, resumed, 1)
			if exists(t, filepath.Join(root, name)) {
				t.Fatal("empty directory not retired on fresh retry")
			}
		})
	}
}

func TestEmptySweepRetiresActualScopeAfterLegacyAcknowledgedUnlink(t *testing.T) {
	gc, root := sweepFixture(t)
	scope, err := artifactscope.New(root, nil, artifactscope.Limits{MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxTotalOutputBytes: 1024, MaxOutputs: 1})
	if err != nil {
		t.Fatal(err)
	}
	output, err := scope.CreateArtifact(context.Background(), artifactscope.JPEG)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = output.Write([]byte("sealed legacy artifact")); err != nil {
		t.Fatal(err)
	}
	if err = output.Close(); err != nil {
		t.Fatal(err)
	}
	sealed, err := scope.Seal(context.Background())
	if err != nil || len(sealed) != 1 {
		t.Fatal("scope seal", err)
	}
	if err = scope.Release(); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(sealed[0].RelativePath)
	// A pre-pruning GC version had acknowledged this leaf but left its directory.
	if err = os.Remove(filepath.Join(root, sealed[0].RelativePath)); err != nil {
		t.Fatal(err)
	}
	if !exists(t, filepath.Join(root, parent)) {
		t.Fatal("fixture did not retain old directory")
	}
	sweep, err := gc.StartEmptySweep(context.Background(), unreferencedTestDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer sweep.Close()
	if progress := sweepAll(t, sweep, 1); progress.Removed != 1 {
		t.Fatal("real producer directory not recognized", progress)
	}
	if exists(t, filepath.Join(root, parent)) {
		t.Fatal("legacy directory retained")
	}
}

func TestEmptySweepSameCursorSettlesLostDirectorySync(t *testing.T) {
	gc, root := sweepFixture(t)
	name := fmt.Sprintf("work_%048x", 1)
	sweepDirectory(t, root, name)
	sweep, err := gc.StartEmptySweep(context.Background(), unreferencedTestDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer sweep.Close()
	failed := false
	settled := 0
	gc.syncDir = func(fd int) error {
		if !exists(t, filepath.Join(root, name)) {
			if !failed {
				failed = true
				return unix.EIO
			}
			settled++
		}
		return unix.Fsync(fd)
	}
	if _, err = sweep.Step(context.Background(), 64, func(context.Context) error { return nil }); !errors.Is(err, ErrStorage) || !failed {
		t.Fatal("sync failure not surfaced", err)
	}
	result := sweepAll(t, sweep, 64)
	if !result.Done || settled == 0 || exists(t, filepath.Join(root, name)) {
		t.Fatal("absent entry completed without settling lost sync", result, settled)
	}
}

func unreferencedTestDirectory(context.Context, string) (bool, error) { return true, nil }
