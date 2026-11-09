package evaluation

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"scanner-kiosk/pkg/ocrplugin"
)

type OCROracleSpan struct {
	Text  string     `json:"text"`
	Order int        `json:"order"`
	Box   [4]float64 `json:"box"`
	Cell  string     `json:"cell,omitempty"`
}
type OCRPageOracle struct {
	PageID       string          `json:"page_id"`
	SourceSHA256 string          `json:"source_sha256"`
	Width        int             `json:"width"`
	Height       int             `json:"height"`
	Blank        bool            `json:"blank"`
	Spans        []OCROracleSpan `json:"spans"`
}
type OCRPageQuality struct {
	PageID                 string `json:"page_id"`
	TextErrors             int    `json:"text_errors"`
	ReferenceCodePoints    int    `json:"reference_code_points"`
	ReadingOrderInversions int    `json:"reading_order_inversions"`
	MatchedRegions         int    `json:"matched_regions"`
	Regions                int    `json:"regions"`
	MatchedCells           int    `json:"matched_cells"`
	Cells                  int    `json:"cells"`
	Status                 string `json:"status"`
}
type OCRComparison struct {
	Format         string             `json:"format"`
	Version        int                `json:"version"`
	Status         string             `json:"status"`
	Normalization  string             `json:"normalization"`
	Units          string             `json:"units"`
	Rows           []OCRComparisonRow `json:"rows"`
	ManifestSHA256 string             `json:"manifest_sha256"`
	OracleSHA256   string             `json:"oracle_sha256"`
}
type OCRComparisonRow struct {
	EngineID              string                `json:"engine_id"`
	FixtureID             string                `json:"fixture_id"`
	FixtureSHA256         string                `json:"fixture_sha256"`
	Pages                 []OCRPageQuality      `json:"pages"`
	RunID                 string                `json:"run_id"`
	CandidateVersion      string                `json:"candidate_version"`
	CandidateConfigSHA256 string                `json:"candidate_config_sha256"`
	ExecutableSHA256      string                `json:"executable_sha256"`
	PDFStatus             string                `json:"pdf_status"`
	LicenseStatus         string                `json:"license_status"`
	ResourceStatus        string                `json:"resource_status"`
	Mode                  string                `json:"mode"`
	WallMillis            int64                 `json:"wall_millis"`
	PeakRSSBytes          int64                 `json:"peak_rss_bytes"`
	TempDiskBytes         int64                 `json:"sampled_temp_disk_bytes"`
	CatalogSHA256         string                `json:"artifact_catalog_sha256"`
	Artifacts             []OCRArtifactIdentity `json:"artifacts"`
	Provenance            []OCRPinEvidence      `json:"provenance"`
	Resources             *BenchmarkMetrics     `json:"resources,omitempty"`
}

type OCRArtifactIdentity struct {
	ID          string       `json:"id"`
	Role        ArtifactRole `json:"role"`
	SHA256      string       `json:"sha256"`
	SourcePages []string     `json:"source_pages"`
}
type OCRPinEvidence struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Version string `json:"version"`
	License string `json:"license"`
	SHA256  string `json:"sha256"`
}

