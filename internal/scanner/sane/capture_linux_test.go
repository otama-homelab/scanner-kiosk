//go:build linux

package sane

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, script string) (*Adapter, Options) {
	t.Helper()
	root := t.TempDir()
	config := t.TempDir()
	binary := filepath.Join(t.TempDir(), "scanimage")
	for _, dir := range []string{root, config} {
		if err := os.Chmod(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(config, "dll.conf"), []byte("test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var source bytes.Buffer
	png.Encode(&source, image.NewGray(image.Rect(0, 0, 80, 100)))
	if err := os.WriteFile(filepath.Join(config, "source.png"), source.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	prefix := `#!/bin/sh
[ -z "$SCANNER_OIDC_CLIENT_SECRET" ] || exit 9
printf '%s\n' "$@" > "$SANE_CONFIG_DIR/seen-args"
`
	if script == "" {
		script = `cat "$SANE_CONFIG_DIR/source.png"`
	}
	if err := os.WriteFile(binary, []byte(prefix+script+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	options := Options{Binary: binary, ConfigDir: config, WorkRoot: root, DeviceID: "scanner_sane_test_001", DeviceName: "test:0", AllowTestBackend: true, MaxBytes: 1 << 20, MaxPixels: 1000000, Timeout: 5 * time.Second, StopGrace: 50 * time.Millisecond}
	adapter, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return adapter, options
}
func plan() Plan {
	return Plan{DeviceID: "scanner_sane_test_001", Size: "photo10x15", Color: "gray", DPI: 300, Width: 80, Height: 100, ByteBudget: 1 << 20}
}
func TestCaptureValidatesAndOwnsOneBitmap(t *testing.T) {
	t.Setenv("SCANNER_OIDC_CLIENT_SECRET", "must-not-inherit")
	adapter, options := fixture(t, "")
	original := filepath.Join(options.WorkRoot, "keep-original")
	os.WriteFile(original, []byte("keep"), 0600)
	attempt, err := adapter.Capture(context.Background(), plan())
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	artifact, err := attempt.Artifact()
	if err != nil || artifact.Image.Width != 80 || artifact.Image.Height != 100 || artifact.Image.MediaType != "image/png" || attempt.StoppedAtMS() == 0 {
		t.Fatalf("capture metadata: %v", err)
	}
	args, err := os.ReadFile(filepath.Join(options.ConfigDir, "seen-args"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--device-name=test:0\n", "--source=Flatbed\n", "--mode=Gray\n", "--resolution=300\n", "-x\n100\n-y\n150\n"} {
		if !strings.Contains(string(args), expected) {
			t.Fatal("capture command changed validated plan")
		}
	}
	if err = attempt.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(options.WorkRoot, artifact.RelativePath)); !os.IsNotExist(err) {
		t.Fatal("aborted capture retained")
	}
	if data, err := os.ReadFile(original); err != nil || string(data) != "keep" {
		t.Fatal("unrelated original changed")
	}
}
func TestCaptureRejectsUnsafePlansBeforeExecution(t *testing.T) {
	for _, mode := range []string{"foreign-device", "size", "dpi", "geometry", "byte-budget"} {
		t.Run(mode, func(t *testing.T) {
			adapter, options := fixture(t, "")
			request := plan()
			switch mode {
			case "foreign-device":
				request.DeviceID = "foreign_scanner_001"
			case "size":
				request.Size = "unknown"
			case "dpi":
				request.DPI = 1200
			case "geometry":
				request.Width = 1000000
			case "byte-budget":
				request.ByteBudget = 1 << 21
			}
			if attempt, err := adapter.Capture(context.Background(), request); err == nil || attempt != nil {
				t.Fatal("invalid plan executed")
			}
			if _, err := os.Stat(filepath.Join(options.ConfigDir, "seen-args")); !os.IsNotExist(err) {
				t.Fatal("invalid plan launched scanimage")
			}
		})
	}
}
func TestCaptureRejectsAdjustedOrInvalidOutput(t *testing.T) {
	for _, mode := range []string{"adjusted", "corrupt", "dimensions", "limit", "failed"} {
		t.Run(mode, func(t *testing.T) {
			script := ""
			switch mode {
			case "adjusted":
				script = "echo 'rounded value of resolution' >&2\ncat \"$SANE_CONFIG_DIR/source.png\""
			case "corrupt":
				script = "printf 'not a PNG'"
			case "failed":
				script = "exit 3"
			}
			adapter, _ := fixture(t, script)
			request := plan()
			if mode == "dimensions" {
				request.Width = 81
			}
			if mode == "limit" {
				request.ByteBudget = 1
			}
			attempt, err := adapter.Capture(context.Background(), request)
			if err == nil || attempt == nil {
				t.Fatal("invalid capture accepted")
			}
			defer attempt.Close()
			if _, err = attempt.Artifact(); !errors.Is(err, ErrNotReady) {
				t.Fatal("invalid output returned artifact")
			}
			if err = attempt.Release(); !errors.Is(err, ErrNotReady) {
				t.Fatal("invalid output transferred")
			}
			if err = attempt.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestCaptureCancellationAndRetainedTransfer(t *testing.T) {
	adapter, _ := fixture(t, "exec /bin/sleep 5")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	attempt, err := adapter.Capture(ctx, plan())
	if !errors.Is(err, context.DeadlineExceeded) || attempt == nil || attempt.StoppedAtMS() == 0 {
		t.Fatalf("native cancellation: %v", err)
	}
	if err = attempt.Close(); err != nil {
		t.Fatal(err)
	}
	adapter, options := fixture(t, "")
	attempt, err = adapter.Capture(context.Background(), plan())
	if err != nil {
		t.Fatal(err)
	}
	artifact, _ := attempt.Artifact()
	if err = attempt.Release(); err != nil {
		t.Fatal(err)
	}
	attempt.Close()
	if _, err = os.Stat(filepath.Join(options.WorkRoot, artifact.RelativePath)); err != nil {
		t.Fatal("transferred original removed")
	}
}
func TestUnknownProcessBlocksCleanup(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "capture_unknown"), 0700)
	os.WriteFile(filepath.Join(root, "capture_unknown", "image.png"), []byte("pending"), 0600)
	pinned, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	attempt := &Attempt{root: pinned, directory: "capture_unknown"}
	if !errors.Is(attempt.Close(), ErrReconciliation) || !errors.Is(attempt.Release(), ErrNotReady) {
		t.Fatal("unknown native work allowed cleanup/transfer")
	}
	if _, err = os.Stat(filepath.Join(root, "capture_unknown", "image.png")); err != nil {
		t.Fatal("uncertain capture removed")
	}
}

func TestCAFileIsExplicitForInspectionAndCapture(t *testing.T) {
	t.Setenv("SSL_CERT_FILE", "/ambient-must-not-inherit.pem")
	t.Setenv("API_KEY", "must-not-inherit")
	for _, caFile := range []string{"", "/private/configured-ca.pem"} {
		for _, operation := range []string{"inspection", "capture"} {
			t.Run(operation+caFile, func(t *testing.T) {
				script := "[ -z \"$API_KEY\" ] || exit 9\nprintf '%s' \"${SSL_CERT_FILE-unset}\" > \"$SANE_CONFIG_DIR/seen-ca\"\n"
				if operation == "capture" {
					script += "cat \"$SANE_CONFIG_DIR/source.png\"\n"
				} else {
					script += "cat \"$SANE_CONFIG_DIR/options.txt\"\n"
				}
				_, options := fixture(t, script)
				options.CAFile = caFile
				if err := os.WriteFile(filepath.Join(options.ConfigDir, "options.txt"), []byte(listing), 0600); err != nil {
					t.Fatal(err)
				}
				adapter, err := New(options)
				if err != nil {
					t.Fatal(err)
				}
				if operation == "capture" {
					attempt, err := adapter.Capture(context.Background(), plan())
					if err != nil {
						t.Fatal(err)
					}
					if err = attempt.Close(); err != nil {
						t.Fatal(err)
					}
				} else {
					_, attempt, err := adapter.Probe(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					if err = attempt.Close(); err != nil {
						t.Fatal(err)
					}
				}
				want := caFile
				if caFile == "" {
					want = "unset"
				}
				if got, err := os.ReadFile(filepath.Join(options.ConfigDir, "seen-ca")); err != nil || string(got) != want {
					t.Fatal("native CA environment was not explicit", err)
				}
			})
		}
	}
}

func TestCAFileRejectsUnsafeAdapterPaths(t *testing.T) {
	_, options := fixture(t, "")
	for _, value := range []string{"relative", "/tmp/../ca.pem", "/ca.pem\n", strings.Repeat("/a", 2050)} {
		options.CAFile = value
		if _, err := New(options); !errors.Is(err, ErrInvalid) {
			t.Fatal("unsafe native CA path accepted")
		}
	}
}
