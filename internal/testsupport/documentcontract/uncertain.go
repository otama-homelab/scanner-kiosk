package documentcontract

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"scanner-kiosk/internal/documenthttp"
	"scanner-kiosk/internal/documents"
)

// RunUncertainLifecycle requires a backend-level commit fault: either roll back
// or commit while losing its acknowledgement. A Records error wrapper is not
// sufficient. It checks intent/receipt recovery, never physical stop or cleanup.
func RunUncertainLifecycle(t *testing.T, newFixture func(*testing.T) LifecycleFixture) {
	t.Helper()
	ctx := context.Background()
	for _, operation := range []string{"cancel", "delete"} {
		for _, committed := range []bool{false, true} {
			name := operation + " rollback"
			if committed {
				name = operation + " lost acknowledgement"
			}
			t.Run(name, func(t *testing.T) {
				f := newFixture(t)
				peer := f.OpenPeerRecords()
				invoke := func(records documenthttp.Records) (documents.JobProjection, error) {
					if operation == "cancel" {
						return records.CancelJob(ctx, f.Owner, f.Job, "contract_uncertain_001", time.Hour)
					}
					return records.RequestDelete(ctx, f.Owner, documents.DeleteRequest{DocumentID: f.Document, Revision: f.Revision, IdempotencyKey: "contract_uncertain_001"}, time.Hour)
				}
				restore := f.FailCommit(committed)
				result, err := invoke(f.Records)
				restore()
				if !errors.Is(err, documents.ErrCommitUncertain) || !reflect.DeepEqual(result, documents.JobProjection{}) {
					t.Fatal("uncertain command exposed success", result, err)
				}
				if operation == "cancel" {
					job, err := peer.ReadJob(ctx, f.Owner, f.Job)
					expected := "queued"
					if committed {
						expected = "cancelling"
					}
					if err != nil || job.State != expected {
						t.Fatal("wrong durable cancellation outcome", job, err)
					}
				} else {
					_, err := peer.ReadPageOriginal(ctx, f.Owner, f.Document, f.Pages[0])
					if committed && !errors.Is(err, documents.ErrUnavailable) || !committed && err != nil {
						t.Fatal("wrong durable revocation outcome", err)
					}
				}
				recovered, err := invoke(peer)
				if err != nil || recovered.ID == "" {
					t.Fatal("same-key recovery", recovered, err)
				}
				expectedKind, expectedState := "render", "cancelling"
				if operation == "delete" {
					expectedKind, expectedState = "cleanup", "queued"
				}
				if operation == "cancel" && recovered.ID != f.Job {
					t.Fatal("cancellation recovered another job", recovered)
				}
				if recovered.Kind != expectedKind || recovered.State != expectedState {
					t.Fatal("wrong recovered intent", recovered)
				}
				replay, err := invoke(f.Records)
				if err != nil || !reflect.DeepEqual(recovered, replay) {
					t.Fatal("recovered receipt changed", replay, err)
				}
			})
		}
	}
}

