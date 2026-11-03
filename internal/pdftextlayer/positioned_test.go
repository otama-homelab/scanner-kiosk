package pdftextlayer_test

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"image/png"
	"math"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"golang.org/x/image/font/gofont/goregular"
	"scanner-kiosk/internal/pdftextlayer"
	"scanner-kiosk/pkg/ocrplugin"
)

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
