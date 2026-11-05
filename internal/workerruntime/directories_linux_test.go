//go:build linux

package workerruntime

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"scanner-kiosk/internal/documents"
	"strings"
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
			_, store, _, root := fixture(t)
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
			err := sweepStartupDirectories(ctx, store, root, 1024, budget, check)
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
			if err = sweepStartupDirectories(context.Background(), store, root, 1024, time.Minute, func(context.Context) error { return nil }); err != nil {
				t.Fatal("fresh maintenance retry", err)
			}
		})
	}
}

func TestStartupKeepsEmptyReceivingImportDirectory(t *testing.T) {
	c, store, _, root := fixture(t)
	ctx := context.Background()
	a, err := store.AdmitImport(ctx, owner, documents.ImportRequest{IdempotencyKey: "request_live_intake1", InputBytes: 20, SHA256: sha256.Sum256([]byte("source"))}, documents.ImportLimits{AdmissionLimits: documents.AdmissionLimits{UserMaxBytes: 2 << 20, GlobalMaxBytes: 4 << 20, JobMaxBytes: 1 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour}, MaxDocuments: 20, DocumentLifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.StartImportUpload(ctx, owner, a.JobID, a.ProducerID); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, strings.Split(a.Input.RelativePath, "/")[0])
	if err = os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err = Run(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		t.Fatal("startup removed live API intake directory", err)
	}
}

func TestStartupSweepReferenceFailurePreservesDirectory(t *testing.T) {
	_, store, db, root := fixture(t)
	directory := filepath.Join(root, fmt.Sprintf("work_%048x", 42))
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sweepStartupDirectories(context.Background(), store, root, 1024, time.Minute, func(context.Context) error { return nil }); err == nil {
		t.Fatal("reference lookup failure accepted")
	}
	if _, err := os.Stat(directory); err != nil {
		t.Fatal("uncertain reference directory removed", err)
	}
}
