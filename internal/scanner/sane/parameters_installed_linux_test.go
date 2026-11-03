//go:build linux && saneparams

package sane

import (
	"context"
	"errors"
	"os"
	"testing"
)

func parameterAdapter(t *testing.T, configuration string) (*Adapter, Options) {
	t.Helper()
	_, options := installed(t, configuration)
	options.ParametersBinary = os.Getenv("SCANNER_TEST_SANE_PARAMETERS")
	if options.ParametersBinary == "" {
		t.Skip("requires compiled native parameter helper")
	}
	adapter, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return adapter, options
}
func TestInstalledParametersWithoutAcquisition(t *testing.T) {
	for _, selection := range []Selection{{Size: "a4", Color: "gray", DPI: 300}, {Size: "photo10x15", Color: "color", DPI: 600}} {
		t.Run(selection.Color, func(t *testing.T) {
			adapter, options := parameterAdapter(t, "geometry_max 300\nread-status-code SANE_STATUS_DEVICE_BUSY\n")
			params, attempt, err := adapter.ProbeParameters(context.Background(), selection)
			if attempt != nil {
				defer attempt.Close()
			}
			if err != nil || params.Width <= 0 || params.Height <= 0 || attempt == nil || attempt.StoppedAtMS() <= 0 {
				t.Fatal("native parameter read failed", err)
			}
			if params.Color != selection.Color || params.DPI != selection.DPI {
				t.Fatal("native selection changed")
			}
			if err = attempt.Close(); err != nil {
				t.Fatal(err)
			}
			if files, err := os.ReadDir(options.WorkRoot); err != nil || len(files) != 0 {
				t.Fatal("native inspection retained files", err)
			}
		})
	}
}
func TestInstalledParametersMatchActualBitmap(t *testing.T) {
	for _, selection := range []Selection{{Size: "a4", Color: "gray", DPI: 300}, {Size: "photo10x15", Color: "color", DPI: 600}} {
		t.Run(selection.Color, func(t *testing.T) {
			adapter, options := parameterAdapter(t, "geometry_max 300\n")
			params, probe, err := adapter.ProbeParameters(context.Background(), selection)
			if probe != nil {
				defer probe.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = probe.Close(); err != nil {
				t.Fatal(err)
			}
			attempt, err := adapter.Capture(context.Background(), Plan{DeviceID: options.DeviceID, Size: selection.Size, Color: selection.Color, DPI: selection.DPI, Width: params.Width, Height: params.Height, ByteBudget: options.MaxBytes})
			if attempt != nil {
				defer attempt.Close()
			}
			if err != nil {
				t.Fatal("read-back geometry did not match upstream capture", err)
			}
			artifact, err := attempt.Artifact()
			if err != nil || artifact.Image.Width != params.Width || artifact.Image.Height != params.Height {
				t.Fatal("parameter/image mismatch", err)
			}
			if err = attempt.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestInstalledParametersRejectUnsupportedArea(t *testing.T) {
	adapter, _ := parameterAdapter(t, "read-status-code SANE_STATUS_DEVICE_BUSY\n")
	params, attempt, err := adapter.ProbeParameters(context.Background(), Selection{Size: "a4", Color: "gray", DPI: 300})
	if attempt != nil {
		defer attempt.Close()
	}
	if !errors.Is(err, ErrParameters) || params != (Parameters{}) {
		t.Fatal("inexact area accepted", err)
	}
}
func TestInstalledParametersEnforcePixelLimit(t *testing.T) {
	_, options := parameterAdapter(t, "geometry_max 300\nread-status-code SANE_STATUS_DEVICE_BUSY\n")
	options.MaxPixels = 100
	adapter, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	params, attempt, err := adapter.ProbeParameters(context.Background(), Selection{Size: "a4", Color: "gray", DPI: 300})
	if attempt != nil {
		defer attempt.Close()
	}
	if !errors.Is(err, ErrParameters) || params != (Parameters{}) {
		t.Fatal("pixel limit bypassed", err)
	}
}
