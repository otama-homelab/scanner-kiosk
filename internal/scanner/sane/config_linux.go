//go:build linux

package sane

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// ConfigFiles serializes a single administrator-registered eSCL device. Display
// names never enter native configuration. Files are generated inputs, not a
// second user-editable configuration source.
func ConfigFiles(deviceID, endpoint string) (map[string][]byte, error) {
	if !opaque(deviceID) || len(endpoint) > 4096 {
		return nil, ErrInvalid
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, ErrInvalid
	}
	if u.Port() != "" {
		port, e := strconv.Atoi(u.Port())
		if e != nil || port < 1 || port > 65535 {
			return nil, ErrInvalid
		}
	}
	// Airscan's INI parser treats comments, commas and quotes as syntax. Reject
	// these bytes rather than relying on URL validation or undocumented escaping.
	for _, r := range endpoint {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("\"'#,;\\", r) {
			return nil, ErrInvalid
		}
	}
	return map[string][]byte{
		"dll.conf":     []byte("airscan\n"),
		"airscan.conf": []byte("[devices]\n\"" + deviceID + "\" = " + endpoint + ", eSCL\n\n[options]\ndiscovery = disable\nws-discovery = off\nprotocol = manual\n\n[debug]\nenable = false\nhexdump = false\n"),
	}, nil
}

// WriteConfig creates a private, unique configuration directory under an
// existing private parent. The caller retains the path even on write/sync
// failure so cleanup failures cannot be mistaken for successful removal. The
// directory must outlive every native process using it.
func WriteConfig(parent, deviceID, endpoint string) (directory string, err error) {
	if !absolute(parent) || strings.Contains(parent, ":") {
		return "", ErrInvalid
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", ErrStorage
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return "", ErrStorage
	}
	defer root.Close()
	return WriteConfigAt(root, deviceID, endpoint)
}

// WriteConfigAt writes relative to a pinned private parent; it never follows a
// replaced absolute parent path. The returned path remains owned on failure.
func WriteConfigAt(parent *os.Root, deviceID, endpoint string) (directory string, err error) {
	files, err := ConfigFiles(deviceID, endpoint)
	if err != nil {
		return "", err
	}
	if parent == nil || !absolute(parent.Name()) || strings.Contains(parent.Name(), ":") {
		return "", ErrInvalid
	}
	file, err := parent.Open(".")
	if err != nil {
		return "", ErrStorage
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", ErrStorage
	}
	var nonce [24]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return "", ErrStorage
	}
	name := "sane_config_" + hex.EncodeToString(nonce[:])
	if err = parent.Mkdir(name, 0700); err != nil {
		return "", ErrStorage
	}
	directory = filepath.Join(parent.Name(), name)
	root, err := parent.OpenRoot(name)
	if err != nil {
		return directory, ErrStorage
	}
	defer root.Close()
	for _, name := range []string{"dll.conf", "airscan.conf"} {
		file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return directory, ErrStorage
		}
		_, writeErr := file.Write(files[name])
		syncErr := file.Sync()
		closeErr := file.Close()
		if errors.Join(writeErr, syncErr, closeErr) != nil {
			return directory, ErrStorage
		}
	}
	for _, entry := range []*os.Root{root, parent} {
		file, err := entry.Open(".")
		if err != nil {
			return directory, ErrStorage
		}
		syncErr := file.Sync()
		closeErr := file.Close()
		if errors.Join(syncErr, closeErr) != nil {
			return filepath.Join(parent.Name(), name), ErrStorage
		}
	}
	return directory, nil
}
