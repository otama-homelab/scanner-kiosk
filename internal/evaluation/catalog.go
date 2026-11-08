package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxFixtures = 128
const maxFixtureBytes = 200 << 20

var RequiredDocumentTypes = []string{
	"a4-text",
	"receipt",
	"card",
	"photo",
	"table-form",
	"vertical-text",
	"faint",
	"skew",
	"shadow",
	"white-on-white",
	"invalid",
	"oversized",
	"dependency-failure",
}

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

func LoadPrivateCatalog(path string) (PrivateCatalog, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return PrivateCatalog{}, ErrInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return PrivateCatalog{}, ErrInvalid
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4<<20))
	decoder.DisallowUnknownFields()
	var catalog PrivateCatalog
	if err := decoder.Decode(&catalog); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return PrivateCatalog{}, ErrInvalid
	}
	if err := catalog.Validate(); err != nil {
		return PrivateCatalog{}, err
	}
	return catalog, nil
}

func ResolveFixture(catalogPath, privateCatalogPath, privateRoot, fixtureID string) (FixtureDefinition, []byte, error) {
	if (privateCatalogPath == "") != (privateRoot == "") ||
		privateRoot != "" && (!filepath.IsAbs(privateRoot) || filepath.Clean(privateRoot) != privateRoot) ||
		fixtureID == "" {
		return FixtureDefinition{}, nil, ErrInvalid
	}
	public, err := LoadCatalog(catalogPath)
	if err != nil {
		return FixtureDefinition{}, nil, err
	}
	if privateCatalogPath == "" {
		for _, fixture := range public.Fixtures {
			if fixture.ID == fixtureID {
				return resolvePublicFixture(filepath.Dir(filepath.Dir(catalogPath)), fixture)
			}
		}
		return FixtureDefinition{}, nil, ErrInvalid
	}

	private, err := LoadPrivateCatalog(privateCatalogPath)
	if err != nil {
		return FixtureDefinition{}, nil, err
	}
	publicIDs := make(map[string]bool, len(public.Fixtures))
	for _, fixture := range public.Fixtures {
		publicIDs[fixture.ID] = true
	}
	for _, fixture := range private.Fixtures {
		if publicIDs[fixture.ID] {
			return FixtureDefinition{}, nil, ErrInvalid
		}
	}
	for _, fixture := range private.Fixtures {
		if fixture.ID == fixtureID {
			root, err := os.OpenRoot(privateRoot)
			if err != nil {
				return FixtureDefinition{}, nil, ErrUnavailable
			}
			defer root.Close()
			body, err := readPrivateFixture(root, fixture)
			if err != nil {
				return FixtureDefinition{}, nil, ErrUnavailable
			}
			resolved := privateFixtureDefinition(fixture)
			if failure := classifyPrivateFailure(fixture, body); failure != nil {
				return resolved, nil, failure
			}
			return resolved, body, nil
		}
	}
	for _, fixture := range public.Fixtures {
		if fixture.ID == fixtureID {
			return resolvePublicFixture(filepath.Dir(filepath.Dir(catalogPath)), fixture)
		}
	}
	return FixtureDefinition{}, nil, ErrInvalid
}

type FixtureFailure struct {
	FixtureID     string
	FixtureSHA256 string
	ExpectedClass string
	ObservedClass string
	Match         bool
}

func (failure *FixtureFailure) Error() string {
	if failure.Match {
		return "expected fixture failure"
	}
	return "fixture failure mismatch"
}

func (failure *FixtureFailure) Unwrap() error {
	if failure.Match {
		return nil
	}
	return ErrInvalid
}

func resolvePublicFixture(base string, fixture FixtureDefinition) (FixtureDefinition, []byte, error) {
	if fixture.ExpectedFailureClass != "" {
		sha, observed, err := observePublicFailure(base, fixture)
		if err != nil || fixture.SHA256 != "" && sha != fixture.SHA256 {
			return FixtureDefinition{}, nil, ErrInvalid
		}
		return fixture, nil, &FixtureFailure{
			FixtureID: fixture.ID, FixtureSHA256: sha,
			ExpectedClass: fixture.ExpectedFailureClass, ObservedClass: observed,
			Match: observed == fixture.ExpectedFailureClass,
		}
	}
	body, err := generateFixture(base, fixture)
	if err != nil || fixture.SHA256 != "" && !matchesFixture(fixture, body) {
		return FixtureDefinition{}, nil, ErrInvalid
	}
	return fixture, body, nil
}

