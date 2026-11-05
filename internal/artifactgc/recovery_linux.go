//go:build linux

package artifactgc

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// RecoverUnpublished is an offline administrative operation. The caller must
// hold the worker guard and independently confirm the old runtime and ALL its
// children have stopped. A lock or physical Idle observation alone is not proof.
// Registered directories are preserved in full; only flat generated scratch is
// eligible. Unknown entries leave recovery blocked, never recursively deleted.
func (r *Root) RecoverUnpublished(ctx context.Context, unreferenced func(context.Context, string) (bool, error), check func(context.Context) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if unreferenced == nil || check == nil {
		return ErrInvalid
	}
	if err := check(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !privateDirectory(int(r.directory.Fd())) {
		return ErrStorage
	}
	fd, err := openDirectory(int(r.directory.Fd()), ".")
	if err != nil {
		return ErrStorage
	}
	directory := os.NewFile(uintptr(fd), "recovery-root")
	defer directory.Close()
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = check(ctx); err != nil {
			return err
		}
		names, readErr := directory.Readdirnames(64)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return ErrStorage
		}
		for _, name := range names {
			generated := generatedDirectory(name)
			if strings.HasPrefix(name, "probe_") {
				generated = generatedDirectory("work_" + strings.TrimPrefix(name, "probe_"))
			}
			if !generated {
				continue
			}
			allowed, e := unreferenced(ctx, name)
			if e != nil {
				return e
			}
			if !allowed {
				continue
			}
			if e = r.removeUnpublishedDirectory(ctx, name, check); e != nil {
				return e
			}
		}
		if errors.Is(readErr, io.EOF) {
			return r.syncDir(int(r.directory.Fd()))
		}
	}
}

func (r *Root) removeUnpublishedDirectory(ctx context.Context, name string, check func(context.Context) error) error {
	parent := int(r.directory.Fd())
	before, exists, err := statEntry(parent, name)
	if err != nil {
		return ErrStorage
	}
	if !exists {
		return nil
	}
	if !privateEntry(before) {
		return ErrStorage
	}
	fd, err := openDirectory(parent, name)
	if err != nil {
		return ErrStorage
	}
	directory := os.NewFile(uintptr(fd), "recovery-scratch")
	defer directory.Close()
	var pinned unix.Stat_t
	if unix.Fstat(fd, &pinned) != nil || pinned.Dev != before.Dev || pinned.Ino != before.Ino {
		return ErrStorage
	}
	names, err := directory.Readdirnames(4097)
	if err != nil && !errors.Is(err, io.EOF) || len(names) > 4096 {
		return ErrStorage
	}
	entries := make(map[string]unix.Stat_t, len(names))
	var total int64
	for _, file := range names {
		if err = ctx.Err(); err != nil {
			return err
		}
		info, exists, e := statEntry(fd, file)
		if e != nil || !exists || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&0077 != 0 || info.Uid != uint32(os.Geteuid()) || info.Nlink != 1 || info.Size < 0 || info.Size > r.max-total {
			return ErrStorage
		}
		total += info.Size
		entries[file] = info
	}
	for _, file := range names {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = check(ctx); err != nil {
			return err
		}
		current, exists, e := statEntry(fd, file)
		expected := entries[file]
		if e != nil || !exists || current.Dev != expected.Dev || current.Ino != expected.Ino || current.Mode != expected.Mode || current.Size != expected.Size || current.Mtim != expected.Mtim {
			return ErrMismatch
		}
		if unix.Unlinkat(fd, file, 0) != nil {
			return ErrStorage
		}
	}
	if r.syncDir(fd) != nil {
		return ErrStorage
	}
	current, exists, err := statEntry(parent, name)
	if err != nil || !exists || current.Dev != pinned.Dev || current.Ino != pinned.Ino {
		return ErrMismatch
	}
	if err = check(ctx); err != nil {
		return err
	}
	if unix.Unlinkat(parent, name, unix.AT_REMOVEDIR) != nil {
		return ErrStorage
	}
	return r.syncDir(parent)
}
