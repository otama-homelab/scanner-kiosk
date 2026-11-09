package renderadmission

import (
	"context"
	"errors"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/pkg/ocrplugin"
	"testing"
	"time"
)

type catalogSource struct {
	snapshot CatalogSnapshot
	err      error
}

func (s catalogSource) ReadOCRCatalog(context.Context) (CatalogSnapshot, error) {
	return s.snapshot, s.err
}
func catalogFixture() CatalogSnapshot {
	return CatalogSnapshot{PublishedAtMS: 1000, ExpiresAtMS: 10000, Providers: []ocrplugin.Capabilities{{APIVersion: ocrplugin.APIVersion, ID: "custom", Name: "Custom OCR", Version: "plugin-v1", Models: []ocrplugin.Model{{ID: "model", Name: "Model", Version: "model-v2", Languages: []string{"jpn", "eng"}, QualityPresets: []string{"fast", "standard"}, Outputs: []ocrplugin.OutputKind{ocrplugin.PDFTextLayer}, MaxPages: 50, MaxPagePixels: 40000000}}}}, Defaults: []ModelDefault{{ProviderID: "custom", ModelID: "model", QualityPreset: "standard"}}}
}
func selection() documents.RenderOCR {
	return documents.RenderOCR{Enabled: true, ProviderID: "custom", ModelID: "model", Languages: []string{"jpn", "eng"}}
}
func TestCatalogPinsExactVersionAndExplicitDefault(t *testing.T) {
	source := catalogFixture()
	resolver := CatalogResolver{Source: catalogSource{snapshot: source}, Now: func() time.Time { return time.UnixMilli(2000) }}
	result, err := resolver.ResolveOCR(context.Background(), selection(), false)
	if err != nil || result.ModelVersion != "model-v2" || result.QualityPreset != "standard" || result.Output != ocrplugin.PDFTextLayer {
		t.Fatal(result, err)
	}
	request := selection()
	request.QualityPreset = "fast"
	result, err = resolver.ResolveOCR(context.Background(), request, false)
	if err != nil || result.QualityPreset != "fast" {
		t.Fatal("explicit preset lost", result, err)
	}
	t.Run("no substitution or probing", func(t *testing.T) {
		for _, mode := range []string{"provider", "model", "version", "output", "unavailable"} {
			t.Run(mode, func(t *testing.T) {
				snapshot := catalogFixture()
				alternative := catalogFixture().Providers[0]
				alternative.ID = "alternative"
				snapshot.Providers = append(snapshot.Providers, alternative)
				request := selection()
				want := ErrUnsupported
				source := &countedCatalogSource{snapshot: snapshot}
				switch mode {
				case "provider":
					request.ProviderID = "absent"
				case "model":
					request.ModelID = "absent"
				case "version":
					request.ModelVersion = "browser-pin"
				case "output":
					snapshot.Providers[0].Models[0].Outputs = []ocrplugin.OutputKind{ocrplugin.Text}
				case "unavailable":
					source.err = errors.New("private endpoint credential")
					want = ErrProviderUnavailable
				}
				source.snapshot = snapshot
				resolver := CatalogResolver{Source: source, Now: func() time.Time { return time.UnixMilli(2000) }}
				if _, err := resolver.ResolveOCR(context.Background(), request, false); !errors.Is(err, want) || source.reads != 1 {
					t.Fatal("selection substituted or retried", err, source.reads)
				}
			})
		}
	})
	t.Run("plain save and replay ignore optional outage", func(t *testing.T) {
		source := &countedCatalogSource{err: errors.New("optional provider offline")}
		record := &records{}
		service := Service{Records: record, Resolver: &CatalogResolver{Source: source, Now: time.Now}}
		request := documents.RenderAdmission{Settings: documents.RenderSettings{Format: "pdf"}}
		first, err := service.Admit(context.Background(), "owner", request)
		if err != nil || record.admitted != 1 || record.request.Settings.OCR.Enabled || source.reads != 0 {
			t.Fatal("plain save depended on OCR", err)
		}
		record.found, record.previous = true, first
		again, err := service.Admit(context.Background(), "owner", request)
		if err != nil || again.JobID != first.JobID || record.admitted != 1 || source.reads != 0 {
			t.Fatal("plain save replay depended on OCR", err)
		}
	})
}