func observePublicFailure(base string, fixture FixtureDefinition) (string, string, error) {
	switch fixture.Recipe.Variant {
	case "invalid":
		body, err := generateFixture(base, fixture)
		if err != nil {
			return "", "", err
		}
		observed := "expected-class-mismatch"
		if _, _, err := image.Decode(bytes.NewReader(body)); err != nil {
			observed = "invalid-input"
		}
		return hexSHA256(body), observed, nil
	case "oversized":
		sha, err := hashZeroFixture(fixture.Recipe.Size)
		return sha, "oversized-input", err
	case "dependency-failure":
		body, err := generateFixture(base, fixture)
		if err != nil {
			return "", "", err
		}
		missing, err := dependencyMissing(base, fixture.Recipe.Dependency)
		if err != nil {
			return "", "", err
		}
		observed := "expected-class-mismatch"
		if missing {
			observed = "dependency-unavailable"
		}
		return hexSHA256(body), observed, nil
	default:
		return "", "expected-class-mismatch", nil
	}
}

func privateFixtureDefinition(fixture PrivateFixture) FixtureDefinition {
	return FixtureDefinition{
		ID: fixture.ID, SourceClass: "owner-private", Privacy: fixture.Privacy,
		MediaType: fixture.MediaType, PageIDs: append([]string(nil), fixture.PageIDs...),
		Languages: append([]string(nil), fixture.Languages...), DocumentType: fixture.DocumentType,
		ExpectedOutputs:      append([]ArtifactRole(nil), fixture.ExpectedOutputs...),
		ExpectedFailureClass: fixture.ExpectedFailureClass,
		SHA256:               fixture.SHA256, Bytes: fixture.Bytes,
	}
}

func classifyPrivateFailure(fixture PrivateFixture, body []byte) *FixtureFailure {
	if fixture.ExpectedFailureClass == "" {
		return nil
	}
	observed := "expected-class-mismatch"
	if fixture.ExpectedFailureClass == "invalid-input" {
		if _, _, err := image.Decode(bytes.NewReader(body)); err != nil {
			observed = "invalid-input"
		}
	}
	return &FixtureFailure{
		FixtureID: fixture.ID, FixtureSHA256: fixture.SHA256,
		ExpectedClass: fixture.ExpectedFailureClass, ObservedClass: observed,
		Match: observed == fixture.ExpectedFailureClass,
	}
}

