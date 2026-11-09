package paperless

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPaperlessHealthContract(t *testing.T) {
	const routes = `{"documents":"https://untrusted.invalid/private-documents/","tasks":"https://untrusted.invalid/private-tasks/"}`
	for _, tc := range []struct {
		name, body, media, version, api string
		code                            int
		want                            HealthStatus
	}{
		{"healthy", routes, "application/json", "3.4.2", "10", 200, HealthHealthy},
		{"unauthorized", `{"detail":"synthetic-token private-content"}`, "application/json", "3.4.2", "10", 401, HealthUnauthorized},
		{"forbidden", "private-content", "text/html", "3.4.2", "10", 403, HealthForbidden},
		{"redirect", routes, "application/json", "3.4.2", "10", 302, HealthRedirect},
		{"server error", routes, "application/json", "3.4.2", "10", 503, HealthUnavailable},
		{"wrong media", routes, "text/html", "3.4.2", "10", 200, HealthIncompatible},
		{"wrong version", routes, "application/json", "3.4.3", "10", 200, HealthIncompatible},
		{"wrong api", routes, "application/json", "3.4.2", "9", 200, HealthIncompatible},
		{"no auth version", routes, "application/json", "", "", 200, HealthIncompatible},
		{"list schema", `[]`, "application/json", "3.4.2", "10", 200, HealthIncompatible},
		{"null", `null`, "application/json", "3.4.2", "10", 200, HealthIncompatible},
		{"empty schema", `{}`, "application/json", "3.4.2", "10", 200, HealthIncompatible},
		{"wrong field", `{"documents":42,"tasks":"private-content"}`, "application/json", "3.4.2", "10", 200, HealthIncompatible},
		{"trailing", routes + ` {}`, "application/json", "3.4.2", "10", 200, HealthIncompatible},
		{"oversize", `{"documents":"` + strings.Repeat("a", maxResponseBytes) + `","tasks":"x"}`, "application/json", "3.4.2", "10", 200, HealthIncompatible},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var escaped, posts atomic.Int64
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { escaped.Add(1) }))
			defer target.Close()
			client, calls := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts.Add(1)
				}
				body, _ := io.ReadAll(r.Body)
				if r.Method != "GET" || r.URL.Path != "/api/" || r.URL.RawQuery != "" || len(body) != 0 || r.ContentLength != 0 {
					t.Error("health carried authority/payload", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", tc.media)
				w.Header().Set("X-Version", tc.version)
				w.Header().Set("X-Api-Version", tc.api)
				w.Header().Set("Location", target.URL)
				w.WriteHeader(tc.code)
				fmt.Fprint(w, tc.body)
			})
			if calls.Load() != 0 {
				t.Fatal("construction probed remote")
			}
			before := time.Now()
			got := client.Health(context.Background())
			after := time.Now()
			if got.Status != tc.want || got.CheckedAt.Before(before) || got.CheckedAt.After(after) {
				t.Fatal("wrong bounded observation", got)
			}
			encoded, _ := json.Marshal(got)
			var fields map[string]any
			if json.Unmarshal(encoded, &fields) != nil || len(fields) != 2 {
				t.Fatal("extra health authority", string(encoded))
			}
			for _, private := range []string{"synthetic-token", "private-content", "untrusted.invalid", target.URL, client.base.String(), "confirmed", "accepted"} {
				if strings.Contains(string(encoded), private) {
					t.Fatal("remote details or delivery state leaked")
				}
			}
			if calls.Load() != 1 || posts.Load() != 0 || escaped.Load() != 0 {
				t.Fatal("health followed/replayed/uploaded", calls.Load(), posts.Load(), escaped.Load())
			}
			transport := client.client.Transport.(*http.Transport)
			if transport.Proxy != nil || client.client.Timeout != 15*time.Second || transport.ResponseHeaderTimeout != 10*time.Second || transport.MaxResponseHeaderBytes != 16<<10 {
				t.Fatal("lost direct transport bounds")
			}
		})
	}
	t.Run("timeout", func(t *testing.T) {
		client, calls := testClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if got := client.Health(ctx); got.Status != HealthTimeout {
			t.Fatal("timeout not distinguished", got)
		}
		if calls.Load() != 1 {
			t.Fatal("timeout replay", calls.Load())
		}
	})
	t.Run("body timeout", func(t *testing.T) {
		client, calls := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Version", "3.4.2")
			w.Header().Set("X-Api-Version", "10")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		})
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if got := client.Health(ctx); got.Status != HealthTimeout {
			t.Fatal("body timeout not distinguished", got)
		}
		if calls.Load() != 1 {
			t.Fatal("body timeout replay", calls.Load())
		}
	})
	t.Run("base path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/paperless/api/" {
				t.Error("lost fixed base path")
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Version", "3.4.2")
			w.Header().Set("X-Api-Version", "10")
			fmt.Fprint(w, routes)
		}))
		defer server.Close()
		client, err := New(testOptions(server.URL + "/paperless/"))
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if client.Health(context.Background()).Status != HealthHealthy {
			t.Fatal("base path health failed")
		}
	})
}