type countedCatalogSource struct {
	snapshot CatalogSnapshot
	err      error
	reads    int
}

func (s *countedCatalogSource) ReadOCRCatalog(context.Context) (CatalogSnapshot, error) {
	s.reads++
	return s.snapshot, s.err
}

func TestReOCRCatalogPinsOutput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outputs []ocrplugin.OutputKind
		want    ocrplugin.OutputKind
	}{
		{"positioned", []ocrplugin.OutputKind{ocrplugin.PDFTextLayer, ocrplugin.Text, ocrplugin.PositionedText}, ocrplugin.PositionedText},
		{"text-only", []ocrplugin.OutputKind{ocrplugin.Text}, ocrplugin.Text},
		{"layer-only", []ocrplugin.OutputKind{ocrplugin.PDFTextLayer}, ocrplugin.PDFTextLayer},
		{"searchable-only", []ocrplugin.OutputKind{ocrplugin.SearchablePDF}, ocrplugin.SearchablePDF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := catalogFixture()
			snapshot.Providers[0].Models[0].Outputs = tc.outputs
			resolver := &CatalogResolver{Source: catalogSource{snapshot: snapshot}, Now: func() time.Time { return time.UnixMilli(2000) }}
			resolved, err := resolver.ResolveOCR(context.Background(), selection(), true)
			if err != nil || resolved.Output != tc.want || resolved.ModelVersion != "model-v2" || resolved.QualityPreset != "standard" || resolved.MaxPages != 50 || resolved.MaxPagePixels != 40000000 {
				t.Fatal("selection changed model/settings/bounds", resolved, err)
			}
			record := &records{}
			service := Service{Records: record, Resolver: resolver, Limits: documents.AdmissionLimits{JobMaxBytes: 100, OCRMaxPagePixels: 200}}
			request := documents.RenderAdmission{ReOCR: true, Settings: documents.RenderSettings{Format: "pdf", OCR: selection()}}
			if _, err := service.Admit(context.Background(), "owner", request); err != nil || record.request.ResolvedOutput != tc.want || record.request.Settings.OCR.ModelVersion != "model-v2" || record.limits.OCRMaxPagePixels != 200 {
				t.Fatal("trusted output not carried to admission", record.request, err)
			}
			request.ResolvedOutput = tc.want
			if _, err := service.Admit(context.Background(), "owner", request); !errors.Is(err, documents.ErrInvalid) {
				t.Fatal("caller-selected output admitted", err)
			}
		})
	}
	for _, mode := range []string{"expired", "withdrawn", "unsupported-language", "incompatible"} {
		t.Run(mode, func(t *testing.T) {
			snapshot := catalogFixture()
			request := selection()
			want := ErrUnsupported
			switch mode {
			case "expired":
				snapshot.ExpiresAtMS = 2000
				want = ErrProviderUnavailable
			case "withdrawn":
				snapshot.Providers = nil
				snapshot.Defaults = nil
			case "unsupported-language":
				request.Languages = []string{"unknown"}
			case "incompatible":
				snapshot.Providers[0].Models[0].Outputs = []ocrplugin.OutputKind{"invalid"}
				want = ErrProviderUnavailable
			}
			resolver := &CatalogResolver{Source: catalogSource{snapshot: snapshot}, Now: func() time.Time { return time.UnixMilli(2000) }}
			if _, err := resolver.ResolveOCR(context.Background(), request, true); !errors.Is(err, want) {
				t.Fatal("incompatible choice substituted", err)
			}
		})
	}
}
func TestCatalogRejectsStaleUnknownAndIncompatibleSelections(t *testing.T) {
	for _, name := range []string{"expired", "future", "too-long", "duplicate-provider", "bad-default", "missing-default", "language", "duplicate-language", "model", "preset", "renderer", "server-version", "malformed"} {
		t.Run(name, func(t *testing.T) {
			snapshot := catalogFixture()
			request := selection()
			expected := ErrProviderUnavailable
			switch name {
			case "expired":
				snapshot.ExpiresAtMS = 2000
			case "future":
				snapshot.PublishedAtMS = 3000
			case "too-long":
				snapshot.ExpiresAtMS = 400000
			case "duplicate-provider":
				snapshot.Providers = append(snapshot.Providers, snapshot.Providers[0])
			case "bad-default":
				snapshot.Defaults[0].QualityPreset = "unknown"
			case "missing-default":
				snapshot.Defaults = nil
			case "language":
				request.Languages = []string{"fra"}
				expected = ErrUnsupported
			case "duplicate-language":
				request.Languages = []string{"eng", "eng"}
				expected = ErrUnsupported
			case "model":
				request.ModelID = "different"
				expected = ErrUnsupported
			case "preset":
				request.QualityPreset = "unknown"
				expected = ErrUnsupported
			case "renderer":
				snapshot.Providers[0].Models[0].Outputs = []ocrplugin.OutputKind{ocrplugin.PositionedText}
				expected = ErrUnsupported
			case "server-version":
				request.ModelVersion = "browser-version"
				expected = ErrUnsupported
			case "malformed":
				snapshot.Providers[0].Models[0].Languages = append(snapshot.Providers[0].Models[0].Languages, "eng")
			}
			resolver := CatalogResolver{Source: catalogSource{snapshot: snapshot}, Now: func() time.Time { return time.UnixMilli(2000) }}
			if _, err := resolver.ResolveOCR(context.Background(), request, false); !errors.Is(err, expected) {
				t.Fatal(name, err)
			}
		})
	}
}
func TestCatalogUnavailableAndExpiryDuringResolution(t *testing.T) {
	now := 1000
	resolver := CatalogResolver{Source: catalogSource{snapshot: catalogFixture()}, Now: func() time.Time { now += 5000; return time.UnixMilli(int64(now)) }}
	if _, err := resolver.ResolveOCR(context.Background(), selection(), false); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatal("expired resolution returned authority", err)
	}
	resolver.Source = catalogSource{err: errors.New("private connection detail")}
	if _, err := resolver.ResolveOCR(context.Background(), selection(), false); err != ErrProviderUnavailable {
		t.Fatal("source error escaped", err)
	}
}
func TestServicePreservesUnsupportedSelectionWithoutAdmission(t *testing.T) {
	record := &records{}
	resolver := &CatalogResolver{Source: catalogSource{snapshot: catalogFixture()}, Now: func() time.Time { return time.UnixMilli(2000) }}
	service := Service{Records: record, Resolver: resolver}
	request := documents.RenderAdmission{Settings: documents.RenderSettings{Format: "pdf", OCR: selection()}}
	request.Settings.OCR.ModelID = "unknown"
	if _, err := service.Admit(context.Background(), "owner", request); !errors.Is(err, ErrUnsupported) || record.admitted != 0 {
		t.Fatal("unsupported choice admitted/fell back", err)
	}
}

