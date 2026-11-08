//go:build linux

package sane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func selectedListing() string {
	return strings.ReplaceAll(listing, "-x 0..215.9mm [215.9]", "-x 0..215.9mm [210]")
}
func TestSelectedProbeUsesCaptureOptionsWithoutAcquisition(t *testing.T) {
	adapter, options := fixture(t, `for arg in "$@"; do printf '%s\n' "$arg"; done > "$SANE_CONFIG_DIR/arguments"
cat "$SANE_CONFIG_DIR/options.txt"`)
	if err := os.WriteFile(filepath.Join(options.ConfigDir, "options.txt"), []byte(selectedListing()), 0600); err != nil {
		t.Fatal(err)
	}
	caps, attempt, err := adapter.ProbeSelection(context.Background(), Selection{Size: "a4", Color: "gray", DPI: 300})
	if attempt != nil {
		defer attempt.Close()
	}
	if err != nil || attempt == nil || attempt.StoppedAtMS() <= 0 || len(caps.DPI) != 6 {
		t.Fatal("selected inspection failed", err)
	}
	args, err := os.ReadFile(filepath.Join(options.ConfigDir, "arguments"))
	if err != nil {
		t.Fatal(err)
	}
	expected := "--device-name=test:0\n--format=png\n--source=Flatbed\n--mode=Gray\n--resolution=300\n-l\n0\n-t\n0\n-x\n210\n-y\n297\n--all-options\n"
	if string(args) != expected {
		t.Fatalf("unexpected options: %s", args)
	}
	if err = attempt.Close(); err != nil {
		t.Fatal(err)
	}
	if files, err := os.ReadDir(options.WorkRoot); err != nil || len(files) != 0 {
		t.Fatal("probe retained scratch", err)
	}
}

func TestNoScanIntentAndFixedAreaDefault(t *testing.T) {
	args, err := selectionArguments(Selection{Size: "a4", Color: "color", DPI: 300})
	if err != nil {
		t.Fatal(err)
	}
	want := "--source=Flatbed --mode=Color --resolution=300 -l 0 -t 0 -x 210 -y 297"
	if got := strings.Join(args, " "); got != want {
		t.Fatalf("fixed-area selection changed: %s", got)
	}
	for _, arg := range args {
		if strings.Contains(strings.ToLower(arg), "intent") {
			t.Fatalf("production selection contains unsupported intent: %s", arg)
		}
	}
}
func TestSelectedProbeRejectsAdjustedOrUnsupportedSettings(t *testing.T) {
	cases := map[string]string{
		"mode":                  strings.ReplaceAll(selectedListing(), "Gray|Color [Gray]", "Gray|Color [Color]"),
		"source":                strings.ReplaceAll(selectedListing(), "[Flatbed]", "[Automatic Document Feeder]"),
		"resolution":            strings.ReplaceAll(selectedListing(), "dpi [300]", "dpi [150]"),
		"area":                  strings.ReplaceAll(selectedListing(), "[210]", "[209]"),
		"origin":                strings.ReplaceAll(selectedListing(), "-l 0..215.9mm [0]", "-l 0..215.9mm [0.00001]"),
		"geometry-constraint":   strings.ReplaceAll(selectedListing(), "-x 0..215.9mm [210]", "-x 0..200mm [210]"),
		"resolution-constraint": strings.ReplaceAll(selectedListing(), "75|100|150|200|300|600|1200dpi [300]", "75|150|600dpi [300]"),
		"mode-constraint":       strings.ReplaceAll(selectedListing(), "Gray|Color [Gray]", "Color [Gray]"),
	}
	for name, output := range cases {
		t.Run(name, func(t *testing.T) {
			adapter, options := fixture(t, `cat "$SANE_CONFIG_DIR/options.txt"`)
			if err := os.WriteFile(filepath.Join(options.ConfigDir, "options.txt"), []byte(output), 0600); err != nil {
				t.Fatal(err)
			}
			caps, attempt, err := adapter.ProbeSelection(context.Background(), Selection{Size: "a4", Color: "gray", DPI: 300})
			if attempt != nil {
				defer attempt.Close()
			}
			if !errors.Is(err, ErrCapabilities) || len(caps.DPI) != 0 {
				t.Fatal("adjusted selection accepted", err)
			}
		})
	}
}
func TestSelectedProbeRejectsInvalidEnumsBeforeNativeStart(t *testing.T) {
	adapter, options := fixture(t, `touch "$SANE_CONFIG_DIR/started"`)
	for _, selection := range []Selection{{Size: "--help", Color: "gray", DPI: 300}, {Size: "a4", Color: "Lineart", DPI: 300}, {Size: "a4", Color: "color", DPI: 1200}} {
		_, attempt, err := adapter.ProbeSelection(context.Background(), selection)
		if err != ErrInvalid || attempt != nil {
			t.Fatal("invalid selection started native work", err)
		}
	}
	if _, err := os.Stat(filepath.Join(options.ConfigDir, "started")); !os.IsNotExist(err) {
		t.Fatal("invalid selection invoked native command")
	}
}
func TestInstalledSelectedProbeDoesNotReadScanner(t *testing.T) {
	for _, selection := range []Selection{{Size: "a4", Color: "gray", DPI: 300}, {Size: "photo10x15", Color: "color", DPI: 600}} {
		t.Run(selection.Color, func(t *testing.T) {
			adapter, options := installed(t, "geometry_max 300\nread-status-code SANE_STATUS_DEVICE_BUSY\n")
			caps, attempt, err := adapter.ProbeSelection(context.Background(), selection)
			if attempt != nil {
				defer attempt.Close()
			}
			if err != nil || len(caps.Modes) == 0 || attempt == nil || attempt.StoppedAtMS() <= 0 {
				t.Fatal("upstream selected probe failed", err)
			}
			if err = attempt.Close(); err != nil {
				t.Fatal(err)
			}
			if files, err := os.ReadDir(options.WorkRoot); err != nil || len(files) != 0 {
				t.Fatal("inspection scratch retained", err)
			}
		})
	}
}

func TestInstalledSelectedProbeRejectsSmallBed(t *testing.T) {
	adapter, _ := installed(t, "read-status-code SANE_STATUS_DEVICE_BUSY\n")
	caps, attempt, err := adapter.ProbeSelection(context.Background(), Selection{Size: "a4", Color: "gray", DPI: 300})
	if attempt != nil {
		defer attempt.Close()
	}
	if !errors.Is(err, ErrCapabilities) || len(caps.DPI) != 0 {
		t.Fatal("A4 accepted on limited test bed", err)
	}
}
