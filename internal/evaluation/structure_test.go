package evaluation

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStructureGapRequiresMeasuredEvidence(t *testing.T) {
	b := OCRComparison{Format: "scanner-eval-ocr-comparison", Version: 1, Status: "complete", OracleSHA256: structureOracleSHA, ManifestSHA256: structureBaselineManifestSHA, Normalization: "NFC", Units: "unicode-code-points", Rows: []OCRComparisonRow{{EngineID: "tesseract-5.5.4", FixtureID: "fixture_ocr_table_001", Mode: "standalone", Pages: []OCRPageQuality{{PageID: "page_ocr_table_001", MatchedCells: 3, Cells: 4, ReadingOrderInversions: 12, Status: "failed"}}}, {EngineID: "ndlocr-lite-1.3.0", FixtureID: "fixture_ocr_table_001", Mode: "standalone", Pages: []OCRPageQuality{{PageID: "page_ocr_table_001", MatchedCells: 3, Cells: 4, Status: "failed"}}}}}
	if gap, err := structureGap(b); err != nil || len(gap) != 2 {
		t.Fatalf("measured 3/4 table gap rejected: %+v %v", gap, err)
	}
	b.Status = "incomplete"
	if _, err := structureGap(b); err == nil {
		t.Fatal("incomplete baseline accepted")
	}
	b.Status = "complete"
	b.Rows[0].Pages[0].MatchedCells = 4
	if _, err := structureGap(b); err == nil {
		t.Fatal("unproven gap accepted")
	}
}
func TestStructureSingleCandidateCompleteness(t *testing.T) {
	a := StructureAttempt{CandidateID: structureCandidate, Disposition: OCRFileRef{Path: "disposition.json", SHA256: structureDispositionSHA}}
	m := StructureManifest{Attempts: []StructureAttempt{a}}
	if err := structureAttempts(m, structureCandidate); err != nil {
		t.Fatalf("one exact unavailable attempt rejected: %v", err)
	}
	for _, attempts := range [][]StructureAttempt{nil, {a, a}, {{CandidateID: "second-candidate", Disposition: a.Disposition}}, {{CandidateID: structureCandidate}}} {
		m.Attempts = attempts
		if err := structureAttempts(m, structureCandidate); err == nil {
			t.Fatal("missing/mixed/extra candidate accepted")
		}
	}
	if err := structureAttempts(StructureManifest{Attempts: []StructureAttempt{a}}, "other"); err == nil {
		t.Fatal("candidate requirement ignored")
	}
}
func TestStructureUnavailablePreservesIndependentAnalysis(t *testing.T) {
	// Unsupported successful evidence cannot authorize an unavailable comparison.
	for _, raw := range []string{`{"format":"scanner-eval-structure-disposition","version":1,"status":"complete"}`, `{"format":"scanner-eval-structure-disposition","version":1,"status":"unavailable","quality_measured":true}`, `{"format":"scanner-eval-structure-disposition","version":1,"status":"unavailable","adoption":"adopt"}`} {
		if _, err := validateStructureFailure([]byte(raw)); err == nil {
			t.Fatal("invented measurements or adoption accepted")
		}
	}
	s, body := structureFixture(t)
	if _, err := ValidateStructurePayload(body, s.Source, s.RuntimeLockSHA256); err != nil {
		t.Fatalf("independent schema unavailable because inference is unavailable: %v", err)
	}
	for _, flags := range [][2]bool{{false, false}, {false, true}, {true, false}} {
		if _, err := AnalyzeStructureEvidence("missing", "missing", "missing", structureCandidate, flags[0], flags[1]); err == nil {
			t.Fatal("missing complete/unavailable gate accepted")
		}
	}
	// Boundary fixtures prove completeness only, never real candidate measurements.
	oracle, err := LoadOCROracles(filepath.Join("..", "..", "scripts", "fixtures", "evaluation", "ocr-oracles.json"))
	if err != nil {
		t.Fatal(err)
	}
	baseline := OCRComparison{}
	for _, engine := range []string{"tesseract-5.5.4", "ndlocr-lite-1.3.0"} {
		for _, entry := range oracle.Entries {
			for _, mode := range []string{"standalone", "cold", "warm", "warm"} {
				r := OCRComparisonRow{EngineID: engine, FixtureID: entry.FixtureID, FixtureSHA256: entry.FixtureSHA256, CandidateConfigSHA256: hashBytes([]byte(engine)), Mode: mode, ResourceStatus: "measured"}
				if mode == "standalone" {
					r.ResourceStatus = "not-measured"
				}
				for _, p := range entry.Pages {
					r.Pages = append(r.Pages, OCRPageQuality{PageID: p.PageID, TextErrors: 7, MatchedRegions: 0, Regions: len(p.Spans), Status: "failed"})
				}
				baseline.Rows = append(baseline.Rows, r)
			}
		}
	}
	rows, err := structureBaselineRows(baseline, oracle)
	if err != nil || len(rows) != 16 {
		t.Fatalf("unavailable candidate must retain all 16 measured baseline layout rows: got %d %v", len(rows), err)
	}
	for _, row := range rows {
		if row.Pages[0].TextErrors != 7 || row.CandidateConfigSHA256 == "" {
			t.Fatal("baseline independent dimensions lost")
		}
	}
	for _, mutate := range []func(*OCRComparison){func(b *OCRComparison) { b.Rows = b.Rows[:len(b.Rows)-1] }, func(b *OCRComparison) { b.Rows[0].FixtureSHA256 = hashBytes([]byte("drift")) }, func(b *OCRComparison) { b.Rows[0].CandidateConfigSHA256 = hashBytes([]byte("changed configuration")) }, func(b *OCRComparison) { b.Rows[1].ResourceStatus = "not-measured" }, func(b *OCRComparison) { b.Rows[0].Pages = nil }, func(b *OCRComparison) { b.Rows[0].EngineID = "extra-candidate" }} {
		raw, _ := json.Marshal(baseline)
		var corrupt OCRComparison
		_ = json.Unmarshal(raw, &corrupt)
		mutate(&corrupt)
		if _, err := structureBaselineRows(corrupt, oracle); err == nil {
			t.Fatal("incomplete or drifted baseline coverage accepted")
		}
	}
}

