package main

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDirectESCLCaptureCommand(t *testing.T) {
	imageBytes := cliPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /eSCL/ScanJobs":
			w.Header().Set("Location", "/eSCL/ScanJobs/job_cli_001")
			w.WriteHeader(http.StatusCreated)
		case "GET /eSCL/ScanJobs/job_cli_001/NextDocument":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageBytes)
		case "DELETE /eSCL/ScanJobs/job_cli_001":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	output := filepath.Join(root, "capture")
	receiptPath := filepath.Join(root, "receipt.json")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"capture", "--endpoint", server.URL + "/eSCL", "--arm", "e1",
		"--region", "0,0,2480,3508", "--resolution", "300", "--color", "rgb",
		"--output", output, "--receipt", receiptPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("capture failed: %d %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(output, "capture.png")); err != nil {
		t.Fatal(err)
	}
	value, err := readReceipt(receiptPath)
	if err != nil || value.Arm != "e1" || value.SHA256 == "" || value.Width != 1 || value.Height != 1 {
		t.Fatalf("receipt=%+v err=%v", value, err)
	}
}

func TestEvaluateC1Command(t *testing.T) {
	input := filepath.Join(t.TempDir(), "c1.png")
	if err := os.WriteFile(input, cliPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(t.TempDir(), "c1.json")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"evaluate-c1", "--input", input, "--region", "0,0,2480,3508",
		"--resolution", "300", "--color", "gray", "--receipt", receiptPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("C1 evaluation failed: %d %s", code, stderr.String())
	}
	value, err := readReceipt(receiptPath)
	if err != nil || value.Arm != "c1" || value.Color != "gray" || value.SHA256 == "" {
		t.Fatalf("receipt=%+v err=%v", value, err)
	}
}

func cliPNG(t *testing.T) []byte {
	t.Helper()
	bitmap := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var output bytes.Buffer
	if err := png.Encode(&output, bitmap); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
