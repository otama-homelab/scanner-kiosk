//go:build linux

package artifactscope

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileArtifactPinsOneFileAndChargesOnce(t *testing.T) {
	root, inputs, limits := fixture(t)
	budget, _ := NewBudget(16)
	s, err := NewWithBudget(root, inputs, limits, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	output, err := s.CreateFileArtifact(context.Background(), PDF, 16)
	if err != nil {
		t.Fatal(err)
	}
	if budget.Used() != 16 {
		t.Fatal("producer bound not reserved", budget.Used())
	}
	before, err := output.File().Stat()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = output.File().Write([]byte("PDF bytes")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Seal(context.Background()); !errors.Is(err, ErrNotReady) {
		t.Fatal("live producer sealed", err)
	}
	if err = output.CloseAfterStop(); err != nil {
		t.Fatal(err)
	}
	if err = output.CloseAfterStop(); err != nil || budget.Used() != 9 {
		t.Fatal("duplicate charge/refund", err, budget.Used())
	}
	sealed, err := s.Seal(context.Background())
	if err != nil || len(sealed) != 1 || sealed[0].ID != output.Reference() {
		t.Fatal(sealed, err)
	}
	a := sealed[0]
	after, err := os.Stat(filepath.Join(root, a.RelativePath))
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("file was copied/replaced", err)
	}
	if a.Size != 9 || a.SHA256 != sha256.Sum256([]byte("PDF bytes")) {
		t.Fatal("unverified file metadata", a)
	}
	if err = s.Close(); err != nil || budget.Used() != 0 {
		t.Fatal("cleanup charge retained", err, budget.Used())
	}
}
func TestFileArtifactFailureAndPrematureCleanup(t *testing.T) {
	for _, mode := range []string{"overrun", "cancel", "replacement", "premature-close"} {
		t.Run(mode, func(t *testing.T) {
			root, inputs, limits := fixture(t)
			budget, _ := NewBudget(16)
			s, err := NewWithBudget(root, inputs, limits, budget)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			output, err := s.CreateFileArtifact(ctx, PDF, 8)
			if err != nil {
				t.Fatal(err)
			}
			data := []byte("small")
			if mode == "overrun" {
				data = []byte("too large")
			}
			if _, err = output.File().Write(data); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "cancel":
				cancel()
			case "replacement":
				relative := output.writer.metadata.RelativePath + ".tmp"
				if err = s.root.Remove(relative); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(root, relative), []byte("other"), 0600); err != nil {
					t.Fatal(err)
				}
			case "premature-close":
				if err = s.Close(); !errors.Is(err, ErrNotReady) {
					t.Fatal("live file removed", err)
				}
				if _, err = output.File().Write([]byte("x")); err != nil {
					t.Fatal("live descriptor closed", err)
				}
			}
			if err = output.CloseAfterStop(); err == nil {
				t.Fatal("invalid native output accepted")
			}
			if _, err = s.Seal(context.Background()); err == nil {
				t.Fatal("failed output sealed")
			}
			if budget.Used() != 8 {
				t.Fatal("failed producer charge refunded before removal", budget.Used())
			}
			if err = s.Close(); err != nil || budget.Used() != 0 {
				t.Fatal("stopped failure not cleaned", err, budget.Used())
			}
		})
	}
}
func TestFileArtifactSharesBudgetWithOtherScopes(t *testing.T) {
	root, inputs, limits := fixture(t)
	budget, _ := NewBudget(16)
	a, err := NewWithBudget(root, inputs, limits, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := NewWithBudget(root, inputs, limits, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ordinary, err := a.CreateArtifact(context.Background(), PDF)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ordinary.Write([]byte("retained")); err != nil {
		t.Fatal(err)
	}
	if err = ordinary.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = b.CreateFileArtifact(context.Background(), PDF, 9); !errors.Is(err, ErrLimit) {
		t.Fatal("shared capacity overspent", err)
	}
	native, err := b.CreateFileArtifact(context.Background(), PDF, 8)
	if err != nil {
		t.Fatal(err)
	}
	if budget.Used() != 16 {
		t.Fatal(budget.Used())
	}
	if _, err = native.File().Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err = native.CloseAfterStop(); err != nil || budget.Used() != 11 {
		t.Fatal(err, budget.Used())
	}
	if err = b.Close(); err != nil || budget.Used() != 8 {
		t.Fatal("retained stage charge changed", err, budget.Used())
	}
	if err = a.Close(); err != nil || budget.Used() != 0 {
		t.Fatal(err, budget.Used())
	}
}

func TestFileArtifactCreationFailureRefundsReservation(t *testing.T) {
	root, inputs, limits := fixture(t)
	limits.MaxOutputs = 1
	budget, _ := NewBudget(16)
	scope, err := NewWithBudget(root, inputs, limits, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	old, err := scope.CreateArtifact(context.Background(), PDF)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.Write([]byte("old")); err != nil {
		t.Fatal(err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	if native, err := scope.CreateFileArtifact(context.Background(), PDF, 8); native != nil || !errors.Is(err, ErrLimit) {
		t.Fatal(native, err)
	}
	if budget.Used() != 3 {
		t.Fatal("failed creation retained speculative charge", budget.Used())
	}
	if err = scope.Close(); err != nil || budget.Used() != 0 {
		t.Fatal(err, budget.Used())
	}
}
