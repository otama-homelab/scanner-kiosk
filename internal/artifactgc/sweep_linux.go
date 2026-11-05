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

// EmptySweep is a streaming cursor over known flat producer directories and the
// two-level GC namespace. Caller must hold the worker guard, pass startup's
// unknown-work barrier and start no new producer until Close. It never removes
// files, follows symlinks or descends into producer/native workspaces.
type EmptySweep struct {
	root         *Root
	frames       []*os.File // independent root enumeration, then optional .cleanup
	done, closed bool
	beforeRemove func() // deterministic filesystem-race injection in package tests
}
type SweepProgress struct {
	Examined, Removed int
	Done              bool
}

func generatedDirectory(name string) bool {
	suffix := ""
	switch {
	case strings.HasPrefix(name, "work_"):
		suffix = strings.TrimPrefix(name, "work_")
	case strings.HasPrefix(name, "capture_"):
		suffix = strings.TrimPrefix(name, "capture_")
	default:
		return false
	}
	return generatedNonce(suffix)
}

func generatedNonce(suffix string) bool {
	if len(suffix) != 48 {
		return false
	}
	for _, c := range suffix {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func privateEntry(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Mode&0077 == 0 && stat.Uid == uint32(os.Geteuid())
}

func (r *Root) StartEmptySweep(ctx context.Context) (*EmptySweep, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !privateDirectory(int(r.directory.Fd())) {
		return nil, ErrStorage
	}
	// Also settle a prior rmdir whose parent sync acknowledgement was lost, even
	// when that directory name is now absent and enumeration cannot rediscover it.
	if err := r.syncDir(int(r.directory.Fd())); err != nil {
		return nil, ErrStorage
	}
	fd, err := openDirectory(int(r.directory.Fd()), ".")
	if err != nil {
		return nil, ErrStorage
	}
	return &EmptySweep{root: r, frames: []*os.File{os.NewFile(uintptr(fd), "empty-sweep-root")}}, nil
}
func (s *EmptySweep) Close() error {
	s.root.mu.Lock()
	defer s.root.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var result error
	for _, file := range s.frames {
		result = errors.Join(result, file.Close())
	}
	s.frames = nil
	return result
}
func (s *EmptySweep) cleanupCurrent() error {
	if len(s.frames) != 2 {
		return nil
	}
	var opened unix.Stat_t
	if unix.Fstat(int(s.frames[1].Fd()), &opened) != nil {
		return ErrStorage
	}
	current, exists, err := statEntry(int(s.frames[0].Fd()), ".cleanup")
	if err != nil {
		return ErrStorage
	}
	if !exists || !sameFile(opened, current) || !privateEntry(current) {
		return ErrMismatch
	}
	return nil
}
func (s *EmptySweep) remove(ctx context.Context, parent int, name string) (bool, error) {
	entry, exists, err := statEntry(parent, name)
	if err != nil {
		return false, ErrStorage
	}
	// Foreign/public directories, regular files and links are not cleanup input.
	if !exists || !privateEntry(entry) {
		return false, nil
	}
	fd, err := openDirectory(parent, name)
	if err != nil {
		return false, ErrStorage
	}
	defer unix.Close(fd)
	var opened unix.Stat_t
	if unix.Fstat(fd, &opened) != nil {
		return false, ErrStorage
	}
	if !sameFile(entry, opened) {
		return false, ErrMismatch
	}
	if s.beforeRemove != nil {
		s.beforeRemove()
	}
	if err = s.cleanupCurrent(); err != nil {
		return false, err
	}
	return s.root.removeEmptyDirectory(ctx, parent, fd, name)
}

// Step examines at most limit directory entries (1..256), including ignored
// control/unknown names. check revalidates caller authority before every step.
// Directory descriptors/memory stay bounded regardless of volume size. Partial
// errors retain the cursor; startup stops, and a fresh sweep safely retries later.
func (s *EmptySweep) Step(ctx context.Context, limit int, check func(context.Context) error) (SweepProgress, error) {
	s.root.mu.Lock()
	defer s.root.mu.Unlock()
	var result SweepProgress
	if s.closed || limit < 1 || limit > 256 || check == nil {
		return result, ErrInvalid
	}
	if !privateDirectory(int(s.root.directory.Fd())) {
		return result, ErrStorage
	}
	if s.done {
		return SweepProgress{Done: true}, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := check(ctx); err != nil {
		return result, err
	}
	// A caller retrying this cursor after a lost rmdir acknowledgement must
	// settle parent metadata even if readdir has already passed the absent name.
	for _, frame := range s.frames {
		if err := s.root.syncDir(int(frame.Fd())); err != nil {
			return result, ErrStorage
		}
	}
	for result.Examined < limit {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := check(ctx); err != nil {
			return result, err
		}
		if err := s.cleanupCurrent(); err != nil {
			return result, err
		}
		current := s.frames[len(s.frames)-1]
		names, err := current.Readdirnames(1)
		if err != nil && !errors.Is(err, io.EOF) {
			return result, ErrStorage
		}
		if len(names) == 0 {
			if len(s.frames) == 1 {
				s.done = true
				result.Done = true
				return result, nil
			}
			// The complete .cleanup namespace was visited; only its empty container
			// can be removed. Any file or unknown/nonempty child keeps it in place.
			removed, e := s.root.removeEmptyDirectory(ctx, int(s.frames[0].Fd()), int(current.Fd()), ".cleanup")
			if e != nil {
				return result, e
			}
			if removed {
				result.Removed++
			}
			if e = current.Close(); e != nil {
				return result, ErrStorage
			}
			s.frames = s.frames[:1]
			continue
		}
		name := names[0]
		result.Examined++
		parent := int(current.Fd())
		if len(s.frames) == 1 && name == ".cleanup" {
			entry, exists, e := statEntry(parent, name)
			if e != nil {
				return result, ErrStorage
			}
			if !exists || !privateEntry(entry) {
				continue
			}
			fd, e := openDirectory(parent, name)
			if e != nil {
				return result, ErrStorage
			}
			file := os.NewFile(uintptr(fd), "empty-sweep-cleanup")
			s.frames = append(s.frames, file)
			if e = s.cleanupCurrent(); e != nil {
				return result, e
			}
			// Settle child rmdir from a previous interrupted sweep.
			if e = s.root.syncDir(fd); e != nil {
				return result, ErrStorage
			}
			continue
		}
		candidate := len(s.frames) == 1 && generatedDirectory(name) || len(s.frames) == 2 && identifier(name)
		if !candidate {
			continue
		}
		removed, e := s.remove(ctx, parent, name)
		if e != nil {
			return result, e
		}
		if removed {
			result.Removed++
		}
	}
	return result, nil
}
