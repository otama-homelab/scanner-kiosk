package ocrplugin

import (
	"context"
	"encoding/json"
	"math"
	"testing"
)

func TestPositionedWritingDirection(t *testing.T) {
	input := InputPage{ID: "page", Width: 100, Height: 200}
	for _, direction := range []string{"", "horizontal", "vertical", "sideways", "Vertical"} {
		var page PositionedPage
		data := `{"page_id":"page","width":100,"height":200,"spans":[{"text":"日本語","box":{"x":10,"y":20,"width":30,"height":90},"writing_direction":"` + direction + `"}]}`
		if err := json.Unmarshal([]byte(data), &page); err != nil {
			t.Fatal(err)
		}
		wantValid := direction == "" || direction == "horizontal" || direction == "vertical"
		if (ValidatePositionedPage(page, input) == nil) != wantValid {
			t.Fatalf("writing direction %q validation mismatch", direction)
		}
		if wantValid && direction != "" {
			raw, err := json.Marshal(page)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err = json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if fields["spans"].([]any)[0].(map[string]any)["writing_direction"] != direction {
				t.Fatalf("writing direction %q lost", direction)
			}
		}
	}
}

type fixture struct {
	caps              Capabilities
	discoveries, runs int
}

func (f *fixture) Capabilities(context.Context) (Capabilities, error) {
	f.discoveries++
	return f.caps, nil
}
func (f *fixture) Run(context.Context, Request, Access) (Result, error) {
	f.runs++
	return Result{}, nil
}
func provider(id string, output OutputKind) *fixture {
	return &fixture{caps: Capabilities{APIVersion: 1, ID: id, Name: id, Version: "plugin-1", Models: []Model{{ID: "model", Name: "Model", Version: "pinned-1", Languages: []string{"ja", "en"}, QualityPresets: []string{"balanced"}, Outputs: []OutputKind{output}, MaxPages: 50, MaxPagePixels: 40_000_000}}}}
}
func request() Request {
	return Request{JobID: "job-fixture", InputRevision: 1, ModelID: "model", ModelVersion: "pinned-1", Languages: []string{"ja", "en"}, QualityPreset: "balanced", Output: SearchablePDF, Pages: []InputPage{{ID: "page-1", ArtifactID: "artifact-1", Width: 2480, Height: 3508, DPI: 300}}}
}
func TestAddProviderWithoutChangingRegistryOrRequest(t *testing.T) {
	tess, llm, extra := provider("tesseract", SearchablePDF), provider("llm", PositionedText), provider("new-plugin", SearchablePDF)
	registry, err := New(context.Background(), tess, llm, extra)
	if err != nil {
		t.Fatal(err)
	}
	caps := registry.Capabilities()
	for i, id := range []string{"tesseract", "llm", "new-plugin"} {
		if caps[i].ID != id {
			t.Fatal("capability order moved")
		}
	}
	chosen, err := registry.Select("new-plugin", request())
	if err != nil || chosen != extra {
		t.Fatal("extension was not selected", err)
	}
	if _, err = registry.Select("llm", request()); err == nil {
		t.Fatal("positioned text advertised as direct PDF")
	}
	positioned := request()
	positioned.Output = PositionedText
	if _, err = registry.Select("llm", positioned); err != nil {
		t.Fatal(err)
	}
}
func TestNoImplicitOCRFallback(t *testing.T) {
	selected, alternative := provider("tesseract", SearchablePDF), provider("alternative", Text)
	registry, err := New(context.Background(), selected, alternative)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := registry.Select("tesseract", request()); err != nil || got != selected {
		t.Fatal("exact provider not selected", err)
	}
	for _, name := range []string{"model", "version", "empty-version", "language", "duplicate-language", "preset", "output", "pixel-limit", "duplicate-page"} {
		t.Run(name, func(t *testing.T) {
			r := request()
			switch name {
			case "model":
				r.ModelID = "missing"
			case "version":
				r.ModelVersion = "new"
			case "empty-version":
				r.ModelVersion = ""
			case "language":
				r.Languages = []string{"fr"}
			case "duplicate-language":
				r.Languages = []string{"ja", "ja"}
			case "preset":
				r.QualityPreset = "accurate"
			case "output":
				r.Output = Text
			case "pixel-limit":
				r.Pages[0].Width = 100000
			case "duplicate-page":
				r.Pages = append(r.Pages, r.Pages[0])
			}
			if _, err := registry.Select("tesseract", r); err == nil {
				t.Fatal("unsupported selection accepted")
			}
		})
	}
	if _, err := registry.Select("unknown", request()); err == nil {
		t.Fatal("missing provider accepted")
	}
	for range 3 {
		registry.Capabilities()
	}
	for _, p := range []*fixture{selected, alternative} {
		if p.discoveries != 1 || p.runs != 0 {
			t.Fatalf("selection/metadata rediscovered or processed: discoveries=%d runs=%d", p.discoveries, p.runs)
		}
	}
}
func TestCapabilitySnapshotsAreIsolated(t *testing.T) {
	p := provider("tesseract", SearchablePDF)
	registry, err := New(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	p.caps.Models[0].Languages[0] = "fr"
	view := registry.Capabilities()
	view[0].Models[0].Version = "changed"
	view[0].Models[0].Languages[0] = "de"
	if _, err = registry.Select("tesseract", request()); err != nil {
		t.Fatal("capability state leaked", err)
	}
}
func TestRejectIncompatibleOrDuplicatePlugins(t *testing.T) {
	p := provider("one", SearchablePDF)
	p.caps.APIVersion = 2
	if _, err := New(context.Background(), p); err == nil {
		t.Fatal("incompatible API accepted")
	}
	if _, err := New(context.Background(), provider("same", Text), provider("same", Text)); err == nil {
		t.Fatal("duplicate provider accepted")
	}
}

func TestPositionedOutputCannotEscapeInputOrInventConfidence(t *testing.T) {
	input := InputPage{ID: "page", Width: 100, Height: 200, DPI: 300}
	valid := func() PositionedPage {
		return PositionedPage{PageID: "page", Width: 100, Height: 200, Spans: []Span{{Text: "日本語 English", Box: Box{X: 10, Y: 20, Width: 80, Height: 30}}}}
	}
	if err := ValidatePositionedPage(valid(), input); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"page-id", "dimensions", "outside", "negative", "nan", "confidence"} {
		t.Run(name, func(t *testing.T) {
			page := valid()
			switch name {
			case "page-id":
				page.PageID = "another"
			case "dimensions":
				page.Height = 201
			case "outside":
				page.Spans[0].Box.Width = 100
			case "negative":
				page.Spans[0].Box.X = -1
			case "nan":
				page.Spans[0].Box.Y = math.NaN()
			case "confidence":
				v := 2.0
				page.Spans[0].Confidence = &v
			}
			if ValidatePositionedPage(page, input) == nil {
				t.Fatal("invalid positioned text accepted")
			}
		})
	}
	empty := valid()
	empty.Spans = nil
	if err := ValidatePositionedPage(empty, input); err != nil {
		t.Fatal("blank page should allow zero spans", err)
	}
}

