package esclcapture

import (
	"bytes"
	"context"
	"image"
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