func hashZeroFixture(size int64) (string, error) {
	if size < 0 {
		return "", ErrInvalid
	}
	hasher := sha256.New()
	if _, err := io.CopyN(hasher, zeroReader{}, size); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

func hexSHA256(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func readPrivateFixture(root *os.Root, fixture PrivateFixture) ([]byte, error) {
	info, err := root.Lstat(fixture.RelativePath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 ||
		info.Size() != fixture.Bytes || info.Size() > maxFixtureBytes {
		return nil, ErrUnavailable
	}
	file, err := root.Open(fixture.RelativePath)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(file, maxFixtureBytes+1))
	if err != nil || int64(len(body)) != fixture.Bytes || !matchesFixture(FixtureDefinition{
		SHA256: fixture.SHA256, Bytes: fixture.Bytes,
	}, body) {
		return nil, ErrUnavailable
	}
	return body, nil
}

func (c PrivateCatalog) Validate() error {
	if c.Format != CatalogFormat || c.Version != CatalogVersion || len(c.Fixtures) == 0 || len(c.Fixtures) > maxFixtures {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for index, fixture := range c.Fixtures {
		if !ValidID(fixture.ID) || seen[fixture.ID] || fixture.Privacy != PrivacyOwnerPrivate ||
			fixture.MediaType != "image/png" || len(fixture.PageIDs) == 0 || len(fixture.PageIDs) > 256 ||
			!boundedToken(fixture.DocumentType, 64) || len(fixture.ExpectedOutputs) > 6 ||
			len(fixture.ExpectedOutputs) == 0 && fixture.ExpectedFailureClass == "" ||
			!boundedOptionalToken(fixture.ExpectedFailureClass, 64) || !safeRelative(fixture.RelativePath) ||
			!validHash(fixture.SHA256) || fixture.Bytes <= 0 || fixture.Bytes > maxFixtureBytes ||
			!validStrings(fixture.Languages, 8, 16) {
			return fmt.Errorf("%w: private fixture %d", ErrInvalid, index)
		}
		seen[fixture.ID] = true
		roles := map[ArtifactRole]bool{}
		for _, role := range fixture.ExpectedOutputs {
			if !ValidRole(role) || roles[role] {
				return fmt.Errorf("%w: private fixture %d", ErrInvalid, index)
			}
			roles[role] = true
		}
	}
	return nil
}

func (c Catalog) Validate() error {
	if c.Format != CatalogFormat || c.Version != CatalogVersion || len(c.Fixtures) == 0 || len(c.Fixtures) > maxFixtures {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for index, fixture := range c.Fixtures {
		if !ValidID(fixture.ID) || seen[fixture.ID] || fixture.SourceClass != "public_synthetic" || fixture.Privacy != PrivacyPublicSynthetic ||
			fixture.MediaType != "image/png" || len(fixture.PageIDs) == 0 || len(fixture.PageIDs) > 256 || !boundedToken(fixture.DocumentType, 64) ||
			len(fixture.ExpectedOutputs) > 6 || fixture.Bytes < 0 ||
			fixture.Bytes > maxFixtureBytes && !(fixture.Recipe.Variant == "oversized" && fixture.ExpectedFailureClass == "oversized-input") ||
			!validStrings(fixture.Languages, 8, 16) || !validStrings(fixture.ExpectedText, 64, 1024) ||
			!boundedOptionalToken(fixture.ExpectedFailureClass, 64) {
			return fmt.Errorf("%w: fixture %d", ErrInvalid, index)
		}
		if len(fixture.ExpectedOutputs) == 0 && fixture.ExpectedFailureClass == "" {
			return ErrInvalid
		}
		seen[fixture.ID] = true
		pageSeen := map[string]bool{}
		for _, page := range fixture.PageIDs {
			if !boundedToken(page, 96) || pageSeen[page] {
				return fmt.Errorf("%w: fixture %d", ErrInvalid, index)
			}
			pageSeen[page] = true
		}
		roleSeen := map[ArtifactRole]bool{}
		for _, role := range fixture.ExpectedOutputs {
			if !ValidRole(role) || roleSeen[role] {
				return fmt.Errorf("%w: fixture %d", ErrInvalid, index)
			}
			roleSeen[role] = true
		}
		if fixture.Recipe.Kind == "copy" {
			if !safeRelative(fixture.Recipe.Source) || !validHash(fixture.SHA256) || fixture.Bytes == 0 {
				return ErrInvalid
			}
		} else if fixture.Recipe.Kind == "pattern" {
			if !validPatternVariant(fixture.Recipe.Variant) || fixture.SHA256 != "" && !validHash(fixture.SHA256) {
				return ErrInvalid
			}
			if fixture.SHA256 != "" && fixture.Bytes == 0 {
				return ErrInvalid
			}
		} else {
			return fmt.Errorf("%w: fixture %d", ErrInvalid, index)
		}
		if !validRecipeFields(fixture) {
			return ErrInvalid
		}
		for _, text := range fixture.ExpectedText {
			if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
				return fmt.Errorf("%w: fixture %d", ErrInvalid, index)
			}
		}
	}
	return nil
}

func validRecipeFields(fixture FixtureDefinition) bool {
	if fixture.Recipe.Kind == "copy" {
		return fixture.Recipe.Size == 0 && fixture.Recipe.Dependency == "" && fixture.ExpectedFailureClass == ""
	}
	switch fixture.Recipe.Variant {
	case "invalid":
		return fixture.Recipe.Size == 0 && fixture.Recipe.Dependency == "" &&
			fixture.ExpectedFailureClass == "invalid-input" && len(fixture.ExpectedOutputs) == 0
	case "oversized":
		return fixture.Recipe.Size == maxFixtureBytes+1 && fixture.Bytes == fixture.Recipe.Size &&
			fixture.Recipe.Dependency == "" && fixture.ExpectedFailureClass == "oversized-input" &&
			len(fixture.ExpectedOutputs) == 0
	case "dependency-failure":
		return fixture.Recipe.Size == 0 && safeRelative(fixture.Recipe.Dependency) &&
			fixture.Bytes > 0 && fixture.ExpectedFailureClass == "dependency-unavailable" &&
			len(fixture.ExpectedOutputs) == 0
	default:
		return fixture.Recipe.Size == 0 && fixture.Recipe.Dependency == "" && fixture.ExpectedFailureClass == ""
	}
}

func ValidateRequiredCoverage(catalog Catalog) error {
	if err := catalog.Validate(); err != nil {
		return err
	}
	present := map[string]bool{}
	for _, fixture := range catalog.Fixtures {
		present[fixture.DocumentType] = true
	}
	for _, required := range RequiredDocumentTypes {
		if !present[required] {
			return ErrInvalid
		}
	}
	return nil
}

func Generate(catalogPath, output, manifestPath string) error {
	catalog, err := LoadCatalog(catalogPath)
	if err != nil {
		return err
	}
	if err := ValidateRequiredCoverage(catalog); err != nil {
		return err
	}
	if err := ensureEmptyDirectory(output); err != nil {
		return err
	}
	fixtures := append([]FixtureDefinition(nil), catalog.Fixtures...)
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].ID < fixtures[j].ID })
	manifest := GeneratedManifest{Format: CatalogFormat, Version: CatalogVersion, ToolVersion: ToolVersion}
	base := filepath.Dir(filepath.Dir(catalogPath))
	for _, fixture := range fixtures {
		path := filepath.Join(output, fixture.ID+".png")
		sha, size, err := materializeFixture(base, fixture, path)
		if err != nil {
			return err
		}
		availability, err := classifyGeneratedFixture(base, fixture, path)
		if err != nil {
			return err
		}
		manifest.Fixtures = append(manifest.Fixtures, GeneratedFixture{
			ID: fixture.ID, Privacy: fixture.Privacy, MediaType: fixture.MediaType,
			PageIDs: fixture.PageIDs, Languages: fixture.Languages, DocumentType: fixture.DocumentType,
			ExpectedOutputs: fixture.ExpectedOutputs, ExpectedFailureClass: fixture.ExpectedFailureClass,
			SHA256: sha, Bytes: size, ExpectedText: fixture.ExpectedText, Availability: availability,
		})
	}
	return writeJSONExclusive(manifestPath, manifest)
}

