package documentcontract

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
)

// ReopenRecords must close all fixture DB handles and recreate the adapter via
// normal backend initialization, preserving files and clock. No concurrent work
// may be active. These tests prove clean reopen, not upgrade or crash durability.
func RunReopenEdits(t *testing.T, newFixture func(*testing.T) Fixture) {
	t.Helper()
	ctx := context.Background()
	for _, disabled := range []bool{false, true} {
		name := "historical receipt and document"
		if disabled {
			name = "disabled principal persists"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			request := documents.EditRequest{DocumentID: f.Document, Revision: 1, IdempotencyKey: "contract_reopen_edit_001", Operation: "rotate", PageID: f.Pages[0], Rotation: 90}
			first, err := f.Records.Edit(ctx, f.Owner, request, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			later := request
			later.IdempotencyKey, later.Revision, later.Rotation = "contract_reopen_edit_002", 2, 180
			current, err := f.Records.Edit(ctx, f.Owner, later, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if disabled {
				f.Disable()
			}
			records := f.ReopenRecords()
			if disabled {
				if _, err := records.ReadDocument(ctx, f.Owner, f.Document); !errors.Is(err, documents.ErrUnavailable) {
					t.Fatal("reopen revived disabled owner", err)
				}
				if _, err := records.Edit(ctx, f.Owner, request, time.Hour); !errors.Is(err, documents.ErrUnavailable) {
					t.Fatal("disabled receipt replay after reopen", err)
				}
				return
			}
			after, err := records.ReadDocument(ctx, f.Owner, f.Document)
			if err != nil || !reflect.DeepEqual(current, after) {
				t.Fatal("reopen changed document", after, err)
			}
			replay, err := records.Edit(ctx, f.Owner, request, time.Hour)
			if err != nil || !reflect.DeepEqual(first, replay) {
				t.Fatal("reopen lost historical receipt", replay, err)
			}
			changed := request
			changed.Rotation = 270
			if _, err := records.Edit(ctx, f.Owner, changed, time.Hour); !errors.Is(err, documents.ErrIdempotencyConflict) {
				t.Fatal("reopen forgot receipt intent", err)
			}
			if _, err := records.ReadDocument(ctx, f.OtherOwner, f.Document); !errors.Is(err, documents.ErrUnavailable) {
				t.Fatal("reopen lost owner isolation", err)
			}
		})
	}
}

func RunReopenLifecycle(t *testing.T, newFixture func(*testing.T) LifecycleFixture) {
	t.Helper()
	ctx := context.Background()
	for _, operation := range []string{"cancel", "delete"} {
		t.Run(operation, func(t *testing.T) {
			f := newFixture(t)
			request := documents.DeleteRequest{DocumentID: f.Document, Revision: f.Revision, IdempotencyKey: "contract_reopen_delete_001"}
			var before documents.JobProjection
			var err error
			if operation == "cancel" {
				before, err = f.Records.CancelJob(ctx, f.Owner, f.Job, "contract_reopen_cancel_001", time.Hour)
			} else {
				before, err = f.Records.RequestDelete(ctx, f.Owner, request, time.Hour)
			}
			if err != nil {
				t.Fatal(err)
			}
			records := f.ReopenRecords()
			var after documents.JobProjection
			if operation == "cancel" {
				after, err = records.CancelJob(ctx, f.Owner, f.Job, "contract_reopen_cancel_001", time.Hour)
			} else {
				after, err = records.RequestDelete(ctx, f.Owner, request, time.Hour)
			}
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("reopen lost intent receipt", after, err)
			}
			if operation == "delete" {
				if _, err := records.ReadPageOriginal(ctx, f.Owner, f.Document, f.Pages[0]); !errors.Is(err, documents.ErrUnavailable) {
					t.Fatal("reopen revived deleted original access", err)
				}
				request.IdempotencyKey = "contract_reopen_delete_002"
				again, err := records.RequestDelete(ctx, f.Owner, request, time.Hour)
				if err != nil || again.ID != before.ID {
					t.Fatal("reopen duplicated cleanup", again, err)
				}
			}
			if _, err := records.ReadJob(ctx, f.OtherOwner, after.ID); !errors.Is(err, documents.ErrUnavailable) {
				t.Fatal("reopen lost job owner isolation", err)
			}
		})
	}
}

