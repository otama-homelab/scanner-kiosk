package documents

import (
	"context"
	"fmt"
	"testing"
)

func placedScan(t *testing.T, f *fixture, request ScanRequest, limits ScanLimits) (ScanAdmission, ScanStop) {
	t.Helper()
	admitted, err := f.store.AdmitScan(context.Background(), alice, request, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimScan(context.Background(), alice, admitted.JobID, runner); err != nil {
		t.Fatal(err)
	}
	work, err := f.store.LoadScanWork(context.Background(), alice, admitted.JobID, runner)
	if err != nil || work.Snapshot.InsertBeforePageID != request.InsertBeforePageID || work.Snapshot.ReplacePageID != request.ReplacePageID {
		t.Fatal("worker lost placement snapshot", err)
	}
	f.now += 100
	return admitted, ScanStop{DeviceID: scanner, WorkerBootID: runner, LeaseEpoch: admitted.LeaseEpoch, ProcessStoppedAtMS: f.now - 20, IdleObservedAtMS: f.now - 10}
}
func seedPlacementPages(t *testing.T) (*fixture, ScanAdmission, []string) {
	t.Helper()
	f, a, stop := startedCapture(t)
	first, err := f.store.CommitScanPage(context.Background(), alice, a.JobID, a.Revision, captureArtifact(), stop)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{first.ID}
	for i := 2; i <= 3; i++ {
		request := scanRequest()
		request.DocumentID = a.DocumentID
		request.Revision = a.Revision
		request.Purpose = ""
		request.IdempotencyKey = fmt.Sprintf("placement_seed_%03d", i)
		a, stop = placedScan(t, f, request, scanLimits())
		artifact := captureArtifact()
		artifact.ID = fmt.Sprintf("placement_original_%03d", i)
		artifact.RelativePath = fmt.Sprintf("placement/original_%03d", i)
		page, err := f.store.CommitScanPage(context.Background(), alice, a.JobID, a.Revision, artifact, stop)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, page.ID)
	}
	return f, a, ids
}
func TestScanInsertionAndReplacementAreAtomic(t *testing.T) {
	for _, mode := range []string{"insert-first", "insert-middle", "replace-middle", "replace-cancel", "replace-failure", "replace-failed-commit", "replace-storage-failure"} {
		t.Run(mode, func(t *testing.T) {
			f, prior, ids := seedPlacementPages(t)
			request := scanRequest()
			request.DocumentID = prior.DocumentID
			request.Revision = prior.Revision
			request.Purpose = ""
			request.IdempotencyKey = "placement_request_001"
			limits := scanLimits()
			if mode == "insert-first" {
				request.InsertBeforePageID = ids[0]
			} else if mode == "insert-middle" {
				request.InsertBeforePageID = ids[1]
			} else {
				request.ReplacePageID = ids[1]
				limits.MaxPages = 3
			}
			a, stop := placedScan(t, f, request, limits)
			// Admission and physical work must retain the old page and source.
			if scalar(t, f.docs, "SELECT count(*) FROM pages WHERE id=?", ids[1]) != 1 {
				t.Fatal("old page lost before success")
			}
			replay, err := f.store.AdmitScan(context.Background(), alice, request, limits)
			if err != nil || !replay.Replay || replay.JobID != a.JobID {
				t.Fatal("placement replay", err)
			}
			changed := request
			changed.ReplacePageID = ""
			changed.InsertBeforePageID = ids[2]
			if _, err = f.store.AdmitScan(context.Background(), alice, changed, limits); err == nil {
				t.Fatal("placement omitted from idempotency identity")
			}
			if mode == "replace-cancel" || mode == "replace-failure" {
				if mode == "replace-cancel" {
					if err = f.store.RequestCancel(context.Background(), alice, a.JobID); err != nil {
						t.Fatal(err)
					}
				}
				if err = f.store.FinishStoppedScan(context.Background(), alice, a.JobID, stop, f.now-5, "failed"); err != nil {
					t.Fatal(err)
				}
			} else {
				artifact := captureArtifact()
				artifact.ID = "placement_result_001"
				artifact.RelativePath = "placement/result_001"
				if mode == "replace-storage-failure" {
					artifact.ID = captureArtifact().ID
				}
				revision := a.Revision
				if mode == "replace-failed-commit" {
					revision++
				}
				page, err := f.store.CommitScanPage(context.Background(), alice, a.JobID, revision, artifact, stop)
				if mode == "replace-failed-commit" || mode == "replace-storage-failure" {
					if err == nil {
						t.Fatal("stale completion accepted")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					want := []string{ids[0], page.ID, ids[1], ids[2]}
					if mode == "insert-first" {
						want = []string{page.ID, ids[0], ids[1], ids[2]}
					}
					if mode == "replace-middle" {
						want = []string{ids[0], page.ID, ids[2]}
					}
					projection, err := f.store.ReadDocument(context.Background(), alice, a.DocumentID)
					if err != nil || len(projection.Pages) != len(want) {
						t.Fatal("result page count", err)
					}
					for i, id := range want {
						if projection.Pages[i].ID != id || projection.Pages[i].Ordinal != i {
							t.Fatal("wrong order", projection.Pages)
						}
					}
					return
				}
			}
			if scalar(t, f.docs, "SELECT count(*) FROM pages WHERE id=?", ids[1]) != 1 || scalar(t, f.docs, "SELECT count(*) FROM pages WHERE document_id=?", a.DocumentID) != 3 {
				t.Fatal("failed replacement lost original")
			}
		})
	}
}

func TestScanPlacementRejectsInvalidTargets(t *testing.T) {
	for _, mode := range []string{"foreign-target", "both", "new-document", "invalid-id", "page-limit", "stale-revision", "wrong-owner"} {
		t.Run(mode, func(t *testing.T) {
			f, prior, ids := seedPlacementPages(t)
			request := scanRequest()
			request.DocumentID = prior.DocumentID
			request.Revision = prior.Revision
			request.Purpose = ""
			request.IdempotencyKey = "placement_reject_001"
			request.InsertBeforePageID = ids[1]
			limits := scanLimits()
			owner := alice
			switch mode {
			case "foreign-target":
				request.InsertBeforePageID = "page_not_in_document"
			case "both":
				request.ReplacePageID = ids[1]
			case "new-document":
				request.DocumentID = ""
				request.Revision = 0
				request.Purpose = "document"
			case "invalid-id":
				request.InsertBeforePageID = "x"
			case "page-limit":
				limits.MaxPages = 3
			case "stale-revision":
				request.Revision--
			case "wrong-owner":
				owner = bob
			}
			if _, err := f.store.AdmitScan(context.Background(), owner, request, limits); err == nil {
				t.Fatal("invalid placement accepted")
			}
			if scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 || scalar(t, f.docs, "SELECT count(*) FROM pages WHERE document_id=?", prior.DocumentID) != 3 {
				t.Fatal("rejection changed pages/reservations")
			}
		})
	}
}
