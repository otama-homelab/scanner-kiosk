//go:build linux

package sane

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigRejectsNativeSyntaxInjection(t *testing.T) {
	for _, endpoint := range []string{"http://scanner/eSCL\n[debug]", "http://scanner/eSCL;trace=/tmp", "http://scanner/eSCL, WSD", "http://scanner/eSCL#fragment", "http://user:secret@scanner/eSCL", "file:///tmp/scanner", "http://scanner/eSCL?", "http://scanner:99999/eSCL", "http://scanner/a\"b", "http://scanner/a\\b"} {
		if _, err := ConfigFiles("scanner_example_0001", endpoint); err == nil {
			t.Errorf("accepted unsafe endpoint %q", endpoint)
		}
	}
	if _, err := ConfigFiles("name\" = disable", "http://scanner/eSCL"); err == nil {
		t.Fatal("accepted unsafe identity")
	}
}
func TestGeneratedConfigPrivateAndIsolated(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	endpoint := "https://[2001:db8::1]:8443/eSCL"
	directory, err := WriteConfig(parent, "scanner_example_0001", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	other, err := WriteConfig(parent, "scanner_example_0002", "http://192.0.2.10/eSCL")
	if err != nil {
		t.Fatal(err)
	}
	if directory == other {
		t.Fatal("shared configuration directory")
	}
	for _, name := range []string{"dll.conf", "airscan.conf"} {
		info, err := os.Stat(filepath.Join(directory, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("file not private", err)
		}
	}
	data, err := os.ReadFile(filepath.Join(directory, "airscan.conf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"\"scanner_example_0001\" = " + endpoint + ", eSCL", "discovery = disable", "ws-discovery = off", "enable = false", "hexdump = false"} {
		if !strings.Contains(string(data), required) {
			t.Fatalf("missing %q", required)
		}
	}
	if strings.Contains(string(data), "scanner_example_0002") {
		t.Fatal("cross-device configuration")
	}
	if _, err := WriteConfig(parent, "bad", "http://scanner/eSCL"); err == nil {
		t.Fatal("invalid identity accepted")
	}
	entries, _ := os.ReadDir(parent)
	if len(entries) != 2 {
		t.Fatal("invalid configuration left files")
	}
	if err := os.Chmod(parent, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteConfig(parent, "scanner_example_0003", endpoint); err == nil {
		t.Fatal("public parent accepted")
	}
}

func TestPinnedConfigWriterDoesNotFollowReplacedParent(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "parent")
	saved := filepath.Join(base, "saved")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = os.Rename(parent, saved); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	directory, err := WriteConfigAt(root, "scanner_pinned_0001", "http://192.0.2.10/eSCL")
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(parent); err != nil || len(entries) != 0 {
		t.Fatal("replacement parent modified", err)
	}
	if _, err = os.Stat(filepath.Join(saved, filepath.Base(directory), "airscan.conf")); err != nil {
		t.Fatal("pinned parent not used", err)
	}
}
