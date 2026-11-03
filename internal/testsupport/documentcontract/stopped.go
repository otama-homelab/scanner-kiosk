package documentcontract

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
)

type StopRecords interface {
	FinishStoppedRender(context.Context, string, string, documents.RenderStop, string) error
	RenderStoppedRecorded(context.Context, string, string, string) (bool, error)
	FinishStoppedScan(context.Context, string, string, documents.ScanStop, int64, string) error
	ScanStoppedRecorded(context.Context, string, string) (bool, error)
}

type StopFixture struct {
	DispatchFixture
	Stops                StopRecords
	OpenPeerStops        func() StopRecords
	RenderStop           documents.RenderStop
	ScanStop             documents.ScanStop
	WorkspaceCleanedAtMS int64
}

// RunUncertainStop checks metadata after synthetic stop/cleanup evidence. It
// neither stops processes nor removes files or proves physical device idle.
func RunUncertainStop(t *testing.T, newFixture func(*testing.T, string) StopFixture) {
	t.Helper()
	ctx := context.Background()
	for _, kind := range []string{"scan", "render"} {
		for _, cancelled := range []bool{false, true} {
			for _, committed := range []bool{false, true} {
				name := kind + " failure"
				if cancelled {
					name = kind + " cancellation"
				}
				if committed {
					name += " lost acknowledgement"
				} else {
					name += " rollback"
				}
				t.Run(name, func(t *testing.T) {
					f := newFixture(t, kind)
					peer := f.OpenPeerStops()
					reader := f.OpenPeerRecords()
					before, err := reader.ReadDocument(ctx, f.Owner, f.Document)
					if err != nil {
						t.Fatal(err)
					}
					if cancelled {
						if _, err := f.Records.CancelJob(ctx, f.Owner, f.Job, "contract_stopped_cancel_001", time.Hour); err != nil {
							t.Fatal(err)
						}
					}
					recorded := func(records StopRecords, owner string) (bool, error) {
						if kind == "scan" {
							return records.ScanStoppedRecorded(ctx, owner, f.Job)
						}
						return records.RenderStoppedRecorded(ctx, owner, f.Job, f.Boot)
					}
					invoke := func(records StopRecords, invalid bool) error {
						if kind == "scan" {
							stop := f.ScanStop
							if invalid {
								stop.ProcessStoppedAtMS = 0
							}
							return records.FinishStoppedScan(ctx, f.Owner, f.Job, stop, f.WorkspaceCleanedAtMS, "failed")
						}
						stop := f.RenderStop
						if invalid {
							stop.ProcessStoppedAtMS = 0
						}
						return records.FinishStoppedRender(ctx, f.Owner, f.Job, stop, "failed")
					}
					if err := invoke(f.Stops, true); !errors.Is(err, documents.ErrConflict) {
						t.Fatal("missing stop evidence accepted", err)
					}
					done, err := recorded(peer, f.Owner)
					if err != nil || done {
						t.Fatal("cancel intent or rejected evidence became confirmed stop", done, err)
					}
					restore := f.FailCommit(committed)
					err = invoke(f.Stops, false)
					restore()
					if !errors.Is(err, documents.ErrCommitUncertain) {
						t.Fatal("uncertain stop reported success", err)
					}
					done, err = recorded(peer, f.Owner)
					if err != nil || done != committed {
						t.Fatal("incorrect durable stop read-back", done, err)
					}
					foreign, err := recorded(peer, f.OtherOwner)
					if err != nil || foreign {
						t.Fatal("foreign stop disclosed", foreign, err)
					}
					job, err := reader.ReadJob(ctx, f.Owner, f.Job)
					expected := "running"
					if cancelled {
						expected = "cancelling"
					}
					if committed {
						expected = "failed"
						if cancelled {
							expected = "cancelled"
						}
					}
					if err != nil || job.State != expected {
						t.Fatal("job/stop receipt diverged", job, err)
					}
					current, err := reader.ReadDocument(ctx, f.Owner, f.Document)
					if err != nil || current.PageCount != before.PageCount || !reflect.DeepEqual(current.Pages, before.Pages) || current.Revision != before.Revision || !current.CreatedAt.Equal(before.CreatedAt) || !current.ExpiresAt.Equal(before.ExpiresAt) {
						t.Fatal("stop changed prior content or retention", current, err)
					}
					again, err := recorded(f.Stops, f.Owner)
					if err != nil || again != done {
						t.Fatal("stop read-back disagrees across connections", again, err)
					}
				})
			}
		}
	}
}
