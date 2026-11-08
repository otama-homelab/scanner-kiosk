package evaluation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"scanner-kiosk/internal/processrun"
)

const (
	ValidationFormat  = "scanner-eval-validation"
	ValidationVersion = 1
	toolOutputLimit   = 8 << 20
	toolTimeout       = 30 * time.Second
)

type ToolEvidence struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Version string `json:"version"`
}

type ValidationCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type PDFPageEvidence struct {
	Index               int        `json:"index"`
	PageID              string     `json:"page_id"`
	MediaBox            [4]float64 `json:"media_box"`
	Rotation            int        `json:"rotation"`
	EmbeddedImages      int        `json:"embedded_images"`
	ImageEncodings      []string   `json:"image_encodings,omitempty"`
	ExtractedTextSHA256 string     `json:"extracted_text_sha256"`
	ExtractedTextBytes  int64      `json:"extracted_text_bytes"`
	ExpectedTextMatches int        `json:"expected_text_matches"`
	RenderSHA256        string     `json:"render_sha256"`
}

type ValidationReceipt struct {
	Format       string            `json:"format"`
	Version      int               `json:"version"`
	Status       string            `json:"status"`
	FailureClass string            `json:"failure_class,omitempty"`
	PDFSHA256    string            `json:"pdf_sha256,omitempty"`
	Tools        []ToolEvidence    `json:"tools,omitempty"`
	Pages        []PDFPageEvidence `json:"pages,omitempty"`
	Checks       []ValidationCheck `json:"checks"`
}

type PDFValidationOptions struct {
	OriginalImagePath string
	ExpectLossless    bool
}

type popplerPaths struct {
	Info   string
	Images string
	Text   string
	Render string
}

func ValidatePDF(ctx context.Context, pdfPath string, pageIDs, expectedText []string, options PDFValidationOptions) (ValidationReceipt, error) {
	return validatePDF(ctx, pdfPath, pageIDs, expectedText, options, []string{"/usr/bin", "/bin"})
}

