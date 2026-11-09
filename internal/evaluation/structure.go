package evaluation

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"unicode/utf8"
)

const structureCandidate = "docling-cpu-2.149.0"
const structureOracleSHA = "e5f91c00ff449274611b24fcfbfe431188b82903cd11ba94188f9152de4f435c"
const structureCatalogSHA = "2d9b0f17fe3802ec8284c23c17d78e5e36896ae0cb6a027a2fe2e980fbba0abc"
const structureBaselineManifestSHA = "0ffb6c24b6504d228852f43551ae5d2ecb846022433d348ce93df86ff93a040d"
const structureBaselineComparisonSHA = "63cc28a655ce140da3cb11b5d455d9bc8188871db527a592901e3bf2a25ee0e2"
const structureDispositionSHA = "2dca9c05676811b9f25d7fc2e939b743a664dab73183b27b4803140d0c895d19"

type structureDisposition struct {
	Format      string `json:"format"`
	Version     int    `json:"version"`
	CandidateID string `json:"candidate_id"`
	Status      string `json:"status"`
	Stage       string `json:"stage"`
	Reason      string `json:"reason"`
	Inputs      struct {
		CatalogSHA256             string   `json:"catalog_sha256"`
		OracleSHA256              string   `json:"oracle_sha256"`
		FixtureID                 string   `json:"fixture_id"`
		FixtureSHA256             string   `json:"fixture_sha256"`
		SourcePages               []string `json:"source_pages"`
		Source                    string   `json:"source"`
		Tesseract                 string   `json:"tesseract"`
		TesseractSHA256           string   `json:"tesseract_sha256"`
		RetainedRuntimeLockSHA256 string   `json:"retained_runtime_lock_sha256"`
	} `json:"inputs"`
	Evidence struct {
		Command              []string        `json:"command"`
		ExitCode             int             `json:"exit_code"`
		Stderr               string          `json:"stderr"`
		References           []OCRFileRef    `json:"references"`
		WeightsAcquired      *bool           `json:"weights_acquired"`
		Installed            *bool           `json:"installed"`
		InferenceStarted     *bool           `json:"inference_started"`
		ResourceObservations json.RawMessage `json:"resource_observations"`
	} `json:"evidence"`
	Fallback               *bool             `json:"fallback"`
	QualityMeasured        *bool             `json:"quality_measured"`
	ResourcesMeasured      *bool             `json:"resources_measured"`
	Adoption               string            `json:"adoption"`
	SourceCommit           string            `json:"source_commit"`
	Packages               map[string]string `json:"packages"`
	Models                 [][]string        `json:"models"`
	Configuration          json.RawMessage   `json:"configuration"`
	Limits                 json.RawMessage   `json:"limits"`
	PackageLegitimacyAudit string            `json:"package_legitimacy_audit"`
}

