//go:build linux

// Package artifactgc removes only immutable manifest files through a private
// quarantine. The worker guard and document cleanup barrier belong to the caller.
package artifactgc

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var ErrInvalid = errors.New("invalid cleanup artifact")
var ErrStorage = errors.New("cleanup storage unavailable")
var ErrMismatch = errors.New("cleanup artifact identity mismatch")
var ErrConflict = errors.New("cleanup source and quarantine both exist")

type Input struct {
	ArtifactID, RelativePath string
	Size                     int64
	SHA256                   [32]byte
}
type Removal struct {
	RemovedAtMS       int64
	JobID, ArtifactID string
	Size              int64
	SHA256            [32]byte
}
type Root struct {
	mu                      sync.Mutex
	directory               *os.File
	max                     int64
	beforeMove, afterVerify func()
	syncDir                 func(int) error
}

func OpenRoot(path string, max int64) (*Root, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || max < 1 || max > 1<<40 {
		return nil, ErrInvalid
	}
	entry, err := os.Lstat(path)
	if err != nil || !entry.IsDir() || entry.Mode().Perm()&0077 != 0 {
		return nil, ErrStorage
	}
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrStorage
	}
	info, err := file.Stat()
	if err != nil || !os.SameFile(entry, info) || !privateDirectory(int(file.Fd())) {
		file.Close()
		return nil, ErrStorage
	}
	return &Root{directory: file, max: max, syncDir: unix.Fsync}, nil
}
func (r *Root) Close() error { r.mu.Lock(); defer r.mu.Unlock(); return r.directory.Close() }
func identifier(value string) bool {
	if len(value) < 16 || len(value) > 96 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func privateDirectory(fd int) bool {
	var stat unix.Stat_t
	return unix.Fstat(fd, &stat) == nil && stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Mode&0077 == 0 && stat.Uid == uint32(os.Geteuid())
}
func openDirectory(parent int, name string) (int, error) {
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	if !privateDirectory(fd) {
		unix.Close(fd)
		return -1, ErrStorage
	}
	return fd, nil
}
func (r *Root) makeDirectory(parent int, name string) (int, error) {
	if err := unix.Mkdirat(parent, name, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return -1, ErrStorage
	}
	fd, err := openDirectory(parent, name)
	if err != nil {
		return -1, ErrStorage
	}
	if err = r.syncDir(parent); err != nil {
		unix.Close(fd)
		return -1, ErrStorage
	}
	return fd, nil
}
func statEntry(parent int, name string) (unix.Stat_t, bool, error) {
	var stat unix.Stat_t
	err := unix.Fstatat(parent, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return stat, false, nil
	}
	return stat, err == nil, err
}
func sameFile(a, b unix.Stat_t) bool { return a.Dev == b.Dev && a.Ino == b.Ino }
func validFile(stat unix.Stat_t, size int64) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&0077 == 0 && stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1 && stat.Size == size
}

