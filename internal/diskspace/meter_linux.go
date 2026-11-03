//go:build linux

// Package diskspace measures unprivileged free bytes on the pinned document
// filesystem. It is an admission observation, not a filesystem quota.
package diskspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

var ErrUnavailable = errors.New("document filesystem capacity unavailable")

type Meter struct {
	mu       sync.RWMutex
	fd       int
	headroom int64
	closed   bool
}

func Open(root string, headroom int64) (*Meter, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || headroom < 0 {
		return nil, ErrUnavailable
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return nil, ErrUnavailable
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &Meter{fd: fd, headroom: headroom}, nil
}

// Available deducts deployment headroom and uses Bavail, not root-only reserved
// blocks. It checks cancellation before/after the syscall; it cannot interrupt a
// hung kernel filesystem. Only supported local, stable mounts are valid inputs.
func (m *Meter) Available(ctx context.Context) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return 0, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var stat unix.Statfs_t
	if err := unix.Fstatfs(m.fd, &stat); err != nil {
		return 0, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	unit := stat.Frsize
	if unit <= 0 {
		unit = stat.Bsize
	}
	if unit <= 0 {
		return 0, ErrUnavailable
	}
	return usableBytes(stat.Bavail, uint64(unit), m.headroom), nil
}
func usableBytes(blocks, unit uint64, headroom int64) int64 {
	const max = uint64(1<<63 - 1)
	bytes := max
	if unit != 0 && blocks <= max/unit {
		bytes = blocks * unit
	}
	if headroom < 0 || bytes <= uint64(headroom) {
		return 0
	}
	return int64(bytes) - headroom
}
func (m *Meter) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	if err := unix.Close(m.fd); err != nil {
		return ErrUnavailable
	}
	return nil
}
