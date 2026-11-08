package evaluation

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const (
	BenchmarkFormat  = "scanner-eval-benchmark"
	BenchmarkVersion = 1
	CompareFormat    = "scanner-eval-comparison"
	CompareVersion   = 1
)

type BenchmarkSettings struct {
	ColdRepetitions  int   `json:"cold_repetitions"`
	WarmRepetitions  int   `json:"warm_repetitions"`
	TimeoutSeconds   int   `json:"timeout_seconds"`
	MaxRSSBytes      int64 `json:"max_rss_bytes"`
	MaxTempDiskBytes int64 `json:"max_temp_disk_bytes"`
}

type ObserverEvidence struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
	License string `json:"license"`
}

type BenchmarkSample struct {
	SampleID              string            `json:"sample_id"`
	Mode                  string            `json:"mode"`
	Sequence              int               `json:"sequence"`
	StartedAt             string            `json:"started_at"`
	FinishedAt            string            `json:"finished_at"`
	FixtureID             string            `json:"fixture_id"`
	FixtureSHA256         string            `json:"fixture_sha256"`
	Candidate             CandidateReceipt  `json:"candidate"`
	CandidateConfigSHA256 string            `json:"candidate_config_sha256"`
	Parameters            map[string]string `json:"parameters"`
	WallMillis            int64             `json:"wall_millis"`
	UserCPUSeconds        float64           `json:"user_cpu_seconds"`
	SystemCPUSeconds      float64           `json:"system_cpu_seconds"`
	PeakRSSBytes          int64             `json:"peak_rss_bytes"`
	ExitCode              int               `json:"exit_code"`
	SampledTempDiskBytes  int64             `json:"sampled_temp_disk_bytes"`
	TempDiskSampled       bool              `json:"temp_disk_sampled"`
	TimedOut              bool              `json:"timed_out"`
	ResourceFailure       string            `json:"resource_failure,omitempty"`
	Status                string            `json:"status"`
	FailureClass          string            `json:"failure_class,omitempty"`
	ValidationStatus      string            `json:"validation_status"`
	ArtifactCatalog       string            `json:"artifact_catalog,omitempty"`
	ReceiptPath           string            `json:"receipt_path"`
	SourceHashesBefore    map[string]string `json:"source_hashes_before"`
	SourceHashesAfter     map[string]string `json:"source_hashes_after"`
}

type ModeMetrics struct {
	Samples          int     `json:"samples"`
	P50Millis        *int64  `json:"p50_millis,omitempty"`
	P95Millis        *int64  `json:"p95_millis,omitempty"`
	PeakRSSBytes     int64   `json:"peak_rss_bytes,omitempty"`
	UserCPUSeconds   float64 `json:"user_cpu_seconds,omitempty"`
	SystemCPUSeconds float64 `json:"system_cpu_seconds,omitempty"`
	MaxTempDiskBytes int64   `json:"sampled_temp_disk_bytes,omitempty"`
}

type BenchmarkMetrics struct {
	Cold ModeMetrics `json:"cold"`
	Warm ModeMetrics `json:"warm"`
}

type BenchmarkSummary struct {
	Format                string            `json:"format"`
	Version               int               `json:"version"`
	RunID                 string            `json:"run_id"`
	Status                string            `json:"status"`
	FailureClass          string            `json:"failure_class,omitempty"`
	StartedAt             string            `json:"started_at"`
	FinishedAt            string            `json:"finished_at"`
	FixtureID             string            `json:"fixture_id"`
	FixtureSHA256         string            `json:"fixture_sha256"`
	Candidate             CandidateReceipt  `json:"candidate"`
	CandidateConfigSHA256 string            `json:"candidate_config_sha256"`
	Parameters            map[string]string `json:"parameters"`
	EnvironmentNames      []string          `json:"environment_names"`
	InvocationTemplate    []string          `json:"invocation_template"`
	Observer              ObserverEvidence  `json:"observer"`
	Settings              BenchmarkSettings `json:"settings"`
	Samples               []BenchmarkSample `json:"samples"`
	Metrics               BenchmarkMetrics  `json:"metrics"`
}

type ComparisonEntry struct {
	CandidateID          string   `json:"candidate_id"`
	CandidateVersion     string   `json:"candidate_version"`
	Mode                 string   `json:"mode"`
	Status               string   `json:"status"`
	Samples              int      `json:"samples"`
	P50Millis            *int64   `json:"p50_millis,omitempty"`
	P95Millis            *int64   `json:"p95_millis,omitempty"`
	PeakRSSBytes         int64    `json:"peak_rss_bytes,omitempty"`
	SampledTempDiskBytes int64    `json:"sampled_temp_disk_bytes,omitempty"`
	FailureClasses       []string `json:"failure_classes,omitempty"`
}