func TestPaperlessHealthHasNoUploadAuthority(t *testing.T) {
	var roots, metadata, posts atomic.Int64
	expected := sha256.Sum256([]byte("synthetic-PDF"))
	client, calls := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
		}
		switch r.URL.Path {
		case "/api/":
			roots.Add(1)
			w.Header().Set("X-Version", "3.4.2")
			w.Header().Set("X-Api-Version", "10")
			fmt.Fprint(w, `{"documents":"ignored","tasks":"ignored"}`)
		case "/api/documents/42/metadata/":
			metadata.Add(1)
			fmt.Fprintf(w, `{"original_checksum":"%x"}`, expected)
		default:
			t.Error("health requested content/list/upload", r.URL.Path)
		}
	})
	if err := client.Metadata(context.Background(), 42, expected); err != nil {
		t.Fatal(err)
	}
	if roots.Load() != 0 || metadata.Load() != 1 {
		t.Fatal("metadata implicitly probed health")
	}
	beforeID, beforeFingerprint := client.DestinationID(), client.ConfigFingerprint()
	if result := client.Health(context.Background()); result.Status != HealthHealthy {
		t.Fatal(result)
	}
	if roots.Load() != 1 || metadata.Load() != 1 || posts.Load() != 0 || calls.Load() != 2 || client.DestinationID() != beforeID || client.ConfigFingerprint() != beforeFingerprint {
		t.Fatal("health acquired upload/reconciliation authority")
	}
}

const testTask = "12345678-1234-4234-8234-123456789abc"

var testFilename = "delivery_" + strings.Repeat("a", 48) + ".pdf"

