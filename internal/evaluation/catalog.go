package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxFixtures = 128
const maxFixtureBytes = 200 << 20

func LoadCatalog(path string) (Catalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return Catalog{}, ErrInvalid
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4<<20))
	decoder.DisallowUnknownFields()
	var catalog Catalog
	if err := decoder.Decode(&catalog); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return Catalog{}, ErrInvalid
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func (c Catalog) Validate() error {
	if c.Format != CatalogFormat || c.Version != CatalogVersion || len(c.Fixtures) == 0 || len(c.Fixtures) > maxFixtures {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, fixture := range c.Fixtures {
		if !ValidID(fixture.ID) || seen[fixture.ID] || fixture.SourceClass != "public_synthetic" || fixture.Privacy != PrivacyPublicSynthetic ||
			fixture.MediaType != "image/png" || len(fixture.PageIDs) == 0 || len(fixture.PageIDs) > 256 || !boundedToken(fixture.DocumentType, 64) ||
			len(fixture.ExpectedOutputs) == 0 || len(fixture.ExpectedOutputs) > 6 || fixture.Bytes < 0 || fixture.Bytes > maxFixtureBytes ||
			!validHash(fixture.SHA256) || !validStrings(fixture.Languages, 8, 16) || !validStrings(fixture.ExpectedText, 64, 1024) {
			return ErrInvalid
		}
		seen[fixture.ID] = true
		pageSeen := map[string]bool{}
		for _, page := range fixture.PageIDs {
			if !ValidID(page) || pageSeen[page] {
				return ErrInvalid
			}
			pageSeen[page] = true
		}
		roleSeen := map[ArtifactRole]bool{}
		for _, role := range fixture.ExpectedOutputs {
			if !ValidRole(role) || roleSeen[role] {
				return ErrInvalid
			}
			roleSeen[role] = true
		}
		if fixture.Recipe.Kind != "copy" || !safeRelative(fixture.Recipe.Source) {
			return ErrInvalid
		}
		for _, text := range fixture.ExpectedText {
			if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
				return ErrInvalid
			}
		}
	}
	return nil
}

func Generate(catalogPath, output, manifestPath string) error {
	catalog, err := LoadCatalog(catalogPath)
	if err != nil {
		return err
	}
	if err := ensureEmptyDirectory(output); err != nil {
		return err
	}
	fixtures := append([]FixtureDefinition(nil), catalog.Fixtures...)
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].ID < fixtures[j].ID })
	manifest := GeneratedManifest{Format: CatalogFormat, Version: CatalogVersion, ToolVersion: ToolVersion}
	for _, fixture := range fixtures {
		source, err := resolveRecipeSource(filepath.Dir(filepath.Dir(catalogPath)), fixture.Recipe.Source)
		if err != nil {
			return err
		}
		body, err := readBoundedFile(source, maxFixtureBytes)
		if err != nil {
			return err
		}
		if !matchesFixture(fixture, body) {
			return ErrInvalid
		}
		name := fixture.ID + ".png"
		if err := os.WriteFile(filepath.Join(output, name), body, 0o644); err != nil {
			return err
		}
		manifest.Fixtures = append(manifest.Fixtures, GeneratedFixture{
			ID: fixture.ID, Privacy: fixture.Privacy, MediaType: fixture.MediaType,
			PageIDs: fixture.PageIDs, Languages: fixture.Languages, DocumentType: fixture.DocumentType,
			ExpectedOutputs: fixture.ExpectedOutputs, ExpectedFailureClass: fixture.ExpectedFailureClass,
			SHA256: fixture.SHA256, Bytes: fixture.Bytes, ExpectedText: fixture.ExpectedText, Availability: "available",
		})
	}
	return writeJSONExclusive(manifestPath, manifest)
}

func List(catalogPath string) (GeneratedManifest, error) {
	catalog, err := LoadCatalog(catalogPath)
	if err != nil {
		return GeneratedManifest{}, err
	}
	fixtures := append([]FixtureDefinition(nil), catalog.Fixtures...)
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].ID < fixtures[j].ID })
	manifest := GeneratedManifest{Format: CatalogFormat, Version: CatalogVersion, ToolVersion: ToolVersion}
	for _, fixture := range fixtures {
		availability := "available"
		source, err := resolveRecipeSource(filepath.Dir(filepath.Dir(catalogPath)), fixture.Recipe.Source)
		if err != nil {
			availability = "unavailable"
		} else {
			body, readErr := readBoundedFile(source, maxFixtureBytes)
			if readErr != nil || !matchesFixture(fixture, body) {
				availability = "unavailable"
			}
		}
		manifest.Fixtures = append(manifest.Fixtures, GeneratedFixture{
			ID: fixture.ID, Privacy: fixture.Privacy, MediaType: fixture.MediaType,
			PageIDs: fixture.PageIDs, Languages: fixture.Languages, DocumentType: fixture.DocumentType,
			ExpectedOutputs: fixture.ExpectedOutputs, ExpectedFailureClass: fixture.ExpectedFailureClass,
			SHA256: fixture.SHA256, Bytes: fixture.Bytes, ExpectedText: fixture.ExpectedText, Availability: availability,
		})
	}
	return manifest, nil
}

func ensureEmptyDirectory(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return ErrInvalid
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return ErrInvalid
	}
	return nil
}

func resolveRecipeSource(base, source string) (string, error) {
	if !safeRelative(source) {
		return "", ErrInvalid
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return "", ErrInvalid
	}
	defer root.Close()
	info, err := root.Lstat(source)
	if err != nil || !info.Mode().IsRegular() {
		return "", ErrInvalid
	}
	file, err := root.Open(source)
	if err != nil {
		return "", ErrInvalid
	}
	file.Close()
	return filepath.Join(base, source), nil
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, ErrInvalid
	}
	return body, nil
}

func matchesFixture(fixture FixtureDefinition, body []byte) bool {
	sum := sha256.Sum256(body)
	return int64(len(body)) == fixture.Bytes && hex.EncodeToString(sum[:]) == fixture.SHA256
}

func writeJSONExclusive(path string, value any) error {
	if path == "" || !filepath.IsAbs(path) {
		return ErrInvalid
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrInvalid
	}
	if _, err := io.Copy(file, bytes.NewReader(body)); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func validHash(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validStrings(values []string, maxItems, maxLen int) bool {
	if len(values) > maxItems {
		return false
	}
	for _, value := range values {
		if !utf8.ValidString(value) || len(value) == 0 || len(value) > maxLen || strings.ContainsRune(value, 0) {
			return false
		}
	}
	return true
}

func boundedToken(value string, max int) bool {
	return value != "" && len(value) <= max && utf8.ValidString(value) && !strings.ContainsAny(value, "/\\\x00")
}

func safeRelative(value string) bool {
	return value != "" && !filepath.IsAbs(value) && filepath.Clean(value) == value &&
		value != "." && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsRune(value, 0)
}