// RunUncertainEdits checks durable revision/page changes and exact replay after
// transaction acknowledgement loss, without a SQL handle in the assertions.
func RunUncertainEdits(t *testing.T, newFixture func(*testing.T) Fixture) {
	t.Helper()
	ctx := context.Background()
	for _, committed := range []bool{false, true} {
		name := "rollback"
		if committed {
			name = "lost acknowledgement"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			peer := f.OpenPeerRecords()
			before, err := peer.ReadDocument(ctx, f.Owner, f.Document)
			if err != nil {
				t.Fatal(err)
			}
			request := documents.EditRequest{DocumentID: f.Document, Revision: 1, IdempotencyKey: "contract_uncertain_edit_001", Operation: "rotate", PageID: f.Pages[0], Rotation: 90}
			restore := f.FailCommit(committed)
			result, err := f.Records.Edit(ctx, f.Owner, request, time.Hour)
			restore()
			if !errors.Is(err, documents.ErrCommitUncertain) || !reflect.DeepEqual(result, documents.Projection{}) {
				t.Fatal("uncertain edit exposed success", result, err)
			}
			current, err := peer.ReadDocument(ctx, f.Owner, f.Document)
			revision, rotation := int64(1), 0
			if committed {
				revision, rotation = 2, 90
			}
			if err != nil || current.Revision != revision || len(current.Pages) != 2 || current.Pages[0].Rotation != rotation || !current.ExpiresAt.Equal(before.ExpiresAt) {
				t.Fatal("wrong durable edit outcome", current, err)
			}
			recovered, err := peer.Edit(ctx, f.Owner, request, time.Hour)
			if err != nil || recovered.Revision != 2 || len(recovered.Pages) != 2 || recovered.Pages[0].Rotation != 90 {
				t.Fatal("same-key edit recovery", recovered, err)
			}
			replay, err := f.Records.Edit(ctx, f.Owner, request, time.Hour)
			if err != nil || !reflect.DeepEqual(recovered, replay) {
				t.Fatal("recovered edit receipt changed", replay, err)
			}
			current, err = peer.ReadDocument(ctx, f.Owner, f.Document)
			if err != nil || current.Revision != 2 || !current.ExpiresAt.Equal(before.ExpiresAt) {
				t.Fatal("recovery edited twice or changed retention", current, err)
			}
		})
	}
}

// RunUncertainRender checks the durable receipt before same-key recovery. It
// admits jobs only; native rendering and output publication are separate tests.
func RunUncertainRender(t *testing.T, newFixture func(*testing.T) RenderFixture) {
	t.Helper()
	ctx := context.Background()
	for _, committed := range []bool{false, true} {
		name := "rollback"
		if committed {
			name = "lost acknowledgement"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			peer := f.OpenPeer()
			request := documents.RenderAdmission{DocumentID: f.Document, Revision: 1, IdempotencyKey: "contract_uncertain_render_001", Settings: documents.RenderSettings{Format: "pdf"}, ReserveBytes: 1 << 20}
			restore := f.FailCommit(committed)
			result, err := f.Renders.AdmitRender(ctx, f.Owner, request, f.Limits)
			restore()
			if !errors.Is(err, documents.ErrCommitUncertain) || result != (documents.Admission{}) {
				t.Fatal("uncertain render exposed success", result, err)
			}
			previous, found, err := peer.FindRender(ctx, f.Owner, request)
			if err != nil || found != committed || committed && (!previous.Replay || previous.JobID == "") {
				t.Fatal("wrong durable render receipt", previous, found, err)
			}
			recovered, err := peer.AdmitRender(ctx, f.Owner, request, f.Limits)
			if err != nil || recovered.Replay != committed || recovered.DocumentID != f.Document || recovered.Revision != 2 || recovered.JobID == "" || committed && recovered.JobID != previous.JobID {
				t.Fatal("same-key render recovery", recovered, err)
			}
			replay, err := f.Renders.AdmitRender(ctx, f.Owner, request, f.Limits)
			if err != nil || !replay.Replay || replay.JobID != recovered.JobID || replay.Revision != recovered.Revision || replay.DocumentID != recovered.DocumentID {
				t.Fatal("recovered render receipt changed", replay, err)
			}
			current, err := f.Records.ReadDocument(ctx, f.Owner, f.Document)
			if err != nil || current.Revision != 2 {
				t.Fatal("recovery advanced render revision twice", current, err)
			}
			job, err := f.Records.ReadJob(ctx, f.Owner, recovered.JobID)
			if err != nil || job.Kind != "render" || job.State != "queued" || job.InputRevision != 2 {
				t.Fatal("recovered render job", job, err)
			}
		})
	}
}