func List(catalogPath string) (GeneratedManifest, error) {
	catalog, err := LoadCatalog(catalogPath)
	if err != nil {
		return GeneratedManifest{}, err
	}
	if err := ValidateRequiredCoverage(catalog); err != nil {
		return GeneratedManifest{}, err
	}
	fixtures := append([]FixtureDefinition(nil), catalog.Fixtures...)
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].ID < fixtures[j].ID })
	manifest := GeneratedManifest{Format: CatalogFormat, Version: CatalogVersion, ToolVersion: ToolVersion}
	base := filepath.Dir(filepath.Dir(catalogPath))
	for _, fixture := range fixtures {
		availability := "available"
		sha := fixture.SHA256
		size := fixture.Bytes
		if fixture.Recipe.Kind == "copy" {
			source, readErr := resolveRecipeSource(base, fixture.Recipe.Source)
			if readErr != nil {
				availability = "unavailable"
			} else {
				body, readErr := readBoundedFile(source, maxFixtureBytes)
				if readErr != nil || !matchesFixture(fixture, body) {
					availability = "unavailable"
				}
			}
		} else if sha == "" || size == 0 {
			availability = "unavailable"
		}
		if availability == "available" {
			availability, err = classifyListedFixture(base, fixture)
			if err != nil {
				return GeneratedManifest{}, err
			}
		}
		manifest.Fixtures = append(manifest.Fixtures, GeneratedFixture{
			ID: fixture.ID, Privacy: fixture.Privacy, MediaType: fixture.MediaType,
			PageIDs: fixture.PageIDs, Languages: fixture.Languages, DocumentType: fixture.DocumentType,
			ExpectedOutputs: fixture.ExpectedOutputs, ExpectedFailureClass: fixture.ExpectedFailureClass,
			SHA256: sha, Bytes: size, ExpectedText: fixture.ExpectedText, Availability: availability,
		})
	}
	return manifest, nil
}

