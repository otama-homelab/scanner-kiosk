package evaluation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultColdRepetitions  = 1
	defaultWarmRepetitions  = 5
	defaultBenchmarkTimeout = 900 * time.Second
	defaultMaxRSSBytes      = 2 << 30
	defaultMaxTempDiskBytes = 1 << 30
	benchmarkTimePath       = "/usr/bin/time"
)

type BenchmarkRequest struct {
	CatalogPath        string
	PrivateCatalogPath string
	PrivateRoot        string
	CandidatePath      string
	FixtureID          string
	RunDir             string
	Cold               int
	Warm               int
	Timeout            time.Duration
	MaxRSSBytes        int64
	MaxTempDiskBytes   int64
}

func RunBenchmark(ctx context.Context, request BenchmarkRequest) (BenchmarkSummary, error) {
	if ctx.Err() != nil {
		return BenchmarkSummary{}, ctx.Err()
	}
	if request.Cold == 0 && request.Warm == 0 {
		request.Cold = defaultColdRepetitions
		request.Warm = defaultWarmRepetitions
	}
	if request.Timeout == 0 {
		request.Timeout = defaultBenchmarkTimeout
	}
	if request.MaxRSSBytes == 0 {
		request.MaxRSSBytes = defaultMaxRSSBytes
	}
	if request.MaxTempDiskBytes == 0 {
		request.MaxTempDiskBytes = defaultMaxTempDiskBytes
	}
	if request.Cold < 0 || request.Warm < 0 || request.Cold+request.Warm < 1 ||
		request.Cold+request.Warm > 100 || request.Timeout < time.Second ||
		request.Timeout > defaultBenchmarkTimeout || request.MaxRSSBytes < 1 ||
		request.MaxTempDiskBytes < 1 || request.FixtureID == "" ||
		!filepath.IsAbs(request.RunDir) || filepath.Clean(request.RunDir) != request.RunDir {
		return BenchmarkSummary{}, ErrInvalid
	}
	if err := os.Mkdir(request.RunDir, 0o700); err != nil {
		return BenchmarkSummary{}, ErrInvalid
	}
	runID, err := newRunID("benchmark_")
	if err != nil {
		return BenchmarkSummary{}, ErrUnavailable
	}
	started := time.Now().UTC()
	summary := BenchmarkSummary{
		Format: BenchmarkFormat, Version: BenchmarkVersion, RunID: runID, Status: "incomplete",
		StartedAt: started.Format(time.RFC3339Nano), FixtureID: request.FixtureID,
		Settings: BenchmarkSettings{
			ColdRepetitions: request.Cold, WarmRepetitions: request.Warm,
			TimeoutSeconds: int(request.Timeout / time.Second),
			MaxRSSBytes:    request.MaxRSSBytes, MaxTempDiskBytes: request.MaxTempDiskBytes,
		},
	}
	finish := func(err error) (BenchmarkSummary, error) {
		summary.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		summary.Metrics = summarizeBenchmark(summary.Samples)
		if writeErr := writeJSONExclusiveFile(filepath.Join(request.RunDir, "benchmark.json"), summary); writeErr != nil {
			return BenchmarkSummary{}, writeErr
		}
		return summary, err
	}
	config, err := LoadCandidateConfig(request.CandidatePath)
	if err != nil {
		summary.FailureClass = "candidate-invalid"
		return finish(ErrInvalid)
	}
	configSHA, err := hashRegularFile(request.CandidatePath, 1<<20)
	if err != nil {
		summary.FailureClass = "candidate-invalid"
		return finish(ErrInvalid)
	}
	observer, err := resolveBenchmarkObserver(ctx, request.RunDir)
	if err != nil {
		summary.FailureClass = "observer-unavailable"
		return finish(ErrUnavailable)
	}
	candidate := CandidateReceipt{
		ID: config.CandidateID, Version: config.CandidateVersion,
		ExecutableSHA256: config.ExecutableSHA256,
		Sources:          append([]CandidatePin(nil), config.Sources...),
		Dependencies:     append([]CandidatePin(nil), config.Dependencies...),
		OperatorSteps:    append([]string{}, config.OperatorSteps...),
		Parameters:       copyStringMap(config.Parameters),
	}
	summary.Candidate = candidate
	summary.CandidateConfigSHA256 = configSHA
	summary.Parameters = copyStringMap(config.Parameters)
	resolvedFixture, _, err := ResolveFixture(
		request.CatalogPath, request.PrivateCatalogPath, request.PrivateRoot, request.FixtureID,
	)
	if err != nil {
		summary.Status = "unavailable"
		summary.FailureClass = "fixture-unavailable"
		return finish(ErrUnavailable)
	}
	summary.FixtureSHA256 = resolvedFixture.SHA256
	summary.Observer = observer
	for _, entry := range config.Environment {
		name, _, _ := strings.Cut(entry, "=")
		summary.EnvironmentNames = append(summary.EnvironmentNames, name)
	}
	sort.Strings(summary.EnvironmentNames)
	summary.InvocationTemplate = append([]string{benchmarkTimePath, "-f", "%e %U %S %M", "-o", "{time_usage}", "--", "{candidate}"}, config.Argv...)
	if err := os.Mkdir(filepath.Join(request.RunDir, "receipts"), 0o700); err != nil {
		summary.FailureClass = "storage-unavailable"
		return finish(ErrUnavailable)
	}
	if err := os.Mkdir(filepath.Join(request.RunDir, "repetitions"), 0o700); err != nil {
		summary.FailureClass = "storage-unavailable"
		return finish(ErrUnavailable)
	}
	sequence := 0
	for _, mode := range []struct {
		name  string
		count int
	}{{"cold", request.Cold}, {"warm", request.Warm}} {
		for index := 0; index < mode.count; index++ {
			if err := ctx.Err(); err != nil {
				summary.FailureClass = "cancelled"
				return finish(err)
			}
			sequence++
			sample := runBenchmarkSample(ctx, request, config, summary, sequence, mode.name, observer)
			if err := writeJSONExclusiveFile(filepath.Join(request.RunDir, sample.ReceiptPath), sample); err != nil {
				summary.FailureClass = "storage-unavailable"
				return finish(ErrUnavailable)
			}
			summary.Samples = append(summary.Samples, sample)
		}
	}
	summary.Status = "complete"
	for _, sample := range summary.Samples {
		if sample.Status != "complete" {
			summary.Status = "incomplete"
			summary.FailureClass = sample.FailureClass
			break
		}
	}
	if summary.Status != "complete" {
		return finish(ErrUnavailable)
	}
	return finish(nil)
}

