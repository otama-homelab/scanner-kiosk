package staterecovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"scanner-kiosk/internal/storage"
)

var ErrUsage = errors.New("usage: scanner state export|restore PRIVATE_ARCHIVE_PATH")
var ErrFilesystem = errors.New("state recovery requires private existing directories and a new destination")
var ErrPublished = errors.New("state recovery file published; durability confirmation failed; inspect before retrying")

// Run never reads HTTP/OIDC/provider/device settings. Restore only publishes a
// fully initialized new database in an otherwise empty private state root.
func Run(ctx context.Context, args []string, lookup func(string) (string, bool), output io.Writer) error {
	if len(args) != 2 || args[0] != "export" && args[0] != "restore" || lookup == nil || output == nil {
		return ErrUsage
	}
	root, ok := lookup("SCANNER_STATE_DIR")
	if !ok {
		root = "/var/lib/scanner/state"
	}
	if !privateDirectory(root) || !cleanAbsolute(args[1]) || !privateDirectory(filepath.Dir(args[1])) {
		return ErrFilesystem
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if args[0] == "export" {
		db, err := storage.OpenReadOnly(filepath.Join(root, "state.sqlite3"))
		if err != nil {
			return ErrArchive
		}
		data, err := Export(ctx, db)
		closeErr := db.Close()
		if err != nil || closeErr != nil {
			return ErrArchive
		}
		defer clear(data)
		if err = publishArchive(ctx, args[1], data); err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, "State identities exported. Sessions and documents are excluded.")
		return err
	}
	info, err := os.Lstat(args[1])
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > MaxArchiveBytes {
		return ErrFilesystem
	}
	file, err := os.Open(args[1])
	if err != nil {
		return ErrFilesystem
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxArchiveBytes+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil || len(data) > MaxArchiveBytes {
		return ErrArchive
	}
	defer clear(data)
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		return ErrFilesystem
	}
	stage, err := os.MkdirTemp(root, ".state-restore-")
	if err != nil {
		return ErrFilesystem
	}
	defer os.RemoveAll(stage)
	path := filepath.Join(stage, "state.sqlite3")
	db, err := storage.Open(path, storage.State)
	if err != nil {
		return ErrArchive
	}
	if err = Restore(ctx, db, data); err != nil {
		db.Close()
		return err
	}
	var busy, frames, checkpointed int
	if err = db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checkpointed); err != nil || busy != 0 {
		db.Close()
		return ErrArchive
	}
	if db.Close() != nil {
		return ErrArchive
	}
	if err = syncFile(path); err != nil {
		return ErrFilesystem
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Hard-link publication is atomic and fails if any API/operator concurrently
	// created state.sqlite3. It never replaces an existing database/inode.
	if os.Link(path, filepath.Join(root, "state.sqlite3")) != nil {
		return ErrFilesystem
	}
	if os.RemoveAll(stage) != nil || syncDirectory(root) != nil {
		return ErrPublished
	}
	_, err = fmt.Fprintln(output, "State identities restored. Sign in again; documents and scanner readiness were not restored.")
	return err
}

func publishArchive(ctx context.Context, path string, data []byte) error {
	parent := filepath.Dir(path)
	stage, err := os.MkdirTemp(parent, ".state-export-")
	if err != nil {
		return ErrFilesystem
	}
	defer os.RemoveAll(stage)
	temporary := filepath.Join(stage, "archive.json")
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrFilesystem
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return ErrFilesystem
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if os.Link(temporary, path) != nil {
		return ErrFilesystem
	}
	if os.RemoveAll(stage) != nil || syncDirectory(parent) != nil {
		return ErrPublished
	}
	return nil
}
func cleanAbsolute(path string) bool { return filepath.IsAbs(path) && filepath.Clean(path) == path }
func privateDirectory(path string) bool {
	if !cleanAbsolute(path) {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode().Perm()&0077 == 0
}
func syncDirectory(path string) error { return syncFile(path) }
func syncFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