func materializeFixture(base string, fixture FixtureDefinition, outputPath string) (string, int64, error) {
	if fixture.Recipe.Kind == "pattern" && fixture.Recipe.Variant == "oversized" {
		file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
		if err != nil {
			return "", 0, err
		}
		if err := file.Truncate(fixture.Recipe.Size); err != nil {
			file.Close()
			return "", 0, err
		}
		if err := file.Sync(); err != nil {
			file.Close()
			return "", 0, err
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			file.Close()
			return "", 0, err
		}
		hasher := sha256.New()
		size, err := io.Copy(hasher, file)
		closeErr := file.Close()
		if err != nil {
			return "", 0, err
		}
		if closeErr != nil {
			return "", 0, closeErr
		}
		sha := hex.EncodeToString(hasher.Sum(nil))
		if size != fixture.Bytes || fixture.SHA256 != "" && fixture.SHA256 != sha {
			return "", 0, ErrInvalid
		}
		return sha, size, nil
	}
	body, err := generateFixture(base, fixture)
	if err != nil {
		return "", 0, err
	}
	if fixture.SHA256 != "" && !matchesFixture(fixture, body) {
		return "", 0, ErrInvalid
	}
	if err := os.WriteFile(outputPath, body, 0o644); err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), int64(len(body)), nil
}

func declaredFailureClass(fixture FixtureDefinition) (string, error) {
	expected := ""
	switch fixture.Recipe.Variant {
	case "invalid":
		expected = "invalid-input"
	case "oversized":
		expected = "oversized-input"
	case "dependency-failure":
		expected = "dependency-unavailable"
	}
	if fixture.ExpectedFailureClass != expected {
		return "", ErrInvalid
	}
	return expected, nil
}

func classifyGeneratedFixture(base string, fixture FixtureDefinition, outputPath string) (string, error) {
	declared, err := declaredFailureClass(fixture)
	if err != nil {
		return "", err
	}
	if declared == "" {
		return "available", nil
	}
	observed := ""
	switch fixture.Recipe.Variant {
	case "invalid":
		file, err := os.Open(outputPath)
		if err != nil {
			return "", err
		}
		_, _, decodeErr := image.Decode(file)
		file.Close()
		if decodeErr == nil {
			return "", ErrInvalid
		}
		observed = "invalid-input"
	case "oversized":
		info, err := os.Lstat(outputPath)
		if err != nil || !info.Mode().IsRegular() || info.Size() != maxFixtureBytes+1 {
			return "", ErrInvalid
		}
		if _, err := readBoundedFile(outputPath, maxFixtureBytes); !errors.Is(err, ErrInvalid) {
			return "", ErrInvalid
		}
		observed = "oversized-input"
	case "dependency-failure":
		missing, err := dependencyMissing(base, fixture.Recipe.Dependency)
		if err != nil || !missing {
			return "", ErrInvalid
		}
		observed = "dependency-unavailable"
	}
	if observed != declared {
		return "", ErrInvalid
	}
	return "expected-failure", nil
}

func classifyListedFixture(base string, fixture FixtureDefinition) (string, error) {
	declared, err := declaredFailureClass(fixture)
	if err != nil {
		return "", err
	}
	if declared == "" {
		return "available", nil
	}
	observed := ""
	switch fixture.Recipe.Variant {
	case "invalid":
		body, err := generateFixture(base, fixture)
		if err != nil {
			return "", err
		}
		if _, _, err := image.Decode(bytes.NewReader(body)); err == nil {
			return "", ErrInvalid
		}
		observed = "invalid-input"
	case "oversized":
		if fixture.Recipe.Size != maxFixtureBytes+1 || fixture.Bytes != maxFixtureBytes+1 {
			return "", ErrInvalid
		}
		observed = "oversized-input"
	case "dependency-failure":
		missing, err := dependencyMissing(base, fixture.Recipe.Dependency)
		if err != nil || !missing {
			return "", ErrInvalid
		}
		observed = "dependency-unavailable"
	}
	if observed != declared {
		return "", ErrInvalid
	}
	return "expected-failure", nil
}

