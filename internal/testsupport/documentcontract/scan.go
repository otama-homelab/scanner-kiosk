package documentcontract

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"scanner-kiosk/internal/documenthttp"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanadmission"
)

// ScanFixture starts with one live, idle, registered device. Withdraw removes
// actual readiness authority; it must not replace admission with a fake error.
type ScanFixture struct {
	Fixture
	Scans scanadmission.Records
	// OpenPeer connects to the same authority through independent DB handles.
	OpenPeer func() scanadmission.Records
	Reopen   func() (documenthttp.Records, scanadmission.Records)
	Request  documents.ScanRequest
	Limits   documents.ScanLimits
	Withdraw func()
}

// RunScanAdmission covers public admission and receipt semantics.
// Physical fencing, worker execution and uncertain commits need separate tests.
func RunScanAdmission(t *testing.T, newFixture func(*testing.T) ScanFixture) {
	t.Helper()
	ctx := context.Background()
	for _, competingOwners := range []bool{false, true} {
		name := "concurrent identical requests replay one admission"
		if competingOwners {
			name = "competing owners admit one writer"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			peer := f.OpenPeer()
			type result struct {
				owner     string
				admission documents.ScanAdmission
				err       error
			}
			start := make(chan struct{})
			results := make(chan result, 8)
			for i := range 8 {
				owner := f.Owner
				records := f.Scans
				if i%2 == 1 {
					records = peer
				}
				if competingOwners && i%2 == 1 {
					owner = f.OtherOwner
				}
				go func() {
					<-start
					a, err := records.AdmitReadyScan(ctx, owner, f.Request, f.Limits)
					results <- result{owner, a, err}
				}()
			}
			close(start)
			var all []result
			for range 8 {
				all = append(all, <-results)
			}
			var winner result
			fresh := 0
			for _, r := range all {
				if r.err == nil && !r.admission.Replay {
					fresh++
					winner = r
				}
			}
			if fresh != 1 || winner.admission.JobID == "" || winner.admission.DocumentID == "" {
				t.Fatal("concurrent requests did not produce exactly one admission", all)
			}
			for _, r := range all {
				if r.owner != winner.owner {
					if !errors.Is(r.err, documents.ErrDeviceBusy) || r.admission.JobID != "" || r.admission.DocumentID != "" {
						t.Fatal("competing owner acquired writer or receipt", r)
					}
					continue
				}
				if r.err != nil || r.admission.JobID != winner.admission.JobID || r.admission.DocumentID != winner.admission.DocumentID || r.admission.LeaseEpoch != winner.admission.LeaseEpoch || r.admission.Revision != winner.admission.Revision {
					t.Fatal("identical concurrent request diverged", r, winner)
				}
			}
		})
	}
	t.Run("next page preserves published pages and original retention", func(t *testing.T) {
		f := newFixture(t)
		before, err := f.Records.ReadDocument(ctx, f.Owner, f.Document)
		if err != nil || before.Revision != 1 || before.PageCount != 2 {
			t.Fatal("invalid next-page fixture", before, err)
		}
		request := f.Request
		request.DocumentID, request.Revision, request.Purpose = f.Document, before.Revision, ""
		if _, err := f.Scans.AdmitReadyScan(ctx, f.OtherOwner, request, f.Limits); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("foreign next page", err)
		}
		stale := request
		stale.Revision++
		if _, err := f.Scans.AdmitReadyScan(ctx, f.Owner, stale, f.Limits); !errors.Is(err, documents.ErrRevision) {
			t.Fatal("stale next-page revision", err)
		}
		first, err := f.Scans.AdmitReadyScan(ctx, f.Owner, request, f.Limits)
		if err != nil || first.Replay || first.DocumentID != f.Document || first.Revision != before.Revision+1 {
			t.Fatal("next page admission", first, err)
		}
		after, err := f.Records.ReadDocument(ctx, f.Owner, f.Document)
		if err != nil || after.State != "scanning" || after.Revision != first.Revision || after.PageCount != before.PageCount || !reflect.DeepEqual(after.Pages, before.Pages) || !after.CreatedAt.Equal(before.CreatedAt) || !after.ExpiresAt.Equal(before.ExpiresAt) {
			t.Fatal("next page changed retained document", before, after, err)
		}
		f.Withdraw()
		replay, err := f.Scans.AdmitReadyScan(ctx, f.Owner, request, f.Limits)
		if err != nil || !replay.Replay || replay.JobID != first.JobID || replay.DocumentID != f.Document || replay.Revision != first.Revision || replay.LeaseEpoch != first.LeaseEpoch {
			t.Fatal("next-page receipt depended on updated revision/readiness", replay, err)
		}
	})
	t.Run("replay survives withdrawn readiness and changed quota", func(t *testing.T) {
		f := newFixture(t)
		first, err := f.Scans.AdmitReadyScan(ctx, f.Owner, f.Request, f.Limits)
		if err != nil || first.Replay || first.JobID == "" || first.DocumentID == "" || first.Revision != 1 {
			t.Fatal("initial scan", first, err)
		}
		job, err := f.Records.ReadJob(ctx, f.Owner, first.JobID)
		if err != nil || job.Kind != "scan" || job.State != "queued" {
			t.Fatal("admitted scan job", job, err)
		}
		if _, err := f.Records.ReadJob(ctx, f.OtherOwner, first.JobID); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("foreign scan job", err)
		}
		if _, err := f.Records.ReadDocument(ctx, f.OtherOwner, first.DocumentID); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("foreign scan document", err)
		}
		f.Withdraw()
		limits := f.Limits
		limits.UserMaxBytes, limits.GlobalMaxBytes, limits.JobMaxBytes = 1, 1, 1
		request := f.Request
		request.ReserveBytes = -1
		replay, err := f.Scans.AdmitReadyScan(ctx, f.Owner, request, limits)
		if err != nil || !replay.Replay || replay.JobID != first.JobID || replay.DocumentID != first.DocumentID || replay.Revision != first.Revision || replay.LeaseEpoch != first.LeaseEpoch {
			t.Fatal("receipt depended on live readiness or fresh quota", replay, err)
		}
		request.Capture.DPI = 600
		if _, err := f.Scans.AdmitReadyScan(ctx, f.Owner, request, f.Limits); !errors.Is(err, documents.ErrIdempotencyConflict) {
			t.Fatal("changed scan intent reused key", err)
		}
		request = f.Request
		request.IdempotencyKey = "contract_scan_nextkey"
		if _, err := f.Scans.AdmitReadyScan(ctx, f.Owner, request, f.Limits); !errors.Is(err, documents.ErrDeviceBusy) {
			t.Fatal("fresh scan bypassed withdrawn authority", err)
		}
	})
	for _, mode := range []string{"expired", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			first, err := f.Scans.AdmitReadyScan(ctx, f.Owner, f.Request, f.Limits)
			if err != nil {
				t.Fatal(err)
			}
			expected := documents.ErrGone
			if mode == "expired" {
				f.Expire()
			} else {
				f.Disable()
				expected = documents.ErrUnavailable
			}
			if _, err := f.Scans.AdmitReadyScan(ctx, f.Owner, f.Request, f.Limits); !errors.Is(err, expected) {
				t.Fatal("revoked scan replay", err)
			}
			if _, err := f.Records.ReadJob(ctx, f.Owner, first.JobID); !errors.Is(err, documents.ErrUnavailable) {
				t.Fatal("revoked scan read", err)
			}
		})
	}
}
