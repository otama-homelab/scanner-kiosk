package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	summary.Candidate.OperatorSteps = []string{}
	if err := summary.Validate(); err != nil {
		t.Fatalf("explicit empty operator evidence was rejected: %v", err)
	}
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
	missingSteps := summary
	missingSteps.Candidate.OperatorSteps = nil
	if err := missingSteps.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("receipt without operator evidence was accepted: %v", err)
	}
	missingDependencyHash := summary
	missingDependencyHash.Candidate.Dependencies = []CandidatePin{{
		Name: "jpn.traineddata", Version: "1", License: "reviewed-license",
	}}
	if err := missingDependencyHash.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("receipt dependency without SHA256 was accepted: %v", err)
	}
	missingDependencyLicense := summary
	missingDependencyLicense.Candidate.Dependencies = []CandidatePin{{
		Name: "jpn.traineddata", Version: "1", SHA256: repeatTestHash("b"),
	}}
	if err := missingDependencyLicense.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("receipt dependency without a license was accepted: %v", err)
	}
	sourceWithoutHash := summary
	sourceWithoutHash.Candidate.Sources = []CandidatePin{{
		Name: "scanner-kiosk", Version: "test", License: "test-only",
	}}
	if err := sourceWithoutHash.Validate(); err != nil {
		t.Fatalf("receipt source pin without an archive hash was rejected: %v", err)
	}
}

func TestComparisonGroupsFixtureAndSettings(t *testing.T) {
	photo, photoPath := benchmarkForReceiptTest(t, "complete", "fixture_photo_001")
	_, a4Path := benchmarkForReceiptTest(t, "", "fixture_a4_ja_en_001")

	differentSettings := photo
	differentSettings.RunID = "benchmark_synthetic_settings"
	differentSettings.Settings.TimeoutSeconds++
	differentSettingsPath := writeReceiptSummary(t, differentSettings)

	differentHash := photo
	differentHash.RunID = "benchmark_synthetic_hash"
	differentHash.FixtureSHA256 = repeatTestHash("b")
	for index := range differentHash.Samples {
		differentHash.Samples[index].FixtureSHA256 = differentHash.FixtureSHA256
	}
	differentHashPath := writeReceiptSummary(t, differentHash)

	comparison, err := CompareBenchmarkReceipts([]string{photoPath, a4Path, differentSettingsPath, differentHashPath})
	if err != nil {
		t.Fatal(err)
	}
	decoded := marshalComparison(t, comparison)
	if len(decoded.Groups) != 4 {
		t.Fatalf("groups=%d, want 4: %+v", len(decoded.Groups), decoded.Groups)
	}
	seen := map[string]bool{}
	for _, group := range decoded.Groups {
		if group.FixtureID == "" || group.FixtureSHA256 == "" ||
			group.Settings.ColdRepetitions < 0 || group.Settings.WarmRepetitions < 0 ||
			group.Settings.TimeoutSeconds < 1 || group.Settings.MaxRSSBytes < 1 ||
			group.Settings.MaxTempDiskBytes < 1 || len(group.Entries) == 0 {
			t.Fatalf("incomplete group identity: %+v", group)
		}
		key := fmt.Sprintf("%s|%s|%+v", group.FixtureID, group.FixtureSHA256, group.Settings)
		if seen[key] {
			t.Fatalf("duplicate comparison group: %s", key)
		}
		seen[key] = true
	}
}

func TestComparisonKeepsModesAndFailuresPerGroup(t *testing.T) {
	success, successPath := benchmarkForReceiptTest(t, "complete", "fixture_photo_001")
	failure, failurePath := benchmarkForReceiptTest(t, "", "fixture_photo_001")
	if success.Status != "complete" || failure.Status == "complete" {
		t.Fatalf("unexpected summaries: success=%+v failure=%+v", success, failure)
	}
	comparison, err := CompareBenchmarkReceipts([]string{successPath, failurePath})
	if err != nil {
		t.Fatal(err)
	}
	decoded := marshalComparison(t, comparison)
	if len(decoded.Groups) != 1 || len(decoded.Groups[0].Entries) != 4 {
		t.Fatalf("comparison groups failed: %+v", decoded)
	}
	var completed, incomplete, cold, warm int
	for _, entry := range decoded.Groups[0].Entries {
		if entry.Status == "complete" {
			completed++
			if len(entry.FailureClasses) != 0 {
				t.Fatalf("complete entry retained a failure class: %+v", entry)
			}
		} else {
			incomplete++
			if len(entry.FailureClasses) == 0 {
				t.Fatalf("failed entry lost its failure class: %+v", entry)
			}
		}
		if entry.Mode == "cold" {
			cold++
		}
		if entry.Mode == "warm" {
			warm++
		}
	}
	if completed != 2 || incomplete != 2 || cold != 2 || warm != 2 {
		t.Fatalf("modes or failures were merged: %+v", decoded.Groups[0].Entries)
	}
	if _, err := CompareBenchmarkReceipts([]string{successPath, successPath}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate receipt accepted: %v", err)
	}
	conflict := success
	conflict.RunID = "benchmark_synthetic_conflict"
	conflict.Candidate.ExecutableSHA256 = repeatTestHash("c")
	conflictPath := writeReceiptSummary(t, conflict)
	if _, err := CompareBenchmarkReceipts([]string{successPath, conflictPath}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("conflicting candidate identity accepted: %v", err)
	}
}

