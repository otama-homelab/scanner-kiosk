package pdftextlayer_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image/png"
	"math"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"golang.org/x/image/font/gofont/goregular"
	"scanner-kiosk/internal/pdftextlayer"
	"scanner-kiosk/pkg/ocrplugin"
)

func TestPositionedVerticalJapaneseSearch(t *testing.T) {
	checkJapaneseSearch(t, `[{"text":"日本語","box":{"x":160,"y":20,"width":24,"height":90},"writing_direction":"vertical"},{"text":"東京","box":{"x":110,"y":20,"width":24,"height":60},"writing_direction":"vertical"}]`, "日本語東京")
}

func TestPositionedMixedJapaneseSearch(t *testing.T) {
	checkJapaneseSearch(t, `[{"text":"日本 English","box":{"x":10,"y":10,"width":180,"height":24}},{"text":"縦組","box":{"x":160,"y":70,"width":24,"height":60},"writing_direction":"vertical"}]`, "日本English縦組")
}

func checkJapaneseSearch(t *testing.T, spansJSON, expected string) {
	t.Helper()
	fontPath := os.Getenv("SCANNER_TEST_POSITIONED_FONT")
	if fontPath == "" {
		fontPath = "/usr/share/fonts/opentype/ipafont-gothic/ipag.ttf"
	}
	fontData, err := os.ReadFile(fontPath)
	if err != nil {
		t.Fatalf("required approved Japanese font: %v", err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(fontData)) != "503af4a8b84d1079b8e2e358dc7f7a7fb8cb7a1f212f35eaef6782dbfc75a55e" {
		t.Fatal("Japanese font differs from the approved IPA Gothic003.03 fixture")
	}
	page := ocrplugin.PositionedPage{PageID: "japanese", Width: 220, Height: 240}
	if err := json.Unmarshal([]byte(spansJSON), &page.Spans); err != nil {
		t.Fatal(err)
	}
	output, err := pdftextlayer.RenderPositioned(context.Background(), page, ocrplugin.InputPage{ID: page.PageID, Width: page.Width, Height: page.Height}, fontData, 32<<20)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	run := func(args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, "pdftotext", args...)
		cmd.Stdin = bytes.NewReader(output)
		body, err := cmd.Output()
		if err != nil {
			t.Fatalf("required independent Poppler search proof: %v", err)
		}
		return body
	}
	compact := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, s)
	}
	if actual := compact(string(run("-raw", "-", "-"))); actual != expected {
		t.Fatalf("reading order %q, want %q", actual, expected)
	}
	var document struct {
		Words []struct {
			Text string  `xml:",chardata"`
			XMin float64 `xml:"xMin,attr"`
			YMin float64 `xml:"yMin,attr"`
			XMax float64 `xml:"xMax,attr"`
			YMax float64 `xml:"yMax,attr"`
		} `xml:"body>doc>page>word"`
	}
	if err := xml.Unmarshal(run("-bbox", "-", "-"), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Words) == 0 {
		t.Fatal("no search regions")
	}
	for _, span := range page.Spans {
		// Source span regions are authoritative; these are PDF search regions,
		// not invented recognition character boxes.
		found := ""
		minY, maxY := math.Inf(1), math.Inf(-1)
		for _, w := range document.Words {
			b := span.Box
			if w.XMin >= b.X-.1 && w.XMax <= b.X+b.Width+.1 && w.YMin >= b.Y-.1 && w.YMax <= b.Y+b.Height+.1 {
				if b.Height > b.Width {
					glyphs := []rune(span.Text)
					index := len([]rune(found))
					if len([]rune(w.Text)) != 1 || index >= len(glyphs) || math.Abs(w.YMin-(b.Y+float64(index)*b.Height/float64(len(glyphs)))) > .1 || math.Abs(w.YMax-w.YMin-b.Height/float64(len(glyphs))) > .1 {
						t.Fatalf("vertical glyph search region is horizontal or misplaced: %+v", w)
					}
				}
				found += w.Text
				minY, maxY = math.Min(minY, w.YMin), math.Max(maxY, w.YMax)
			}
		}
		if compact(found) != compact(span.Text) || math.Abs(minY-span.Box.Y) > .1 || math.Abs(maxY-span.Box.Y-span.Box.Height) > .1 {
			t.Fatalf("search regions do not cover source span %+v: text=%q y=%g..%g", span, found, minY, maxY)
		}
	}
}

func TestPositionedTextLayer(t *testing.T) {
	input := ocrplugin.InputPage{ID: "page", Width: 80, Height: 100}
	page := ocrplugin.PositionedPage{PageID: input.ID, Width: 80, Height: 100, Spans: []ocrplugin.Span{{Text: "OCR (layer) \\", Box: ocrplugin.Box{X: 4, Y: 20, Width: 70, Height: 12}}}}
	output, err := pdftextlayer.RenderPositioned(context.Background(), page, input, goregular.TTF, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err = api.Validate(context.Background(), bytes.NewReader(output), model.NewStatelessConfiguration(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err = pdftextlayer.Overlay(context.Background(), basePDF(t), output, 80, 100, 1<<20); err != nil {
		t.Fatal(err)
	}
	t.Run("independent-text-extraction", func(t *testing.T) {
		tool, err := exec.LookPath("pdftotext")
		if err != nil {
			t.Skip("requires independent Poppler extractor")
		}
		command := exec.Command(tool, "-", "-")
		command.Stdin = bytes.NewReader(output)
		text, err := command.Output()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(text), page.Spans[0].Text) {
			t.Fatalf("text did not round trip: %q", text)
		}
	})
	if exact, err := pdftextlayer.RenderPositioned(context.Background(), page, input, goregular.TTF, int64(len(output))); err != nil || !bytes.Equal(exact, output) {
		t.Fatalf("exact byte bound changed output: %v", err)
	}
	for _, mode := range []string{"font", "missing-glyph", "geometry", "limit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			p := page
			p.Spans = append([]ocrplugin.Span(nil), page.Spans...)
			data, limit := goregular.TTF, int64(1<<20)
			ctx := context.Background()
			want := pdftextlayer.ErrInvalid
			switch mode {
			case "font":
				data = []byte("invalid")
			case "missing-glyph":
				p.Spans[0].Text = "日本語"
			case "geometry":
				p.Spans[0].Box.X = -1
			case "limit":
				limit = int64(len(output) - 1)
				want = pdftextlayer.ErrLimit
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			}
			result, err := pdftextlayer.RenderPositioned(ctx, p, input, data, limit)
			if !errors.Is(err, want) || result != nil {
				t.Fatalf("got %v, want %v", err, want)
			}
		})
	}
}

