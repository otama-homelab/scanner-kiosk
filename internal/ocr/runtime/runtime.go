// Package runtime composes configured OCR providers at the worker boundary.
// Document services depend on the OCR port, never on these concrete factories.
package runtime

import (
	"context"
	"errors"
	"sync"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/pkg/ocrplugin"
)

type Instance struct {
	Provider ocrplugin.Provider
	Close    func()
}
type Factory func(context.Context, any) (Instance, error)
type Defaults struct {
	PluginID, ModelID, ModelVersion, QualityPreset string
	Languages                                      []string
}
type Runtime struct {
	Registry *ocrplugin.Registry
	defaults Defaults
	close    func()
	once     sync.Once
}

func (r *Runtime) Defaults() Defaults {
	out := r.defaults
	out.Languages = append([]string(nil), out.Languages...)
	return out
}

// Close releases provider clients after the worker has stopped owned jobs.
func (r *Runtime) Close() { r.once.Do(r.close) }

// Build constructs enabled providers only. Optional OCR initialization may obtain
// bounded remote metadata, but must never send document data or perform recognition.
// Failed construction closes prior instances and returns a payload-free error.
func Build(ctx context.Context, settings config.OCR, factories map[string]Factory) (*Runtime, error) {
	closers := []func(){}
	cleanup := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}
	fail := func() (*Runtime, error) { cleanup(); return nil, errors.New("OCR runtime configuration unavailable") }
	providers := []ocrplugin.Provider{}
	seen := map[string]bool{}
	for _, selected := range settings.Plugins {
		if ctx.Err() != nil {
			cleanup()
			return nil, ctx.Err()
		}
		factory := factories[selected.ID]
		if factory == nil || seen[selected.ID] {
			return fail()
		}
		seen[selected.ID] = true
		instance, err := factory(ctx, selected.Settings)
		if instance.Close != nil {
			closers = append(closers, instance.Close)
		}
		if err != nil || instance.Provider == nil {
			return fail()
		}
		caps, err := instance.Provider.Capabilities(ctx)
		if err != nil || caps.ID != selected.ID {
			return fail()
		}
		providers = append(providers, instance.Provider)
	}
	if ctx.Err() != nil {
		cleanup()
		return nil, ctx.Err()
	}
	registry, err := ocrplugin.New(ctx, providers...)
	if err != nil {
		return fail()
	}
	out := &Runtime{Registry: registry, close: cleanup}
	if len(settings.Plugins) == 0 {
		if settings.DefaultPlugin != "" {
			return fail()
		}
		return out, nil
	}
	if len(settings.DefaultLanguages) == 0 {
		return fail()
	}
	languages := map[string]bool{}
	for _, language := range settings.DefaultLanguages {
		if language == "" || languages[language] {
			return fail()
		}
		languages[language] = true
	}
	for _, provider := range registry.Capabilities() {
		if provider.ID != settings.DefaultPlugin {
			continue
		}
		for _, model := range provider.Models {
			supported := map[string]bool{}
			for _, lang := range model.Languages {
				supported[lang] = true
			}
			match := true
			for _, language := range settings.DefaultLanguages {
				match = match && supported[language]
			}
			if !match {
				continue
			}
			out.defaults = Defaults{PluginID: provider.ID, ModelID: model.ID, ModelVersion: model.Version, Languages: append([]string(nil), settings.DefaultLanguages...)}
			if len(model.QualityPresets) > 0 {
				out.defaults.QualityPreset = model.QualityPresets[0]
			}
			return out, nil
		}
	}
	return fail()
}
