package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"

	"scanner-kiosk/internal/correction"
)

const (
	CorrectionOracleFormat  = "scanner-eval-correction-oracles"
	CorrectionCompareFormat = "scanner-eval-correction-comparison"
	CorrectionFormatVersion = 1
	maxCorrectionPixels     = 80_000_000
	maxCorrectionReceipts   = 256
	maxCorrectionSamples    = 4096
)

type CorrectionPoint = correction.Point
type CorrectionTransform = correction.Transform

type CorrectionRegion struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type CorrectionOracleEntry struct {
	FixtureID      string             `json:"fixture_id"`
	FixtureSHA256  string             `json:"fixture_sha256"`
	BackgroundRGB  [3]uint8           `json:"background_rgb"`
	EdgeRegions    []CorrectionRegion `json:"edge_regions,omitempty"`
	ContentRegions []CorrectionRegion `json:"content_regions,omitempty"`
}

type CorrectionOracleFile struct {
	Format  string                  `json:"format"`
	Version int                     `json:"version"`
	Entries []CorrectionOracleEntry `json:"entries"`
}

type CorrectionQuality struct {
	Status                 string                    `json:"status"`
	TransformValid         bool                      `json:"transform_valid"`
	EdgeRegions            int                       `json:"edge_regions"`
	EdgeRegionsRetained    int                       `json:"edge_regions_retained"`
	ContentRegions         int                       `json:"content_regions"`
	ContentRegionsRetained int                       `json:"content_regions_retained"`
	OriginalForeground     int64                     `json:"original_foreground_pixels"`
	CorrectionForeground   int64                     `json:"correction_foreground_pixels"`
	ForegroundRetention    float64                   `json:"foreground_retention_ratio"`
	CorrectionWidth        int                       `json:"correction_width"`
	CorrectionHeight       int                       `json:"correction_height"`
	EdgeMeasurements       []CorrectionRegionQuality `json:"edge_measurements"`
	ContentMeasurements    []CorrectionRegionQuality `json:"content_measurements"`
}

type CorrectionRegionQuality struct {
	X                  int     `json:"x"`
	Y                  int     `json:"y"`
	Width              int     `json:"width"`
	Height             int     `json:"height"`
	OriginalForeground int     `json:"original_foreground_pixels"`
	MappedForeground   int     `json:"mapped_foreground_pixels"`
	RetainedForeground int     `json:"retained_foreground_pixels"`
	RetentionRatio     float64 `json:"retention_ratio"`
}

type CorrectionComparisonRow struct {
	FixtureID             string              `json:"fixture_id"`
	FixtureSHA256         string              `json:"fixture_sha256"`
	CandidateID           string              `json:"candidate_id"`
	CandidateVersion      string              `json:"candidate_version"`
	ExecutableSHA256      string              `json:"executable_sha256"`
	CandidateConfigSHA256 string              `json:"candidate_config_sha256"`
	OriginalSHA256        string              `json:"original_sha256"`
	CorrectionID          string              `json:"correction_id"`
	CorrectionSHA256      string              `json:"correction_sha256"`
	MeasurementStatus     string              `json:"measurement_status"`
	Status                string              `json:"status"`
	FailureClass          string              `json:"failure_class,omitempty"`
	Transform             CorrectionTransform `json:"transform"`
	Quality               CorrectionQuality   `json:"quality"`
	Resources             CorrectionResources `json:"resources"`
}

type CorrectionResources struct {
	Status                string           `json:"status"`
	FailureClass          string           `json:"failure_class,omitempty"`
	BenchmarkRunID        string           `json:"benchmark_run_id,omitempty"`
	CandidateID           string           `json:"candidate_id,omitempty"`
	CandidateVersion      string           `json:"candidate_version,omitempty"`
	ExecutableSHA256      string           `json:"executable_sha256,omitempty"`
	CandidateConfigSHA256 string           `json:"candidate_config_sha256,omitempty"`
	OriginalSHA256        string           `json:"original_sha256,omitempty"`
	CorrectionSHA256      string           `json:"correction_sha256,omitempty"`
	Metrics               BenchmarkMetrics `json:"metrics"`
	Sources               []CandidatePin   `json:"sources,omitempty"`
	Dependencies          []CandidatePin   `json:"dependencies,omitempty"`
	OperatorSteps         []string         `json:"operator_steps,omitempty"`
}

