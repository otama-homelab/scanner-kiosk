package evaluation

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"scanner-kiosk/pkg/ocrplugin"
	"strings"
	"testing"
)

func hashBytes(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func ocrPageTest() (OCRPageOracle, ocrplugin.PositionedPage) {
	o := OCRPageOracle{PageID: "page_ocr_test_001", SourceSHA256: hashBytes([]byte("source")), Width: 100, Height: 100, Spans: []OCROracleSpan{{Text: "日本", Order: 0, Box: [4]float64{10, 10, 30, 10}, Cell: "r1c1"}, {Text: "123", Order: 1, Box: [4]float64{10, 30, 30, 10}, Cell: "r2c1"}}}
	p := ocrplugin.PositionedPage{PageID: o.PageID, Width: 100, Height: 100, Spans: []ocrplugin.Span{{Text: "日本", Box: ocrplugin.Box{X: 10, Y: 10, Width: 30, Height: 10}}, {Text: "123", Box: ocrplugin.Box{X: 10, Y: 30, Width: 30, Height: 10}}}}
	return o, p
}
func TestOCRAnalysis(t *testing.T) {
	o, p := ocrPageTest()
	q, err := AnalyzeOCRPage(o, "日本123", p)
	if err != nil || q.Status != "passed" || q.ReferenceCodePoints != 5 || q.TextErrors != 0 || q.MatchedCells != 2 || q.MatchedRegions != 2 {
		t.Fatalf("expected independent complete quality, got %+v %v", q, err)
	}
	p.Spans[0], p.Spans[1] = p.Spans[1], p.Spans[0]
	q, err = AnalyzeOCRPage(o, "123日本", p)
	if err != nil || q.ReadingOrderInversions != 1 || q.Status != "failed" {
		t.Fatalf("order inversion not measured: %+v %v", q, err)
	}
}
func TestOCRMissingOutput(t *testing.T) {
	o, p := ocrPageTest()
	if _, err := AnalyzeOCRPage(o, "", ocrplugin.PositionedPage{}); err == nil {
		t.Fatal("missing recognition accepted")
	}
	for _, v := range []float64{math.NaN(), -1, 101} {
		p.Spans[0].Box.X = v
		if _, err := AnalyzeOCRPage(o, "日本123", p); err == nil {
			t.Fatal("bad source box accepted")
		}
	}
	o.Spans = nil
	o.Blank = true
	p.Spans = nil
	p.Width = 100
	p.Height = 100
	q, err := AnalyzeOCRPage(o, "", p)
	if err != nil || q.Status != "passed" {
		t.Fatalf("explicit blank artifact rejected: %+v %v", q, err)
	}
}
func TestOCRUnicodeUnits(t *testing.T) {
	o, p := ocrPageTest()
	o.Spans = o.Spans[:1]
	o.Spans[0].Text = "が１"
	p.Spans = p.Spans[:1]
	p.Spans[0].Text = "か\u3099１"
	q, err := AnalyzeOCRPage(o, "か\u3099１", p)
	if err != nil || q.TextErrors != 0 || q.ReferenceCodePoints != 2 {
		t.Fatalf("NFC code points: %+v %v", q, err)
	}
	q, err = AnalyzeOCRPage(o, "が1", p)
	if err != nil || q.TextErrors != 1 {
		t.Fatalf("fullwidth digit was folded: %+v %v", q, err)
	}
}

func ocrEvidenceTest(t *testing.T) (string, string, OCRRunManifest) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, value any) OCRFileRef {
		t.Helper()
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return ocrWriteBytes(t, root, name, body)
	}
	o, p := ocrPageTest()
	oracle := OCROracleFile{Format: "scanner-eval-ocr-oracles", Version: 1, Normalization: "NFC", Units: "unicode-code-points", Entries: []OCROracleEntry{{FixtureID: "fixture_ocr_test_001", FixtureSHA256: o.SourceSHA256, Layout: "horizontal", Pages: []OCRPageOracle{o}}}}
	oracle.Font.Name = "font"
	oracle.Font.SHA256 = hashBytes([]byte("font"))
	oracle.Font.License = "test-only"
	oracle.Font.Source = "synthetic"
	oracleRef := write("oracle.json", oracle)
	manifest := OCRRunManifest{Format: "scanner-eval-ocr-run-manifest", Version: 1, EvidenceRoot: root, OracleSHA256: oracleRef.SHA256}
	for _, engine := range []string{"tesseract-5.5.4", "ndlocr-lite-1.3.0"} {
		pair := OCRManifestPair{EngineID: engine, FixtureID: oracle.Entries[0].FixtureID, PageSources: []OCRFileRef{ocrWriteBytes(t, root, engine+"/page.png", []byte("source"))}}
		for _, kind := range []string{"executable", "source", "model", "dictionary", "font", "tool"} {
			ref := ocrWriteBytes(t, root, engine+"/"+kind, []byte(kind))
			pair.Identities = append(pair.Identities, OCRIdentity{Kind: kind, Name: kind, Version: "test-only", License: "test-only", File: ref})
		}
		config := CandidateConfig{Format: CandidateConfigFormat, Version: 1, CandidateID: engine, CandidateVersion: "test-only", Executable: filepath.Join(root, engine, "executable"), ExecutableSHA256: pair.Identities[0].File.SHA256, Sources: []CandidatePin{{Name: "source", Version: "test-only", License: "test-only", SHA256: pair.Identities[1].File.SHA256}}, Dependencies: []CandidatePin{{Name: "model", Version: "test-only", License: "test-only", SHA256: pair.Identities[2].File.SHA256}}, OperatorSteps: []string{"synthetic validator unit fixture"}, Parameters: map[string]string{"mode": "test-only"}}
		pair.CandidateConfig = write(engine+"/config.json", config)
		candidate := CandidateReceipt{ID: config.CandidateID, Version: config.CandidateVersion, ExecutableSHA256: config.ExecutableSHA256, Sources: config.Sources, Dependencies: config.Dependencies, OperatorSteps: config.OperatorSteps, Parameters: config.Parameters}
		run := func(dir string) OCRFileRef {
			original := Artifact{ID: "artifact_original_001", Role: RoleOriginal, MediaType: "image/png", SourcePages: []string{o.PageID}}
			b := ocrWriteBytes(t, root, dir+"/artifacts/"+original.ID, []byte("source"))
			original.SHA256 = b.SHA256
			original.Bytes = 6
			text := Artifact{ID: "artifact_text_00001", Role: RoleOCRText, MediaType: "text/plain; charset=utf-8", SourcePages: []string{o.PageID}, Parents: []string{original.ID}}
			b = ocrWriteBytes(t, root, dir+"/artifacts/"+text.ID, []byte("日本123"))
			text.SHA256 = b.SHA256
			text.Bytes = int64(len("日本123"))
			pos := Artifact{ID: "artifact_position_001", Role: RolePositionedText, MediaType: "application/json", SourcePages: []string{o.PageID}, Parents: []string{original.ID}}
			b = write(dir+"/artifacts/"+pos.ID, p)
			pos.SHA256 = b.SHA256
			body, _ := json.Marshal(p)
			pos.Bytes = int64(len(body))
			pdf := Artifact{ID: "artifact_pdf_000001", Role: RolePDF, MediaType: "application/pdf", SourcePages: []string{o.PageID}, Parents: []string{original.ID}}
			b = ocrWriteBytes(t, root, dir+"/artifacts/"+pdf.ID, []byte("%PDF-unit-test-only"))
			pdf.SHA256 = b.SHA256
			pdf.Bytes = 19
			return write(dir+"/artifact-catalog.json", ArtifactCatalog{Format: ArtifactCatalogFormat, Version: 1, RunID: "run_ocr_test_0001", Status: "complete", FixtureID: pair.FixtureID, FixtureSHA256: o.SourceSHA256, Candidate: candidate, CandidateConfigSHA256: pair.CandidateConfig.SHA256, Original: &original, Artifacts: []Artifact{text, pos, pdf}, Validation: &ValidationReceipt{Format: ValidationFormat, Version: 1, Status: "passed", PDFSHA256: pdf.SHA256, Pages: []PDFPageEvidence{{PageID: o.PageID, Index: 1}}, Checks: []ValidationCheck{{Name: "source-page-text-binding", Status: "pass"}}}})
		}
		pair.Standalone = run(engine + "/standalone")
		settings := BenchmarkSettings{ColdRepetitions: 1, WarmRepetitions: 2, TimeoutSeconds: 20, MaxRSSBytes: 1 << 20, MaxTempDiskBytes: 1 << 20}
		benchmark := BenchmarkSummary{Format: BenchmarkFormat, Version: 1, Status: "complete", FixtureID: pair.FixtureID, FixtureSHA256: o.SourceSHA256, Candidate: candidate, CandidateConfigSHA256: pair.CandidateConfig.SHA256, Settings: settings, Observer: ObserverEvidence{SHA256: pair.Identities[5].File.SHA256, License: "test-only"}}
		for i, mode := range []string{"cold", "warm", "warm"} {
			dir := fmt.Sprintf("repetitions/%03d-%s", i+1, mode)
			pair.SampleCatalogs = append(pair.SampleCatalogs, run(engine+"/benchmark/"+dir))
			sample := BenchmarkSample{SampleID: fmt.Sprintf("sample_%03d", i+1), Sequence: i + 1, Mode: mode, FixtureID: pair.FixtureID, FixtureSHA256: o.SourceSHA256, Candidate: candidate, CandidateConfigSHA256: pair.CandidateConfig.SHA256, WallMillis: 10, PeakRSSBytes: 1000, SampledTempDiskBytes: 100, TempDiskSampled: true, Status: "complete", ValidationStatus: "passed", ArtifactCatalog: dir + "/artifact-catalog.json", ReceiptPath: fmt.Sprintf("receipts/%03d.json", i+1), SourceHashesBefore: map[string]string{"fixture": o.SourceSHA256, "candidate_config": pair.CandidateConfig.SHA256, "executable": candidate.ExecutableSHA256}, SourceHashesAfter: map[string]string{"fixture": o.SourceSHA256, "candidate_config": pair.CandidateConfig.SHA256, "executable": candidate.ExecutableSHA256}}
			benchmark.Samples = append(benchmark.Samples, sample)
			pair.Samples = append(pair.Samples, write(engine+"/benchmark/"+sample.ReceiptPath, sample))
		}
		pair.Benchmark = write(engine+"/benchmark/benchmark.json", benchmark)
		manifest.Pairs = append(manifest.Pairs, pair)
	}
	write("manifest.json", manifest)
	return filepath.Join(root, "oracle.json"), filepath.Join(root, "manifest.json"), manifest
}
func ocrWriteBytes(t *testing.T, root, name string, body []byte) OCRFileRef {
	t.Helper()
	filename := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, body, 0600); err != nil {
		t.Fatal(err)
	}
	return OCRFileRef{Path: name, SHA256: hashBytes(body)}
}
func ocrRewriteManifest(t *testing.T, name string, value OCRRunManifest) {
	t.Helper()
	body, _ := json.Marshal(value)
	if err := os.WriteFile(name, body, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestOCRComparisonEvidenceGate(t *testing.T) {
	oracle, manifest, _ := ocrEvidenceTest(t)
	q, err := AnalyzeOCREvidence(oracle, manifest, []string{"tesseract-5.5.4", "ndlocr-lite-1.3.0"}, true)
	if err != nil || q.Status != "complete" || len(q.Rows) != 8 {
		t.Fatalf("complete synthetic evidence validator failed: %+v %v", q, err)
	}
	for _, row := range q.Rows {
		if len(row.Pages) != 1 || row.Pages[0].Status != "passed" || row.Resources == nil || row.Resources.Cold.Samples != 1 || row.Resources.Warm.Samples != 2 || !validHash(row.CatalogSHA256) {
			t.Fatalf("page quality missing: %+v", row)
		}
	}
	body, _ := json.Marshal(q)
	if strings.Contains(string(body), filepath.Dir(manifest)) || strings.Contains(string(body), "日本") {
		t.Fatal("private path or OCR transcript leaked into comparison")
	}
}

func TestOCRComparisonMeasuredFailure(t *testing.T) {
	oracle, manifest, m := ocrEvidenceTest(t)
	file := filepath.Join(m.EvidenceRoot, m.Pairs[0].Standalone.Path)
	body, _ := os.ReadFile(file)
	var c ArtifactCatalog
	json.Unmarshal(body, &c)
	for i := range c.Artifacts {
		if c.Artifacts[i].Role == RoleOCRText {
			text := []byte("日米123")
			os.WriteFile(filepath.Join(filepath.Dir(file), "artifacts", c.Artifacts[i].ID), text, 0600)
			c.Artifacts[i].Bytes = int64(len(text))
			c.Artifacts[i].SHA256 = hashBytes(text)
		}
	}
	body, _ = json.Marshal(c)
	os.WriteFile(file, body, 0600)
	m.Pairs[0].Standalone.SHA256 = hashBytes(body)
	m.Pairs[0].Identities[2].License = "unresolved"
	ocrRewriteManifest(t, manifest, m)
	q, err := AnalyzeOCREvidence(oracle, manifest, []string{"tesseract-5.5.4", "ndlocr-lite-1.3.0"}, true)
	if err != nil || q.Status != "complete" || q.Rows[0].Pages[0].Status != "failed" || q.Rows[0].LicenseStatus != "unresolved" {
		t.Fatalf("truthful quality/license failure confused with evidence coverage: %+v %v", q, err)
	}
}
func TestOCRComparisonMissingEngine(t *testing.T) {
	oracle, manifest, m := ocrEvidenceTest(t)
	m.Pairs = m.Pairs[:1]
	ocrRewriteManifest(t, manifest, m)
	if _, err := AnalyzeOCREvidence(oracle, manifest, []string{"tesseract-5.5.4", "ndlocr-lite-1.3.0"}, true); err == nil {
		t.Fatal("missing engine accepted")
	}
}
func TestOCRResourceIdentity(t *testing.T) {
	for _, kind := range []string{"config", "model", "sample", "fixture", "artifact", "root", "verdict", "receipt", "engine", "oracle", "sample-catalog", "page-source", "missing-fixture"} {
		t.Run(kind, func(t *testing.T) {
			oracle, manifest, m := ocrEvidenceTest(t)
			switch kind {
			case "config":
				m.Pairs[0].CandidateConfig.SHA256 = hashBytes([]byte("wrong"))
			case "model":
				m.Pairs[0].Identities[2].File.SHA256 = hashBytes([]byte("wrong"))
			case "sample":
				m.Pairs[0].Samples = m.Pairs[0].Samples[:2]
			case "fixture":
				m.Pairs[0].FixtureID = "fixture_unknown_001"
			case "artifact":
				os.WriteFile(filepath.Join(m.EvidenceRoot, "tesseract-5.5.4/standalone/artifacts/artifact_text_00001"), []byte("tampered"), 0600)
			case "root":
				m.EvidenceRoot = filepath.Join(m.EvidenceRoot, "missing")
			case "verdict":
				file := filepath.Join(m.EvidenceRoot, m.Pairs[0].Standalone.Path)
				body, _ := os.ReadFile(file)
				var c ArtifactCatalog
				json.Unmarshal(body, &c)
				c.Validation.Status = ""
				body, _ = json.Marshal(c)
				os.WriteFile(file, body, 0600)
				m.Pairs[0].Standalone.SHA256 = hashBytes(body)
			case "receipt":
				os.WriteFile(filepath.Join(m.EvidenceRoot, m.Pairs[0].Samples[0].Path), []byte("{}"), 0600)
			case "engine":
				m.Pairs[0].EngineID = "wrong-engine"
			case "oracle":
				m.OracleSHA256 = hashBytes([]byte("wrong"))
			case "sample-catalog":
				m.Pairs[0].SampleCatalogs[0].SHA256 = hashBytes([]byte("wrong"))
			case "page-source":
				m.Pairs[0].PageSources[0].SHA256 = hashBytes([]byte("wrong"))
			case "missing-fixture":
				body, _ := os.ReadFile(oracle)
				var o OCROracleFile
				json.Unmarshal(body, &o)
				entry := o.Entries[0]
				entry.FixtureID = "fixture_missing_001"
				o.Entries = append(o.Entries, entry)
				body, _ = json.Marshal(o)
				os.WriteFile(oracle, body, 0600)
				m.OracleSHA256 = hashBytes(body)
			}
			ocrRewriteManifest(t, manifest, m)
			if _, err := AnalyzeOCREvidence(oracle, manifest, []string{"tesseract-5.5.4", "ndlocr-lite-1.3.0"}, true); err == nil {
				t.Fatal("mismatched evidence accepted")
			}
		})
	}
}

func TestOCRPDFAnalyzerRechecksEvidence(t *testing.T) {
	oracle, manifest, _ := ocrEvidenceTest(t)
	q, err := AnalyzeOCREvidence(oracle, manifest, []string{"tesseract-5.5.4", "ndlocr-lite-1.3.0"}, true)
	if err != nil || q.Status != "complete" {
		t.Fatalf("synthetic evidence incomplete: %v", err)
	}
	for _, row := range q.Rows {
		if row.PDFStatus != "failed" {
			t.Fatal("trusted candidate receipt for malformed PDF instead of independent validation")
		}
	}
}