// Remove quarantines with RENAME_NOREPLACE before opening/verifying the file.
// It never overwrites an occupied quarantine, follows symlinks, deletes control
// files or recursively removes directories. Retry the same job/artifact after an
// interruption: a verified quarantine resumes; confirmed absent leaves are synced.
// The private quarantine has one conforming writer under the worker guard. It is
// not a security boundary against a malicious process with the same Unix UID.
func (r *Root) Remove(ctx context.Context, job string, input Input) (Removal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !privateDirectory(int(r.directory.Fd())) {
		return Removal{}, ErrStorage
	}
	if !identifier(job) || !identifier(input.ArtifactID) || !fs.ValidPath(input.RelativePath) || input.RelativePath == "." || strings.ContainsAny(input.RelativePath, "\\\x00") || input.Size < 0 || input.Size > r.max {
		return Removal{}, ErrInvalid
	}
	parts := strings.Split(input.RelativePath, "/")
	for _, part := range parts {
		if strings.HasPrefix(part, ".") {
			return Removal{}, ErrInvalid
		}
	}
	if len(parts) > 65 || strings.HasPrefix(parts[0], ".") || strings.HasPrefix(parts[0], "documents.sqlite3") || strings.HasPrefix(parts[0], "state.sqlite3") {
		return Removal{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Removal{}, err
	}
	parent, err := unix.Dup(int(r.directory.Fd()))
	if err != nil {
		return Removal{}, ErrStorage
	}
	// Pin the ancestor chain for empty-directory retirement. Missing parents are
	// a valid absent retry after a prior successful unlink/rmdir with lost DB ack.
	directories := []int{parent}
	defer func() {
		for _, fd := range directories {
			unix.Close(fd)
		}
	}()
	missingParent := false
	for _, part := range parts[:len(parts)-1] {
		next, e := openDirectory(parent, part)
		if errors.Is(e, unix.ENOENT) {
			missingParent = true
			break
		}
		if e != nil {
			return Removal{}, ErrStorage
		}
		directories = append(directories, next)
		parent = next
	}
	leaf := parts[len(parts)-1]
	quarantine, err := r.makeDirectory(int(r.directory.Fd()), ".cleanup")
	if err != nil {
		return Removal{}, err
	}
	defer unix.Close(quarantine)
	target, err := r.makeDirectory(quarantine, job)
	if err != nil {
		return Removal{}, err
	}
	defer unix.Close(target)
	var source unix.Stat_t
	sourceExists := false
	if !missingParent {
		source, sourceExists, err = statEntry(parent, leaf)
		if err != nil {
			return Removal{}, ErrStorage
		}
	}
	staged, targetExists, err := statEntry(target, input.ArtifactID)
	if err != nil {
		return Removal{}, ErrStorage
	}
	if sourceExists && targetExists {
		return Removal{}, ErrConflict
	}
	if sourceExists {
		if !validFile(source, input.Size) {
			return Removal{}, ErrMismatch
		}
		if r.beforeMove != nil {
			r.beforeMove()
		}
		if err = ctx.Err(); err != nil {
			return Removal{}, err
		}
		if err = unix.Renameat2(parent, leaf, target, input.ArtifactID, unix.RENAME_NOREPLACE); err != nil {
			return Removal{}, ErrStorage
		}
		staged, targetExists, err = statEntry(target, input.ArtifactID)
		if err != nil || !targetExists || !sameFile(source, staged) {
			return Removal{}, ErrMismatch
		}
	}
	// Persist both sides of the rename (also on resumed or absent retries).
	if err = r.syncDir(parent); err != nil {
		return Removal{}, ErrStorage
	}
	if err = r.syncDir(target); err != nil {
		return Removal{}, ErrStorage
	}
	if targetExists {
		if !validFile(staged, input.Size) {
			return Removal{}, ErrMismatch
		}
		fd, e := unix.Openat(target, input.ArtifactID, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if e != nil {
			return Removal{}, ErrStorage
		}
		file := os.NewFile(uintptr(fd), "cleanup-artifact")
		defer file.Close()
		var opened unix.Stat_t
		if unix.Fstat(fd, &opened) != nil || !sameFile(opened, staged) || !validFile(opened, input.Size) {
			return Removal{}, ErrMismatch
		}
		hash := sha256.New()
		buffer := make([]byte, 64<<10)
		var total int64
		for {
			if err = ctx.Err(); err != nil {
				return Removal{}, err
			}
			n, e := file.Read(buffer)
			if n > 0 {
				total += int64(n)
				if total > input.Size {
					return Removal{}, ErrMismatch
				}
				hash.Write(buffer[:n])
			}
			if errors.Is(e, io.EOF) {
				break
			}
			if e != nil {
				return Removal{}, ErrStorage
			}
		}
		var digest [32]byte
		copy(digest[:], hash.Sum(nil))
		if total != input.Size || digest != input.SHA256 {
			return Removal{}, ErrMismatch
		}
		if r.afterVerify != nil {
			r.afterVerify()
		}
		if err = ctx.Err(); err != nil {
			return Removal{}, err
		}
		current, exists, e := statEntry(target, input.ArtifactID)
		if e != nil || !exists || !sameFile(opened, current) || !validFile(current, input.Size) {
			return Removal{}, ErrMismatch
		}
		if err = unix.Unlinkat(target, input.ArtifactID, 0); err != nil {
			return Removal{}, ErrStorage
		}
		if err = r.syncDir(target); err != nil {
			return Removal{}, ErrStorage
		}
	}
	if err = ctx.Err(); err != nil {
		return Removal{}, err
	}
	// No file progress proof is returned until empty ancestors are durable.
	// rmdir never recursively deletes or crosses an entry with remaining content.
	for i := len(directories) - 1; i > 0; i-- {
		removed, e := r.removeEmptyDirectory(ctx, directories[i-1], directories[i], parts[i-1])
		if e != nil {
			return Removal{}, e
		}
		if !removed {
			break
		}
	}
	removed, e := r.removeEmptyDirectory(ctx, quarantine, target, job)
	if e != nil {
		return Removal{}, e
	}
	if removed {
		if _, e = r.removeEmptyDirectory(ctx, int(r.directory.Fd()), quarantine, ".cleanup"); e != nil {
			return Removal{}, e
		}
	}
	if err = ctx.Err(); err != nil {
		return Removal{}, err
	}
	return Removal{RemovedAtMS: time.Now().UnixMilli(), JobID: job, ArtifactID: input.ArtifactID, Size: input.Size, SHA256: input.SHA256}, nil
}

// removeEmptyDirectory verifies the pinned child against its current parent
// entry and removes only that empty directory. The caller owns the worker guard;
// same-UID malicious mutation is outside this private filesystem boundary.
func (r *Root) removeEmptyDirectory(ctx context.Context, parent, child int, name string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !privateDirectory(parent) || !privateDirectory(child) {
		return false, ErrStorage
	}
	var opened unix.Stat_t
	if unix.Fstat(child, &opened) != nil {
		return false, ErrStorage
	}
	current, exists, err := statEntry(parent, name)
	if err != nil {
		return false, ErrStorage
	}
	if exists {
		if current.Mode&unix.S_IFMT != unix.S_IFDIR || !sameFile(opened, current) {
			return false, ErrMismatch
		}
		err = unix.Unlinkat(parent, name, unix.AT_REMOVEDIR)
		if errors.Is(err, unix.ENOTEMPTY) || errors.Is(err, unix.EEXIST) {
			return false, nil
		}
		if err != nil && !errors.Is(err, unix.ENOENT) {
			return false, ErrStorage
		}
	}
	if err = r.syncDir(parent); err != nil {
		return false, ErrStorage
	}
	return true, nil
}
