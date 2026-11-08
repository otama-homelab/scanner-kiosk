package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxFixtures = 128
const maxFixtureBytes = 200 << 20

var RequiredDocumentTypes = []string{
	"a4-text",
	"receipt",
	"card",
	"photo",
	"table-form",
	"vertical-text",
	"faint",
	"skew",
	"shadow",
	"white-on-white",
	"invalid",
	"oversized",
	"dependency-failure",
}

func LoadCatalog(path string) (Catalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return Catalog{}, ErrInvalid
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4<<20))
	decoder.DisallowUnknownFields()
	var catalog Catalog
	if err := decoder.Decode(&catalog); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return Catalog{}, ErrInvalid
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func (c Catalog) Validate() error {
	if c.Format != CatalogFormat || c.Version != CatalogVersion || len(c.Fixtures) == 0 || len(c.Fixtures) > maxFixtures {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for index, fixture := range c.Fixtures {
		if !ValidID(fixture.ID) || seen[fixture.ID] || fixture.SourceClass != "public_synthetic" || fixture.Privacy != PrivacyPublicSynthetic ||
			fixture.MediaType != "image/png" || len(fixture.PageIDs) == 0 || len(fixture.PageIDs) > 256 || !boundedToken(fixture.DocumentType, 64) ||
			len(fixture.ExpectedOutputs) > 6 || fixture.Bytes < 0 || fixture.Bytes > maxFixtureBytes ||
			!validStrings(fixture.Languages, 8, 16) || !validStrings(fixture.ExpectedText, 64, 1024) ||
			!boundedOptionalToken(fixture.ExpectedFailureClass, 64) {
			return fmt.Errorf("%w: fixture %d", ErrInvalid, index)
		}
		if len(fixture.ExpectedOutputs) == 0 && fixture.ExpectedFailureClass == "" {
			return ErrInvalid
		}
		seen[fixture.ID] = true
		pageSeen := map[string]bool{}
		for _, page := range fixture.PageIDs {
			if !boundedToken(page, 96) || pageSeen[page] {
				return fmt.Errorf("%w: fixture %d", ErrInvalid, index)
			}
			pageSeen[page] = true
		}
		roleSeen := map[ArtifactRole]bool{}
		for _, role := range fixture.ExpectedOutputs {
			if !ValidRole(role) || roleSeen[role] {
				return fmt.Errorf("%w: fixture %d", ErrInvalid, index)
			}
			roleSeen[role] = true
		}
		if fixture.Recipe.Kind == "copy" {
			if !safeRelative(fixture.Recipe.Source) || !validHash(fixture.SHA256) || fixture.Bytes == 0 {
				return ErrInvalid
			}
		} else if fixture.Recipe.Kind == "pattern" {
			if !validPatternVariant(fixture.Recipe.Variant) || fixture.SHA256 != "" && !validHash(fixture.SHA256) {
				return ErrInvalid
			}
			if fixture.SHA256 != "" && fixture.Bytes == 0 {
				return ErrInvalid
			}
		} else {
			return fmt.Errorf("%w: fixture %d", ErrInvalid, index)
		}
		for _, text := range fixture.ExpectedText {
			if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
				return fmt.Errorf("%w: fixture %d", ErrInvalid, index)
			}
		}
	}
	return nil
}

func ValidateRequiredCoverage(catalog Catalog) error {
	if err := catalog.Validate(); err != nil {
		return err
	}
	present := map[string]bool{}
	for _, fixture := range catalog.Fixtures {
		present[fixture.DocumentType] = true
	}
	for _, required := range RequiredDocumentTypes {
		if !present[required] {
			return ErrInvalid
		}
	}
	return nil
}

func Generate(catalogPath, output, manifestPath string) error {
	catalog, err := LoadCatalog(catalogPath)
	if err != nil {
		return err
	}
	if err := ensureEmptyDirectory(output); err != nil {
		return err
	}
	fixtures := append([]FixtureDefinition(nil), catalog.Fixtures...)
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].ID < fixtures[j].ID })
	manifest := GeneratedManifest{Format: CatalogFormat, Version: CatalogVersion, ToolVersion: ToolVersion}
	for _, fixture := range fixtures {
		body, err := generateFixture(filepath.Dir(filepath.Dir(catalogPath)), fixture)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		sha := hex.EncodeToString(sum[:])
		if fixture.SHA256 != "" && !matchesFixture(fixture, body) {
			return ErrInvalid
		}
		name := fixture.ID + ".png"
		if err := os.WriteFile(filepath.Join(output, name), body, 0o644); err != nil {
			return err
		}
		manifest.Fixtures = append(manifest.Fixtures, GeneratedFixture{
			ID: fixture.ID, Privacy: fixture.Privacy, MediaType: fixture.MediaType,
			PageIDs: fixture.PageIDs, Languages: fixture.Languages, DocumentType: fixture.DocumentType,
			ExpectedOutputs: fixture.ExpectedOutputs, ExpectedFailureClass: fixture.ExpectedFailureClass,
			SHA256: sha, Bytes: int64(len(body)), ExpectedText: fixture.ExpectedText, Availability: "available",
		})
	}
	return writeJSONExclusive(manifestPath, manifest)
}