func RunReopenRenderAdmission(t *testing.T, newFixture func(*testing.T) RenderFixture) {
	t.Helper()
	t.Run("render receipt and job persist", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		request := documents.RenderAdmission{DocumentID: f.Document, Revision: 1, IdempotencyKey: "contract_reopen_render_001", Settings: documents.RenderSettings{Format: "pdf"}, ReserveBytes: 1 << 20}
		first, err := f.Renders.AdmitRender(ctx, f.Owner, request, f.Limits)
		if err != nil {
			t.Fatal(err)
		}
		before, err := f.Records.ReadDocument(ctx, f.Owner, f.Document)
		if err != nil {
			t.Fatal(err)
		}
		records, renders := f.Reopen()
		found, yes, err := renders.FindRender(ctx, f.Owner, request)
		if err != nil || !yes || !found.Replay || found.JobID != first.JobID || found.DocumentID != first.DocumentID || found.Revision != first.Revision {
			t.Fatal("reopen lost render receipt", found, yes, err)
		}
		replay, err := renders.AdmitRender(ctx, f.Owner, request, f.Limits)
		if err != nil || !reflect.DeepEqual(found, replay) {
			t.Fatal("reopen duplicated render admission", replay, err)
		}
		after, err := records.ReadDocument(ctx, f.Owner, f.Document)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("render reopen changed document", after, err)
		}
		job, err := records.ReadJob(ctx, f.Owner, first.JobID)
		if err != nil || job.State != "queued" || job.Kind != "render" || job.InputRevision != first.Revision {
			t.Fatal("render reopen changed queued job", job, err)
		}
		if _, err := records.ReadJob(ctx, f.OtherOwner, first.JobID); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("reopen disclosed render job", err)
		}
	})
}

func RunReopenScanAdmission(t *testing.T, newFixture func(*testing.T) ScanFixture) {
	t.Helper()
	for _, nextPage := range []bool{false, true} {
		name := "first page"
		if nextPage {
			name = "next page"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			request := f.Request
			if nextPage {
				request.DocumentID, request.Revision, request.Purpose = f.Document, 1, ""
			}
			first, err := f.Scans.AdmitReadyScan(ctx, f.Owner, request, f.Limits)
			if err != nil {
				t.Fatal(err)
			}
			before, err := f.Records.ReadDocument(ctx, f.Owner, first.DocumentID)
			if err != nil {
				t.Fatal(err)
			}
			f.Withdraw()
			records, scans := f.Reopen()
			replay, err := scans.AdmitReadyScan(ctx, f.Owner, request, f.Limits)
			if err != nil || !replay.Replay || replay.JobID != first.JobID || replay.DocumentID != first.DocumentID || replay.Revision != first.Revision || replay.LeaseEpoch != first.LeaseEpoch {
				t.Fatal("reopen lost scan receipt or consumed epoch twice", replay, err)
			}
			after, err := records.ReadDocument(ctx, f.Owner, first.DocumentID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("scan reopen changed prior content or TTL", after, err)
			}
			job, err := records.ReadJob(ctx, f.Owner, first.JobID)
			if err != nil || job.Kind != "scan" || job.State != "queued" || job.InputRevision != first.Revision {
				t.Fatal("scan reopen changed queued work", job, err)
			}
			fresh := f.Request
			fresh.IdempotencyKey = "contract_reopen_fresh_scan_001"
			if _, err := scans.AdmitReadyScan(ctx, f.Owner, fresh, f.Limits); !errors.Is(err, documents.ErrDeviceBusy) {
				t.Fatal("reopen bypassed withdrawn readiness", err)
			}
			if _, err := records.ReadJob(ctx, f.OtherOwner, first.JobID); !errors.Is(err, documents.ErrUnavailable) {
				t.Fatal("reopen disclosed scan job", err)
			}
		})
	}
}
