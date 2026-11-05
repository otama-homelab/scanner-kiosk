//go:build linux

// Package importupload owns the API's reserved PDF intake filesystem producer.
package importupload

import (
	"context"
	"crypto/sha256"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"scanner-kiosk/internal/documents"
)

var ErrReconciliation = errors.New("PDF intake requires reconciliation")

type Records interface {
	AdmitImport(context.Context, string, documents.ImportRequest, documents.ImportLimits) (documents.ImportAdmission, error)
	StartImportUpload(context.Context, string, string, string) error
	SealStoppedImportUpload(context.Context, string, string, string, documents.ArtifactRecord, int64) error
	FinishStoppedImportUpload(context.Context, string, string, string, documents.ArtifactRecord, int64, string) error
	ImportUploadRecorded(context.Context, string, string, string, documents.ArtifactRecord) (string, error)
}
type Service struct {
	root    *os.Root
	records Records
	limits  documents.ImportLimits
}

func New(rootPath string, records Records, limits documents.ImportLimits) (*Service, error) {
	if records == nil || !filepath.IsAbs(rootPath) || filepath.Clean(rootPath) != rootPath {
		return nil, documents.ErrInvalid
	}
	info, err := os.Lstat(rootPath)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, documents.ErrStorage
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, documents.ErrStorage
	}
	return &Service{root: root, records: records, limits: limits}, nil
}
func (s *Service) Close() error { return s.root.Close() }

// Receive requires a reader with a bounded read deadline (the HTTP adapter owns
// that deadline). It never parses PDFs. A replay never reads the body or creates
// another producer. No inode is unlinked after uncertain IO or metadata commit.
func (s *Service) Receive(ctx context.Context, owner string, request documents.ImportRequest, body io.Reader) (documents.Admission, error) {
	if body == nil {
		return documents.Admission{}, documents.ErrInvalid
	}
	a, err := s.records.AdmitImport(ctx, owner, request, s.limits)
	if err != nil {
		return documents.Admission{}, err
	}
	if a.Replay {
		return a.Admission, nil
	}
	if err = s.records.StartImportUpload(ctx, owner, a.JobID, a.ProducerID); err != nil {
		empty := a.Input
		empty.SHA256 = sha256.Sum256(nil)
		if stopErr := s.finish(ctx, owner, a, empty, false); stopErr != nil {
			return documents.Admission{}, ErrReconciliation
		}
		return documents.Admission{}, err
	}
	parts := strings.Split(a.Input.RelativePath, "/")
	if len(parts) != 2 || parts[1] != a.Input.ID || !strings.HasPrefix(parts[0], "work_") || !safeID(parts[0]) || !safeID(parts[1]) {
		return documents.Admission{}, ErrReconciliation
	}
	if err = s.root.Mkdir(parts[0], 0700); err != nil {
		return documents.Admission{}, ErrReconciliation
	}
	file, err := s.root.OpenFile(a.Input.RelativePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return documents.Admission{}, ErrReconciliation
	}
	writer := &intakeWriter{ctx: ctx, file: file, hash: sha256.New(), limit: request.InputBytes}
	_, copyErr := io.CopyBuffer(writer, io.LimitReader(body, request.InputBytes+1), make([]byte, 64<<10))
	syncErr := file.Sync()
	closeErr := file.Close()
	directoryErr := s.syncDirectory(parts[0])
	rootErr := s.syncDirectory(".")
	if errors.Join(syncErr, closeErr, directoryErr, rootErr) != nil {
		return documents.Admission{}, ErrReconciliation
	}
	artifact := a.Input
	artifact.Size = writer.count
	copy(artifact.SHA256[:], writer.hash.Sum(nil))
	if copyErr != nil || artifact.Size != request.InputBytes || artifact.SHA256 != request.SHA256 {
		if err = s.finish(ctx, owner, a, artifact, false); err != nil {
			return documents.Admission{}, ErrReconciliation
		}
		if copyErr != nil && !errors.Is(copyErr, documents.ErrInvalid) {
			return documents.Admission{}, documents.ErrStorage
		}
		return documents.Admission{}, documents.ErrInvalid
	}
	if err = s.finish(ctx, owner, a, artifact, true); err != nil {
		if errors.Is(err, ErrReconciliation) {
			return documents.Admission{}, err
		}
		if abortErr := s.finish(ctx, owner, a, artifact, false); abortErr != nil {
			return documents.Admission{}, ErrReconciliation
		}
		return documents.Admission{}, err
	}
	return a.Admission, nil
}
func (s *Service) finish(ctx context.Context, owner string, a documents.ImportAdmission, artifact documents.ArtifactRecord, seal bool) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var err error
	want := "aborted"
	if seal {
		want = "ready"
		err = s.records.SealStoppedImportUpload(cleanup, owner, a.JobID, a.ProducerID, artifact, time.Now().UnixMilli())
	} else {
		err = s.records.FinishStoppedImportUpload(cleanup, owner, a.JobID, a.ProducerID, artifact, time.Now().UnixMilli(), "failed")
	}
	if errors.Is(err, documents.ErrCommitUncertain) {
		state, readErr := s.records.ImportUploadRecorded(cleanup, owner, a.JobID, a.ProducerID, artifact)
		if readErr != nil || state != want {
			return ErrReconciliation
		}
		return nil
	}
	return err
}
func (s *Service) syncDirectory(name string) error {
	file, err := s.root.OpenFile(name, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}
func safeID(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

type intakeWriter struct {
	ctx          context.Context
	file         *os.File
	hash         hash.Hash
	limit, count int64
}

func (w *intakeWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	tooLarge := int64(len(p)) > w.limit-w.count
	if tooLarge {
		p = p[:int(w.limit-w.count)]
	}
	n, err := w.file.Write(p)
	w.count += int64(n)
	_, _ = w.hash.Write(p[:n])
	if tooLarge {
		return n, errors.Join(err, documents.ErrInvalid)
	}
	return n, err
}
