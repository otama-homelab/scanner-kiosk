package evaluation

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	a := Artifact{ID: "artifact_structure_001", Role: RoleGenerated, MediaType: "application/json", SHA256: ref.SHA256, Bytes: int64(len(body)), SourcePages: []string{s.Source.PageID}, Parents: []string{s.Source.ArtifactID}, Algorithm: "docling", AlgorithmVersion: "2.149.0"}
	if _, err := ReadStructureArtifact(root, ref, a, s.Source, s.RuntimeLockSHA256); err == nil {
		t.Fatal("generated role supplied structure")
	}
	a.Role = RoleStructure
	if _, err := ReadStructureArtifact(root, ref, a, s.Source, s.RuntimeLockSHA256); err != nil {
		t.Fatalf("hash-bound structure rejected: %v", err)
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
	a := Artifact{ID: "artifact_structure_001", Role: RoleStructure, MediaType: "application/json", SHA256: ref.SHA256, Bytes: int64(len(body)), SourcePages: []string{s.Source.PageID}, Parents: []string{s.Source.ArtifactID}, Algorithm: "docling", AlgorithmVersion: "2.149.0"}
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
