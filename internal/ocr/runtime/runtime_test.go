package runtime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

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
