package evaluation

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"scanner-kiosk/internal/processrun"
)

const (
	CandidateConfigFormat  = "scanner-eval-candidate"
	CandidateConfigVersion = 1
	candidateTimeout       = 2 * time.Minute
	candidateStopGrace     = 2 * time.Second
)

type CandidatePin struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	License string `json:"license,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
}

type CandidateConfig struct {
	Format           string            `json:"format"`
	Version          int               `json:"version"`
	CandidateID      string            `json:"candidate_id"`
	Executable       string            `json:"executable"`
	ExecutableSHA256 string            `json:"executable_sha256"`
	Argv             []string          `json:"argv"`
	Environment      []string          `json:"environment,omitempty"`
	CandidateVersion string            `json:"candidate_version"`
	Sources          []CandidatePin    `json:"sources"`
	Dependencies     []CandidatePin    `json:"dependencies,omitempty"`
	OperatorSteps    []string          `json:"operator_steps"`
	Parameters       map[string]string `json:"parameters,omitempty"`
}

type CandidateReceipt struct {
	ID               string            `json:"id"`
	Version          string            `json:"version"`
	ExecutableSHA256 string            `json:"executable_sha256"`
	Sources          []CandidatePin    `json:"sources"`
	Dependencies     []CandidatePin    `json:"dependencies,omitempty"`
	OperatorSteps    []string          `json:"operator_steps"`
	Parameters       map[string]string `json:"parameters"`
}

type CandidateRunRequest struct {
	CatalogPath        string
	PrivateCatalogPath string
	PrivateRoot        string
	CandidatePath      string
	FixtureID          string
	OutputDir          string
	TimePath           string
	TimeUsagePath      string
	Timeout            time.Duration
	MaxRSSBytes        int64
	MaxTempDiskBytes   int64
	Metrics            *CandidateMetrics
}

type CandidateMetrics struct {
	WallMillis           int64
	UserCPUSeconds       float64
	SystemCPUSeconds     float64
	PeakRSSBytes         int64
	ExitCode             int
	SampledTempDiskBytes int64
	TempDiskSampled      bool
	TimedOut             bool
	ResourceFailure      string
}

func LoadCandidateConfig(path string) (CandidateConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return CandidateConfig{}, ErrInvalid
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var config CandidateConfig
	if err := decoder.Decode(&config); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return CandidateConfig{}, ErrInvalid
	}
	if err := config.Validate(); err != nil {
		return CandidateConfig{}, err
	}
	return config, nil
}

func (c CandidateConfig) Validate() error {
	if c.Format != CandidateConfigFormat || c.Version != CandidateConfigVersion ||
		!boundedText(c.CandidateID, 96) || c.CandidateVersion == "" ||
		!boundedText(c.CandidateVersion, 64) || !filepath.IsAbs(c.Executable) ||
		filepath.Clean(c.Executable) != c.Executable || !validHash(c.ExecutableSHA256) ||
		len(c.Argv) > 64 || len(c.Environment) > 64 || len(c.Sources) == 0 ||
		len(c.Sources) > 32 || len(c.Dependencies) > 32 || len(c.OperatorSteps) > 64 {
		return ErrInvalid
	}
	seenEnv := map[string]bool{}
	for _, value := range c.Environment {
		key, _, ok := strings.Cut(value, "=")
		if !ok || !validEnvironmentName(key) || strings.HasPrefix(key, "SCANNER_EVAL_") ||
			seenEnv[key] || !boundedOptionalText(value, 4096) {
			return ErrInvalid
		}
		seenEnv[key] = true
	}
	if !validProvenance(c.Sources, c.Dependencies, c.OperatorSteps) ||
		!validParameters(c.Parameters) {
		return ErrInvalid
	}
	for _, arg := range c.Argv {
		if !boundedOptionalText(arg, 4096) {
			return ErrInvalid
		}
	}
	return nil
}

func RunCandidate(ctx context.Context, request CandidateRunRequest) (ArtifactCatalog, error) {
	if ctx.Err() != nil {
		return ArtifactCatalog{}, ctx.Err()
	}
	config, err := LoadCandidateConfig(request.CandidatePath)
	if err != nil {
		return ArtifactCatalog{}, fmt.Errorf("candidate config: %w", err)
	}
	configSHA, err := hashRegularFile(request.CandidatePath, 1<<20)
	if err != nil {
		return ArtifactCatalog{}, fmt.Errorf("candidate config: %w", err)
	}
	if request.FixtureID == "" || !filepath.IsAbs(request.OutputDir) ||
		filepath.Clean(request.OutputDir) != request.OutputDir ||
		request.Timeout < 0 || request.Timeout > 900*time.Second ||
		request.MaxRSSBytes < 0 || request.MaxTempDiskBytes < 0 ||
		(request.TimePath == "") != (request.TimeUsagePath == "") {
		return ArtifactCatalog{}, fmt.Errorf("candidate request: %w", ErrInvalid)
	}
	if err := verifyExecutable(config.Executable, config.ExecutableSHA256); err != nil {
		return ArtifactCatalog{}, fmt.Errorf("candidate executable: %w", err)
	}
	fixture, fixtureBody, err := ResolveFixture(
		request.CatalogPath, request.PrivateCatalogPath, request.PrivateRoot, request.FixtureID,
	)
	if err != nil {
		var failure *FixtureFailure
		if errors.As(err, &failure) {
			runID, idErr := newRunID("run_")
			if idErr != nil {
				return ArtifactCatalog{}, ErrUnavailable
			}
			status := "invalid"
			if failure.Match {
				status = "expected-failure"
			}
			match := failure.Match
			receipt := ArtifactCatalog{
				Format: ArtifactCatalogFormat, Version: ArtifactCatalogVersion, RunID: runID,
				Candidate: candidateReceipt(config), CandidateConfigSHA256: configSHA,
				FixtureID:     failure.FixtureID,
				FixtureSHA256: failure.FixtureSHA256, Status: status, Artifacts: []Artifact{},
				ExpectedFailureClass: failure.ExpectedClass, ObservedFailureClass: failure.ObservedClass,
				FailureMatch: &match,
			}
			if writeErr := writePreflightArtifactCatalog(request.OutputDir, receipt); writeErr != nil {
				return ArtifactCatalog{}, writeErr
			}
			if failure.Match {
				return receipt, nil
			}
			return receipt, fmt.Errorf("fixture failure: %w", ErrInvalid)
		}
		return ArtifactCatalog{}, fmt.Errorf("fixture resolution: %w", err)
	}
	fixtureSum := sha256.Sum256(fixtureBody)
	fixtureSHA := hex.EncodeToString(fixtureSum[:])
	if fixture.SHA256 != "" && fixtureSHA != fixture.SHA256 {
		return ArtifactCatalog{}, fmt.Errorf("fixture hash: %w", ErrInvalid)
	}
	runRoot, err := os.MkdirTemp("", "scanner-eval-run-")
	if err != nil {
		return ArtifactCatalog{}, ErrUnavailable
	}
	defer os.RemoveAll(runRoot)
	if err := os.Chmod(runRoot, 0o700); err != nil {
		return ArtifactCatalog{}, ErrUnavailable
	}
	root, err := os.OpenRoot(runRoot)
	if err != nil {
		return ArtifactCatalog{}, ErrUnavailable
	}
	defer root.Close()
	for _, name := range []string{"input", "candidate-output", "tmp"} {
		if err := root.Mkdir(name, 0o700); err != nil {
			return ArtifactCatalog{}, ErrUnavailable
		}
	}
	if err := writePrivateFile(root, "input/original.png", fixtureBody); err != nil {
		return ArtifactCatalog{}, fmt.Errorf("candidate staging: %w", err)
	}
	timeout := candidateTimeout
	if request.Timeout > 0 {
		timeout = request.Timeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	commandPath := config.Executable
	commandArgs := append([]string(nil), config.Argv...)
	if request.TimePath != "" {
		if err := verifyExecutableFile(request.TimePath); err != nil {
			return ArtifactCatalog{}, ErrUnavailable
		}
		if !filepath.IsAbs(request.TimeUsagePath) || filepath.Clean(request.TimeUsagePath) != request.TimeUsagePath {
			return ArtifactCatalog{}, ErrInvalid
		}
		usage, err := os.OpenFile(request.TimeUsagePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return ArtifactCatalog{}, ErrInvalid
		}
		if err := usage.Close(); err != nil {
			return ArtifactCatalog{}, ErrUnavailable
		}
		commandPath = request.TimePath
		commandArgs = append([]string{"-f", "%e %U %S %M", "-o", request.TimeUsagePath, "--", config.Executable}, config.Argv...)
	}
	originalID, err := newRunID("artifact_original_")
	if err != nil {
		return ArtifactCatalog{}, ErrUnavailable
	}
	runID, err := newRunID("run_")
	if err != nil {
		return ArtifactCatalog{}, ErrUnavailable
	}
	inputPath := filepath.Join(runRoot, "input", "original.png")
	outputPath := filepath.Join(runRoot, "candidate-output")
	tmpPath := filepath.Join(runRoot, "tmp")
	environment := append([]string{"PATH=/usr/bin:/bin", "TMPDIR=" + tmpPath}, config.Environment...)
	environment = append(environment,
		"SCANNER_EVAL_INPUT="+inputPath,
		"SCANNER_EVAL_OUTPUT="+outputPath,
		"SCANNER_EVAL_ORIGINAL_ID="+originalID,
		"SCANNER_EVAL_SOURCE_PAGES="+strings.Join(fixture.PageIDs, ","),
	)
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		return ArtifactCatalog{}, ErrUnavailable
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		stdoutRead.Close()
		stdoutWrite.Close()
		return ArtifactCatalog{}, ErrUnavailable
	}
	stdoutCapture := newCapture(stdoutRead)
	stderrCapture := newCapture(stderrRead)
	started := time.Now()
	stopSampler := startDiskSampler(runRoot, request.MaxTempDiskBytes, cancel)
	process := processrun.Run(runCtx, processrun.Spec{
		Path: commandPath, Args: commandArgs,
		Env: environment, Dir: runRoot, Stdout: stdoutWrite, Stderr: stderrWrite,
		Grace: candidateStopGrace,
	})
	wallMillis := time.Since(started).Milliseconds()
	stdoutWrite.Close()
	stderrWrite.Close()
	stdoutCapture.wait()
	stderrCapture.wait()
	sampledDisk := stopSampler()
	metrics := CandidateMetrics{
		WallMillis: wallMillis, ExitCode: processExitCode(process.Err),
		SampledTempDiskBytes: sampledDisk, TempDiskSampled: true,
		TimedOut: runCtx.Err() == context.DeadlineExceeded,
	}
	if request.TimePath != "" {
		usage, err := parseTimeUsage(request.TimeUsagePath)
		if err != nil {
			if request.Metrics != nil {
				*request.Metrics = metrics
			}
			return ArtifactCatalog{}, ErrUnavailable
		}
		metrics.UserCPUSeconds = usage.UserCPUSeconds
		metrics.SystemCPUSeconds = usage.SystemCPUSeconds
		metrics.PeakRSSBytes = usage.PeakRSSBytes
	}
	if request.MaxTempDiskBytes > 0 && sampledDisk > request.MaxTempDiskBytes {
		metrics.ResourceFailure = "temp-disk"
	}
	if request.MaxRSSBytes > 0 && metrics.PeakRSSBytes > request.MaxRSSBytes {
		if metrics.ResourceFailure == "" {
			metrics.ResourceFailure = "rss"
		} else {
			metrics.ResourceFailure += ",rss"
		}
	}
	if request.Metrics != nil {
		*request.Metrics = metrics
	}
	if process.Err != nil || !process.GroupStopped {
		return ArtifactCatalog{}, fmt.Errorf("candidate process: %w", ErrUnavailable)
	}
	if metrics.ResourceFailure != "" {
		return ArtifactCatalog{}, ErrUnavailable
	}
	if err := verifyFile(filepath.Join(runRoot, "input", "original.png"), fixtureSHA); err != nil {
		return ArtifactCatalog{}, fmt.Errorf("candidate original verification: %w", err)
	}
	result, err := loadCandidateResult(root)
	if err != nil {
		return ArtifactCatalog{}, fmt.Errorf("candidate result: %w", err)
	}
	if err := validateCandidateResult(result, config.CandidateID, fixture, originalID); err != nil {
		return ArtifactCatalog{}, fmt.Errorf("candidate result validation: %w", err)
	}
	parent := filepath.Dir(request.OutputDir)
	if info, statErr := os.Lstat(parent); statErr != nil || !info.IsDir() {
		return ArtifactCatalog{}, ErrInvalid
	}
	staging, err := os.MkdirTemp(parent, ".scanner-eval-output-")
	if err != nil {
		return ArtifactCatalog{}, ErrUnavailable
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(staging)
		}
	}()
	if err := os.Chmod(staging, 0o700); err != nil {
		return ArtifactCatalog{}, ErrUnavailable
	}
	destination, err := os.OpenRoot(staging)
	if err != nil {
		return ArtifactCatalog{}, ErrUnavailable
	}
	defer destination.Close()
	original := Artifact{
		ID: originalID, Role: RoleOriginal, MediaType: fixture.MediaType,
		SHA256: fixtureSHA, Bytes: int64(len(fixtureBody)), SourcePages: append([]string(nil), fixture.PageIDs...),
		Algorithm: "harness-copy", AlgorithmVersion: ToolVersion,
	}
	if err := destination.Mkdir("artifacts", 0o700); err != nil {
		return ArtifactCatalog{}, fmt.Errorf("artifact directory: %w", ErrInvalid)
	}
	if err := writePrivateFile(destination, filepath.Join("artifacts", originalID), fixtureBody); err != nil {
		return ArtifactCatalog{}, fmt.Errorf("original normalization: %w", err)
	}
	artifacts, err := normalizeCandidateArtifacts(root, destination, result)
	if err != nil {
		return ArtifactCatalog{}, fmt.Errorf("artifact normalization: %w", err)
	}
	missing := missingOutputs(fixture, artifacts)
	status := "complete"
	if len(missing) != 0 || len(fixture.ExpectedOutputs) == 0 {
		status = "incomplete"
	}
	var validation *ValidationReceipt
	if status == "complete" {
		if err := ValidateTextArtifacts(destination, artifacts, fixture); err != nil {
			status = "incomplete"
			validation = &ValidationReceipt{
				Format: ValidationFormat, Version: ValidationVersion, Status: "failed", FailureClass: "text-binding",
				Checks: []ValidationCheck{{Name: "source-page-text-binding", Status: "fail"}},
			}
		}
	}
	for _, artifact := range artifacts {
		if artifact.Role != RolePDF {
			continue
		}
		pdfPath := filepath.Join(staging, "artifacts", artifact.ID)
		receipt, validationErr := ValidatePDF(ctx, pdfPath, fixture.PageIDs, fixture.ExpectedText, PDFValidationOptions{
			OriginalImagePath: filepath.Join(runRoot, "input", "original.png"),
			ExpectLossless:    true,
		})
		if validation != nil {
			receipt.Status = "failed"
			if receipt.FailureClass == "" {
				receipt.FailureClass = validation.FailureClass
			}
			receipt.Checks = append(receipt.Checks, validation.Checks...)
		}
		validation = &receipt
		if validationErr != nil {
			status = "incomplete"
		}
		break
	}
	receipt := ArtifactCatalog{
		Format: ArtifactCatalogFormat, Version: ArtifactCatalogVersion, RunID: runID,
		Candidate: candidateReceipt(config), CandidateConfigSHA256: configSHA,
		FixtureID: fixture.ID, FixtureSHA256: fixtureSHA, Status: status, MissingOutputs: missing,
		Original: &original, Artifacts: artifacts, Validation: validation,
	}
	if err := writeJSONExclusiveInRoot(destination, "artifact-catalog.json", receipt); err != nil {
		return ArtifactCatalog{}, err
	}
	if err := syncRoot(destination); err != nil {
		return ArtifactCatalog{}, err
	}
	if _, err := os.Lstat(request.OutputDir); !os.IsNotExist(err) {
		return ArtifactCatalog{}, ErrInvalid
	}
	if err := os.Rename(staging, request.OutputDir); err != nil {
		return ArtifactCatalog{}, ErrUnavailable
	}
	committed = true
	if status != "complete" {
		return receipt, ErrUnavailable
	}
	return receipt, nil
}

func candidateReceipt(config CandidateConfig) CandidateReceipt {
	return CandidateReceipt{
		ID: config.CandidateID, Version: config.CandidateVersion, ExecutableSHA256: config.ExecutableSHA256,
		Sources:       append([]CandidatePin(nil), config.Sources...),
		Dependencies:  append([]CandidatePin(nil), config.Dependencies...),
		OperatorSteps: append([]string{}, config.OperatorSteps...),
		Parameters:    copyStringMap(config.Parameters),
	}
}

func writePreflightArtifactCatalog(outputDir string, receipt ArtifactCatalog) error {
	parent := filepath.Dir(outputDir)
	if info, err := os.Lstat(parent); err != nil || !info.IsDir() {
		return ErrInvalid
	}
	if _, err := os.Lstat(outputDir); !os.IsNotExist(err) {
		return ErrInvalid
	}
	staging, err := os.MkdirTemp(parent, ".scanner-eval-output-")
	if err != nil {
		return ErrUnavailable
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(staging)
		}
	}()
	if err := os.Chmod(staging, 0o700); err != nil {
		return ErrUnavailable
	}
	root, err := os.OpenRoot(staging)
	if err != nil {
		return ErrUnavailable
	}
	defer root.Close()
	if err := writeJSONExclusiveInRoot(root, "artifact-catalog.json", receipt); err != nil {
		return err
	}
	if err := syncRoot(root); err != nil {
		return err
	}
	if err := os.Rename(staging, outputDir); err != nil {
		return ErrUnavailable
	}
	committed = true
	return nil
}

func verifyFile(path, want string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ErrUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return ErrUnavailable
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return ErrUnavailable
	}
	if hex.EncodeToString(digest.Sum(nil)) != want {
		return ErrInvalid
	}
	return nil
}

func verifyExecutable(path, want string) error {
	if err := verifyExecutableFile(path); err != nil {
		return err
	}
	return verifyFile(path, want)
}

func verifyExecutableFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return ErrUnavailable
	}
	return nil
}

func writePrivateFile(root *os.Root, name string, body []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrInvalid
	}
	if _, err := io.Copy(file, bytes.NewReader(body)); err != nil {
		file.Close()
		return ErrInvalid
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return ErrInvalid
	}
	if err := file.Close(); err != nil {
		return ErrInvalid
	}
	if err := root.Chmod(name, 0o400); err != nil {
		return ErrInvalid
	}
	return nil
}

func writeJSONExclusiveInRoot(root *os.Root, name string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrInvalid
	}
	if _, err := file.Write(append(body, '\n')); err != nil {
		file.Close()
		return ErrInvalid
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return ErrInvalid
	}
	return file.Close()
}

func syncRoot(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return ErrUnavailable
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return ErrUnavailable
	}
	return nil
}

func newRunID(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value[:]), nil
}

func validEnvironmentName(value string) bool {
	if value == "" || len(value) > 64 || value[0] >= '0' && value[0] <= '9' {
		return false
	}
	for _, r := range value {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

func validPins(values []CandidatePin, requireSHA256 bool) bool {
	for _, pin := range values {
		if !boundedText(pin.Name, 96) || !boundedText(pin.Version, 64) ||
			!boundedText(pin.License, 96) ||
			requireSHA256 && !validHash(pin.SHA256) ||
			pin.SHA256 != "" && !validHash(pin.SHA256) {
			return false
		}
	}
	return true
}

func validProvenance(sources, dependencies []CandidatePin, operatorSteps []string) bool {
	return validPins(sources, false) && validPins(dependencies, true) &&
		operatorSteps != nil && validOptionalStrings(operatorSteps, 64, 256)
}

func validOptionalStrings(values []string, maxItems, maxLen int) bool {
	if len(values) > maxItems {
		return false
	}
	for _, value := range values {
		if !boundedText(value, maxLen) {
			return false
		}
	}
	return true
}

func copyStringMap(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

type timeUsage struct {
	UserCPUSeconds   float64
	SystemCPUSeconds float64
	PeakRSSBytes     int64
}

func parseTimeUsage(path string) (timeUsage, error) {
	body, err := readRootFileValue(path, 4096)
	if err != nil {
		return timeUsage{}, ErrUnavailable
	}
	fields := strings.Fields(string(body))
	if len(fields) != 4 {
		return timeUsage{}, ErrUnavailable
	}
	user, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return timeUsage{}, ErrUnavailable
	}
	system, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return timeUsage{}, ErrUnavailable
	}
	kilobytes, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || kilobytes < 0 {
		return timeUsage{}, ErrUnavailable
	}
	return timeUsage{UserCPUSeconds: user, SystemCPUSeconds: system, PeakRSSBytes: kilobytes * 1024}, nil
}

func readRootFileValue(name string, limit int64) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, ErrUnavailable
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, ErrUnavailable
	}
	return body, nil
}

func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

func startDiskSampler(rootPath string, limit int64, cancel context.CancelFunc) func() int64 {
	var peak atomic.Int64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			size := directoryBytes(rootPath)
			if size > peak.Load() {
				peak.Store(size)
			}
			if limit > 0 && size > limit {
				cancel()
			}
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()
	return func() int64 {
		close(stop)
		<-done
		return peak.Load()
	}
}

func directoryBytes(rootPath string) int64 {
	var total int64
	_ = filepath.WalkDir(rootPath, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

type capture struct {
	reader *os.File
	done   chan struct{}
}

func newCapture(reader *os.File) *capture {
	c := &capture{reader: reader, done: make(chan struct{})}
	go func() {
		defer close(c.done)
		defer reader.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(reader, 64<<10))
		_, _ = io.Copy(io.Discard, reader)
	}()
	return c
}

func (c *capture) wait() {
	<-c.done
}