func testOptions(base string) Options {
	return Options{BaseURL: base, Token: "synthetic-token", DestinationID: "test-destination", ConfigFingerprint: strings.Repeat("a", 64), AllowHTTP: true}
}
func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Accept") != APIVersion || r.Header.Get("Authorization") != "Token synthetic-token" {
			t.Error("missing API9/authentication header")
		}
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	client, err := New(testOptions(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client, &calls
}
func contentFree(t *testing.T, err error) {
	t.Helper()
	if err != ErrUnavailable {
		t.Fatal("expected content-free unavailable", err)
	}
}

func TestPaperlessAPI9Contract(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, body, media string
		status            int
		valid             bool
	}{
		{"uuid", fmt.Sprintf("%q", testTask), "application/json", 200, true},
		{"object", `{"task_id":"` + testTask + `","secret":"remote-private-content"}`, "application/json", 200, false},
		{"html", "<html>remote-private-content</html>", "text/html", 200, false},
		{"trailing", fmt.Sprintf("%q {}", testTask), "application/json", 200, false},
		{"invalid uuid", `"not-a-uuid"`, "application/json", 200, false},
		{"nil uuid", `"00000000-0000-0000-0000-000000000000"`, "application/json", 200, false},
		{"null", "null", "application/json", 200, false},
		{"oversize", `"` + strings.Repeat("a", maxResponseBytes) + `"`, "application/json", 200, false},
		{"unexpected status", fmt.Sprintf("%q", testTask), "application/json", 202, false},
		{"unauthorized", `{"detail":"synthetic-token remote-private-content"}`, "application/json", 401, false},
	} {
		t.Run("upload/"+tc.name, func(t *testing.T) {
			client, calls := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/api/documents/post_document/" {
					t.Error("wrong upload endpoint")
				}
				w.Header().Set("Content-Type", tc.media)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			task, err := client.Upload(ctx, testFilename, 4, strings.NewReader("%PDF"))
			if tc.valid {
				if err != nil || task != testTask {
					t.Fatal(task, err)
				}
			} else {
				contentFree(t, err)
			}
			if calls.Load() != 1 {
				t.Fatal("unexpected upload replay", calls.Load())
			}
		})
	}
	taskJSON := func(status, filename, related string) string {
		return fmt.Sprintf(`{"task_id":%q,"task_file_name":%q,"status":%q,"related_document":%s}`, testTask, filename, status, related)
	}
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"pending", "[" + taskJSON("PENDING", testFilename, "null") + "]", true},
		{"started", "[" + taskJSON("STARTED", testFilename, "null") + "]", true},
		{"success", "[" + taskJSON("SUCCESS", testFilename, "42") + "]", true},
		{"failure", "[" + taskJSON("FAILURE", testFilename, "null") + "]", true},
		{"revoked", "[" + taskJSON("REVOKED", testFilename, "null") + "]", true},
		{"api10", `{"count":1,"next":"https://attacker.invalid/","results":[` + taskJSON("SUCCESS", testFilename, "42") + `]}`, false},
		{"zero", "[]", false},
		{"two", "[" + taskJSON("SUCCESS", testFilename, "42") + "," + taskJSON("SUCCESS", testFilename, "42") + "]", false},
		{"foreign marker", "[" + taskJSON("SUCCESS", "foreign-document.pdf", "42") + "]", false},
		{"foreign uuid", strings.Replace("["+taskJSON("SUCCESS", testFilename, "42")+"]", testTask, "22345678-1234-4234-8234-123456789abc", 1), false},
		{"unknown status", "[" + taskJSON("FUTURE", testFilename, "42") + "]", false},
		{"missing document", "[" + taskJSON("SUCCESS", testFilename, "null") + "]", false},
		{"string document", "[" + taskJSON("SUCCESS", testFilename, `"42"`) + "]", false},
		{"zero document", "[" + taskJSON("SUCCESS", testFilename, "0") + "]", false},
		{"negative document", "[" + taskJSON("SUCCESS", testFilename, "-1") + "]", false},
		{"fraction document", "[" + taskJSON("SUCCESS", testFilename, "1.5") + "]", false},
		{"freeform success only", `[{"task_id":"` + testTask + `","task_file_name":"` + testFilename + `","result":"Success. New document id 42 created"}]`, false},
		{"oversize", `[{"result":"` + strings.Repeat("a", maxResponseBytes) + `"}]`, false},
		{"trailing", "[" + taskJSON("SUCCESS", testFilename, "42") + "] null", false},
	} {
		t.Run("task/"+tc.name, func(t *testing.T) {
			client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/tasks/" || r.URL.RawQuery != "task_id="+testTask {
					t.Error("task lookup not exact")
				}
				fmt.Fprint(w, tc.body)
			})
			_, err := client.Task(ctx, testTask, testFilename)
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				contentFree(t, err)
			}
		})
	}
	expected := sha256.Sum256([]byte("%PDF"))
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"sha256", fmt.Sprintf(`{"original_checksum":"%x"}`, expected), true},
		{"wrong sha256", `{"original_checksum":"` + strings.Repeat("0", 64) + `"}`, false},
		{"md5", `{"original_checksum":"` + strings.Repeat("a", 32) + `"}`, false},
		{"missing", `{"archive_checksum":"` + strings.Repeat("a", 64) + `"}`, false},
		{"null", `{"original_checksum":null}`, false},
	} {
		t.Run("metadata/"+tc.name, func(t *testing.T) {
			client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/documents/42/metadata/" {
					t.Error("wrong metadata endpoint")
				}
				fmt.Fprint(w, tc.body)
			})
			err := client.Metadata(ctx, 42, expected)
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				contentFree(t, err)
			}
		})
	}
	t.Run("fixed endpoint", func(t *testing.T) {
		for _, base := range []string{"https://example.invalid", "https://example.invalid/", "https://example.invalid/tenant/paperless", "https://example.invalid/tenant/paperless/"} {
			client, err := New(testOptions(base))
			if err != nil {
				t.Fatal("valid administrator URL rejected", base, err)
			}
			client.Close()
		}
		for _, base := range []string{"https://example.invalid/path%00", "https://example.invalid/path%0A", "https://example.invalid:"} {
			if client, err := New(testOptions(base)); err == nil {
				client.Close()
				t.Error("invalid decoded path/port accepted", base)
			}
		}
		for _, base := range []string{"https://user:password@example.invalid/", "https://example.invalid/?x=1", "https://example.invalid/#x", "https://example.invalid/?", "https://example.invalid/#", "https://example.invalid/\rpath", "https://example.invalid/\tpath", "https://example.invalid/a/../b", "https://example.invalid/%2fother", "file:///tmp/paperless", "//example.invalid/path", "http://example.invalid:0", "https://example.invalid:65536"} {
			o := testOptions(base)
			if c, err := New(o); err == nil {
				c.Close()
				t.Error("unsafe URL accepted", base)
			}
		}
		o := testOptions("http://example.invalid")
		o.AllowHTTP = false
		if c, err := New(o); err == nil {
			c.Close()
			t.Error("implicit HTTP allowed")
		}
		client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, "%q", testTask) })
		client.base.Path = "/tenant/paperless/"
		if client.endpoint("tasks/") != client.base.Scheme+"://"+client.base.Host+"/tenant/paperless/api/tasks/" {
			t.Fatal("administrator base path lost")
		}
	})
}

