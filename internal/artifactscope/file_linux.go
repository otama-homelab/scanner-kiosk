//go:build linux

package artifactscope

import (
	"context"
	"io"
	"os"
)

// FileArtifact is one scope-owned output for a supervised file-backed producer.
// The bound is charged before handoff. The supervisor must enforce it in the
// child and prove the entire producer group stopped before CloseAfterStop.
type FileArtifact struct{ writer *writer }

func (s *Scope) CreateFileArtifact(ctx context.Context, kind string, bound int64) (*FileArtifact, error) {
	if kind != PDF && kind != JPEG && kind != PNG {
		return nil, ErrNotReady
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.active(ctx); err != nil {
		return nil, err
	}
	if bound < 1 || bound > s.limits.MaxOutputBytes || bound > s.limits.MaxTotalOutputBytes-s.used {
		return nil, ErrLimit
	}
	if s.budget != nil && !s.budget.reserve(bound) {
		return nil, ErrLimit
	}
	w, err := s.createLocked(ctx, kind, os.O_RDWR)
	if err != nil {
		if s.budget != nil {
			s.budget.refund(bound)
		}
		return nil, err
	}
	w.nativeLimit = bound
	s.used += bound
	return &FileArtifact{writer: w}, nil
}
func (f *FileArtifact) File() *os.File    { return f.writer.file }
func (f *FileArtifact) Reference() string { return f.writer.Reference() }

// CloseAfterStop verifies the still-pinned file, hashes/syncs/closes it and
// renames it in place. Failure keeps the full charge until confirmed deletion.
// Scope.Close refuses to remove a pending native output, including after cancel.
func (f *FileArtifact) CloseAfterStop() error {
	w := f.writer
	s := w.scope
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.done {
		return w.err
	}
	bound := w.nativeLimit
	err := s.active(w.ctx)
	var size int64
	if err == nil {
		info, statErr := w.file.Stat()
		entry, entryErr := s.root.Lstat(w.metadata.RelativePath + ".tmp")
		if statErr != nil || entryErr != nil || !info.Mode().IsRegular() || !entry.Mode().IsRegular() || !os.SameFile(info, entry) || info.Mode().Perm()&0077 != 0 {
			err = ErrStorage
		} else if info.Size() > bound {
			err = ErrLimit
		} else {
			size = info.Size()
			var total int64
			reader := io.NewSectionReader(w.file, 0, size)
			buffer := make([]byte, 64<<10)
			for {
				if err = w.ctx.Err(); err != nil {
					break
				}
				n, e := reader.Read(buffer)
				if n > 0 {
					total += int64(n)
					w.hash.Write(buffer[:n])
				}
				if e == io.EOF {
					break
				}
				if e != nil {
					err = ErrStorage
					break
				}
			}
			if err == nil && total != size {
				err = ErrStorage
			}
			if err == nil {
				w.metadata.Size = size
			}
		}
	}
	if err != nil {
		w.err = err
	}
	err = w.closeLocked()
	if err == nil {
		s.used -= bound - size
		if s.budget != nil {
			s.budget.refund(bound - size)
		}
	}
	return err
}
