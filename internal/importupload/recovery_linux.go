//go:build linux

package importupload

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"time"

	"scanner-kiosk/internal/documents"
)

// RecoverStopped requires the administrative coordinator's assertion that the
// complete old API/worker runtime and its children have terminated. It preserves
// partial source bytes; a dead worker or an acquired worker lock is insufficient.
func RecoverStopped(ctx context.Context, rootPath string, records Records, item documents.RecoveryImport, stoppedAt int64) (result error) {
	if records == nil || stoppedAt < item.CreatedAtMS || stoppedAt < item.StartedAtMS || stoppedAt > time.Now().UnixMilli() || (item.IntakeState != "pending" && item.IntakeState != "receiving") || item.MaxInputBytes < 1 || item.MaxInputBytes > 1<<30 {
		return documents.ErrInvalid
	}
	parts := strings.Split(item.Input.RelativePath, "/")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "work_") || !safeID(parts[0]) || !safeID(parts[1]) || parts[1] != item.Input.ID {
		return ErrReconciliation
	}
	root, err := openRoot(rootPath)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, root.Close()) }()
	artifact := item.Input
	artifact.Size = 0
	artifact.SHA256 = sha256.Sum256(nil)
	directory, err := root.Lstat(parts[0])
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrReconciliation
	}
	if err == nil {
		if !directory.IsDir() || directory.Mode().Perm()&0077 != 0 {
			return ErrReconciliation
		}
		entry, statErr := root.Lstat(item.Input.RelativePath)
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return ErrReconciliation
		}
		if statErr == nil {
			if item.IntakeState == "pending" || !entry.Mode().IsRegular() || entry.Mode().Perm()&0077 != 0 || entry.Size() < 0 || entry.Size() > item.MaxInputBytes {
				return ErrReconciliation
			}
			file, err := root.OpenFile(item.Input.RelativePath, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
			if err != nil {
				return ErrReconciliation
			}
			info, err := file.Stat()
			if err != nil || !os.SameFile(entry, info) {
				file.Close()
				return ErrReconciliation
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) {
				file.Close()
				return ErrReconciliation
			}
			hash := sha256.New()
			buffer := make([]byte, 64<<10)
			var total int64
			for {
				if err = ctx.Err(); err != nil {
					file.Close()
					return err
				}
				n, readErr := file.Read(buffer)
				total += int64(n)
				if total > item.MaxInputBytes {
					file.Close()
					return ErrReconciliation
				}
				hash.Write(buffer[:n])
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					file.Close()
					return ErrReconciliation
				}
			}
			if err = errors.Join(file.Sync(), file.Close()); err != nil || total != info.Size() {
				return ErrReconciliation
			}
			artifact.Size = total
			copy(artifact.SHA256[:], hash.Sum(nil))
		}
		service := &Service{root: root}
		if err = service.syncDirectory(parts[0]); err != nil {
			return ErrReconciliation
		}
	}
	service := &Service{root: root}
	if err = service.syncDirectory("."); err != nil {
		return ErrReconciliation
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	err = records.FinishStoppedImportUpload(ctx, item.OwnerID, item.JobID, item.ProducerID, artifact, time.Now().UnixMilli(), "interrupted")
	if errors.Is(err, documents.ErrCommitUncertain) {
		state, readErr := records.ImportUploadRecorded(ctx, item.OwnerID, item.JobID, item.ProducerID, artifact)
		if readErr == nil && state == "aborted" {
			return nil
		}
		return errors.Join(ErrReconciliation, err, readErr)
	}
	return err
}
