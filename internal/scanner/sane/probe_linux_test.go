//go:build linux

package sane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const listing = "All options specific to device `test:0':\n" +
	"    --mode Gray|Color [Gray]\n" +
	"    --source Flatbed|Automatic Document Feeder [Flatbed]\n" +
	"    --resolution 75|100|150|200|300|600|1200dpi [300]\n" +
	"    -l 0..215.9mm [0]\n" +
	"    -t 0..297mm [0]\n" +
	"    -x 0..215.9mm [215.9]\n" +
	"    -y 0..297mm [297]\n"

func TestCapabilitiesUseConstraintsNotDefaults(t *testing.T) {
	caps, err := parseCapabilities([]byte(listing), "test:0")
	if err != nil || !reflect.DeepEqual(caps.Modes, []string{"gray", "color"}) || !reflect.DeepEqual(caps.DPI, []int{75, 100, 150, 200, 300, 600}) || !caps.Width.Contains(210) || !caps.Height.Contains(297) || caps.Width.Contains(216) {
		t.Fatalf("wrong constraints: %+v %v", caps, err)
	}
	rangeListing := strings.ReplaceAll(listing, "75|100|150|200|300|600|1200dpi [300]", "75..1200dpi (in steps of 75) [0.000762939]")
	caps, err = parseCapabilities([]byte(rangeListing), "test:0")
	if err != nil || !reflect.DeepEqual(caps.DPI, []int{75, 150, 300, 600}) {
		t.Fatalf("quantized resolution accepted off-step values: %+v %v", caps, err)
	}
}
func TestCapabilitiesRejectIncompleteOrUnsupportedListing(t *testing.T) {
	cases := map[string]string{
		"wrong-device":         strings.ReplaceAll(listing, "test:0", "test:1"),
		"duplicate":            listing + "    --mode Gray|Color [Gray]\n",
		"inactive":             strings.Replace(listing, "Gray|Color [Gray]", "Gray|Color [inactive]", 1),
		"unknown-modes":        strings.Replace(listing, "Gray|Color [Gray]", "Lineart [Lineart]", 1),
		"missing-source":       strings.ReplaceAll(listing, "    --source Flatbed|Automatic Document Feeder [Flatbed]\n", ""),
		"adf-only":             strings.ReplaceAll(listing, "Flatbed|Automatic Document Feeder [Flatbed]", "Automatic Document Feeder [Automatic Document Feeder]"),
		"missing-range":        strings.ReplaceAll(listing, "    -y 0..297mm [297]\n", ""),
		"wrong-unit":           strings.ReplaceAll(listing, "0..297mm", "0..297pel"),
		"wrong-dpi-unit":       strings.ReplaceAll(listing, "1200dpi", "1200mm"),
		"bad-list":             strings.ReplaceAll(listing, "75|100|150|200|300|600|1200dpi", "75|100|100dpi"),
		"unconstrained":        strings.ReplaceAll(listing, "75|100|150|200|300|600|1200dpi", "<int>"),
		"unsupported-dpi-only": strings.ReplaceAll(listing, "75|100|150|200|300|600|1200dpi", "1200dpi"),
		"backward-range":       strings.ReplaceAll(listing, "0..297mm", "297..0mm"),
		"huge-range":           strings.ReplaceAll(listing, "0..297mm", "0..999999999999999999999999999999999999999999mm"),
		"infinite-range":       strings.ReplaceAll(listing, "0..297mm", "0..Infmm"),
		"partial":              "All options specific to device `test:0':\n    --mode Gray|Color [Gray]\n",
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			caps, err := parseCapabilities([]byte(data), "test:0")
			if err == nil || len(caps.DPI) != 0 || len(caps.Modes) != 0 {
				t.Fatal("invalid capabilities escaped", err)
			}
		})
	}
}
func TestProbeRestrictedCommandAndOwnedScratch(t *testing.T) {
	t.Setenv("SCANNER_OIDC_CLIENT_SECRET", "must-not-inherit")
	adapter, options := fixture(t, `cat "$SANE_CONFIG_DIR/options.txt"`)
	if err := os.WriteFile(filepath.Join(options.ConfigDir, "options.txt"), []byte(listing), 0600); err != nil {
		t.Fatal(err)
	}
	caps, attempt, err := adapter.Probe(context.Background())
	if err != nil || attempt == nil || attempt.StoppedAtMS() <= 0 || len(caps.DPI) != 6 {
		t.Fatal("probe failed", err)
	}
	args, err := os.ReadFile(filepath.Join(options.ConfigDir, "seen-args"))
	if err != nil {
		t.Fatal(err)
	}
	if string(args) != "--device-name=test:0\n--format=png\n--all-options\n" {
		t.Fatal("probe gained scan/listing/options mutation arguments")
	}
	if err = attempt.Close(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(options.WorkRoot)
	if len(entries) != 0 {
		t.Fatal("probe scratch retained")
	}
}
func TestProbeFailureKeepsCleanupOwnership(t *testing.T) {
	for _, mode := range []string{"invalid", "warning", "oversize", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			script := "printf invalid"
			switch mode {
			case "warning":
				script = "printf warning >&2"
			case "oversize":
				script = "exec /bin/dd if=/dev/zero bs=1024 count=300 2>/dev/null"
			case "cancel":
				script = "exec /bin/sleep 5"
			}
			adapter, options := fixture(t, script)
			ctx := context.Background()
			if mode == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 30*time.Millisecond)
				defer cancel()
			}
			caps, attempt, err := adapter.Probe(ctx)
			if err == nil || attempt == nil || len(caps.DPI) != 0 || attempt.StoppedAtMS() <= 0 {
				t.Fatal("failed probe lost ownership or supplied capabilities", err)
			}
			if mode == "oversize" && !errors.Is(err, ErrLimit) {
				t.Fatal("oversize output not bounded", err)
			}
			if mode == "cancel" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("probe cancellation lost", err)
			}
			if err = attempt.Close(); err != nil {
				t.Fatal(err)
			}
			entries, _ := os.ReadDir(options.WorkRoot)
			if len(entries) != 0 {
				t.Fatal("failed probe left scratch")
			}
		})
	}
}
func TestInstalledScanimageCapabilityProbe(t *testing.T) {
	adapter, options := installed(t, "geometry_max 300\nread-status-code SANE_STATUS_DEVICE_BUSY\n")
	caps, attempt, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	if len(caps.Modes) != 2 || !reflect.DeepEqual(caps.DPI, []int{75, 100, 150, 200, 300, 600}) || !caps.Width.Contains(210) || !caps.Height.Contains(297) || !caps.Left.Contains(0) || !caps.Top.Contains(0) {
		t.Fatalf("official backend constraints: %+v", caps)
	}
	if err = attempt.Close(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(options.WorkRoot)
	if len(entries) != 0 {
		t.Fatal("official probe left scratch")
	}
}
