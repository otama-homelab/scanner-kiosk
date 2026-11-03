//go:build linux

// Package sane captures one flatbed bitmap with upstream scanimage. Lease
// admission, capabilities and physical-device idle reconciliation are separate.
package sane

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"scanner-kiosk/internal/captureimage"
	"scanner-kiosk/internal/processrun"
	"scanner-kiosk/internal/scanner"
)

var (
	ErrInvalid        = errors.New("invalid scanner capture plan")
	ErrStorage        = errors.New("scanner capture storage unavailable")
	ErrCapture        = errors.New("scanner capture failed")
	ErrLimit          = errors.New("scanner capture output exceeds byte budget")
	ErrAdjusted       = errors.New("scanner adjusted requested capture parameters")
	ErrReconciliation = errors.New("scanner local process requires reconciliation")
	ErrNotReady       = errors.New("scanner capture not ready for transfer")
)

type Options struct {
	Binary, ConfigDir, WorkRoot string
	ParametersBinary            string
	CAFile                      string
	DeviceID, DeviceName        string
	LibraryDirs                 []string
	AllowTestBackend            bool
	MaxBytes, MaxPixels         int64
	Timeout, StopGrace          time.Duration
}
type Plan = scanner.Plan
type Adapter struct {
	options Options
	env     []string
}
type Artifact = scanner.Artifact
type Attempt struct {
	mu                               sync.Mutex
	root                             *os.Root
	directory                        string
	artifact                         Artifact
	stoppedAtMS                      int64
	stopped, valid, closed, released bool
}

func New(options Options) (*Adapter, error) {
	if !opaque(options.DeviceID) || !cleanString(options.DeviceName) || options.MaxBytes < 1 || options.MaxBytes > 1<<40 || options.MaxPixels < 1 || options.Timeout <= 0 || options.StopGrace <= 0 {
		return nil, ErrInvalid
	}
	backend := "airscan"
	if options.AllowTestBackend && options.DeviceName == "test:0" {
		backend = "test"
	} else if !strings.HasPrefix(options.DeviceName, "airscan:") {
		return nil, ErrInvalid
	}
	for _, directory := range []string{options.ConfigDir, options.WorkRoot} {
		if !absolute(directory) || strings.Contains(options.ConfigDir, ":") {
			return nil, ErrInvalid
		}
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return nil, ErrStorage
		}
	}
	if !absolute(options.Binary) {
		return nil, ErrInvalid
	}
	if options.ParametersBinary != "" {
		if !absolute(options.ParametersBinary) {
			return nil, ErrInvalid
		}
		info, err := os.Stat(options.ParametersBinary)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return nil, ErrInvalid
		}
	}
	binary, err := os.Stat(options.Binary)
	if err != nil || !binary.Mode().IsRegular() || binary.Mode().Perm()&0111 == 0 {
		return nil, ErrInvalid
	}
	configFile, err := os.Open(filepath.Join(options.ConfigDir, "dll.conf"))
	if err != nil {
		return nil, ErrInvalid
	}
	config, readErr := io.ReadAll(io.LimitReader(configFile, 33))
	closeErr := configFile.Close()
	if readErr != nil || closeErr != nil || string(config) != backend+"\n" {
		return nil, ErrInvalid
	}
	env := []string{"LC_ALL=C", "SANE_CONFIG_DIR=" + options.ConfigDir}
	if options.CAFile != "" {
		if !absolute(options.CAFile) {
			return nil, ErrInvalid
		}
		env = append(env, "SSL_CERT_FILE="+options.CAFile)
	}
	for _, directory := range options.LibraryDirs {
		if !absolute(directory) || strings.ContainsAny(directory, ":;") {
			return nil, ErrInvalid
		}
	}
	options.LibraryDirs = append([]string(nil), options.LibraryDirs...)
	if len(options.LibraryDirs) > 0 {
		env = append(env, "LD_LIBRARY_PATH="+strings.Join(options.LibraryDirs, ":"))
	}
	return &Adapter{options: options, env: env}, nil
}

