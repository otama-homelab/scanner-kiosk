//go:build linux

package scannerdriver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

type ownedFile struct {
	info   os.FileInfo
	digest [32]byte
}
type ownedConfig struct {
	info     os.FileInfo
	root     *os.Root
	files    map[string]ownedFile
	verified bool
}
type configWorkspace struct {
	path, name      string
	parent, root    *os.Root
	info            os.FileInfo
	configs         map[string]*ownedConfig
	removed, closed bool
	terminalError   error
}

func newConfigWorkspace(parentPath string) (*configWorkspace, error) {
	parent, err := os.OpenRoot(parentPath)
	if err != nil {
		return nil, err
	}
	var nonce [24]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		parent.Close()
		return nil, err
	}
	name := "scanner_config_" + hex.EncodeToString(nonce[:])
	if err = parent.Mkdir(name, 0700); err != nil {
		parent.Close()
		return nil, err
	}
	path := filepath.Join(parentPath, name)

	w := &configWorkspace{path: path, name: filepath.Base(path), parent: parent, configs: map[string]*ownedConfig{}}
	w.info, err = parent.Lstat(w.name)
	if err != nil {
		return w, err
	}
	w.root, err = parent.OpenRoot(w.name)
	if err != nil {
		return w, err
	}
	if err = syncRoot(parent); err != nil {
		return w, err
	}
	return w, nil
}
func names(root *os.Root, max int) ([]string, error) {
	directory, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(max + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > max {
		return nil, ErrConfigCleanup
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out, nil
}
func snapshot(root *os.Root, name string) (ownedFile, error) {
	var out ownedFile
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16384 {
		return out, ErrConfigCleanup
	}
	file, err := root.Open(name)
	if err != nil {
		return out, err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return out, ErrConfigCleanup
	}
	data, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil || int64(len(data)) != info.Size() {
		return out, ErrConfigCleanup
	}
	return ownedFile{info: info, digest: sha256.Sum256(data)}, nil
}
func (w *configWorkspace) remember(path string) error {
	if filepath.Dir(path) != w.path {
		return ErrConfigCleanup
	}
	name := filepath.Base(path)
	entry := &ownedConfig{files: map[string]ownedFile{}}
	w.configs[name] = entry
	info, err := w.root.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return ErrConfigCleanup
	}
	entry.info = info
	entry.root, err = w.root.OpenRoot(name)
	if err != nil {
		return err
	}
	files, err := names(entry.root, 2)
	if err != nil {
		return err
	}
	for _, name := range files {
		if name != "dll.conf" && name != "airscan.conf" {
			return ErrConfigCleanup
		}
		file, err := snapshot(entry.root, name)
		if err != nil {
			return err
		}
		entry.files[name] = file
	}
	entry.verified = true
	return nil
}
func rootMatches(root *os.Root, info os.FileInfo) bool {
	file, err := root.Open(".")
	if err != nil {
		return false
	}
	defer file.Close()
	actual, err := file.Stat()
	return err == nil && os.SameFile(actual, info)
}
func (w *configWorkspace) verify() error {
	if w.root == nil || w.parent == nil || w.info == nil || w.removed {
		return ErrConfigCleanup
	}
	info, err := w.parent.Lstat(w.name)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 || !os.SameFile(info, w.info) || !rootMatches(w.root, w.info) {
		return ErrConfigCleanup
	}
	dirs, err := names(w.root, 32)
	if err != nil || len(dirs) != len(w.configs) {
		return ErrConfigCleanup
	}
	for _, name := range dirs {
		entry, ok := w.configs[name]
		if !ok || !entry.verified || entry.root == nil {
			return ErrConfigCleanup
		}
		info, err := w.root.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 || !os.SameFile(info, entry.info) || !rootMatches(entry.root, entry.info) {
			return ErrConfigCleanup
		}
		files, err := names(entry.root, 2)
		if err != nil || len(files) != len(entry.files) {
			return ErrConfigCleanup
		}
		for _, name := range files {
			expected, ok := entry.files[name]
			if !ok {
				return ErrConfigCleanup
			}
			actual, err := snapshot(entry.root, name)
			if err != nil || !os.SameFile(actual.info, expected.info) || actual.info.Mode().Perm() != expected.info.Mode().Perm() || actual.digest != expected.digest {
				return ErrConfigCleanup
			}
		}
	}
	return nil
}
func syncRoot(root *os.Root) error {
	file, err := root.Open(".")
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
func (w *configWorkspace) close(ctx context.Context) error {
	if w.closed {
		return w.terminalError
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !w.removed {
		// Preflight the complete tree before deleting even one generated file.
		if w.verify() != nil {
			return ErrConfigCleanup
		}
		for name, entry := range w.configs {
			if err := ctx.Err(); err != nil {
				return err
			}
			for file := range entry.files {
				if entry.root.Remove(file) != nil {
					return ErrConfigCleanup
				}
				delete(entry.files, file)
			}
			if syncRoot(entry.root) != nil {
				return ErrConfigCleanup
			}
			if w.root.Remove(name) != nil {
				return ErrConfigCleanup
			}
			delete(w.configs, name)
			if entry.root.Close() != nil {
				return ErrConfigCleanup
			}
		}
		if syncRoot(w.root) != nil {
			return ErrConfigCleanup
		}
		if w.parent.Remove(w.name) != nil {
			return ErrConfigCleanup
		}
		w.removed = true
		if w.root.Close() != nil {
			return ErrConfigCleanup
		}
		w.root = nil
	}
	if syncRoot(w.parent) != nil {
		return ErrConfigCleanup
	}
	w.closed = true
	if w.parent.Close() != nil {
		w.terminalError = ErrConfigCleanup
	}
	w.parent = nil
	return w.terminalError
}
