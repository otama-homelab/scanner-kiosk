package evaluation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReceiptAppendOnly(t *testing.T) {
	summary, path := benchmarkForReceiptTest(t, "complete", "fixture_photo_001")
	if summary.Status != "complete" {
		t.Fatalf("unexpected benchmark status: %+v", summary)
	}
	if err := writeJSONExclusiveFile(path, summary); !errors.Is(err, ErrInvalid) {
		t.Fatalf("existing receipt was overwritten: %v", err)
	}
	runDir := filepath.Dir(path)
	_, err := RunBenchmark(context.Background(), BenchmarkRequest{
		CatalogPath:   filepath.Join("..", "..", "scripts", "fixtures", "evaluation", "catalog.json"),
		CandidatePath: filepath.Join(filepath.Dir(runDir), "candidate.json"),
		FixtureID:     "fixture_photo_001", RunDir: runDir,
		Cold: 1, Warm: 1, Timeout: 20 * time.Second,
		MaxRSSBytes: 2 << 30, MaxTempDiskBytes: 1 << 30,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("run directory was reused: %v", err)
	}
}

func TestReceiptRequiresProvenance(t *testing.T) {
	summary, _ := benchmarkForReceiptTest(t, "complete", "fixture_photo_001")
	missingLicense := summary
	missingLicense.Candidate.Sources = append([]CandidatePin(nil), summary.Candidate.Sources...)
	missingLicense.Candidate.Sources[0].License = ""
	if err := missingLicense.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing license accepted: %v", err)
	}
	emptySamples := summary
	emptySamples.Samples = nil
	if err := emptySamples.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("complete receipt without samples accepted: %v", err)
	}
	badSource := summary
	badSource.Samples = append([]BenchmarkSample(nil), summary.Samples...)
	badSource.Samples[0].SourceHashesAfter = map[string]string{
		"candidate_config": summary.Samples[0].SourceHashesBefore["candidate_config"],
		"executable":       summary.Samples[0].SourceHashesBefore["executable"],
		"fixture":          summary.Samples[0].SourceHashesBefore["fixture"] + "changed",
	}
	if err := badSource.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("source mismatch accepted: %v", err)
	}
}

func TestCompareSeparatesModesAndFailures(t *testing.T) {
	success, successPath := benchmarkForReceiptTest(t, "complete", "fixture_photo_001")
	failure, failurePath := benchmarkForReceiptTest(t, "", "fixture_a4_ja_en_001")
	if success.Status != "complete" || failure.Status == "complete" {
		t.Fatalf("unexpected summaries: success=%+v failure=%+v", success, failure)
	}
	comparison, err := CompareBenchmarkReceipts([]string{successPath, failurePath})
	if err != nil || len(comparison.Entries) != 4 {
		t.Fatalf("comparison failed: %+v %v", comparison, err)
	}
	var completed, incomplete, cold, warm int
	for _, entry := range comparison.Entries {
		if entry.Status == "complete" {
			completed++
		} else {
			incomplete++
		}
		if entry.Mode == "cold" {
			cold++
		}
		if entry.Mode == "warm" {
			warm++
		}
	}
	if completed != 2 || incomplete != 2 || cold != 2 || warm != 2 {
		t.Fatalf("modes or failures were merged: %+v", comparison.Entries)
	}
	if _, err := CompareBenchmarkReceipts([]string{successPath, successPath}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate receipt accepted: %v", err)
	}
}

func benchmarkForReceiptTest(t *testing.T, mode, fixtureID string) (BenchmarkSummary, string) {
	t.Helper()
	work := t.TempDir()
	runDir := filepath.Join(work, "benchmark")
	configPath := writeCandidateConfig(t, filepath.Join(work, "candidate-output"), mode)
	summary, err := RunBenchmark(context.Background(), BenchmarkRequest{
		CatalogPath:   filepath.Join("..", "..", "scripts", "fixtures", "evaluation", "catalog.json"),
		CandidatePath: configPath, FixtureID: fixtureID, RunDir: runDir,
		Cold: 1, Warm: 1, Timeout: 20 * time.Second,
		MaxRSSBytes: 2 << 30, MaxTempDiskBytes: 1 << 30,
	})
	if err != nil && !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	path := filepath.Join(runDir, "benchmark.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return summary, path
}
