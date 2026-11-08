package evaluation

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

const (
	CorrectionOracleFormat  = "scanner-eval-correction-oracles"
	CorrectionCompareFormat = "scanner-eval-correction-comparison"
	CorrectionFormatVersion = 1
)

type CorrectionPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type CorrectionTransform struct {
	CropQuad        [4]CorrectionPoint `json:"crop_quad"`
	RotationDegrees float64            `json:"rotation_degrees"`
	OutputWidth     int                `json:"output_width"`
	OutputHeight    int                `json:"output_height"`
	ConfidenceKind  string             `json:"confidence_kind"`
	ConfidenceValue *float64           `json:"confidence_value,omitempty"`
	FallbackReason  string             `json:"fallback_reason,omitempty"`
}

type CorrectionRegion struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type CorrectionOracleEntry struct {
	FixtureID      string             `json:"fixture_id"`
	FixtureSHA256  string             `json:"fixture_sha256"`
	BackgroundRGB  [3]uint8           `json:"background_rgb"`
	EdgeRegions    []CorrectionRegion `json:"edge_regions,omitempty"`
	ContentRegions []CorrectionRegion `json:"content_regions,omitempty"`
}

type CorrectionOracleFile struct {
	Format  string                  `json:"format"`
	Version int                     `json:"version"`
	Entries []CorrectionOracleEntry `json:"entries"`
}

type CorrectionQuality struct {
	Status                 string  `json:"status"`
	TransformValid         bool    `json:"transform_valid"`
	EdgeRegions            int     `json:"edge_regions"`
	EdgeRegionsRetained    int     `json:"edge_regions_retained"`
	ContentRegions         int     `json:"content_regions"`
	ContentRegionsRetained int     `json:"content_regions_retained"`
	OriginalForeground     int64   `json:"original_foreground_pixels"`
	CorrectionForeground   int64   `json:"correction_foreground_pixels"`
	ForegroundRetention    float64 `json:"foreground_retention_ratio"`
	CorrectionWidth        int     `json:"correction_width"`
	CorrectionHeight       int     `json:"correction_height"`
}

type CorrectionComparisonRow struct {
	FixtureID         string              `json:"fixture_id"`
	FixtureSHA256     string              `json:"fixture_sha256"`
	CandidateID       string              `json:"candidate_id"`
	CandidateVersion  string              `json:"candidate_version"`
	OriginalSHA256    string              `json:"original_sha256"`
	CorrectionID      string              `json:"correction_id"`
	CorrectionSHA256  string              `json:"correction_sha256"`
	MeasurementStatus string              `json:"measurement_status"`
	Status            string              `json:"status"`
	FailureClass      string              `json:"failure_class,omitempty"`
	Transform         CorrectionTransform `json:"transform"`
	Quality           CorrectionQuality   `json:"quality"`
}

type CorrectionComparison struct {
	Format  string                    `json:"format"`
	Version int                       `json:"version"`
	Status  string                    `json:"status"`
	Rows    []CorrectionComparisonRow `json:"rows"`
}

var RequiredCorrectionOracleFixtures = []string{
	"fixture_a4_ja_en_001",
	"fixture_card_001",
	"fixture_receipt_001",
	"fixture_table_form_001",
	"fixture_vertical_text_001",
	"fixture_faint_print_001",
	"fixture_skew_001",
	"fixture_shadow_001",
	"fixture_white_on_white_001",
}

func ValidateCorrectionOracleCoverage(catalog Catalog, oracles CorrectionOracleFile) error {
	if err := catalog.Validate(); err != nil {
		return err
	}
	if err := oracles.Validate(); err != nil {
		return err
	}
	fixtures := map[string]string{}
	for _, fixture := range catalog.Fixtures {
		fixtures[fixture.ID] = fixture.SHA256
	}
	seen := map[string]bool{}
	for _, oracle := range oracles.Entries {
		sha, ok := fixtures[oracle.FixtureID]
		if !ok || sha == "" || sha != oracle.FixtureSHA256 {
			return ErrInvalid
		}
		seen[oracle.FixtureID] = true
	}
	for _, fixtureID := range RequiredCorrectionOracleFixtures {
		if !seen[fixtureID] {
			return ErrInvalid
		}
	}
	return nil
}

