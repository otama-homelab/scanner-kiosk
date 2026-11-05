package pdfimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func fixturePDF(extra string) []byte {
	streams := []string{"BT /F1 12 Tf 20 30 Td (IMPORT_ALPHA) Tj ET\n0 0 1 rg 10 10 30 40 re f\n", "BT /F1 12 Tf 20 30 Td (IMPORT_BETA) Tj ET\n1 0 0 rg 30 40 20 10 re f\n"}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R " + extra + " >>",
		"<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 300] /Resources << /Font << /F1 7 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(streams[0]), streams[0]),
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 250] /Resources << /Font << /F1 7 0 R >> >> /Contents 6 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(streams[1]), streams[1]),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n")
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}
func options() Options { return Options{MaxInputBytes: 1 << 20, MaxOutputBytes: 4 << 20, MaxPages: 50} }

func TestSplitPreservesTextVectorGeometryOrderAndOriginal(t *testing.T) {
	input := fixturePDF("")
	original := sha256.Sum256(input)
	pages, err := Split(context.Background(), input, options())
	if err != nil || len(pages) != 2 {
		t.Fatal("split", len(pages), err)
	}
	for i, page := range pages {
		width, height := 200.0, 300.0
		word := "IMPORT_ALPHA"
		if i == 1 {
			width, height = 400, 250
			word = "IMPORT_BETA"
		}
		if page.WidthPoints != width || page.HeightPoints != height {
			t.Fatal("page geometry changed", page.WidthPoints, page.HeightPoints)
		}
		parsed, err := api.ReadValidateAndOptimize(context.Background(), bytes.NewReader(page.PDF), model.NewStatelessConfiguration(), nil)
		if err != nil || parsed.PageCount != 1 {
			t.Fatal("not a standalone page PDF", err)
		}
		dictionary, _, _, err := parsed.PageDict(context.Background(), 1, false)
		if err != nil {
			t.Fatal(err)
		}
		content, err := parsed.PageContent(dictionary, 1)
		if err != nil || !bytes.Contains(content, []byte(word)) || !bytes.Contains(content, []byte(" re f")) {
			t.Fatal("text/vector content lost", err, string(content))
		}
	}
	if sha256.Sum256(input) != original {
		t.Fatal("original PDF mutated")
	}
}
func TestSplitRejectsInvalidLimitedCancelledAndActivePDFs(t *testing.T) {
	for _, mode := range []string{"malformed", "input-limit", "page-limit", "output-limit", "cancelled", "javascript", "embedded-files", "form", "javascript-escaped", "external-action", "external-reference", "postscript"} {
		t.Run(mode, func(t *testing.T) {
			input := fixturePDF("")
			limits := options()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := ErrInvalid
			switch mode {
			case "malformed":
				input = []byte("%PDF-1.7\ntruncated")
			case "input-limit":
				limits.MaxInputBytes = int64(len(input) - 1)
				want = ErrLimit
			case "page-limit":
				limits.MaxPages = 1
				want = ErrLimit
			case "output-limit":
				limits.MaxOutputBytes = 64
				want = ErrLimit
			case "cancelled":
				cancel()
				want = context.Canceled
			case "javascript":
				input = fixturePDF("/OpenAction << /S /JavaScript /JS (app.alert\\(1\\)) >>")
				want = ErrUnsafe
			case "external-action":
				input = fixturePDF("/Example << /S /U#52I /URI (https://example.invalid/) >>")
				want = ErrUnsafe
			case "external-reference":
				input = fixturePDF("/Ref << /F (outside.pdf) /Page 0 >>")
				want = ErrUnsafe
			case "postscript":
				input = fixturePDF("/Example << /Type /XObject /Subtype /PS >>")
				want = ErrUnsafe
			case "javascript-escaped":
				input = fixturePDF("/Open#41ction << /S /Java#53cript /J#53 (alert) >>")
				want = ErrUnsafe
			case "embedded-files":
				input = fixturePDF("/Names << /EmbeddedFiles << /Names [] >> >>")
				want = ErrUnsafe
			case "form":
				input = fixturePDF("/AcroForm << /Fields [] >>")
				want = ErrUnsafe
			}
			pages, err := Split(ctx, input, limits)
			if !errors.Is(err, want) || len(pages) != 0 {
				t.Fatalf("%s: partial output or wrong failure: %d %v", mode, len(pages), err)
			}
		})
	}
}
func TestFixtureContainsSelectableText(t *testing.T) {
	raw := fixturePDF("")
	parsed, err := api.ReadValidateAndOptimize(context.Background(), bytes.NewReader(raw), model.NewStatelessConfiguration(), nil)
	if err != nil || parsed.PageCount != 2 || !strings.Contains(string(raw), "IMPORT_ALPHA") {
		t.Fatal("invalid test fixture", err)
	}
}

func TestSplitRejectsPasswordProtectedPDF(t *testing.T) {
	conf := model.NewStatelessConfiguration()
	conf.OwnerPW = "synthetic-owner"
	conf.UserPW = "synthetic-user"
	var encrypted bytes.Buffer
	if err := api.Encrypt(context.Background(), bytes.NewReader(fixturePDF("")), &encrypted, conf); err != nil {
		t.Fatal(err)
	}
	if pages, err := Split(context.Background(), encrypted.Bytes(), options()); !errors.Is(err, ErrEncrypted) || len(pages) != 0 {
		t.Fatal("encrypted PDF accepted", err)
	}
}

func TestSplitMatchesIndependentTextAndRasterReaders(t *testing.T) {
	textTool, textErr := exec.LookPath("pdftotext")
	rasterTool, rasterErr := exec.LookPath("pdftoppm")
	if textErr != nil || rasterErr != nil {
		t.Skip("requires installed Poppler text/raster tools")
	}
	input := fixturePDF("")
	pages, err := Split(context.Background(), input, options())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	original := filepath.Join(root, "original.pdf")
	if err = os.WriteFile(original, input, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(tool string, args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, tool, args...)
		command.Env = []string{"PATH=/usr/bin:/bin"}
		output, err := command.Output()
		if err != nil {
			t.Fatal("independent reader failed", err)
		}
		return output
	}
	for i, page := range pages {
		name := filepath.Join(root, fmt.Sprintf("page-%d.pdf", i+1))
		if err = os.WriteFile(name, page.PDF, 0600); err != nil {
			t.Fatal(err)
		}
		word := "IMPORT_ALPHA"
		if i == 1 {
			word = "IMPORT_BETA"
		}
		if text := run(textTool, name, "-"); !bytes.Contains(text, []byte(word)) {
			t.Fatal("text is not selectable", string(text))
		}
		expected := run(rasterTool, "-f", fmt.Sprint(i+1), "-l", fmt.Sprint(i+1), "-singlefile", "-scale-to", "256", "-png", original)
		actual := run(rasterTool, "-f", "1", "-l", "1", "-singlefile", "-scale-to", "256", "-png", name)
		if !bytes.Equal(expected, actual) {
			t.Fatal("rendered text/vector appearance changed", i+1)
		}
	}
}