// RunUncertainScan covers admission only. It never claims a job, launches a
// scanner or treats a receipt/device epoch as evidence of physical stopping.
func RunUncertainScan(t *testing.T, newFixture func(*testing.T) ScanFixture) {
	t.Helper()
	ctx := context.Background()
	for _, nextPage := range []bool{false, true} {
		for _, committed := range []bool{false, true} {
			name := "first page"
			if nextPage {
				name = "next page"
			}
			if committed {
				name += " lost acknowledgement"
			} else {
				name += " rollback"
			}
			t.Run(name, func(t *testing.T) {
				f := newFixture(t)
				peer := f.OpenPeer()
				records := f.OpenPeerRecords()
				before, err := records.ReadDocument(ctx, f.Owner, f.Document)
				if err != nil {
					t.Fatal(err)
				}
				request := f.Request
				if nextPage {
					request.DocumentID, request.Revision, request.Purpose = f.Document, 1, ""
				}
				restore := f.FailCommit(committed)
				result, err := f.Scans.AdmitReadyScan(ctx, f.Owner, request, f.Limits)
				restore()
				if !errors.Is(err, documents.ErrCommitUncertain) || result != (documents.ScanAdmission{}) {
					t.Fatal("uncertain scan exposed success", result, err)
				}
				history, _, err := records.ReadHistory(ctx, f.Owner, 20, nil)
				expected := 1
				if committed && !nextPage {
					expected = 2
				}
				if err != nil || len(history) != expected {
					t.Fatal("wrong durable document count", history, err)
				}
				if !committed && !reflect.DeepEqual(history[0], before) {
					t.Fatal("rollback changed existing document", history[0], before)
				}
				var durable documents.Projection
				for _, doc := range history {
					if nextPage && doc.ID == f.Document || !nextPage && doc.ID != f.Document {
						durable = doc
					}
				}
				if committed && (durable.ID == "" || durable.ActiveJobID == nil || durable.State != "scanning") {
					t.Fatal("committed scan absent", durable)
				}
				recovered, err := peer.AdmitReadyScan(ctx, f.Owner, request, f.Limits)
				revision := int64(1)
				if nextPage {
					revision = 2
				}
				if err != nil || recovered.Replay != committed || recovered.DocumentID == "" || recovered.JobID == "" || recovered.Revision != revision || recovered.LeaseEpoch != 2 {
					t.Fatal("scan recovery duplicated or lost admission", recovered, err)
				}
				if committed && (recovered.DocumentID != durable.ID || recovered.JobID != *durable.ActiveJobID || recovered.Revision != durable.Revision) {
					t.Fatal("recovery changed durable admission", recovered, durable)
				}
				replay, err := f.Scans.AdmitReadyScan(ctx, f.Owner, request, f.Limits)
				if err != nil || !replay.Replay || replay.Admission.JobID != recovered.JobID || replay.DocumentID != recovered.DocumentID || replay.Revision != recovered.Revision || replay.LeaseEpoch != recovered.LeaseEpoch {
					t.Fatal("scan receipt changed", replay, err)
				}
				current, err := records.ReadDocument(ctx, f.Owner, recovered.DocumentID)
				if err != nil || current.Revision != revision || current.ActiveJobID == nil || *current.ActiveJobID != recovered.JobID {
					t.Fatal("recovered document", current, err)
				}
				if nextPage && (current.ID != f.Document || !reflect.DeepEqual(current.Pages, before.Pages) || !current.CreatedAt.Equal(before.CreatedAt) || !current.ExpiresAt.Equal(before.ExpiresAt)) {
					t.Fatal("next-page recovery changed prior content or retention", current)
				}
				history, _, err = records.ReadHistory(ctx, f.Owner, 20, nil)
				expected = 2
				if nextPage {
					expected = 1
				}
				if err != nil || len(history) != expected {
					t.Fatal("recovery duplicated document", history, err)
				}
				job, err := records.ReadJob(ctx, f.Owner, recovered.JobID)
				if err != nil || job.Kind != "scan" || job.State != "queued" || job.InputRevision != revision {
					t.Fatal("recovered scan job", job, err)
				}
			})
		}
	}
}