type CorrectionComparison struct {
	Format                    string                    `json:"format"`
	Version                   int                       `json:"version"`
	Status                    string                    `json:"status"`
	UnmatchedBenchmarkSamples int                       `json:"unmatched_benchmark_samples,omitempty"`
	Rows                      []CorrectionComparisonRow `json:"rows"`
}

type correctionIdentity struct {
	FixtureID             string
	FixtureSHA256         string
	CandidateID           string
	CandidateVersion      string
	ExecutableSHA256      string
	CandidateConfigSHA256 string
	OriginalSHA256        string
	CorrectionSHA256      string
}

type correctionResourceLookupKey struct {
	fixtureID             string
	fixtureSHA256         string
	candidateID           string
	candidateVersion      string
	executableSHA256      string
	candidateConfigSHA256 string
	originalSHA256        string
	correctionSHA256      string
}

type correctionBenchmarkEntry struct {
	identity correctionIdentity
	summary  BenchmarkSummary
	catalog  ArtifactCatalog
}

type correctionBenchmarkIssue struct {
	identity     correctionIdentity
	failureClass string
	count        int
}

var RequiredCorrectionOracleFixtures = []string{
	"fixture_a4_ja_en_001",
	"fixture_card_001",
	"fixture_receipt_001",
	"fixture_table_form_001",
	"fixture_vertical_text_001",
	"fixture_faint_print_001",
	"fixture_skew_001",
	"fixture_shadow_001",
	"fixture_white_on_white_001",
}

func ValidateCorrectionOracleCoverage(catalog Catalog, oracles CorrectionOracleFile) error {
	if err := catalog.Validate(); err != nil {
		return err
	}
	if err := oracles.Validate(); err != nil {
		return err
	}
	fixtures := map[string]string{}
	for _, fixture := range catalog.Fixtures {
		fixtures[fixture.ID] = fixture.SHA256
	}
	seen := map[string]bool{}
	for _, oracle := range oracles.Entries {
		sha, ok := fixtures[oracle.FixtureID]
		if !ok || sha == "" || sha != oracle.FixtureSHA256 {
			return ErrInvalid
		}
		seen[oracle.FixtureID] = true
	}
	for _, fixtureID := range RequiredCorrectionOracleFixtures {
		if !seen[fixtureID] {
			return ErrInvalid
		}
	}
	return nil
}

func LoadCorrectionOracles(path string) (CorrectionOracleFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return CorrectionOracleFile{}, ErrInvalid
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4<<20))
	decoder.DisallowUnknownFields()
	var oracles CorrectionOracleFile
	if err := decoder.Decode(&oracles); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return CorrectionOracleFile{}, ErrInvalid
	}
	if err := oracles.Validate(); err != nil {
		return CorrectionOracleFile{}, err
	}
	return oracles, nil
}

func (o CorrectionOracleFile) Validate() error {
	if o.Format != CorrectionOracleFormat || o.Version != CorrectionFormatVersion ||
		len(o.Entries) == 0 || len(o.Entries) > 128 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, entry := range o.Entries {
		if !ValidID(entry.FixtureID) || seen[entry.FixtureID] || !validHash(entry.FixtureSHA256) ||
			len(entry.EdgeRegions) > 64 || len(entry.ContentRegions) > 64 {
			return ErrInvalid
		}
		for _, region := range append(append([]CorrectionRegion(nil), entry.EdgeRegions...), entry.ContentRegions...) {
			if region.X < 0 || region.Y < 0 || region.Width < 1 || region.Height < 1 {
				return ErrInvalid
			}
		}
		seen[entry.FixtureID] = true
	}
	return nil
}

func ParseCorrectionTransform(parameters map[string]string, original image.Rectangle) (CorrectionTransform, error) {
	transform, err := correction.Parse(parameters, original)
	if err != nil {
		return CorrectionTransform{}, ErrInvalid
	}
	return transform, nil
}

