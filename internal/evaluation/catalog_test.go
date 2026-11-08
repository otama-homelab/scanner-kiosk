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
	if err := Generate(catalog, filepath.Join(work, "fixtures"), filepath.Join(work, "manifest.json")); err != nil {
		t.Fatal(err)
	}
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
