package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	CandidateResultFormat  = "scanner-eval-candidate-result"
	ArtifactCatalogFormat  = "scanner-eval-artifacts"
	CandidateResultVersion = 1
	ArtifactCatalogVersion = 1
	maxCandidateArtifacts  = 32
	maxArtifactBytes       = 200 << 20
	maxTotalArtifactBytes  = 512 << 20
)

type CandidateArtifact struct {
	ID               string            `json:"id"`
	Role             ArtifactRole      `json:"role"`
	Path             string            `json:"path"`
	MediaType        string            `json:"media_type"`
	SourcePages      []string          `json:"source_pages"`
	Parents          []string          `json:"parents"`
	Algorithm        string            `json:"algorithm"`
	AlgorithmVersion string            `json:"algorithm_version"`
	Parameters       map[string]string `json:"parameters,omitempty"`
	SHA256           string            `json:"sha256,omitempty"`
	Bytes            int64             `json:"bytes,omitempty"`
}

type CandidateResult struct {
	Format      string              `json:"format"`
	Version     int                 `json:"version"`
	CandidateID string              `json:"candidate_id"`
	Artifacts   []CandidateArtifact `json:"artifacts"`
}

type Artifact struct {
	ID               string            `json:"id"`
	Role             ArtifactRole      `json:"role"`
	MediaType        string            `json:"media_type"`
	SHA256           string            `json:"sha256"`
	Bytes            int64             `json:"bytes"`
	SourcePages      []string          `json:"source_pages"`
	Parents          []string          `json:"parents,omitempty"`
	Algorithm        string            `json:"algorithm"`
	AlgorithmVersion string            `json:"algorithm_version"`
	Parameters       map[string]string `json:"parameters,omitempty"`
}

type ArtifactCatalog struct {
	Format         string           `json:"format"`
	Version        int              `json:"version"`
	RunID          string           `json:"run_id"`
	Candidate      CandidateReceipt `json:"candidate"`
	FixtureID      string           `json:"fixture_id"`
	FixtureSHA256  string           `json:"fixture_sha256"`
	Status         string           `json:"status"`
	MissingOutputs []ArtifactRole   `json:"missing_outputs,omitempty"`
	Original       Artifact         `json:"original"`
	Artifacts      []Artifact       `json:"artifacts"`
}

func loadCandidateResult(root *os.Root) (CandidateResult, error) {
	info, err := root.Lstat("candidate-output/result.json")
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return CandidateResult{}, ErrInvalid
	}
	file, err := root.Open("candidate-output/result.json")
	if err != nil {
		return CandidateResult{}, ErrInvalid
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var result CandidateResult
	if err := decoder.Decode(&result); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return CandidateResult{}, ErrInvalid
	}
	if result.Format != CandidateResultFormat || result.Version != CandidateResultVersion ||
		!boundedText(result.CandidateID, 96) || len(result.Artifacts) == 0 ||
		len(result.Artifacts) > maxCandidateArtifacts {
		return CandidateResult{}, ErrInvalid
	}
	return result, nil
}

func validateCandidateResult(result CandidateResult, candidateID string, fixture FixtureDefinition, originalID string) error {
	if result.CandidateID != candidateID || !boundedText(result.CandidateID, 96) ||
		len(result.Artifacts) == 0 || len(result.Artifacts) > maxCandidateArtifacts {
		return ErrInvalid
	}
	fixturePages := map[string]bool{}
	for _, page := range fixture.PageIDs {
		fixturePages[page] = true
	}
	ids := map[string]bool{originalID: true}
	paths := map[string]bool{}
	parents := map[string][]string{}
	for index, artifact := range result.Artifacts {
		if !ValidID(artifact.ID) || ids[artifact.ID] || paths[artifact.Path] ||
			artifact.Role == RoleOriginal || !ValidRole(artifact.Role) ||
			!validRoleMediaType(artifact.Role, artifact.MediaType) ||
			!safeRelative(artifact.Path) || !boundedText(artifact.Algorithm, 96) ||
			!boundedText(artifact.AlgorithmVersion, 64) ||
			len(artifact.SourcePages) == 0 || len(artifact.SourcePages) > 256 ||
			len(artifact.Parents) == 0 || len(artifact.Parents) > 64 ||
			!validStringSet(artifact.SourcePages, 96) || !validStringSet(artifact.Parents, 96) ||
			!validParameters(artifact.Parameters) ||
			artifact.SHA256 != "" && !validHash(artifact.SHA256) ||
			artifact.Bytes < 0 || artifact.Bytes > maxArtifactBytes {
			return fmt.Errorf("%w: candidate artifact %d", ErrInvalid, index)
		}
		for _, page := range artifact.SourcePages {
			if !fixturePages[page] {
				return fmt.Errorf("%w: candidate artifact %d page", ErrInvalid, index)
			}
		}
		ids[artifact.ID] = true
		paths[artifact.Path] = true
		parents[artifact.ID] = artifact.Parents
	}
	for id, entries := range parents {
		for _, parent := range entries {
			if !ids[parent] {
				return fmt.Errorf("%w: missing parent for %s", ErrInvalid, id)
			}
		}
	}
	if hasArtifactCycle(parents) {
		return ErrInvalid
	}
	return nil
}

