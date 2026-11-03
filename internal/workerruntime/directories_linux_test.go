//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStartupRetiresLegacyEmptyDirectoriesBeforeNewWork(t *testing.T) {
	c, _, _, root := fixture(t)
	for i := 0; i < 137; i++ {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("work_%048x", i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	capture := fmt.Sprintf("capture_%048x", 1)
	if err := os.Mkdir(filepath.Join(root, capture), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".cleanup"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".cleanup", "job_legacy_empty_01"), 0700); err != nil {
		t.Fatal(err)
	}
	keep := fmt.Sprintf("work_%048x", 900)
	if err := os.Mkdir(filepath.Join(root, keep), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, keep, "retained.pdf"), []byte("retained private bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 137; i++ {
		if _, err := os.Stat(filepath.Join(root, fmt.Sprintf("work_%048x", i))); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("old empty scope retained", i, err)
		}
	}
	for _, name := range []string{capture, ".cleanup"} {
		if _, err := os.Stat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("old empty capture/quarantine retained", name, err)
		}
	}
	content, err := os.ReadFile(filepath.Join(root, keep, "retained.pdf"))
	if err != nil || string(content) != "retained private bytes" {
		t.Fatal("retained file modified", err)
	}
	if err = Run(context.Background(), c, true); err != nil {
		t.Fatal("repeat startup", err)
	}
}
func TestStartupUnknownPriorWorkPreventsDirectorySweep(t *testing.T) {
	c, store, _, root := fixture(t)
	job := admit(t, store)
	if _, err := store.ClaimExport(context.Background(), owner, job, "worker_previous_001"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, fmt.Sprintf("work_%048x", 1))
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), c, true); !errors.Is(err, ErrReconciliation) {
		t.Fatal("unknown prior work allowed maintenance", err)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatal("unknown producer workspace removed", err)
	}
}
func TestStartupDirectorySweepRechecksAuthorityAndCancellation(t *testing.T) {
	for _, mode := range []string{"authority", "cancel", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			_, _, _, root := fixture(t)
			for i := 0; i < 80; i++ {
				if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("work_%048x", i)), 0700); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			check := func(ctx context.Context) error {
				calls++
				if calls == 10 {
					if mode == "authority" {
						return ErrReconciliation
					}
					if mode == "cancel" {
						cancel()
					}
				}
				return ctx.Err()
			}
			budget := time.Minute
			if mode == "timeout" {
				budget = 0
			}
			err := sweepStartupDirectories(ctx, root, 1024, budget, check)
			if mode == "authority" && !errors.Is(err, ErrReconciliation) || mode == "cancel" && !errors.Is(err, context.Canceled) || mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("maintenance error hidden", err)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			retained := 0
			for _, entry := range entries {
				if entry.IsDir() && len(entry.Name()) == 53 {
					retained++
				}
			}
			if retained == 0 {
				t.Fatal("lost authority failed to bound deletion")
			}
			if err = sweepStartupDirectories(context.Background(), root, 1024, time.Minute, func(context.Context) error { return nil }); err != nil {
				t.Fatal("fresh maintenance retry", err)
			}
		})
	}
}