func AnalyzeCorrectionCatalogs(paths []string, oracles CorrectionOracleFile) (CorrectionComparison, error) {
	if len(paths) == 0 || oracles.Validate() != nil {
		return CorrectionComparison{}, ErrInvalid
	}
	comparison := CorrectionComparison{Format: CorrectionCompareFormat, Version: CorrectionFormatVersion, Status: "complete"}
	for _, path := range paths {
		row := analyzeCorrectionCatalog(path, oracles)
		if row.Status != "passed" || row.MeasurementStatus != "complete" {
			comparison.Status = "incomplete"
		}
		comparison.Rows = append(comparison.Rows, row)
	}
	sort.Slice(comparison.Rows, func(i, j int) bool {
		if comparison.Rows[i].FixtureID != comparison.Rows[j].FixtureID {
			return comparison.Rows[i].FixtureID < comparison.Rows[j].FixtureID
		}
		return comparison.Rows[i].CandidateID < comparison.Rows[j].CandidateID
	})
	return comparison, nil
}

func AnalyzeCorrectionCatalogsWithBenchmarks(artifactPaths, benchmarkPaths []string,
	oracles CorrectionOracleFile) (CorrectionComparison, error) {
	if len(artifactPaths) == 0 || len(artifactPaths) > maxCorrectionReceipts ||
		len(benchmarkPaths) > maxCorrectionReceipts || oracles.Validate() != nil {
		return CorrectionComparison{}, ErrInvalid
	}
	comparison, err := AnalyzeCorrectionCatalogs(artifactPaths, oracles)
	if err != nil {
		return CorrectionComparison{}, err
	}
	entries, issues, err := loadCorrectionBenchmarks(benchmarkPaths)
	if err != nil {
		return CorrectionComparison{}, err
	}
	entriesByIdentity := map[correctionResourceLookupKey][]correctionBenchmarkEntry{}
	for _, entry := range entries {
		key := correctionLookupKey(entry.identity)
		entriesByIdentity[key] = append(entriesByIdentity[key], entry)
	}
	rowIdentities := make([]correctionIdentity, len(comparison.Rows))
	rowIdentityCounts := map[correctionResourceLookupKey]int{}
	for index := range comparison.Rows {
		identity, ok := correctionRowIdentity(comparison.Rows[index])
		if ok {
			rowIdentities[index] = identity
			rowIdentityCounts[correctionLookupKey(identity)]++
		}
	}
	attached := map[string]bool{}
	for index := range comparison.Rows {
		row := &comparison.Rows[index]
		row.Status = "failed"
		identity := rowIdentities[index]
		if identity.CandidateID == "" {
			row.Resources = CorrectionResources{Status: "unavailable", FailureClass: "resource-unavailable"}
			continue
		}
		lookupKey := correctionLookupKey(identity)
		if rowIdentityCounts[lookupKey] > 1 {
			row.Resources = CorrectionResources{Status: "invalid", FailureClass: "duplicate-quality-identity"}
			continue
		}
		matches := entriesByIdentity[lookupKey]
		if len(matches) == 1 {
			entry := matches[0]
			row.Resources = correctionResourcesFromBenchmark(entry)
			row.Status = "failed"
			if row.MeasurementStatus == "complete" && row.Quality.Status == "passed" &&
				row.Resources.Status == "complete" {
				row.Status = "passed"
			}
			attached[entry.summary.RunID] = true
			continue
		}
		if len(matches) > 1 {
			row.Resources = CorrectionResources{Status: "invalid", FailureClass: "resource-duplicate"}
			continue
		}
		related := false
		failureClass := "resource-identity-mismatch"
		for _, issue := range issues {
			if correctionIdentityRelated(identity, issue.identity) {
				related = true
				failureClass = issue.failureClass
				break
			}
		}
		if related {
			row.Resources = CorrectionResources{Status: "invalid", FailureClass: failureClass}
		} else {
			for _, entry := range entries {
				if correctionIdentityConfigMismatch(identity, entry.identity) {
					related = true
					break
				}
			}
		}
		if related {
			row.Resources = CorrectionResources{Status: "invalid", FailureClass: failureClass}
		} else {
			row.Resources = CorrectionResources{Status: "unavailable", FailureClass: "resource-unavailable"}
		}
	}
	for _, entry := range entries {
		if !attached[entry.summary.RunID] {
			comparison.UnmatchedBenchmarkSamples += len(entry.summary.Samples)
		}
	}
	for _, issue := range issues {
		comparison.UnmatchedBenchmarkSamples += issue.count
	}
	comparison.Status = "complete"
	if comparison.UnmatchedBenchmarkSamples != 0 {
		comparison.Status = "incomplete"
	}
	for _, row := range comparison.Rows {
		if row.Status != "passed" {
			comparison.Status = "incomplete"
			break
		}
	}
	return comparison, nil
}

