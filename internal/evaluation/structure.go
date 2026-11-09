package evaluation

import "encoding/json"

const structureCandidate = "docling-cpu-2.149.0"
const structureOracleSHA = "e5f91c00ff449274611b24fcfbfe431188b82903cd11ba94188f9152de4f435c"
const structureCatalogSHA = "2d9b0f17fe3802ec8284c23c17d78e5e36896ae0cb6a027a2fe2e980fbba0abc"
const structureBaselineManifestSHA = "0ffb6c24b6504d228852f43551ae5d2ecb846022433d348ce93df86ff93a040d"
const structureBaselineComparisonSHA = "63cc28a655ce140da3cb11b5d455d9bc8188871db527a592901e3bf2a25ee0e2"

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
	Models                 [][4]string       `json:"models"`
	Configuration          json.RawMessage   `json:"configuration"`
	Limits                 json.RawMessage   `json:"limits"`
	PackageLegitimacyAudit string            `json:"package_legitimacy_audit"`
}

func structureGap(b OCRComparison) ([]OCRPageQuality, error)        { return nil, ErrUnavailable }
func structureAttempts(m StructureManifest, candidate string) error { return ErrUnavailable }
func validateStructureFailure(body []byte) (structureDisposition, error) {
	return structureDisposition{}, ErrUnavailable
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
	return StructurePayload{}, ErrUnavailable
}
func ValidateGeneratedPayload(body []byte, revision string, structure StructurePayload, structureHash string) (GeneratedPayload, error) {
	return GeneratedPayload{}, ErrUnavailable
}
func ReadStructureArtifact(rootPath string, ref OCRFileRef, artifact Artifact, source StructureSource, lock string) (StructurePayload, error) {
	return StructurePayload{}, ErrUnavailable
}
func AnalyzeStructureEvidence(oraclePath, catalogPath, manifestPath, candidate string, complete, unavailable bool) (StructureComparison, error) {
	return StructureComparison{}, ErrUnavailable
}
