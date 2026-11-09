//go:build bundled && linux && amd64

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func fixture(t *testing.T, name string, content []byte, link string) ([]byte, manifest, string, string) {
	t.Helper()
	var packed bytes.Buffer
	gz := gzip.NewWriter(&packed)
	tarball := tar.NewWriter(gz)
	header := &tar.Header{Name: name, Size: int64(len(content)), Mode: 0444, Typeflag: tar.TypeReg}
	e := entry{Size: int64(len(content)), Mode: 0444, SHA256: digest(content)}
	if link != "" {
		header.Typeflag = tar.TypeSymlink
		header.Linkname = link
		header.Size = 0
		e = entry{Link: link}
	}
	if err := tarball.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if link == "" {
		if _, err := tarball.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarball.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	m := manifest{PayloadSHA256: digest(packed.Bytes()), Files: map[string]entry{name: e}}
	work := t.TempDir()
	executable := filepath.Join(work, "source-launcher")
	if err := os.WriteFile(executable, []byte("fixture launcher"), 0700); err != nil {
		t.Fatal(err)
	}
	return packed.Bytes(), m, filepath.Join(work, "cache"), executable
}

func TestInstallReuseAndConcurrentPublication(t *testing.T) {
	data, m, base, executable := fixture(t, "app/scanner", []byte("synthetic app"), "")
	var wg sync.WaitGroup
	paths := make(chan string, 2)
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			root, err := install(base, executable, data, m)
			paths <- root
			failures <- err
		}()
	}
	wg.Wait()
	close(paths)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var root string
	for p := range paths {
		if root != "" && root != p {
			t.Fatal("different cache roots")
		}
		root = p
	}
	if got, err := os.ReadFile(filepath.Join(root, "app/scanner")); err != nil || string(got) != "synthetic app" {
		t.Fatal("payload differs", err)
	}
	if again, err := install(base, executable, data, m); err != nil || again != root {
		t.Fatal("valid cache not reused", err)
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 1 {
		t.Fatal("unpublished staging residue", err)
	}
}

func TestInstallRejectsUnsafePayload(t *testing.T) {
	for _, name := range []string{"../outside", "/absolute", "tools/runner", "app/../escape"} {
		t.Run(name, func(t *testing.T) {
			data, m, base, exe := fixture(t, name, []byte("bad"), "")
			if _, err := install(base, exe, data, m); err == nil {
				t.Fatal("unsafe entry accepted")
			}
		})
	}
	for _, target := range []string{"/etc/passwd", "../../escape"} {
		t.Run(target, func(t *testing.T) {
			data, m, base, exe := fixture(t, "lib/alias", nil, target)
			if _, err := install(base, exe, data, m); err == nil {
				t.Fatal("escaping link accepted")
			}
		})
	}
}

func TestInstallRejectsMissingExtraAndChangedPayload(t *testing.T) {
	for _, kind := range []string{"hash", "missing", "extra", "content"} {
		t.Run(kind, func(t *testing.T) {
			data, m, base, exe := fixture(t, "app/scanner", []byte("app"), "")
			switch kind {
			case "hash":
				m.PayloadSHA256 = strings.Repeat("0", 64)
			case "missing":
				m.Files["another"] = entry{Mode: 0444, SHA256: digest(nil)}
			case "extra":
				delete(m.Files, "app/scanner")
				m.Files["another"] = entry{Mode: 0444, SHA256: digest(nil)}
			case "content":
				e := m.Files["app/scanner"]
				e.SHA256 = strings.Repeat("0", 64)
				m.Files["app/scanner"] = e
			}
			if _, err := install(base, exe, data, m); err == nil {
				t.Fatal("invalid payload accepted")
			}
			entries, _ := os.ReadDir(base)
			if len(entries) != 0 {
				t.Fatal("failed install published state")
			}
		})
	}
}

func TestCacheTamperingFailsWithoutOverwrite(t *testing.T) {
	for _, kind := range []string{"content", "permission", "symlink", "extra"} {
		t.Run(kind, func(t *testing.T) {
			data, m, base, exe := fixture(t, "app/scanner", []byte("app"), "")
			root, err := install(base, exe, data, m)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "app/scanner")
			switch kind {
			case "content":
				os.Chmod(file, 0600)
				os.WriteFile(file, []byte("bad"), 0444)
			case "permission":
				os.Chmod(file, 0666)
			case "symlink":
				os.Remove(file)
				os.Symlink(exe, file)
			case "extra":
				os.WriteFile(filepath.Join(root, "injected"), []byte("bad"), 0600)
			}
			if _, err = install(base, exe, data, m); err == nil {
				t.Fatal("tampered cache accepted")
			}
			if _, err = os.Lstat(file); err != nil {
				t.Fatal("changed cache was deleted")
			}
		})
	}
}