func dependencyMissing(base, dependency string) (bool, error) {
	root, err := os.OpenRoot(base)
	if err != nil {
		return false, err
	}
	defer root.Close()
	_, err = root.Lstat(dependency)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func ListWithPrivate(catalogPath, privateCatalogPath, privateRoot string) (GeneratedManifest, error) {
	manifest, err := List(catalogPath)
	if err != nil {
		return GeneratedManifest{}, err
	}
	if privateCatalogPath == "" && privateRoot == "" {
		return manifest, nil
	}
	if privateCatalogPath == "" || privateRoot == "" || !filepath.IsAbs(privateRoot) {
		return GeneratedManifest{}, ErrInvalid
	}
	public, err := LoadCatalog(catalogPath)
	if err != nil {
		return GeneratedManifest{}, err
	}
	private, err := LoadPrivateCatalog(privateCatalogPath)
	if err != nil {
		return GeneratedManifest{}, err
	}
	publicIDs := map[string]bool{}
	for _, fixture := range public.Fixtures {
		publicIDs[fixture.ID] = true
	}
	root, err := os.OpenRoot(privateRoot)
	if err != nil {
		return GeneratedManifest{}, ErrInvalid
	}
	defer root.Close()
	for _, fixture := range private.Fixtures {
		if publicIDs[fixture.ID] {
			return GeneratedManifest{}, ErrInvalid
		}
		generated := GeneratedFixture{
			ID: fixture.ID, Privacy: fixture.Privacy, MediaType: fixture.MediaType,
			PageIDs: fixture.PageIDs, Languages: fixture.Languages, DocumentType: fixture.DocumentType,
			ExpectedOutputs: fixture.ExpectedOutputs, ExpectedFailureClass: fixture.ExpectedFailureClass,
			SHA256: fixture.SHA256, Bytes: fixture.Bytes, Availability: "unavailable",
		}
		info, err := root.Lstat(fixture.RelativePath)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o077 == 0 {
			file, openErr := root.Open(fixture.RelativePath)
			if openErr == nil {
				body, readErr := io.ReadAll(io.LimitReader(file, maxFixtureBytes+1))
				file.Close()
				if readErr == nil && int64(len(body)) == fixture.Bytes {
					sum := sha256.Sum256(body)
					if hex.EncodeToString(sum[:]) == fixture.SHA256 {
						generated.Availability = "available"
					}
				}
			}
		}
		manifest.Fixtures = append(manifest.Fixtures, generated)
	}
	sort.Slice(manifest.Fixtures, func(i, j int) bool { return manifest.Fixtures[i].ID < manifest.Fixtures[j].ID })
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

func generateFixture(base string, fixture FixtureDefinition) ([]byte, error) {
	if fixture.Recipe.Kind == "copy" {
		source, err := resolveRecipeSource(base, fixture.Recipe.Source)
		if err != nil {
			return nil, err
		}
		return readBoundedFile(source, maxFixtureBytes)
	}
	if fixture.Recipe.Kind != "pattern" {
		return nil, ErrInvalid
	}
	if fixture.Recipe.Variant == "invalid" {
		return []byte("not a png fixture\n"), nil
	}
	if fixture.Recipe.Variant == "oversized" {
		return nil, ErrInvalid
	}
	var baseImage image.Image
	if fixture.Recipe.Source != "" {
		source, err := resolveRecipeSource(base, fixture.Recipe.Source)
		if err != nil {
			return nil, err
		}
		file, err := os.Open(source)
		if err != nil {
			return nil, ErrInvalid
		}
		decoded, _, err := image.Decode(file)
		file.Close()
		if err != nil {
			return nil, ErrInvalid
		}
		baseImage = decoded
	}
	img, err := renderPattern(fixture.Recipe.Variant, baseImage)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&output, img); err != nil {
		return nil, err
	}
	if output.Len() > maxFixtureBytes {
		return nil, ErrInvalid
	}
	return output.Bytes(), nil
}

func renderPattern(variant string, base image.Image) (image.Image, error) {
	switch variant {
	case "vertical":
		if base == nil {
			return nil, ErrInvalid
		}
		src := rgbaImage(base)
		b := src.Bounds()
		dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				target := x*dst.Stride + (b.Dy()-1-y)*4
				copy(dst.Pix[target:target+4], src.Pix[y*src.Stride+x*4:y*src.Stride+x*4+4])
			}
		}
		return dst, nil
	case "faint", "skew", "shadow":
		if base == nil {
			return nil, ErrInvalid
		}
		return transformBase(variant, base), nil
	case "receipt":
		return patternCanvas(640, 960, variant), nil
	case "card":
		return patternCanvas(960, 640, variant), nil
	case "photo":
		return patternCanvas(960, 640, variant), nil
	case "table-form":
		return patternCanvas(1280, 1800, variant), nil
	case "white-on-white":
		return patternCanvas(1200, 1600, variant), nil
	case "dependency-failure":
		return patternCanvas(64, 64, variant), nil
	default:
		return nil, ErrInvalid
	}
}

