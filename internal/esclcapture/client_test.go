package esclcapture

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDirectESCLCapture(t *testing.T) {
	imageBytes := testPNG(t)
	var posts, gets, deletes atomic.Int64
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /eSCL/ScanJobs":
			posts.Add(1)
			body := new(bytes.Buffer)
			_, _ = body.ReadFrom(r.Body)
			requestBody = body.String()
			w.Header().Set("Location", "/eSCL/ScanJobs/job_capture_001")
			w.WriteHeader(http.StatusCreated)
		case "GET /eSCL/ScanJobs/job_capture_001/NextDocument":
			if r.Header.Get("Accept") != "image/jpeg" {
				t.Errorf("TS8130 requires JPEG negotiation, got %q", r.Header.Get("Accept"))
			}
			gets.Add(1)
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageBytes)
		case "DELETE /eSCL/ScanJobs/job_capture_001":
			deletes.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for _, tc := range []struct {
		intent string
		arm    string
		want   bool
	}{{"", "e1", false}, {"Document", "e2", true}} {
		t.Run(tc.arm, func(t *testing.T) {
			client, err := New(Options{Endpoint: server.URL + "/eSCL", Region: Region{X: 0, Y: 0, Width: 2480, Height: 3508}, Resolution: 300, Color: "rgb", Intent: tc.intent, Timeout: 5 * time.Second, MaxResponseBytes: 1 << 20, MaxImageBytes: 1 << 20, MaxPixels: 40_000_000})
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.Capture(context.Background())
			if err != nil || result.Arm != tc.arm || result.Width != 1 || result.Height != 1 || result.SHA256 == "" || !bytes.Equal(result.Image, imageBytes) {
				t.Fatalf("capture: %+v %v", result, err)
			}
			hasIntent := strings.Contains(requestBody, "<scan:Intent>"+tc.intent+"</scan:Intent>")
			if !strings.Contains(requestBody, "<pwg:DocumentFormat>image/jpeg</pwg:DocumentFormat>") {
				t.Errorf("TS8130 requires advertised JPEG format: %s", requestBody)
			}
			if hasIntent != tc.want || !strings.Contains(requestBody, `pwg:MustHonor="true"`) || !strings.Contains(requestBody, "<pwg:InputSource>Platen</pwg:InputSource>") {
				t.Fatalf("unexpected settings: %s", requestBody)
			}
		})
	}
	if posts.Load() != 2 || gets.Load() != 2 || deletes.Load() != 2 {
		t.Fatalf("request count posts=%d gets=%d deletes=%d", posts.Load(), gets.Load(), deletes.Load())
	}
}

func TestDirectESCLNoRetry(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client, err := New(Options{Endpoint: server.URL + "/eSCL", Region: Region{Width: 10, Height: 10}, Resolution: 300, Color: "gray", Timeout: 5 * time.Second, MaxResponseBytes: 4096, MaxImageBytes: 4096, MaxPixels: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Capture(context.Background()); err == nil {
		t.Fatal("failed capture accepted")
	}
	if calls.Load() != 1 {
		t.Fatalf("capture retried: %d", calls.Load())
	}
}

func TestDirectESCLCleanupFailureIdentifiesStage(t *testing.T) {
	imageBytes := testPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.Header().Set("Location", "/eSCL/ScanJobs/test")
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageBytes)
		case http.MethodDelete:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	client, err := New(Options{Endpoint: server.URL + "/eSCL", Region: Region{Width: 1, Height: 1}, Resolution: 300, Color: "rgb", Timeout: time.Second, MaxResponseBytes: 4096, MaxImageBytes: 4096, MaxPixels: 100})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Capture(context.Background())
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "delete job: HTTP 405") {
		t.Fatalf("missing bounded stage diagnostic: %v", err)
	}
}

func TestDirectESCLAlreadyRemovedJob(t *testing.T) {
	imageBytes := testPNG(t)
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.Method {
		case http.MethodPost:
			w.Header().Set("Location", "/eSCL/ScanJobs/test")
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageBytes)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := New(Options{Endpoint: server.URL + "/eSCL", Region: Region{Width: 1, Height: 1}, Resolution: 300, Color: "rgb", Timeout: time.Second, MaxResponseBytes: 4096, MaxImageBytes: 4096, MaxPixels: 100})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Capture(context.Background())
	if err != nil || !bytes.Equal(result.Image, imageBytes) || calls.Load() != 3 {
		t.Fatalf("completed image with absent job: %v, requests %d", err, calls.Load())
	}
}