func TestPositionedLayerIsInvisible(t *testing.T) {
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skip("requires independent Poppler renderer")
	}
	input := ocrplugin.InputPage{ID: "page", Width: 80, Height: 100}
	page := ocrplugin.PositionedPage{PageID: input.ID, Width: 80, Height: 100, Spans: []ocrplugin.Span{{Text: "Visible?", Box: ocrplugin.Box{X: 0, Y: 0, Width: 80, Height: 100}}}}
	output, err := pdftextlayer.RenderPositioned(context.Background(), page, input, goregular.TTF, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(tool, "-singlefile", "-r", "72", "-png", "-")
	command.Stdin = bytes.NewReader(output)
	rendered, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	bitmap, err := png.Decode(bytes.NewReader(rendered))
	if err != nil {
		t.Fatal(err)
	}
	for y := bitmap.Bounds().Min.Y; y < bitmap.Bounds().Max.Y; y++ {
		for x := bitmap.Bounds().Min.X; x < bitmap.Bounds().Max.X; x++ {
			r, g, b, a := bitmap.At(x, y).RGBA()
			if r != 65535 || g != 65535 || b != 65535 || a != 65535 {
				t.Fatal("invisible text changed rendered pixels")
			}
		}
	}
}

func TestPositionedJapaneseWithApprovedFont(t *testing.T) {
	path := os.Getenv("SCANNER_TEST_POSITIONED_FONT")
	if path == "" {
		t.Skip("requires caller-approved Japanese TrueType fixture")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	input := ocrplugin.InputPage{ID: "japanese", Width: 200, Height: 100}
	page := ocrplugin.PositionedPage{PageID: input.ID, Width: 200, Height: 100, Spans: []ocrplugin.Span{{Text: "日本語 English", Box: ocrplugin.Box{X: 5, Y: 10, Width: 190, Height: 30}}}}
	output, err := pdftextlayer.RenderPositioned(context.Background(), page, input, data, 32<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err = api.Validate(context.Background(), bytes.NewReader(output), model.NewStatelessConfiguration(), nil); err != nil {
		t.Fatal(err)
	}
	tool, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("requires independent Poppler extractor")
	}
	command := exec.Command(tool, "-", "-")
	command.Stdin = bytes.NewReader(output)
	text, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), page.Spans[0].Text) {
		t.Fatalf("Japanese text did not round trip: %q", text)
	}
}

func TestPositionedSearchCoordinates(t *testing.T) {
	tool, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("requires independent Poppler extractor")
	}
	input := ocrplugin.InputPage{ID: "coordinates", Width: 200, Height: 300}
	for _, box := range []ocrplugin.Box{{X: 10, Y: 20, Width: 120, Height: 30}, {X: 70, Y: 210, Width: 110, Height: 60}} {
		page := ocrplugin.PositionedPage{PageID: input.ID, Width: input.Width, Height: input.Height, Spans: []ocrplugin.Span{{Text: "Position", Box: box}}}
		output, err := pdftextlayer.RenderPositioned(context.Background(), page, input, goregular.TTF, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(tool, "-bbox", "-", "-")
		command.Stdin = bytes.NewReader(output)
		raw, err := command.Output()
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Pages []struct {
				Words []struct {
					Text string  `xml:",chardata"`
					XMin float64 `xml:"xMin,attr"`
					YMin float64 `xml:"yMin,attr"`
					XMax float64 `xml:"xMax,attr"`
					YMax float64 `xml:"yMax,attr"`
				} `xml:"word"`
			} `xml:"body>doc>page"`
		}
		if err = xml.Unmarshal(raw, &document); err != nil {
			t.Fatal(err)
		}
		if len(document.Pages) != 1 || len(document.Pages[0].Words) != 1 {
			t.Fatalf("unexpected search geometry: %s", raw)
		}
		word := document.Pages[0].Words[0]
		if word.Text != "Position" || math.Abs(word.XMin-box.X) > .1 || math.Abs(word.YMin-box.Y) > .1 || math.Abs(word.XMax-(box.X+box.Width)) > .1 || math.Abs(word.YMax-(box.Y+box.Height)) > .1 {
			t.Fatalf("search bounds %+v do not match %+v", word, box)
		}
	}
}

func TestBlankPositionedPage(t *testing.T) {
	input := ocrplugin.InputPage{ID: "blank", Width: 80, Height: 100}
	page := ocrplugin.PositionedPage{PageID: input.ID, Width: input.Width, Height: input.Height}
	output, err := pdftextlayer.RenderPositioned(context.Background(), page, input, goregular.TTF, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err = api.Validate(context.Background(), bytes.NewReader(output), model.NewStatelessConfiguration(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err = pdftextlayer.Overlay(context.Background(), basePDF(t), output, 80, 100, 1<<20); err != nil {
		t.Fatal(err)
	}
}
