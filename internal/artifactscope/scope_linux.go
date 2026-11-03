//go:build linux

// Package artifactscope implements job-scoped OCR artifact access. It does not
// authenticate owners or publish documents; the coordinator supplies an authorized
// immutable allowlist and commits sealed outputs with its revision/expiry checks.
package artifactscope

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"scanner-kiosk/pkg/ocrplugin"
)

var (
	ErrClosed   = errors.New("artifact scope closed")
	ErrLimit    = errors.New("artifact scope limit exceeded")
	ErrNotReady = errors.New("artifact scope not ready")
	ErrInput    = errors.New("artifact input unavailable or invalid")
	ErrStorage  = errors.New("artifact storage operation failed")
)

type Input struct {
	RelativePath string
	Size         int64
	SHA256       [32]byte
}
type Limits struct {
	MaxInputBytes, MaxOutputBytes, MaxTotalOutputBytes int64
	MaxOutputs                                         int
}
type Artifact struct {
	ID, RelativePath string
	Kind             string
	Size             int64
	SHA256           [32]byte
}
type Scope struct {
	scratch                  map[*scratchReservation]bool
	budget                   *Budget
	mu                       sync.Mutex
	root                     *os.Root
	directory                string
	inputs                   map[string]Input
	limits                   Limits
	readers                  map[*reader]bool
	outputs                  []*writer
	used                     int64
	sealed, closed, released bool
	failure                  error
}
type reader struct {
	scope  *Scope
	file   *os.File
	ctx    context.Context
	closed bool
}
type writer struct {
	scope    *Scope
	file     *os.File
	ctx      context.Context
	metadata Artifact
	hash     hash.Hash
	done     bool
	err      error
}

