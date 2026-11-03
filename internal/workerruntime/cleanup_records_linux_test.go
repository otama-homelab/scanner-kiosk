//go:build linux

package workerruntime

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
)

const cleanupOwner = "owner_dispatch_0001"
const cleanupJob = "job_dispatch_000001"
const cleanupBoot = "worker_dispatch_001"
const cleanupPrior = "worker_previous_001"

type memoryCleanupDispatch struct {
	mu                                            sync.Mutex
	t                                             *testing.T
	mode                                          string
	file                                          documents.CleanupFile
	job                                           documents.Job
	claimed, recorded, finished                   bool
	claimCalls, loads, progressReads, finishReads int
}

func (m *memoryCleanupDispatch) NextCleanup(context.Context) (documents.CleanupCandidate, error) {
	if m.mode == "empty" {
		return documents.CleanupCandidate{}, documents.ErrNoWork
	}
	return documents.CleanupCandidate{OwnerID: cleanupOwner, JobID: cleanupJob, PreviousRunner: cleanupPrior}, nil
}
func (m *memoryCleanupDispatch) ClaimCleanupAttempt(_ context.Context, owner, job, previous, runner string, budget time.Duration) (documents.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.claimCalls++
	if owner != cleanupOwner || job != cleanupJob || previous != cleanupPrior || runner != cleanupBoot || budget != time.Minute {
		m.t.Fatal("changed claim identity/budget")
	}
	if m.mode == "claim-rollback" {
		return documents.Job{}, documents.ErrCommitUncertain
	}
	m.claimed = true
	if m.mode == "claim-committed" || m.mode == "claim-unreadable" {
		return documents.Job{}, documents.ErrCommitUncertain
	}
	return m.job, nil
}
func (m *memoryCleanupDispatch) LoadCleanupWork(ctx context.Context, owner, job, runner string, limit int) (documents.CleanupWork, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loads++
	if owner != cleanupOwner || job != cleanupJob || runner != cleanupBoot || limit != 1 {
		m.t.Fatal("changed load identity")
	}
	if err := ctx.Err(); err != nil {
		return documents.CleanupWork{}, err
	}
	if m.mode == "claim-unreadable" {
		return documents.CleanupWork{}, documents.ErrStorage
	}
	if !m.claimed {
		return documents.CleanupWork{}, documents.ErrConflict
	}
	work := documents.CleanupWork{Job: m.job}
	if !m.recorded {
		work.Files = []documents.CleanupFile{m.file}
		work.Remaining = 1
	}
	return work, nil
}
func (m *memoryCleanupDispatch) RecordCleanupRemoval(_ context.Context, owner, job, runner string, proof documents.CleanupRemoval) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if owner != cleanupOwner || job != cleanupJob || runner != cleanupBoot || proof.ArtifactID != m.file.ArtifactID || proof.Size != m.file.Size || proof.SHA256 != m.file.SHA256 || proof.RemovedAtMS <= 0 {
		m.t.Fatal("removal identity changed")
	}
	m.recorded = true
	if m.mode == "progress-committed" {
		return documents.ErrCommitUncertain
	}
	return nil
}
func (m *memoryCleanupDispatch) CleanupRemovalRecorded(context.Context, string, string, string, string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.progressReads++
	return m.recorded, nil
}
func (m *memoryCleanupDispatch) FinishCleanup(context.Context, string, string, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.recorded {
		return documents.ErrConflict
	}
	m.finished = true
	if m.mode == "finish-committed" {
		return documents.ErrCommitUncertain
	}
	return nil
}
func (m *memoryCleanupDispatch) CleanupFinishedRecorded(context.Context, string, string, string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.finishReads++
	return m.finished, nil
}

func TestCleanupDispatchRecordsWithRealFilesAndClaimUncertainty(t *testing.T) {
	for _, mode := range []string{"normal", "empty", "claim-committed", "claim-rollback", "claim-unreadable", "progress-committed", "finish-committed", "authority-before-claim", "authority-after-claim"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			data := []byte("private cleanup fixture")
			path := filepath.Join(root, "old.pdf")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			records := &memoryCleanupDispatch{t: t, mode: mode, file: documents.CleanupFile{ArtifactID: "artifact_dispatch_01", RelativePath: "old.pdf", Size: int64(len(data)), SHA256: sha256.Sum256(data)}, job: documents.Job{ID: cleanupJob, DocumentID: "document_dispatch_01", Kind: "cleanup", Configuration: `{"version":1}`, Revision: 1, DeadlineAtMS: time.Now().Add(time.Minute).UnixMilli()}}
			var checks atomic.Int32
			check := func(context.Context) error {
				count := checks.Add(1)
				if mode == "authority-before-claim" && count == 1 || mode == "authority-after-claim" && count == 2 {
					return ErrReconciliation
				}
				return nil
			}
			handled, err := dispatchCleanup(context.Background(), records, root, cleanupBoot, 1024, time.Minute, check)
			removed := mode == "normal" || mode == "claim-committed" || mode == "progress-committed" || mode == "finish-committed"
			if removed {
				if err != nil || !handled || !records.recorded || !records.finished {
					t.Fatal("confirmed dispatch failed", handled, err)
				}
				if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("manifest file retained", err)
				}
				if records.claimCalls != 1 {
					t.Fatal("claim automatically retried")
				}
				if mode == "progress-committed" && records.progressReads != 1 || mode == "finish-committed" && records.finishReads != 1 {
					t.Fatal("missing commit readback")
				}
			} else {
				if handled || mode != "empty" && !errors.Is(err, ErrReconciliation) || mode == "empty" && err != nil {
					t.Fatal("unconfirmed claim allowed work", handled, err)
				}
				actual, err := os.ReadFile(path)
				if err != nil || string(actual) != string(data) || records.recorded || records.finished {
					t.Fatal("unconfirmed authority touched files", err)
				}
				if mode == "empty" || mode == "authority-before-claim" {
					if records.claimCalls != 0 {
						t.Fatal("claimed without authority/work")
					}
				} else if records.claimCalls != 1 {
					t.Fatal("uncertain claim repeated")
				}
			}
		})
	}
}
