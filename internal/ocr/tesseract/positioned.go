package tesseract

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"scanner-kiosk/pkg/ocrplugin"
)

// TSV follows Tesseract's result iterator order; its boxes describe words, not
// individual glyphs. Keep that evidence rather than synthesizing character boxes.
func positionedTSVJSON(ctx context.Context, reader io.Reader, input ocrplugin.InputPage, maxBytes int64) ([]byte, error) {
	invalid := errors.New("invalid Tesseract positioned output")
	// Match the existing positioned-provider memory ceiling even if a native
	// file budget is configured larger.
	limit := min(maxBytes, 64<<20)
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || int64(len(data)) > limit || !utf8.Valid(data) {
		return nil, invalid
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), int(limit)+1)
	if !scanner.Scan() || scanner.Text() != "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext" {
		return nil, invalid
	}
	page := ocrplugin.PositionedPage{PageID: input.ID, Width: input.Width, Height: input.Height, Spans: []ocrplugin.Span{}}
	seenPage := false
	var hierarchy [4]int64
	rows := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows++
		if rows > 400001 {
			return nil, invalid
		}
		fields := strings.SplitN(scanner.Text(), "\t", 12)
		if len(fields) != 12 {
			return nil, invalid
		}
		var values [10]int64
		for i := range values {
			value, err := strconv.ParseInt(fields[i], 10, 64)
			if err != nil || value < 0 {
				return nil, invalid
			}
			values[i] = value
		}
		level := values[0]
		if level < 1 || level > 5 || values[1] != 1 {
			return nil, invalid
		}
		x, y, width, height := values[6], values[7], values[8], values[9]
		if x > int64(input.Width) || y > int64(input.Height) || width <= 0 || height <= 0 || width > int64(input.Width)-x || height > int64(input.Height)-y {
			return nil, invalid
		}
		if level == 1 {
			if seenPage || rows != 1 || x != 0 || y != 0 || width != int64(input.Width) || height != int64(input.Height) {
				return nil, invalid
			}
			seenPage = true
		} else if !seenPage {
			return nil, invalid
		}
		for i := range hierarchy {
			value := values[2+i]
			switch {
			case int64(i) < level-2:
				if hierarchy[i] == 0 || value != hierarchy[i] {
					return nil, invalid
				}
			case int64(i) == level-2:
				if value != hierarchy[i]+1 {
					return nil, invalid
				}
			default:
				if value != 0 {
					return nil, invalid
				}
			}
			hierarchy[i] = value
		}
		confidence, err := strconv.ParseFloat(fields[10], 64)
		if err != nil || math.IsNaN(confidence) || math.IsInf(confidence, 0) {
			return nil, invalid
		}
		if level < 5 {
			if confidence != -1 || fields[11] != "" {
				return nil, invalid
			}
			continue
		}
		if confidence < 0 || confidence > 100 || fields[11] == "" || strings.ContainsRune(fields[11], 0) || len(page.Spans) >= 100000 {
			return nil, invalid
		}
		confidence /= 100
		page.Spans = append(page.Spans, ocrplugin.Span{Text: fields[11], Box: ocrplugin.Box{X: float64(x), Y: float64(y), Width: float64(width), Height: float64(height)}, Confidence: &confidence})
	}
	if scanner.Err() != nil || !seenPage || ocrplugin.ValidatePositionedPage(page, input) != nil {
		return nil, invalid
	}
	encoded, err := json.Marshal(page)
	if err != nil || int64(len(encoded)) > limit {
		return nil, errors.New("OCR positioned output exceeds limits")
	}
	return encoded, nil
}
