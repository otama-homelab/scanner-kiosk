package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/pkg/ocrplugin"
)

type access struct {
	input   []byte
	outputs []*output
}
type output struct {
	bytes.Buffer
	closed bool
}

func (o *output) Close() error      { o.closed = true; return nil }
func (o *output) Reference() string { return "output_0000000001" }
func (a *access) OpenInput(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(a.input)), nil
}
func (a *access) CreateOutput(context.Context, ocrplugin.OutputKind) (ocrplugin.ArtifactWriter, error) {
	o := &output{}
	a.outputs = append(a.outputs, o)
	return o, nil
}
func fixture(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 80, 100))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func setup(t *testing.T, handler http.HandlerFunc) (*Provider, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	opts := Options{Settings: config.LLM{Profiles: []config.LLMProfile{{ID: "vision", Name: "Vision", ProcessingDestination: "Fixture OCR service", Protocol: "chat-completions", Endpoint: server.URL + "/v1/chat/completions", AllowInsecureHTTP: true, APIKeyEnv: "SCANNER_OCR_LLM_KEY_TEST", Model: "requested", ResponseModel: "pinned", Version: "fixture-v1", Languages: []string{"ja", "en"}, Outputs: []ocrplugin.OutputKind{ocrplugin.Text, ocrplugin.PositionedText}, MaxCompletionTokens: 8192}}}, Credentials: map[string]config.Secret{"vision": "secret-canary"}, MaxInputBytes: 1 << 20, MaxResponseBytes: 1 << 20, MaxPagePixels: 1000000, MaxPages: 10, MaxSpans: 100, MaxTextBytes: 10000, Timeout: 5 * time.Second}
	p, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p, server
}
func request(p *Provider, kind ocrplugin.OutputKind) ocrplugin.Request {
	return ocrplugin.Request{JobID: "job_000000000001", InputRevision: 1, ModelID: "vision", ModelVersion: p.caps.Models[0].Version, Languages: []string{"ja", "en"}, QualityPreset: "standard", Output: kind, Pages: []ocrplugin.InputPage{{ID: "page_00000000001", ArtifactID: "input_0000000001", Width: 80, Height: 100, DPI: 300}}}
}
func completion(content any) map[string]any {
	raw, _ := json.Marshal(content)
	return map[string]any{"model": "pinned", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(raw)}}}}
}
func TestVisionWireAndPositionedResult(t *testing.T) {
	input := fixture(t)
	var calls atomic.Int32
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer secret-canary" {
			t.Error("incorrect endpoint/auth")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid request")
		}
		if body["model"] != "requested" || body["stream"] != false || body["store"] != false || body["max_completion_tokens"] != float64(8192) || body["tools"] != nil {
			t.Error("incorrect protocol options")
		}
		messages := body["messages"].([]any)
		url := messages[1].(map[string]any)["content"].([]any)[0].(map[string]any)["image_url"].(map[string]any)["url"].(string)
		encoded := strings.TrimPrefix(url, "data:image/png;base64,")
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || !bytes.Equal(data, input) {
			t.Error("image bytes changed")
		}
		format := body["response_format"].(map[string]any)
		if format["type"] != "json_schema" || format["json_schema"].(map[string]any)["strict"] != true {
			t.Error("missing strict schema")
		}
		json.NewEncoder(w).Encode(completion(map[string]any{"spans": []any{map[string]any{"text": "日本語 English", "box": map[string]any{"x": 1, "y": 2, "width": 60, "height": 10}}}}))
	})
	a := &access{input: input}
	result, err := p.Run(context.Background(), request(p, ocrplugin.PositionedText), a)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len(result.Pages) != 1 || len(a.outputs) != 1 || !a.outputs[0].closed {
		t.Fatal("incorrect result")
	}
	var page ocrplugin.PositionedPage
	if json.Unmarshal(a.outputs[0].Bytes(), &page) != nil || page.PageID != "page_00000000001" || page.Width != 80 || page.Spans[0].Text != "日本語 English" || page.Spans[0].Confidence != nil {
		t.Fatal("incorrect normalized geometry")
	}
}
func TestConfiguredTimeoutAllowsSlowResponseHeaders(t *testing.T) {
	t.Parallel()
	template, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(31 * time.Second):
			json.NewEncoder(w).Encode(completion(map[string]any{"text": "complete"}))
		case <-r.Context().Done():
		}
	})
	options := template.options
	options.Timeout = 45 * time.Second
	provider, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(provider.Close)
	output := &access{input: fixture(t)}
	if _, err = provider.Run(context.Background(), request(provider, ocrplugin.Text), output); err != nil {
		t.Fatalf("valid response within the configured OCR timeout was rejected: %v", err)
	}
	if len(output.outputs) != 1 || output.outputs[0].String() != "complete" {
		t.Fatal("slow response was not saved")
	}
}

