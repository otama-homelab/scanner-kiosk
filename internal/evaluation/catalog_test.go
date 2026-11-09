package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogRejectsDuplicateID(t *testing.T) {
	catalog := Catalog{
		Format:  CatalogFormat,
		Version: CatalogVersion,
		Fixtures: []FixtureDefinition{
			testFixture("fixture_a4_ja_en_001"),
			testFixture("fixture_a4_ja_en_001"),
		},
	}
	if err := catalog.Validate(); err == nil {
		t.Fatal("duplicate fixture ID accepted")
	}
}

func TestJapaneseOCRCatalog(t *testing.T) {
	catalog, err := LoadCatalog(filepath.Join(repositoryRoot(t), "scripts/fixtures/evaluation/ocr-catalog.json"))
	if err != nil {
		t.Fatalf("Japanese copy catalog must support the two-page PDF source: %v", err)
	}
	if err := ValidateRequiredCoverage(catalog); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range catalog.Fixtures {
		if fixture.ID == "fixture_ocr_two_page_001" {
			_, body, err := ResolveFixture(filepath.Join(repositoryRoot(t), "scripts/fixtures/evaluation/ocr-catalog.json"), "", "", fixture.ID)
			if err != nil || !bytes.HasPrefix(body, []byte("%PDF-")) || len(fixture.PageIDs) != 2 {
				t.Fatal("two-page original unavailable")
			}
			return
		}
	}
	t.Fatal("missing two-page source")
}