func TestComparisonPreservesExpectedFailureSummary(t *testing.T) {
	expected, expectedPath := benchmarkForReceiptTest(t, "", "fixture_invalid_input_001")
	if expected.Status != "expected-failure" || expected.FailureMatch == nil || !*expected.FailureMatch {
		t.Fatalf("unexpected expected-failure summary: %+v", expected)
	}

	normal, _ := benchmarkForReceiptTest(t, "complete", "fixture_photo_001")
	normal.RunID = "benchmark_synthetic_normal"
	normal.FixtureID = expected.FixtureID
	normal.FixtureSHA256 = expected.FixtureSHA256
	for index := range normal.Samples {
		normal.Samples[index].FixtureID = normal.FixtureID
		normal.Samples[index].FixtureSHA256 = normal.FixtureSHA256
	}
	normalPath := writeReceiptSummary(t, normal)

	comparison, err := CompareBenchmarkReceipts([]string{normalPath, expectedPath})
	if err != nil {
		t.Fatal(err)
	}
	decoded := marshalComparison(t, comparison)
	if len(decoded.Groups) != 2 {
		t.Fatalf("expected-failure collided with a normal group: %+v", decoded.Groups)
	}
	var failureGroup *decodedComparisonGroup
	for index := range decoded.Groups {
		if decoded.Groups[index].FailureMatch != nil {
			failureGroup = &decoded.Groups[index]
		}
	}
	if failureGroup == nil || failureGroup.ExpectedFailureClass != expected.ExpectedFailureClass ||
		failureGroup.ObservedFailureClass != expected.ObservedFailureClass ||
		!*failureGroup.FailureMatch || failureGroup.FixtureID != expected.FixtureID ||
		failureGroup.FixtureSHA256 != expected.FixtureSHA256 ||
		failureGroup.Settings != expected.Settings {
		t.Fatalf("expected-failure group identity was lost: %+v", decoded.Groups)
	}
	for _, entry := range failureGroup.Entries {
		if entry.Samples != 0 || entry.P50Millis != nil || entry.P95Millis != nil ||
			entry.PeakRSSBytes != 0 || entry.SampledTempDiskBytes != 0 {
			t.Fatalf("expected failure fabricated sample metrics: %+v", entry)
		}
	}

	mismatch := expected
	mismatch.RunID = "benchmark_synthetic_mismatch"
	matched := false
	mismatch.FailureMatch = &matched
	mismatchPath := writeReceiptSummary(t, mismatch)
	if _, err := CompareBenchmarkReceipts([]string{mismatchPath}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mismatched expected failure was accepted: %v", err)
	}

	incomplete := expected
	incomplete.RunID = "benchmark_synthetic_zero"
	incomplete.Status = "incomplete"
	incomplete.ExpectedFailureClass = ""
	incomplete.ObservedFailureClass = ""
	incomplete.FailureMatch = nil
	incompletePath := writeReceiptSummary(t, incomplete)
	if _, err := CompareBenchmarkReceipts([]string{incompletePath}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero-sample summary without expected-failure evidence was accepted: %v", err)
	}
}

type decodedComparison struct {
	Groups []decodedComparisonGroup `json:"groups"`
}

type decodedComparisonGroup struct {
	FixtureID            string            `json:"fixture_id"`
	FixtureSHA256        string            `json:"fixture_sha256"`
	Settings             BenchmarkSettings `json:"settings"`
	ExpectedFailureClass string            `json:"expected_failure_class"`
	ObservedFailureClass string            `json:"observed_failure_class"`
	FailureMatch         *bool             `json:"failure_match"`
	Entries              []struct {
		CandidateID          string   `json:"candidate_id"`
		CandidateVersion     string   `json:"candidate_version"`
		Mode                 string   `json:"mode"`
		Status               string   `json:"status"`
		Samples              int      `json:"samples"`
		P50Millis            *int64   `json:"p50_millis"`
		P95Millis            *int64   `json:"p95_millis"`
		PeakRSSBytes         int64    `json:"peak_rss_bytes"`
		SampledTempDiskBytes int64    `json:"sampled_temp_disk_bytes"`
		FailureClasses       []string `json:"failure_classes"`
	} `json:"entries"`
}

func marshalComparison(t *testing.T, comparison Comparison) decodedComparison {
	t.Helper()
	body, err := json.Marshal(comparison)
	if err != nil {
		t.Fatal(err)
	}
	var decoded decodedComparison
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func writeReceiptSummary(t *testing.T, summary BenchmarkSummary) string {
	t.Helper()
	runDir := filepath.Join(t.TempDir(), "benchmark")
	if err := os.Mkdir(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runDir, "benchmark.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func repeatTestHash(value string) string {
	repeated := ""
	for range 64 {
		repeated += value
	}
	return repeated
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