func structureFixture(t *testing.T) (StructurePayload, []byte) {
	t.Helper()
	text, parent, rows, columns := "日本123", "#/groups/0", 2, 2
	s := StructurePayload{Format: "scanner-eval-structure", Version: 1, CandidateID: "docling-cpu-2.149.0", RuntimeLockSHA256: hashBytes([]byte("runtime")), Source: StructureSource{ArtifactID: "artifact_source_001", SHA256: hashBytes([]byte("source")), PageID: "page_ocr_test_001", Width: 100, Height: 100}, Blocks: []StructureBlock{
		{ID: "#/groups/0", PageID: "page_ocr_test_001", Kind: "group", Order: 0, GeometryStatus: "unavailable"},
		{ID: "#/tables/0", ParentID: &parent, PageID: "page_ocr_test_001", Kind: "table", Order: 1, Text: &text, Box: &StructureBox{10, 10, 60, 60}, GeometryStatus: "available", Rows: &rows, Columns: &columns, CellsStatus: "available"},
	}, Cells: []StructureCell{{ID: "#/tables/0/cells/0", TableID: "#/tables/0", PageID: "page_ocr_test_001", Row: 0, Column: 0, RowSpan: 1, ColumnSpan: 2, Text: "日本", Box: &StructureBox{10, 10, 60, 20}, GeometryStatus: "available"}}}
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return s, body
}
func generatedFixture(t *testing.T, s StructurePayload, sha string) GeneratedPayload {
	t.Helper()
	return GeneratedPayload{Format: "scanner-eval-generated", Version: 1, Role: RoleGenerated, SourceRevision: "revision_001", SourceArtifactID: s.Source.ArtifactID, SourceSHA256: s.Source.SHA256, StructureSHA256: sha, Algorithm: "synthetic-boundary-test", AlgorithmVersion: "1", Records: []GeneratedRecord{{Category: "filename", Content: "日本123", References: []GeneratedReference{{Kind: "page", ID: s.Source.PageID, PageID: s.Source.PageID}, {Kind: "block", ID: s.Blocks[1].ID, PageID: s.Source.PageID}, {Kind: "cell", ID: s.Cells[0].ID, PageID: s.Source.PageID}}}}}
}
func TestStructurePageBlockCellReferences(t *testing.T) {
	s, body := structureFixture(t)
	if _, err := ValidateStructurePayload(body, s.Source, s.RuntimeLockSHA256); err != nil {
		t.Fatalf("valid observed page/block/cell topology rejected: %v", err)
	}
	for _, mutation := range []func(*StructurePayload){
		func(v *StructurePayload) { v.Source.SHA256 = hashBytes([]byte("stale")) },
		func(v *StructurePayload) { v.RuntimeLockSHA256 = hashBytes([]byte("other runtime")) },
		func(v *StructurePayload) { v.Blocks[1].PageID = "page_foreign_001" },
		func(v *StructurePayload) { x := v.Blocks[1].ID; v.Blocks[0].ParentID = &x },
		func(v *StructurePayload) { x := "missing"; v.Blocks[0].ParentID = &x },
		func(v *StructurePayload) { v.Blocks[1].Box.X = 99 },
		func(v *StructurePayload) { v.Blocks[1].Order = 0 },
		func(v *StructurePayload) { v.Cells[0].RowSpan = 3 },
		func(v *StructurePayload) { v.Cells[0].TableID = v.Blocks[0].ID },
		func(v *StructurePayload) { v.Cells = append(v.Cells, v.Cells[0]) },
		func(v *StructurePayload) {
			v.Cells[0].ID = "#/tables/0/cells/1"
			v.Cells = append(v.Cells, StructureCell{ID: "#/tables/0/cells/2", TableID: "#/tables/0", PageID: s.Source.PageID, Row: 0, Column: 1, RowSpan: 1, ColumnSpan: 1, GeometryStatus: "unavailable"})
		},
		func(v *StructurePayload) { v.Blocks[1].CellsStatus = "unavailable" },
	} {
		_, raw := structureFixture(t)
		var v StructurePayload
		_ = json.Unmarshal(raw, &v)
		mutation(&v)
		b, _ := json.Marshal(v)
		if _, err := ValidateStructurePayload(b, s.Source, s.RuntimeLockSHA256); err == nil {
			t.Fatalf("corrupt topology accepted: %s", b)
		}
	}
	var bad StructurePayload
	_ = json.Unmarshal(body, &bad)
	bad.Blocks[1].Box.X = math.NaN()
	if _, err := json.Marshal(bad); err == nil {
		t.Fatal("nonfinite geometry serialized")
	}
}
func TestGeneratedContentSeparateFromTranscript(t *testing.T) {
	s, body := structureFixture(t)
	g := generatedFixture(t, s, hashBytes(body))
	for _, category := range []string{"summary", "classification", "filename"} {
		g.Records[0].Category = category
		b, _ := json.Marshal(g)
		if _, err := ValidateGeneratedPayload(b, "revision_001", s, hashBytes(body)); err != nil {
			t.Fatalf("valid separate generated category rejected: %v", err)
		}
		if _, err := ValidateStructurePayload(b, s.Source, s.RuntimeLockSHA256); err == nil {
			t.Fatal("generated record supplied structure completeness")
		}
	}
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	ref := ocrWriteBytes(t, root, "structure.json", body)
	a := Artifact{ID: "artifact_structure_001", Role: RoleGenerated, MediaType: "application/json", SHA256: ref.SHA256, Bytes: int64(len(body)), SourcePages: []string{s.Source.PageID}, Parents: []string{s.Source.ArtifactID}, Algorithm: "docling-cpu", AlgorithmVersion: "2.149.0"}
	if _, err := ReadStructureArtifact(root, ref, a, s.Source, s.RuntimeLockSHA256); err == nil {
		t.Fatal("generated role supplied structure")
	}
	a.Role = RoleStructure
	if _, err := ReadStructureArtifact(root, ref, a, s.Source, s.RuntimeLockSHA256); err != nil {
		t.Fatalf("hash-bound structure rejected: %v", err)
	}
	g = generatedFixture(t, s, ref.SHA256)
	generatedBody, _ := json.Marshal(g)
	generatedRef := ocrWriteBytes(t, root, "generated.json", generatedBody)
	ga := Artifact{ID: "artifact_generated_001", Role: RoleGenerated, MediaType: "application/json", SHA256: generatedRef.SHA256, Bytes: int64(len(generatedBody)), SourcePages: []string{s.Source.PageID}, Parents: []string{a.ID}, Algorithm: g.Algorithm, AlgorithmVersion: g.AlgorithmVersion}
	if _, err := ReadGeneratedArtifact(root, generatedRef, ga, "revision_001", ref, a, s.Source, s.RuntimeLockSHA256); err != nil {
		t.Fatalf("reopened generated and structure rejected: %v", err)
	}
	ga.Role = RoleOCRText
	if _, err := ReadGeneratedArtifact(root, generatedRef, ga, "revision_001", ref, a, s.Source, s.RuntimeLockSHA256); err == nil {
		t.Fatal("generated artifact masqueraded as transcript")
	}
	ga.Role = RoleGenerated
	_ = os.WriteFile(filepath.Join(root, "generated.json"), []byte("changed"), 0600)
	if _, err := ReadGeneratedArtifact(root, generatedRef, ga, "revision_001", ref, a, s.Source, s.RuntimeLockSHA256); err == nil {
		t.Fatal("changed generated bytes accepted")
	}
	for _, role := range []ArtifactRole{RoleOCRText, RolePositionedText, RolePDF} {
		a.Role = role
		if _, err := ReadStructureArtifact(root, ref, a, s.Source, s.RuntimeLockSHA256); err == nil {
			t.Fatal("transcript/PDF role supplied structure")
		}
	}
}
func TestGeneratedReferencesRejectInvalid(t *testing.T) {
	s, body := structureFixture(t)
	for _, mutate := range []func(*GeneratedPayload){
		func(g *GeneratedPayload) { g.SourceRevision = "stale" }, func(g *GeneratedPayload) { g.SourceSHA256 = hashBytes([]byte("foreign")) }, func(g *GeneratedPayload) { g.SourceArtifactID = "artifact_foreign_001" }, func(g *GeneratedPayload) { g.StructureSHA256 = hashBytes([]byte("stale structure")) }, func(g *GeneratedPayload) { g.Algorithm = "" }, func(g *GeneratedPayload) { g.AlgorithmVersion = "" }, func(g *GeneratedPayload) { g.Role = RoleOCRText }, func(g *GeneratedPayload) { g.Records[0].Category = "transcript" }, func(g *GeneratedPayload) { g.Records[0].References = nil }, func(g *GeneratedPayload) { g.Records[0].References[1].ID = "missing" }, func(g *GeneratedPayload) { g.Records[0].References[2].PageID = "page_foreign_001" }, func(g *GeneratedPayload) { g.Records[0].References[0].Kind = "generated" }, func(g *GeneratedPayload) { g.Records[0].Content = strings.Repeat("x", 65537) },
	} {
		g := generatedFixture(t, s, hashBytes(body))
		mutate(&g)
		b, _ := json.Marshal(g)
		if _, err := ValidateGeneratedPayload(b, "revision_001", s, hashBytes(body)); err == nil {
			t.Fatalf("invalid generated reference accepted: %s", b)
		}
	}
}
func TestStructureEvidenceGate(t *testing.T) {
	s, body := structureFixture(t)
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	ref := ocrWriteBytes(t, root, "structure.json", body)
	a := Artifact{ID: "artifact_structure_001", Role: RoleStructure, MediaType: "application/json", SHA256: ref.SHA256, Bytes: int64(len(body)), SourcePages: []string{s.Source.PageID}, Parents: []string{s.Source.ArtifactID}, Algorithm: "docling-cpu", AlgorithmVersion: "2.149.0"}
	if _, err := ReadStructureArtifact(root, ref, a, s.Source, s.RuntimeLockSHA256); err != nil {
		t.Fatalf("private hash-bound structure rejected: %v", err)
	}
	for _, raw := range [][]byte{append(append([]byte{}, body...), []byte(" {}")...), []byte(strings.Replace(string(body), `"version":1`, `"version":1,"unknown":true`, 1))} {
		if _, err := ValidateStructurePayload(raw, s.Source, s.RuntimeLockSHA256); err == nil {
			t.Fatal("unknown/trailing JSON accepted")
		}
	}
	_ = os.WriteFile(filepath.Join(root, "structure.json"), []byte("changed"), 0600)
	if _, err := ReadStructureArtifact(root, ref, a, s.Source, s.RuntimeLockSHA256); err == nil {
		t.Fatal("changed bytes accepted")
	}
	_ = os.WriteFile(filepath.Join(root, "structure.json"), body, 0644)
	_ = os.Chmod(filepath.Join(root, "structure.json"), 0644)
	if _, err := ReadStructureArtifact(root, ref, a, s.Source, s.RuntimeLockSHA256); err == nil {
		t.Fatal("public evidence file accepted")
	}
	_ = os.Remove(filepath.Join(root, "structure.json"))
	_ = os.Symlink("/etc/passwd", filepath.Join(root, "structure.json"))
	if _, err := ReadStructureArtifact(root, ref, a, s.Source, s.RuntimeLockSHA256); err == nil {
		t.Fatal("symlink escape accepted")
	}
}