func analyzeCorrectionCatalog(path string, oracles CorrectionOracleFile) CorrectionComparisonRow {
	row := CorrectionComparisonRow{
		MeasurementStatus: "incomplete", Status: "failed",
		Resources: CorrectionResources{Status: "unavailable", FailureClass: "resource-unavailable"},
	}
	catalog, err := loadArtifactCatalog(path)
	if err != nil {
		row.FailureClass = "artifact-catalog"
		return row
	}
	row.FixtureID, row.FixtureSHA256 = catalog.FixtureID, catalog.FixtureSHA256
	row.CandidateID, row.CandidateVersion = catalog.Candidate.ID, catalog.Candidate.Version
	row.ExecutableSHA256 = catalog.Candidate.ExecutableSHA256
	row.CandidateConfigSHA256 = catalog.CandidateConfigSHA256
	if !validHash(catalog.CandidateConfigSHA256) {
		row.FailureClass = "candidate-config"
		return row
	}
	row.MeasurementStatus, row.OriginalSHA256 = catalog.Status, catalog.Original.SHA256
	var oracle *CorrectionOracleEntry
	for index := range oracles.Entries {
		if oracles.Entries[index].FixtureID == catalog.FixtureID {
			oracle = &oracles.Entries[index]
			break
		}
	}
	if oracle == nil || oracle.FixtureSHA256 != catalog.FixtureSHA256 {
		row.FailureClass = "oracle-mismatch"
		return row
	}
	var correction *Artifact
	for index := range catalog.Artifacts {
		if catalog.Artifacts[index].Role == RoleCorrection {
			if correction != nil {
				row.FailureClass = "duplicate-correction"
				return row
			}
			correction = &catalog.Artifacts[index]
		}
	}
	if correction == nil {
		row.FailureClass = "missing-correction"
		return row
	}
	row.CorrectionID, row.CorrectionSHA256 = correction.ID, correction.SHA256
	if !containsString(correction.Parents, catalog.Original.ID) {
		row.FailureClass = "provenance"
		return row
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		row.FailureClass = "artifact-catalog"
		return row
	}
	defer root.Close()
	originalBytes, err := readCorrectionRootFile(root, filepath.Join("artifacts", catalog.Original.ID), maxArtifactBytes)
	if err != nil {
		row.FailureClass = "original-unavailable"
		return row
	}
	correctionBytes, err := readCorrectionRootFile(root, filepath.Join("artifacts", correction.ID), maxArtifactBytes)
	if err != nil {
		row.FailureClass = "correction-unavailable"
		return row
	}
	if fmt.Sprintf("%x", sha256.Sum256(originalBytes)) != catalog.Original.SHA256 ||
		fmt.Sprintf("%x", sha256.Sum256(correctionBytes)) != correction.SHA256 {
		row.FailureClass = "artifact-hash"
		return row
	}
	originalImage, err := decodeCorrectionPNG(originalBytes)
	if err != nil {
		row.FailureClass = "original-image"
		return row
	}
	correctedImage, err := decodeCorrectionPNG(correctionBytes)
	if err != nil {
		row.FailureClass = "correction-image"
		return row
	}
	transform, err := ParseCorrectionTransform(correction.Parameters, originalImage.Bounds())
	if err != nil || transform.OutputWidth != correctedImage.Bounds().Dx() ||
		transform.OutputHeight != correctedImage.Bounds().Dy() {
		row.FailureClass = "transform"
		return row
	}
	row.Transform = transform
	quality := CorrectionQuality{
		Status: "passed", TransformValid: true,
		CorrectionWidth: correctedImage.Bounds().Dx(), CorrectionHeight: correctedImage.Bounds().Dy(),
		EdgeRegions: len(oracle.EdgeRegions), ContentRegions: len(oracle.ContentRegions),
	}
	var regionFailure string
	quality.EdgeMeasurements, regionFailure = measureCorrectionRegions(
		oracle.EdgeRegions, originalImage, correctedImage, transform, oracle.BackgroundRGB,
	)
	for _, measurement := range quality.EdgeMeasurements {
		if measurement.RetainedForeground > 0 {
			quality.EdgeRegionsRetained++
		}
		if measurement.OriginalForeground == 0 {
			regionFailure = "empty-oracle-region"
		}
	}
	quality.ContentMeasurements, regionFailure = measureCorrectionRegions(
		oracle.ContentRegions, originalImage, correctedImage, transform, oracle.BackgroundRGB,
	)
	for _, measurement := range quality.ContentMeasurements {
		if measurement.RetainedForeground > 0 {
			quality.ContentRegionsRetained++
		}
		if measurement.OriginalForeground == 0 {
			regionFailure = "empty-oracle-region"
		}
	}
	quality.OriginalForeground = foregroundPixels(originalImage, oracle.BackgroundRGB)
	quality.CorrectionForeground = foregroundPixels(correctedImage, oracle.BackgroundRGB)
	if quality.OriginalForeground > 0 {
		quality.ForegroundRetention = math.Min(1, float64(quality.CorrectionForeground)/float64(quality.OriginalForeground))
	}
	if quality.EdgeRegionsRetained != quality.EdgeRegions || quality.ContentRegionsRetained != quality.ContentRegions ||
		quality.CorrectionForeground == 0 {
		quality.Status = "failed"
		row.FailureClass = "edge-or-content-loss"
	}
	if regionFailure != "" {
		quality.Status = "failed"
		row.FailureClass = regionFailure
	}
	row.Quality = quality
	if row.MeasurementStatus == "complete" && quality.Status == "passed" {
		row.Status = "passed"
	}
	return row
}