func List(catalogPath string) (GeneratedManifest, error) {
	catalog, err := LoadCatalog(catalogPath)
	if err != nil {
		return GeneratedManifest{}, err
	}
	fixtures := append([]FixtureDefinition(nil), catalog.Fixtures...)
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].ID < fixtures[j].ID })
	manifest := GeneratedManifest{Format: CatalogFormat, Version: CatalogVersion, ToolVersion: ToolVersion}
	for _, fixture := range fixtures {
		availability := "available"
		body, readErr := generateFixture(filepath.Dir(filepath.Dir(catalogPath)), fixture)
		if readErr != nil || fixture.SHA256 != "" && !matchesFixture(fixture, body) {
			availability = "unavailable"
		}
		sha := fixture.SHA256
		size := fixture.Bytes
		if readErr == nil {
			sum := sha256.Sum256(body)
			sha = hex.EncodeToString(sum[:])
			size = int64(len(body))
		}
		manifest.Fixtures = append(manifest.Fixtures, GeneratedFixture{
			ID: fixture.ID, Privacy: fixture.Privacy, MediaType: fixture.MediaType,
			PageIDs: fixture.PageIDs, Languages: fixture.Languages, DocumentType: fixture.DocumentType,
			ExpectedOutputs: fixture.ExpectedOutputs, ExpectedFailureClass: fixture.ExpectedFailureClass,
			SHA256: sha, Bytes: size, ExpectedText: fixture.ExpectedText, Availability: availability,
		})
	}
	return manifest, nil
}

func ensureEmptyDirectory(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return ErrInvalid
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return ErrInvalid
	}
	return nil
}

func resolveRecipeSource(base, source string) (string, error) {
	if !safeRelative(source) {
		return "", ErrInvalid
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return "", ErrInvalid
	}
	defer root.Close()
	info, err := root.Lstat(source)
	if err != nil || !info.Mode().IsRegular() {
		return "", ErrInvalid
	}
	file, err := root.Open(source)
	if err != nil {
		return "", ErrInvalid
	}
	file.Close()
	return filepath.Join(base, source), nil
}

func generateFixture(base string, fixture FixtureDefinition) ([]byte, error) {
	if fixture.Recipe.Kind == "copy" {
		source, err := resolveRecipeSource(base, fixture.Recipe.Source)
		if err != nil {
			return nil, err
		}
		return readBoundedFile(source, maxFixtureBytes)
	}
	if fixture.Recipe.Kind != "pattern" {
		return nil, ErrInvalid
	}
	if fixture.Recipe.Variant == "invalid" {
		return []byte("not a png fixture\n"), nil
	}
	if fixture.Recipe.Variant == "oversized" {
		body := make([]byte, 256)
		for i := range body {
			body[i] = byte(i)
		}
		return body, nil
	}
	var baseImage image.Image
	if fixture.Recipe.Source != "" {
		source, err := resolveRecipeSource(base, fixture.Recipe.Source)
		if err != nil {
			return nil, err
		}
		file, err := os.Open(source)
		if err != nil {
			return nil, ErrInvalid
		}
		decoded, _, err := image.Decode(file)
		file.Close()
		if err != nil {
			return nil, ErrInvalid
		}
		baseImage = decoded
	}
	img, err := renderPattern(fixture.Recipe.Variant, baseImage)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&output, img); err != nil {
		return nil, err
	}
	if output.Len() > maxFixtureBytes {
		return nil, ErrInvalid
	}
	return output.Bytes(), nil
}