func TestCatalogRejectsInvalidHashAndRole(t *testing.T) {
	for name, mutate := range map[string]func(*Catalog){
		"hash": func(c *Catalog) { c.Fixtures[0].SHA256 = "ABC" },
		"role": func(c *Catalog) { c.Fixtures[0].ExpectedOutputs = []ArtifactRole{"unknown"} },
		"id":   func(c *Catalog) { c.Fixtures[0].ID = "short" },
	} {
		t.Run(name, func(t *testing.T) {
			c := Catalog{
				Format:   CatalogFormat,
				Version:  CatalogVersion,
				Fixtures: []FixtureDefinition{testFixture("fixture_a4_ja_en_001")},
			}
			mutate(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	body := []byte(`{"format":"scanner-eval-fixtures","version":1,"fixtures":[],"unknown":true}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCatalog(path); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestCatalogJSONRoundTrip(t *testing.T) {
	body, err := json.Marshal(Catalog{
		Format:   CatalogFormat,
		Version:  CatalogVersion,
		Fixtures: []FixtureDefinition{testFixture("fixture_a4_ja_en_001")},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got Catalog
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryCatalogGenerates(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	work := t.TempDir()
	manifestPath := filepath.Join(work, "manifest.json")
	if err := Generate(catalog, filepath.Join(work, "fixtures"), manifestPath); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("SCANNER_EVAL_PRINT_HASHES") == "1" {
		var manifest GeneratedManifest
		body, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &manifest); err != nil {
			t.Fatal(err)
		}
		for _, fixture := range manifest.Fixtures {
			t.Logf("%s %s %d", fixture.ID, fixture.SHA256, fixture.Bytes)
		}
	}
}

func TestRequiredFixtureCoverage(t *testing.T) {
	catalog := loadRepositoryCatalog(t)
	if err := ValidateRequiredCoverage(catalog); err != nil {
		t.Fatal(err)
	}
}

func TestCorpusGenerationIsDeterministic(t *testing.T) {
	catalog := filepath.Join(repositoryRoot(t), "scripts", "fixtures", "evaluation", "catalog.json")
	var manifests [][]byte
	for i := 0; i < 2; i++ {
		work := t.TempDir()
		manifest := filepath.Join(work, "manifest.json")
		if err := Generate(catalog, filepath.Join(work, "fixtures"), manifest); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatal(err)
		}
		manifests = append(manifests, body)
	}
	if string(manifests[0]) != string(manifests[1]) {
		t.Fatal("generation manifest is not deterministic")
	}
}

func TestCatalogRejectsIncompleteCorpus(t *testing.T) {
	catalog := loadRepositoryCatalog(t)
	catalog.Fixtures = catalog.Fixtures[:1]
	if err := ValidateRequiredCoverage(catalog); err == nil {
		t.Fatal("incomplete corpus accepted")
	}
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestNegativeFixturesExerciseDeclaredFailures(t *testing.T) {
	catalog, _, manifest := generateRepositoryCorpus(t)
	expected := map[string]string{
		"fixture_invalid_input_001":      "invalid-input",
		"fixture_oversized_input_001":    "oversized-input",
		"fixture_dependency_failure_001": "dependency-unavailable",
	}
	for _, fixture := range manifest.Fixtures {
		if class, ok := expected[fixture.ID]; ok {
			if fixture.Availability != "expected-failure" || fixture.ExpectedFailureClass != class {
				t.Fatalf("%s availability=%s class=%s", fixture.ID, fixture.Availability, fixture.ExpectedFailureClass)
			}
			delete(expected, fixture.ID)
		}
	}
	if len(expected) != 0 {
		t.Fatalf("negative fixtures missing: %v", expected)
	}

	listed, err := List(catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range listed.Fixtures {
		if fixture.ExpectedFailureClass != "" && fixture.Availability != "expected-failure" {
			t.Fatalf("%s listed as %s", fixture.ID, fixture.Availability)
		}
	}
}

func TestOversizedFixtureFailsBoundedRead(t *testing.T) {
	_, output, manifest := generateRepositoryCorpus(t)
	fixture := findGenerated(t, manifest, "fixture_oversized_input_001")
	if fixture.Bytes != maxFixtureBytes+1 {
		t.Fatalf("oversized fixture bytes=%d, want %d", fixture.Bytes, maxFixtureBytes+1)
	}
	path := filepath.Join(output, "fixture_oversized_input_001.png")
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Size() != maxFixtureBytes+1 {
		t.Fatalf("materialized oversized fixture mode=%s bytes=%d", info.Mode(), info.Size())
	}
	if _, err := readBoundedFile(path, maxFixtureBytes); err == nil {
		t.Fatal("bounded read accepted the oversized fixture")
	}
}

func TestCardAndPhotoHaveDistinctContent(t *testing.T) {
	_, output, _ := generateRepositoryCorpus(t)
	cardBody, err := os.ReadFile(filepath.Join(output, "fixture_card_001.png"))
	if err != nil {
		t.Fatal(err)
	}
	photoBody, err := os.ReadFile(filepath.Join(output, "fixture_photo_001.png"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(cardBody, photoBody) || sha256Hex(cardBody) == sha256Hex(photoBody) {
		t.Fatal("card and photo share fixture bytes")
	}
	card, err := png.Decode(bytes.NewReader(cardBody))
	if err != nil {
		t.Fatal(err)
	}
	photo, err := png.Decode(bytes.NewReader(photoBody))
	if err != nil {
		t.Fatal(err)
	}
	if differentPixels(card, photo) < 1000 {
		t.Fatal("card and photo do not have substantive image differences")
	}
}

func TestPrivateFixture(t *testing.T) {
	public, privateCatalog, root := privateFixtureSet(t, "PRIVATE_CANARY_DO_NOT_EMIT", 0o600)
	manifest, err := ListWithPrivate(public, privateCatalog, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Fixtures) != len(RequiredDocumentTypes)+1 {
		t.Fatalf("got %d fixtures", len(manifest.Fixtures))
	}
	private := findGenerated(t, manifest, "fixture_owner_private_001")
	if private.Availability != "available" {
		t.Fatalf("private fixture unavailable: %+v", private)
	}
}

func TestPrivateCatalogRequiresPrivateMode(t *testing.T) {
	public, privateCatalog, root := privateFixtureSet(t, "PRIVATE_CANARY_DO_NOT_EMIT", 0o600)
	if err := os.Chmod(privateCatalog, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ListWithPrivate(public, privateCatalog, root); !errors.Is(err, ErrInvalid) {
		t.Fatalf("world-readable private catalog accepted: %v", err)
	}
}

func TestPrivateCanary(t *testing.T) {
	canary := "PRIVATE_CANARY_DO_NOT_EMIT"
	public, privateCatalog, root := privateFixtureSet(t, canary, 0o600)
	manifest, err := ListWithPrivate(public, privateCatalog, root)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{canary, root, "owner-private.png"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("private value leaked: %s", secret)
		}
	}
}

func TestMissingPrivateFixture(t *testing.T) {
	public, privateCatalog, root := privateFixtureSet(t, "PRIVATE_CANARY_DO_NOT_EMIT", 0o600)
	if err := os.Remove(filepath.Join(root, "owner", "private.png")); err != nil {
		t.Fatal(err)
	}
	manifest, err := ListWithPrivate(public, privateCatalog, root)
	if err != nil {
		t.Fatal(err)
	}
	if findGenerated(t, manifest, "fixture_owner_private_001").Availability != "unavailable" {
		t.Fatal("missing private fixture was not unavailable")
	}
}

func TestPrivateRejectsUnsafeFiles(t *testing.T) {
	for name, mutate := range map[string]func(t *testing.T, path string){
		"public": func(t *testing.T, path string) {
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"symlink": func(t *testing.T, path string) {
			real := path + ".real"
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(real, body, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, path); err != nil {
				t.Skip("symlinks unavailable")
			}
		},
		"hash": func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("different"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			public, privateCatalog, root := privateFixtureSet(t, "PRIVATE_CANARY_DO_NOT_EMIT", 0o600)
			mutate(t, filepath.Join(root, "owner", "private.png"))
			manifest, err := ListWithPrivate(public, privateCatalog, root)
			if err != nil {
				t.Fatal(err)
			}
			if findGenerated(t, manifest, "fixture_owner_private_001").Availability != "unavailable" {
				t.Fatal("unsafe private fixture accepted")
			}
		})
	}
}

func loadRepositoryCatalog(t *testing.T) Catalog {
	t.Helper()
	catalog, err := LoadCatalog(filepath.Join(repositoryRoot(t), "scripts", "fixtures", "evaluation", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func generateRepositoryCorpus(t *testing.T) (string, string, GeneratedManifest) {
	t.Helper()
	catalog := filepath.Join(repositoryRoot(t), "scripts", "fixtures", "evaluation", "catalog.json")
	work := t.TempDir()
	output := filepath.Join(work, "fixtures")
	manifestPath := filepath.Join(work, "manifest.json")
	if err := Generate(catalog, output, manifestPath); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest GeneratedManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	return catalog, output, manifest
}

func differentPixels(first, second image.Image) int {
	if first.Bounds() != second.Bounds() {
		return 0
	}
	different := 0
	for y := first.Bounds().Min.Y; y < first.Bounds().Max.Y; y++ {
		for x := first.Bounds().Min.X; x < first.Bounds().Max.X; x++ {
			if first.At(x, y) != second.At(x, y) {
				different++
			}
		}
	}
	return different
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func privateFixtureSet(t *testing.T, canary string, mode os.FileMode) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	public := filepath.Join(repositoryRoot(t), "scripts", "fixtures", "evaluation", "catalog.json")
	privateDir := filepath.Join(root, "owner")
	if err := os.Mkdir(privateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(privateDir, "private.png")
	if err := os.WriteFile(path, []byte(canary), mode); err != nil {
		t.Fatal(err)
	}
	sum := sha256Hex([]byte(canary))
	privateCatalog := filepath.Join(root, "private-catalog.json")
	body, err := json.Marshal(PrivateCatalog{
		Format:  CatalogFormat,
		Version: CatalogVersion,
		Fixtures: []PrivateFixture{{
			ID:              "fixture_owner_private_001",
			Privacy:         PrivacyOwnerPrivate,
			MediaType:       "image/png",
			PageIDs:         []string{"page_owner_private_001"},
			Languages:       []string{"ja"},
			DocumentType:    "owner-text",
			ExpectedOutputs: []ArtifactRole{RoleOCRText, RolePDF},
			RelativePath:    "owner/private.png",
			SHA256:          sum,
			Bytes:           int64(len(canary)),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(privateCatalog, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return public, privateCatalog, root
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func findGenerated(t *testing.T, manifest GeneratedManifest, id string) GeneratedFixture {
	t.Helper()
	for _, fixture := range manifest.Fixtures {
		if fixture.ID == id {
			return fixture
		}
	}
	t.Fatalf("fixture %s missing", id)
	return GeneratedFixture{}
}

func testFixture(id string) FixtureDefinition {
	return FixtureDefinition{
		ID:              id,
		SourceClass:     "public_synthetic",
		Privacy:         PrivacyPublicSynthetic,
		MediaType:       "image/png",
		PageIDs:         []string{"page_a4_ja_en_001"},
		Languages:       []string{"ja", "en"},
		DocumentType:    "a4-text",
		ExpectedOutputs: []ArtifactRole{RoleOCRText, RolePositionedText, RolePDF},
		SHA256:          "e2a8b58d65452c9a7e4b471481e2149c9d14502cc75b1c48120ceef9bc860e08",
		Bytes:           69624,
		ExpectedText:    []string{"日本語の書類", "Scanner OCR 2026", "テスト文書 300 dpi"},
		Recipe: Recipe{
			Kind:   "copy",
			Source: "ocr-ja-en-300dpi.png",
		},
	}
}
