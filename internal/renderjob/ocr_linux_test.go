//go:build linux

package renderjob

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/image/font/gofont/goregular"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/pkg/ocrplugin"
)

type candidateProvider struct {
	kind  ocrplugin.OutputKind
	calls int
}

func (p *candidateProvider) Capabilities(context.Context) (ocrplugin.Capabilities, error) {
	c, _ := (&layerProvider{}).Capabilities(context.Background())
	c.Models[0].Outputs = []ocrplugin.OutputKind{p.kind}
	return c, nil
}
func (p *candidateProvider) Run(ctx context.Context, r ocrplugin.Request, a ocrplugin.Access) (ocrplugin.Result, error) {
	p.calls++
	out := ocrplugin.Result{ModelVersion: r.ModelVersion}
	for _, page := range r.Pages {
		w, e := a.CreateOutput(ctx, r.Output)
		if e != nil {
			return out, e
		}
		data := []byte("REAL OCR TEXT")
		if r.Output == ocrplugin.PositionedText {
			data, _ = json.Marshal(ocrplugin.PositionedPage{PageID: page.ID, Width: page.Width, Height: page.Height, Spans: []ocrplugin.Span{{Text: "REAL OCR TEXT", Box: ocrplugin.Box{X: 2, Y: 3, Width: 60, Height: 10}}}})
		}
		if r.Output == ocrplugin.PDFTextLayer {
			data = invisibleLayer()
		}
		if _, e = w.Write(data); e != nil {
			return out, e
		}
		if e = w.Close(); e != nil {
			return out, e
		}
		out.Pages = append(out.Pages, ocrplugin.PageResult{PageID: page.ID, ArtifactID: w.Reference(), Kind: r.Output})
	}
	return out, nil
}

type candidateRecords struct {
	*faultRecords
	provider *candidateProvider
	db       *sql.DB
}