type ComparisonGroup struct {
	FixtureID     string            `json:"fixture_id"`
	FixtureSHA256 string            `json:"fixture_sha256"`
	Settings      BenchmarkSettings `json:"settings"`
	Entries       []ComparisonEntry `json:"entries"`
}

type Comparison struct {
	Format  string            `json:"format"`
	Version int               `json:"version"`
	Groups  []ComparisonGroup `json:"groups"`
}

func LoadBenchmarkSummary(receiptPath string) (BenchmarkSummary, error) {
	path := receiptPath
	if info, err := os.Stat(receiptPath); err == nil && info.IsDir() {
		path = filepath.Join(receiptPath, "benchmark.json")
	}
	file, err := os.Open(path)
	if err != nil {
		return BenchmarkSummary{}, ErrInvalid
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 8<<20))
	decoder.DisallowUnknownFields()
	var summary BenchmarkSummary
	if err := decoder.Decode(&summary); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return BenchmarkSummary{}, ErrInvalid
	}
	if err := summary.Validate(); err != nil {
		return BenchmarkSummary{}, err
	}
	return summary, nil
}

func (summary BenchmarkSummary) Validate() error {
	if summary.Format != BenchmarkFormat || summary.Version != BenchmarkVersion ||
		!boundedText(summary.RunID, 96) || !validHash(summary.FixtureSHA256) ||
		!boundedText(summary.Candidate.ID, 96) || !boundedText(summary.Candidate.Version, 64) ||
		!validHash(summary.Candidate.ExecutableSHA256) || !validHash(summary.CandidateConfigSHA256) ||
		summary.Parameters == nil || len(summary.Candidate.Sources) == 0 ||
		summary.Settings.ColdRepetitions < 0 || summary.Settings.WarmRepetitions < 0 ||
		summary.Settings.ColdRepetitions+summary.Settings.WarmRepetitions < 1 ||
		summary.Settings.TimeoutSeconds < 1 || summary.Settings.TimeoutSeconds > 900 ||
		summary.Settings.MaxRSSBytes < 1 || summary.Settings.MaxTempDiskBytes < 1 ||
		!filepath.IsAbs(summary.Observer.Path) || !validHash(summary.Observer.SHA256) ||
		summary.Observer.Version == "" || summary.Observer.License == "" {
		return ErrInvalid
	}
	if !validProvenance(summary.Candidate.Sources, summary.Candidate.Dependencies, summary.Candidate.OperatorSteps) ||
		!validParameters(summary.Parameters) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, sample := range summary.Samples {
		if sample.SampleID == "" || seen[sample.SampleID] ||
			sample.Mode != "cold" && sample.Mode != "warm" ||
			sample.Sequence < 1 || sample.FixtureID != summary.FixtureID ||
			sample.FixtureSHA256 != summary.FixtureSHA256 ||
			sample.Candidate.ID != summary.Candidate.ID ||
			sample.Candidate.Version != summary.Candidate.Version ||
			sample.CandidateConfigSHA256 != summary.CandidateConfigSHA256 ||
			len(sample.Candidate.Sources) == 0 ||
			!validProvenance(sample.Candidate.Sources, sample.Candidate.Dependencies, sample.Candidate.OperatorSteps) ||
			sample.ReceiptPath == "" || len(sample.SourceHashesBefore) == 0 ||
			len(sample.SourceHashesAfter) == 0 ||
			!validParameters(sample.Parameters) {
			return ErrInvalid
		}
		if sample.Status == "complete" &&
			(sample.ValidationStatus != "passed" && sample.ValidationStatus != "not-required" ||
				sample.FailureClass != "" || !sameStringMap(sample.SourceHashesBefore, sample.SourceHashesAfter) ||
				sample.WallMillis <= 0 || sample.PeakRSSBytes <= 0) {
			return ErrInvalid
		}
		seen[sample.SampleID] = true
	}
	if summary.Status == "complete" {
		if len(summary.Samples) != summary.Settings.ColdRepetitions+summary.Settings.WarmRepetitions {
			return ErrInvalid
		}
		for _, sample := range summary.Samples {
			if sample.Status != "complete" {
				return ErrInvalid
			}
		}
	}
	return nil
}

