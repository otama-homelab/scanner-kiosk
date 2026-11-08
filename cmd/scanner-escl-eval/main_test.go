package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
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
	targetPath := writeCLITarget(t, root)
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"capture", "--endpoint", server.URL + "/eSCL", "--arm", "e1",
		"--region", "0,0,40,20", "--resolution", "300", "--color", "rgb",
		"--target", targetPath, "--sequence", "1", "--output", output, "--receipt", receiptPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("capture failed: %d %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(output, "capture.png")); err != nil {
		t.Fatal(err)
	}
	value, err := readJSONMap(receiptPath)
	if err != nil || value["arm"] != "e1" || value["sha256"] == "" || value["width"] != float64(1) ||
		value["height"] != float64(1) || value["target_sha256"] != targetSHA256 ||
		value["device"] != "canon-ts8130" || value["firmware"] != "2.63" ||
		value["sequence"] != float64(1) || value["image"] != "capture/capture.png" {
		t.Fatalf("receipt=%+v err=%v", value, err)
	}
}

func TestEvaluateC1Command(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "c1.png")
	if err := os.WriteFile(input, cliPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(root, "c1.json")
	targetPath := writeCLITarget(t, root)
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"evaluate-c1", "--input", input, "--region", "0,0,40,20",
		"--resolution", "300", "--color", "rgb", "--target", targetPath, "--sequence", "2",
		"--elapsed-millis", "25", "--receipt", receiptPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("C1 evaluation failed: %d %s", code, stderr.String())
	}
	value, err := readJSONMap(receiptPath)
	if err != nil || value["arm"] != "c1" || value["color"] != "rgb" || value["sha256"] == "" ||
		value["media_type"] != "image/png" || value["sequence"] != float64(2) ||
		value["elapsed_millis"] != float64(25) {
		t.Fatalf("receipt=%+v err=%v", value, err)
	}
}

