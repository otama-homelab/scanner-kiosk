//go:build linux

package tesseractapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/ocr/tesseract"
	"scanner-kiosk/pkg/ocrplugin"
)

const key = "fixture-service-key-00000000000000000000"
const modelVersion = "sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type engine struct {
	run   func(context.Context, ocrplugin.Request, ocrplugin.Access) (ocrplugin.Result, error)
	calls atomic.Int32
}

func (e *engine) Capabilities(context.Context) (ocrplugin.Capabilities, error) {
	return ocrplugin.Capabilities{APIVersion: 1, ID: "tesseract", Name: "Tesseract", Version: "1", Models: []ocrplugin.Model{{ID: "installed", Name: "Installed dictionaries", Version: modelVersion, Languages: []string{"ja", "en"}, QualityPresets: []string{"auto", "block"}, Outputs: []ocrplugin.OutputKind{ocrplugin.Text, ocrplugin.PDFTextLayer}, MaxPages: 5, MaxPagePixels: 10000}}}, nil
}
func (e *engine) Run(ctx context.Context, r ocrplugin.Request, a ocrplugin.Access) (ocrplugin.Result, error) {
	e.calls.Add(1)
	if e.run != nil {
		return e.run(ctx, r, a)
	}
	in, err := a.OpenInput(ctx, r.Pages[0].ArtifactID)
	if err != nil {
		return ocrplugin.Result{}, err
	}
	raw, err := io.ReadAll(in)
	in.Close()
	if err != nil || len(raw) == 0 {
		return ocrplugin.Result{}, errors.New("input")
	}
	out, err := a.CreateOutput(ctx, r.Output)
	if err != nil {
		return ocrplugin.Result{}, err
	}
	_, err = out.Write([]byte("日本語 English"))
	if err != nil {
		return ocrplugin.Result{}, err
	}
	if err = out.Close(); err != nil {
		return ocrplugin.Result{}, err
	}
	return ocrplugin.Result{ModelVersion: r.ModelVersion, Pages: []ocrplugin.PageResult{{PageID: r.Pages[0].ID, ArtifactID: out.Reference(), Kind: r.Output}}}, nil
}

type access struct {
	input   []byte
	outputs []*output
}
type output struct {
	bytes.Buffer
	closed bool
}

func (o *output) Close() error      { o.closed = true; return nil }
func (o *output) Reference() string { return "result-local" }
func (a *access) OpenInput(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(a.input)), nil
}
func (a *access) CreateOutput(context.Context, ocrplugin.OutputKind) (ocrplugin.ArtifactWriter, error) {
	o := &output{}
	a.outputs = append(a.outputs, o)
	return o, nil
}
func bitmap(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 40, 50))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func limits() Limits {
	return Limits{MaxInputBytes: 1 << 20, MaxResultBytes: 1 << 20, MaxPagePixels: 10000, MaxPages: 5, Timeout: time.Second}
}
func setup(t *testing.T, e *engine) (*Client, *httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(HandlerOptions{Provider: e, APIKey: key, WorkRoot: root, Limits: limits()})
	if err != nil {
		t.Fatal("handler initialization", err)
	}
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c, err := NewClient(context.Background(), ClientOptions{Settings: config.Tesseract{Endpoint: s.URL, AllowInsecureHTTP: true, ProcessingDestination: "Household OCR"}, APIKey: key, Limits: limits()})
	if err != nil {
		t.Fatal("client initialization", err)
	}
	t.Cleanup(c.Close)
	return c, s, root
}
func job(c *Client) ocrplugin.Request {
	caps, _ := c.Capabilities(context.Background())
	return ocrplugin.Request{JobID: "private-job-id", InputRevision: 3, ModelID: "installed", ModelVersion: caps.Models[0].Version, Languages: []string{"ja", "en"}, QualityPreset: "auto", Output: ocrplugin.Text, Pages: []ocrplugin.InputPage{{ID: "private-page-id", ArtifactID: "private-input-id", Width: 40, Height: 50, DPI: 300}}}
}

