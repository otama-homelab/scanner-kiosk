//go:build linux

package tesseractapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"scanner-kiosk/internal/buildinfo"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/ocr/tesseract"
)

type serviceConfig struct {
	native        config.Tesseract
	key           config.Secret
	root, address string
	limits        Limits
	stopGrace     time.Duration
}

func runtimeRevision(native, implementation string) (string, error) {
	if native == "" {
		return "", nil
	} // Source-only development keeps the legacy fingerprint.
	if _, err := hex.DecodeString(native); err != nil || len(native) != 64 || implementation == "" || len(implementation) > 128 || strings.ContainsAny(implementation, "\x00\r\n") {
		return "", errService
	}
	sum := sha256.Sum256([]byte("tesseract-container-v1\x00" + native + "\x00" + implementation))
	return hex.EncodeToString(sum[:]), nil
}

func loadService(lookup config.Lookup) (serviceConfig, error) {
	get := func(key, def string) string {
		if v, ok := lookup(key); ok {
			return v
		}
		return def
	}
	v, err := config.LoadTesseract(lookup)
	if err != nil {
		return serviceConfig{}, errService
	}
	native := v.(config.Tesseract)
	c := serviceConfig{native: native, key: config.Secret(get("SCANNER_OCR_TESSERACT_API_KEY", "")), root: get("SCANNER_OCR_TESSERACT_WORK_DIR", "/tmp/ocr"), address: get("SCANNER_OCR_TESSERACT_LISTEN_ADDR", ":8090"), limits: Limits{MaxPages: 10000}}
	if native.Endpoint != "" || !validKey(string(c.key)) || !filepath.IsAbs(c.root) || filepath.Clean(c.root) != c.root || strings.ContainsAny(c.root, "\x00\r\n") {
		return serviceConfig{}, errService
	}
	host, port, err := net.SplitHostPort(c.address)
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || number < 1 || number > 65535 || (host != "" && net.ParseIP(host) == nil) {
		return serviceConfig{}, errService
	}
	values := map[string]int64{"MAX_INPUT_BYTES": 200 << 20, "MAX_RESULT_BYTES": 16 << 20, "MAX_PAGE_PIXELS": 40000000, "TIMEOUT_SECONDS": 180, "STOP_GRACE_SECONDS": 2}
	for name, def := range values {
		n, err := strconv.ParseInt(get("SCANNER_OCR_TESSERACT_"+name, strconv.FormatInt(def, 10)), 10, 64)
		if err != nil || n < 1 {
			return serviceConfig{}, errService
		}
		values[name] = n
	}
	if values["TIMEOUT_SECONDS"] > 3600 || values["STOP_GRACE_SECONDS"] > 30 {
		return serviceConfig{}, errService
	}
	c.limits.MaxInputBytes = values["MAX_INPUT_BYTES"]
	c.limits.MaxResultBytes = values["MAX_RESULT_BYTES"]
	c.limits.MaxPagePixels = values["MAX_PAGE_PIXELS"]
	c.limits.Timeout = time.Duration(values["TIMEOUT_SECONDS"]) * time.Second
	c.stopGrace = time.Duration(values["STOP_GRACE_SECONDS"]) * time.Second
	if !c.limits.valid() {
		return serviceConfig{}, errService
	}
	return c, nil
}

// Run serves a terminal OCR API. It never opens application databases or user sessions.
func Run(ctx context.Context, lookup config.Lookup) error {
	c, err := loadService(lookup)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	revision, err := runtimeRevision(buildinfo.NativeRuntimeSHA256, buildinfo.Revision)
	if err != nil {
		return err
	}
	for _, path := range []string{c.root, filepath.Join(c.root, "native"), filepath.Join(c.root, "requests")} {
		if err = os.MkdirAll(path, 0700); err != nil {
			return errService
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return errService
		}
	}
	entries, err := os.ReadDir(filepath.Join(c.root, "native"))
	if err != nil || len(entries) != 0 {
		return errService
	}
	provider, err := tesseract.New(tesseract.Options{Binary: c.native.Path, DataDir: c.native.DataDir, WorkRoot: filepath.Join(c.root, "native"), RuntimeRevision: revision, MaxInputBytes: c.limits.MaxInputBytes, MaxResultBytes: c.limits.MaxResultBytes, MaxPagePixels: c.limits.MaxPagePixels, MaxPages: c.limits.MaxPages, Timeout: c.limits.Timeout, StopGrace: c.stopGrace})
	if err != nil {
		return errService
	}
	h, err := NewHandler(HandlerOptions{Provider: provider, APIKey: c.key, WorkRoot: filepath.Join(c.root, "requests"), Limits: c.limits})
	if err != nil {
		return err
	}
	s := &http.Server{Addr: c.address, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: c.limits.Timeout, WriteTimeout: c.limits.Timeout + c.stopGrace + 5*time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, ErrorLog: log.New(io.Discard, "", 0), BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan error, 1)
	go func() { done <- s.ListenAndServe() }()
	select {
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errService
	case <-ctx.Done():
		stopping, cancel := context.WithTimeout(context.Background(), c.stopGrace+5*time.Second)
		defer cancel()
		if err = s.Shutdown(stopping); err != nil {
			_ = s.Close()
			return errService
		}
		err = <-done
		if !errors.Is(err, http.ErrServerClosed) {
			return errService
		}
		return nil
	}
}
