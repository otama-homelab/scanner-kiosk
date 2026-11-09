package evaluation

import "scanner-kiosk/pkg/ocrplugin"

type OCROracleSpan struct {
	Text string `json:"text"`
	Order int `json:"order"`
	Box [4]float64 `json:"box"`
	Cell string `json:"cell,omitempty"`
}
type OCRPageOracle struct {
	PageID string `json:"page_id"`
	SourceSHA256 string `json:"source_sha256"`
	Width int `json:"width"`
	Height int `json:"height"`
	Blank bool `json:"blank"`
	Spans []OCROracleSpan `json:"spans"`
}
type OCRPageQuality struct {
	PageID string `json:"page_id"`
	TextErrors int `json:"text_errors"`
	ReferenceCodePoints int `json:"reference_code_points"`
	ReadingOrderInversions int `json:"reading_order_inversions"`
	MatchedRegions int `json:"matched_regions"`
	Regions int `json:"regions"`
	MatchedCells int `json:"matched_cells"`
	Cells int `json:"cells"`
	Status string `json:"status"`
}
type OCRComparison struct {
	Format string `json:"format"`
	Version int `json:"version"`
	Status string `json:"status"`
	Normalization string `json:"normalization"`
	Units string `json:"units"`
	Rows []OCRComparisonRow `json:"rows"`
}
type OCRComparisonRow struct {
	EngineID string `json:"engine_id"`
	FixtureID string `json:"fixture_id"`
	FixtureSHA256 string `json:"fixture_sha256"`
	Pages []OCRPageQuality `json:"pages"`
}

func AnalyzeOCRPage(oracle OCRPageOracle, text string, positions ocrplugin.PositionedPage) (OCRPageQuality,error) {
	return OCRPageQuality{}, ErrUnavailable
}
func AnalyzeOCREvidence(oraclePath, manifestPath string, engines []string, complete bool) (OCRComparison,error) {
	return OCRComparison{}, ErrUnavailable
}
