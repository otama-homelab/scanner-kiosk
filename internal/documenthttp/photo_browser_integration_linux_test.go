//go:build linux

package documenthttp_test

import (
	"bytes"
	"context"
	"net"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var photoBuild struct {
	sync.Once
	binary string
	err    error
	output []byte
}

// The fixture runs the production server and worker, including their bounded child dispatch.
func photoBrowser(t *testing.T, scenario string) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	photoBuild.Do(func() {
		dir, e := os.MkdirTemp("", "scanner-photo-browser-build-")
		if e != nil {
			photoBuild.err = e
			return
		}
		photoBuild.binary = filepath.Join(dir, "scanner")
		cmd := exec.Command("python3", "scripts/build-web.py", "--output", photoBuild.binary)
		cmd.Dir = root
		photoBuild.output, photoBuild.err = cmd.CombinedOutput()
	})
	if photoBuild.err != nil {
		t.Fatalf("build: %v %s", photoBuild.err, photoBuild.output)
	}
	for _, key := range []string{"SCANNER_TEST_PLAYWRIGHT_MODULE", "SCANNER_TEST_CHROMIUM"} {
		if os.Getenv(key) == "" {
			t.Fatalf("required runtime %s missing", key)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	target, _ := url.Parse("http://" + address)
	proxy := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(target))
	defer proxy.Close()
	work := t.TempDir()
	for _, name := range []string{"state", "documents"} {
		if err = os.Mkdir(filepath.Join(work, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	env := append(os.Environ(), "SCANNER_STATE_DIR="+filepath.Join(work, "state"), "SCANNER_DOCUMENT_DIR="+filepath.Join(work, "documents"), "SCANNER_PUBLIC_URL="+proxy.URL, "SCANNER_LISTEN_ADDR="+address, "SCANNER_AUTHENTICATE_TYPE=none", "SCANNER_SCANNERS_JSON=[]", "SCANNER_OCR_PLUGINS=", "SCANNER_PAPERLESS_ENABLED=false")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var logs bytes.Buffer
	api := exec.CommandContext(ctx, photoBuild.binary, "serve")
	api.Env = env
	api.Stdout = &logs
	api.Stderr = &logs
	if err = api.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { api.Process.Signal(os.Interrupt); api.Wait() }()
	deadline := time.Now().Add(15 * time.Second)
	for {
		conn, e := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if e == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server startup timeout")
		}
		time.Sleep(50 * time.Millisecond)
	}
	worker := exec.CommandContext(ctx, photoBuild.binary, "worker")
	worker.Env = env
	var workerLogs bytes.Buffer
	worker.Stdout = &workerLogs
	worker.Stderr = &workerLogs
	if err = worker.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { worker.Process.Signal(os.Interrupt); worker.Wait() }()
	browser := exec.CommandContext(ctx, "node", "scripts/check-web-photo.cjs")
	browser.Dir = root
	browser.Env = append(env, "PLAYWRIGHT_MODULE="+os.Getenv("SCANNER_TEST_PLAYWRIGHT_MODULE"), "CHROMIUM_EXECUTABLE="+os.Getenv("SCANNER_TEST_CHROMIUM"), "SCANNER_TEST_ORIGIN="+proxy.URL, "SCANNER_PHOTO_SCENARIO="+scenario, "SCANNER_PHOTO_RECEIPT="+filepath.Join(root, ".planning/phases/49-server-app-completion/49-02-"+scenario+"-BROWSER.json"))
	output, e := browser.CombinedOutput()
	if e != nil {
		t.Fatalf("real photo browser %s: %v\n%s", scenario, e, output)
	}
	t.Log(string(output))
}

func TestPhotoCaptureAndPicker(t *testing.T) { photoBrowser(t, "capture") }
func TestPhotoReloadRecovery(t *testing.T)   { photoBrowser(t, "reload") }

func TestPhotoManualCropRotateUndo(t *testing.T) { photoBrowser(t, "manual") }
