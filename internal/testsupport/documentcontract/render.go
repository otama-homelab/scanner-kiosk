package documentcontract

import (
	"context"
	"errors"
	"testing"

	"scanner-kiosk/internal/documenthttp"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/renderadmission"
)

type RenderFixture struct {
	Fixture
	Renders  renderadmission.Records
	OpenPeer func() renderadmission.Records
	Reopen   func() (documenthttp.Records, renderadmission.Records)
	Limits   documents.AdmissionLimits
}

// RunRenderAdmission tests normalized replay/ownership/expiry via the portable
// service surface. It does not execute render jobs or inspect SQL reservations.
func RunRenderAdmission(t *testing.T, newFixture func(*testing.T) RenderFixture) {
	t.Helper()
	ctx := context.Background()
	request := func(f RenderFixture) documents.RenderAdmission {
		return documents.RenderAdmission{DocumentID: f.Document, Revision: 1, IdempotencyKey: "contract_render_001", Settings: documents.RenderSettings{Format: "pdf"}, ReserveBytes: 1 << 20}
	}
	t.Run("concurrent identical requests replay one admission", func(t *testing.T) {
		f := newFixture(t)
		peer := f.OpenPeer()
		r := request(f)
		type result struct {
			admission documents.Admission
			err       error
		}
		start := make(chan struct{})
		results := make(chan result, 8)
		for i := range 8 {
			records := f.Renders
			if i%2 == 1 {
				records = peer
			}
			go func() {
				<-start
				a, err := records.AdmitRender(ctx, f.Owner, r, f.Limits)
				results <- result{a, err}
			}()
		}
		close(start)
		var first documents.Admission
		fresh := 0
		for i := range 8 {
			got := <-results
			if got.err != nil {
				t.Errorf("concurrent render: %v", got.err)
				continue
			}
			if !got.admission.Replay {
				fresh++
			}
			if i == 0 {
				first = got.admission
			}
			if got.admission.JobID != first.JobID || got.admission.DocumentID != f.Document || got.admission.Revision != first.Revision {
				t.Errorf("receipt identity diverged: %+v vs %+v", got.admission, first)
			}
		}
		if fresh != 1 || first.JobID == "" {
			t.Fatal("expected one fresh admission", fresh, first)
		}
		job, err := f.Records.ReadJob(ctx, f.Owner, first.JobID)
		if err != nil || job.Kind != "render" || job.State != "queued" || job.InputRevision != first.Revision {
			t.Fatal("admitted job", job, err)
		}
	})
	t.Run("replay precedes changed policy", func(t *testing.T) {
		f := newFixture(t)
		r := request(f)
		foreign := r
		foreign.IdempotencyKey = "contract_render_foreign"
		if _, err := f.Renders.AdmitRender(ctx, f.OtherOwner, foreign, f.Limits); !errors.Is(err, documents.ErrUnavailable) {
			t.Fatal("foreign render", err)
		}
		first, err := f.Renders.AdmitRender(ctx, f.Owner, r, f.Limits)
		if err != nil || first.Replay || first.DocumentID != f.Document {
			t.Fatal("initial render", first, err)
		}
		job, err := f.Records.ReadJob(ctx, f.Owner, first.JobID)
		if err != nil || job.Kind != "render" || job.State != "queued" || job.InputRevision != first.Revision {
			t.Fatal("admitted job", job, err)
		}
		previous, found, err := f.Renders.FindRender(ctx, f.Owner, r)
		if err != nil || !found || !previous.Replay || previous.JobID != first.JobID {
			t.Fatal("receipt lookup", previous, found, err)
		}
		limited := f.Limits
		limited.UserMaxBytes = 1
		limited.GlobalMaxBytes = 1
		limited.JobMaxBytes = 1
		limited.MaxPages = 1
		r.ReserveBytes = -1
		r.Settings.PDFEncoding = "jpeg"
		replay, err := f.Renders.AdmitRender(ctx, f.Owner, r, limited)
		if err != nil || !replay.Replay || replay.JobID != first.JobID || replay.Revision != first.Revision {
			t.Fatal("replay depended on new policy/default spelling", replay, err)
		}
		r.Settings.PDFEncoding = "lossless"
		if _, err = f.Renders.AdmitRender(ctx, f.Owner, r, f.Limits); !errors.Is(err, documents.ErrIdempotencyConflict) {
			t.Fatal("changed render reused key", err)
		}
	})
	for _, mode := range []string{"expired", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			r := request(f)
			if _, err := f.Renders.AdmitRender(ctx, f.Owner, r, f.Limits); err != nil {
				t.Fatal(err)
			}
			expected := documents.ErrGone
			if mode == "expired" {
				f.Expire()
			} else {
				f.Disable()
				expected = documents.ErrUnavailable
			}
			if _, _, err := f.Renders.FindRender(ctx, f.Owner, r); !errors.Is(err, expected) {
				t.Fatal("revoked receipt lookup", err)
			}
			if _, err := f.Renders.AdmitRender(ctx, f.Owner, r, f.Limits); !errors.Is(err, expected) {
				t.Fatal("revoked render replay", err)
			}
		})
	}
}