func TestCompareCompletePhysicalMatrix(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "target.json")
	if err := os.WriteFile(targetPath, cliTargetJSON(), 0o600); err != nil {
		t.Fatal(err)
	}
	var receipts []string
	timings := map[string][]int64{
		"c1": {10, 20, 30},
		"e1": {11, 21, 31},
		"e2": {12, 22, 32},
	}
	sequences := map[string][]int{
		"c1": {10, 20, 30},
		"e1": {11, 21, 31},
		"e2": {12, 22, 32},
	}
	order := []string{"c1", "e1", "e2", "e2", "e1", "c1", "e1", "c1", "e2"}
	counts := map[string]int{}
	for index, arm := range order {
		counts[arm]++
		name := arm + "-" + string(rune('1'+counts[arm]-1)) + ".png"
		imagePath := filepath.Join(root, name)
		if err := os.WriteFile(imagePath, cliMarkerPNG(t, arm), 0o600); err != nil {
			t.Fatal(err)
		}
		receiptPath := filepath.Join(root, arm+"-"+string(rune('1'+counts[arm]-1))+".json")
		if err := writeCLIReceipt(receiptPath, arm, name, sequences[arm][counts[arm]-1],
			timings[arm][counts[arm]-1], index); err != nil {
			t.Fatal(err)
		}
		receipts = append(receipts, receiptPath)
	}
	args := []string{"compare", "--target", targetPath}
	for _, path := range receipts {
		args = append(args, "--receipt", path)
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("compare failed: %d %s %s", code, stderr.String(), stdout.String())
	}
	var evidence struct {
		Format string `json:"format"`
		Status string `json:"status"`
		Arms   []struct {
			Arm          string `json:"arm"`
			CaptureCount int    `json:"capture_count"`
			ElapsedP50   int64  `json:"elapsed_p50_millis"`
			ElapsedP95   int64  `json:"elapsed_p95_millis"`
			Regions      []struct {
				ID             string  `json:"id"`
				Kind           string  `json:"kind"`
				Foreground     int64   `json:"foreground_pixels"`
				RetentionRatio float64 `json:"retention_ratio"`
			} `json:"regions"`
			Captures []struct {
				Sequence         int     `json:"sequence"`
				Width            int     `json:"width"`
				Height           int     `json:"height"`
				ExpectedWidth    int     `json:"expected_width"`
				ExpectedHeight   int     `json:"expected_height"`
				CropWidthPixels  int     `json:"crop_width_pixels"`
				CropHeightPixels int     `json:"crop_height_pixels"`
				ScaleX           float64 `json:"scale_x"`
				ScaleY           float64 `json:"scale_y"`
				RotationDegrees  *int    `json:"rotation_degrees"`
			} `json:"captures"`
		} `json:"arms"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Format != "scanner-escl-input-comparison-evidence" || evidence.Status != "complete" || len(evidence.Arms) != 3 {
		t.Fatalf("evidence=%+v", evidence)
	}
	byArm := map[string]int{}
	for index, arm := range evidence.Arms {
		byArm[arm.Arm] = index
		if arm.CaptureCount != 3 || len(arm.Captures) != 3 || len(arm.Regions) != 2 {
			t.Fatalf("arm=%+v", arm)
		}
		for _, capture := range arm.Captures {
			if capture.Width != 40 || capture.Height != 20 || capture.ExpectedWidth != 40 ||
				capture.ExpectedHeight != 20 || capture.CropWidthPixels != 0 ||
				capture.CropHeightPixels != 0 || capture.ScaleX != 1 || capture.ScaleY != 1 ||
				capture.RotationDegrees == nil || *capture.RotationDegrees != 0 {
				t.Fatalf("geometry=%+v", capture)
			}
		}
	}
	c1 := evidence.Arms[byArm["c1"]]
	e1 := evidence.Arms[byArm["e1"]]
	e2 := evidence.Arms[byArm["e2"]]
	if c1.ElapsedP50 != 20 || c1.ElapsedP95 != 30 ||
		e1.ElapsedP50 != 21 || e1.ElapsedP95 != 31 ||
		e2.ElapsedP50 != 22 || e2.ElapsedP95 != 32 {
		t.Fatalf("timings c1=%+v e1=%+v e2=%+v", c1, e1, e2)
	}
	if regionRetention(c1.Regions, "edge") != 1 || regionRetention(c1.Regions, "content") != 1 ||
		regionRetention(e1.Regions, "edge") != 1 || regionRetention(e1.Regions, "content") != 0 ||
		regionRetention(e2.Regions, "edge") != 0 || regionRetention(e2.Regions, "content") != 1 {
		t.Fatalf("retention c1=%+v e1=%+v e2=%+v", c1.Regions, e1.Regions, e2.Regions)
	}
}

func regionRetention(regions []struct {
	ID             string  `json:"id"`
	Kind           string  `json:"kind"`
	Foreground     int64   `json:"foreground_pixels"`
	RetentionRatio float64 `json:"retention_ratio"`
}, id string) float64 {
	for _, region := range regions {
		if region.ID == id {
			return region.RetentionRatio
		}
	}
	return -1
}

const targetSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func cliTargetJSON() []byte {
	return []byte(`{
  "format": "scanner-escl-input-target",
  "version": 1,
  "target_sha256": "` + targetSHA256 + `",
  "device": "canon-ts8130",
  "firmware": "2.63",
  "region": {"x": 0, "y": 0, "width": 40, "height": 20},
  "resolution": 300,
  "color": "rgb",
  "background_rgb": [255, 255, 255],
  "edge_regions": [{"id": "edge", "x": 0, "y": 0, "width": 0.1, "height": 0.1}],
  "content_regions": [{"id": "content", "x": 0.4, "y": 0.4, "width": 0.2, "height": 0.2}]
}`)
}

func writeCLITarget(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "cli-target.json")
	if err := os.WriteFile(path, cliTargetJSON(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeCLIReceipt(path, arm, imageName string, sequence int, elapsed int64, created int) error {
	imagePath := filepath.Join(filepath.Dir(path), imageName)
	body, err := os.ReadFile(imagePath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	value := map[string]any{
		"format": receiptFormat, "version": 1,
		"created_at": "2026-11-09T00:00:" + string(rune('0'+created)) + "Z",
		"arm":        arm, "region": map[string]int{"x": 0, "y": 0, "width": 40, "height": 20},
		"resolution": 300, "color": "rgb", "media_type": "image/png",
		"bytes": len(body), "sha256": hex.EncodeToString(sum[:]), "width": 40, "height": 20,
		"elapsed_millis": elapsed, "image": imageName, "target_sha256": targetSHA256,
		"device": "canon-ts8130", "firmware": "2.63", "sequence": sequence,
	}
	if arm == "e2" {
		value["intent"] = "Document"
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o600)
}

func readJSONMap(path string) (map[string]any, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	return value, json.Unmarshal(body, &value)
}

func cliMarkerPNG(t *testing.T, arm string) []byte {
	t.Helper()
	bitmap := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			bitmap.Set(x, y, color.White)
		}
	}
	if arm == "c1" || arm == "e1" {
		for y := 0; y < 2; y++ {
			for x := 0; x < 4; x++ {
				bitmap.Set(x, y, color.Black)
			}
		}
	}
	if arm == "c1" || arm == "e2" {
		for y := 8; y < 12; y++ {
			for x := 16; x < 24; x++ {
				bitmap.Set(x, y, color.Black)
			}
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, bitmap); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
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