func loadArtifactCatalog(path string) (ArtifactCatalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return ArtifactCatalog{}, ErrInvalid
	}
	defer file.Close()
	return loadArtifactCatalogReader(file)
}

func loadArtifactCatalogReader(reader io.Reader) (ArtifactCatalog, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, 8<<20))
	decoder.DisallowUnknownFields()
	var catalog ArtifactCatalog
	if err := decoder.Decode(&catalog); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		catalog.Format != ArtifactCatalogFormat || catalog.Version != ArtifactCatalogVersion ||
		catalog.Status == "expected-failure" || catalog.Original == nil ||
		!validHash(catalog.Original.SHA256) || !validHash(catalog.FixtureSHA256) {
		return ArtifactCatalog{}, ErrInvalid
	}
	return catalog, nil
}

func loadArtifactCatalogInRoot(root *os.Root, name string) (ArtifactCatalog, error) {
	if !safeRelative(name) {
		return ArtifactCatalog{}, ErrInvalid
	}
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return ArtifactCatalog{}, ErrInvalid
	}
	file, err := root.Open(name)
	if err != nil {
		return ArtifactCatalog{}, ErrInvalid
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return ArtifactCatalog{}, ErrInvalid
	}
	return loadArtifactCatalogReader(file)
}

func readCorrectionRootFile(root *os.Root, name string, limit int64) ([]byte, error) {
	if !safeRelative(name) {
		return nil, ErrUnavailable
	}
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, ErrUnavailable
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, ErrUnavailable
	}
	return body, nil
}

func loadCorrectionBenchmarks(paths []string) ([]correctionBenchmarkEntry, []correctionBenchmarkIssue, error) {
	seenRuns := map[string]bool{}
	totalSamples := 0
	var entries []correctionBenchmarkEntry
	var issues []correctionBenchmarkIssue
	for _, receiptPath := range paths {
		benchmarkPath, summaryDir, err := correctionBenchmarkPath(receiptPath)
		if err != nil {
			return nil, nil, ErrInvalid
		}
		summary, err := LoadBenchmarkSummary(benchmarkPath)
		if err != nil || seenRuns[summary.RunID] {
			return nil, nil, ErrInvalid
		}
		seenRuns[summary.RunID] = true
		if summary.FailureMatch != nil && !*summary.FailureMatch {
			return nil, nil, ErrInvalid
		}
		totalSamples += len(summary.Samples)
		if totalSamples > maxCorrectionSamples {
			return nil, nil, ErrInvalid
		}
		if summary.Status == "expected-failure" {
			continue
		}
		root, err := os.OpenRoot(summaryDir)
		if err != nil {
			issues = append(issues, correctionBenchmarkIssue{
				identity: correctionSummaryIdentity(summary), failureClass: "resource-artifact-catalog",
				count: max(1, len(summary.Samples)),
			})
			continue
		}
		entry, entryIssues := loadCorrectionBenchmarkSummary(root, summary)
		root.Close()
		issues = append(issues, entryIssues...)
		if entry.identity.CandidateID != "" {
			entries = append(entries, entry)
		}
	}
	return entries, issues, nil
}

