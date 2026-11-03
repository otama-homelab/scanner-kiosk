//go:build linux

package artifactscope

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
)

func stage(t *testing.T, budget *Budget) *Scope {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	scope, err := NewWithBudget(root, nil, Limits{MaxInputBytes: 100, MaxOutputBytes: 100, MaxTotalOutputBytes: 100, MaxOutputs: 2}, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { scope.Close() })
	return scope
}
func TestSharedBudgetCountsSealedIntermediateUntilDeletion(t *testing.T) {
	budget, err := NewBudget(10)
	if err != nil {
		t.Fatal(err)
	}
	ocr, final := stage(t, budget), stage(t, budget)
	ctx := context.Background()
	first, _ := ocr.CreateArtifact(ctx, PDF)
	if _, err = first.Write([]byte("ocr")); err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = ocr.Seal(ctx); err != nil {
		t.Fatal(err)
	}
	if budget.Used() != 3 || budget.Remaining() != 7 {
		t.Fatal("seal released intermediate bytes")
	}
	output, _ := final.CreateArtifact(ctx, PDF)
	if _, err = output.Write([]byte("1234567")); err != nil {
		t.Fatal(err)
	}
	if budget.Remaining() != 0 {
		t.Fatal("stage outputs not combined")
	}
	if _, err = output.Write([]byte("x")); !errors.Is(err, ErrLimit) {
		t.Fatal("shared limit exceeded")
	}
	if err = final.Close(); err != nil {
		t.Fatal(err)
	}
	if budget.Used() != 3 {
		t.Fatal("failed output refund lost retained intermediate")
	}
	if err = ocr.Close(); err != nil {
		t.Fatal(err)
	}
	if budget.Used() != 0 || budget.Remaining() != 10 {
		t.Fatal("deleted files remain charged")
	}
	if err = ocr.Close(); err != nil || budget.Used() != 0 {
		t.Fatal("double close changed accounting")
	}
}
func TestConcurrentScopesCannotOverdrawBudget(t *testing.T) {
	budget, _ := NewBudget(6)
	a, b := stage(t, budget), stage(t, budget)
	ctx := context.Background()
	wa, _ := a.CreateArtifact(ctx, PDF)
	wb, _ := b.CreateArtifact(ctx, PDF)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, handle := range []*writer{wa.(*writer), wb.(*writer)} {
		wait.Add(1)
		go func(w *writer) { defer wait.Done(); <-start; _, err := w.Write([]byte("four")); results <- err }(handle)
	}
	close(start)
	wait.Wait()
	close(results)
	ok, rejected := 0, 0
	for err := range results {
		if err == nil {
			ok++
		} else if errors.Is(err, ErrLimit) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || rejected != 1 || budget.Used() != 4 {
		t.Fatal("parallel scopes overcharged or bypassed limit")
	}
}
func TestFailedCleanupRetainsChargeAndCanRetry(t *testing.T) {
	budget, _ := NewBudget(10)
	scope := stage(t, budget)
	ctx := context.Background()
	output, _ := scope.CreateArtifact(ctx, PDF)
	if _, err := output.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	rootPath := scope.root.Name()
	if err := scope.root.Close(); err != nil {
		t.Fatal(err)
	} // Inject a broken root descriptor.
	if err := scope.Close(); !errors.Is(err, ErrStorage) || budget.Used() != 4 {
		t.Fatal("unconfirmed deletion refunded bytes")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	scope.root = root
	if err = scope.Close(); err != nil || budget.Used() != 0 {
		t.Fatal("cleanup retry did not refund confirmed deletion")
	}
}
func TestReleaseDoesNotRefundRetainedFiles(t *testing.T) {
	budget, _ := NewBudget(10)
	scope := stage(t, budget)
	ctx := context.Background()
	output, _ := scope.CreateArtifact(ctx, PDF)
	if _, err := output.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.Seal(ctx); err != nil {
		t.Fatal(err)
	}
	if err := scope.Release(); err != nil {
		t.Fatal(err)
	}
	if err := scope.Close(); err != nil || budget.Used() != 4 {
		t.Fatal("retained files refunded")
	}
}
