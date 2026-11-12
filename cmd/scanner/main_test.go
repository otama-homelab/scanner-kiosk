package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhotoNormalizeChildDispatch(t *testing.T) {
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewNRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	// Parser limits describe the production executable, not the race runtime's shadow memory.
	self := filepath.Join(t.TempDir(), "scanner")
	build := exec.Command("go", "build", "-o", self, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if diagnostics, err := build.CombinedOutput(); err != nil {
		t.Fatal("build production child", err, string(diagnostics))
	}
	root := t.TempDir()
	for _, valid := range []bool{true, false} {
		command := exec.Command(self, "__photo-normalize", "image/png", "1048576")
		command.Env = []string{"SCANNER_STATE_DIR=" + filepath.Join(root, "state"), "SCANNER_DOCUMENT_DIR=" + filepath.Join(root, "documents")}
		if valid {
			command.Stdin = bytes.NewReader(imageBytes.Bytes())
		} else {
			command.Stdin = bytes.NewReader([]byte("premature invalid child input"))
		}
		var diagnostics bytes.Buffer
		command.Stderr = &diagnostics
		result, err := command.Output()
		if valid {
			if err != nil || len(result) < 64 || string(result[:8]) != "PHOTO001" {
				t.Fatal("production child dispatch", err, diagnostics.String())
			}
		} else {
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 10 || len(result) != 0 {
				t.Fatal("invalid child publication", err, diagnostics.String())
			}
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("normalizer started application services", err)
	}
}

func TestRecoveryCLIExplicitConfirmationAndDocumentRetentionMessage(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, confirmed := range []bool{false, true} {
		root := t.TempDir()
		state := filepath.Join(root, "state")
		docs := filepath.Join(root, "documents")
		for _, dir := range []string{state, docs} {
			if err = os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
		}
		args := "worker recover"
		if confirmed {
			args += " --previous-runtime-stopped"
		}
		child := exec.Command(binary, "-test.run=^TestScannerCLIProcess$")
		child.Env = []string{"SCANNER_CLI_TEST=1", "SCANNER_CLI_TEST_ARGS=" + args, "SCANNER_STATE_DIR=" + state, "SCANNER_DOCUMENT_DIR=" + docs}
		result, runErr := child.CombinedOutput()
		if confirmed {
			if runErr != nil || !strings.Contains(string(result), "Existing unexpired documents retained") {
				t.Fatal("recovery command", runErr, string(result))
			}
		} else {
			exit, ok := runErr.(*exec.ExitError)
			if !ok || exit.ExitCode() != 2 || !strings.Contains(string(result), "--previous-runtime-stopped") {
				t.Fatal("missing confirmation accepted", runErr, string(result))
			}
			entries, err := os.ReadDir(docs)
			if err != nil || len(entries) != 0 {
				t.Fatal("unconfirmed command touched store", entries, err)
			}
		}
	}
}
func TestScannerCLIProcess(t *testing.T) {
	if os.Getenv("SCANNER_CLI_TEST") != "1" {
		return
	}
	os.Args = append([]string{"scanner"}, strings.Fields(os.Getenv("SCANNER_CLI_TEST_ARGS"))...)
	main()
	os.Exit(0)
}

func TestPDFImportCLIDoesNotStartServices(t *testing.T) {
	for _, args := range []string{"__pdf-import 1024 1024 10", "__pdf-assemble 1024"} {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		state := filepath.Join(root, "state")
		documents := filepath.Join(root, "documents")
		command := exec.Command(executable, "-test.run=^TestScannerCLIProcess$")
		command.Env = []string{"SCANNER_CLI_TEST=1", "SCANNER_CLI_TEST_ARGS=" + args, "SCANNER_STATE_DIR=" + state, "SCANNER_DOCUMENT_DIR=" + documents}
		output, err := command.CombinedOutput()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 10 || len(output) != 0 {
			t.Fatal("internal parser command", err, string(output))
		}
		for _, path := range []string{state, documents} {
			if _, err = os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("parser touched service store", path, err)
			}
		}
	}
}