func validatePDF(ctx context.Context, pdfPath string, pageIDs, expectedText []string, options PDFValidationOptions, toolDirs []string) (ValidationReceipt, error) {
	receipt := ValidationReceipt{Format: ValidationFormat, Version: ValidationVersion, Status: "failed"}
	if ctx.Err() != nil {
		return receipt, ctx.Err()
	}
	if !filepath.IsAbs(pdfPath) || filepath.Clean(pdfPath) != pdfPath ||
		len(pageIDs) == 0 || len(pageIDs) > 256 || !validStringSet(pageIDs, 96) ||
		!validStrings(expectedText, 256, 1024) ||
		(options.OriginalImagePath == "") != !options.ExpectLossless ||
		options.OriginalImagePath != "" && (!filepath.IsAbs(options.OriginalImagePath) ||
			filepath.Clean(options.OriginalImagePath) != options.OriginalImagePath) {
		receipt.FailureClass = "invalid-input"
		return receipt, ErrInvalid
	}
	pdfSHA, err := hashRegularFile(pdfPath, maxArtifactBytes)
	if err != nil {
		receipt.FailureClass = "invalid-pdf"
		return receipt, ErrInvalid
	}
	receipt.PDFSHA256 = pdfSHA
	file, err := os.Open(pdfPath)
	if err != nil {
		receipt.FailureClass = "invalid-pdf"
		return receipt, ErrInvalid
	}
	header := make([]byte, 5)
	_, readErr := io.ReadFull(file, header)
	file.Close()
	if readErr != nil || string(header) != "%PDF-" {
		receipt.FailureClass = "invalid-pdf"
		return receipt, ErrInvalid
	}
	tools, err := resolvePoppler(toolDirs)
	if err != nil {
		receipt.Status = "unavailable"
		receipt.FailureClass = "tool-unavailable"
		return receipt, ErrUnavailable
	}
	for _, tool := range []struct{ Name, Path string }{
		{"pdfinfo", tools.Info}, {"pdfimages", tools.Images}, {"pdftotext", tools.Text}, {"pdftoppm", tools.Render},
	} {
		version, versionErr := toolVersion(ctx, tool.Path)
		if versionErr != nil {
			receipt.Status = "unavailable"
			receipt.FailureClass = "tool-unavailable"
			return receipt, ErrUnavailable
		}
		receipt.Tools = append(receipt.Tools, ToolEvidence{Name: tool.Name, Path: tool.Path, Version: version})
	}
	work, err := os.MkdirTemp("", "scanner-eval-poppler-")
	if err != nil {
		receipt.Status = "unavailable"
		receipt.FailureClass = "temporary-storage"
		return receipt, ErrUnavailable
	}
	defer os.RemoveAll(work)
	textMatches := map[string]bool{}
	for index, pageID := range pageIDs {
		pageNumber := index + 1
		infoOutput, _, commandErr := runTool(ctx, tools.Info, []string{"-f", strconv.Itoa(pageNumber), "-l", strconv.Itoa(pageNumber), "-box", pdfPath}, work)
		if commandErr != nil {
			return validationFailure(receipt, "pdfinfo", "invalid-pdf")
		}
		totalPages, mediaBox, rotation, parseErr := parsePDFInfo(infoOutput)
		if parseErr != nil || totalPages != len(pageIDs) {
			return validationFailure(receipt, "page-count-and-geometry", "page-count")
		}
		receipt.Checks = append(receipt.Checks, ValidationCheck{Name: "page-count-and-geometry", Status: "pass"})
		imagesOutput, _, commandErr := runTool(ctx, tools.Images, []string{"-list", "-f", strconv.Itoa(pageNumber), "-l", strconv.Itoa(pageNumber), pdfPath}, work)
		if commandErr != nil {
			return validationFailure(receipt, "embedded-image-retention", "invalid-pdf")
		}
		imageCount, encodings := parsePDFImages(imagesOutput, pageNumber)
		if imageCount == 0 {
			return validationFailure(receipt, "embedded-image-retention", "image-missing")
		}
		receipt.Checks = append(receipt.Checks, ValidationCheck{Name: "embedded-image-retention", Status: "pass"})
		textOutput, _, commandErr := runTool(ctx, tools.Text, []string{"-f", strconv.Itoa(pageNumber), "-l", strconv.Itoa(pageNumber), "-raw", pdfPath, "-"}, work)
		if commandErr != nil || !utf8.Valid(textOutput) {
			return validationFailure(receipt, "source-page-text-binding", "text-invalid")
		}
		textSHA := sha256.Sum256(textOutput)
		compact := compactText(string(textOutput))
		matches := 0
		for _, expected := range expectedText {
			want := compactText(expected)
			if want != "" && strings.Contains(compact, want) {
				textMatches[expected] = true
				matches++
			}
		}
		firstPrefix := filepath.Join(work, fmt.Sprintf("first-%d", pageNumber))
		secondPrefix := filepath.Join(work, fmt.Sprintf("second-%d", pageNumber))
		if _, _, err := runTool(ctx, tools.Render, []string{"-png", "-r", "72", "-f", strconv.Itoa(pageNumber), "-l", strconv.Itoa(pageNumber), "-singlefile", pdfPath, firstPrefix}, work); err != nil {
			return validationFailure(receipt, "render-stability", "render-failed")
		}
		if _, _, err := runTool(ctx, tools.Render, []string{"-png", "-r", "72", "-f", strconv.Itoa(pageNumber), "-l", strconv.Itoa(pageNumber), "-singlefile", pdfPath, secondPrefix}, work); err != nil {
			return validationFailure(receipt, "render-stability", "render-failed")
		}
		firstHash, err := hashRegularFile(firstPrefix+".png", maxArtifactBytes)
		if err != nil {
			return validationFailure(receipt, "render-stability", "render-failed")
		}
		secondHash, err := hashRegularFile(secondPrefix+".png", maxArtifactBytes)
		if err != nil || firstHash != secondHash {
			return validationFailure(receipt, "render-stability", "render-unstable")
		}
		receipt.Checks = append(receipt.Checks, ValidationCheck{Name: "render-stability", Status: "pass"})
		if options.OriginalImagePath != "" && options.ExpectLossless {
			if err := compareExtractedImage(ctx, tools.Images, pdfPath, pageNumber, options.OriginalImagePath, work); err != nil {
				return validationFailure(receipt, "lossless-original-pixels", "pixel-mismatch")
			}
			receipt.Checks = append(receipt.Checks, ValidationCheck{Name: "lossless-original-pixels", Status: "pass"})
		}
		receipt.Pages = append(receipt.Pages, PDFPageEvidence{
			Index: pageNumber, PageID: pageID, MediaBox: mediaBox, Rotation: rotation,
			EmbeddedImages: imageCount, ImageEncodings: encodings,
			ExtractedTextSHA256: hex.EncodeToString(textSHA[:]), ExtractedTextBytes: int64(len(textOutput)),
			ExpectedTextMatches: matches, RenderSHA256: firstHash,
		})
	}
	for _, expected := range expectedText {
		if !textMatches[expected] {
			return validationFailure(receipt, "source-page-text-binding", "text-binding")
		}
	}
	if len(expectedText) != 0 {
		receipt.Checks = append(receipt.Checks, ValidationCheck{Name: "source-page-text-binding", Status: "pass"})
	}
	receipt.Status = "passed"
	return receipt, nil
}