func TestCatalogModelWithoutPresetAcceptsEmptyDefault(t *testing.T) {
	snapshot := catalogFixture()
	snapshot.Providers[0].Models[0].QualityPresets = nil
	snapshot.Defaults[0].QualityPreset = ""
	resolver := CatalogResolver{Source: catalogSource{snapshot: snapshot}, Now: func() time.Time { return time.UnixMilli(2000) }}
	resolved, err := resolver.ResolveOCR(context.Background(), selection(), false)
	if err != nil || resolved.QualityPreset != "" {
		t.Fatal(resolved, err)
	}
}

func TestCatalogValidatesConfiguredPreference(t *testing.T) {
	for _, mode := range []string{"valid", "unknown-model", "unsupported-language", "duplicate-language", "unsupported-preset"} {
		t.Run(mode, func(t *testing.T) {
			snapshot := catalogFixture()
			snapshot.Preferred = &CatalogSelection{ProviderID: "custom", ModelID: "model", Languages: []string{"jpn", "eng"}, QualityPreset: "standard"}
			switch mode {
			case "unknown-model":
				snapshot.Preferred.ModelID = "unknown"
			case "unsupported-language":
				snapshot.Preferred.Languages = []string{"fra"}
			case "duplicate-language":
				snapshot.Preferred.Languages = []string{"eng", "eng"}
			case "unsupported-preset":
				snapshot.Preferred.QualityPreset = "unknown"
			}
			err := ValidateCatalogSnapshot(snapshot, time.UnixMilli(2000))
			if (err == nil) != (mode == "valid") {
				t.Fatal(mode, err)
			}
		})
	}
}
