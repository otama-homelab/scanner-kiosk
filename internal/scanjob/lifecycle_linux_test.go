//go:build linux

package scanjob

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type lifecycleRecords struct {
	*faultRecords
	releaseError, abortError error
	releases, aborts         int
}

func (r *lifecycleRecords) ReleaseScanArtifacts() error { r.releases++; return r.releaseError }
func (r *lifecycleRecords) AbortScanArtifacts() error   { r.aborts++; return r.abortError }

func TestPublishedDerivativeReleaseFailureCannotDeleteOriginal(t *testing.T) {
	base, db, root, job, driver, _ := fixture(t, false)
	records := &lifecycleRecords{faultRecords: base, releaseError: errors.New("release failed")}
	prepared, err := PrepareClaimed(context.Background(), records, driver, owner, job, boot, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	original, err := prepared.Publish(context.Background())
	if err == nil || original.ID == "" {
		t.Fatal(original, err)
	}
	if !errors.Is(prepared.Finish(context.Background(), "failed"), ErrClosed) {
		t.Fatal("published bundle was abortable")
	}
	if _, err = os.Stat(filepath.Join(root, original.RelativePath)); err != nil {
		t.Fatal("committed original lost", err)
	}
	if count(t, db, "SELECT count(*) FROM pages") != 1 || records.aborts != 0 {
		t.Fatal("committed bundle cleaned")
	}
	records.releaseError = nil
	if _, err = prepared.Publish(context.Background()); err != nil || base.writes != 1 || records.releases != 2 {
		t.Fatal("release retry repeated publication", err, base.writes, records.releases)
	}
}

func TestDerivativeAbortMustFinishBeforeNativeOriginalCleanup(t *testing.T) {
	base, db, root, job, driver, _ := fixture(t, false)
	records := &lifecycleRecords{faultRecords: base, abortError: errors.New("unpublished preview cleanup failed")}
	prepared, err := PrepareClaimed(context.Background(), records, driver, owner, job, boot, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err = records.RequestCancel(context.Background(), owner, job); err != nil {
		t.Fatal(err)
	}
	if _, err = prepared.Publish(context.Background()); err == nil {
		t.Fatal("cancelled original published")
	}
	if err = prepared.Finish(context.Background(), "failed"); err == nil {
		t.Fatal("unknown preview cleanup released original")
	}
	if _, err = os.Stat(filepath.Join(root, prepared.artifact.RelativePath)); err != nil {
		t.Fatal("original removed before derivative cleanup", err)
	}
	if count(t, db, "SELECT count(*) FROM reservations WHERE job_id=?", job) != 1 || records.releases != 0 {
		t.Fatal("uncertain cleanup freed resources")
	}
	records.abortError = nil
	if err = prepared.Finish(context.Background(), "failed"); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "SELECT count(*) FROM reservations WHERE job_id=?", job) != 0 || records.aborts != 2 {
		t.Fatal("known cleanup did not finish")
	}
}
