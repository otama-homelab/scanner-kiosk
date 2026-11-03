package pdftextlayer

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf16"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"scanner-kiosk/pkg/ocrplugin"
)

// RenderPositioned embeds a caller-approved TrueType font and creates one
// image-free invisible-text page. Font licensing and recognition quality remain
// the caller's responsibility. Text follows provider span order, without shaping.
func RenderPositioned(ctx context.Context, page ocrplugin.PositionedPage, input ocrplugin.InputPage, fontData []byte, maxBytes int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxBytes < 1 || maxBytes > 1<<30 || len(fontData) < 12 || len(fontData) > 32<<20 || len(page.Spans) > 10000 || ocrplugin.ValidatePositionedPage(page, input) != nil {
		return nil, ErrInvalid
	}
	// FontFile2 requires TrueType outlines; CFF/OpenType and collections differ.
	if !bytes.Equal(fontData[:4], []byte{0, 1, 0, 0}) && string(fontData[:4]) != "true" {
		return nil, ErrInvalid
	}
	if int64(len(fontData)) > maxBytes {
		return nil, ErrLimit
	}
	face, err := sfnt.Parse(fontData)
	if err != nil {
		return nil, ErrInvalid
	}
	var scratch sfnt.Buffer
	metrics, err := face.Metrics(&scratch, fixed.I(1000), font.HintingNone)
	if err != nil {
		return nil, ErrInvalid
	}
	bounds, err := face.Bounds(&scratch, fixed.I(1000), font.HintingNone)
	if err != nil {
		return nil, ErrInvalid
	}
	height := float64(metrics.Ascent+metrics.Descent) / 64
	if height <= 0 {
		return nil, ErrInvalid
	}
	ids := map[rune]uint16{}
	runes := []rune{}
	glyphs := []byte{0, 0}
	widths := []float64{0}
	textBytes := 0
	for _, span := range page.Spans {
		textBytes += len(span.Text)
		if textBytes > 8<<20 {
			return nil, ErrLimit
		}
		for _, r := range span.Text {
			if _, ok := ids[r]; ok {
				continue
			}
			if len(runes) >= 65534 {
				return nil, ErrLimit
			}
			glyph, e := face.GlyphIndex(&scratch, r)
			if e != nil || glyph == 0 {
				return nil, ErrInvalid
			}
			advance, e := face.GlyphAdvance(&scratch, glyph, fixed.I(1000), font.HintingNone)
			if e != nil || advance < 0 {
				return nil, ErrInvalid
			}
			ids[r] = uint16(len(runes) + 1)
			runes = append(runes, r)
			glyphs = binary.BigEndian.AppendUint16(glyphs, uint16(glyph))
			widths = append(widths, float64(advance)/64)
		}
	}
	content := &bounded{limit: maxBytes}
	for _, span := range page.Spans {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		encoded := make([]byte, 0, len(span.Text)*2)
		width := 0.0
		for _, r := range span.Text {
			id := ids[r]
			encoded = binary.BigEndian.AppendUint16(encoded, id)
			width += widths[id]
		}
		if width <= 0 {
			return nil, ErrInvalid
		}
		sx, sy := span.Box.Width/width, span.Box.Height/height
		baseline := float64(page.Height) - span.Box.Y - span.Box.Height + float64(metrics.Descent)/64*sy
		if math.IsInf(sx, 0) || math.IsInf(sy, 0) || sx <= 0 || sy <= 0 {
			return nil, ErrInvalid
		}
		if _, err := fmt.Fprintf(content, "BT /F1 1000 Tf 3 Tr %.9g 0 0 %.9g %.9g %.9g Tm <%X> Tj ET\n", sx, sy, span.Box.X, baseline, encoded); err != nil {
			return nil, err
		}
	}
	cmap := &bounded{limit: maxBytes}
	if _, err := fmt.Fprint(cmap, "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def /CMapName /ScannerUnicode def /CMapType 2 def 1 begincodespacerange <0000> <FFFF> endcodespacerange\n"); err != nil {
		return nil, err
	}
	for start := 0; start < len(runes); start += 100 {
		end := min(start+100, len(runes))
		if _, err := fmt.Fprintf(cmap, "%d beginbfchar\n", end-start); err != nil {
			return nil, err
		}
		for i := start; i < end; i++ {
			unicode := []byte{}
			for _, unit := range utf16.Encode([]rune{runes[i]}) {
				unicode = binary.BigEndian.AppendUint16(unicode, unit)
			}
			if _, err := fmt.Fprintf(cmap, "<%04X> <%X>\n", i+1, unicode); err != nil {
				return nil, err
			}
		}
		if _, err := fmt.Fprint(cmap, "endbfchar\n"); err != nil {
			return nil, err
		}
	}
	if _, err := fmt.Fprint(cmap, "endcmap CMapName currentdict /CMap defineresource pop end end\n"); err != nil {
		return nil, err
	}
	var w bytes.Buffer
	for _, width := range widths {
		fmt.Fprintf(&w, "%.6g ", width)
	}
	stream := func(data []byte) string {
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(data), data)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", page.Width, page.Height),
		"<< /Type /Font /Subtype /Type0 /BaseFont /ScannerOCR /Encoding /Identity-H /DescendantFonts [6 0 R] /ToUnicode 9 0 R >>",
		stream(content.data.Bytes()),
		fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ScannerOCR /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 7 0 R /CIDToGIDMap 10 0 R /W [0 [%s]] >>", w.String()),
		fmt.Sprintf("<< /Type /FontDescriptor /FontName /ScannerOCR /Flags 4 /FontBBox [%.6g %.6g %.6g %.6g] /ItalicAngle 0 /Ascent %.6g /Descent %.6g /CapHeight %.6g /StemV 80 /FontFile2 8 0 R >>", float64(bounds.Min.X)/64, -float64(bounds.Max.Y)/64, float64(bounds.Max.X)/64, -float64(bounds.Min.Y)/64, float64(metrics.Ascent)/64, -float64(metrics.Descent)/64, float64(metrics.CapHeight)/64),
		fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream", len(fontData), len(fontData), fontData),
		stream(cmap.data.Bytes()), stream(glyphs),
	}
	output := &bounded{limit: maxBytes}
	if _, err := fmt.Fprint(output, "%PDF-1.7\n"); err != nil {
		return nil, err
	}
	offsets := []int{0}
	for i, object := range objects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		offsets = append(offsets, output.data.Len())
		if _, err := fmt.Fprintf(output, "%d 0 obj\n%s\nendobj\n", i+1, object); err != nil {
			return nil, err
		}
	}
	xref := output.data.Len()
	if _, err := fmt.Fprintf(output, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)); err != nil {
		return nil, err
	}
	for _, offset := range offsets[1:] {
		if _, err := fmt.Fprintf(output, "%010d 00000 n \n", offset); err != nil {
			return nil, err
		}
	}
	if _, err := fmt.Fprintf(output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return output.data.Bytes(), nil
}