func TestPositionedBlankAndExactPageIdentity(t *testing.T) {
	input := InputPage{ID: "exact-source", Width: 80, Height: 100}
	blank := PositionedPage{PageID: input.ID, Width: input.Width, Height: input.Height, Spans: []Span{}}
	if err := ValidatePositionedPage(blank, input); err != nil {
		t.Fatal("explicit blank recognition rejected", err)
	}
	blank.PageID = "other-source"
	if err := ValidatePositionedPage(blank, input); err == nil {
		t.Fatal("even blank recognition must bind the actual source page")
	}
}

func TestTextLayerIsNotACompleteSearchableDocument(t *testing.T) {
	registry, err := New(context.Background(), provider("tesseract-layer", PDFTextLayer))
	if err != nil {
		t.Fatal(err)
	}
	r := request()
	if _, err = registry.Select("tesseract-layer", r); err == nil {
		t.Fatal("text layer treated as complete PDF")
	}
	r.Output = PDFTextLayer
	if _, err = registry.Select("tesseract-layer", r); err != nil {
		t.Fatal(err)
	}
}

func TestCapabilityCopyIsDetachedAndRejectsInvalidUTF8(t *testing.T) {
	source := Capabilities{APIVersion: APIVersion, ID: "plugin", Name: "Plugin", Version: "v1", Models: []Model{{ID: "model", Name: "Model", Version: "v2", Languages: []string{"jpn", "eng"}, Outputs: []OutputKind{PDFTextLayer}, MaxPages: 2, MaxPagePixels: 10000}}}
	copied, err := CopyCapabilities(source)
	if err != nil {
		t.Fatal(err)
	}
	source.Models[0].Languages[0] = "changed"
	if copied.Models[0].Languages[0] != "jpn" {
		t.Fatal("mutable snapshot alias")
	}
	source.ID = string([]byte{255})
	if _, err := CopyCapabilities(source); err == nil {
		t.Fatal("invalid UTF-8 metadata accepted")
	}
}

func TestExternalPDFModelsRequirePublicDestination(t *testing.T) {
	for _, kind := range []OutputKind{SearchablePDF, PDFTextLayer} {
		t.Run(string(kind), func(t *testing.T) {
			f := provider("external", kind)
			f.caps.ExternalProcessing = true
			if _, err := New(context.Background(), f); err == nil {
				t.Fatal("undisclosed external PDF model accepted")
			}
			f.caps.Models[0].ProcessingDestination = "Family OCR server"
			registry, err := New(context.Background(), f)
			if err != nil {
				t.Fatal(err)
			}
			f.caps.Models[0].ProcessingDestination = "changed after registration"
			if registry.Capabilities()[0].Models[0].ProcessingDestination != "Family OCR server" {
				t.Fatal("registered destination aliases provider metadata")
			}
			f.caps.Models[0].ProcessingDestination = "server\nprivate-value"
			if _, err := New(context.Background(), f); err == nil {
				t.Fatal("control characters accepted in public destination")
			}
		})
	}
	for _, kind := range []OutputKind{Text, PositionedText} {
		f := provider("external", kind)
		f.caps.ExternalProcessing = true
		if _, err := New(context.Background(), f); err != nil {
			t.Fatal("non-PDF provider compatibility changed", err)
		}
	}
	f := provider("local", PDFTextLayer)
	if _, err := New(context.Background(), f); err != nil {
		t.Fatal("local OCR requires an external destination", err)
	}
}
