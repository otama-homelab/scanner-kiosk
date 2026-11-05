//go:build linux

package workerruntime

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/diskspace"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/workeridentity"
)

type workerStorage struct {
	guard       *workeridentity.Guard
	state, docs *sql.DB
	meter       *diskspace.Meter
	store       *documents.Store
}

func openWorkerStorage(c config.Config) (out *workerStorage, err error) {
	if c.Role != config.Worker {
		return nil, configError()
	}
	for _, root := range []string{c.StateDir, c.DocumentDir} {
		info, e := os.Lstat(root)
		if e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return nil, ErrRoot
		}
	}
	w := &workerStorage{}
	defer func() {
		if err != nil {
			err = errors.Join(err, w.close())
		}
	}()
	if w.guard, err = workeridentity.Acquire(c.DocumentDir); err != nil {
		return nil, err
	}
	if w.state, err = storage.Open(filepath.Join(c.StateDir, "state.sqlite3"), storage.State); err != nil {
		return nil, err
	}
	if w.docs, err = storage.Open(filepath.Join(c.DocumentDir, "documents.sqlite3"), storage.Documents); err != nil {
		return nil, err
	}
	if w.meter, err = diskspace.Open(c.DocumentDir, c.Limits["SCANNER_MIN_FREE_BYTES"]); err != nil {
		return nil, err
	}
	if w.store, err = documents.New(w.state, w.docs, time.Now, w.meter.Available); err != nil {
		return nil, err
	}
	return w, nil
}
func (w *workerStorage) close() error {
	var err error
	if w.meter != nil {
		err = errors.Join(err, w.meter.Close())
	}
	if w.docs != nil {
		err = errors.Join(err, w.docs.Close())
	}
	if w.state != nil {
		err = errors.Join(err, w.state.Close())
	}
	if w.guard != nil {
		err = errors.Join(err, w.guard.Close())
	}
	return err
}
