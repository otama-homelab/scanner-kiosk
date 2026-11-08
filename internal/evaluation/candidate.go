package evaluation

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	Format           string         `json:"format"`
	Version          int            `json:"version"`
	CandidateID      string         `json:"candidate_id"`
	Executable       string         `json:"executable"`
	ExecutableSHA256 string         `json:"executable_sha256"`
	Argv             []string       `json:"argv"`
	Environment      []string       `json:"environment,omitempty"`
	CandidateVersion string         `json:"candidate_version"`
	Sources          []CandidatePin `json:"sources"`
	Dependencies     []CandidatePin `json:"dependencies,omitempty"`
	OperatorSteps    []string       `json:"operator_steps,omitempty"`
}

type CandidateReceipt struct {
	ID               string         `json:"id"`
	Version          string         `json:"version"`
	ExecutableSHA256 string         `json:"executable_sha256"`
	Sources          []CandidatePin `json:"sources"`
	Dependencies     []CandidatePin `json:"dependencies,omitempty"`
	OperatorSteps    []string       `json:"operator_steps,omitempty"`
}

type CandidateRunRequest struct {
	CatalogPath   string
	CandidatePath string
	FixtureID     string
	OutputDir     string
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
	if !validPins(c.Sources) || !validPins(c.Dependencies) ||
		!validOptionalStrings(c.OperatorSteps, 64, 256) {
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
	if request.FixtureID == "" || !filepath.IsAbs(request.OutputDir) ||
		filepath.Clean(request.OutputDir) != request.OutputDir {
		return ArtifactCatalog{}, fmt.Errorf("candidate request: %w", ErrInvalid)
	}
	if err := verifyExecutable(config.Executable, config.ExecutableSHA256); err != nil {
		return ArtifactCatalog{}, fmt.Errorf("candidate executable: %w", err)
	}
	catalog, err := LoadCatalog(request.CatalogPath)
	if err != nil {
		return ArtifactCatalog{}, fmt.Errorf("fixture catalog: %w", err)
	}
	var fixture FixtureDefinition
	for _, candidate := range catalog.Fixtures {
		if candidate.ID == request.FixtureID {
			fixture = candidate
			break
		}
	}
	if fixture.ID == "" {
		return ArtifactCatalog{}, fmt.Errorf("fixture id: %w", ErrInvalid)
	}
	fixtureBody, err := generateFixture(filepath.Dir(filepath.Dir(request.CatalogPath)), fixture)
	if err != nil {
		return ArtifactCatalog{}, fmt.Errorf("fixture generation: %w", err)
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
		return ArtifactCatalog{}, err
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
	runCtx, cancel := context.WithTimeout(ctx, candidateTimeout)
	defer cancel()
	process := processrun.Run(runCtx, processrun.Spec{
		Path: config.Executable, Args: append([]string(nil), config.Argv...),
		Env: environment, Dir: runRoot, Stdout: stdoutWrite, Stderr: stderrWrite,
		Grace: candidateStopGrace,
	})
	stdoutWrite.Close()
	stderrWrite.Close()
	stdoutCapture.wait()
	stderrCapture.wait()
	if process.Err != nil || !process.GroupStopped {
		return ArtifactCatalog{}, ErrUnavailable
	}
	if err := verifyFile(filepath.Join(runRoot, "input", "original.png"), fixtureSHA); err != nil {
		return ArtifactCatalog{}, ErrInvalid
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
		return ArtifactCatalog{}, ErrInvalid
	}
	if err := writePrivateFile(destination, filepath.Join("artifacts", originalID), fixtureBody); err != nil {
		return ArtifactCatalog{}, err
	}
	artifacts, err := normalizeCandidateArtifacts(root, destination, result, fixture, original)
	if err != nil {
		return ArtifactCatalog{}, fmt.Errorf("artifact normalization: %w", err)
	}
	missing := missingOutputs(fixture, artifacts)
	status := "complete"
	if len(missing) != 0 {
		status = "incomplete"
	}
	receipt := ArtifactCatalog{
		Format: ArtifactCatalogFormat, Version: ArtifactCatalogVersion, RunID: runID,
		Candidate: CandidateReceipt{
			ID: config.CandidateID, Version: config.CandidateVersion, ExecutableSHA256: config.ExecutableSHA256,
			Sources:       append([]CandidatePin(nil), config.Sources...),
			Dependencies:  append([]CandidatePin(nil), config.Dependencies...),
			OperatorSteps: append([]string(nil), config.OperatorSteps...),
		},
		FixtureID: fixture.ID, FixtureSHA256: fixtureSHA, Status: status, MissingOutputs: missing,
		Original: original, Artifacts: artifacts,
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
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return ErrUnavailable
	}
	return verifyFile(path, want)
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

func validPins(values []CandidatePin) bool {
	for _, pin := range values {
		if !boundedText(pin.Name, 96) || !boundedText(pin.Version, 64) ||
			!boundedOptionalText(pin.License, 96) ||
			pin.SHA256 != "" && !validHash(pin.SHA256) {
			return false
		}
	}
	return true
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
