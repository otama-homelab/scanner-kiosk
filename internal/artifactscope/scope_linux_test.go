//go:build linux

package artifactscope

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"scanner-kiosk/pkg/ocrplugin"
)

func fixture(t *testing.T) (string, map[string]Input, Limits) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte("original bytes")
	if err := os.WriteFile(filepath.Join(root, "original"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return root, map[string]Input{"input_0000000001": {RelativePath: "original", Size: int64(len(data)), SHA256: sha256.Sum256(data)}}, Limits{MaxInputBytes: 1024, MaxOutputBytes: 16, MaxTotalOutputBytes: 32, MaxOutputs: 2}
}
func create(t *testing.T) (*Scope, string) {
	t.Helper()
	root, inputs, limits := fixture(t)
	s, err := New(root, inputs, limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, root
}
func TestAllowlistedInputAndAbortPreserveOriginal(t *testing.T) {
	s, root := create(t)
	r, err := s.OpenInput(context.Background(), "input_0000000001")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil || string(data) != "original bytes" {
		t.Fatal("input read failed")
	}
	r.Close()
	for _, id := range []string{"another_00000001", "../original", "/original"} {
		if _, err = s.OpenInput(context.Background(), id); !errors.Is(err, ErrInput) {
			t.Fatal("unlisted input accepted")
		}
	}
	w, err := s.CreateOutput(context.Background(), ocrplugin.Text)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("derived"))
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("late")); !errors.Is(err, ErrClosed) {
		t.Fatal("closed writer accepted")
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 1 || files[0].Name() != "original" {
		t.Fatal("cleanup touched input or left outputs")
	}
	if err = s.Close(); err != nil {
		t.Fatal("close not idempotent")
	}
}
func TestSealAndReleaseTransferImmutableMetadata(t *testing.T) {
	s, root := create(t)
	w, err := s.CreateOutput(context.Background(), ocrplugin.PDFTextLayer)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Release(); !errors.Is(err, ErrNotReady) {
		t.Fatal("released unsealed output")
	}
	if _, err = w.Write([]byte("fixture bytes")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Seal(context.Background()); !errors.Is(err, ErrNotReady) {
		t.Fatal("sealed open output")
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	artifacts, err := s.Seal(context.Background())
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("seal failed: %v", err)
	}
	a := artifacts[0]
	if a.ID != w.Reference() || a.Size != 13 || a.SHA256 != sha256.Sum256([]byte("fixture bytes")) || a.Kind != string(ocrplugin.PDFTextLayer) {
		t.Fatal("invalid metadata")
	}
	file, err := os.ReadFile(filepath.Join(root, a.RelativePath))
	if err != nil || string(file) != "fixture bytes" {
		t.Fatal("missing sealed file")
	}
	info, _ := os.Stat(filepath.Join(root, a.RelativePath))
	if info.Mode().Perm() != 0600 {
		t.Fatal("output not private")
	}
	artifacts[0].RelativePath = "changed"
	again, err := s.Seal(context.Background())
	if err != nil || again[0].RelativePath != a.RelativePath {
		t.Fatal("mutable seal metadata")
	}
	if _, err = s.CreateOutput(context.Background(), ocrplugin.Text); !errors.Is(err, ErrClosed) {
		t.Fatal("sealed scope accepted write")
	}
	if err = s.Release(); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err = os.Stat(filepath.Join(root, a.RelativePath)); err != nil {
		t.Fatal("released artifact removed")
	}
}
func TestAbortedSealedOutputIsRemoved(t *testing.T) {
	s, root := create(t)
	w, _ := s.CreateOutput(context.Background(), ocrplugin.Text)
	w.Write([]byte("text"))
	w.Close()
	a, err := s.Seal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err = os.Stat(filepath.Join(root, a[0].RelativePath)); !os.IsNotExist(err) {
		t.Fatal("failed publication output retained")
	}
}
func TestRejectsCorruptOrSubstitutedInputBeforeAccess(t *testing.T) {
	for _, mode := range []string{"digest", "size", "symlink", "directory", "public"} {
		t.Run(mode, func(t *testing.T) {
			root, inputs, limits := fixture(t)
			input := inputs["input_0000000001"]
			switch mode {
			case "digest":
				input.SHA256 = sha256.Sum256([]byte("other"))
			case "size":
				input.Size++
			case "symlink":
				if err := os.Symlink("original", filepath.Join(root, "link")); err != nil {
					t.Fatal(err)
				}
				input.RelativePath = "link"
			case "directory":
				input.RelativePath = "."
			case "public":
				os.Chmod(filepath.Join(root, "original"), 0644)
			}
			inputs["input_0000000001"] = input
			s, err := New(root, inputs, limits)
			if err != nil {
				if mode == "directory" {
					return
				}
				t.Fatal(err)
			}
			defer s.Close()
			if _, err = s.OpenInput(context.Background(), "input_0000000001"); !errors.Is(err, ErrInput) {
				t.Fatal("invalid input handed to provider")
			}
		})
	}
}
func TestRootConfinementAndTraversal(t *testing.T) {
	root, inputs, limits := fixture(t)
	outside := t.TempDir()
	secret := []byte("outside-secret-canary")
	os.WriteFile(filepath.Join(outside, "secret"), secret, 0600)
	os.Symlink(outside, filepath.Join(root, "escape"))
	inputs["input_0000000001"] = Input{RelativePath: "escape/secret", Size: int64(len(secret)), SHA256: sha256.Sum256(secret)}
	s, err := New(root, inputs, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.OpenInput(context.Background(), "input_0000000001"); !errors.Is(err, ErrInput) {
		t.Fatal("outside root accessed")
	}
	for _, path := range []string{"../secret", "/secret", "a/../../secret", "a\\secret"} {
		inputs["input_0000000001"] = Input{RelativePath: path}
		if scope, err := New(root, inputs, limits); err == nil {
			scope.Close()
			t.Fatal("nonlocal path accepted")
		}
	}
}
func TestAggregateBudgetIsAtomic(t *testing.T) {
	root, inputs, limits := fixture(t)
	limits.MaxOutputBytes = 8
	limits.MaxTotalOutputBytes = 8
	s, err := New(root, inputs, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.CreateOutput(context.Background(), ocrplugin.Text)
	b, _ := s.CreateOutput(context.Background(), ocrplugin.Text)
	var wg sync.WaitGroup
	wg.Add(2)
	counts := make(chan int, 2)
	for _, w := range []ocrplugin.ArtifactWriter{a, b} {
		go func(w ocrplugin.ArtifactWriter) { defer wg.Done(); n, _ := w.Write([]byte("12345678")); counts <- n }(w)
	}
	wg.Wait()
	close(counts)
	total := 0
	for n := range counts {
		total += n
	}
	if total != 8 {
		t.Fatalf("aggregate budget exceeded or not used: %d", total)
	}
	a.Close()
	b.Close()
	if _, err = s.Seal(context.Background()); !errors.Is(err, ErrLimit) {
		t.Fatal("partial failed job publishable")
	}
}
func TestOutputLimitsPoisonPublication(t *testing.T) {
	for _, mode := range []string{"bytes", "count"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := create(t)
			w, _ := s.CreateOutput(context.Background(), ocrplugin.Text)
			if mode == "bytes" {
				if n, err := w.Write(make([]byte, 17)); n != 0 || !errors.Is(err, ErrLimit) {
					t.Fatal("oversized write accepted")
				}
			} else {
				s.CreateOutput(context.Background(), ocrplugin.Text)
				if _, err := s.CreateOutput(context.Background(), ocrplugin.Text); !errors.Is(err, ErrLimit) {
					t.Fatal("output count exceeded")
				}
			}
			w.Close()
			if _, err := s.Seal(context.Background()); !errors.Is(err, ErrLimit) {
				t.Fatal("failed output can be sealed")
			}
		})
	}
}
func TestCancellationAndOpenReaderPreventSeal(t *testing.T) {
	s, _ := create(t)
	ctx, cancel := context.WithCancel(context.Background())
	r, err := s.OpenInput(ctx, "input_0000000001")
	if err != nil {
		t.Fatal(err)
	}
	w, _ := s.CreateOutput(context.Background(), ocrplugin.Text)
	w.Write([]byte("text"))
	w.Close()
	if _, err = s.Seal(context.Background()); !errors.Is(err, ErrNotReady) {
		t.Fatal("open input ignored")
	}
	cancel()
	if _, err = r.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled read continued")
	}
	r.Close()
	if _, err = s.Seal(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled seal accepted")
	}
	s.Close()
}
func TestScopeIsolation(t *testing.T) {
	a, _ := create(t)
	b, _ := create(t)
	w, _ := a.CreateOutput(context.Background(), ocrplugin.Text)
	w.Write([]byte("one scope"))
	w.Close()
	if _, err := b.OpenInput(context.Background(), w.Reference()); !errors.Is(err, ErrInput) {
		t.Fatal("cross-scope reference resolved")
	}
}
func TestErrorsDoNotExposePaths(t *testing.T) {
	_, err := New("/missing/secret-canary", nil, Limits{MaxInputBytes: 1, MaxOutputBytes: 1, MaxTotalOutputBytes: 1, MaxOutputs: 1})
	if err == nil || strings.Contains(err.Error(), "secret-canary") {
		t.Fatal("path leaked")
	}
}

func TestRootMustBePrivateDirectory(t *testing.T) {
	root, inputs, limits := fixture(t)
	public := t.TempDir()
	if err := os.Chmod(public, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{public, link, filepath.Join(root, "original"), "relative"} {
		if scope, err := New(candidate, inputs, limits); err == nil {
			scope.Close()
			t.Fatal("unsafe root accepted")
		}
	}
}
func TestAbortClosesAbandonedHandles(t *testing.T) {
	s, _ := create(t)
	r, err := s.OpenInput(context.Background(), "input_0000000001")
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateOutput(context.Background(), ocrplugin.Text)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Read(make([]byte, 1)); !errors.Is(err, ErrClosed) {
		t.Fatal("abandoned reader alive")
	}
	if _, err = r.(*reader).file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("input descriptor not closed")
	}
	if _, err = w.(*writer).file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("output descriptor not closed")
	}
}
func TestCancelledWriterCannotBeSealedWithNewContext(t *testing.T) {
	s, _ := create(t)
	ctx, cancel := context.WithCancel(context.Background())
	w, err := s.CreateOutput(ctx, ocrplugin.Text)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err = w.Write([]byte("late")); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled writer continued")
	}
	w.Close()
	if _, err = s.Seal(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatal("new context revived cancelled output")
	}
}
