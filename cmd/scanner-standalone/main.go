//go:build bundled && linux && amd64

// Command scanner-standalone adds a private native runtime to the existing app.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

//go:embed payload.tar.gz
var payload []byte

//go:embed manifest.json
var manifestJSON []byte

var errRuntime = errors.New("bundled runtime unavailable or changed")

const bundledCAFile = "usr/share/scanner-kiosk/ca-certificates.crt"

type entry struct {
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256"`
	Link   string `json:"link,omitempty"`
}
type manifest struct {
	PayloadSHA256 string           `json:"payload_sha256"`
	Files         map[string]entry `json:"files"`
}

var tools = map[string]string{
	"scanner-native-scanimage":  "usr/bin/scanimage",
	"scanner-native-parameters": "usr/libexec/scanner/sane-parameters",
	"scanner-native-tesseract":  "usr/bin/tesseract",
}

func digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func fileDigest(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 512<<20 {
		return "", errRuntime
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func cacheID(m manifest, executableHash string) string {
	return digest([]byte(m.PayloadSHA256 + "\x00" + executableHash))
}
func cleanRelative(name string) bool {
	return name != "" && name != "." && !strings.HasPrefix(name, "/") && path.Clean(name) == name && name != ".." && !strings.HasPrefix(name, "../") && !strings.ContainsAny(name, "\\\x00")
}
func validateManifest(m manifest) error {
	if len(m.Files) == 0 || len(m.Files) > 4096 || len(m.PayloadSHA256) != 64 {
		return errRuntime
	}
	var total int64
	for name, e := range m.Files {
		if !cleanRelative(name) || name == "tools" || strings.HasPrefix(name, "tools/") || e.Size < 0 || e.Mode & ^uint32(0555) != 0 {
			return errRuntime
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if _, exists := m.Files[parent]; exists {
				return errRuntime
			}
		}
		if e.Link != "" {
			if path.IsAbs(e.Link) || !cleanRelative(path.Join(path.Dir(name), e.Link)) || e.Size != 0 {
				return errRuntime
			}
		} else if len(e.SHA256) != 64 {
			return errRuntime
		}
		total += e.Size
		if total > 512<<20 {
			return errRuntime
		}
	}
	return nil
}
func privateDir(name string) error {
	i, err := os.Lstat(name)
	if err != nil || !i.IsDir() || i.Mode().Perm() != 0700 {
		return errRuntime
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok || int(s.Uid) != os.Geteuid() {
		return errRuntime
	}
	return nil
}
func verify(root string, m manifest, executableHash string) error {
	if err := privateDir(root); err != nil {
		return err
	}
	allowed := map[string]bool{".": true, "tools": true, "tools/runner": true}
	for name := range tools {
		allowed["tools/"+name] = true
	}
	for name, e := range m.Files {
		allowed[name] = true
		for p := path.Dir(name); p != "."; p = path.Dir(p) {
			allowed[p] = true
		}
		filename := filepath.Join(root, name)
		i, err := os.Lstat(filename)
		if err != nil {
			return errRuntime
		}
		if e.Link != "" {
			target, err := os.Readlink(filename)
			if err != nil || target != e.Link {
				return errRuntime
			}
		} else {
			if !i.Mode().IsRegular() || i.Size() != e.Size || uint32(i.Mode().Perm()) != e.Mode {
				return errRuntime
			}
			h, err := fileDigest(filename)
			if err != nil || h != e.SHA256 {
				return errRuntime
			}
		}
	}
	runner, err := os.Lstat(filepath.Join(root, "tools/runner"))
	if err != nil || !runner.Mode().IsRegular() || runner.Mode().Perm() != 0555 {
		return errRuntime
	}
	h, err := fileDigest(filepath.Join(root, "tools/runner"))
	if err != nil || h != executableHash {
		return errRuntime
	}
	for name := range tools {
		i, err := os.Lstat(filepath.Join(root, "tools", name))
		if err != nil || !i.Mode().IsRegular() || !os.SameFile(i, runner) {
			return errRuntime
		}
	}
	return filepath.WalkDir(root, func(name string, d os.DirEntry, err error) error {
		if err != nil {
			return errRuntime
		}
		rel, err := filepath.Rel(root, name)
		if err != nil || !allowed[filepath.ToSlash(rel)] {
			return errRuntime
		}
		if d.IsDir() {
			return privateDir(name)
		}
		return nil
	})
}
func install(base, executable string, data []byte, m manifest) (string, error) {
	if err := validateManifest(m); err != nil || digest(data) != m.PayloadSHA256 {
		return "", errRuntime
	}
	executableHash, err := fileDigest(executable)
	if err != nil {
		return "", errRuntime
	}
	if err = os.MkdirAll(base, 0700); err != nil {
		return "", errRuntime
	}
	if err = privateDir(base); err != nil {
		return "", err
	}
	destination := filepath.Join(base, cacheID(m, executableHash))
	if _, err := os.Lstat(destination); err == nil {
		return destination, verify(destination, m, executableHash)
	} else if !os.IsNotExist(err) {
		return "", errRuntime
	}
	stage, err := os.MkdirTemp(base, ".stage-")
	if err != nil {
		return "", errRuntime
	}
	defer os.RemoveAll(stage) // Only this invocation's unpublished private staging root.
	compressed, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", errRuntime
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	seen := map[string]bool{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", errRuntime
		}
		e, ok := m.Files[header.Name]
		if !ok || seen[header.Name] {
			return "", errRuntime
		}
		seen[header.Name] = true
		filename := filepath.Join(stage, header.Name)
		if err = os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			return "", errRuntime
		}
		if e.Link != "" {
			if header.Typeflag != tar.TypeSymlink || header.Linkname != e.Link || header.Size != 0 {
				return "", errRuntime
			}
			continue // Create all links after regular files and directories.
		}
		if header.Typeflag != tar.TypeReg || header.Size != e.Size {
			return "", errRuntime
		}
		f, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return "", errRuntime
		}
		hash := sha256.New()
		_, copyErr := io.CopyN(io.MultiWriter(f, hash), reader, e.Size)
		modeErr := f.Chmod(os.FileMode(e.Mode))
		syncErr := f.Sync()
		closeErr := f.Close()
		if copyErr != nil || modeErr != nil || syncErr != nil || closeErr != nil || hex.EncodeToString(hash.Sum(nil)) != e.SHA256 {
			return "", errRuntime
		}
	}
	if len(seen) != len(m.Files) {
		return "", errRuntime
	}
	for name, e := range m.Files {
		if e.Link != "" {
			if err = os.Symlink(e.Link, filepath.Join(stage, name)); err != nil {
				return "", errRuntime
			}
		}
	}
	if err = os.Mkdir(filepath.Join(stage, "tools"), 0700); err != nil {
		return "", errRuntime
	}
	input, err := os.Open(executable)
	if err != nil {
		return "", errRuntime
	}
	runner, err := os.OpenFile(filepath.Join(stage, "tools/runner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0555)
	if err != nil {
		input.Close()
		return "", errRuntime
	}
	_, copyErr := io.Copy(runner, input)
	inputErr := input.Close()
	modeErr := runner.Chmod(0555)
	syncErr := runner.Sync()
	closeErr := runner.Close()
	if copyErr != nil || inputErr != nil || modeErr != nil || syncErr != nil || closeErr != nil {
		return "", errRuntime
	}
	for name := range tools {
		if err = os.Link(filepath.Join(stage, "tools/runner"), filepath.Join(stage, "tools", name)); err != nil {
			return "", errRuntime
		}
	}
	if err = verify(stage, m, executableHash); err != nil {
		return "", err
	}
	if err = os.Rename(stage, destination); err != nil {
		// A concurrent valid publisher may win; never overwrite a changed cache.
		if err = verify(destination, m, executableHash); err != nil {
			return "", err
		}
	}
	return destination, nil
}
func libraryPath(root string) string {
	return strings.Join([]string{filepath.Join(root, "lib"), filepath.Join(root, "usr/lib"), filepath.Join(root, "usr/lib/sane")}, ":")
}
func nativeEnvironment(root string, input []string) []string {
	allowed := map[string]bool{"HOME": true, "LANG": true, "LC_ALL": true, "TZ": true, "TMPDIR": true, "TMP": true, "TEMP": true, "OMP_THREAD_LIMIT": true, "OMP_NUM_THREADS": true, "SANE_CONFIG_DIR": true, "SSL_CERT_FILE": true}
	result := []string{"LD_LIBRARY_PATH=" + libraryPath(root)}
	caConfigured := false
	for _, value := range input {
		key, _, ok := strings.Cut(value, "=")
		if ok && allowed[key] {
			result = append(result, value)
			caConfigured = caConfigured || key == "SSL_CERT_FILE"
		}
	}
	if !caConfigured {
		result = append(result, "SSL_CERT_FILE="+filepath.Join(root, bundledCAFile))
	}
	return result
}
func applicationEnvironment(root string, args, input []string) []string {
	defaults := map[string]string{
		"SSL_CERT_FILE":                  filepath.Join(root, bundledCAFile),
		"SCANNER_SCANIMAGE_PATH":         filepath.Join(root, "tools/scanner-native-scanimage"),
		"SCANNER_SANE_PARAMETERS_PATH":   filepath.Join(root, "tools/scanner-native-parameters"),
		"SCANNER_OCR_TESSERACT_PATH":     filepath.Join(root, "tools/scanner-native-tesseract"),
		"SCANNER_OCR_TESSERACT_DATA_DIR": filepath.Join(root, "usr/share/tessdata"),
	}
	libraries, _ := json.Marshal([]string{filepath.Join(root, "lib"), filepath.Join(root, "usr/lib"), filepath.Join(root, "usr/lib/sane")})
	defaults["SCANNER_SANE_LIBRARY_DIRS_JSON"] = string(libraries)
	result := []string{}
	for _, value := range input {
		key, _, ok := strings.Cut(value, "=")
		if !ok {
			continue
		}
		if len(args) > 0 && args[0] == "worker" && strings.HasPrefix(key, "SCANNER_OIDC_") {
			continue
		}
		delete(defaults, key)
		result = append(result, value)
	}
	for key, value := range defaults {
		result = append(result, key+"="+value)
	}
	return result
}
func run() error {
	var m manifest
	if json.Unmarshal(manifestJSON, &m) != nil || validateManifest(m) != nil {
		return errRuntime
	}
	self, err := os.Executable()
	if err != nil {
		return errRuntime
	}
	if binary, native := tools[filepath.Base(os.Args[0])]; native {
		if !filepath.IsAbs(os.Args[0]) || filepath.Clean(os.Args[0]) != os.Args[0] {
			return errRuntime
		}
		root := filepath.Dir(filepath.Dir(os.Args[0]))
		selfHash, err := fileDigest(self)
		if err != nil || filepath.Base(root) != cacheID(m, selfHash) {
			return errRuntime
		}
		if err = verify(root, m, selfHash); err != nil {
			return err
		}
		loader := filepath.Join(root, "lib/ld-musl-x86_64.so.1")
		args := append([]string{loader, "--library-path", libraryPath(root), filepath.Join(root, binary)}, os.Args[1:]...)
		return syscall.Exec(loader, args, nativeEnvironment(root, os.Environ()))
	}
	cache, err := os.UserCacheDir()
	if err != nil || !filepath.IsAbs(cache) || strings.ContainsAny(cache, ":\x00\r\n") {
		return errRuntime
	}
	root, err := install(filepath.Join(cache, "scanner-kiosk/native"), self, payload, m)
	if err != nil {
		return err
	}
	app := filepath.Join(root, "app/scanner")
	return syscall.Exec(app, append([]string{app}, os.Args[1:]...), applicationEnvironment(root, os.Args[1:], os.Environ()))
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, errRuntime)
		os.Exit(1)
	}
}