func New(rootPath string, inputs map[string]Input, limits Limits) (*Scope, error) {
	return newScope(rootPath, inputs, limits, nil)
}
func newScope(rootPath string, inputs map[string]Input, limits Limits, budget *Budget) (*Scope, error) {
	if limits.MaxInputBytes < 1 || limits.MaxInputBytes > 1<<40 || limits.MaxOutputBytes < 1 || limits.MaxTotalOutputBytes < limits.MaxOutputBytes || limits.MaxOutputs < 1 || limits.MaxOutputs > 10000 || len(inputs) > 10000 {
		return nil, ErrLimit
	}
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
	scope := &Scope{budget: budget, root: root, inputs: map[string]Input{}, limits: limits, readers: map[*reader]bool{}}
	for id, input := range inputs {
		if !validID(id) || !validPath(input.RelativePath) || input.Size < 0 || input.Size > limits.MaxInputBytes {
			root.Close()
			return nil, ErrInput
		}
		scope.inputs[id] = input
	}
	name, err := randomID("work_")
	if err != nil {
		root.Close()
		return nil, ErrStorage
	}
	if err = root.Mkdir(name, 0700); err != nil {
		root.Close()
		return nil, ErrStorage
	}
	scope.directory = name
	return scope, nil
}
func (s *Scope) OpenInput(ctx context.Context, id string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.active(ctx); err != nil {
		return nil, err
	}
	input, ok := s.inputs[id]
	if !ok {
		return nil, ErrInput
	}
	info, err := s.root.Lstat(input.RelativePath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrInput
	}
	file, err := s.root.OpenFile(input.RelativePath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrInput
	}
	bad := func() (io.ReadCloser, error) { file.Close(); return nil, ErrInput }
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != input.Size || info.Mode().Perm()&0077 != 0 {
		return bad()
	}
	// Verify the opened descriptor before handing any bytes to a provider. Inputs
	// must remain immutable after this check; writable capture originals are forbidden.
	digest := sha256.New()
	buffer := make([]byte, 64<<10)
	var read int64
	for {
		if ctx.Err() != nil {
			file.Close()
			return nil, ctx.Err()
		}
		n, e := file.Read(buffer)
		if n > 0 {
			read += int64(n)
			if read > input.Size {
				return bad()
			}
			digest.Write(buffer[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return bad()
		}
	}
	if read != input.Size || !equalDigest(digest.Sum(nil), input.SHA256) {
		return bad()
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return bad()
	}
	r := &reader{scope: s, file: file, ctx: ctx}
	s.readers[r] = true
	return r, nil
}

// CreateOutput implements the OCR-specific port; it cannot create ordinary PDFs.
func (s *Scope) CreateOutput(ctx context.Context, kind ocrplugin.OutputKind) (ocrplugin.ArtifactWriter, error) {
	if kind != ocrplugin.Text && kind != ocrplugin.PositionedText && kind != ocrplugin.PDFTextLayer && kind != ocrplugin.SearchablePDF {
		return nil, ErrNotReady
	}
	return s.create(ctx, string(kind))
}

const (
	PDF  = "pdf"
	JPEG = "jpeg"
	PNG  = "png"
)

// CreateArtifact creates a native document output independently of OCR capabilities.
func (s *Scope) CreateArtifact(ctx context.Context, kind string) (ocrplugin.ArtifactWriter, error) {
	if kind != PDF && kind != JPEG && kind != PNG {
		return nil, ErrNotReady
	}
	return s.create(ctx, kind)
}
func (s *Scope) create(ctx context.Context, kind string) (ocrplugin.ArtifactWriter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.active(ctx); err != nil {
		return nil, err
	}
	if len(s.outputs) >= s.limits.MaxOutputs {
		s.failure = ErrLimit
		return nil, ErrLimit
	}
	id, err := randomID("artifact_")
	if err != nil {
		s.failure = ErrStorage
		return nil, ErrStorage
	}
	relative := path.Join(s.directory, id)
	file, err := s.root.OpenFile(relative+".tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		s.failure = ErrStorage
		return nil, ErrStorage
	}
	w := &writer{scope: s, file: file, ctx: ctx, hash: sha256.New(), metadata: Artifact{ID: id, RelativePath: relative, Kind: kind}}
	s.outputs = append(s.outputs, w)
	return w, nil
}
func (s *Scope) active(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed || s.sealed {
		return ErrClosed
	}
	return s.failure
}
func (r *reader) Read(p []byte) (int, error) {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	if r.closed || r.scope.closed {
		return 0, ErrClosed
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.file.Read(p)
	if err != nil && err != io.EOF {
		return n, ErrInput
	}
	return n, err
}
func (r *reader) Close() error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	delete(r.scope.readers, r)
	if err := r.file.Close(); err != nil {
		r.scope.failure = ErrStorage
		return ErrStorage
	}
	return nil
}
func (w *writer) Reference() string { return w.metadata.ID }
func (w *writer) Write(p []byte) (int, error) {
	s := w.scope
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.done {
		return 0, ErrClosed
	}
	if err := s.active(w.ctx); err != nil {
		w.err = err
		s.failure = err
		return 0, err
	}
	if int64(len(p)) > s.limits.MaxOutputBytes-w.metadata.Size || int64(len(p)) > s.limits.MaxTotalOutputBytes-s.used {
		w.err = ErrLimit
		s.failure = ErrLimit
		return 0, ErrLimit
	}

	if s.budget != nil && !s.budget.reserve(int64(len(p))) {
		w.err = ErrLimit
		s.failure = ErrLimit
		return 0, ErrLimit
	}
	n, err := w.file.Write(p)
	if s.budget != nil {
		s.budget.refund(int64(len(p) - n))
	}
	s.used += int64(n)
	w.metadata.Size += int64(n)
	w.hash.Write(p[:n])
	if err != nil || n != len(p) {
		w.err = ErrStorage
		s.failure = ErrStorage
		return n, ErrStorage
	}
	return n, nil
}
func (w *writer) Close() error {
	s := w.scope
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.done {
		return w.err
	}
	w.done = true
	if err := s.active(w.ctx); err != nil {
		w.err = err
	}
	if w.err == nil {
		if err := w.file.Sync(); err != nil {
			w.err = ErrStorage
		}
	}
	if err := w.file.Close(); err != nil && w.err == nil {
		w.err = ErrStorage
	}
	if w.err == nil {
		if err := s.root.Rename(w.metadata.RelativePath+".tmp", w.metadata.RelativePath); err != nil {
			w.err = ErrStorage
		}
	}
	if w.err == nil {
		copy(w.metadata.SHA256[:], w.hash.Sum(nil))
	}
	if w.err != nil {
		s.failure = w.err
	}
	return w.err
}

// Seal requires every input/output handle closed and every output write successful.
// It syncs directory metadata and returns opaque, unpublished artifact descriptors.
func (s *Scope) Seal(ctx context.Context) ([]Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if s.closed {
		return nil, ErrClosed
	}
	if s.failure != nil {
		return nil, s.failure
	}
	if len(s.scratch) != 0 || len(s.readers) != 0 || len(s.outputs) == 0 {
		return nil, ErrNotReady
	}
	out := make([]Artifact, 0, len(s.outputs))
	for _, w := range s.outputs {
		if !w.done || w.err != nil {
			return nil, ErrNotReady
		}
		out = append(out, w.metadata)
	}
	if !s.sealed {
		for _, name := range []string{s.directory, "."} {
			dir, err := s.root.Open(name)
			if err != nil {
				s.failure = ErrStorage
				return nil, ErrStorage
			}
			syncErr := dir.Sync()
			dir.Close()
			if syncErr != nil {
				s.failure = ErrStorage
				return nil, ErrStorage
			}
		}
		s.sealed = true
	}
	return out, nil
}

// Release transfers file cleanup responsibility to the application after confirmed
// publication or durable reconciliation handoff. An uncertain DB commit requires
// read-back/reconciliation before deletion; it is not a confirmed failed publication.
// Call Close on every confirmed failed publication.
// Release is not authorization or a DB commit and never makes a browser URL.
func (s *Scope) Release() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.released {
		return nil
	}
	if s.closed {
		return ErrClosed
	}
	if !s.sealed {
		return ErrNotReady
	}
	s.released = true
	s.closed = true
	if err := s.root.Close(); err != nil {
		return ErrStorage
	}
	return nil
}

// Close aborts the scope, closes remaining handles and removes only its generated
// workspace. Inputs are never removed. Failed removal can be retried by Close.
func (s *Scope) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.released {
		return nil
	}
	s.closed = true
	if len(s.scratch) != 0 {
		return ErrNotReady
	}
	for r := range s.readers {
		r.file.Close()
		r.closed = true
		delete(s.readers, r)
	}
	for _, w := range s.outputs {
		if !w.done {
			w.file.Close()
			w.done = true
			w.err = ErrClosed
		}
	}
	if s.root == nil {
		return nil
	}
	if err := s.root.RemoveAll(s.directory); err != nil {
		return ErrStorage
	}
	dir, err := s.root.Open(".")
	if err != nil {
		return ErrStorage
	}
	syncErr := dir.Sync()
	dir.Close()
	if syncErr != nil {
		return ErrStorage
	}

	if s.budget != nil {
		s.budget.refund(s.used)
		s.used = 0
	}
	err = s.root.Close()
	s.root = nil
	if err != nil {
		return ErrStorage
	}
	return nil
}
func equalDigest(got []byte, want [32]byte) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
func randomID(prefix string) (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:]), nil
}
func validID(id string) bool {
	if len(id) < 16 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func validPath(p string) bool {
	return fs.ValidPath(p) && p != "." && path.Clean(p) == p && !strings.ContainsAny(p, "\\\x00") && !strings.Contains(p, "..")
}

var _ ocrplugin.Access = (*Scope)(nil)
