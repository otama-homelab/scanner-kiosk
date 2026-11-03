//go:build linux

package scannerdriver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scanner/sane"
)

func TestConfigMutationStopsEveryNativeBoundary(t *testing.T) {
	for _, mode := range []string{"missing", "content", "extra-directory", "symlink", "permissions"} {
		t.Run(mode, func(t *testing.T) {
			c, guard := configuredFixture(t)
			session, err := Configure(context.Background(), c, guard)
			if err != nil {
				t.Fatal(err)
			}
			var config string
			for _, entry := range session.workspace.configs {
				config = entry.root.Name()
			}
			path := filepath.Join(config, "airscan.conf")
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			spy := &backend{}
			wrapped := &sessionBackend{session: session, backend: spy}
			selection := sane.Selection{Size: "a4", Color: "gray", DPI: 300}
			// Positive controls prove each wrapper reaches this same backend when intact.
			if _, _, err = wrapped.InspectSelection(context.Background(), selection); err != nil {
				t.Fatal(err)
			}
			if _, _, err = wrapped.InspectParameters(context.Background(), selection); err != nil {
				t.Fatal(err)
			}
			if _, err = wrapped.Capture(context.Background(), scanner.Plan{}); err != nil {
				t.Fatal(err)
			}
			if spy.selections != 1 || spy.parameterCalls != 1 || spy.captures != 1 {
				t.Fatal("positive control did not reach backend")
			}
			saved := filepath.Join(t.TempDir(), "original.conf")
			switch mode {
			case "missing", "symlink":
				if err = os.Rename(path, saved); err != nil {
					t.Fatal(err)
				}
				if mode == "symlink" {
					if err = os.Symlink(saved, path); err != nil {
						t.Fatal(err)
					}
				}
			case "content":
				err = os.WriteFile(path, []byte("changed"), 0600)
			case "extra-directory":
				err = os.Mkdir(filepath.Join(config, "dll.d"), 0700)
			case "permissions":
				err = os.Chmod(path, 0644)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, attempt, err := wrapped.InspectSelection(context.Background(), selection); !errors.Is(err, scanner.ErrUnresolved) || attempt != nil {
				t.Fatalf("selection guard: %v", err)
			}
			if _, attempt, err := wrapped.InspectParameters(context.Background(), selection); !errors.Is(err, scanner.ErrUnresolved) || attempt != nil {
				t.Fatalf("parameter guard: %v", err)
			}
			if attempt, err := wrapped.Capture(context.Background(), scanner.Plan{}); !errors.Is(err, scanner.ErrUnresolved) || attempt != nil {
				t.Fatalf("capture guard: %v", err)
			}
			if spy.selections != 1 || spy.parameterCalls != 1 || spy.captures != 1 {
				t.Fatal("modified config reached native backend")
			}
			switch mode {
			case "symlink":
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
				fallthrough
			case "missing":
				err = os.Rename(saved, path)
			case "content":
				err = os.WriteFile(path, original, 0600)
			case "extra-directory":
				err = os.Remove(filepath.Join(config, "dll.d"))
			case "permissions":
				err = os.Chmod(path, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = session.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
