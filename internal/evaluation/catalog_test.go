package evaluation

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func loadRepositoryCatalog(t *testing.T) Catalog {
	t.Helper()
	catalog, err := LoadCatalog(filepath.Join(repositoryRoot(t), "scripts", "fixtures", "evaluation", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
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