func TestDirectESCLJPEGCapture(t *testing.T) {
	var original bytes.Buffer
	if err := jpeg.Encode(&original, image.NewRGBA(image.Rect(0, 0, 3, 2)), nil); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /eSCL/ScanJobs":
			w.Header().Set("Location", "/eSCL/ScanJobs/job_jpeg_001")
			w.WriteHeader(http.StatusCreated)
		case "GET /eSCL/ScanJobs/job_jpeg_001/NextDocument":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(original.Bytes())
		case "DELETE /eSCL/ScanJobs/job_jpeg_001":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := New(Options{Endpoint: server.URL + "/eSCL", Region: Region{Width: 3, Height: 2}, Resolution: 300, Color: "rgb", Timeout: 5 * time.Second, MaxResponseBytes: 1 << 20, MaxImageBytes: 1 << 20, MaxPixels: 100})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Capture(context.Background())
	if err != nil || result.MediaType != "image/jpeg" || result.Width != 3 || result.Height != 2 {
		t.Fatalf("capture: %+v %v", result, err)
	}
	decoded, err := png.Decode(bytes.NewReader(result.Image))
	if err != nil || decoded.Bounds().Dx() != 3 || decoded.Bounds().Dy() != 2 {
		t.Fatalf("normalized image: %v", err)
	}
}

func TestDirectESCLMalformedResponses(t *testing.T) {
	imageBytes := testPNG(t)
	for _, mode := range []string{"missing-location", "malformed-location", "wrong-type", "oversized", "bad-image"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					calls.Add(1)
					if mode == "missing-location" {
						w.WriteHeader(http.StatusCreated)
						return
					}
					location := "/eSCL/ScanJobs/job_bad_001"
					if mode == "malformed-location" {
						location = "http://user:secret@other/eSCL/ScanJobs/job_bad_001"
					}
					w.Header().Set("Location", location)
					w.WriteHeader(http.StatusCreated)
					return
				}
				if r.Method == http.MethodGet {
					w.Header().Set("Content-Type", "image/png")
					if mode == "wrong-type" {
						w.Header().Set("Content-Type", "image/jpeg")
					}
					if mode == "oversized" {
						_, _ = w.Write(make([]byte, 4097))
						return
					}
					if mode == "bad-image" {
						_, _ = w.Write([]byte("not png"))
						return
					}
					_, _ = w.Write(imageBytes)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			client, err := New(Options{Endpoint: server.URL + "/eSCL", Region: Region{Width: 10, Height: 10}, Resolution: 300, Color: "gray", Timeout: 5 * time.Second, MaxResponseBytes: 4096, MaxImageBytes: 4096, MaxPixels: 100})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = client.Capture(context.Background()); err == nil {
				t.Fatal("malformed response accepted")
			}
			if calls.Load() != 1 {
				t.Fatalf("malformed response retried: %d", calls.Load())
			}
		})
	}
}

func TestDirectESCLRejectsUnsafeInput(t *testing.T) {
	for _, endpoint := range []string{
		"ftp://scanner/eSCL", "http://user:secret@scanner/eSCL", "http://scanner/eSCL?q=1", "http://scanner/eSCL#fragment",
	} {
		if _, err := New(Options{Endpoint: endpoint, Region: Region{Width: 1, Height: 1}, Resolution: 300, Color: "gray", Timeout: 5 * time.Second, MaxResponseBytes: 4096, MaxImageBytes: 4096, MaxPixels: 100}); err == nil {
			t.Fatalf("unsafe endpoint accepted: %s", endpoint)
		}
	}
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer server.Close()
	client, newErr := New(Options{Endpoint: server.URL + "/eSCL", Region: Region{Width: 10, Height: 10}, Resolution: 300, Color: "gray", Timeout: 5 * time.Second, MaxResponseBytes: 4096, MaxImageBytes: 1 << 20, MaxPixels: 100})
	if newErr != nil {
		t.Fatal(newErr)
	}
	if _, err := client.Capture(context.Background()); err == nil {
		t.Fatal("redirect accepted")
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	bitmap := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var output bytes.Buffer
	if err := png.Encode(&output, bitmap); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
