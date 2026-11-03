package pdftextlayer_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"scanner-kiosk/internal/pdftextlayer"
)

func TestOverlayWithoutUserConfiguration(t *testing.T) {
	if os.Getenv("SCANNER_TEST_STATELESS_OVERLAY") == "1" {
		if _, err := pdftextlayer.Overlay(context.Background(), basePDF(t), textLayer(), 80, 100, 1<<20); err != nil {
			t.Fatal(err)
		}
		return
	}
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestOverlayWithoutUserConfiguration$", "-test.count=1")
	cmd.Env = []string{"SCANNER_TEST_STATELESS_OVERLAY=1", "HOME=" + blocked, "XDG_CONFIG_HOME=" + blocked, "TMPDIR=" + root}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("overlay requires writable user configuration: %v\n%s", err, output)
	}
	if data, err := os.ReadFile(blocked); err != nil || string(data) != "unchanged" {
		t.Fatal("user configuration changed")
	}
}
