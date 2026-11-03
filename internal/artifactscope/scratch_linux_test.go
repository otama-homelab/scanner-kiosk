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

func scratchScope(t *testing.T, budget *Budget) *Scope {
	t.Helper()
	root := t.TempDir()
	os.Chmod(root, 0700)
	data := []byte("input")
	if err := os.WriteFile(filepath.Join(root, "original"), data, 0600); err != nil {
		t.Fatal(err)
	}
	inputs := map[string]Input{"input_scratch_0001": {RelativePath: "original", Size: 5, SHA256: sha256.Sum256(data)}}
	limits := Limits{MaxInputBytes: 100, MaxOutputBytes: 100, MaxTotalOutputBytes: 100, MaxOutputs: 2}
	var scope *Scope
	var err error
	if budget == nil {
		scope, err = New(root, inputs, limits)
	} else {
		scope, err = NewWithBudget(root, inputs, limits, budget)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { scope.Close() })
	return scope
}
func TestScratchHoldBlocksEarlyScopeCleanup(t *testing.T) {
	budget, _ := NewBudget(30)
	scope := scratchScope(t, budget)
	ctx := context.Background()
	output, _ := scope.CreateArtifact(ctx, PDF)
	if _, err := output.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	hold, err := scope.ReserveScratch(ctx, "input_scratch_0001", 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if hold.InputBytes() != 5 || budget.Used() != 22 {
		t.Fatal("native footprint not charged alongside output")
	}
	if _, err = scope.Seal(ctx); !errors.Is(err, ErrNotReady) {
		t.Fatal("scope sealed while native resources held")
	}
	if err = scope.Close(); !errors.Is(err, ErrNotReady) || budget.Used() != 22 {
		t.Fatal("native hold lost during unconfirmed cleanup")
	}
	hold.ReleaseAfterCleanup()
	hold.ReleaseAfterCleanup()
	if budget.Used() != 4 {
		t.Fatal("scratch release changed output charge")
	}
	if err = scope.Close(); err != nil || budget.Used() != 0 {
		t.Fatal("confirmed cleanup did not restore budget")
	}
}
func TestScratchReservationMustFitBeforeAllocation(t *testing.T) {
	budget, _ := NewBudget(17)
	scope := scratchScope(t, budget)
	if _, err := scope.ReserveScratch(context.Background(), "input_scratch_0001", 10, 2); !errors.Is(err, ErrLimit) || budget.Used() != 0 {
		t.Fatal("unfunded native work reserved")
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestScratchPromotesExistingScopeOutputCharge(t *testing.T) {
	scope := scratchScope(t, nil)
	ctx := context.Background()
	output, _ := scope.CreateArtifact(ctx, PDF)
	if _, err := output.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	output.Close()
	hold, err := scope.ReserveScratch(ctx, "input_scratch_0001", 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if scope.budget.Used() != 22 {
		t.Fatal("preexisting outputs omitted when enabling native accounting")
	}
	hold.ReleaseAfterCleanup()
	if scope.budget.Used() != 4 {
		t.Fatal("old output charge refunded with native scratch")
	}
}
