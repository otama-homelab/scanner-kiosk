//go:build !webui

package webui

import "io/fs"

// Backend-only builds deliberately do not pretend to include the product UI.
func embeddedFS() (fs.FS, error) { return nil, fs.ErrNotExist }
