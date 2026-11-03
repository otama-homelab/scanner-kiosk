//go:build linux

package documents

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"scanner-kiosk/internal/diskspace"
)

func TestLiveMeterWiringAndExistingExportSurvivesProbeFailure(t *testing.T) {
	f := renderFixture(t)
	meter, err := diskspace.Open(filepath.Dir(f.docsPath), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer meter.Close()
	f.store.space = meter.Available
	first, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimExport(context.Background(), alice, first.JobID, runner); err != nil {
		t.Fatal(err)
	}
	if err = f.store.PublishExport(context.Background(), alice, first.JobID, runner, first.Revision, output()); err != nil {
		t.Fatal(err)
	}
	meter.Close()
	request := admissionRequest()
	request.Revision = first.Revision
	request.IdempotencyKey = "request_render_0002"
	if _, err = f.store.AdmitRender(context.Background(), alice, request, admissionLimits()); !errors.Is(err, ErrStorage) {
		t.Fatal("unavailable meter allowed new work")
	}
	got, err := f.store.GetExport(context.Background(), alice, docA)
	if err != nil || got.ID != output().ID {
		t.Fatal("capacity failure denied or removed existing export")
	}
}
