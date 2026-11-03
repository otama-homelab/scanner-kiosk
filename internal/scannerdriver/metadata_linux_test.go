//go:build linux

package scannerdriver

import (
	"context"
	"errors"
	"testing"
	"time"

	"scanner-kiosk/internal/scanner"
)

func TestReadOnlyCaptureMetadataDoesNotClaimOrAcquire(t *testing.T) {
	d, backend, idle, work := fixture(t)
	capture := scanner.Capture{Size: "a4", Color: "gray", DPI: 300}
	profile, err := d.InspectCapture(context.Background(), deviceID, capture)
	if err != nil || profile.Capture != capture || profile.Width != 80 || profile.Height != 100 || profile.CheckedAtMS <= 0 || profile.CheckedAtMS > time.Now().UnixMilli() {
		t.Fatal(profile, err)
	}
	if backend.captures != 0 || idle.calls != 0 || len(d.used) != 0 || backend.selection.closes != 1 || backend.parameters.closes != 1 {
		t.Fatal("metadata changed execution state or left scratch")
	}
	if _, _, err := d.Resolve(context.Background(), work); err != nil {
		t.Fatal("metadata consumed job resolution", err)
	}
}

func TestReadOnlyMetadataRejectsInvalidOrUnresolvedInspection(t *testing.T) {
	for _, mode := range []string{"invalid-capture", "unknown-device", "unsupported", "dimensions", "nil-inspection", "unknown-stop", "cleanup"} {
		t.Run(mode, func(t *testing.T) {
			d, backend, _, _ := fixture(t)
			id := deviceID
			capture := scanner.Capture{Size: "a4", Color: "gray", DPI: 300}
			switch mode {
			case "invalid-capture":
				capture.DPI = 1200
			case "unknown-device":
				id = "scanner_unknown_0001"
			case "unsupported":
				backend.caps.DPI = []int{150}
			case "dimensions":
				backend.params.Width = 1000001
			case "nil-inspection":
				backend.selection = nil
			case "unknown-stop":
				backend.selection.stopped = 0
			case "cleanup":
				backend.selection.err = errors.New("injected cleanup failure")
			}
			profile, err := d.InspectCapture(context.Background(), id, capture)
			if err == nil || profile.Width != 0 || backend.captures != 0 {
				t.Fatal("invalid metadata escaped", mode, profile, err)
			}
			if mode == "nil-inspection" || mode == "unknown-stop" || mode == "cleanup" {
				if !errors.Is(err, scanner.ErrUnresolved) {
					t.Fatal("unknown stop/cleanup lost", err)
				}
				if _, err := d.InspectCapture(context.Background(), deviceID, capture); !errors.Is(err, scanner.ErrUnresolved) {
					t.Fatal("unresolved driver resumed", err)
				}
			}
		})
	}
}