func transformBase(variant string, base image.Image) image.Image {
	src := rgbaImage(base)
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	for y := 0; y < b.Dy(); y++ {
		shift := 0
		if variant == "skew" {
			shift = y%41 - 20
		}
		for x := 0; x < b.Dx(); x++ {
			source := y*src.Stride + x*4
			targetX := x + shift
			if targetX < 0 || targetX >= b.Dx() {
				continue
			}
			target := y*dst.Stride + targetX*4
			copy(dst.Pix[target:target+4], src.Pix[source:source+4])
			if variant == "faint" {
				for channel := 0; channel < 3; channel++ {
					dst.Pix[target+channel] = uint8((uint16(dst.Pix[target+channel]) + 3*255) / 4)
				}
			}
			if variant == "shadow" {
				shade := 255 - y*64/(b.Dy()+1)
				for channel := 0; channel < 3; channel++ {
					dst.Pix[target+channel] = uint8(uint16(dst.Pix[target+channel]) * uint16(shade) / 255)
				}
			}
		}
	}
	return dst
}

func rgbaImage(src image.Image) *image.RGBA {
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

func patternCanvas(width, height int, variant string) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	ink := color.RGBA{R: 24, G: 28, B: 34, A: 255}
	for y := height / 10; y < height; y += height / 9 {
		thickness := height / 180
		for t := 0; t < thickness; t++ {
			draw.Draw(img, image.Rect(width/10, y+t, width*9/10, y+t+1), &image.Uniform{C: ink}, image.Point{}, draw.Src)
		}
	}
	if variant == "table-form" {
		for x := width / 5; x < width; x += width / 5 {
			draw.Draw(img, image.Rect(x, height/8, x+2, height*7/8), &image.Uniform{C: ink}, image.Point{}, draw.Src)
		}
	}
	if variant == "photo" {
		draw.Draw(img, image.Rect(width/5, height*3/4, width*4/5, height*7/8),
			&image.Uniform{C: color.RGBA{R: 176, G: 212, B: 232, A: 255}}, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(width*3/4, height/5, width*3/4+48, height/5+48),
			&image.Uniform{C: color.RGBA{R: 242, G: 192, B: 64, A: 255}}, image.Point{}, draw.Src)
	}
	if variant == "white-on-white" {
		draw.Draw(img, image.Rect(width/6, height/5, width*5/6, height*4/5), &image.Uniform{C: color.RGBA{248, 248, 248, 255}}, image.Point{}, draw.Src)
		lowInk := color.RGBA{R: 246, G: 246, B: 246, A: 255}
		for y := height / 4; y < height*3/4; y += height / 16 {
			draw.Draw(img, image.Rect(width/4, y, width*3/4, y+4), &image.Uniform{C: lowInk}, image.Point{}, draw.Src)
		}
	}
	if variant == "card" || variant == "photo" || variant == "receipt" {
		border := color.RGBA{R: 40, G: 90, B: 140, A: 255}
		draw.Draw(img, image.Rect(20, 20, width-20, 24), &image.Uniform{C: border}, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(20, height-24, width-20, height-20), &image.Uniform{C: border}, image.Point{}, draw.Src)
	}
	return img
}

func validPatternVariant(value string) bool {
	switch value {
	case "vertical", "faint", "skew", "shadow", "receipt", "card", "photo", "table-form", "white-on-white", "invalid", "oversized", "dependency-failure":
		return true
	default:
		return false
	}
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, ErrInvalid
	}
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

func boundedOptionalToken(value string, max int) bool {
	return value == "" || boundedToken(value, max)
}

func safeRelative(value string) bool {
	return value != "" && !filepath.IsAbs(value) && filepath.Clean(value) == value &&
		value != "." && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsRune(value, 0)
}