func runBenchmarkSample(ctx context.Context, request BenchmarkRequest, config CandidateConfig, summary BenchmarkSummary, sequence int, mode string, observer ObserverEvidence) BenchmarkSample {
	started := time.Now().UTC()
	outputRel := filepath.Join("repetitions", fmt.Sprintf("%03d-%s", sequence, mode))
	outputPath := filepath.Join(request.RunDir, outputRel)
	usageName := fmt.Sprintf("%03d-%s.time", sequence, mode)
	usagePath := filepath.Join(request.RunDir, "receipts", usageName)
	before := map[string]string{
		"candidate_config": summary.CandidateConfigSHA256,
		"executable":       config.ExecutableSHA256,
		"fixture":          summary.FixtureSHA256,
	}
	var metrics CandidateMetrics
	artifactCatalog, runErr := RunCandidate(ctx, CandidateRunRequest{
		CatalogPath: request.CatalogPath, PrivateCatalogPath: request.PrivateCatalogPath,
		PrivateRoot: request.PrivateRoot, CandidatePath: request.CandidatePath,
		FixtureID: request.FixtureID, OutputDir: outputPath,
		TimePath: benchmarkTimePath, TimeUsagePath: usagePath,
		Timeout: request.Timeout, MaxRSSBytes: request.MaxRSSBytes,
		MaxTempDiskBytes: request.MaxTempDiskBytes, Metrics: &metrics,
	})
	configAfter, configErr := hashRegularFile(request.CandidatePath, 1<<20)
	executableAfter, executableErr := hashRegularFile(config.Executable, maxArtifactBytes)
	after := map[string]string{
		"candidate_config": configAfter,
		"executable":       executableAfter,
		"fixture":          summary.FixtureSHA256,
	}
	sample := BenchmarkSample{
		SampleID: summary.RunID + "_" + fmt.Sprintf("%03d", sequence),
		Mode:     mode, Sequence: sequence, StartedAt: started.Format(time.RFC3339Nano),
		FinishedAt: time.Now().UTC().Format(time.RFC3339Nano),
		FixtureID:  summary.FixtureID, FixtureSHA256: summary.FixtureSHA256,
		Candidate: summary.Candidate, CandidateConfigSHA256: summary.CandidateConfigSHA256,
		Parameters: copyStringMap(config.Parameters),
		WallMillis: metrics.WallMillis, UserCPUSeconds: metrics.UserCPUSeconds,
		SystemCPUSeconds: metrics.SystemCPUSeconds, PeakRSSBytes: metrics.PeakRSSBytes,
		ExitCode: metrics.ExitCode, SampledTempDiskBytes: metrics.SampledTempDiskBytes,
		TempDiskSampled: metrics.TempDiskSampled, TimedOut: metrics.TimedOut,
		ResourceFailure: metrics.ResourceFailure, Status: "incomplete",
		ValidationStatus: "not-run", ReceiptPath: filepath.Join("receipts", fmt.Sprintf("%03d-%s.json", sequence, mode)),
		SourceHashesBefore: before, SourceHashesAfter: after,
	}
	if artifactCatalog.RunID != "" {
		sample.ArtifactCatalog = filepath.Join(outputRel, "artifact-catalog.json")
		sample.Candidate = artifactCatalog.Candidate
	}
	if artifactCatalog.Validation != nil {
		sample.ValidationStatus = artifactCatalog.Validation.Status
	} else if artifactCatalog.Status == "complete" {
		sample.ValidationStatus = "not-required"
	}
	sample.FailureClass = benchmarkFailureClass(runErr, artifactCatalog, metrics)
	if configErr != nil || executableErr != nil || !sameStringMap(before, after) {
		sample.FailureClass = "source-changed"
	}
	if sample.FailureClass == "" && runErr == nil && artifactCatalog.Status == "complete" &&
		(sample.ValidationStatus == "passed" || sample.ValidationStatus == "not-required") {
		sample.Status = "complete"
	}
	return sample
}

