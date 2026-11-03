package documentcontract

import (
	"context"
	"errors"
	"testing"

	"scanner-kiosk/internal/documents"
)

// DispatchRecords is the existing worker claim surface, without a SQL handle.
type DispatchRecords interface {
	ClaimNextScanWithLimit(context.Context, string, int) (documents.ClaimedScan, error)
	ClaimNextRender(context.Context, string) (documents.ClaimedRender, error)
}

type DispatchFixture struct {
	Fixture
	Claims         DispatchRecords
	OpenPeerClaims func() DispatchRecords
	Job, Boot      string
}

// RunUncertainDispatch starts with one queued, reserved job (and a held device
// lease for scans). It never executes work, releases resources or proves a
// process/physical fence. Faults affect the real backend transaction commit.
func RunUncertainDispatch(t *testing.T, newFixture func(*testing.T, string) DispatchFixture) {
	t.Helper()
	ctx := context.Background()
	for _, kind := range []string{"scan", "render"} {
		for _, committed := range []bool{false, true} {
			name := kind + " rollback"
			if committed {
				name = kind + " lost acknowledgement"
			}
			t.Run(name, func(t *testing.T) {
				f := newFixture(t, kind)
				peer := f.OpenPeerClaims()
				reader := f.OpenPeerRecords()
				invoke := func(records DispatchRecords, boot string) (documents.Job, string, error) {
					if kind == "scan" {
						c, err := records.ClaimNextScanWithLimit(ctx, boot, 1)
						return c.Job, c.OwnerID, err
					}
					c, err := records.ClaimNextRender(ctx, boot)
					return c.Job, c.OwnerID, err
				}
				restore := f.FailCommit(committed)
				job, owner, err := invoke(f.Claims, f.Boot)
				restore()
				if !errors.Is(err, documents.ErrCommitUncertain) || job != (documents.Job{}) || owner != "" {
					t.Fatal("uncertain claim exposed executable work", job, owner, err)
				}
				durable, err := reader.ReadJob(ctx, f.Owner, f.Job)
				expected := "queued"
				if committed {
					expected = "running"
				}
				if err != nil || durable.State != expected || durable.Kind != kind {
					t.Fatal("wrong durable claim outcome", durable, err)
				}
				if !committed {
					job, owner, err = invoke(peer, f.Boot)
					if err != nil || job.ID != f.Job || job.DocumentID != f.Document || job.Kind != kind || owner != f.Owner {
						t.Fatal("rolled-back claim lost admitted work", job, owner, err)
					}
				}
				for _, boot := range []string{f.Boot, "worker_other_contract_001"} {
					for _, records := range []DispatchRecords{f.Claims, peer} {
						job, owner, err = invoke(records, boot)
						if !errors.Is(err, documents.ErrNoWork) || job != (documents.Job{}) || owner != "" {
							t.Fatal("running or uncertain claim redispatched", job, owner, err)
						}
					}
				}
				durable, err = reader.ReadJob(ctx, f.Owner, f.Job)
				if err != nil || durable.State != "running" {
					t.Fatal("claim attempts changed running work", durable, err)
				}
			})
		}
	}
}