func TestPlainTranscription(t *testing.T) {
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(completion(map[string]any{"text": "日本語\nEnglish"}))
	})
	a := &access{input: fixture(t)}
	if _, err := p.Run(context.Background(), request(p, ocrplugin.Text), a); err != nil || a.outputs[0].String() != "日本語\nEnglish" {
		t.Fatalf("transcription failed: %v", err)
	}
}
func TestRejectsInvalidOrIncompleteResponse(t *testing.T) {
	for _, mode := range []string{"truncated", "refusal", "tool", "model", "missing", "geometry", "unknown-field", "missing-coordinate", "body-limit", "http-error"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				response := completion(map[string]any{"spans": []any{map[string]any{"text": "x", "box": map[string]any{"x": 1, "y": 1, "width": 5, "height": 5}}}})
				choice := response["choices"].([]any)[0].(map[string]any)
				message := choice["message"].(map[string]any)
				switch mode {
				case "truncated":
					choice["finish_reason"] = "length"
				case "refusal":
					message["refusal"] = "secret-canary"
				case "tool":
					message["tool_calls"] = []any{map[string]any{"name": "delete"}}
				case "model":
					response["model"] = "unapproved"
				case "missing":
					message["content"] = `{}`
				case "geometry":
					message["content"] = `{"spans":[{"text":"x","box":{"x":79,"y":0,"width":9,"height":10}}]}`
				case "unknown-field":
					message["content"] = `{"spans":[],"command":"secret-canary"}`
				case "missing-coordinate":
					message["content"] = `{"spans":[{"text":"x","box":{"y":0,"width":9,"height":10}}]}`
				case "body-limit":
					io.WriteString(w, strings.Repeat("x", 2048))
					return
				case "http-error":
					w.WriteHeader(429)
					io.WriteString(w, "secret-canary")
					return
				}
				json.NewEncoder(w).Encode(response)
			})
			if mode == "body-limit" {
				p.options.MaxResponseBytes = 1024
			}
			a := &access{input: fixture(t)}
			_, err := p.Run(context.Background(), request(p, ocrplugin.PositionedText), a)
			if err == nil || strings.Contains(err.Error(), "secret-canary") || len(a.outputs) != 0 || calls.Load() != 1 {
				t.Fatalf("invalid response accepted, retried or leaked: %v", err)
			}
		})
	}
}
func TestRedirectDoesNotForwardCredential(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(307)
	})
	a := &access{input: fixture(t)}
	_, err := p.Run(context.Background(), request(p, ocrplugin.Text), a)
	if err == nil || forwarded.Load() != 0 || len(a.outputs) != 0 {
		t.Fatal("redirect followed")
	}
}
func TestCancellationEndsRequest(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(stopped)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	a := &access{input: fixture(t)}
	go func() { _, err := p.Run(ctx, request(p, ocrplugin.Text), a); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel hung")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not see cancellation")
	}
	if len(a.outputs) != 0 {
		t.Fatal("cancelled output published")
	}
}

func TestBoundsAndStaleRevisionPreventNetworkCalls(t *testing.T) {
	for _, mode := range []string{"input", "dimensions", "revision", "languages"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
			req := request(p, ocrplugin.Text)
			switch mode {
			case "input":
				p.options.MaxInputBytes = 1
			case "dimensions":
				req.Pages[0].Height = 101
			case "revision":
				req.ModelVersion = "stale"
			case "languages":
				req.Languages = []string{"unsupported"}
			}
			a := &access{input: fixture(t)}
			if _, err := p.Run(context.Background(), req, a); err == nil || calls.Load() != 0 || len(a.outputs) != 0 {
				t.Fatal("invalid input reached endpoint")
			}
		})
	}
}

func TestCapabilitiesDoNotExposeOrShareConfiguration(t *testing.T) {
	p, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	c, _ := p.Capabilities(context.Background())
	raw, _ := json.Marshal(c)
	if strings.Contains(string(raw), "secret-canary") || strings.Contains(string(raw), "http://") {
		t.Fatal("private configuration exposed")
	}
	c.Models[0].Languages[0] = "modified"
	fresh, _ := p.Capabilities(context.Background())
	if fresh.Models[0].Languages[0] == "modified" {
		t.Fatal("capabilities not isolated")
	}
	if p.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("ambient proxy enabled")
	}
}

