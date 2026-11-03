//go:build linux

// Package workeridentity owns a local coordinator lock and fresh process nonce.
// Neither lock acquisition nor a dead coordinator proves scanner work has stopped.
package workeridentity

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

var (
	ErrActive  = errors.New("another local scanner coordinator is active")
	ErrStorage = errors.New("worker identity storage unavailable")
)

type Guard struct {
	mu       sync.Mutex
	file     *os.File
	id       string
	closed   bool
	rootInfo os.FileInfo
}

// Acquire requires a private root on a supported local filesystem shared by the
// API/worker deployment. It never initializes leases or marks the worker ready.
// The lock filename must never be unlinked while any coordinator may be alive.
func Acquire(rootPath string) (*Guard, error) {
	if !filepath.IsAbs(rootPath) || filepath.Clean(rootPath) != rootPath {
		return nil, ErrStorage
	}
	info, err := os.Lstat(rootPath)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrStorage
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, ErrStorage
	}
	defer root.Close()
	directory, err := root.Open(".")
	if err != nil {
		return nil, ErrStorage
	}
	rootInfo, err := directory.Stat()
	closeErr := directory.Close()
	if err != nil || closeErr != nil || !rootInfo.IsDir() || rootInfo.Mode().Perm()&0077 != 0 {
		return nil, ErrStorage
	}
	if info, err = root.Lstat(".worker.lock"); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, ErrStorage
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrStorage
	}
	file, err := root.OpenFile(".worker.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, ErrStorage
	}
	fail := func(cause error) (*Guard, error) { file.Close(); return nil, cause }
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return fail(ErrStorage)
	}
	if err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return fail(ErrActive)
		}
		return fail(ErrStorage)
	}
	var entropy [24]byte
	if _, err = rand.Read(entropy[:]); err != nil {
		return fail(ErrStorage)
	}
	return &Guard{rootInfo: rootInfo, file: file, id: "worker_" + hex.EncodeToString(entropy[:])}, nil
}

// ID is stable for this guard lifetime and fresh on every acquisition. It is a
// fencing identity, not a credential, and must not be restored from configuration.
func (g *Guard) ID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return ""
	}
	return g.id
}

// Close must follow execution drain/reconciliation in production. Kernel lock
// release alone never authorizes a successor to clear persisted device leases.
func (g *Guard) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true
	// Close releases flock. Do not unlink: a new inode would defeat exclusion.
	if err := g.file.Close(); err != nil {
		return ErrStorage
	}
	return nil
}

// WithActive holds this guard alive across a short local metadata transaction.
// Callbacks must not perform network/process work or call Guard methods.
func (g *Guard) WithActive(callback func(string) error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.file == nil {
		return ErrStorage
	}
	return callback(g.id)
}

// MatchesRoot binds a live guard to the same private directory and lock inode.
// It performs local metadata checks only; it does not prove scanner readiness.
func (g *Guard) MatchesRoot(rootPath string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.file == nil || g.rootInfo == nil || !filepath.IsAbs(rootPath) || filepath.Clean(rootPath) != rootPath {
		return false
	}
	info, err := os.Lstat(rootPath)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return false
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return false
	}
	defer root.Close()
	directory, err := root.Open(".")
	if err != nil {
		return false
	}
	defer directory.Close()
	info, err = directory.Stat()
	if err != nil || !os.SameFile(info, g.rootInfo) {
		return false
	}
	current, err := root.Lstat(".worker.lock")
	if err != nil || !current.Mode().IsRegular() || current.Mode().Perm()&0077 != 0 {
		return false
	}
	original, err := g.file.Stat()
	return err == nil && os.SameFile(current, original)
}