func renderPattern(variant string, base image.Image) (image.Image, error) {
	switch variant {
	case "vertical":
		if base == nil {
			return nil, ErrInvalid
		}
		src := rgbaImage(base)
		b := src.Bounds()
		dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				target := x*dst.Stride + (b.Dy()-1-y)*4
				copy(dst.Pix[target:target+4], src.Pix[y*src.Stride+x*4:y*src.Stride+x*4+4])
			}
		}
		return dst, nil
	case "faint", "skew", "shadow":
		if base == nil {
			return nil, ErrInvalid
		}
		return transformBase(variant, base), nil
	case "receipt":
		return patternCanvas(640, 960, variant), nil
	case "card":
		return patternCanvas(960, 640, variant), nil
	case "photo":
		return patternCanvas(960, 640, variant), nil
	case "table-form":
		return patternCanvas(1280, 1800, variant), nil
	case "white-on-white":
		return patternCanvas(1200, 1600, variant), nil
	case "dependency-failure":
		return patternCanvas(64, 64, variant), nil
	default:
		return nil, ErrInvalid
	}
}

func transformBase(variant string, base image.Image) image.Image {
	src := rgbaImage(base)
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	for y := 0; y < b.Dy(); y++ {
		shift := 0
		if variant == "skew" {
			shift = y%41 - 20
		}
		for x := 0; x < b.Dx(); x++ {
			source := y*src.Stride + x*4
			targetX := x + shift
			if targetX < 0 || targetX >= b.Dx() {
				continue
			}
			target := y*dst.Stride + targetX*4
			copy(dst.Pix[target:target+4], src.Pix[source:source+4])
			if variant == "faint" {
				for channel := 0; channel < 3; channel++ {
					dst.Pix[target+channel] = uint8((uint16(dst.Pix[target+channel]) + 3*255) / 4)
				}
			}
			if variant == "shadow" {
				shade := 255 - y*64/(b.Dy()+1)
				for channel := 0; channel < 3; channel++ {
					dst.Pix[target+channel] = uint8(uint16(dst.Pix[target+channel]) * uint16(shade) / 255)
				}
			}
		}
	}
	return dst
}

func rgbaImage(src image.Image) *image.RGBA {
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

func patternCanvas(width, height int, variant string) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	ink := color.RGBA{R: 24, G: 28, B: 34, A: 255}
	for y := height / 10; y < height; y += height / 9 {
		thickness := height / 180
		for t := 0; t < thickness; t++ {
			draw.Draw(img, image.Rect(width/10, y+t, width*9/10, y+t+1), &image.Uniform{C: ink}, image.Point{}, draw.Src)
		}
	}
	if variant == "table-form" {
		for x := width / 5; x < width; x += width / 5 {
			draw.Draw(img, image.Rect(x, height/8, x+2, height*7/8), &image.Uniform{C: ink}, image.Point{}, draw.Src)
		}
	}
	if variant == "white-on-white" {
		draw.Draw(img, image.Rect(width/6, height/5, width*5/6, height*4/5), &image.Uniform{C: color.RGBA{248, 248, 248, 255}}, image.Point{}, draw.Src)
	}
	if variant == "card" || variant == "photo" || variant == "receipt" {
		border := color.RGBA{R: 40, G: 90, B: 140, A: 255}
		draw.Draw(img, image.Rect(20, 20, width-20, 24), &image.Uniform{C: border}, image.Point{}, draw.Src)
		draw.Draw(img, image.Rect(20, height-24, width-20, height-20), &image.Uniform{C: border}, image.Point{}, draw.Src)
	}
	return img
}

func validPatternVariant(value string) bool {
	switch value {
	case "vertical", "faint", "skew", "shadow", "receipt", "card", "photo", "table-form", "white-on-white", "invalid", "oversized", "dependency-failure":
		return true
	default:
		return false
	}
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, ErrInvalid
	}
	return body, nil
}

func matchesFixture(fixture FixtureDefinition, body []byte) bool {
	sum := sha256.Sum256(body)
	return int64(len(body)) == fixture.Bytes && hex.EncodeToString(sum[:]) == fixture.SHA256
}

func writeJSONExclusive(path string, value any) error {
	if path == "" || !filepath.IsAbs(path) {
		return ErrInvalid
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrInvalid
	}
	if _, err := io.Copy(file, bytes.NewReader(body)); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func validHash(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validStrings(values []string, maxItems, maxLen int) bool {
	if len(values) > maxItems {
		return false
	}
	for _, value := range values {
		if !utf8.ValidString(value) || len(value) == 0 || len(value) > maxLen || strings.ContainsRune(value, 0) {
			return false
		}
	}
	return true
}

func boundedToken(value string, max int) bool {
	return value != "" && len(value) <= max && utf8.ValidString(value) && !strings.ContainsAny(value, "/\\\x00")
}

func boundedOptionalToken(value string, max int) bool {
	return value == "" || boundedToken(value, max)
}

func safeRelative(value string) bool {
	return value != "" && !filepath.IsAbs(value) && filepath.Clean(value) == value &&
		value != "." && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsRune(value, 0)
}