func LoadCorrectionOracles(path string) (CorrectionOracleFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return CorrectionOracleFile{}, ErrInvalid
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4<<20))
	decoder.DisallowUnknownFields()
	var oracles CorrectionOracleFile
	if err := decoder.Decode(&oracles); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return CorrectionOracleFile{}, ErrInvalid
	}
	if err := oracles.Validate(); err != nil {
		return CorrectionOracleFile{}, err
	}
	return oracles, nil
}

func (o CorrectionOracleFile) Validate() error {
	if o.Format != CorrectionOracleFormat || o.Version != CorrectionFormatVersion ||
		len(o.Entries) == 0 || len(o.Entries) > 128 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, entry := range o.Entries {
		if !ValidID(entry.FixtureID) || seen[entry.FixtureID] || !validHash(entry.FixtureSHA256) ||
			len(entry.EdgeRegions) > 64 || len(entry.ContentRegions) > 64 {
			return ErrInvalid
		}
		for _, region := range append(append([]CorrectionRegion(nil), entry.EdgeRegions...), entry.ContentRegions...) {
			if region.X < 0 || region.Y < 0 || region.Width < 1 || region.Height < 1 {
				return ErrInvalid
			}
		}
		seen[entry.FixtureID] = true
	}
	return nil
}

func ParseCorrectionTransform(parameters map[string]string, original image.Rectangle) (CorrectionTransform, error) {
	var transform CorrectionTransform
	quadValue := parameters["crop_quad"]
	if quadValue == "" {
		return transform, ErrInvalid
	}
	var quad [4][2]float64
	decoder := json.NewDecoder(io.LimitReader(bytes.NewBufferString(quadValue), 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&quad); err != nil {
		return transform, ErrInvalid
	}
	for index := range quad {
		transform.CropQuad[index] = CorrectionPoint{X: quad[index][0], Y: quad[index][1]}
		if !finite(transform.CropQuad[index].X) || !finite(transform.CropQuad[index].Y) ||
			transform.CropQuad[index].X < float64(original.Min.X) ||
			transform.CropQuad[index].Y < float64(original.Min.Y) ||
			transform.CropQuad[index].X > float64(original.Max.X) ||
			transform.CropQuad[index].Y > float64(original.Max.Y) {
			return CorrectionTransform{}, ErrInvalid
		}
	}
	rotation, err := strconv.ParseFloat(parameters["rotation_degrees"], 64)
	if err != nil || !finite(rotation) || rotation < -45 || rotation > 45 {
		return CorrectionTransform{}, ErrInvalid
	}
	width, err := strconv.Atoi(parameters["output_width"])
	if err != nil || width < 1 || width > 100000 {
		return CorrectionTransform{}, ErrInvalid
	}
	height, err := strconv.Atoi(parameters["output_height"])
	if err != nil || height < 1 || height > 100000 {
		return CorrectionTransform{}, ErrInvalid
	}
	if !validCorrectionQuad(transform.CropQuad) {
		return CorrectionTransform{}, ErrInvalid
	}
	kind := parameters["confidence_kind"]
	if !boundedText(kind, 64) {
		return CorrectionTransform{}, ErrInvalid
	}
	if value := parameters["confidence_value"]; value != "" {
		confidence, err := strconv.ParseFloat(value, 64)
		if err != nil || !finite(confidence) || confidence < -1000 || confidence > 1000 {
			return CorrectionTransform{}, ErrInvalid
		}
		transform.ConfidenceValue = &confidence
	} else if kind != "unavailable" {
		return CorrectionTransform{}, ErrInvalid
	}
	if !boundedOptionalText(parameters["fallback_reason"], 256) {
		return CorrectionTransform{}, ErrInvalid
	}
	transform.RotationDegrees = rotation
	transform.OutputWidth = width
	transform.OutputHeight = height
	transform.ConfidenceKind = kind
	transform.FallbackReason = parameters["fallback_reason"]
	return transform, nil
}

func AnalyzeCorrectionCatalogs(paths []string, oracles CorrectionOracleFile) (CorrectionComparison, error) {
	if len(paths) == 0 || oracles.Validate() != nil {
		return CorrectionComparison{}, ErrInvalid
	}
	comparison := CorrectionComparison{Format: CorrectionCompareFormat, Version: CorrectionFormatVersion, Status: "complete"}
	for _, path := range paths {
		row := analyzeCorrectionCatalog(path, oracles)
		if row.Status != "passed" || row.MeasurementStatus != "complete" {
			comparison.Status = "incomplete"
		}
		comparison.Rows = append(comparison.Rows, row)
	}
	sort.Slice(comparison.Rows, func(i, j int) bool {
		if comparison.Rows[i].FixtureID != comparison.Rows[j].FixtureID {
			return comparison.Rows[i].FixtureID < comparison.Rows[j].FixtureID
		}
		return comparison.Rows[i].CandidateID < comparison.Rows[j].CandidateID
	})
	return comparison, nil
}

func analyzeCorrectionCatalog(path string, oracles CorrectionOracleFile) CorrectionComparisonRow {
	row := CorrectionComparisonRow{MeasurementStatus: "incomplete", Status: "failed"}
	catalog, err := loadArtifactCatalog(path)
	if err != nil {
		row.FailureClass = "artifact-catalog"
		return row
	}
	row.FixtureID, row.FixtureSHA256 = catalog.FixtureID, catalog.FixtureSHA256
	row.CandidateID, row.CandidateVersion = catalog.Candidate.ID, catalog.Candidate.Version
	row.MeasurementStatus, row.OriginalSHA256 = catalog.Status, catalog.Original.SHA256
	var oracle *CorrectionOracleEntry
	for index := range oracles.Entries {
		if oracles.Entries[index].FixtureID == catalog.FixtureID {
			oracle = &oracles.Entries[index]
			break
		}
	}
	if oracle == nil || oracle.FixtureSHA256 != catalog.FixtureSHA256 {
		row.FailureClass = "oracle-mismatch"
		return row
	}
	var correction *Artifact
	for index := range catalog.Artifacts {
		if catalog.Artifacts[index].Role == RoleCorrection {
			if correction != nil {
				row.FailureClass = "duplicate-correction"
				return row
			}
			correction = &catalog.Artifacts[index]
		}
	}
	if correction == nil {
		row.FailureClass = "missing-correction"
		return row
	}
	row.CorrectionID, row.CorrectionSHA256 = correction.ID, correction.SHA256
	if !containsString(correction.Parents, catalog.Original.ID) {
		row.FailureClass = "provenance"
		return row
	}
	root := filepath.Dir(path)
	originalBytes, err := readRootFileValue(filepath.Join(root, "artifacts", catalog.Original.ID), maxArtifactBytes)
	if err != nil {
		row.FailureClass = "original-unavailable"
		return row
	}
	correctionBytes, err := readRootFileValue(filepath.Join(root, "artifacts", correction.ID), maxArtifactBytes)
	if err != nil {
		row.FailureClass = "correction-unavailable"
		return row
	}
	originalImage, err := png.Decode(bytes.NewReader(originalBytes))
	if err != nil {
		row.FailureClass = "original-image"
		return row
	}
	correctedImage, err := png.Decode(bytes.NewReader(correctionBytes))
	if err != nil {
		row.FailureClass = "correction-image"
		return row
	}
	transform, err := ParseCorrectionTransform(correction.Parameters, originalImage.Bounds())
	if err != nil || transform.OutputWidth != correctedImage.Bounds().Dx() ||
		transform.OutputHeight != correctedImage.Bounds().Dy() {
		row.FailureClass = "transform"
		return row
	}
	row.Transform = transform
	quality := CorrectionQuality{
		Status: "passed", TransformValid: true,
		CorrectionWidth: correctedImage.Bounds().Dx(), CorrectionHeight: correctedImage.Bounds().Dy(),
		EdgeRegions: len(oracle.EdgeRegions), ContentRegions: len(oracle.ContentRegions),
	}
	for _, region := range oracle.EdgeRegions {
		if regionCenterInside(region, transform.CropQuad) {
			quality.EdgeRegionsRetained++
		}
	}
	for _, region := range oracle.ContentRegions {
		if regionCenterInside(region, transform.CropQuad) {
			quality.ContentRegionsRetained++
		}
	}
	quality.OriginalForeground = foregroundPixels(originalImage, oracle.BackgroundRGB)
	quality.CorrectionForeground = foregroundPixels(correctedImage, oracle.BackgroundRGB)
	if quality.OriginalForeground > 0 {
		quality.ForegroundRetention = math.Min(1, float64(quality.CorrectionForeground)/float64(quality.OriginalForeground))
	}
	if quality.EdgeRegionsRetained != quality.EdgeRegions || quality.ContentRegionsRetained != quality.ContentRegions ||
		quality.CorrectionForeground == 0 {
		quality.Status = "failed"
		row.FailureClass = "edge-or-content-loss"
	}
	row.Quality = quality
	if row.MeasurementStatus == "complete" && quality.Status == "passed" {
		row.Status = "passed"
	}
	return row
}

func loadArtifactCatalog(path string) (ArtifactCatalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return ArtifactCatalog{}, ErrInvalid
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 8<<20))
	decoder.DisallowUnknownFields()
	var catalog ArtifactCatalog
	if err := decoder.Decode(&catalog); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		catalog.Format != ArtifactCatalogFormat || catalog.Version != ArtifactCatalogVersion ||
		!validHash(catalog.Original.SHA256) || !validHash(catalog.FixtureSHA256) {
		return ArtifactCatalog{}, ErrInvalid
	}
	return catalog, nil
}

