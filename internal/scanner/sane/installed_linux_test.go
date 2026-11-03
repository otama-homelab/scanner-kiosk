//go:build linux

package sane

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func installed(t *testing.T, configuration string) (*Adapter, Options) {
	t.Helper()
	binary := os.Getenv("SCANNER_TEST_SANE_BINARY")
	if binary == "" {
		t.Skip("requires extracted official scanimage/test backend")
	}
	config, root := t.TempDir(), t.TempDir()
	for _, directory := range []string{config, root} {
		if err := os.Chmod(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(config, "dll.conf"), []byte("test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "test.conf"), []byte("number_of_devices 1\ndepth 8\n"+configuration), 0600); err != nil {
		t.Fatal(err)
	}
	options := Options{Binary: binary, ConfigDir: config, WorkRoot: root, DeviceID: "scanner_sane_test_001", DeviceName: "test:0", AllowTestBackend: true, LibraryDirs: strings.Split(os.Getenv("SCANNER_TEST_SANE_LIBRARY_DIRS"), ":"), MaxBytes: 32 << 20, MaxPixels: 40000000, Timeout: 10 * time.Second, StopGrace: time.Second}
	adapter, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return adapter, options
}
func TestInstalledScanimageCapture(t *testing.T) {
	if os.Getenv("SCANNER_TEST_SANE_BINARY") == "" {
		t.Skip("requires official SANE test backend")
	}
	for _, resolution := range []int{300, 600} {
		for _, color := range []string{"color", "gray"} {
			name := color + "-" + map[int]string{300: "300", 600: "600"}[resolution]
			t.Run(name, func(t *testing.T) {
				adapter, options := installed(t, "")
				request := Plan{DeviceID: options.DeviceID, Size: "photo10x15", Color: color, DPI: resolution, Width: int(math.Floor(100.0 / 25.4 * float64(resolution))), Height: int(math.Floor(150.0 / 25.4 * float64(resolution))), ByteBudget: 32 << 20}
				attempt, err := adapter.Capture(context.Background(), request)
				if err != nil {
					t.Fatal(err)
				}
				defer attempt.Close()
				artifact, err := attempt.Artifact()
				if err != nil || artifact.Image.Width != request.Width || artifact.Image.Height != request.Height || artifact.Image.Size <= 0 {
					t.Fatalf("capture metadata: %v", err)
				}
				if err = attempt.Close(); err != nil {
					t.Fatal(err)
				}
				if files, err := os.ReadDir(options.WorkRoot); err != nil || len(files) != 0 {
					t.Fatal("test scan output retained after confirmed cleanup")
				}
			})
		}
	}
}
func TestInstalledScanimageBusyAndCancellation(t *testing.T) {
	if os.Getenv("SCANNER_TEST_SANE_BINARY") == "" {
		t.Skip("requires official SANE test backend")
	}
	for _, mode := range []string{"busy", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			configuration := "read-status-code SANE_STATUS_DEVICE_BUSY\n"
			if mode == "cancel" {
				configuration = "read-delay true\nread-delay-duration 200000\nread-limit true\nread-limit-size 1024\n"
			}
			adapter, options := installed(t, configuration)
			ctx := context.Background()
			var cancel context.CancelFunc = func() {}
			if mode == "cancel" {
				ctx, cancel = context.WithTimeout(ctx, 200*time.Millisecond)
			}
			defer cancel()
			request := Plan{DeviceID: options.DeviceID, Size: "photo10x15", Color: "color", DPI: 300, Width: 1181, Height: 1771, ByteBudget: 32 << 20}
			attempt, err := adapter.Capture(ctx, request)
			if err == nil || attempt == nil || attempt.StoppedAtMS() == 0 {
				t.Fatalf("scan failure/stop evidence: %v", err)
			}
			if mode == "cancel" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("cancel cause lost: %v", err)
			}
			if _, err = attempt.Artifact(); !errors.Is(err, ErrNotReady) {
				t.Fatal("failed scan yielded ready image")
			}
			if err = attempt.Close(); err != nil {
				t.Fatal(err)
			}
			if files, err := os.ReadDir(options.WorkRoot); err != nil || len(files) != 0 {
				t.Fatal("failed scan files retained")
			}
		})
	}
}

func TestInstalledScanimageA4(t *testing.T) {
	adapter, options := installed(t, "geometry_max 300\n")
	request := Plan{DeviceID: options.DeviceID, Size: "a4", Color: "gray", DPI: 300, Width: 2480, Height: 3507, ByteBudget: 32 << 20}
	attempt, err := adapter.Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	artifact, err := attempt.Artifact()
	if err != nil || artifact.Image.Width != 2480 || artifact.Image.Height != 3507 {
		t.Fatalf("A4 bitmap: %v", err)
	}
	if err = attempt.Close(); err != nil {
		t.Fatal(err)
	}
}