func correctionBenchmarkPath(path string) (string, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", ErrInvalid
	}
	benchmarkPath := path
	if info.IsDir() {
		benchmarkPath = filepath.Join(path, "benchmark.json")
	}
	if !filepath.IsAbs(benchmarkPath) {
		benchmarkPath, err = filepath.Abs(benchmarkPath)
		if err != nil {
			return "", "", ErrInvalid
		}
	}
	return benchmarkPath, filepath.Dir(benchmarkPath), nil
}

func loadCorrectionBenchmarkSummary(root *os.Root, summary BenchmarkSummary) (correctionBenchmarkEntry, []correctionBenchmarkIssue) {
	baseIdentity := correctionSummaryIdentity(summary)
	issue := func(failure string) (correctionBenchmarkEntry, []correctionBenchmarkIssue) {
		return correctionBenchmarkEntry{}, []correctionBenchmarkIssue{{
			identity: baseIdentity, failureClass: failure, count: max(1, len(summary.Samples)),
		}}
	}
	if summary.Status != "complete" && summary.Status != "incomplete" || len(summary.Samples) == 0 {
		return issue("resource-incomplete")
	}
	var identity correctionIdentity
	var catalog ArtifactCatalog
	for _, sample := range summary.Samples {
		if sample.FixtureID != summary.FixtureID || sample.FixtureSHA256 != summary.FixtureSHA256 ||
			sample.Candidate.ID != summary.Candidate.ID || sample.Candidate.Version != summary.Candidate.Version ||
			sample.Candidate.ExecutableSHA256 != summary.Candidate.ExecutableSHA256 ||
			sample.CandidateConfigSHA256 != summary.CandidateConfigSHA256 ||
			!correctionSourceHashesMatch(sample, summary) {
			return issue("resource-identity-mismatch")
		}
		if !safeRelative(sample.ArtifactCatalog) {
			return issue("resource-artifact-catalog")
		}
		sampleCatalog, err := loadArtifactCatalogInRoot(root, sample.ArtifactCatalog)
		if err != nil {
			return issue("resource-artifact-catalog")
		}
		if sampleCatalog.FixtureID != summary.FixtureID ||
			sampleCatalog.FixtureSHA256 != summary.FixtureSHA256 ||
			sampleCatalog.Candidate.ID != summary.Candidate.ID ||
			sampleCatalog.Candidate.Version != summary.Candidate.Version ||
			sampleCatalog.Candidate.ExecutableSHA256 != summary.Candidate.ExecutableSHA256 ||
			sampleCatalog.CandidateConfigSHA256 != summary.CandidateConfigSHA256 {
			return issue("resource-identity-mismatch")
		}
		original, correctionArtifact, err := correctionArtifactPair(sampleCatalog)
		if err != nil {
			return issue("resource-artifact-catalog")
		}
		artifactRoot := filepath.Dir(sample.ArtifactCatalog)
		originalBytes, originalErr := readCorrectionRootFile(
			root, filepath.Join(artifactRoot, "artifacts", original.ID), maxArtifactBytes,
		)
		correctionBytes, correctionErr := readCorrectionRootFile(
			root, filepath.Join(artifactRoot, "artifacts", correctionArtifact.ID), maxArtifactBytes,
		)
		if originalErr != nil || correctionErr != nil ||
			fmt.Sprintf("%x", sha256.Sum256(originalBytes)) != original.SHA256 ||
			fmt.Sprintf("%x", sha256.Sum256(correctionBytes)) != correctionArtifact.SHA256 {
			return issue("resource-artifact-hash")
		}
		current := baseIdentity
		current.OriginalSHA256 = original.SHA256
		current.CorrectionSHA256 = correctionArtifact.SHA256
		if identity.CandidateID != "" && identity != current {
			return issue("resource-artifact-conflict")
		}
		identity, catalog = current, sampleCatalog
	}
	return correctionBenchmarkEntry{identity: identity, summary: summary, catalog: catalog}, nil
}