// A nil attempt is returned only before allocating or starting native work.
// Once allocated, the owned attempt is retained on every error path.
// Capture may be called once only after confirmed claim of this exact device's
// immutable plan. It never retries. A returned Attempt owns all local files on
// success or failure; unknown local stop blocks cleanup. No physical-idle proof
// is produced by a successful command or this adapter's StoppedAtMS.
func (a *Adapter) Capture(ctx context.Context, plan Plan) (scanner.Attempt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, _, ok := paper(plan.Size)
	if !ok || plan.DeviceID != a.options.DeviceID || (plan.Color != "color" && plan.Color != "gray") || !dpi(plan.DPI) || plan.Width < 1 || plan.Height < 1 || int64(plan.Width) > a.options.MaxPixels/int64(max(plan.Height, 1)) || plan.ByteBudget < 1 || plan.ByteBudget > a.options.MaxBytes {
		return nil, ErrInvalid
	}
	root, err := os.OpenRoot(a.options.WorkRoot)
	if err != nil {
		return nil, ErrStorage
	}
	var nonce [24]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		root.Close()
		return nil, ErrStorage
	}
	id := hex.EncodeToString(nonce[:])
	directory := "capture_" + id
	if err = root.Mkdir(directory, 0700); err != nil {
		root.Close()
		return nil, ErrStorage
	}
	attempt := &Attempt{root: root, directory: directory, stopped: true, stoppedAtMS: time.Now().UnixMilli(), artifact: Artifact{ID: "original_" + id, RelativePath: path.Join(directory, "image.png")}}
	output, err := root.OpenFile(attempt.artifact.RelativePath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return attempt, ErrStorage
	}
	stderrPath := path.Join(directory, "stderr")
	stderr, err := root.OpenFile(stderrPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		output.Close()
		return attempt, ErrStorage
	}
	ctx, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()
	watching, stop := context.WithCancelCause(ctx)
	check := func() error {
		image, err := root.Stat(attempt.artifact.RelativePath)
		if err != nil {
			return ErrStorage
		}
		diagnostic, err := root.Stat(stderrPath)
		if err != nil {
			return ErrStorage
		}
		if diagnostic.Size() > 1<<20 || diagnostic.Size() > plan.ByteBudget || image.Size() > plan.ByteBudget-diagnostic.Size() {
			return ErrLimit
		}
		return nil
	}
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-watching.Done():
				return
			case <-ticker.C:
				if err := check(); err != nil {
					stop(err)
					return
				}
			}
		}
	}()
	options, _ := selectionArguments(Selection{Size: plan.Size, Color: plan.Color, DPI: plan.DPI})
	args := append([]string{"--device-name=" + a.options.DeviceName, "--format=png"}, options...)

	env := append(append([]string(nil), a.env...), "TMPDIR="+filepath.Join(a.options.WorkRoot, directory))
	attempt.stopped = false
	attempt.stoppedAtMS = 0
	result := processrun.Run(watching, processrun.Spec{Path: a.options.Binary, Args: args, Env: env, Dir: filepath.Join(a.options.WorkRoot, directory), Stdout: output, Stderr: stderr, Grace: a.options.StopGrace})
	cause := context.Cause(watching)
	stop(nil)
	<-joined
	attempt.stopped = result.GroupStopped
	if attempt.stopped {
		attempt.stoppedAtMS = time.Now().UnixMilli()
	}
	if !attempt.stopped {
		output.Close()
		stderr.Close()
		return attempt, ErrReconciliation
	}
	if err = check(); err != nil {
		output.Close()
		stderr.Close()
		return attempt, err
	}
	if cause != nil {
		output.Close()
		stderr.Close()
		return attempt, cause
	}
	if result.Err != nil {
		output.Close()
		stderr.Close()
		return attempt, ErrCapture
	}
	diagnostic, err := os.ReadFile(filepath.Join(a.options.WorkRoot, stderrPath))
	if err != nil {
		output.Close()
		stderr.Close()
		return attempt, ErrStorage
	}
	if text := strings.ToLower(string(diagnostic)); strings.Contains(text, "rounded value") || strings.Contains(text, "adjusted") {
		output.Close()
		stderr.Close()
		return attempt, ErrAdjusted
	}
	if err = output.Sync(); err != nil {
		output.Close()
		stderr.Close()
		return attempt, ErrStorage
	}
	info, validateErr := captureimage.Validate(ctx, output, captureimage.Options{MaxBytes: plan.ByteBudget, MaxPixels: a.options.MaxPixels, Width: plan.Width, Height: plan.Height, Color: plan.Color})
	closeErr := errors.Join(output.Close(), stderr.Close())
	if validateErr != nil {
		return attempt, validateErr
	}
	if closeErr != nil {
		return attempt, ErrStorage
	}
	// Stderr is diagnostic scratch, not a published document artifact.
	if err = root.Remove(stderrPath); err != nil {
		return attempt, ErrStorage
	}
	for _, name := range []string{directory, "."} {
		if err = syncDirectory(root, name); err != nil {
			return attempt, err
		}
	}
	if err = ctx.Err(); err != nil {
		return attempt, err
	}
	attempt.artifact.Image = info
	attempt.valid = true
	return attempt, nil
}
func (a *Attempt) Artifact() (Artifact, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.valid || a.closed && !a.released {
		return Artifact{}, ErrNotReady
	}
	return a.artifact, nil
}
func (a *Attempt) StoppedAtMS() int64 { a.mu.Lock(); defer a.mu.Unlock(); return a.stoppedAtMS }
func (a *Attempt) Release() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.released {
		return nil
	}
	if !a.valid || a.closed || !a.stopped {
		return ErrNotReady
	}
	a.released = true
	a.closed = true
	return a.root.Close()
}
func (a *Attempt) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.released {
		return nil
	}
	if !a.stopped {
		return ErrReconciliation
	}
	a.closed = true
	if a.root == nil {
		return nil
	}
	if err := a.root.RemoveAll(a.directory); err != nil {
		return ErrStorage
	}
	if err := syncDirectory(a.root, "."); err != nil {
		return err
	}
	err := a.root.Close()
	a.root = nil
	if err != nil {
		return ErrStorage
	}
	return nil
}
func syncDirectory(root *os.Root, name string) error {
	file, err := root.Open(name)
	if err != nil {
		return ErrStorage
	}
	err = errors.Join(file.Sync(), file.Close())
	if err != nil {
		return ErrStorage
	}
	return nil
}
func absolute(s string) bool { return filepath.IsAbs(s) && filepath.Clean(s) == s && cleanString(s) }
func cleanString(s string) bool {
	if s == "" || len(s) > 4096 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func opaque(s string) bool {
	if len(s) < 16 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func dpi(n int) bool {
	switch n {
	case 75, 100, 150, 200, 300, 600:
		return true
	}
	return false
}
func paper(s string) (string, string, bool) {
	switch s {
	case "a4":
		return "210", "297", true
	case "a5":
		return "148", "210", true
	case "a6":
		return "105", "148", true
	case "letter":
		return "215.9", "279.4", true
	case "photo10x15":
		return "100", "150", true
	}
	return "", "", false
}

var _ scanner.Capturer = (*Adapter)(nil)

// DeviceID is the immutable registered identity used by this adapter.
func (a *Adapter) DeviceID() string { return a.options.DeviceID }
