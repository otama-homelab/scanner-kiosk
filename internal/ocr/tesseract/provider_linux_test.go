//go:build linux

package tesseract

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/imageexport"
	"scanner-kiosk/pkg/ocrplugin"
)

type memoryAccess struct {
	input   []byte
	outputs []*memoryOutput
	opens   int
}
type memoryOutput struct {
	bytes.Buffer
	closed bool
}

func (m *memoryOutput) Close() error      { m.closed = true; return nil }
func (m *memoryOutput) Reference() string { return "artifact_result_0001" }
func (m *memoryAccess) OpenInput(context.Context, string) (io.ReadCloser, error) {
	m.opens++
	return io.NopCloser(bytes.NewReader(m.input)), nil
}
func (m *memoryAccess) CreateOutput(context.Context, ocrplugin.OutputKind) (ocrplugin.ArtifactWriter, error) {
	o := &memoryOutput{}
	m.outputs = append(m.outputs, o)
	return o, nil
}
func options(t *testing.T) Options {
	t.Helper()
	root := t.TempDir()
	data := filepath.Join(root, "data")
	work := filepath.Join(root, "work")
	for _, p := range []string{data, work} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, lang := range []string{"eng", "jpn", "osd"} {
		if err := os.WriteFile(filepath.Join(data, lang+".traineddata"), []byte(lang), 0600); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(root, "tool")
	// Test fixture only; production never constructs a shell command.
	script := `#!/bin/sh
[ -z "$TESSERACT_TEST_SECRET" ] || exit 9
printf 'clean output' > "$2.txt"
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return Options{Binary: binary, DataDir: data, WorkRoot: work, MaxInputBytes: 1 << 20, MaxResultBytes: 1 << 20, MaxPagePixels: 1000000, MaxPages: 5, Timeout: 5 * time.Second, StopGrace: 50 * time.Millisecond}
}
func request(p *Provider) ocrplugin.Request {
	return ocrplugin.Request{JobID: "job_000000000001", InputRevision: 1, ModelID: "installed", ModelVersion: p.caps.Models[0].Version, Languages: []string{"ja", "en"}, QualityPreset: "auto", Output: ocrplugin.Text, Pages: []ocrplugin.InputPage{{ID: "page_00000000001", ArtifactID: "input_0000000001", Width: 80, Height: 80, DPI: 300}}}
}
func blankPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 80, 80))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestProviderUsesScopedAccessAndCleansStaging(t *testing.T) {
	t.Setenv("TESSERACT_TEST_SECRET", "must-not-leak")
	opts := options(t)
	p, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	access := &memoryAccess{input: blankPNG(t)}
	result, err := p.Run(context.Background(), request(p), access)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Pages) != 1 || result.ModelVersion != p.caps.Models[0].Version || len(access.outputs) != 1 || !access.outputs[0].closed || access.outputs[0].String() != "clean output" {
		t.Fatal("incorrect artifact result")
	}
	files, err := os.ReadDir(opts.WorkRoot)
	if err != nil || len(files) != 0 {
		t.Fatal("staging not removed")
	}
}
func TestProviderRejectsStaleSelectionBeforeReading(t *testing.T) {
	p, err := New(options(t))
	if err != nil {
		t.Fatal(err)
	}
	req := request(p)
	req.ModelVersion = "stale"
	access := &memoryAccess{input: blankPNG(t)}
	if _, err = p.Run(context.Background(), req, access); err == nil || access.opens != 0 {
		t.Fatal("stale model executed")
	}
}
func TestProviderRejectsInputAndOutputBounds(t *testing.T) {
	for _, mode := range []string{"input-bytes", "geometry", "output-bytes"} {
		t.Run(mode, func(t *testing.T) {
			opts := options(t)
			if mode == "input-bytes" {
				opts.MaxInputBytes = 1
			}
			if mode == "output-bytes" {
				opts.MaxResultBytes = 1
			}
			p, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			req := request(p)
			if mode == "geometry" {
				req.Pages[0].Width = 81
			}
			access := &memoryAccess{input: blankPNG(t)}
			if _, err = p.Run(context.Background(), req, access); err == nil || len(access.outputs) != 0 {
				t.Fatal("invalid artifact accepted")
			}
		})
	}
}
func TestDictionaryFingerprintAndCapabilitiesIsolation(t *testing.T) {
	opts := options(t)
	p, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := p.Capabilities(context.Background())
	c.Models[0].Languages[0] = "changed"
	fresh, _ := p.Capabilities(context.Background())
	if fresh.Models[0].Languages[0] == "changed" {
		t.Fatal("mutable capabilities")
	}
	if err = os.WriteFile(filepath.Join(opts.DataDir, "jpn.traineddata"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	replacement, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.caps.Models[0].Version == p.caps.Models[0].Version {
		t.Fatal("model replacement did not change revision")
	}
}

func TestRuntimeRevisionPinsContainerLibrariesAndImplementation(t *testing.T) {
	opts := options(t)
	opts.RuntimeRevision = strings.Repeat("a", 64)
	before, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.RuntimeRevision = strings.Repeat("b", 64)
	after, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if before.caps.Models[0].Version == after.caps.Models[0].Version {
		t.Fatal("runtime-only rebuild did not change model revision")
	}
	opts.RuntimeRevision = "not-a-digest"
	if _, err := New(opts); err == nil {
		t.Fatal("invalid runtime digest accepted")
	}
}

func TestOrientationModelIsRequiredAndVersioned(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		opts := options(t)
		if err := os.Remove(filepath.Join(opts.DataDir, "osd.traineddata")); err != nil {
			t.Fatal(err)
		}
		if _, err := New(opts); err == nil {
			t.Fatal("missing orientation model accepted")
		}
	})
	t.Run("replacement", func(t *testing.T) {
		opts := options(t)
		before, err := New(opts)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(opts.DataDir, "osd.traineddata"), []byte("replacement"), 0600); err != nil {
			t.Fatal(err)
		}
		after, err := New(opts)
		if err != nil || after.caps.Models[0].Version == before.caps.Models[0].Version {
			t.Fatal("orientation model replacement did not change revision", err)
		}
		access := &memoryAccess{input: blankPNG(t)}
		if _, err := after.Run(context.Background(), request(before), access); err == nil || access.opens != 0 || len(access.outputs) != 0 {
			t.Fatal("old model selection executed after orientation model replacement")
		}
	})
}

func TestQualityControlsOrientationDetection(t *testing.T) {
	for _, test := range []struct{ quality, psm string }{{"auto", "1"}, {"block", "6"}} {
		t.Run(test.quality, func(t *testing.T) {
			opts := options(t)
			script := "#!/bin/sh\nbase=$2\nwhile [ \"$#\" -gt 0 ]; do\n if [ \"$1\" = --psm ]; then\n  shift\n  [ \"$1\" = " + test.psm + " ] || exit 3\n  printf 'clean output' > \"$base.txt\"\n  exit 0\n fi\n shift\ndone\nexit 4\n"
			if err := os.WriteFile(opts.Binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			provider, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			req := request(provider)
			req.QualityPreset = test.quality
			if _, err = provider.Run(context.Background(), req, &memoryAccess{input: blankPNG(t)}); err != nil {
				t.Fatal("orientation policy not applied", err)
			}
		})
	}
}

// Explicit integration fixture paths are provided by the caller. The standard
// unit suite never depends on host packages or reads real user documents.
func TestInstalledTesseract(t *testing.T) {
	binary := os.Getenv("SCANNER_TEST_TESSERACT_BINARY")
	if binary == "" {
		t.Skip("requires extracted Tesseract and synthetic fixture")
	}
	fixture := os.Getenv("SCANNER_TEST_OCR_FIXTURE")
	input, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	opts := options(t)
	opts.Binary = binary
	opts.DataDir = os.Getenv("SCANNER_TEST_TESSDATA")
	opts.MaxInputBytes = 20 << 20
	opts.MaxResultBytes = 20 << 20
	opts.MaxPagePixels = 40000000
	opts.Timeout = time.Minute
	p, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	req := request(p)
	req.Pages[0].Width = cfg.Width
	req.Pages[0].Height = cfg.Height
	for _, kind := range []ocrplugin.OutputKind{ocrplugin.Text, ocrplugin.PDFTextLayer} {
		t.Run(string(kind), func(t *testing.T) {
			req.Output = kind
			access := &memoryAccess{input: input}
			result, err := p.Run(context.Background(), req, access)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Pages) != 1 {
				t.Fatal("missing page")
			}
			text := access.outputs[0].String()
			if kind == ocrplugin.PDFTextLayer {
				path := filepath.Join(t.TempDir(), "layer.pdf")
				if err = os.WriteFile(path, access.outputs[0].Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				out, e := exec.Command("pdftotext", "-layout", path, "-").Output()
				if e != nil {
					t.Fatal(e)
				}
				text = string(out)
			}
			compact := strings.Join(strings.Fields(text), "")
			for _, expected := range []string{"日本語の書類", "ScannerOCR2026", "300dpi"} {
				if !strings.Contains(compact, expected) {
					t.Fatalf("synthetic recognition missing expected text (%s)", kind)
				}
			}
		})
	}
}

func TestInstalledTesseractOrientation(t *testing.T) {
	fixture := os.Getenv("SCANNER_TEST_OCR_ORIENTATION_FIXTURE")
	if fixture == "" || os.Getenv("SCANNER_TEST_TESSERACT_BINARY") == "" {
		t.Skip("requires installed Tesseract and dense synthetic orientation fixture")
	}
	source, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	geometry, _, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	opts := options(t)
	opts.Binary = os.Getenv("SCANNER_TEST_TESSERACT_BINARY")
	opts.DataDir = os.Getenv("SCANNER_TEST_TESSDATA")
	opts.MaxInputBytes, opts.MaxResultBytes, opts.MaxPagePixels = 20<<20, 20<<20, 40000000
	opts.Timeout = time.Minute
	provider, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, angle := range []int{0, 90, 180, 270} {
		t.Run(fmt.Sprint(angle), func(t *testing.T) {
			rotated, err := imageexport.Build(context.Background(), source, imageexport.Page{Width: geometry.Width, Height: geometry.Height, Rotation: angle, Color: "color"}, "png", imageexport.Options{MaxInputBytes: 20 << 20, MaxOutputBytes: 20 << 20, MaxPixels: 40000000, JPEGQuality: 92})
			if err != nil {
				t.Fatal(err)
			}
			width, height := geometry.Width, geometry.Height
			if angle == 90 || angle == 270 {
				width, height = height, width
			}
			for _, kind := range []ocrplugin.OutputKind{ocrplugin.Text, ocrplugin.PDFTextLayer} {
				t.Run(string(kind), func(t *testing.T) {
					req := request(provider)
					req.Pages[0].Width, req.Pages[0].Height, req.Output = width, height, kind
					access := &memoryAccess{input: rotated}
					if _, err := provider.Run(context.Background(), req, access); err != nil {
						t.Fatal(err)
					}
					text := access.outputs[0].String()
					if kind == ocrplugin.PDFTextLayer {
						path := filepath.Join(t.TempDir(), "layer.pdf")
						if err := os.WriteFile(path, access.outputs[0].Bytes(), 0600); err != nil {
							t.Fatal(err)
						}
						output, err := exec.Command("pdftotext", "-raw", path, "-").Output()
						if err != nil {
							t.Fatal(err)
						}
						text = string(output)
					}
					compact := strings.Join(strings.Fields(text), "")
					for _, expected := range []string{"日本語の書類", "ScannerOCR2026", "300dpi"} {
						if !strings.Contains(compact, expected) {
							t.Fatal("rotated synthetic page lost expected text", expected)
						}
					}
					if !bytes.Equal(access.input, rotated) {
						t.Fatal("recognition modified the original bitmap")
					}
				})
			}
		})
	}
}

func TestStagingWatchdogStopsUncooperativeTool(t *testing.T) {
	opts := options(t)
	script := `#!/bin/sh
trap '' TERM
printf 'too much output for this budget' > "$2.txt"
while :; do :; done
`
	if err := os.WriteFile(opts.Binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	opts.MaxResultBytes = 1
	p, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	access := &memoryAccess{input: blankPNG(t)}
	_, err = p.Run(context.Background(), request(p), access)
	if err == nil || !strings.Contains(err.Error(), "exceeds limits") || len(access.outputs) != 0 {
		t.Fatalf("watchdog failed: %v", err)
	}
	files, err := os.ReadDir(opts.WorkRoot)
	if err != nil || len(files) != 0 {
		t.Fatal("cancelled staging not cleaned")
	}
}

type scratchAccess struct {
	*memoryAccess
	t                      *testing.T
	root                   string
	held                   bool
	reservations, released int
}
type scratchLease struct {
	access *scratchAccess
	done   bool
}

func (a *scratchAccess) ReserveScratch(_ context.Context, _ string, result, diagnostics int64) (ocrplugin.ScratchReservation, error) {
	if a.held {
		a.t.Fatal("next page borrowed before previous scratch cleanup")
	}
	if result <= 0 || diagnostics != 1<<20 {
		a.t.Fatal("native limits not reserved")
	}
	a.held = true
	a.reservations++
	return &scratchLease{access: a}, nil
}
func (r *scratchLease) InputBytes() int64 { return int64(len(r.access.input)) }
func (r *scratchLease) ReleaseAfterCleanup() {
	if r.done {
		r.access.t.Fatal("adapter released a native lease twice")
		return
	}
	r.done = true
	err := filepath.WalkDir(r.access.root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			r.access.t.Fatal("scratch credit returned while staged files remain")
		}
		return nil
	})
	if err != nil {
		r.access.t.Fatal(err)
	}
	r.access.held = false
	r.access.released++
}
func TestProviderCleansAndReleasesScratchPerPage(t *testing.T) {
	opts := options(t)
	p, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	access := &scratchAccess{memoryAccess: &memoryAccess{input: blankPNG(t)}, t: t, root: opts.WorkRoot}
	req := request(p)
	second := req.Pages[0]
	second.ID = "page_00000000002"
	req.Pages = append(req.Pages, second)
	result, err := p.Run(context.Background(), req, access)
	if err != nil || len(result.Pages) != 2 || access.held || access.reservations != 2 || access.released != 2 {
		t.Fatalf("per-page native cleanup: %v", err)
	}
}