func correctionSummaryIdentity(summary BenchmarkSummary) correctionIdentity {
	return correctionIdentity{
		FixtureID: summary.FixtureID, FixtureSHA256: summary.FixtureSHA256,
		CandidateID: summary.Candidate.ID, CandidateVersion: summary.Candidate.Version,
		ExecutableSHA256:      summary.Candidate.ExecutableSHA256,
		CandidateConfigSHA256: summary.CandidateConfigSHA256,
	}
}

func correctionSourceHashesMatch(sample BenchmarkSample, summary BenchmarkSummary) bool {
	for _, hashes := range []map[string]string{sample.SourceHashesBefore, sample.SourceHashesAfter} {
		if hashes["fixture"] != summary.FixtureSHA256 ||
			hashes["candidate_config"] != summary.CandidateConfigSHA256 ||
			hashes["executable"] != summary.Candidate.ExecutableSHA256 {
			return false
		}
	}
	return true
}

func correctionArtifactPair(catalog ArtifactCatalog) (*Artifact, *Artifact, error) {
	if catalog.Original == nil {
		return nil, nil, ErrInvalid
	}
	var correctionArtifact *Artifact
	for index := range catalog.Artifacts {
		if catalog.Artifacts[index].Role != RoleCorrection {
			continue
		}
		if correctionArtifact != nil {
			return nil, nil, ErrInvalid
		}
		correctionArtifact = &catalog.Artifacts[index]
	}
	if correctionArtifact == nil || !containsString(correctionArtifact.Parents, catalog.Original.ID) {
		return nil, nil, ErrInvalid
	}
	return catalog.Original, correctionArtifact, nil
}

func correctionRowIdentity(row CorrectionComparisonRow) (correctionIdentity, bool) {
	identity := correctionIdentity{
		FixtureID: row.FixtureID, FixtureSHA256: row.FixtureSHA256,
		CandidateID: row.CandidateID, CandidateVersion: row.CandidateVersion,
		ExecutableSHA256: row.ExecutableSHA256, CandidateConfigSHA256: row.CandidateConfigSHA256,
		OriginalSHA256: row.OriginalSHA256, CorrectionSHA256: row.CorrectionSHA256,
	}
	return identity, row.FixtureID != "" && row.FixtureSHA256 != "" && row.CandidateID != "" &&
		row.CandidateVersion != "" && row.ExecutableSHA256 != "" && validHash(row.CandidateConfigSHA256) &&
		row.OriginalSHA256 != "" && row.CorrectionSHA256 != ""
}

func correctionLookupKey(identity correctionIdentity) correctionResourceLookupKey {
	return correctionResourceLookupKey{
		fixtureID: identity.FixtureID, fixtureSHA256: identity.FixtureSHA256,
		candidateID: identity.CandidateID, candidateVersion: identity.CandidateVersion,
		executableSHA256:      identity.ExecutableSHA256,
		candidateConfigSHA256: identity.CandidateConfigSHA256,
		originalSHA256:        identity.OriginalSHA256, correctionSHA256: identity.CorrectionSHA256,
	}
}

func correctionResourcesFromBenchmark(entry correctionBenchmarkEntry) CorrectionResources {
	resources := CorrectionResources{
		Status: "complete", BenchmarkRunID: entry.summary.RunID,
		CandidateID: entry.summary.Candidate.ID, CandidateVersion: entry.summary.Candidate.Version,
		ExecutableSHA256:      entry.summary.Candidate.ExecutableSHA256,
		CandidateConfigSHA256: entry.summary.CandidateConfigSHA256,
		OriginalSHA256:        entry.identity.OriginalSHA256, CorrectionSHA256: entry.identity.CorrectionSHA256,
		Metrics:       entry.summary.Metrics,
		Sources:       append([]CandidatePin(nil), entry.summary.Candidate.Sources...),
		Dependencies:  append([]CandidatePin(nil), entry.summary.Candidate.Dependencies...),
		OperatorSteps: append([]string{}, entry.summary.Candidate.OperatorSteps...),
	}
	if entry.summary.Status != "complete" {
		resources.Status = "incomplete"
		resources.FailureClass = entry.summary.FailureClass
		if resources.FailureClass == "" {
			resources.FailureClass = "resource-incomplete"
		}
	}
	return resources
}