func normalizeCandidateArtifacts(source *os.Root, destination *os.Root, result CandidateResult, fixture FixtureDefinition, original Artifact) ([]Artifact, error) {
	normalized := make([]Artifact, 0, len(result.Artifacts))
	var total int64
	for _, candidate := range result.Artifacts {
		info, err := source.Lstat(path.Join("candidate-output", candidate.Path))
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxArtifactBytes ||
			candidate.Bytes > 0 && info.Size() != candidate.Bytes {
			return nil, ErrInvalid
		}
		input, err := source.Open(path.Join("candidate-output", candidate.Path))
		if err != nil {
			return nil, ErrInvalid
		}
		opened, err := input.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			input.Close()
			return nil, ErrInvalid
		}
		output, err := destination.OpenFile(path.Join("artifacts", candidate.ID), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			input.Close()
			return nil, ErrInvalid
		}
		digest := sha256.New()
		size, copyErr := io.Copy(io.MultiWriter(output, digest), io.LimitReader(input, maxArtifactBytes+1))
		closeIn := input.Close()
		syncErr := output.Sync()
		closeOut := output.Close()
		if copyErr != nil || closeIn != nil || syncErr != nil || closeOut != nil ||
			size > maxArtifactBytes || total > maxTotalArtifactBytes-size ||
			size == 0 && candidate.Role != RoleOCRText {
			return nil, ErrInvalid
		}
		total += size
		sum := hex.EncodeToString(digest.Sum(nil))
		if candidate.SHA256 != "" && candidate.SHA256 != sum {
			return nil, ErrInvalid
		}
		if candidate.Bytes > 0 && candidate.Bytes != size {
			return nil, ErrInvalid
		}
		normalized = append(normalized, Artifact{
			ID: candidate.ID, Role: candidate.Role, MediaType: candidate.MediaType,
			SHA256: sum, Bytes: size, SourcePages: append([]string(nil), candidate.SourcePages...),
			Parents: append([]string(nil), candidate.Parents...), Algorithm: candidate.Algorithm,
			AlgorithmVersion: candidate.AlgorithmVersion, Parameters: copyParameters(candidate.Parameters),
		})
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].ID < normalized[j].ID })
	return normalized, nil
}

func missingOutputs(fixture FixtureDefinition, artifacts []Artifact) []ArtifactRole {
	present := map[ArtifactRole]bool{}
	for _, artifact := range artifacts {
		present[artifact.Role] = true
	}
	var missing []ArtifactRole
	for _, role := range fixture.ExpectedOutputs {
		if !present[role] {
			missing = append(missing, role)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	return missing
}

func hasArtifactCycle(graph map[string][]string) bool {
	const (
		unseen = iota
		visiting
		visited
	)
	state := map[string]int{}
	var visit func(string) bool
	visit = func(id string) bool {
		switch state[id] {
		case visiting:
			return true
		case visited:
			return false
		}
		state[id] = visiting
		for _, parent := range graph[id] {
			if visit(parent) {
				return true
			}
		}
		state[id] = visited
		return false
	}
	for id := range graph {
		if visit(id) {
			return true
		}
	}
	return false
}

func validRoleMediaType(role ArtifactRole, mediaType string) bool {
	switch role {
	case RoleCorrection:
		return mediaType == "image/png"
	case RoleOCRText:
		return mediaType == "text/plain; charset=utf-8"
	case RolePositionedText, RoleStructure:
		return mediaType == "application/json"
	case RoleGenerated:
		return mediaType == "application/json" || mediaType == "text/plain; charset=utf-8"
	case RolePDF:
		return mediaType == "application/pdf"
	default:
		return false
	}
}

func validStringSet(values []string, max int) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if !boundedText(value, max) || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func validParameters(parameters map[string]string) bool {
	if len(parameters) > 32 {
		return false
	}
	for key, value := range parameters {
		if !boundedText(key, 64) || !boundedOptionalText(value, 256) {
			return false
		}
	}
	return true
}

func copyParameters(parameters map[string]string) map[string]string {
	if len(parameters) == 0 {
		return nil
	}
	out := make(map[string]string, len(parameters))
	for key, value := range parameters {
		out[key] = value
	}
	return out
}

func boundedText(value string, max int) bool {
	return value != "" && len(value) <= max && utf8.ValidString(value) &&
		!strings.ContainsAny(value, "\x00\r\n")
}

func boundedOptionalText(value string, max int) bool {
	return value == "" || len(value) <= max && utf8.ValidString(value) &&
		!strings.ContainsAny(value, "\x00\r\n")
}