func TestAuthenticatedRoundTripKeepsPrivateIdentifiersLocal(t *testing.T) {
	e := &engine{run: func(ctx context.Context, r ocrplugin.Request, a ocrplugin.Access) (ocrplugin.Result, error) {
		if strings.Contains(r.JobID, "private") || strings.Contains(r.Pages[0].ID, "private") || strings.Contains(r.Pages[0].ArtifactID, "private") || r.InputRevision != 1 {
			t.Error("private identifiers sent to engine")
		}
		return (&engine{}).Run(ctx, r, a)
	}}
	c, _, root := setup(t, e)
	a := &access{input: bitmap(t)}
	r := job(c)
	r.Pages = append(r.Pages, ocrplugin.InputPage{ID: "second-page", ArtifactID: "second-input", Width: 40, Height: 50, DPI: 300})
	result, err := c.Run(context.Background(), r, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Pages) != 2 || result.Pages[0].PageID != r.Pages[0].ID || result.Pages[1].PageID != "second-page" || e.calls.Load() != 2 || len(a.outputs) != 2 || !a.outputs[0].closed || a.outputs[0].String() != "日本語 English" {
		t.Fatal("incorrect scoped result")
	}
	caps, _ := c.Capabilities(context.Background())
	if !caps.ExternalProcessing || caps.Models[0].ProcessingDestination != "Household OCR" {
		t.Fatal("missing external processing disclosure")
	}
	caps.Models[0].Languages[0] = "changed"
	again, _ := c.Capabilities(context.Background())
	if again.Models[0].Languages[0] == "changed" {
		t.Fatal("mutable capability snapshot")
	}
	files, _ := os.ReadDir(root)
	if len(files) != 0 {
		t.Fatal("service retained image or text")
	}
}

func TestUnauthorizedAndInvalidImagesNeverReachEngine(t *testing.T) {
	e := &engine{}
	_, s, root := setup(t, e)
	for _, tc := range []struct {
		token   string
		payload []byte
		want    int
	}{{"", bitmap(t), 401}, {"wrong", bitmap(t), 401}, {key, []byte("not an image"), 422}, {key, make([]byte, (1<<20)+1), 413}} {
		req, _ := http.NewRequest("POST", s.URL+"/v1/recognize", bytes.NewReader(tc.payload))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("Content-Type", "application/octet-stream")
		raw, _ := json.Marshal(wireRequest{ModelVersion: modelVersion, Languages: []string{"en"}, QualityPreset: "auto", Output: ocrplugin.Text, Width: 40, Height: 50, DPI: 300})
		req.Header.Set("X-OCR-Request", string(raw))
		res, err := s.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != tc.want || bytes.Contains(body, []byte(key)) {
			t.Fatalf("status=%d expected=%d", res.StatusCode, tc.want)
		}
	}
	if e.calls.Load() != 0 {
		t.Fatal("invalid request ran OCR")
	}
	files, _ := os.ReadDir(root)
	if len(files) != 0 {
		t.Fatal("failed request retained data")
	}
}

func TestRemoteFailureAndVersionMismatchDoNotPublishOrRetry(t *testing.T) {
	for _, mode := range []string{"redirect", "version", "oversize", "private-error"} {
		t.Run(mode, func(t *testing.T) {
			var posts atomic.Int32
			e := &engine{}
			c, s, _ := setup(t, e)
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					resp, err := s.Client().Get(s.URL + "/health/ready")
					if err != nil {
						t.Error(err)
					} else {
						resp.Body.Close()
					}
					caps, _ := e.Capabilities(r.Context())
					json.NewEncoder(w).Encode(caps)
					return
				}
				posts.Add(1)
				switch mode {
				case "redirect":
					http.Redirect(w, r, s.URL+"/v1/recognize", 307)
				case "version":
					w.Header().Set("X-OCR-Model-Version", "wrong")
					w.Write([]byte("text"))
				case "oversize":
					w.Header().Set("X-OCR-Model-Version", modelVersion)
					w.Header().Set("Content-Type", "text/plain; charset=utf-8")
					w.Write(make([]byte, (1<<20)+1))
				default:
					http.Error(w, "private engine secret", 500)
				}
			}))
			defer proxy.Close()
			c.Close()
			c, err := NewClient(context.Background(), ClientOptions{Settings: config.Tesseract{Endpoint: proxy.URL, AllowInsecureHTTP: true, ProcessingDestination: "Household OCR"}, APIKey: key, Limits: limits()})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			a := &access{input: bitmap(t)}
			_, err = c.Run(context.Background(), job(c), a)
			if err == nil || strings.Contains(err.Error(), "secret") || posts.Load() != 1 || e.calls.Load() != 0 || len(a.outputs) != 0 {
				t.Fatal("failure retried, leaked or published")
			}
		})
	}
}