func ValidateTextArtifacts(root *os.Root, artifacts []Artifact, fixture FixtureDefinition) error {
	var recognized strings.Builder
	for _, artifact := range artifacts {
		switch artifact.Role {
		case RoleOCRText:
			body, err := readRootFile(root, path.Join("artifacts", artifact.ID), maxArtifactBytes)
			if err != nil || !utf8.Valid(body) {
				return ErrInvalid
			}
			recognized.Write(body)
			recognized.WriteByte('\n')
		case RolePositionedText, RoleStructure:
			body, err := readRootFile(root, path.Join("artifacts", artifact.ID), maxArtifactBytes)
			if err != nil || !utf8.Valid(body) || !jsonValid(body) {
				return ErrInvalid
			}
		}
	}
	if len(fixture.ExpectedText) != 0 {
		compact := compactText(recognized.String())
		for _, expected := range fixture.ExpectedText {
			if !strings.Contains(compact, compactText(expected)) {
				return ErrInvalid
			}
		}
	}
	return nil
}

func resolvePoppler(dirs []string) (popplerPaths, error) {
	names := []string{"pdfinfo", "pdfimages", "pdftotext", "pdftoppm"}
	foundPaths := make([]string, len(names))
	for index, name := range names {
		var found string
		for _, dir := range dirs {
			candidate := filepath.Join(dir, name)
			info, err := os.Lstat(candidate)
			if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 && filepath.Base(candidate) == name {
				found = candidate
				break
			}
		}
		if found == "" {
			return popplerPaths{}, ErrUnavailable
		}
		foundPaths[index] = found
	}
	return popplerPaths{Info: foundPaths[0], Images: foundPaths[1], Text: foundPaths[2], Render: foundPaths[3]}, nil
}

func toolVersion(ctx context.Context, toolPath string) (string, error) {
	stdout, stderr, err := runTool(ctx, toolPath, []string{"-v"}, "")
	if err != nil {
		return "", err
	}
	combined := append(stdout, stderr...)
	for _, line := range strings.Split(string(combined), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "version") {
			return line, nil
		}
	}
	return "", ErrUnavailable
}

func runTool(ctx context.Context, toolPath string, args []string, work string) ([]byte, []byte, error) {
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		stdoutRead.Close()
		stdoutWrite.Close()
		return nil, nil, err
	}
	stdout := newByteCapture(stdoutRead, toolOutputLimit)
	stderr := newByteCapture(stderrRead, toolOutputLimit)
	runCtx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	result := processrun.Run(runCtx, processrun.Spec{
		Path: toolPath, Args: args, Env: []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "LANG=C"},
		Dir: work, Stdout: stdoutWrite, Stderr: stderrWrite, Grace: time.Second,
	})
	stdoutWrite.Close()
	stderrWrite.Close()
	stdout.wait()
	stderr.wait()
	if result.Err != nil || !result.GroupStopped || stdout.overflow || stderr.overflow {
		return nil, nil, ErrUnavailable
	}
	return stdout.body(), stderr.body(), nil
}

func parsePDFInfo(body []byte) (int, [4]float64, int, error) {
	text := string(body)
	pagesMatch := regexp.MustCompile(`(?m)^Pages:\s+(\d+)\s*$`).FindStringSubmatch(text)
	boxMatch := regexp.MustCompile(`(?m)^Page\s+\d+\s+MediaBox:\s+([-0-9.]+)\s+([-0-9.]+)\s+([-0-9.]+)\s+([-0-9.]+)\s*$`).FindStringSubmatch(text)
	rotationMatch := regexp.MustCompile(`(?m)^Page\s+\d+\s+rot:\s+(-?\d+)\s*$`).FindStringSubmatch(text)
	if len(pagesMatch) != 2 || len(boxMatch) != 5 || len(rotationMatch) != 2 {
		return 0, [4]float64{}, 0, ErrInvalid
	}
	pages, err := strconv.Atoi(pagesMatch[1])
	if err != nil {
		return 0, [4]float64{}, 0, ErrInvalid
	}
	var box [4]float64
	for index := range box {
		box[index], err = strconv.ParseFloat(boxMatch[index+1], 64)
		if err != nil {
			return 0, [4]float64{}, 0, ErrInvalid
		}
	}
	rotation, err := strconv.Atoi(rotationMatch[1])
	if err != nil {
		return 0, [4]float64{}, 0, ErrInvalid
	}
	return pages, box, rotation, nil
}