func CompareBenchmarkReceipts(paths []string) (Comparison, error) {
	if len(paths) == 0 {
		return Comparison{}, ErrInvalid
	}
	seenRuns := map[string]bool{}
	seenCandidates := map[string]string{}
	comparison := Comparison{Format: CompareFormat, Version: CompareVersion}
	groupIndexes := map[comparisonGroupKey]int{}
	for _, receiptPath := range paths {
		summary, err := LoadBenchmarkSummary(receiptPath)
		if err != nil || seenRuns[summary.RunID] {
			return Comparison{}, ErrInvalid
		}
		seenRuns[summary.RunID] = true
		identity := summary.Candidate.Version + "|" + summary.Candidate.ExecutableSHA256 + "|" + summary.CandidateConfigSHA256
		if existing, ok := seenCandidates[summary.Candidate.ID]; ok && existing != identity {
			return Comparison{}, ErrInvalid
		}
		seenCandidates[summary.Candidate.ID] = identity
		key := comparisonGroupKey{
			fixtureID: summary.FixtureID, fixtureSHA256: summary.FixtureSHA256,
			settings: summary.Settings,
		}
		groupIndex, ok := groupIndexes[key]
		if !ok {
			groupIndex = len(comparison.Groups)
			groupIndexes[key] = groupIndex
			comparison.Groups = append(comparison.Groups, ComparisonGroup{
				FixtureID: summary.FixtureID, FixtureSHA256: summary.FixtureSHA256,
				Settings: summary.Settings,
			})
		}
		group := &comparison.Groups[groupIndex]
		if summary.Settings.ColdRepetitions > 0 {
			group.Entries = append(group.Entries, comparisonEntry(summary, "cold", summary.Metrics.Cold))
		}
		if summary.Settings.WarmRepetitions > 0 {
			group.Entries = append(group.Entries, comparisonEntry(summary, "warm", summary.Metrics.Warm))
		}
	}
	sort.Slice(comparison.Groups, func(i, j int) bool {
		left, right := comparison.Groups[i], comparison.Groups[j]
		if left.FixtureID != right.FixtureID {
			return left.FixtureID < right.FixtureID
		}
		if left.FixtureSHA256 != right.FixtureSHA256 {
			return left.FixtureSHA256 < right.FixtureSHA256
		}
		return settingsLess(left.Settings, right.Settings)
	})
	for index := range comparison.Groups {
		entries := comparison.Groups[index].Entries
		sort.Slice(entries, func(i, j int) bool {
			left, right := entries[i], entries[j]
			if left.CandidateID != right.CandidateID {
				return left.CandidateID < right.CandidateID
			}
			if left.CandidateVersion != right.CandidateVersion {
				return left.CandidateVersion < right.CandidateVersion
			}
			return left.Mode == "cold" && right.Mode == "warm"
		})
	}
	return comparison, nil
}

type comparisonGroupKey struct {
	fixtureID     string
	fixtureSHA256 string
	settings      BenchmarkSettings
}

func settingsLess(left, right BenchmarkSettings) bool {
	switch {
	case left.ColdRepetitions != right.ColdRepetitions:
		return left.ColdRepetitions < right.ColdRepetitions
	case left.WarmRepetitions != right.WarmRepetitions:
		return left.WarmRepetitions < right.WarmRepetitions
	case left.TimeoutSeconds != right.TimeoutSeconds:
		return left.TimeoutSeconds < right.TimeoutSeconds
	case left.MaxRSSBytes != right.MaxRSSBytes:
		return left.MaxRSSBytes < right.MaxRSSBytes
	default:
		return left.MaxTempDiskBytes < right.MaxTempDiskBytes
	}
}

func comparisonEntry(summary BenchmarkSummary, mode string, metrics ModeMetrics) ComparisonEntry {
	failures := map[string]bool{}
	for _, sample := range summary.Samples {
		if sample.Mode == mode && sample.FailureClass != "" {
			failures[sample.FailureClass] = true
		}
	}
	classes := make([]string, 0, len(failures))
	for class := range failures {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	status := "complete"
	if metrics.Samples == 0 || len(classes) != 0 || summary.Status != "complete" {
		status = "incomplete"
	}
	return ComparisonEntry{
		CandidateID: summary.Candidate.ID, CandidateVersion: summary.Candidate.Version,
		Mode: mode, Status: status, Samples: metrics.Samples, P50Millis: metrics.P50Millis,
		P95Millis: metrics.P95Millis, PeakRSSBytes: metrics.PeakRSSBytes,
		SampledTempDiskBytes: metrics.MaxTempDiskBytes, FailureClasses: classes,
	}
}

func WriteComparison(outputPath string, comparison Comparison) error {
	if outputPath == "" {
		return nil
	}
	if !filepath.IsAbs(outputPath) || filepath.Clean(outputPath) != outputPath {
		return ErrInvalid
	}
	return writeJSONExclusiveFile(outputPath, comparison)
}

func writeJSONExclusiveFile(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrInvalid
	}
	if _, err := file.Write(append(body, '\n')); err != nil {
		file.Close()
		return ErrUnavailable
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return ErrUnavailable
	}
	if err := file.Close(); err != nil {
		return ErrUnavailable
	}
	parent, err := os.Open(filepath.Dir(path))
	if err != nil {
		return ErrUnavailable
	}
	defer parent.Close()
	if err := parent.Sync(); err != nil {
		return ErrUnavailable
	}
	return nil
}

func sameStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
