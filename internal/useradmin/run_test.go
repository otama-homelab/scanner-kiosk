package useradmin

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/authport"
	"scanner-kiosk/internal/simpleauth"
	"scanner-kiosk/internal/storage"
)

const password = "Synthetic CLI passphrase 2026!"
const replacement = "Another CLI passphrase 2027!"

func TestUserAdministrationWithoutOIDCOrWorkerConfiguration(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	lookup := func(key string) (string, bool) {
		if key != "SCANNER_STATE_DIR" {
			t.Fatal("admin read unrelated setting", key)
		}
		return root, true
	}
	var output bytes.Buffer
	ctx := context.Background()
	if err := Run(ctx, []string{"create", "Alice"}, lookup, strings.NewReader(password+"\r\n"), &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "User created.\n" {
		t.Fatal("unexpected admin output")
	}
	output.Reset()
	if err := Run(ctx, []string{"set-password", "ALICE"}, lookup, strings.NewReader(replacement), &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Password updated.\n" {
		t.Fatal("unexpected password update output")
	}
	db, err := storage.Open(filepath.Join(root, "state.sqlite3"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	records, err := simpleauth.NewSQLiteRecords(db, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	s, err := simpleauth.New(records)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := s.Authenticate(ctx, "Alice", replacement)
	if err != nil || verified.CredentialVersion != 2 {
		t.Fatal("CLI credential cannot authenticate", err)
	}
	if _, err := s.Authenticate(ctx, "Alice", password); !errors.Is(err, authport.ErrDenied) {
		t.Fatal("old CLI password accepted", err)
	}
	for _, file := range []string{"state.sqlite3", "state.sqlite3-wal"} {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(password)) || bytes.Contains(data, []byte(replacement)) {
			t.Fatal("plaintext password persisted")
		}
	}
}

func TestUserAdministrationRejectsInvalidRequestsAndInputs(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	lookup := func(string) (string, bool) { return root, true }
	for _, input := range []string{"short", strings.Repeat("x", 1025), password + "\nextra", password + "\r"} {
		var out bytes.Buffer
		err := Run(context.Background(), []string{"create", "alice"}, lookup, strings.NewReader(input), &out)
		if !errors.Is(err, authport.ErrDenied) || out.Len() != 0 || strings.Contains(err.Error(), input) {
			t.Fatal("invalid password accepted or exposed", err)
		}
	}
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"create", "alice", "password-in-argument"}, lookup, strings.NewReader(password), &out); !errors.Is(err, ErrUsage) {
		t.Fatal("password argument accepted", err)
	}
	unsafe := t.TempDir()
	os.Chmod(unsafe, 0755)
	if err := Run(context.Background(), []string{"create", "alice"}, func(string) (string, bool) { return unsafe, true }, strings.NewReader(password), &out); !errors.Is(err, authport.ErrUnavailable) {
		t.Fatal("public state directory accepted", err)
	}
}

func TestCompiledUserCommandAndStoredCredential(t *testing.T) {
	if testing.Short() {
		t.Skip("compiled CLI verification")
	}
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "scanner")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/scanner")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v %s", err, output)
	}
	run := func(action, password string) (string, error) {
		cmd := exec.Command(binary, "user", action, "Alice")
		cmd.Env = []string{"SCANNER_STATE_DIR=" + state}
		cmd.Stdin = strings.NewReader(password + "\n")
		output, err := cmd.CombinedOutput()
		if strings.Contains(string(output), password) {
			t.Fatal("CLI exposed password")
		}
		return string(output), err
	}
	if output, err := run("create", password); err != nil || output != "User created.\n" {
		t.Fatal("compiled create", err, output)
	}
	if output, err := run("create", replacement); err == nil || strings.Contains(output, "User created.") {
		t.Fatal("compiled duplicate overwrote credentials", err)
	}
	if output, err := run("set-password", replacement); err != nil || output != "Password updated.\n" {
		t.Fatal("compiled reset", err, output)
	}
	db, err := storage.Open(filepath.Join(state, "state.sqlite3"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	records, err := simpleauth.NewSQLiteRecords(db, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	s, err := simpleauth.New(records)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Authenticate(context.Background(), "ALICE", replacement)
	if err != nil || result.ID == "" || result.CredentialVersion != 2 {
		t.Fatal("compiled CLI account did not authenticate", err)
	}
}