func AnalyzeOCRPage(oracle OCRPageOracle, text string, positions ocrplugin.PositionedPage) (OCRPageQuality, error) {
	q := OCRPageQuality{PageID: oracle.PageID, Status: "failed", Regions: len(oracle.Spans)}
	if validateOCROraclePage(oracle) != nil || !utf8.ValidString(text) || len(text) > 1<<20 || positions.PageID != oracle.PageID || positions.Width != oracle.Width || positions.Height != oracle.Height || len(positions.Spans) > 4096 {
		return q, ErrInvalid
	}
	for _, span := range positions.Spans {
		if !utf8.ValidString(span.Text) || len(span.Text) > 16384 || !ocrBox([4]float64{span.Box.X, span.Box.Y, span.Box.Width, span.Box.Height}, oracle.Width, oracle.Height) {
			return q, ErrInvalid
		}
	}
	ordered := append([]OCROracleSpan(nil), oracle.Spans...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Order < ordered[j].Order })
	var reference strings.Builder
	for _, span := range ordered {
		reference.WriteString(span.Text)
	}
	a, b := []rune(norm.NFC.String(reference.String())), []rune(norm.NFC.String(text))
	if len(a) > 16384 || len(b) > 16384 {
		return q, ErrInvalid
	}
	q.ReferenceCodePoints = len(a)
	q.TextErrors = ocrDistance(a, b)
	groups := make([]strings.Builder, len(ordered))
	var order []int
	for _, span := range positions.Spans {
		x, y := span.Box.X+span.Box.Width/2, span.Box.Y+span.Box.Height/2
		match := -1
		for i, expected := range ordered {
			box := expected.Box
			if x >= box[0] && x < box[0]+box[2] && y >= box[1] && y < box[1]+box[3] {
				if match != -1 {
					match = -2
					break
				}
				match = i
			}
		}
		if match >= 0 {
			groups[match].WriteString(span.Text)
			order = append(order, match)
		}
	}
	for i, span := range ordered {
		matched := norm.NFC.String(groups[i].String()) == norm.NFC.String(span.Text)
		if matched {
			q.MatchedRegions++
		}
		if span.Cell != "" {
			q.Cells++
			if matched {
				q.MatchedCells++
			}
		}
	}
	for i := range order {
		for j := i + 1; j < len(order); j++ {
			if order[i] > order[j] {
				q.ReadingOrderInversions++
			}
		}
	}
	if q.TextErrors == 0 && q.ReadingOrderInversions == 0 && q.MatchedRegions == q.Regions {
		q.Status = "passed"
	}
	return q, nil
}

