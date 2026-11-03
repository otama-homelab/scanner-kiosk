//go:build linux

// Package artifactread opens verified immutable artifacts below a pinned private
// root. It never creates output directories or accepts caller-selected paths.
package artifactread

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var ErrUnavailable = errors.New("artifact unavailable")

type Input struct {
	RelativePath string
	Size         int64
	SHA256       [32]byte
}
type Root struct {
	root *os.Root
	max  int64
}

func OpenRoot(path string, max int64) (*Root, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || max < 1 || max > 1<<40 {
		return nil, ErrUnavailable
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrUnavailable
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &Root{root, max}, nil
}
func (r *Root) Close() error { return r.root.Close() }

// Open verifies the opened descriptor, not a path inspected before opening.
// Producers must keep ready artifacts immutable after publication.
func (r *Root) Open(ctx context.Context, input Input) (io.ReadCloser, error) {
	if input.Size < 0 || input.Size > r.max || !filepath.IsLocal(input.RelativePath) || filepath.Clean(input.RelativePath) != input.RelativePath || strings.ContainsAny(input.RelativePath, "\\\x00") {
		return nil, ErrUnavailable
	}
	// Root.OpenFile follows confined symlinks even with O_NOFOLLOW supplied.
	// Reject non-regular directory entries and bind the opened descriptor to
	// that entry. os.Root separately prevents traversal outside the pinned root.
	entry, err := r.root.Lstat(input.RelativePath)
	if err != nil || !entry.Mode().IsRegular() {
		return nil, ErrUnavailable
	}
	file, err := r.root.OpenFile(input.RelativePath, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	ok := false
	defer func() {
		if !ok {
			file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(entry, info) || info.Size() != input.Size || info.Mode().Perm()&0077 != 0 {
		return nil, ErrUnavailable
	}
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, e := file.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > input.Size {
				return nil, ErrUnavailable
			}
			hash.Write(buffer[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, ErrUnavailable
		}
	}
	if total != input.Size || subtle.ConstantTimeCompare(hash.Sum(nil), input.SHA256[:]) != 1 {
		return nil, ErrUnavailable
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, ErrUnavailable
	}
	ok = true
	return &reader{file, ctx, input.Size}, nil
}

type reader struct {
	file      *os.File
	ctx       context.Context
	remaining int64
}

func (r *reader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.file.Read(p)
	r.remaining -= int64(n)
	if err != nil && err != io.EOF {
		return n, ErrUnavailable
	}
	if err == io.EOF && r.remaining != 0 {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}
func (r *reader) Close() error { return r.file.Close() }

// OpenRange verifies the entire immutable artifact, then restricts the returned
// reader to the requested interval. Returned reads cannot exceed that interval.
func (r *Root) OpenRange(ctx context.Context, input Input, start, length int64) (io.ReadCloser, error) {
	if start < 0 || length < 0 || start > input.Size || length > input.Size-start {
		return nil, ErrUnavailable
	}
	opened, err := r.Open(ctx, input)
	if err != nil {
		return nil, err
	}
	reader := opened.(*reader)
	if _, err = reader.file.Seek(start, io.SeekStart); err != nil {
		reader.Close()
		return nil, ErrUnavailable
	}
	reader.remaining = length
	return reader, nil
}
