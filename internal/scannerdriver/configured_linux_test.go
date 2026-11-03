//go:build linux

package scannerdriver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scanner/sane"
	"scanner-kiosk/internal/workeridentity"
)

func configuredFixture(t *testing.T) (config.Config, *workeridentity.Guard) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"SCANNER_DOCUMENT_DIR": root, "SCANNER_SCANIMAGE_PATH": "/bin/false", "SCANNER_SANE_PARAMETERS_PATH": "/bin/false", "SCANNER_SCANNERS_JSON": `[{"id":"scanner_configured_01","name":"Display name only","driver":"sane-airscan","endpoint":"http://192.0.2.10/eSCL"}]`}
	c, err := config.LoadWorker(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	guard, err := workeridentity.Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { guard.Close() })
	return c, guard
}

func TestConfiguredSessionRejectsUnboundLegacyWorkBeforeNativeIO(t *testing.T) {
	c, guard := configuredFixture(t)
	tools := t.TempDir()
	marker := filepath.Join(tools, "started")
	c.ScannerTools.ScanimagePath = filepath.Join(tools, "scanimage")
	script := "#!/bin/sh\nprintf started > '" + strings.ReplaceAll(marker, "'", "'\"'\"'") + "'\nexit 1\n"
	if err := os.WriteFile(c.ScannerTools.ScanimagePath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	session, err := Configure(context.Background(), c, guard)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	work := documents.ScanWork{Job: documents.Job{ID: "job_unbound_fixture_001", DocumentID: "document_unbound_001", Kind: "scan", Revision: 1, DeadlineAtMS: time.Now().Add(time.Minute).UnixMilli()}, Snapshot: documents.ScanSnapshot{Version: 1, DeviceID: c.Scanners[0].ID, WorkerBootID: guard.ID(), LeaseEpoch: 1, Capture: documents.Capture{Size: "a4", Color: "gray", DPI: 300}}, ReserveBytes: 1 << 20}
	if capturer, _, err := session.Resolve(context.Background(), work); !errors.Is(err, scanner.ErrUnresolved) || capturer != nil {
		t.Fatal("unbound native job accepted", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("native inspection started without physical binding")
	}
}
func TestConfiguredSessionDoesNotProbeAndOwnsOnlyGeneratedInputs(t *testing.T) {
	c, guard := configuredFixture(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	c.Scanners[0].Endpoint = server.URL + "/eSCL"
	session, err := Configure(context.Background(), c, guard)
	if err != nil {
		t.Fatal(err)
	}
	path := session.workspace.path
	if requests.Load() != 0 {
		t.Fatal("construction contacted device")
	}
	if len(session.workspace.configs) != 1 {
		t.Fatal("missing owned configuration")
	}
	for _, entry := range session.workspace.configs {
		data, err := os.ReadFile(filepath.Join(entry.root.Name(), "airscan.conf"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), c.Scanners[0].ID) || strings.Contains(string(data), c.Scanners[0].Name) || !strings.Contains(string(data), "discovery = disable") {
			t.Fatal("native registration not isolated")
		}
	}
	foreign := filepath.Join(path, "foreign.txt")
	if err := os.WriteFile(foreign, []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = session.Close(context.Background()); !errors.Is(err, ErrConfigCleanup) {
		t.Fatal("foreign data silently removed", err)
	}
	for _, entry := range session.workspace.configs {
		if _, err := os.Stat(filepath.Join(entry.root.Name(), "dll.conf")); err != nil {
			t.Fatal("preflight partially removed inputs", err)
		}
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "retain" {
		t.Fatal("foreign data modified")
	}
	if err = os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	if err = session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("generated config retained", err)
	}
	if !guard.MatchesRoot(c.DocumentDir) || guard.ID() == "" {
		t.Fatal("session released coordinator guard")
	}
	if err = session.Close(context.Background()); err != nil {
		t.Fatal("close not idempotent", err)
	}
}
func TestConfiguredSessionRetainsModifiedOrReplacedConfiguration(t *testing.T) {
	for _, mode := range []string{"content", "symlink", "directory"} {
		t.Run(mode, func(t *testing.T) {
			c, guard := configuredFixture(t)
			session, err := Configure(context.Background(), c, guard)
			if err != nil {
				t.Fatal(err)
			}
			var child string
			for name := range session.workspace.configs {
				child = name
			}
			original := filepath.Join(session.workspace.path, child, "airscan.conf")
			raw, err := os.ReadFile(original)
			if err != nil {
				t.Fatal(err)
			}
			moved := original + ".original"
			savedDirectory := session.workspace.path + ".original"
			switch mode {
			case "content":
				if err = os.WriteFile(original, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err = os.Rename(original, moved); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(moved, original); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err = os.Rename(session.workspace.path, savedDirectory); err != nil {
					t.Fatal(err)
				}
				if err = os.Mkdir(session.workspace.path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err = session.Close(context.Background()); !errors.Is(err, ErrConfigCleanup) {
				t.Fatal("modified config deleted", err)
			}
			switch mode {
			case "content":
				if err = os.WriteFile(original, raw, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err = os.Remove(original); err != nil {
					t.Fatal(err)
				}
				if err = os.Rename(moved, original); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err = os.Remove(session.workspace.path); err != nil {
					t.Fatal(err)
				}
				if err = os.Rename(savedDirectory, session.workspace.path); err != nil {
					t.Fatal(err)
				}
			}
			if err = session.Close(context.Background()); err != nil {
				t.Fatal("restored owned config could not close", err)
			}
		})
	}
}
func TestConfiguredSessionRejectsWrongRoleGuardAndSyntaxBeforeWriting(t *testing.T) {
	for _, mode := range []string{"api", "guard", "endpoint", "id", "duplicate-endpoint"} {
		t.Run(mode, func(t *testing.T) {
			c, guard := configuredFixture(t)
			otherRoot := t.TempDir()
			if err := os.Chmod(otherRoot, 0700); err != nil {
				t.Fatal(err)
			}
			other, err := workeridentity.Acquire(otherRoot)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			switch mode {
			case "api":
				c.Role = config.API
			case "guard":
				guard = other
			case "endpoint":
				c.Scanners[0].Endpoint = "http://192.0.2.10/eSCL;debug=on"
			case "duplicate-endpoint":
				duplicate := c.Scanners[0]
				duplicate.ID = "scanner_driver_alias01"
				duplicate.Endpoint += "/"
				c.Scanners = append(c.Scanners, duplicate)
			case "id":
				c.Scanners[0].ID = strings.Repeat("x", 97)
			}
			session, err := Configure(context.Background(), c, guard)
			if err == nil || session != nil {
				t.Fatal("invalid configuration created session", err)
			}
			entries, err := os.ReadDir(c.DocumentDir)
			if err != nil || len(entries) != 1 || entries[0].Name() != ".worker.lock" {
				t.Fatal("invalid configuration wrote inputs", err)
			}
		})
	}
}
func TestFailedConfiguredSessionRetainsCleanupOwnership(t *testing.T) {
	c, guard := configuredFixture(t)
	c.ScannerTools.ParametersPath = "/not-installed/sane-parameters"
	session, err := Configure(context.Background(), c, guard)
	if err == nil || session == nil || session.workspace == nil {
		t.Fatal("failed construction lost ownership", err)
	}
	path := session.workspace.path
	if err = session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("failed setup left generated inputs", err)
	}
}
func TestConfiguredSessionLossOfGuardRefusesStatusIO(t *testing.T) {
	c, guard := configuredFixture(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	c.Scanners[0].Endpoint = server.URL + "/eSCL"
	session, err := Configure(context.Background(), c, guard)
	if err != nil {
		t.Fatal(err)
	}
	if err = guard.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = session.ObserveIdle(context.Background(), c.Scanners[0].ID, time.Now().UnixMilli()); !errors.Is(err, scanner.ErrUnresolved) || requests.Load() != 0 {
		t.Fatal("lost identity reached scanner", err)
	}
	if err = session.Close(context.Background()); err != nil {
		t.Fatal("known stopped config cleanup failed", err)
	}
}

func TestConfiguredCAFileReachesCleanNativeEnvironment(t *testing.T) {
	c, guard := configuredFixture(t)
	marker := filepath.Join(t.TempDir(), "ca-seen")
	binary := filepath.Join(t.TempDir(), "scanimage")
	script := "#!/bin/sh\n[ -z \"$API_KEY\" ] || exit 9\nprintf '%s' \"${SSL_CERT_FILE-unset}\" > '" + strings.ReplaceAll(marker, "'", "'\"'\"'") + "'\nprintf invalid\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	var err error
	c.ScannerTools, err = config.LoadScannerTools(func(key string) (string, bool) {
		value, ok := map[string]string{"SSL_CERT_FILE": "/private/configured-ca.pem", "SCANNER_SCANIMAGE_PATH": binary, "SCANNER_SANE_PARAMETERS_PATH": binary}[key]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", "/ambient-must-not-inherit.pem")
	t.Setenv("API_KEY", "must-not-inherit")
	session, err := Configure(context.Background(), c, guard)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	backend := session.driver.devices[c.Scanners[0].ID].backend
	_, attempt, err := backend.InspectSelection(context.Background(), sane.Selection{Size: "a4", Color: "gray", DPI: 300})
	if !errors.Is(err, sane.ErrCapabilities) || attempt == nil || attempt.StoppedAtMS() <= 0 {
		t.Fatal("native fixture did not stop with its expected invalid capability response", err)
	}
	if err = attempt.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != c.ScannerTools.CAFile {
		t.Fatal("configured native CA file was dropped or ambient environment inherited", err)
	}
}
