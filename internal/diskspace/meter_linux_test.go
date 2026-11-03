//go:build linux

package diskspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAvailableAndDescriptorLifecycle(t *testing.T) {
	root := t.TempDir()
	meter, err := Open(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer meter.Close()
	free, err := meter.Available(context.Background())
	if err != nil || free < 0 {
		t.Fatal("capacity measurement failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = meter.Available(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled probe continued")
	}
	if err = meter.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = meter.Available(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed descriptor used")
	}
}
func TestHeadroomAndOverflow(t *testing.T) {
	for _, c := range []struct {
		blocks, unit   uint64
		headroom, want int64
	}{{10, 4096, 4096, 36864}, {1, 4096, 8192, 0}, {1, 4096, 4096, 0}, {^uint64(0), 4096, 1, 1<<63 - 2}} {
		if got := usableBytes(c.blocks, c.unit, c.headroom); got != c.want {
			t.Fatalf("incorrect available budget: %d", got)
		}
	}
}
func TestRejectsUnsafeMeterPaths(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(root, link)
	for _, path := range []string{link, "relative", filepath.Join(root, "missing")} {
		if m, err := Open(path, 0); err == nil {
			m.Close()
			t.Fatal("unsafe meter path accepted")
		}
	}
}