func correctionIdentityRelated(row, issue correctionIdentity) bool {
	if row.CandidateID == "" || row.CandidateID != issue.CandidateID {
		return row.OriginalSHA256 != "" && row.OriginalSHA256 == issue.OriginalSHA256 &&
			row.CorrectionSHA256 == issue.CorrectionSHA256
	}
	return row.FixtureID == issue.FixtureID || row.FixtureSHA256 == issue.FixtureSHA256 ||
		row.OriginalSHA256 == issue.OriginalSHA256 && row.CorrectionSHA256 == issue.CorrectionSHA256
}

func correctionIdentityConfigMismatch(row, benchmark correctionIdentity) bool {
	return row.CandidateConfigSHA256 != benchmark.CandidateConfigSHA256 &&
		row.FixtureID == benchmark.FixtureID &&
		row.FixtureSHA256 == benchmark.FixtureSHA256 &&
		row.CandidateID == benchmark.CandidateID &&
		row.CandidateVersion == benchmark.CandidateVersion &&
		row.ExecutableSHA256 == benchmark.ExecutableSHA256 &&
		row.OriginalSHA256 == benchmark.OriginalSHA256 &&
		row.CorrectionSHA256 == benchmark.CorrectionSHA256
}

func decodeCorrectionPNG(body []byte) (image.Image, error) {
	config, err := png.DecodeConfig(bytes.NewReader(body))
	if err != nil || config.Width < 1 || config.Height < 1 ||
		config.Width > maxCorrectionPixels/config.Height {
		return nil, ErrInvalid
	}
	bitmap, err := png.Decode(bytes.NewReader(body))
	if err != nil || bitmap.Bounds().Dx() != config.Width || bitmap.Bounds().Dy() != config.Height {
		return nil, ErrInvalid
	}
	return bitmap, nil
}

func measureCorrectionRegions(regions []CorrectionRegion, original, corrected image.Image,
	transform CorrectionTransform, background [3]uint8) ([]CorrectionRegionQuality, string) {
	measurements := make([]CorrectionRegionQuality, 0, len(regions))
	for _, region := range regions {
		if region.Width < 1 || region.Height < 1 ||
			region.X < original.Bounds().Min.X || region.Y < original.Bounds().Min.Y ||
			region.X+region.Width > original.Bounds().Max.X ||
			region.Y+region.Height > original.Bounds().Max.Y {
			return measurements, "invalid-oracle-region"
		}
		measurement := CorrectionRegionQuality{
			X: region.X, Y: region.Y, Width: region.Width, Height: region.Height,
		}
		for y := region.Y; y < region.Y+region.Height; y++ {
			for x := region.X; x < region.X+region.Width; x++ {
				if !isForeground(original, x, y, background) {
					continue
				}
				measurement.OriginalForeground++
				mapped, ok := correction.MapSourcePoint(
					CorrectionPoint{X: float64(x) + 0.5, Y: float64(y) + 0.5}, transform,
				)
				if !ok {
					continue
				}
				measurement.MappedForeground++
				if isForeground(corrected, int(math.Floor(mapped.X)), int(math.Floor(mapped.Y)), background) {
					measurement.RetainedForeground++
				}
			}
		}
		if measurement.OriginalForeground > 0 {
			measurement.RetentionRatio = float64(measurement.RetainedForeground) /
				float64(measurement.OriginalForeground)
		}
		measurements = append(measurements, measurement)
	}
	return measurements, ""
}

func isForeground(imageValue image.Image, x, y int, background [3]uint8) bool {
	if !image.Pt(x, y).In(imageValue.Bounds()) {
		return false
	}
	r, g, b, _ := imageValue.At(x, y).RGBA()
	return distance(uint8(r>>8), background[0])+
		distance(uint8(g>>8), background[1])+
		distance(uint8(b>>8), background[2]) > 24
}

func foregroundPixels(imageValue image.Image, background [3]uint8) int64 {
	var count int64
	for y := imageValue.Bounds().Min.Y; y < imageValue.Bounds().Max.Y; y++ {
		for x := imageValue.Bounds().Min.X; x < imageValue.Bounds().Max.X; x++ {
			if isForeground(imageValue, x, y, background) {
				count++
			}
		}
	}
	return count
}

func distance(a, b uint8) int {
	value := int(a) - int(b)
	if value < 0 {
		value = -value
	}
	return value
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