func benchmarkFailureClass(runErr error, catalog ArtifactCatalog, metrics CandidateMetrics) string {
	if metrics.TimedOut {
		return "timeout"
	}
	if metrics.ResourceFailure != "" {
		return metrics.ResourceFailure
	}
	if runErr == nil && catalog.Status == "complete" {
		if catalog.Validation != nil && catalog.Validation.Status != "passed" {
			return "validation-failed"
		}
		return ""
	}
	if len(catalog.MissingOutputs) != 0 {
		return "missing-output"
	}
	if catalog.Validation != nil && catalog.Validation.Status != "passed" {
		return "validation-failed"
	}
	if errors.Is(runErr, ErrInvalid) {
		return "invalid-output"
	}
	return "candidate-failed"
}

func summarizeBenchmark(samples []BenchmarkSample) BenchmarkMetrics {
	return BenchmarkMetrics{
		Cold: summarizeMode(samples, "cold"),
		Warm: summarizeMode(samples, "warm"),
	}
}

func summarizeMode(samples []BenchmarkSample, mode string) ModeMetrics {
	var walls []int64
	var metrics ModeMetrics
	for _, sample := range samples {
		if sample.Mode != mode {
			continue
		}
		if sample.Status != "complete" {
			continue
		}
		if sample.PeakRSSBytes > metrics.PeakRSSBytes {
			metrics.PeakRSSBytes = sample.PeakRSSBytes
		}
		if sample.SampledTempDiskBytes > metrics.MaxTempDiskBytes {
			metrics.MaxTempDiskBytes = sample.SampledTempDiskBytes
		}
		walls = append(walls, sample.WallMillis)
		metrics.UserCPUSeconds += sample.UserCPUSeconds
		metrics.SystemCPUSeconds += sample.SystemCPUSeconds
	}
	metrics.Samples = len(walls)
	if value, ok := nearestRank(walls, 50); ok {
		metrics.P50Millis = &value
	}
	if value, ok := nearestRank(walls, 95); ok {
		metrics.P95Millis = &value
	}
	return metrics
}

func nearestRank(values []int64, percentile int) (int64, bool) {
	if len(values) == 0 || percentile < 1 || percentile > 100 {
		return 0, false
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	rank := (percentile*len(sorted) + 99) / 100
	return sorted[rank-1], true
}

func resolveBenchmarkObserver(ctx context.Context, runDir string) (ObserverEvidence, error) {
	if err := verifyExecutableFile(benchmarkTimePath); err != nil {
		return ObserverEvidence{}, ErrUnavailable
	}
	hash, err := hashRegularFile(benchmarkTimePath, maxArtifactBytes)
	if err != nil {
		return ObserverEvidence{}, ErrUnavailable
	}
	stdout, stderr, err := runTool(ctx, benchmarkTimePath, []string{"--version"}, runDir)
	if err != nil {
		return ObserverEvidence{}, ErrUnavailable
	}
	version := ""
	for _, line := range strings.Split(string(append(stdout, stderr...)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			version = line
			break
		}
	}
	if version == "" {
		return ObserverEvidence{}, ErrUnavailable
	}
	return ObserverEvidence{Path: benchmarkTimePath, SHA256: hash, Version: version, License: "GPL-3.0-or-later"}, nil
}