func parsePDFImages(body []byte, pageNumber int) (int, []string) {
	count := 0
	var encodings []string
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 9 || fields[0] != strconv.Itoa(pageNumber) || fields[2] != "image" {
			continue
		}
		count++
		encodings = append(encodings, fields[8])
	}
	sort.Strings(encodings)
	return count, encodings
}

func compareExtractedImage(ctx context.Context, pdfimages, pdfPath string, pageNumber int, originalPath, work string) error {
	prefix := filepath.Join(work, fmt.Sprintf("extract-%d", pageNumber))
	if _, _, err := runTool(ctx, pdfimages, []string{"-png", "-f", strconv.Itoa(pageNumber), "-l", strconv.Itoa(pageNumber), pdfPath, prefix}, work); err != nil {
		return err
	}
	files, err := filepath.Glob(prefix + "-*.png")
	if err != nil || len(files) != 1 {
		return ErrInvalid
	}
	extracted, err := decodeImageFile(files[0])
	if err != nil {
		return err
	}
	original, err := decodeImageFile(originalPath)
	if err != nil {
		return err
	}
	return comparePixels(original, extracted)
}

func decodeImageFile(filePath string) (image.Image, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, ErrInvalid
	}
	defer file.Close()
	decoded, _, err := image.Decode(file)
	if err != nil {
		return nil, ErrInvalid
	}
	return decoded, nil
}

func comparePixels(want, got image.Image) error {
	wantRGBA := toRGBA(want)
	gotRGBA := toRGBA(got)
	if wantRGBA.Bounds() != gotRGBA.Bounds() || !bytes.Equal(wantRGBA.Pix, gotRGBA.Pix) {
		return ErrInvalid
	}
	return nil
}

func toRGBA(source image.Image) *image.RGBA {
	if rgba, ok := source.(*image.RGBA); ok {
		return rgba
	}
	out := image.NewRGBA(source.Bounds())
	draw.Draw(out, out.Bounds(), source, source.Bounds().Min, draw.Src)
	return out
}

func validationFailure(receipt ValidationReceipt, check, class string) (ValidationReceipt, error) {
	receipt.Status = "failed"
	receipt.FailureClass = class
	receipt.Checks = append(receipt.Checks, ValidationCheck{Name: check, Status: "fail"})
	return receipt, ErrInvalid
}

func hashRegularFile(filePath string, limit int64) (string, error) {
	info, err := os.Lstat(filePath)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return "", ErrInvalid
	}
	file, err := os.Open(filePath)
	if err != nil {
		return "", ErrInvalid
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, io.LimitReader(file, limit+1))
	if err != nil || size > limit {
		return "", ErrInvalid
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func compactText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}

func readRootFile(root *os.Root, name string, limit int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, ErrInvalid
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, ErrInvalid
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, ErrInvalid
	}
	return body, nil
}

func jsonValid(body []byte) bool {
	var value any
	return json.Unmarshal(body, &value) == nil
}

type byteCapture struct {
	reader   *os.File
	done     chan struct{}
	data     bytes.Buffer
	limit    int64
	overflow bool
}

func newByteCapture(reader *os.File, limit int64) *byteCapture {
	capture := &byteCapture{reader: reader, done: make(chan struct{}), limit: limit}
	go func() {
		defer close(capture.done)
		defer reader.Close()
		buffer := make([]byte, 32<<10)
		for {
			n, err := reader.Read(buffer)
			if n > 0 {
				if int64(capture.data.Len()+n) <= capture.limit {
					capture.data.Write(buffer[:n])
				} else {
					capture.overflow = true
				}
			}
			if err == io.EOF {
				return
			}
			if err != nil {
				capture.overflow = true
				return
			}
		}
	}()
	return capture
}

func (c *byteCapture) wait()        { <-c.done }
func (c *byteCapture) body() []byte { return c.data.Bytes() }
