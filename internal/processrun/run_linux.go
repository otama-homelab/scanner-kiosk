//go:build linux

// Package processrun supervises trusted local tools. It does not reconcile scanner hardware.
package processrun

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Spec contains administrator-selected executable and validated arguments. Env is
// explicit: nil means an empty environment, never inheritance from the worker.
// Output files belong to the job workspace; callers enforce storage reservations.
type Spec struct {
	Path           string
	Args, Env      []string
	Dir            string
	Stdout, Stderr *os.File
	Grace          time.Duration
}

type Result struct {
	Err       error
	Cancelled bool
	// GroupStopped requires ESRCH after cleanup. False means reconciliation is
	// required; callers must not release a device lease based on immediate-child exit.
	GroupStopped bool
}

func Run(ctx context.Context, spec Spec) Result {
	if err := ctx.Err(); err != nil {
		return Result{Err: err, Cancelled: true, GroupStopped: true}
	}
	if spec.Grace <= 0 {
		return Result{Err: errors.New("positive stop grace required"), GroupStopped: true}
	}
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Env = append([]string{}, spec.Env...)
	cmd.Dir = spec.Dir
	if spec.Stdout != nil {
		cmd.Stdout = spec.Stdout
	}
	if spec.Stderr != nil {
		cmd.Stderr = spec.Stderr
	}
	// File-backed output avoids Wait hanging on a grandchild holding a Go pipe.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return Result{Err: err, GroupStopped: true}
	}
	pgid := cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var result Result
	select {
	case result.Err = <-done:
	case <-ctx.Done():
		result.Cancelled = true
		result.Err = ctx.Err()
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		timer := time.NewTimer(spec.Grace)
		select {
		case <-done:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			<-done // reap the immediate child, including after escalation
		}
	}
	// Also remove descendants left by a normally exited parent. Never interpret
	// parent success as proof that its process group is gone.
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	result.GroupStopped = errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
	return result
}
