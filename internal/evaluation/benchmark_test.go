package evaluation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNearestRank(t *testing.T) {
	values := []int64{40, 10, 30, 20}
	if got, ok := nearestRank(values, 50); !ok || got != 20 {
		t.Fatalf("p50=%d ok=%v", got, ok)
	}
	if got, ok := nearestRank(values, 95); !ok || got != 40 {
		t.Fatalf("p95=%d ok=%v", got, ok)
	}
	if _, ok := nearestRank(nil, 50); ok {
		t.Fatal("empty sample set produced a percentile")
	}
}

func TestBenchmarkColdWarmEndToEnd(t *testing.T) {
	root := filepath.Join("..", "..")
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	runDir := filepath.Join(t.TempDir(), "benchmark")
	configPath := writeCandidateConfig(t, filepath.Join(filepath.Dir(runDir), "candidate-output"), "complete")
	summary, err := RunBenchmark(context.Background(), BenchmarkRequest{
		CatalogPath: catalogPath, CandidatePath: configPath, FixtureID: "fixture_photo_001",
		RunDir: runDir, Cold: 1, Warm: 2, Timeout: 20 * time.Second,
		MaxRSSBytes: 2 << 30, MaxTempDiskBytes: 1 << 30,
	})
	if err != nil || summary.Status != "complete" || len(summary.Samples) != 3 {
		t.Fatalf("benchmark failed: summary=%+v err=%v", summary, err)
	}
	if summary.Metrics.Cold.Samples != 1 || summary.Metrics.Warm.Samples != 2 ||
		summary.Metrics.Cold.P50Millis == nil || summary.Metrics.Warm.P95Millis == nil {
		t.Fatalf("cold/warm metrics were mixed or missing: %+v", summary.Metrics)
	}
	for _, sample := range summary.Samples {
		if sample.PeakRSSBytes <= 0 || !sample.TempDiskSampled || sample.Status != "complete" {
			t.Fatalf("incomplete observation: %+v", sample)
		}
		if _, err := os.Stat(filepath.Join(runDir, sample.ReceiptPath)); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(runDir, sample.ArtifactCatalog)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(runDir, "benchmark.json")); err != nil {
		t.Fatal(err)
	}
}

func TestBenchmarkFailureIsNotPass(t *testing.T) {
	root := filepath.Join("..", "..")
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	runDir := filepath.Join(t.TempDir(), "benchmark")
	configPath := writeCandidateConfig(t, filepath.Join(filepath.Dir(runDir), "candidate-output"), "")
	summary, err := RunBenchmark(context.Background(), BenchmarkRequest{
		CatalogPath: catalogPath, CandidatePath: configPath, FixtureID: "fixture_a4_ja_en_001",
		RunDir: runDir, Cold: 1, Warm: 1, Timeout: 20 * time.Second,
		MaxRSSBytes: 2 << 30, MaxTempDiskBytes: 1 << 30,
	})
	if !errors.Is(err, ErrUnavailable) || summary.Status == "complete" ||
		summary.Metrics.Cold.Samples != 0 || summary.Metrics.Warm.Samples != 0 {
		t.Fatalf("failed evidence became a pass: summary=%+v err=%v", summary, err)
	}
	for _, sample := range summary.Samples {
		if sample.Status == "complete" || sample.FailureClass == "" {
			t.Fatalf("failed sample was not explicit: %+v", sample)
		}
	}
}