func ocrDistance(a, b []rune) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i, x := range a {
		prev := row[0]
		row[0] = i + 1
		for j, y := range b {
			old := row[j+1]
			cost := 0
			if x != y {
				cost = 1
			}
			row[j+1] = min(row[j+1]+1, row[j]+1, prev+cost)
			prev = old
		}
	}
	return row[len(b)]
}
func ocrBox(box [4]float64, w, h int) bool {
	for _, v := range box {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return w > 0 && h > 0 && box[0] >= 0 && box[1] >= 0 && box[2] > 0 && box[3] > 0 && box[0]+box[2] <= float64(w) && box[1]+box[3] <= float64(h)
}
func validateOCROraclePage(p OCRPageOracle) error {
	if !ValidID(p.PageID) || !validHash(p.SourceSHA256) || p.Width < 1 || p.Height < 1 || int64(p.Width)*int64(p.Height) > 80_000_000 || len(p.Spans) > 4096 || p.Blank != (len(p.Spans) == 0) {
		return ErrInvalid
	}
	total := 0
	for _, s := range p.Spans {
		total += len(s.Text)
		if s.Order < 0 || s.Order > 4096 || s.Text == "" || !utf8.ValidString(s.Text) || total > 16384 || !ocrBox(s.Box, p.Width, p.Height) || len(s.Cell) > 96 {
			return ErrInvalid
		}
	}
	return nil
}

type OCROracleEntry struct {
	FixtureID     string          `json:"fixture_id"`
	FixtureSHA256 string          `json:"fixture_sha256"`
	Layout        string          `json:"layout"`
	Pages         []OCRPageOracle `json:"pages"`
}
type OCROracleFile struct {
	Format        string `json:"format"`
	Version       int    `json:"version"`
	Normalization string `json:"normalization"`
	Units         string `json:"units"`
	Font          struct {
		Name    string `json:"name"`
		SHA256  string `json:"sha256"`
		Source  string `json:"source"`
		License string `json:"license"`
	} `json:"font"`
	Renderer struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"renderer"`
	Entries []OCROracleEntry `json:"entries"`
}
type OCRFileRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type OCRIdentity struct {
	Kind    string     `json:"kind"`
	Name    string     `json:"name"`
	Version string     `json:"version"`
	License string     `json:"license"`
	File    OCRFileRef `json:"file"`
}
type OCRManifestPair struct {
	EngineID        string        `json:"engine_id"`
	FixtureID       string        `json:"fixture_id"`
	CandidateConfig OCRFileRef    `json:"candidate_config"`
	Identities      []OCRIdentity `json:"identities"`
	Standalone      OCRFileRef    `json:"standalone"`
	Benchmark       OCRFileRef    `json:"benchmark"`
	Samples         []OCRFileRef  `json:"samples"`
	PageSources     []OCRFileRef  `json:"page_sources"`
	SampleCatalogs  []OCRFileRef  `json:"sample_catalogs"`
}
type OCRRunManifest struct {
	Format       string            `json:"format"`
	Version      int               `json:"version"`
	EvidenceRoot string            `json:"evidence_root"`
	OracleSHA256 string            `json:"oracle_sha256"`
	Pairs        []OCRManifestPair `json:"pairs"`
}

func decodeOCR(body []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(&struct{}{}) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func LoadOCROracles(name string) (OCROracleFile, error) {
	var out OCROracleFile
	body, err := readBoundedFile(name, 4<<20)
	if err != nil || decodeOCR(body, &out) != nil || out.Format != "scanner-eval-ocr-oracles" || out.Version != 1 || out.Normalization != "NFC" || out.Units != "unicode-code-points" || !validHash(out.Font.SHA256) || out.Font.License == "" || len(out.Entries) < 1 || len(out.Entries) > 128 {
		return out, ErrInvalid
	}
	seen := map[string]bool{}
	for _, e := range out.Entries {
		if !ValidID(e.FixtureID) || seen[e.FixtureID] || !validHash(e.FixtureSHA256) || len(e.Pages) < 1 || len(e.Pages) > 256 {
			return out, ErrInvalid
		}
		seen[e.FixtureID] = true
		pages := map[string]bool{}
		for _, p := range e.Pages {
			if pages[p.PageID] || validateOCROraclePage(p) != nil {
				return out, ErrInvalid
			}
			pages[p.PageID] = true
		}
	}
	return out, nil
}

func readOCREvidence(root *os.Root, ref OCRFileRef, limit int64) ([]byte, error) {
	if !safeRelative(ref.Path) || !validHash(ref.SHA256) {
		return nil, ErrInvalid
	}
	body, err := readCorrectionRootFile(root, ref.Path, limit)
	if err != nil || hexSHA256(body) != ref.SHA256 {
		return nil, ErrInvalid
	}
	return body, nil
}

func AnalyzeOCREvidence(oraclePath, manifestPath string, engines []string, complete bool) (OCRComparison, error) {
	out := OCRComparison{Format: "scanner-eval-ocr-comparison", Version: 1, Status: "incomplete", Normalization: "NFC", Units: "unicode-code-points", Rows: []OCRComparisonRow{}}
	oracles, err := LoadOCROracles(oraclePath)
	if err != nil {
		return out, err
	}
	body, err := readBoundedFile(manifestPath, 4<<20)
	var manifest OCRRunManifest
	if err != nil || decodeOCR(body, &manifest) != nil || manifest.Format != "scanner-eval-ocr-run-manifest" || manifest.Version != 1 || len(manifest.Pairs) < 1 || len(manifest.Pairs) > 256 || !filepath.IsAbs(manifest.EvidenceRoot) || filepath.Clean(manifest.EvidenceRoot) != manifest.EvidenceRoot {
		return out, ErrInvalid
	}
	oracleSHA, err := hashRegularFile(oraclePath, 4<<20)
	if err != nil || manifest.OracleSHA256 != oracleSHA {
		return out, ErrInvalid
	}
	out.ManifestSHA256 = hexSHA256(body)
	out.OracleSHA256 = oracleSHA
	info, err := os.Lstat(manifest.EvidenceRoot)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return out, ErrInvalid
	}
	root, err := os.OpenRoot(manifest.EvidenceRoot)
	if err != nil {
		return out, ErrInvalid
	}
	defer root.Close()
	required := map[string]bool{}
	for _, engine := range engines {
		if !boundedToken(engine, 96) || required[engine] {
			return out, ErrInvalid
		}
		required[engine] = true
	}
	if complete && len(required) == 0 {
		return out, ErrInvalid
	}
	entries := map[string]OCROracleEntry{}
	for _, entry := range oracles.Entries {
		entries[entry.FixtureID] = entry
	}
	seen := map[string]bool{}
	for _, pair := range manifest.Pairs {
		entry, ok := entries[pair.FixtureID]
		key := pair.EngineID + "\x00" + pair.FixtureID
		if !ok || seen[key] || !boundedToken(pair.EngineID, 96) || len(required) > 0 && !required[pair.EngineID] {
			return out, ErrInvalid
		}
		seen[key] = true
		rows, err := analyzeOCRPair(root, pair, entry, oracles.Font.SHA256)
		if err != nil {
			return out, err
		}
		out.Rows = append(out.Rows, rows...)
	}
	if complete {
		for engine := range required {
			for fixture := range entries {
				if !seen[engine+"\x00"+fixture] {
					return out, ErrInvalid
				}
			}
		}
	}
	out.Status = "complete"
	return out, nil
}

func analyzeOCRPair(root *os.Root, pair OCRManifestPair, oracle OCROracleEntry, fontSHA string) ([]OCRComparisonRow, error) {
	if len(pair.PageSources) != len(oracle.Pages) || len(pair.SampleCatalogs) != len(pair.Samples) {
		return nil, ErrInvalid
	}
	for i, ref := range pair.PageSources {
		if ref.SHA256 != oracle.Pages[i].SourceSHA256 {
			return nil, ErrInvalid
		}
		if _, err := readOCREvidence(root, ref, maxArtifactBytes); err != nil {
			return nil, err
		}
	}
	configBody, err := readOCREvidence(root, pair.CandidateConfig, 1<<20)
	var config CandidateConfig
	if err != nil || decodeOCR(configBody, &config) != nil || config.Validate() != nil || config.CandidateID != pair.EngineID {
		return nil, ErrInvalid
	}
	if len(pair.Identities) < 6 || len(pair.Identities) > 128 {
		return nil, ErrInvalid
	}
	kinds := map[string]bool{}
	pins := map[string]bool{}
	license := "verified"
	for _, identity := range pair.Identities {
		if !boundedToken(identity.Kind, 32) || !boundedText(identity.Name, 96) || !boundedText(identity.Version, 128) || !boundedText(identity.License, 256) {
			return nil, ErrInvalid
		}
		if _, err := readOCREvidence(root, identity.File, maxArtifactBytes); err != nil {
			return nil, err
		}
		kinds[identity.Kind] = true
		pins[identity.File.SHA256] = true
		if identity.Kind == "font" && identity.File.SHA256 != fontSHA {
			return nil, ErrInvalid
		}
		if identity.License == "unresolved" {
			license = "unresolved"
		}
	}
	for _, kind := range []string{"executable", "source", "model", "dictionary", "font", "tool"} {
		if !kinds[kind] {
			return nil, ErrInvalid
		}
	}
	if !pins[config.ExecutableSHA256] {
		return nil, ErrInvalid
	}
	for _, pin := range append(append([]CandidatePin(nil), config.Sources...), config.Dependencies...) {
		if !validHash(pin.SHA256) || !pins[pin.SHA256] {
			return nil, ErrInvalid
		}
	}
	expected := CandidateReceipt{ID: config.CandidateID, Version: config.CandidateVersion, ExecutableSHA256: config.ExecutableSHA256, Sources: config.Sources, Dependencies: config.Dependencies, OperatorSteps: config.OperatorSteps, Parameters: config.Parameters}
	row, err := analyzeOCRRun(root, pair.Standalone, pair, oracle, expected)
	if err != nil {
		return nil, err
	}
	row.Mode = "standalone"
	row.LicenseStatus = license
	row.ResourceStatus = "not-measured"
	rows := []OCRComparisonRow{row}
	benchmarkBody, err := readOCREvidence(root, pair.Benchmark, 8<<20)
	var benchmark BenchmarkSummary
	if err != nil || decodeOCR(benchmarkBody, &benchmark) != nil || benchmark.Format != BenchmarkFormat || benchmark.Version != 1 || benchmark.FixtureID != oracle.FixtureID || benchmark.FixtureSHA256 != oracle.FixtureSHA256 || benchmark.CandidateConfigSHA256 != pair.CandidateConfig.SHA256 || !sameOCRCandidate(benchmark.Candidate, expected) || len(benchmark.Samples) != len(pair.Samples) || len(pair.Samples) < 3 || len(pair.Samples) > 100 || benchmark.Settings.ColdRepetitions < 1 || benchmark.Settings.WarmRepetitions < 2 {
		return nil, ErrInvalid
	}
	if !pins[benchmark.Observer.SHA256] || benchmark.Observer.License == "" || benchmark.Settings.MaxRSSBytes < 1 || benchmark.Settings.MaxTempDiskBytes < 1 || benchmark.Settings.TimeoutSeconds < 1 || benchmark.Settings.TimeoutSeconds > 900 {
		return nil, ErrInvalid
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	benchmarkDir := filepath.Dir(pair.Benchmark.Path)
	for i, ref := range pair.Samples {
		body, err := readOCREvidence(root, ref, 8<<20)
		var sample BenchmarkSample
		if err != nil || decodeOCR(body, &sample) != nil || !reflect.DeepEqual(sample, benchmark.Samples[i]) || seen[sample.SampleID] || sample.Sequence != i+1 || !safeRelative(sample.ArtifactCatalog) || !safeRelative(sample.ReceiptPath) || filepath.Clean(filepath.Join(benchmarkDir, sample.ReceiptPath)) != ref.Path || sample.FixtureID != oracle.FixtureID || sample.FixtureSHA256 != oracle.FixtureSHA256 || sample.CandidateConfigSHA256 != pair.CandidateConfig.SHA256 || !sameOCRCandidate(sample.Candidate, expected) || !ocrMeasuredSample(sample, benchmark.Settings) {
			return nil, ErrInvalid
		}
		seen[sample.SampleID] = true
		counts[sample.Mode]++
		catalogPath := filepath.Join(benchmarkDir, sample.ArtifactCatalog)
		catalogRef := pair.SampleCatalogs[i]
		if catalogRef.Path != catalogPath {
			return nil, ErrInvalid
		}
		row, err := analyzeOCRRun(root, catalogRef, pair, oracle, expected)
		if err != nil {
			return nil, err
		}
		row.Mode = sample.Mode
		row.WallMillis = sample.WallMillis
		row.PeakRSSBytes = sample.PeakRSSBytes
		row.TempDiskBytes = sample.SampledTempDiskBytes
		row.LicenseStatus = license
		row.ResourceStatus = "measured"
		rows = append(rows, row)
	}
	if counts["cold"] != benchmark.Settings.ColdRepetitions || counts["warm"] != benchmark.Settings.WarmRepetitions || len(counts) != 2 {
		return nil, ErrInvalid
	}
	metrics := summarizeBenchmark(benchmark.Samples)
	for i := range rows {
		rows[i].Resources = &metrics
		for _, pin := range pair.Identities {
			rows[i].Provenance = append(rows[i].Provenance, OCRPinEvidence{Kind: pin.Kind, Name: pin.Name, Version: pin.Version, License: pin.License, SHA256: pin.File.SHA256})
		}
	}
	return rows, nil
}
func sameOCRCandidate(a, b CandidateReceipt) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}
func ocrMeasuredSample(s BenchmarkSample, limits BenchmarkSettings) bool {
	if (s.Mode != "cold" && s.Mode != "warm") || s.WallMillis < 0 || s.PeakRSSBytes <= 0 || !s.TempDiskSampled || s.SampledTempDiskBytes < 0 || math.IsNaN(s.UserCPUSeconds) || math.IsInf(s.UserCPUSeconds, 0) || s.UserCPUSeconds < 0 || math.IsNaN(s.SystemCPUSeconds) || math.IsInf(s.SystemCPUSeconds, 0) || s.SystemCPUSeconds < 0 {
		return false
	}
	if s.Status == "complete" && (s.TimedOut || s.ExitCode != 0 || s.ResourceFailure != "" || s.PeakRSSBytes > limits.MaxRSSBytes || s.SampledTempDiskBytes > limits.MaxTempDiskBytes || s.WallMillis > int64(limits.TimeoutSeconds)*1000) {
		return false
	}
	if s.ValidationStatus != "passed" && s.ValidationStatus != "failed" {
		return false
	}
	for _, name := range []string{"fixture", "candidate_config", "executable"} {
		if !validHash(s.SourceHashesBefore[name]) || s.SourceHashesBefore[name] != s.SourceHashesAfter[name] {
			return false
		}
	}
	if s.SourceHashesBefore["fixture"] != s.FixtureSHA256 || s.SourceHashesBefore["candidate_config"] != s.CandidateConfigSHA256 || s.SourceHashesBefore["executable"] != s.Candidate.ExecutableSHA256 {
		return false
	}
	return s.Status == "complete" || s.Status == "incomplete" && s.FailureClass != ""
}

func analyzeOCRRun(root *os.Root, ref OCRFileRef, pair OCRManifestPair, oracle OCROracleEntry, candidate CandidateReceipt) (OCRComparisonRow, error) {
	row := OCRComparisonRow{EngineID: pair.EngineID, FixtureID: oracle.FixtureID, FixtureSHA256: oracle.FixtureSHA256, CandidateVersion: candidate.Version, CandidateConfigSHA256: pair.CandidateConfig.SHA256, ExecutableSHA256: candidate.ExecutableSHA256}
	body, err := readOCREvidence(root, ref, 8<<20)
	var catalog ArtifactCatalog
	if err != nil || decodeOCR(body, &catalog) != nil || catalog.Format != ArtifactCatalogFormat || catalog.Version != 1 || catalog.Original == nil || catalog.FixtureID != oracle.FixtureID || catalog.FixtureSHA256 != oracle.FixtureSHA256 || catalog.CandidateConfigSHA256 != pair.CandidateConfig.SHA256 || !sameOCRCandidate(catalog.Candidate, candidate) || catalog.Original.SHA256 != oracle.FixtureSHA256 || catalog.Status != "complete" && catalog.Status != "incomplete" || len(catalog.Artifacts) > 32 || catalog.Validation == nil {
		return row, ErrInvalid
	}
	row.RunID = catalog.RunID
	row.CatalogSHA256 = ref.SHA256
	if catalog.Validation.Format != ValidationFormat || catalog.Validation.Version != 1 || len(catalog.Validation.Checks) == 0 || catalog.Validation.Status != "passed" && catalog.Validation.Status != "failed" {
		return row, ErrInvalid
	}
	row.PDFStatus = catalog.Validation.Status
	for _, check := range catalog.Validation.Checks {
		if !boundedToken(check.Name, 96) || (check.Status != "pass" && check.Status != "fail") {
			return row, ErrInvalid
		}
	}
	base := filepath.Dir(ref.Path)
	artifacts := append([]Artifact{*catalog.Original}, catalog.Artifacts...)
	seen := map[string]bool{}
	texts := map[string]string{}
	positions := map[string]ocrplugin.PositionedPage{}
	pdfFound := false
	for _, artifact := range artifacts {
		if !ValidID(artifact.ID) || seen[artifact.ID] || !validHash(artifact.SHA256) || artifact.Bytes < 0 || artifact.Bytes > maxArtifactBytes || !ValidRole(artifact.Role) || artifact.Role != RoleOriginal && !validRoleMediaType(artifact.Role, artifact.MediaType) {
			return row, ErrInvalid
		}
		seen[artifact.ID] = true
		if len(artifact.SourcePages) < 1 || len(artifact.SourcePages) > len(oracle.Pages) {
			return row, ErrInvalid
		}
		for _, page := range artifact.SourcePages {
			if !containsString(pageIDsOCR(oracle.Pages), page) {
				return row, ErrInvalid
			}
		}
		row.Artifacts = append(row.Artifacts, OCRArtifactIdentity{ID: artifact.ID, Role: artifact.Role, SHA256: artifact.SHA256, SourcePages: artifact.SourcePages})
		limit := int64(maxArtifactBytes)
		if artifact.Role == RoleOCRText || artifact.Role == RolePositionedText {
			limit = 1 << 20
		}
		body, err := readOCREvidence(root, OCRFileRef{Path: filepath.Join(base, "artifacts", artifact.ID), SHA256: artifact.SHA256}, limit)
		if err != nil || int64(len(body)) != artifact.Bytes {
			return row, ErrInvalid
		}
		if artifact.Role == RoleOriginal {
			if !reflect.DeepEqual(artifact.SourcePages, pageIDsOCR(oracle.Pages)) {
				return row, ErrInvalid
			}
			continue
		}
		if !containsString(artifact.Parents, catalog.Original.ID) {
			return row, ErrInvalid
		}
		switch artifact.Role {
		case RoleOCRText:
			if len(artifact.SourcePages) != 1 || artifact.MediaType != "text/plain; charset=utf-8" || !utf8.Valid(body) {
				return row, ErrInvalid
			}
			page := artifact.SourcePages[0]
			if _, ok := texts[page]; ok {
				return row, ErrInvalid
			}
			texts[page] = string(body)
		case RolePositionedText:
			if len(artifact.SourcePages) != 1 || artifact.MediaType != "application/json" {
				return row, ErrInvalid
			}
			var positioned ocrplugin.PositionedPage
			if decodeOCR(body, &positioned) != nil || positioned.PageID != artifact.SourcePages[0] {
				return row, ErrInvalid
			}
			if _, ok := positions[positioned.PageID]; ok {
				return row, ErrInvalid
			}
			positions[positioned.PageID] = positioned
		case RolePDF:
			if pdfFound || artifact.MediaType != "application/pdf" || !bytes.HasPrefix(body, []byte("%PDF-")) || !reflect.DeepEqual(artifact.SourcePages, pageIDsOCR(oracle.Pages)) || artifact.SHA256 != catalog.Validation.PDFSHA256 {
				return row, ErrInvalid
			}
			pdfFound = true
		}
	}
	if !pdfFound || len(texts) != len(oracle.Pages) || len(positions) != len(oracle.Pages) || len(catalog.Validation.Pages) != len(oracle.Pages) {
		return row, ErrInvalid
	}
	for i, page := range oracle.Pages {
		text, ok := texts[page.PageID]
		pos, found := positions[page.PageID]
		if !ok || !found || catalog.Validation.Pages[i].PageID != page.PageID || catalog.Validation.Pages[i].Index != i+1 {
			return row, ErrInvalid
		}
		q, err := AnalyzeOCRPage(page, text, pos)
		if err != nil {
			return row, err
		}
		row.Pages = append(row.Pages, q)
	}
	return row, nil
}
func pageIDsOCR(pages []OCRPageOracle) []string {
	ids := make([]string, len(pages))
	for i, p := range pages {
		ids[i] = p.PageID
	}
	return ids
}
