package runtime

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/pkg/ocrplugin"
)

type fixtureProvider struct {
	id        string
	languages []string
}

func (p fixtureProvider) Capabilities(context.Context) (ocrplugin.Capabilities, error) {
	return ocrplugin.Capabilities{ID: p.id, Name: p.id, APIVersion: ocrplugin.APIVersion, Version: "1", Models: []ocrplugin.Model{{ID: "model", Name: "Model", Version: "fixed", Languages: p.languages, QualityPresets: []string{"standard"}, Outputs: []ocrplugin.OutputKind{ocrplugin.Text}, MaxPages: 10, MaxPagePixels: 1000000}}}, nil
}
func (fixtureProvider) Run(context.Context, ocrplugin.Request, ocrplugin.Access) (ocrplugin.Result, error) {
	return ocrplugin.Result{}, errors.New("not part of startup")
}
func TestFactoryExtensionAndLifecycle(t *testing.T) {
	closed := []string{}
	factory := func(id string) Factory {
		return func(context.Context, any) (Instance, error) {
			return Instance{Provider: fixtureProvider{id: id, languages: []string{"ja", "en"}}, Close: func() { closed = append(closed, id) }}, nil
		}
	}
	settings := config.OCR{Plugins: []config.PluginSettings{{ID: "future-one"}, {ID: "future-two"}}, DefaultPlugin: "future-two", DefaultLanguages: []string{"ja", "en"}}
	r, err := Build(context.Background(), settings, map[string]Factory{"future-one": factory("future-one"), "future-two": factory("future-two"), "disabled": func(context.Context, any) (Instance, error) {
		t.Fatal("disabled factory called")
		return Instance{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defaults := r.Defaults()
	if defaults.PluginID != "future-two" || defaults.ModelID != "model" || defaults.ModelVersion != "fixed" {
		t.Fatal("incorrect defaults")
	}
	defaults.Languages[0] = "changed"
	if r.Defaults().Languages[0] != "ja" {
		t.Fatal("mutable defaults")
	}
	if caps := r.Registry.Capabilities(); caps[0].ID != "future-one" || caps[1].ID != "future-two" {
		t.Fatal("order changed")
	}
	r.Close()
	r.Close()
	if !reflect.DeepEqual(closed, []string{"future-two", "future-one"}) {
		t.Fatal("cleanup not reversed or repeated")
	}
}
func TestStartupFailureCleansAllCreatedProviders(t *testing.T) {
	for _, mode := range []string{"factory", "identity", "languages", "missing-default", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			closed := 0
			settings := config.OCR{Plugins: []config.PluginSettings{{ID: "one"}, {ID: "two"}}, DefaultPlugin: "one", DefaultLanguages: []string{"ja"}}
			factories := map[string]Factory{}
			for _, id := range []string{"one", "two"} {
				factories[id] = func(context.Context, any) (Instance, error) {
					instance := Instance{Provider: fixtureProvider{id: id, languages: []string{"en", "ja"}}, Close: func() { closed++ }}
					if id == "two" && mode == "factory" {
						return instance, errors.New("secret-canary")
					}
					if id == "two" && mode == "identity" {
						instance.Provider = fixtureProvider{id: "wrong", languages: []string{"en"}}
					}
					return instance, nil
				}
			}
			if mode == "languages" {
				settings.DefaultLanguages = []string{"fr"}
			}
			if mode == "missing-default" {
				settings.DefaultPlugin = "absent"
			}
			if mode == "duplicate" {
				settings.Plugins[1].ID = "one"
			}
			_, err := Build(context.Background(), settings, factories)
			expected := 2
			if mode == "duplicate" {
				expected = 1
			}
			if err == nil || strings.Contains(err.Error(), "secret-canary") || closed != expected {
				t.Fatalf("startup failure cleanup incorrect: %v (%d)", err, closed)
			}
		})
	}
}
func TestDisabledRuntimeAndCancelledStartup(t *testing.T) {
	r, err := Build(context.Background(), config.OCR{}, nil)
	if err != nil || len(r.Registry.Capabilities()) != 0 || r.Defaults().PluginID != "" {
		t.Fatal("disabled runtime failed")
	}
	r.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Build(ctx, config.OCR{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled startup accepted")
	}
}

func TestDisabledCapabilityDoesNotRun(t *testing.T) {
	built := 0
	factories := map[string]Factory{"optional": func(context.Context, any) (Instance, error) {
		built++
		t.Fatal("unconfigured factory constructed")
		return Instance{}, nil
	}}
	r, err := Build(context.Background(), config.OCR{}, factories)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for range 3 {
		if len(r.Registry.Capabilities()) != 0 {
			t.Fatal("disabled provider advertised")
		}
		if p, err := r.Registry.Select("optional", ocrplugin.Request{}); err == nil || p != nil {
			t.Fatal("disabled provider selected")
		}
	}
	if built != 0 {
		t.Fatal("metadata built a disabled provider")
	}
}

type outputProvider struct{ fixtureProvider }

func (outputProvider) Run(ctx context.Context, request ocrplugin.Request, access ocrplugin.Access) (ocrplugin.Result, error) {
	if _, err := access.OpenInput(ctx, "foreign_artifact_001"); err == nil {
		return ocrplugin.Result{}, errors.New("foreign input accessible")
	}
	input, err := access.OpenInput(ctx, request.Pages[0].ArtifactID)
	if err != nil {
		return ocrplugin.Result{}, err
	}
	defer input.Close()
	output, err := access.CreateOutput(ctx, request.Output)
	if err != nil {
		return ocrplugin.Result{}, err
	}
	if _, err = io.Copy(output, input); err != nil {
		return ocrplugin.Result{}, err
	}
	if err = output.Close(); err != nil {
		return ocrplugin.Result{}, err
	}
	return ocrplugin.Result{ModelVersion: request.ModelVersion, Pages: []ocrplugin.PageResult{{PageID: request.Pages[0].ID, ArtifactID: output.Reference(), Kind: request.Output}}}, nil
}

func TestProviderCannotPublish(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte("synthetic transcript")
	if err := os.WriteFile(filepath.Join(root, "source"), data, 0600); err != nil {
		t.Fatal(err)
	}
	scope, err := artifactscope.New(root, map[string]artifactscope.Input{"input_artifact_001": {RelativePath: "source", Size: int64(len(data)), SHA256: sha256.Sum256(data)}}, artifactscope.Limits{MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxTotalOutputBytes: 1024, MaxOutputs: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	registry, err := ocrplugin.New(ctx, outputProvider{fixtureProvider{id: "fixture", languages: []string{"ja"}}})
	if err != nil {
		t.Fatal(err)
	}
	request := ocrplugin.Request{JobID: "job_000000000001", InputRevision: 1, ModelID: "model", ModelVersion: "fixed", Languages: []string{"ja"}, QualityPreset: "standard", Output: ocrplugin.Text, Pages: []ocrplugin.InputPage{{ID: "page_00000000001", ArtifactID: "input_artifact_001", Width: 100, Height: 100, DPI: 300}}}
	provider, err := registry.Select("fixture", request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Run(ctx, request, scope)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := scope.Seal(ctx)
	if err != nil || len(artifacts) != 1 || result.Pages[0].ArtifactID != artifacts[0].ID {
		t.Fatal("provider result did not bind scoped output", result, artifacts, err)
	}
	outputPath := filepath.Join(root, artifacts[0].RelativePath)
	if got, err := os.ReadFile(outputPath); err != nil || string(got) != string(data) {
		t.Fatal("scoped output bytes missing", err)
	}
	// Completion cannot release staging; application publication must succeed first.
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatal("provider output survived aborted application publication", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "source")); err != nil || string(got) != string(data) {
		t.Fatal("abort changed original", err)
	}
}
