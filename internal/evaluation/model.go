// Package evaluation provides an evaluation-only fixture and artifact harness.
// It is independent of the production API, worker and document store.
package evaluation

import "errors"

const (
	CatalogFormat  = "scanner-eval-fixtures"
	CatalogVersion = 1
	ToolVersion    = "scanner-eval/1"
)

const (
	PrivacyPublicSynthetic = "public-synthetic"
	PrivacyOwnerPrivate    = "owner-private"
)

type ArtifactRole string

const (
	RoleOriginal       ArtifactRole = "original"
	RoleCorrection     ArtifactRole = "correction"
	RoleOCRText        ArtifactRole = "ocr-text"
	RolePositionedText ArtifactRole = "positioned-text"
	RoleStructure      ArtifactRole = "structure"
	RoleGenerated      ArtifactRole = "generated"
	RolePDF            ArtifactRole = "pdf"
)

var ErrInvalid = errors.New("invalid evaluation input")
var ErrUnavailable = errors.New("evaluation input unavailable")

type Recipe struct {
	Kind       string `json:"kind"`
	Source     string `json:"source,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	Variant    string `json:"variant,omitempty"`
	Size       int64  `json:"size,omitempty"`
	Dependency string `json:"dependency,omitempty"`
}

type FixtureDefinition struct {
	ID                   string         `json:"id"`
	SourceClass          string         `json:"source_class"`
	Privacy              string         `json:"privacy"`
	MediaType            string         `json:"media_type"`
	PageIDs              []string       `json:"page_ids"`
	Languages            []string       `json:"languages,omitempty"`
	DocumentType         string         `json:"document_type"`
	ExpectedOutputs      []ArtifactRole `json:"expected_outputs"`
	ExpectedFailureClass string         `json:"expected_failure_class,omitempty"`
	SHA256               string         `json:"sha256,omitempty"`
	Bytes                int64          `json:"bytes,omitempty"`
	ExpectedText         []string       `json:"expected_text,omitempty"`
	Recipe               Recipe         `json:"recipe"`
}

type Catalog struct {
	Format   string              `json:"format"`
	Version  int                 `json:"version"`
	Fixtures []FixtureDefinition `json:"fixtures"`
}

type PrivateCatalog struct {
	Format   string           `json:"format"`
	Version  int              `json:"version"`
	Fixtures []PrivateFixture `json:"fixtures"`
}

type PrivateFixture struct {
	ID                   string         `json:"id"`
	Privacy              string         `json:"privacy"`
	MediaType            string         `json:"media_type"`
	PageIDs              []string       `json:"page_ids"`
	Languages            []string       `json:"languages,omitempty"`
	DocumentType         string         `json:"document_type"`
	ExpectedOutputs      []ArtifactRole `json:"expected_outputs"`
	ExpectedFailureClass string         `json:"expected_failure_class,omitempty"`
	RelativePath         string         `json:"relative_path"`
	SHA256               string         `json:"sha256"`
	Bytes                int64          `json:"bytes"`
}

type GeneratedFixture struct {
	ID                   string         `json:"id"`
	Privacy              string         `json:"privacy"`
	MediaType            string         `json:"media_type"`
	PageIDs              []string       `json:"page_ids"`
	Languages            []string       `json:"languages,omitempty"`
	DocumentType         string         `json:"document_type"`
	ExpectedOutputs      []ArtifactRole `json:"expected_outputs"`
	ExpectedFailureClass string         `json:"expected_failure_class,omitempty"`
	SHA256               string         `json:"sha256"`
	Bytes                int64          `json:"bytes"`
	ExpectedText         []string       `json:"expected_text,omitempty"`
	Availability         string         `json:"availability"`
}

type GeneratedManifest struct {
	Format      string             `json:"format"`
	Version     int                `json:"version"`
	ToolVersion string             `json:"tool_version"`
	Fixtures    []GeneratedFixture `json:"fixtures"`
}

func ValidRole(role ArtifactRole) bool {
	switch role {
	case RoleOriginal, RoleCorrection, RoleOCRText, RolePositionedText, RoleStructure, RoleGenerated, RolePDF:
		return true
	default:
		return false
	}
}

func ValidID(id string) bool {
	if len(id) < 16 || len(id) > 96 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