func TestCancellationCleansServiceAndBusyDoesNotQueue(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	e := &engine{run: func(ctx context.Context, _ ocrplugin.Request, _ ocrplugin.Access) (ocrplugin.Result, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ocrplugin.Result{}, ctx.Err()
	}}
	c, _, root := setup(t, e)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.Run(ctx, job(c), &access{input: bitmap(t)}); done <- err }()
	<-started
	if _, err := c.Run(context.Background(), job(c), &access{input: bitmap(t)}); err == nil || e.calls.Load() != 1 {
		t.Fatal("busy request queued or ran")
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancel succeeded")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("service ignored disconnect")
	}
	deadline := time.Now().Add(time.Second)
	for {
		files, _ := os.ReadDir(root)
		if len(files) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancelled request retained data")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestUncertainNativeStopPoisonsServiceAndRetainsWorkspace(t *testing.T) {
	e := &engine{run: func(context.Context, ocrplugin.Request, ocrplugin.Access) (ocrplugin.Result, error) {
		return ocrplugin.Result{}, ocrplugin.ErrReconciliation
	}}
	c, s, root := setup(t, e)
	if _, err := c.Run(context.Background(), job(c), &access{input: bitmap(t)}); err == nil {
		t.Fatal("uncertain stop accepted")
	}
	res, err := s.Client().Get(s.URL + "/health/ready")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 503 {
		t.Fatal("uncertain service remained ready")
	}
	if _, err := c.Run(context.Background(), job(c), &access{input: bitmap(t)}); err == nil || e.calls.Load() != 1 {
		t.Fatal("uncertain process allowed another call")
	}
	files, _ := os.ReadDir(root)
	if len(files) != 1 {
		t.Fatal("uncertain workspace removed")
	}
	if _, err := NewHandler(HandlerOptions{Provider: e, APIKey: key, WorkRoot: root, Limits: limits()}); err == nil {
		t.Fatal("new service reused an unresolved workspace")
	}
}

func TestHandlerRequiresPrivateWorkRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHandler(HandlerOptions{Provider: &engine{}, APIKey: key, WorkRoot: root, Limits: limits()}); err == nil {
		t.Fatal("public workspace accepted")
	}
}

func nativeFixture(t *testing.T) (tesseract.Options, string) {
	t.Helper()
	root := t.TempDir()
	data := filepath.Join(root, "data")
	nativeWork := filepath.Join(root, "native")
	requestWork := filepath.Join(root, "requests")
	for _, dir := range []string{data, nativeWork, requestWork} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"eng", "jpn", "osd"} {
		if err := os.WriteFile(filepath.Join(data, name+".traineddata"), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(root, "tesseract")
	script := "#!/bin/sh\n[ -z \"$SCANNER_OCR_TESSERACT_API_KEY\" ] || exit 9\nprintf 'native 日本語 English' > \"$2.txt\"\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return tesseract.Options{Binary: binary, DataDir: data, WorkRoot: nativeWork, MaxInputBytes: 1 << 20, MaxResultBytes: 1 << 20, MaxPagePixels: 10000, MaxPages: 5, Timeout: time.Second, StopGrace: 50 * time.Millisecond}, requestWork
}
func TestNativeAdapterAcrossHTTPPreservesBoundaries(t *testing.T) {
	opts, requestWork := nativeFixture(t)
	t.Setenv("SCANNER_OCR_TESSERACT_API_KEY", key)
	p, err := tesseract.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(HandlerOptions{Provider: p, APIKey: key, WorkRoot: requestWork, Limits: limits()})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	defer s.Close()
	c, err := NewClient(context.Background(), ClientOptions{Settings: config.Tesseract{Endpoint: s.URL, AllowInsecureHTTP: true, ProcessingDestination: "Household OCR"}, APIKey: key, Limits: limits()})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := job(c)
	caps, _ := c.Capabilities(context.Background())
	r.ModelVersion = caps.Models[0].Version
	a := &access{input: bitmap(t)}
	if _, err = c.Run(context.Background(), r, a); err != nil {
		t.Fatal(err)
	}
	if len(a.outputs) != 1 || a.outputs[0].String() != "native 日本語 English" {
		t.Fatal("incorrect native result")
	}
	for _, dir := range []string{opts.WorkRoot, requestWork} {
		files, _ := os.ReadDir(dir)
		if len(files) != 0 {
			t.Fatal("native/API retained input, stderr or text")
		}
	}
}

func TestTerminalServiceRunsWithoutApplicationStateAndShutsDown(t *testing.T) {
	opts, _ := nativeFixture(t)
	root := filepath.Join(filepath.Dir(opts.WorkRoot), "service")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	values := map[string]string{"SCANNER_OCR_TESSERACT_API_KEY": key, "SCANNER_OCR_TESSERACT_PATH": opts.Binary, "SCANNER_OCR_TESSERACT_DATA_DIR": opts.DataDir, "SCANNER_OCR_TESSERACT_WORK_DIR": root, "SCANNER_OCR_TESSERACT_LISTEN_ADDR": address, "SCANNER_OCR_TESSERACT_TIMEOUT_SECONDS": "3"}
	lookup := func(k string) (string, bool) {
		if !strings.HasPrefix(k, "SCANNER_OCR_TESSERACT_") {
			t.Fatal("service read application state or identity settings")
		}
		v, ok := values[k]
		return v, ok
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, lookup); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("service did not stop")
		}
	})
	url := "http://" + address
	deadline := time.Now().Add(3 * time.Second)
	httpClient := &http.Client{Timeout: 100 * time.Millisecond}
	for {
		res, err := httpClient.Get(url + "/health/ready")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("service did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	c, err := NewClient(context.Background(), ClientOptions{Settings: config.Tesseract{Endpoint: url, AllowInsecureHTTP: true, ProcessingDestination: "Household OCR"}, APIKey: key, Limits: limits()})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := job(c)
	caps, _ := c.Capabilities(ctx)
	r.ModelVersion = caps.Models[0].Version
	a := &access{input: bitmap(t)}
	if _, err = c.Run(ctx, r, a); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("service shutdown timed out")
	}
	for _, name := range []string{"native", "requests"} {
		files, err := os.ReadDir(filepath.Join(root, name))
		if err != nil || len(files) != 0 {
			t.Fatal("service shutdown retained request files")
		}
	}
}