func (r *candidateRecords) SelectOCR(ctx context.Context, id string, req ocrplugin.Request) (ocrplugin.Provider, error) {
	reg, e := ocrplugin.New(ctx, r.provider)
	if e != nil {
		return nil, e
	}
	return reg.Select(id, req)
}
func (*candidateRecords) OCRTextLayerFont(context.Context) ([]byte, error) { return goregular.TTF, nil }
func candidateFixture(t *testing.T, kind ocrplugin.OutputKind) (*candidateRecords, string, string, pdfassemble.Options, documents.Export) {
	t.Helper()
	records, db, root, job, options := fixture(t)
	p, e := Prepare(context.Background(), records, root, owner, job, runner, options)
	if e != nil {
		t.Fatal(e)
	}
	old, e := p.Publish(context.Background())
	if e != nil {
		t.Fatal(e)
	}

	a, e := records.AdmitRender(context.Background(), owner, documents.RenderAdmission{DocumentID: document, Revision: 2, IdempotencyKey: "request_reocr_00001", ReOCR: true, ResolvedOutput: kind, Settings: documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless", OCR: documents.RenderOCR{Enabled: true, ProviderID: "testocr", ModelID: "installed", ModelVersion: "test-version", Languages: []string{"en"}, QualityPreset: "auto"}}, ReserveBytes: 1 << 20}, documents.AdmissionLimits{UserMaxBytes: 4 << 20, GlobalMaxBytes: 8 << 20, JobMaxBytes: 1 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = records.ClaimExport(context.Background(), owner, a.JobID, runner); e != nil {
		t.Fatal(e)
	}
	return &candidateRecords{faultRecords: records, provider: &candidateProvider{kind: kind}, db: db}, root, a.JobID, options, old
}
func readCandidateBundle(t *testing.T, r *candidateRecords, job string) documents.OCRResultBundle {
	t.Helper()
	var raw string
	if e := r.db.QueryRow("SELECT bundle_json FROM ocr_results WHERE job_id=?", job).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var b documents.OCRResultBundle
	if e := json.Unmarshal([]byte(raw), &b); e != nil {
		t.Fatal(e)
	}
	return b
}
func TestReOCRWorkerCandidate(t *testing.T) { testWorkerCandidate(t, ocrplugin.PositionedText) }
func TestReOCRWorkerTextOnly(t *testing.T)  { testWorkerCandidate(t, ocrplugin.Text) }
func testWorkerCandidate(t *testing.T, kind ocrplugin.OutputKind) {
	records, root, job, options, old := candidateFixture(t, kind)
	p, e := Prepare(context.Background(), records, root, owner, job, runner, options)
	if e != nil {
		t.Fatalf("reviewable candidate preparation: %v", e)
	}
	defer p.Close()
	if _, e = p.Publish(context.Background()); e != nil {
		t.Fatal(e)
	}
	current, e := records.GetExport(context.Background(), owner, document)
	if e != nil || current.ID != old.ID {
		t.Fatal("candidate auto-adopted", e)
	}
	// Read the durable bundle through the actual documents database.
	var raw string
	// Store is embedded, so use the same read API used by result review.
	_ = raw
	if records.provider.calls != 1 {
		t.Fatal("recognition repeated")
	}
	b := readCandidateBundle(t, records, job)
	if len(b.Pages) != 1 || b.Pages[0].EffectiveArtifactID != "original_render_001" || b.Pages[0].Preview == nil {
		t.Fatal("source bundle incomplete")
	}
	data, e := os.ReadFile(filepath.Join(root, b.Pages[0].Text.RelativePath))
	if e != nil || !strings.Contains(string(data), "REAL OCR TEXT") {
		t.Fatal("transcript missing", e)
	}
	if (b.Pages[0].Positions != nil) != (kind == ocrplugin.PositionedText) {
		t.Fatal("invented or missing geometry")
	}
	for _, a := range []documents.ArtifactRecord{b.Output, b.Pages[0].Text, *b.Pages[0].Preview} {
		f, e := os.Open(filepath.Join(root, a.RelativePath))
		if e != nil {
			t.Fatal(e)
		}
		_, e = io.Copy(io.Discard, f)
		f.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
}

type ocrRecords struct {
	*faultRecords
	provider *layerProvider
	stop     documents.RenderStop
	stopErr  error
}

func (r *ocrRecords) FinishStoppedRender(ctx context.Context, owner, job string, stop documents.RenderStop, outcome string) error {
	r.stop = stop
	r.stopErr = r.faultRecords.FinishStoppedRender(ctx, owner, job, stop, outcome)
	return r.stopErr
}

func (r *ocrRecords) SelectOCR(ctx context.Context, id string, request ocrplugin.Request) (ocrplugin.Provider, error) {
	registry, err := ocrplugin.New(ctx, r.provider)
	if err != nil {
		return nil, err
	}
	return registry.Select(id, request)
}

type layerProvider struct {
	mode string
	seen []ocrplugin.InputPage
}

func (p *layerProvider) Capabilities(context.Context) (ocrplugin.Capabilities, error) {
	return ocrplugin.Capabilities{APIVersion: 1, ID: "testocr", Name: "Test OCR", Version: "1", Models: []ocrplugin.Model{{ID: "installed", Name: "Installed", Version: "test-version", Languages: []string{"ja", "en"}, QualityPresets: []string{"auto"}, Outputs: []ocrplugin.OutputKind{ocrplugin.PDFTextLayer}, MaxPages: 50, MaxPagePixels: 1000000}}}, nil
}
func (p *layerProvider) Run(ctx context.Context, request ocrplugin.Request, access ocrplugin.Access) (ocrplugin.Result, error) {
	p.seen = append([]ocrplugin.InputPage(nil), request.Pages...)
	result := ocrplugin.Result{ModelVersion: request.ModelVersion}
	for _, page := range request.Pages {
		writer, err := access.CreateOutput(ctx, ocrplugin.PDFTextLayer)
		if err != nil {
			return result, err
		}
		if _, err = writer.Write(invisibleLayer()); err != nil {
			return result, err
		}
		if err = writer.Close(); err != nil {
			return result, err
		}
		result.Pages = append(result.Pages, ocrplugin.PageResult{PageID: page.ID, ArtifactID: writer.Reference(), Kind: ocrplugin.PDFTextLayer})
		if p.mode == "mutating-request" {
			request.Pages[0].ArtifactID = writer.Reference()
			request.Languages[0] = "mutated"
		}
	}
	switch p.mode {
	case "bad-version":
		result.ModelVersion = "wrong"
	case "foreign-page":
		result.Pages[0].PageID = "foreign-page"
	case "unknown-execution":
		return ocrplugin.Result{}, ocrplugin.ErrReconciliation
	}
	return result, nil
}
func invisibleLayer() []byte {
	content := "BT /F1 8 Tf 3 Tr 10 50 Td (OCR RESULT) Tj ET"
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 80 100] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return b.Bytes()
}
func TestOCRExecutionPublishesOrPreservesUncertainWork(t *testing.T) {
	for _, mode := range []string{"success", "mutating-request", "bad-version", "foreign-page", "unknown-execution"} {
		t.Run(mode, func(t *testing.T) {
			settings := documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless", OCR: documents.RenderOCR{Enabled: true, ProviderID: "testocr", ModelID: "installed", ModelVersion: "test-version", Languages: []string{"ja", "en"}, QualityPreset: "auto"}}
			records, docs, root, job, options := fixture(t, settings)
			selected := &ocrRecords{faultRecords: records, provider: &layerProvider{mode: mode}}
			recovery, err := ExecuteClaimed(context.Background(), selected, root, owner, job, runner, options, time.Millisecond)
			if mode == "success" || mode == "mutating-request" {
				if err != nil || recovery != nil {
					t.Fatalf("OCR execution: %v", err)
				}
				output, e := records.GetExport(context.Background(), owner, document)
				if e != nil {
					t.Fatal(e)
				}
				data, e := os.ReadFile(filepath.Join(root, output.RelativePath))
				if e != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
					t.Fatal("OCR PDF missing")
				}
			} else if mode == "unknown-execution" {
				if recovery == nil || !errors.Is(err, ocrplugin.ErrReconciliation) {
					t.Fatal("uncertain OCR lost its recovery owner")
				}
				if !errors.Is(recovery.Close(), ErrPending) || !errors.Is(recovery.Finish(context.Background(), "failed"), ErrPending) {
					t.Fatal("unknown execution allowed deletion/terminal state")
				}
				var reservations int
				docs.QueryRow("SELECT count(*) FROM reservations WHERE job_id=?", job).Scan(&reservations)
				if reservations != 1 {
					t.Fatal("unknown execution reservation released")
				}
				// Fault injection launched no native child; only the test can confirm this.
				recovery.executionUncertain = false
				if e := recovery.Finish(context.Background(), "interrupted"); e != nil {
					t.Fatal(e)
				}
			} else {
				if err == nil || recovery != nil {
					t.Logf("OCR failure: mode=%s recovery=%t error=%v cleanup_calls=%d stop=%+v stop_error=%v now=%d", mode, recovery != nil, err, records.cleanupCalls, selected.stop, selected.stopErr, time.Now().UnixMilli())
					var state string
					var started, heartbeat, deadline int64
					var reservations int
					diagnosticErr := docs.QueryRow(`SELECT state,started_at_ms,heartbeat_at_ms,deadline_at_ms,(SELECT count(*) FROM reservations WHERE job_id=jobs.id) FROM jobs WHERE id=?`, job).Scan(&state, &started, &heartbeat, &deadline, &reservations)
					t.Logf("OCR fixture: state=%s started=%d heartbeat=%d deadline=%d reservations=%d read=%v", state, started, heartbeat, deadline, reservations, diagnosticErr)
					if recovery != nil {
						recovery.mu.Lock()
						t.Logf("OCR recovery: stopped=%d pending=%t execution_unknown=%t cleanup_pending=%t discarded=%t finished=%t", recovery.stoppedAtMS, recovery.pending, recovery.executionUncertain, recovery.cleanupPending, recovery.discarded, recovery.finished)
						recovery.mu.Unlock()
					}
					t.Fatal("invalid OCR result accepted or cleanup incomplete")
				}
				var reservations int
				docs.QueryRow("SELECT count(*) FROM reservations WHERE job_id=?", job).Scan(&reservations)
				if reservations != 0 {
					t.Fatal("confirmed failed OCR reservation retained")
				}
			}
			entries, e := os.ReadDir(root)
			if e != nil {
				t.Fatal(e)
			}
			scopeCount := 0
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "work_") {
					scopeCount++
				}
			}
			expected := 0
			if mode == "success" || mode == "mutating-request" {
				expected = 1
			}
			if scopeCount != expected {
				t.Fatal("unexpected retained OCR scopes")
			}
			if _, e = os.Stat(filepath.Join(root, "original.png")); e != nil {
				t.Fatal("original removed")
			}
		})
	}
}
