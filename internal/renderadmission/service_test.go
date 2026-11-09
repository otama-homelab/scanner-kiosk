package renderadmission

import (
	"context"
	"errors"
	"scanner-kiosk/internal/documents"
	"testing"
	"time"
)

type records struct {
	previous documents.Admission
	found    bool
	admitted int
	request  documents.RenderAdmission
	limits   documents.AdmissionLimits
	findHook func(documents.RenderAdmission)
}

func (r *records) FindRender(_ context.Context, _ string, request documents.RenderAdmission) (documents.Admission, bool, error) {
	if r.findHook != nil {
		r.findHook(request)
	}
	return r.previous, r.found, nil
}
func (r *records) AdmitRender(_ context.Context, _ string, request documents.RenderAdmission, limits documents.AdmissionLimits) (documents.Admission, error) {
	r.admitted++
	r.request = request
	r.limits = limits
	return documents.Admission{JobID: "render_job_0000001"}, nil
}

type resolver struct {
	called int
	fail   bool
}

func (r *resolver) ResolveOCR(context.Context, documents.RenderOCR, bool) (ResolvedOCR, error) {
	r.called++
	if r.fail {
		return ResolvedOCR{}, errors.New("private provider error")
	}
	return ResolvedOCR{ModelVersion: "pinned-model-version", QualityPreset: "standard", MaxPages: 50, MaxPagePixels: 40000000}, nil
}
func TestReplayDoesNotResolveChangedProvider(t *testing.T) {
	record := &records{previous: documents.Admission{JobID: "original_job_00001", Replay: true}, found: true}
	provider := &resolver{fail: true}
	s := Service{Records: record, Resolver: provider}
	result, err := s.Admit(context.Background(), "owner", documents.RenderAdmission{Settings: documents.RenderSettings{OCR: documents.RenderOCR{Enabled: true}}})
	if err != nil || result.JobID != "original_job_00001" || provider.called != 0 || record.admitted != 0 {
		t.Fatal(result, err)
	}
}

func TestReOCRAdmissionReplayBeforeCatalog(t *testing.T) {
	for _, snapshot := range []CatalogSnapshot{{}, {PublishedAtMS: 1000, ExpiresAtMS: 2000}} {
		record := &records{previous: documents.Admission{JobID: "original_job_00001", Replay: true}, found: true}
		service := Service{Records: record, Resolver: &CatalogResolver{Source: catalogSource{snapshot: snapshot}, Now: func() time.Time { return time.UnixMilli(3000) }}}
		request := documents.RenderAdmission{ReOCR: true, Settings: documents.RenderSettings{Format: "pdf", OCR: selection()}}
		result, err := service.Admit(context.Background(), "owner", request)
		if err != nil || !result.Replay || result.JobID != "original_job_00001" || record.admitted != 0 {
			t.Fatal("changed catalog prevented replay", result, err)
		}
		record.found = false
		if _, err := service.Admit(context.Background(), "owner", request); !errors.Is(err, ErrProviderUnavailable) || record.admitted != 0 {
			t.Fatal("unavailable new work admitted", err)
		}
	}
	for _, request := range []documents.RenderAdmission{
		{ReOCR: true, Settings: documents.RenderSettings{Format: "pdf"}},
		{ReOCR: true, Settings: documents.RenderSettings{Format: "png", OCR: selection()}},
	} {
		service := Service{Records: &records{found: true}}
		if _, err := service.Admit(context.Background(), "owner", request); !errors.Is(err, documents.ErrInvalid) {
			t.Fatal("invalid reOCR intent replayed", err)
		}
	}
}
func TestAdmissionUsesPolicyReservationAndPinnedOCR(t *testing.T) {
	record := &records{}
	provider := &resolver{}
	s := Service{Records: record, Resolver: provider, Limits: documents.AdmissionLimits{JobMaxBytes: 12345}}
	request := documents.RenderAdmission{Settings: documents.RenderSettings{Format: "pdf", OCR: documents.RenderOCR{Enabled: true, ProviderID: "plugin", ModelID: "model", Languages: []string{"jpn", "eng"}}}}
	if _, err := s.Admit(context.Background(), "owner", request); err != nil {
		t.Fatal(err)
	}
	if record.limits.OCRMaxPages != 50 || record.limits.OCRMaxPagePixels != 40000000 || record.request.ReserveBytes != 12345 || record.request.Settings.OCR.ModelVersion != "pinned-model-version" || record.request.ResolvedQualityPreset != "standard" {
		t.Fatal("untrusted policy or unpinned OCR", record.request)
	}
	request.Settings.OCR.ModelVersion = "client-version"
	if _, err := s.Admit(context.Background(), "owner", request); !errors.Is(err, documents.ErrInvalid) {
		t.Fatal("client-pinned version accepted", err)
	}
}
func TestUnavailableOCRDoesNotFallBackOrAdmit(t *testing.T) {
	record := &records{}
	s := Service{Records: record}
	request := documents.RenderAdmission{Settings: documents.RenderSettings{OCR: documents.RenderOCR{Enabled: true}}}
	if _, err := s.Admit(context.Background(), "owner", request); !errors.Is(err, ErrProviderUnavailable) || record.admitted != 0 {
		t.Fatal("unavailable OCR admitted", err)
	}
}

func TestAdmissionRetainsSmallerDeploymentPixelLimit(t *testing.T) {
	record := &records{}
	service := Service{Records: record, Resolver: &resolver{}, Limits: documents.AdmissionLimits{JobMaxBytes: 100, OCRMaxPagePixels: 512}}
	if _, err := service.Admit(context.Background(), "owner", documents.RenderAdmission{Settings: documents.RenderSettings{Format: "pdf", OCR: documents.RenderOCR{Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	if record.limits.OCRMaxPagePixels != 512 {
		t.Fatal("deployment bound widened", record.limits)
	}
}

type resolverFunc func(context.Context, documents.RenderOCR, bool) (ResolvedOCR, error)

func (f resolverFunc) ResolveOCR(ctx context.Context, selection documents.RenderOCR, reOCR bool) (ResolvedOCR, error) {
	return f(ctx, selection, reOCR)
}

func TestAdmissionFreezesLanguagesAcrossCallerRecordsAndResolver(t *testing.T) {
	languages := []string{"eng"}
	record := &records{findHook: func(request documents.RenderAdmission) {
		request.Settings.OCR.Languages[0] = "records_mutation"
		languages[0] = "caller_mutation"
	}}
	provider := resolverFunc(func(_ context.Context, selection documents.RenderOCR, _ bool) (ResolvedOCR, error) {
		if len(selection.Languages) != 1 || selection.Languages[0] != "eng" {
			t.Fatal("selection changed after lookup", selection)
		}
		selection.Languages[0] = "resolver_mutation"
		return ResolvedOCR{ModelVersion: "version", QualityPreset: "standard", MaxPages: 10, MaxPagePixels: 10000}, nil
	})
	service := Service{Records: record, Resolver: provider, Limits: documents.AdmissionLimits{JobMaxBytes: 100}}
	request := documents.RenderAdmission{Settings: documents.RenderSettings{Format: "pdf", OCR: documents.RenderOCR{Enabled: true, ProviderID: "plugin", ModelID: "model", Languages: languages}}}
	if _, err := service.Admit(context.Background(), "owner", request); err != nil {
		t.Fatal(err)
	}
	if record.admitted != 1 || record.request.Settings.OCR.Languages[0] != "eng" || languages[0] != "caller_mutation" {
		t.Fatal("lookup/resolver changed the admitted client intent", record.request, languages)
	}
}