func TestMetadataIsStrictAndRevisionIsPinned(t *testing.T) {
	e := &engine{}
	_, server, root := setup(t, e)
	base := wireRequest{ModelVersion: modelVersion, Languages: []string{"en"}, QualityPreset: "auto", Output: ocrplugin.Text, Width: 40, Height: 50, DPI: 300}
	raw, _ := json.Marshal(base)
	for _, tc := range []struct {
		meta   string
		status int
	}{{strings.Replace(string(raw), modelVersion, "changed", 1), 409}, {strings.TrimSuffix(string(raw), "}") + `,"path":"/etc/shadow"}`, 422}, {string(raw) + ` {}`, 422}, {strings.Replace(string(raw), `"width":40`, `"width":41`, 1), 422}} {
		r, _ := http.NewRequest("POST", server.URL+"/v1/recognize", bytes.NewReader(bitmap(t)))
		r.Header.Set("Authorization", "Bearer "+key)
		r.Header.Set("Content-Type", "application/octet-stream")
		r.Header.Set("X-OCR-Request", tc.meta)
		res, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Fatalf("got=%d want=%d", res.StatusCode, tc.status)
		}
	}
	if e.calls.Load() != 0 {
		t.Fatal("invalid metadata ran engine")
	}
	files, _ := os.ReadDir(root)
	if len(files) != 0 {
		t.Fatal("rejected metadata retained inputs")
	}
}

func TestSelectionPinsEndpointAndDisclosureButAllowsKeyRotation(t *testing.T) {
	var posts atomic.Int32
	metadata := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			posts.Add(1)
			http.Error(w, "unexpected recognition", 500)
			return
		}
		caps, _ := (&engine{}).Capabilities(r.Context())
		json.NewEncoder(w).Encode(caps)
	})
	one := httptest.NewServer(metadata)
	defer one.Close()
	two := httptest.NewServer(metadata)
	defer two.Close()
	makeClient := func(endpoint, label, token string) *Client {
		t.Helper()
		c, err := NewClient(context.Background(), ClientOptions{Settings: config.Tesseract{Endpoint: endpoint, AllowInsecureHTTP: true, ProcessingDestination: label}, APIKey: config.Secret(token), Limits: limits()})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(c.Close)
		return c
	}
	c := makeClient(one.URL, "First destination", key)
	changedLabel := makeClient(one.URL, "Changed destination", key)
	changedEndpoint := makeClient(two.URL, "First destination", key)
	rotated := makeClient(one.URL, "First destination", "fixture-rotated-key-00000000000000000000")
	first, _ := c.Capabilities(context.Background())
	renamed, _ := changedLabel.Capabilities(context.Background())
	moved, _ := changedEndpoint.Capabilities(context.Background())
	renewed, _ := rotated.Capabilities(context.Background())
	if first.Models[0].Version == renamed.Models[0].Version || first.Models[0].Version == moved.Models[0].Version || first.Models[0].Version != renewed.Models[0].Version {
		t.Fatal("selection failed to pin processing destination or unnecessarily pinned a secret")
	}
	for _, next := range []*Client{changedLabel, changedEndpoint} {
		if _, err := next.Run(context.Background(), job(c), &access{input: bitmap(t)}); err == nil {
			t.Fatal("old job accepted changed destination")
		}
	}
	if posts.Load() != 0 {
		t.Fatal("old job sent an image to a changed destination")
	}
}