func TestPositionedResponseBecomesPinnedTextLayer(t *testing.T) {
	var text atomic.Value
	text.Store("Position")
	baseline, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid wire JSON")
		}
		schema := body["response_format"].(map[string]any)["json_schema"].(map[string]any)["schema"].(map[string]any)
		if schema["properties"].(map[string]any)["spans"] == nil {
			t.Error("PDF request must ask for positioned spans")
		}
		json.NewEncoder(w).Encode(completion(map[string]any{"spans": []any{map[string]any{"text": text.Load().(string), "box": map[string]any{"x": 5, "y": 10, "width": 70, "height": 20}}}}))
	})
	options := baseline.options
	// A PDF-capable external model must carry a public destination disclosure.
	missing := options
	missing.Settings.Profiles = append([]config.LLMProfile(nil), options.Settings.Profiles...)
	missing.Settings.Profiles[0].ProcessingDestination = ""
	missing.TextLayerFont = goregular.TTF
	missing.MaxTextLayerBytes = 1 << 20
	if candidate, err := New(missing); err == nil {
		candidate.Close()
		t.Fatal("undisclosed external PDF destination accepted")
	}
	options.TextLayerFont = append([]byte(nil), goregular.TTF...)
	options.MaxTextLayerBytes = 1 << 20
	provider, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	if provider.caps.Models[0].Version == baseline.caps.Models[0].Version {
		t.Fatal("renderer not pinned in model version")
	}
	options.TextLayerFont[0] = 255
	if _, err := ocrplugin.New(context.Background(), provider); err != nil {
		t.Fatal(err)
	}
	scoped := &access{input: fixture(t)}
	result, err := provider.Run(context.Background(), request(provider, ocrplugin.PDFTextLayer), scoped)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Pages) != 1 || result.Pages[0].Kind != ocrplugin.PDFTextLayer || len(scoped.outputs) != 1 || !scoped.outputs[0].closed {
		t.Fatal("missing closed text-layer artifact")
	}
	output := scoped.outputs[0].Bytes()
	if err = api.Validate(context.Background(), bytes.NewReader(output), model.NewStatelessConfiguration(), nil); err != nil {
		t.Fatal(err)
	}
	t.Run("independent-extraction", func(t *testing.T) {
		tool, err := exec.LookPath("pdftotext")
		if err != nil {
			t.Skip("requires independent Poppler extractor")
		}
		command := exec.Command(tool, "-", "-")
		command.Stdin = bytes.NewReader(output)
		data, err := command.Output()
		if err != nil || !strings.Contains(string(data), "Position") {
			t.Fatalf("extract: %q %v", data, err)
		}
	})
	options.TextLayerFont = gobold.TTF
	alternate, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer alternate.Close()
	if alternate.caps.Models[0].Version == provider.caps.Models[0].Version {
		t.Fatal("font change did not change pinned model version")
	}
	text.Store("日本語")
	rejected := &access{input: fixture(t)}
	if result, err := provider.Run(context.Background(), request(provider, ocrplugin.PDFTextLayer), rejected); err == nil || len(result.Pages) != 0 || len(rejected.outputs) != 0 {
		t.Fatal("missing font glyph must fail before allocating output")
	}
	if _, err := baseline.Run(context.Background(), request(baseline, ocrplugin.PDFTextLayer), &access{input: fixture(t)}); err == nil {
		t.Fatal("unconfigured renderer advertised PDF capability")
	}
}

func TestLargeLocalTextBudgetDoesNotRequireLargeSchemaStringLimit(t *testing.T) {
	for _, kind := range []ocrplugin.OutputKind{ocrplugin.Text, ocrplugin.PositionedText} {
		t.Run(string(kind), func(t *testing.T) {
			var calls atomic.Int32
			p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("invalid request JSON: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				schema := body["response_format"].(map[string]any)["json_schema"].(map[string]any)["schema"].(map[string]any)
				properties := schema["properties"].(map[string]any)
				if kind == ocrplugin.PositionedText {
					properties = properties["spans"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
				}
				text := properties["text"].(map[string]any)
				// Real endpoints may cap schema string lengths below the local artifact budget.
				if maximum, exists := text["maxLength"]; exists && maximum.(float64) > 1<<20 {
					http.Error(w, "schema string limit unsupported", http.StatusBadRequest)
					return
				}
				var content any = map[string]any{"text": "日本語 English"}
				if kind == ocrplugin.PositionedText {
					content = map[string]any{"spans": []any{map[string]any{"text": "日本語 English", "box": map[string]any{"x": 1, "y": 2, "width": 60, "height": 10}}}}
				}
				json.NewEncoder(w).Encode(completion(content))
			})
			p.options.MaxTextBytes = 16 << 20
			a := &access{input: fixture(t)}
			if _, err := p.Run(context.Background(), request(p, kind), a); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 || len(a.outputs) != 1 || !a.outputs[0].closed {
				t.Fatal("missing output or unexpected retry")
			}
		})
	}
}

func TestLocalTextByteLimitRemainsAuthoritative(t *testing.T) {
	for _, kind := range []ocrplugin.OutputKind{ocrplugin.Text, ocrplugin.PositionedText} {
		t.Run(string(kind), func(t *testing.T) {
			var calls atomic.Int32
			p, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var content any = map[string]any{"text": "日本語"}
				if kind == ocrplugin.PositionedText {
					content = map[string]any{"spans": []any{
						map[string]any{"text": "日", "box": map[string]any{"x": 1, "y": 2, "width": 20, "height": 10}},
						map[string]any{"text": "本", "box": map[string]any{"x": 21, "y": 2, "width": 20, "height": 10}},
					}}
				}
				json.NewEncoder(w).Encode(completion(content))
			})
			p.options.MaxTextBytes = 4
			a := &access{input: fixture(t)}
			_, err := p.Run(context.Background(), request(p, kind), a)
			if err == nil || len(a.outputs) != 0 || calls.Load() != 1 {
				t.Fatal("oversized UTF-8 text accepted, partially published or retried")
			}
		})
	}
}
