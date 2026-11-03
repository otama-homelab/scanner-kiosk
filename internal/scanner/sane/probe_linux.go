//go:build linux

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
	"scanner-kiosk/internal/processrun"
	"time"
)

// ProbeAttempt owns inspection scratch until the caller confirms local stop and
// removes it with Close. It has no publishable artifact or transfer operation.
type ProbeAttempt struct{ owned *Attempt }

func (p *ProbeAttempt) Close() error       { return p.owned.Close() }
func (p *ProbeAttempt) StoppedAtMS() int64 { return p.owned.StoppedAtMS() }

// Probe opens only the configured device and lists options with -A. Unlike
// --help, this command exits before scanimage's global device-list enumeration.
// No acquisition, diagnostic scan, source change or option-setting retry occurs.
// A returned attempt must be retained/closed even when capability parsing fails.
func (a *Adapter) Probe(ctx context.Context) (Capabilities, *ProbeAttempt, error) {
	return a.probe(ctx, nil)
}

// Selection contains application enums only, never arbitrary native options.
type Selection struct {
	Size, Color string
	DPI         int
}

// ProbeSelection applies the requested flatbed/mode/resolution/area before
// listing the resulting active constraints. It does not start acquisition or
// establish expected pixel dimensions, device readiness or physical idle.
func (a *Adapter) ProbeSelection(ctx context.Context, selection Selection) (Capabilities, *ProbeAttempt, error) {
	if _, err := selectionArguments(selection); err != nil {
		return Capabilities{}, nil, err
	}
	return a.probe(ctx, &selection)
}
func (a *Adapter) probe(ctx context.Context, selection *Selection) (Capabilities, *ProbeAttempt, error) {
	args := []string{"--device-name=" + a.options.DeviceName, "--format=png"}
	if selection != nil {
		options, _ := selectionArguments(*selection)
		args = append(args, options...)
	}
	args = append(args, "--all-options")
	data, attempt, err := a.inspect(ctx, a.options.Binary, args)
	if err != nil {
		return Capabilities{}, attempt, err
	}
	caps, err := parseCapabilities(data, a.options.DeviceName)
	if err == nil && selection != nil {
		err = validateSelection(data, caps, *selection)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return Capabilities{}, attempt, err
	}
	return caps, attempt, nil
}
func (a *Adapter) inspect(ctx context.Context, binary string, args []string) ([]byte, *ProbeAttempt, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(a.options.WorkRoot)
	if err != nil {
		return nil, nil, ErrStorage
	}
	var nonce [24]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		root.Close()
		return nil, nil, ErrStorage
	}
	directory := "probe_" + hex.EncodeToString(nonce[:])
	if err = root.Mkdir(directory, 0700); err != nil {
		root.Close()
		return nil, nil, ErrStorage
	}
	owned := &Attempt{root: root, directory: directory, stopped: true, stoppedAtMS: time.Now().UnixMilli()}
	attempt := &ProbeAttempt{owned: owned}
	output, err := root.OpenFile(path.Join(directory, "options"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, attempt, ErrStorage
	}
	stderr, err := root.OpenFile(path.Join(directory, "stderr"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		output.Close()
		return nil, attempt, ErrStorage
	}
	ctx, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()
	running, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	limit := min(a.options.MaxBytes, int64(256<<10))
	check := func() error {
		out, err := output.Stat()
		if err != nil {
			return ErrStorage
		}
		diagnostic, err := stderr.Stat()
		if err != nil {
			return ErrStorage
		}
		if out.Size() > limit || diagnostic.Size() > limit-out.Size() {
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
			case <-running.Done():
				return
			case <-ticker.C:
				if err := check(); err != nil {
					stop(err)
					return
				}
			}
		}
	}()
	env := append(append([]string(nil), a.env...), "TMPDIR="+filepath.Join(a.options.WorkRoot, directory))
	owned.stopped = false
	owned.stoppedAtMS = 0
	result := processrun.Run(running, processrun.Spec{Path: binary, Args: args, Env: env, Dir: filepath.Join(a.options.WorkRoot, directory), Stdout: output, Stderr: stderr, Grace: a.options.StopGrace})
	cause := context.Cause(running)
	stop(nil)
	<-joined
	owned.stopped = result.GroupStopped
	if owned.stopped {
		owned.stoppedAtMS = time.Now().UnixMilli()
	}
	defer output.Close()
	defer stderr.Close()
	if !owned.stopped {
		return nil, attempt, ErrReconciliation
	}
	if err = check(); err != nil {
		return nil, attempt, err
	}
	if cause != nil {
		return nil, attempt, cause
	}
	if result.Err != nil {
		return nil, attempt, ErrCapabilities
	}
	if _, err = output.Seek(0, io.SeekStart); err != nil {
		return nil, attempt, ErrStorage
	}
	data, err := io.ReadAll(io.LimitReader(output, limit+1))
	if err != nil {
		return nil, attempt, ErrStorage
	}
	// Successful inspection must have no warning/error diagnostics to reinterpret.
	diagnostic, err := stderr.Stat()
	if err != nil || diagnostic.Size() != 0 {
		return nil, attempt, ErrCapabilities
	}
	closeErr := errors.Join(output.Close(), stderr.Close())
	if closeErr != nil {
		return nil, attempt, ErrStorage
	}
	if err = ctx.Err(); err != nil {
		return nil, attempt, err
	}
	return data, attempt, nil
}
