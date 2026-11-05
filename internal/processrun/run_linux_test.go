//go:build linux

package processrun

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestHelper(t *testing.T) {
	mode := os.Getenv("PROCESSRUN_HELPER")
	if mode == "" {
		return
	}
	if mode == "orphan" {
		child := exec.Command("/bin/sleep", "60")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(7)
		}
		_ = os.WriteFile(os.Getenv("READY"), []byte(strconv.Itoa(child.Process.Pid)), 0600)
		os.Exit(0)
	}
	if mode == "stall" {
		signal.Ignore(syscall.SIGTERM)
		_ = os.WriteFile(os.Getenv("READY"), []byte("ready"), 0600)
		<-time.After(time.Hour)
	}
	if mode == "env" {
		if os.Getenv("PROCESSRUN_SECRET") != "" {
			os.Exit(8)
		}
		os.Exit(0)
	}
	os.Exit(9)
}

func TestCancelEscalatesAndReaps(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan Result, 1)
	go func() {
		done <- Run(ctx, Spec{Path: os.Args[0], Args: []string{"-test.run=^TestHelper$"}, Env: []string{"PROCESSRUN_HELPER=stall", "READY=" + ready}, Grace: 50 * time.Millisecond})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper not ready")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case result := <-done:
		if !result.Cancelled || !errors.Is(result.Err, context.Canceled) || !result.GroupStopped {
			t.Fatalf("unexpected result: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation hung")
	}
}

func TestEnvironmentDoesNotInheritSecrets(t *testing.T) {
	t.Setenv("PROCESSRUN_SECRET", "must-not-reach-tool")
	result := Run(context.Background(), Spec{Path: os.Args[0], Args: []string{"-test.run=^TestHelper$"}, Env: []string{"PROCESSRUN_HELPER=env"}, Grace: time.Second})
	if result.Err != nil || !result.GroupStopped {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestAlreadyCancelledDoesNotLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := Run(ctx, Spec{Path: "/does-not-exist", Grace: time.Second})
	if !errors.Is(result.Err, context.Canceled) || !result.GroupStopped {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestNormalExitAlsoStopsDescendant(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "child")
	output, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	result := Run(context.Background(), Spec{Path: os.Args[0], Args: []string{"-test.run=^TestHelper$"}, Env: []string{"PROCESSRUN_HELPER=orphan", "READY=" + ready}, Stdout: output, Stderr: output, Grace: time.Second})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	pid, err := os.ReadFile(ready)
	if err != nil {
		t.Fatal(err)
	}
	// An orphan may remain a zombie until the container init reaps it. This is
	// deliberately not reported as GroupStopped: the coordinator must reconcile.
	deadline := time.Now().Add(3 * time.Second)
	for {
		stat, err := os.ReadFile("/proc/" + string(pid) + "/stat")
		if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		tail := string(stat)[strings.LastIndex(string(stat), ")")+2:]
		if strings.HasPrefix(tail, "Z ") {
			if result.GroupStopped {
				t.Fatal("claimed zombie group was gone")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("descendant remains alive")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