func structureGap(b OCRComparison) ([]OCRPageQuality, error) {
	if b.Format != "scanner-eval-ocr-comparison" || b.Version != 1 || b.Status != "complete" || b.Normalization != "NFC" || b.Units != "unicode-code-points" || b.OracleSHA256 != structureOracleSHA || b.ManifestSHA256 != structureBaselineManifestSHA {
		return nil, ErrInvalid
	}
	var gap []OCRPageQuality
	seen := map[string]bool{}
	for _, r := range b.Rows {
		if r.FixtureID != "fixture_ocr_table_001" || r.Mode != "standalone" {
			continue
		}
		if (r.EngineID != "tesseract-5.5.4" && r.EngineID != "ndlocr-lite-1.3.0") || seen[r.EngineID] || len(r.Pages) != 1 {
			return nil, ErrInvalid
		}
		p := r.Pages[0]
		if p.PageID != "page_ocr_table_001" || p.MatchedCells != 3 || p.Cells != 4 || p.Status != "failed" {
			return nil, ErrInvalid
		}
		seen[r.EngineID] = true
		gap = append(gap, p)
	}
	if len(gap) != 2 {
		return nil, ErrInvalid
	}
	return gap, nil
}
func structureAttempts(m StructureManifest, candidate string) error {
	if candidate != structureCandidate || len(m.Attempts) != 1 || m.Attempts[0].CandidateID != candidate || !safeRelative(m.Attempts[0].Disposition.Path) || m.Attempts[0].Disposition.SHA256 != structureDispositionSHA {
		return ErrInvalid
	}
	return nil
}
func validateStructureFailure(body []byte) (structureDisposition, error) {
	var d structureDisposition
	falseFlag := func(v *bool) bool { return v != nil && !*v }
	if len(body) > 4<<20 || decodeOCR(body, &d) != nil || d.Format != "scanner-eval-structure-disposition" || d.Version != 1 || d.CandidateID != structureCandidate || (d.Status != "unavailable" && d.Status != "failed") || d.Stage != "isolation" || !boundedText(d.Reason, 1024) || d.SourceCommit != "8103d817983eeaaa2234d110072fa70d5c59a075" || !falseFlag(d.Fallback) || !falseFlag(d.QualityMeasured) || !falseFlag(d.ResourcesMeasured) || d.Adoption != "defer" || !falseFlag(d.Evidence.Installed) || !falseFlag(d.Evidence.WeightsAcquired) || !falseFlag(d.Evidence.InferenceStarted) || !bytes.Equal(d.Evidence.ResourceObservations, []byte("null")) || d.Evidence.ExitCode != 32 || len(d.Evidence.Command) < 12 || d.Evidence.Command[0] != "/usr/bin/unshare" || !strings.Contains(d.Evidence.Stderr, "mount:") || !strings.Contains(d.Evidence.Stderr, "/usr:") || len(d.Evidence.Stderr) > 16384 || len(d.Evidence.References) != 6 || d.PackageLegitimacyAudit != "SUS (research heuristic retained; exact official provenance checked separately)" {
		return d, ErrInvalid
	}
	packages := map[string]string{"docling-slim": "2.149.0", "docling-core": "2.106.0", "docling-ibm-models": "4.0.4"}
	models := [][]string{
		{"docling-models", "fc0f2d45e2218ea24bce5045f58a389aed16dc23", "model_artifacts/tableformer/accurate/tableformer_accurate.safetensors", "2a7d6c924b3cd12fb99a09280ca9c33a89c5d60b93253617d2e088c1a40374d9"},
		{"docling-models", "fc0f2d45e2218ea24bce5045f58a389aed16dc23", "model_artifacts/tableformer/accurate/tm_config.json", "984e122ceb8ccf84d84c9d2882f6f2302a44b4f1e577babd6289892c36f3cffd"},
		{"docling-layout-heron", "8f39ad3c0b4c58e9c2d2c84a38465abf757272d8", "model.safetensors", "00333a43451945aaf89db8ca9c0a17e75d1537c17db60fdb91aa95f4c7929e0c"},
		{"docling-layout-heron", "8f39ad3c0b4c58e9c2d2c84a38465abf757272d8", "config.json", "fdea30805ce2f5666b147fca941dcdd27ad468e27d6ed21902207d3da056a97d"},
		{"docling-layout-heron", "8f39ad3c0b4c58e9c2d2c84a38465abf757272d8", "preprocessor_config.json", "cd38cd59999e7a95d68e487fbe5132df3d4e5c32a0836add57e6126ba0c4eaf1"},
	}
	config := `{"device":"cpu","threads":2,"table_mode":"accurate","ocr":"tesseract-5.5.4","languages":["jpn","eng"],"pages_per_process":1,"remote":false,"enrichment":false}`
	limits := `{"timeout_seconds":900,"max_rss_bytes":2147483648,"max_temp_disk_bytes":1073741824}`
	if !reflect.DeepEqual(d.Packages, packages) || !reflect.DeepEqual(d.Models, models) || !sameStructureJSON(d.Configuration, []byte(config)) || !sameStructureJSON(d.Limits, []byte(limits)) || d.Inputs.CatalogSHA256 != structureCatalogSHA || d.Inputs.OracleSHA256 != structureOracleSHA || d.Inputs.FixtureID != "fixture_ocr_table_001" || d.Inputs.FixtureSHA256 != "d388859643e6e629b0a0a4f182b54ff47b79285e27783f4ed1d2f68620a8b416" || !reflect.DeepEqual(d.Inputs.SourcePages, []string{"page_ocr_table_001"}) || !filepath.IsAbs(d.Inputs.Source) || !filepath.IsAbs(d.Inputs.Tesseract) || d.Inputs.TesseractSHA256 != "c284a3a76e6231c1d01b27f2d775f078535a82fbd952c6e777358e9bbe685ea2" || d.Inputs.RetainedRuntimeLockSHA256 != "90a85ea007c391d5e627ed9fb52753cb769f68cf523700c7124b3327d6e0a14b" {
		return d, ErrInvalid
	}
	return d, nil
}