func TestServiceConfigurationIsBoundedAndSecretRedacted(t *testing.T) {
	values := map[string]string{"SCANNER_OCR_TESSERACT_API_KEY": key}
	lookup := func(k string) (string, bool) {
		if !strings.HasPrefix(k, "SCANNER_OCR_TESSERACT_") {
			t.Fatal("service read unrelated environment")
		}
		v, ok := values[k]
		return v, ok
	}
	c, err := loadService(lookup)
	if err != nil {
		t.Fatal(err)
	}
	if c.limits.MaxInputBytes != 200<<20 || c.limits.MaxPages != 10000 || c.address != ":8090" || !validKey(string(c.key)) {
		t.Fatal("unexpected service defaults")
	}
	for _, pair := range [][2]string{{"SCANNER_OCR_TESSERACT_API_KEY", "short"}, {"SCANNER_OCR_TESSERACT_ENDPOINT", "https://recursive.example"}, {"SCANNER_OCR_TESSERACT_MAX_INPUT_BYTES", "9999999999999"}, {"SCANNER_OCR_TESSERACT_MAX_RESULT_BYTES", "-1"}, {"SCANNER_OCR_TESSERACT_TIMEOUT_SECONDS", "0"}, {"SCANNER_OCR_TESSERACT_STOP_GRACE_SECONDS", "999999"}, {"SCANNER_OCR_TESSERACT_LISTEN_ADDR", ":0"}, {"SCANNER_OCR_TESSERACT_WORK_DIR", "../public"}} {
		values[pair[0]] = pair[1]
		if _, err := loadService(lookup); err == nil || strings.Contains(err.Error(), key) {
			t.Fatal("invalid service setting accepted or leaked")
		}
		delete(values, pair[0])
		values["SCANNER_OCR_TESSERACT_API_KEY"] = key
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

type largeAccess struct {
	*access
	prefix []byte
	size   int64
}

func (a *largeAccess) OpenInput(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(io.MultiReader(bytes.NewReader(a.prefix), io.LimitReader(zeroReader{}, a.size-int64(len(a.prefix))))), nil
}
func TestRemoteTesseractPreservesInputsLargerThanLLMDataURLLimit(t *testing.T) {
	const size = 70 << 20
	e := &engine{run: func(ctx context.Context, r ocrplugin.Request, a ocrplugin.Access) (ocrplugin.Result, error) {
		in, err := a.OpenInput(ctx, "input")
		if err != nil {
			return ocrplugin.Result{}, err
		}
		n, err := io.Copy(io.Discard, in)
		in.Close()
		if err != nil || n != size {
			t.Error("input was truncated")
		}
		out, err := a.CreateOutput(ctx, r.Output)
		if err != nil {
			return ocrplugin.Result{}, err
		}
		_, err = out.Write([]byte("accepted"))
		if err != nil {
			return ocrplugin.Result{}, err
		}
		if err = out.Close(); err != nil {
			return ocrplugin.Result{}, err
		}
		return ocrplugin.Result{ModelVersion: r.ModelVersion, Pages: []ocrplugin.PageResult{{PageID: "page", ArtifactID: out.Reference(), Kind: r.Output}}}, nil
	}}
	l := limits()
	l.MaxInputBytes = 80 << 20
	l.Timeout = 5 * time.Second
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(HandlerOptions{Provider: e, APIKey: key, WorkRoot: root, Limits: l})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	defer s.Close()
	c, err := NewClient(context.Background(), ClientOptions{Settings: config.Tesseract{Endpoint: s.URL, AllowInsecureHTTP: true, ProcessingDestination: "Household OCR"}, APIKey: key, Limits: l})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	a := &largeAccess{access: &access{}, prefix: bitmap(t), size: size}
	if _, err = c.Run(context.Background(), job(c), a); err != nil {
		t.Fatal(err)
	}
	if len(a.outputs) != 1 || a.outputs[0].String() != "accepted" {
		t.Fatal("large input failed")
	}
}
