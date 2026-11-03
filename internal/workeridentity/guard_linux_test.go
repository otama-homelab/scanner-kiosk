//go:build linux

package workeridentity

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func root(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	if err := os.Chmod(r, 0700); err != nil {
		t.Fatal(err)
	}
	return r
}
func TestLifetimeExclusionAndFreshNonce(t *testing.T) {
	r := root(t)
	first, err := Acquire(r)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err = Acquire(r); !errors.Is(err, ErrActive) {
		t.Fatal("duplicate coordinator acquired root")
	}
	id := first.ID()
	if len(id) != 55 {
		t.Fatal("unexpected nonce size")
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	if first.ID() != "" {
		t.Fatal("closed guard still offered a ready boot identity")
	}
	second, err := Acquire(r)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.ID() == id {
		t.Fatal("boot identity reused")
	}
	info, err := os.Stat(filepath.Join(r, ".worker.lock"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("lock was unlinked or not private")
	}
}
func TestUnsafeRootsAndLockSymlinksRejected(t *testing.T) {
	r := root(t)
	public := root(t)
	os.Chmod(public, 0755)
	link := filepath.Join(root(t), "link")
	os.Symlink(r, link)
	for _, p := range []string{public, link, "relative"} {
		if guard, err := Acquire(p); err == nil {
			guard.Close()
			t.Fatal("unsafe root accepted")
		}
	}
	os.WriteFile(filepath.Join(r, "target"), nil, 0600)
	os.Symlink("target", filepath.Join(r, ".worker.lock"))
	if guard, err := Acquire(r); err == nil {
		guard.Close()
		t.Fatal("symlink lock accepted")
	}
}
func TestLockHelper(t *testing.T) {
	r := os.Getenv("SCANNER_WORKER_LOCK_TEST_ROOT")
	if r == "" {
		return
	}
	g, err := Acquire(r)
	if err != nil {
		os.Exit(8)
	}
	if err = os.WriteFile(filepath.Join(r, "ready"), []byte(g.ID()), 0600); err != nil {
		os.Exit(9)
	}
	io.Copy(io.Discard, os.Stdin)
	g.Close()
	os.Exit(0)
}
func TestCrossProcessLockAndCrashRelease(t *testing.T) {
	r := root(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelper$")
	cmd.Env = []string{"SCANNER_WORKER_LOCK_TEST_ROOT=" + r}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stdin.Close(); cmd.Process.Kill(); cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	var old []byte
	for {
		old, err = os.ReadFile(filepath.Join(r, "ready"))
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not acquire lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err = Acquire(r); !errors.Is(err, ErrActive) {
		t.Fatal("cross-process exclusion failed")
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	next, err := Acquire(r)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.ID() == string(old) {
		t.Fatal("crash restart reused nonce")
	}
}

func TestGuardRootBindingIncludesDirectoryAndLockIdentity(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	for _, root := range []string{first, second} {
		if err := os.Chmod(root, 0700); err != nil {
			t.Fatal(err)
		}
	}
	guard, err := Acquire(first)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if !guard.MatchesRoot(first) {
		t.Fatal("own root rejected")
	}
	if err = os.Link(filepath.Join(first, ".worker.lock"), filepath.Join(second, ".worker.lock")); err != nil {
		t.Fatal(err)
	}
	if guard.MatchesRoot(second) {
		t.Fatal("hard-linked lock substituted for root binding")
	}
	if err = guard.Close(); err != nil {
		t.Fatal(err)
	}
	if guard.MatchesRoot(first) {
		t.Fatal("closed guard retained authority")
	}
}