func sameStructureJSON(a, b []byte) bool {
	var left, right any
	return decodeOCR(a, &left) == nil && decodeOCR(b, &right) == nil && reflect.DeepEqual(left, right)
}

// StructureSource is the immutable single-page source emitted by the Docling adapter.
type StructureSource struct {
	ArtifactID string `json:"artifact_id"`
	SHA256     string `json:"sha256"`
	PageID     string `json:"page_id"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
}
type StructureBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
type StructureBlock struct {
	ID             string        `json:"id"`
	PageID         string        `json:"page_id"`
	ParentID       *string       `json:"parent_id"`
	Kind           string        `json:"kind"`
	Order          int           `json:"order"`
	Text           *string       `json:"text"`
	Box            *StructureBox `json:"box"`
	GeometryStatus string        `json:"geometry_status"`
	Rows           *int          `json:"rows,omitempty"`
	Columns        *int          `json:"columns,omitempty"`
	CellsStatus    string        `json:"cells_status,omitempty"`
}
type StructureCell struct {
	ID             string        `json:"id"`
	TableID        string        `json:"table_id"`
	PageID         string        `json:"page_id"`
	Row            int           `json:"row"`
	Column         int           `json:"column"`
	RowSpan        int           `json:"row_span"`
	ColumnSpan     int           `json:"column_span"`
	Box            *StructureBox `json:"box"`
	GeometryStatus string        `json:"geometry_status"`
	Text           string        `json:"text"`
}
type StructurePayload struct {
	Format            string           `json:"format"`
	Version           int              `json:"version"`
	CandidateID       string           `json:"candidate_id"`
	RuntimeLockSHA256 string           `json:"runtime_lock_sha256"`
	Source            StructureSource  `json:"source"`
	Blocks            []StructureBlock `json:"blocks"`
	Cells             []StructureCell  `json:"cells"`
}
type GeneratedReference struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	PageID string `json:"page_id"`
}
type GeneratedRecord struct {
	Category   string               `json:"category"`
	Content    string               `json:"content"`
	References []GeneratedReference `json:"references"`
}
type GeneratedPayload struct {
	Format           string            `json:"format"`
	Version          int               `json:"version"`
	Role             ArtifactRole      `json:"role"`
	SourceRevision   string            `json:"source_revision"`
	SourceArtifactID string            `json:"source_artifact_id"`
	SourceSHA256     string            `json:"source_sha256"`
	StructureSHA256  string            `json:"structure_sha256"`
	Algorithm        string            `json:"algorithm"`
	AlgorithmVersion string            `json:"algorithm_version"`
	Records          []GeneratedRecord `json:"records"`
}
type StructureAttempt struct {
	CandidateID string     `json:"candidate_id"`
	Disposition OCRFileRef `json:"disposition"`
}
type StructureManifest struct {
	Format             string             `json:"format"`
	Version            int                `json:"version"`
	EvidenceRoot       string             `json:"evidence_root"`
	BaselineRoot       string             `json:"baseline_root"`
	BaselineComparison OCRFileRef         `json:"baseline_comparison"`
	BaselineManifest   OCRFileRef         `json:"baseline_manifest"`
	Attempts           []StructureAttempt `json:"attempts"`
}
type StructureComparison struct {
	Format                   string             `json:"format"`
	Version                  int                `json:"version"`
	Status                   string             `json:"status"`
	CandidateID              string             `json:"candidate_id"`
	ManifestSHA256           string             `json:"manifest_sha256"`
	DispositionSHA256        string             `json:"disposition_sha256"`
	OracleSHA256             string             `json:"oracle_sha256"`
	CatalogSHA256            string             `json:"catalog_sha256"`
	BaselineComparisonSHA256 string             `json:"baseline_comparison_sha256"`
	BaselineManifestSHA256   string             `json:"baseline_manifest_sha256"`
	FailureStage             string             `json:"failure_stage"`
	FailureExitCode          int                `json:"failure_exit_code"`
	Decision                 string             `json:"decision"`
	Adopted                  bool               `json:"adopted"`
	Rows                     []OCRComparisonRow `json:"rows"`
	Measurements             map[string]string  `json:"measurements"`
	BaselineGap              []OCRPageQuality   `json:"baseline_gap"`
}

func ValidateStructurePayload(body []byte, source StructureSource, lock string) (StructurePayload, error) {
	var s StructurePayload
	if len(body) > 4<<20 || !utf8.Valid(body) || decodeOCR(body, &s) != nil || s.Format != "scanner-eval-structure" || s.Version != 1 || s.CandidateID != structureCandidate || !validHash(lock) || s.RuntimeLockSHA256 != lock || s.Source != source || !ValidID(source.ArtifactID) || !ValidID(source.PageID) || !validHash(source.SHA256) || source.Width < 1 || source.Height < 1 || source.Width > 40000000/source.Height || s.Blocks == nil || s.Cells == nil || len(s.Blocks) > 4096 || len(s.Cells) > 8192 {
		return s, ErrInvalid
	}
	ids := map[string]StructureBlock{}
	textBytes := 0
	for i, b := range s.Blocks {
		if !structureBlockID.MatchString(b.ID) || b.PageID != source.PageID || b.Order != i || !boundedToken(b.Kind, 96) || !structureGeometry(b.Box, b.GeometryStatus, source) {
			return s, ErrInvalid
		}
		if _, ok := ids[b.ID]; ok {
			return s, ErrInvalid
		}
		ids[b.ID] = b
		if b.Text != nil {
			if !utf8.ValidString(*b.Text) || len(*b.Text) > 65536 {
				return s, ErrInvalid
			}
			textBytes += len(*b.Text)
		}
		isTable := strings.HasPrefix(b.ID, "#/tables/")
		if !isTable && (b.Rows != nil || b.Columns != nil || b.CellsStatus != "") {
			return s, ErrInvalid
		}
		if isTable && ((b.Rows == nil) != (b.Columns == nil) || b.CellsStatus != "available" && b.CellsStatus != "unavailable") {
			return s, ErrInvalid
		}
		if b.Rows != nil && (*b.Rows < 0 || *b.Rows > 512 || *b.Columns < 0 || *b.Columns > 512 || *b.Rows**b.Columns > 65536) {
			return s, ErrInvalid
		}
	}
	for _, b := range s.Blocks {
		seen := map[string]bool{b.ID: true}
		parent := b.ParentID
		for depth := 0; parent != nil; depth++ {
			p, ok := ids[*parent]
			if !ok || seen[*parent] || depth >= 32 {
				return s, ErrInvalid
			}
			seen[*parent] = true
			parent = p.ParentID
		}
	}
	occupied := map[string]map[[2]int]bool{}
	occupiedSlots := 0
	cellIDs := map[string]bool{}
	counts := map[string]int{}
	for _, c := range s.Cells {
		table, ok := ids[c.TableID]
		if !ok || !strings.HasPrefix(c.TableID, "#/tables/") || table.Rows == nil || table.Columns == nil || !strings.HasPrefix(c.ID, c.TableID+"/cells/") || !structureCellID.MatchString(c.ID) || cellIDs[c.ID] || c.PageID != source.PageID || c.Row < 0 || c.Column < 0 || c.RowSpan < 1 || c.ColumnSpan < 1 || c.RowSpan > 512 || c.ColumnSpan > 512 || c.Row > *table.Rows-c.RowSpan || c.Column > *table.Columns-c.ColumnSpan || !structureGeometry(c.Box, c.GeometryStatus, source) || !utf8.ValidString(c.Text) || len(c.Text) > 65536 {
			return s, ErrInvalid
		}
		cellIDs[c.ID] = true
		counts[c.TableID]++
		textBytes += len(c.Text)
		occupiedSlots += c.RowSpan * c.ColumnSpan
		if occupiedSlots > 65536 {
			return s, ErrInvalid
		}
		if occupied[c.TableID] == nil {
			occupied[c.TableID] = map[[2]int]bool{}
		}
		for r := c.Row; r < c.Row+c.RowSpan; r++ {
			for column := c.Column; column < c.Column+c.ColumnSpan; column++ {
				slot := [2]int{r, column}
				if occupied[c.TableID][slot] {
					return s, ErrInvalid
				}
				occupied[c.TableID][slot] = true
			}
		}
	}
	for _, b := range s.Blocks {
		if strings.HasPrefix(b.ID, "#/tables/") && (b.CellsStatus == "available") != (counts[b.ID] > 0) {
			return s, ErrInvalid
		}
	}
	if textBytes > 2<<20 {
		return s, ErrInvalid
	}
	return s, nil
}

var structureBlockID = regexp.MustCompile(`^#/(texts|tables|pictures|groups)/[0-9]+$`)
var structureCellID = regexp.MustCompile(`^#/tables/[0-9]+/cells/[0-9]+$`)

func structureGeometry(b *StructureBox, status string, s StructureSource) bool {
	if b == nil {
		return status == "unavailable"
	}
	if status != "available" {
		return false
	}
	for _, v := range []float64{b.X, b.Y, b.Width, b.Height} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return b.X >= 0 && b.Y >= 0 && b.Width > 0 && b.Height > 0 && b.X+b.Width <= float64(s.Width) && b.Y+b.Height <= float64(s.Height)
}
func ValidateGeneratedPayload(body []byte, revision string, structure StructurePayload, structureHash string) (GeneratedPayload, error) {
	var g GeneratedPayload
	structureBody, err := json.Marshal(structure)
	if err != nil {
		return g, ErrInvalid
	}
	if _, err := ValidateStructurePayload(structureBody, structure.Source, structure.RuntimeLockSHA256); err != nil {
		return g, ErrInvalid
	}
	if len(body) > 4<<20 || !utf8.Valid(body) || decodeOCR(body, &g) != nil || g.Format != "scanner-eval-generated" || g.Version != 1 || g.Role != RoleGenerated || !boundedToken(revision, 128) || g.SourceRevision != revision || g.SourceArtifactID != structure.Source.ArtifactID || g.SourceSHA256 != structure.Source.SHA256 || !validHash(structureHash) || g.StructureSHA256 != structureHash || !boundedToken(g.Algorithm, 96) || !boundedToken(g.AlgorithmVersion, 128) || len(g.Records) < 1 || len(g.Records) > 128 {
		return g, ErrInvalid
	}
	blocks := map[string]bool{}
	cells := map[string]bool{}
	for _, b := range structure.Blocks {
		blocks[b.ID] = true
	}
	for _, c := range structure.Cells {
		cells[c.ID] = true
	}
	textBytes := 0
	for _, r := range g.Records {
		if r.Category != "summary" && r.Category != "classification" && r.Category != "filename" || !boundedText(r.Content, 65536) || len(r.References) < 1 || len(r.References) > 128 {
			return g, ErrInvalid
		}
		textBytes += len(r.Content)
		seen := map[string]bool{}
		for _, ref := range r.References {
			key := ref.Kind + "\x00" + ref.ID
			if ref.PageID != structure.Source.PageID || seen[key] {
				return g, ErrInvalid
			}
			seen[key] = true
			switch ref.Kind {
			case "page":
				if ref.ID != structure.Source.PageID {
					return g, ErrInvalid
				}
			case "block":
				if !blocks[ref.ID] {
					return g, ErrInvalid
				}
			case "cell":
				if !cells[ref.ID] {
					return g, ErrInvalid
				}
			default:
				return g, ErrInvalid
			}
		}
	}
	if textBytes > 2<<20 {
		return g, ErrInvalid
	}
	return g, nil
}
func ReadStructureArtifact(rootPath string, ref OCRFileRef, artifact Artifact, source StructureSource, lock string) (StructurePayload, error) {
	root, err := openStructureRoot(rootPath)
	if err != nil {
		return StructurePayload{}, ErrInvalid
	}
	defer root.Close()
	body, err := readStructureEvidence(root, ref, 4<<20)
	if err != nil || artifact.Role != RoleStructure || artifact.MediaType != "application/json" || artifact.SHA256 != ref.SHA256 || artifact.Bytes != int64(len(body)) || !ValidID(artifact.ID) || artifact.Algorithm != "docling-cpu" || artifact.AlgorithmVersion != "2.149.0" || !reflect.DeepEqual(artifact.SourcePages, []string{source.PageID}) || !reflect.DeepEqual(artifact.Parents, []string{source.ArtifactID}) {
		return StructurePayload{}, ErrInvalid
	}
	return ValidateStructurePayload(body, source, lock)
}

// ReadGeneratedArtifact reopens both roles; caller supplies the authorized immutable snapshot.
func ReadGeneratedArtifact(rootPath string, ref OCRFileRef, artifact Artifact, revision string, structureRef OCRFileRef, structureArtifact Artifact, source StructureSource, lock string) (GeneratedPayload, error) {
	structure, err := ReadStructureArtifact(rootPath, structureRef, structureArtifact, source, lock)
	if err != nil {
		return GeneratedPayload{}, ErrInvalid
	}
	root, err := openStructureRoot(rootPath)
	if err != nil {
		return GeneratedPayload{}, ErrInvalid
	}
	defer root.Close()
	body, err := readStructureEvidence(root, ref, 4<<20)
	if err != nil || artifact.Role != RoleGenerated || artifact.MediaType != "application/json" || artifact.SHA256 != ref.SHA256 || artifact.Bytes != int64(len(body)) || !ValidID(artifact.ID) || !reflect.DeepEqual(artifact.SourcePages, []string{source.PageID}) || !reflect.DeepEqual(artifact.Parents, []string{structureArtifact.ID}) {
		return GeneratedPayload{}, ErrInvalid
	}
	g, err := ValidateGeneratedPayload(body, revision, structure, structureRef.SHA256)
	if err != nil || artifact.Algorithm != g.Algorithm || artifact.AlgorithmVersion != g.AlgorithmVersion {
		return GeneratedPayload{}, ErrInvalid
	}
	return g, nil
}

func openStructureRoot(path string) (*os.Root, error) {
	info, err := os.Lstat(path)
	if err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || !info.IsDir() || !structurePrivate(info) {
		return nil, ErrInvalid
	}
	return os.OpenRoot(path)
}
func structurePrivate(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid()) && info.Mode().Perm()&0077 == 0
}
func readStructureEvidence(root *os.Root, ref OCRFileRef, limit int64) ([]byte, error) {
	info, err := root.Lstat(ref.Path)
	if err != nil || !info.Mode().IsRegular() || !structurePrivate(info) {
		return nil, ErrInvalid
	}
	return readOCREvidence(root, ref, limit)
}
func AnalyzeStructureEvidence(oraclePath, catalogPath, manifestPath, candidate string, complete, unavailable bool) (StructureComparison, error) {
	out := StructureComparison{Format: "scanner-eval-structure-comparison", Version: 1, Rows: []OCRComparisonRow{}}
	if !complete || !unavailable || candidate != structureCandidate {
		return out, ErrInvalid
	}
	oracleSHA, err := hashRegularFile(oraclePath, 4<<20)
	if err != nil || oracleSHA != structureOracleSHA {
		return out, ErrInvalid
	}
	catalogSHA, err := hashRegularFile(catalogPath, 4<<20)
	if err != nil || catalogSHA != structureCatalogSHA {
		return out, ErrInvalid
	}
	if _, err := LoadOCROracles(oraclePath); err != nil {
		return out, ErrInvalid
	}
	manifestRoot, err := openStructureRoot(filepath.Dir(manifestPath))
	if err != nil {
		return out, ErrInvalid
	}
	defer manifestRoot.Close()
	info, err := manifestRoot.Lstat(filepath.Base(manifestPath))
	if err != nil || !info.Mode().IsRegular() || !structurePrivate(info) {
		return out, ErrInvalid
	}
	body, err := readCorrectionRootFile(manifestRoot, filepath.Base(manifestPath), 4<<20)
	var m StructureManifest
	if err != nil || decodeOCR(body, &m) != nil || m.Format != "scanner-eval-structure-manifest" || m.Version != 1 || structureAttempts(m, candidate) != nil {
		return out, ErrInvalid
	}
	out.ManifestSHA256 = hexSHA256(body)
	root, err := openStructureRoot(m.EvidenceRoot)
	if err != nil {
		return out, ErrInvalid
	}
	defer root.Close()
	baselineRoot, err := openStructureRoot(m.BaselineRoot)
	if err != nil {
		return out, ErrInvalid
	}
	defer baselineRoot.Close()
	if m.BaselineComparison.SHA256 != structureBaselineComparisonSHA || m.BaselineManifest.SHA256 != structureBaselineManifestSHA {
		return out, ErrInvalid
	}
	baselineBody, err := readOCREvidence(baselineRoot, m.BaselineComparison, 16<<20)
	var baseline OCRComparison
	if err != nil || decodeOCR(baselineBody, &baseline) != nil {
		return out, ErrInvalid
	}
	gap, err := structureGap(baseline)
	if err != nil {
		return out, ErrInvalid
	}
	if _, err := readOCREvidence(baselineRoot, m.BaselineManifest, 4<<20); err != nil {
		return out, ErrInvalid
	}
	dispositionBody, err := readStructureEvidence(root, m.Attempts[0].Disposition, 4<<20)
	if err != nil {
		return out, ErrInvalid
	}
	d, err := validateStructureFailure(dispositionBody)
	if err != nil {
		return out, ErrInvalid
	}
	refs := map[string][]byte{}
	for _, ref := range d.Evidence.References {
		if _, ok := refs[ref.Path]; ok {
			return out, ErrInvalid
		}
		b, err := readOCREvidence(root, ref, 8<<20)
		if err != nil {
			return out, ErrInvalid
		}
		refs[ref.Path] = b
	}
	for _, name := range []string{"attempt-tools/prepare_candidate.py", "attempt-tools/candidate.py", "inputs.json", "wheel-inspections.json", "requirements.lock", "isolation-placement-reprobe.json"} {
		if _, ok := refs[name]; !ok {
			return out, ErrInvalid
		}
	}
	var inputs = d.Inputs
	if decodeOCR(refs["inputs.json"], &inputs) != nil || !reflect.DeepEqual(inputs, d.Inputs) {
		return out, ErrInvalid
	}
	var probe struct {
		Command  []string `json:"command"`
		ExitCode int      `json:"exit_code"`
		Stdout   string   `json:"stdout"`
		Stderr   string   `json:"stderr"`
	}
	if decodeOCR(refs["isolation-placement-reprobe.json"], &probe) != nil || !reflect.DeepEqual(probe.Command, d.Evidence.Command) || probe.ExitCode != d.Evidence.ExitCode || probe.Stderr != d.Evidence.Stderr || probe.Stdout != "" {
		return out, ErrInvalid
	}
	sourceRelative := "corpus/page_ocr_table_001.png"
	if d.Inputs.Source != filepath.Join(m.BaselineRoot, sourceRelative) {
		return out, ErrInvalid
	}
	if _, err := readOCREvidence(baselineRoot, OCRFileRef{Path: sourceRelative, SHA256: d.Inputs.FixtureSHA256}, 32<<20); err != nil {
		return out, ErrInvalid
	}
	out.Status = "unavailable"
	out.CandidateID = candidate
	out.DispositionSHA256 = m.Attempts[0].Disposition.SHA256
	out.OracleSHA256 = oracleSHA
	out.CatalogSHA256 = catalogSHA
	out.BaselineComparisonSHA256 = m.BaselineComparison.SHA256
	out.BaselineManifestSHA256 = m.BaselineManifest.SHA256
	out.FailureStage = d.Stage
	out.FailureExitCode = d.Evidence.ExitCode
	out.Decision = "defer"
	out.BaselineGap = gap
	out.Measurements = map[string]string{}
	for _, dimension := range []string{"text_errors", "reading_order", "matched_regions", "matched_cells", "row_column_topology", "geometry", "elapsed_time", "peak_rss", "sampled_temp_disk", "model_license_closure", "installed_dependency_closure", "generated_quality"} {
		out.Measurements[dimension] = "unresolved"
	}
	return out, nil
}