func TestNativeEnvironmentDropsCredentialsAndLoaderInjection(t *testing.T) {
	env := nativeEnvironment("/private/runtime", []string{"SCANNER_OIDC_CLIENT_SECRET=secret", "API_KEY=secret", "LD_PRELOAD=/tmp/injected.so", "SANE_CONFIG_DIR=/private/config", "HOME=/nonexistent"})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "secret") || strings.Contains(joined, "LD_PRELOAD") || !strings.Contains(joined, "SANE_CONFIG_DIR=/private/config") {
		t.Fatal("native environment boundary changed")
	}
}

func TestApplicationOverridesPreservedAndWorkerOIDCRemoved(t *testing.T) {
	env := applicationEnvironment("/private/runtime", []string{"worker"}, []string{"SCANNER_OIDC_CLIENT_SECRET=secret", "SCANNER_OCR_TESSERACT_PATH=/custom/tesseract", "MODEL_KEY=worker-key"})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "SCANNER_OIDC_") || !strings.Contains(joined, "SCANNER_OCR_TESSERACT_PATH=/custom/tesseract") || !strings.Contains(joined, "MODEL_KEY=worker-key") {
		t.Fatal("application environment ownership changed")
	}
}

func TestTarDecoderDoesNotUsePayloadReaderAfterEOF(t *testing.T) {
	// A truncated archive must never become a reusable cache.
	data, m, base, exe := fixture(t, "app/scanner", []byte("app"), "")
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	var packed bytes.Buffer
	gz := gzip.NewWriter(&packed)
	gz.Write(raw[:100])
	gz.Close()
	m.PayloadSHA256 = digest(packed.Bytes())
	if _, err = install(base, exe, packed.Bytes(), m); err == nil {
		t.Fatal("truncated tar accepted")
	}
}

func TestFileDigestRejectsOversizedFile(t *testing.T) {
	name := filepath.Join(t.TempDir(), "large")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate((512 << 20) + 1); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = fileDigest(name); err == nil {
		t.Fatal("accepted oversized executable")
	}
}

func TestCAEnvironmentSelectsBundleOrExplicitOverride(t *testing.T) {
	for _, value := range []string{"/private/ca.pem", ""} {
		env := nativeEnvironment("/private/runtime", []string{"SSL_CERT_FILE=" + value, "SSL_CERT_DIR=/ambient/roots", "API_KEY=secret"})
		var count int
		for _, item := range env {
			if item == "SSL_CERT_FILE="+value {
				count++
			}
			if strings.HasPrefix(item, "SSL_CERT_DIR=") || strings.Contains(item, "secret") {
				t.Fatal("native trust allowed unrelated environment")
			}
		}
		if count != 1 {
			t.Fatal("explicit native CA file was dropped or duplicated")
		}
	}
	for _, args := range [][]string{{"serve"}, {"worker"}} {
		for _, env := range [][]string{nativeEnvironment("/private/runtime", nil), applicationEnvironment("/private/runtime", args, nil)} {
			var count int
			for _, item := range env {
				if item == "SSL_CERT_FILE=/private/runtime/usr/share/scanner-kiosk/ca-certificates.crt" {
					count++
				}
			}
			if count != 1 {
				t.Fatal("bundled CA was not selected by default")
			}
		}
		for _, value := range []string{"/custom/ca.pem", ""} {
			env := applicationEnvironment("/private/runtime", args, []string{"SSL_CERT_FILE=" + value})
			var count int
			for _, item := range env {
				if strings.HasPrefix(item, "SSL_CERT_FILE=") {
					if item != "SSL_CERT_FILE="+value {
						t.Fatal("explicit trust selection overwritten")
					}
					count++
				}
			}
			if count != 1 {
				t.Fatal("explicit application CA missing or duplicated")
			}
		}
	}
}

func TestInstallWithPrivateUmask(t *testing.T) {
	previous := syscall.Umask(0077)
	defer syscall.Umask(previous)
	data, m, base, exe := fixture(t, "app/scanner", []byte("synthetic app"), "")
	root, err := install(base, exe, data, m)
	if err != nil {
		t.Fatal("private umask prevented installation", err)
	}
	if err = verify(root, m, digest([]byte("fixture launcher"))); err != nil {
		t.Fatal(err)
	}
}

func TestPopplerRequiresVerifiedDataMount(t *testing.T) {
	root := t.TempDir()
	if popplerDataAvailable(root) {
		t.Fatal("missing verified CMaps accepted")
	}
	if e := os.MkdirAll(filepath.Join(root, "usr/share/poppler"), 0700); e != nil {
		t.Fatal(e)
	}
	if popplerDataAvailable(root) {
		t.Fatal("unrelated ambient host CMaps accepted")
	}
}
