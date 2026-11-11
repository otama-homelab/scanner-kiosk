//go:build ignore

// Build-only notice collector. Run explicitly using the pinned Go toolchain;
// this program is never included in the scanner runtime binary.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type module struct {
	Path, Version, Dir, Sum, GoModSum string
	Replace                           *module
}
type noticeFile struct {
	SourceRelativePath string `json:"source_relative_path"`
	BundledPath        string `json:"bundled_path"`
	SHA256             string `json:"sha256"`
}
type moduleNotices struct {
	Module   string       `json:"module"`
	Version  string       `json:"version"`
	Sum      string       `json:"sum"`
	GoModSum string       `json:"go_mod_sum"`
	Files    []noticeFile `json:"files"`
}

func main() {
	if err := collect(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func collect() error {
	if len(os.Args) != 2 || runtime.Version() != "go1.27.4" {
		return fmt.Errorf("usage: Go 1.27.4 go run scripts/collect-go-notices.go OUTPUT_DIRECTORY")
	}
	destination, err := filepath.Abs(os.Args[1])
	if err != nil {
		return err
	}
	// Refuse merging with stale output from another dependency graph.
	if err = os.Mkdir(destination, 0755); err != nil {
		return err
	}
	sumBytes, err := os.ReadFile("go.sum")
	if err != nil {
		return err
	}
	sums := map[string]string{}
	for _, line := range strings.Split(string(sumBytes), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 {
			sums[fields[0]+" "+fields[1]] = fields[2]
		}
	}
	command := exec.Command("go", "list", "-deps", "-json", "-tags", "webui", "./cmd/scanner")
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	data, err := command.Output()
	if err != nil {
		return fmt.Errorf("list product module dependencies: %w", err)
	}
	modules := map[string]module{}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	for {
		var pkg struct{ Module *module }
		if err = decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		if pkg.Module == nil || pkg.Module.Version == "" {
			continue
		}
		m := *pkg.Module
		if m.Replace != nil || m.Dir == "" || m.Sum == "" || m.GoModSum == "" || sums[m.Path+" "+m.Version] != m.Sum || sums[m.Path+" "+m.Version+"/go.mod"] != m.GoModSum {
			return fmt.Errorf("module lacks committed checksum evidence: %s", m.Path)
		}
		modules[m.Path] = m
	}
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	var entries []moduleNotices
	for _, name := range names {
		m := modules[name]
		hash := sha256.Sum256([]byte(m.Path + "@" + m.Version))
		prefix := filepath.Join("modules", hex.EncodeToString(hash[:])[:16])
		entry := moduleNotices{Module: m.Path, Version: m.Version, Sum: m.Sum, GoModSum: m.GoModSum}
		err = filepath.WalkDir(m.Dir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("unexpected symlink in verified module cache")
			}
			if !d.Type().IsRegular() || !noticeName(d.Name()) {
				return nil
			}
			relative, err := filepath.Rel(m.Dir, path)
			if err != nil || !filepath.IsLocal(relative) {
				return fmt.Errorf("notice path outside module")
			}
			file, err := copyNotice(destination, path, filepath.Join(prefix, relative))
			if err == nil {
				file.SourceRelativePath = filepath.ToSlash(relative)
				entry.Files = append(entry.Files, file)
			}
			return err
		})
		if err != nil {
			return err
		}
		if len(entry.Files) == 0 {
			return fmt.Errorf("module has no notice evidence: %s", m.Path)
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return fmt.Errorf("empty product dependency graph")
	}
	var standard []noticeFile
	for _, name := range []string{"LICENSE", "PATENTS"} {
		path := filepath.Join(runtime.GOROOT(), name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		file, err := copyNotice(destination, path, filepath.Join("stdlib", name))
		if err != nil {
			return err
		}
		file.SourceRelativePath = name
		standard = append(standard, file)
	}
	if len(standard) == 0 {
		return fmt.Errorf("missing Go runtime license")
	}
	index := struct {
		Scope, GoVersion, ProjectLicense string
		Modules                          []moduleNotices `json:"modules"`
		Standard                         []noticeFile    `json:"stdlib"`
	}{"Verified source notices for product Go dependencies; legal obligations require review", runtime.Version(), "not selected", entries, standard}
	encoded, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(destination, "index.json"), append(encoded, '\n'), 0644)
}

func noticeName(name string) bool {
	name = strings.ToUpper(name)
	for _, prefix := range []string{"LICENSE", "LICENCE", "COPYING", "NOTICE", "COPYRIGHT", "AUTHORS", "PATENTS"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func copyNotice(root, source, relative string) (noticeFile, error) {
	var file noticeFile
	data, err := os.ReadFile(source)
	if err != nil {
		return file, err
	}
	path := filepath.Join(root, relative)
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return file, err
	}
	if err = os.WriteFile(path, data, 0644); err != nil {
		return file, err
	}
	hash := sha256.Sum256(data)
	file.BundledPath, file.SHA256 = filepath.ToSlash(relative), hex.EncodeToString(hash[:])
	return file, nil
}