func validCorrectionQuad(quad [4]CorrectionPoint) bool {
	seen := map[CorrectionPoint]bool{}
	sign := 0
	for index := range quad {
		if seen[quad[index]] {
			return false
		}
		seen[quad[index]] = true
		a, b, c := quad[index], quad[(index+1)%4], quad[(index+2)%4]
		cross := (b.X-a.X)*(c.Y-b.Y) - (b.Y-a.Y)*(c.X-b.X)
		if math.Abs(cross) < 1e-9 {
			return false
		}
		current := 1
		if cross < 0 {
			current = -1
		}
		if sign == 0 {
			sign = current
		} else if sign != current {
			return false
		}
	}
	return sign > 0
}

func regionCenterInside(region CorrectionRegion, quad [4]CorrectionPoint) bool {
	point := CorrectionPoint{
		X: float64(region.X) + float64(region.Width)/2,
		Y: float64(region.Y) + float64(region.Height)/2,
	}
	inside := false
	for i, j := 0, 3; i < 4; j, i = i, i+1 {
		a, b := quad[i], quad[j]
		if (a.Y > point.Y) != (b.Y > point.Y) &&
			point.X < (b.X-a.X)*(point.Y-a.Y)/(b.Y-a.Y)+a.X {
			inside = !inside
		}
	}
	return inside
}

func foregroundPixels(imageValue image.Image, background [3]uint8) int64 {
	var count int64
	for y := imageValue.Bounds().Min.Y; y < imageValue.Bounds().Max.Y; y++ {
		for x := imageValue.Bounds().Min.X; x < imageValue.Bounds().Max.X; x++ {
			r, g, b, _ := imageValue.At(x, y).RGBA()
			r8, g8, b8 := uint8(r>>8), uint8(g>>8), uint8(b>>8)
			if distance(r8, background[0])+distance(g8, background[1])+distance(b8, background[2]) > 24 {
				count++
			}
		}
	}
	return count
}

func distance(a, b uint8) int {
	value := int(a) - int(b)
	if value < 0 {
		value = -value
	}
	return value
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