type inspectTransport struct {
	t       *testing.T
	next    http.RoundTripper
	uploads atomic.Int64
}

func (i *inspectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == "POST" {
		i.uploads.Add(1)
		if r.GetBody != nil || r.Header.Get("Idempotency-Key") != "" || r.Header.Get("X-Idempotency-Key") != "" {
			i.t.Error("upload became replayable")
		}
		if r.ContentLength < 4 {
			i.t.Error("unbounded upload length")
		}
	}
	return i.next.RoundTrip(r)
}

func TestPaperlessUploadIsNonReplayable(t *testing.T) {
	ctx := context.Background()
	t.Run("proxy redirect and reused connection", func(t *testing.T) {
		var escaped atomic.Int64
		trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { escaped.Add(1); w.WriteHeader(500) }))
		defer trap.Close()
		t.Setenv("HTTP_PROXY", trap.URL)
		t.Setenv("HTTPS_PROXY", trap.URL)
		t.Setenv("ALL_PROXY", trap.URL)
		t.Setenv("NO_PROXY", "")
		client, calls := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			if strings.HasSuffix(r.URL.Path, "post_document/") {
				w.Header().Set("Location", trap.URL)
				w.WriteHeader(307)
			} else {
				fmt.Fprint(w, "[]")
			}
		})
		actualAddress := client.base.Host
		client.base.Host = "paperless.invalid:12345"
		transport := client.client.Transport.(*http.Transport)
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, actualAddress)
		}
		if transport.Proxy != nil {
			t.Fatal("environment proxy was enabled")
		}
		spy := &inspectTransport{t: t, next: transport}
		client.client.Transport = spy
		// Prime an idle connection before the non-replayable upload.
		_, _ = client.Task(ctx, testTask, testFilename)
		_, err := client.Upload(ctx, testFilename, 4, strings.NewReader("%PDF"))
		contentFree(t, err)
		if calls.Load() != 2 || spy.uploads.Load() != 1 || escaped.Load() != 0 {
			t.Fatal("redirect/proxy/replay escaped", calls.Load(), spy.uploads.Load(), escaped.Load())
		}
	})
	t.Run("accepted then socket response lost", func(t *testing.T) {
		client, calls := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
		})
		spy := &inspectTransport{t: t, next: client.client.Transport}
		client.client.Transport = spy
		_, err := client.Upload(ctx, testFilename, 4, strings.NewReader("%PDF"))
		contentFree(t, err)
		if calls.Load() != 1 || spy.uploads.Load() != 1 {
			t.Fatal("response loss replayed upload", calls.Load(), spy.uploads.Load())
		}
	})
}
