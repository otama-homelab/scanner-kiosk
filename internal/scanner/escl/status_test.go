package escl

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const deviceID = "scanner_status_0001"

func status(state, jobs string) string {
	return `<s:ScannerStatus xmlns:s="` + scanNS + `" xmlns:p="` + pwgNS + `"><p:Version>2.0</p:Version><p:State>` + state + `</p:State>` + jobs + `</s:ScannerStatus>`
}
func job(state, extra string) string {
	return `<s:Jobs><s:JobInfo><p:JobUri>/eSCL/ScanJobs/fixture</p:JobUri><p:JobState>` + state + `</p:JobState>` + extra + `</s:JobInfo></s:Jobs>`
}
func TestStatusRejectsAmbiguousOrActiveWork(t *testing.T) {
	cases := map[string]string{
		"processing": status("Processing", ""), "testing": status("Testing", ""), "stopped": status("Stopped", ""), "unknown-state": status("Unknown", ""),
		"pending-job": status("Idle", job("Pending", "")), "held-job": status("Idle", job("PendingHeld", "")), "processing-job": status("Idle", job("Processing", "")), "unknown-job": status("Idle", job("Unknown", "")),
		"untransferred-image": status("Idle", job("Completed", "<p:ImagesToTransfer>1</p:ImagesToTransfer>")), "bad-count": status("Idle", job("Completed", "<p:ImagesToTransfer>-1</p:ImagesToTransfer>")),
		"duplicate-state": status("Idle", "<p:State>Idle</p:State>"), "duplicate-jobs": status("Idle", "<s:Jobs/><s:Jobs/>"), "duplicate-job-state": status("Idle", job("Completed", "<p:JobState>Completed</p:JobState>")),
		"missing-job-state": status("Idle", "<s:Jobs><s:JobInfo/></s:Jobs>"), "unknown-job-child": status("Idle", "<s:Jobs><s:Unknown/></s:Jobs>"),
		"nested-state": status("Idle", "<p:State><p:Value>Idle</p:Value></p:State>"), "wrong-namespace": strings.ReplaceAll(status("Idle", ""), pwgNS, "urn:wrong"),
		"wrong-root": strings.ReplaceAll(status("Idle", ""), "ScannerStatus", "Other"), "missing-version": strings.ReplaceAll(status("Idle", ""), "<p:Version>2.0</p:Version>", ""),
		"dtd": `<!DOCTYPE x [<!ENTITY x "Idle">]>` + status("Idle", ""), "entity": status("&notdefined;", ""), "truncated": "<s:ScannerStatus", "trailing": status("Idle", "") + "junk", "second-root": status("Idle", "") + status("Idle", ""),
		"too-deep": status("Idle", strings.Repeat("<x>", 40)+strings.Repeat("</x>", 40)), "too-many-nodes": status("Idle", strings.Repeat("<x/>", 2050)), "oversized": status("Idle", strings.Repeat(" ", 1<<20)),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if idleStatus([]byte(data)) {
				t.Fatal("ambiguous status accepted as idle")
			}
		})
	}
}
func TestStatusAcceptsNamespacesAndTerminalHistory(t *testing.T) {
	for _, state := range []string{"Completed", "Canceled", "Aborted"} {
		if !idleStatus([]byte(status("Idle", job(state, "<p:ImagesToTransfer>0</p:ImagesToTransfer>")))) {
			t.Fatal("terminal history rejected", state)
		}
	}
	if !idleStatus([]byte(status("Idle", ""))) || !idleStatus([]byte(strings.NewReplacer("<s:", "<scan2:", "</s:", "</scan2:", "xmlns:s=", "xmlns:scan2=").Replace(status("Idle", "<s:Jobs/>")))) {
		t.Fatal("prefix-independent idle status rejected")
	}
}
func observer(t *testing.T, endpoint string, timeout time.Duration) *Observer {
	t.Helper()
	o, err := New([]Device{{ID: deviceID, Endpoint: endpoint}}, Options{Timeout: timeout, MaxResponseBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func TestObserverFreshReadOnlyAndNoCredentials(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/eSCL/ScannerStatus" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Accept-Encoding") != "identity" || r.Header.Get("Cache-Control") != "no-cache, no-store" {
			t.Error("unexpected scanner request")
		}
		fmt.Fprint(w, status("Idle", job("Completed", "")))
	}))
	defer server.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	o := observer(t, server.URL+"/eSCL/", 5*time.Second)
	stopped := time.Now().UnixMilli()
	for range 2 {
		observed, err := o.ObserveIdle(context.Background(), deviceID, stopped)
		if err != nil || observed < stopped {
			t.Fatal("idle observation failed", err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("cached or retried observation")
	}
	if timestamp, err := o.ObserveIdle(context.Background(), "scanner_foreign_001", stopped); err == nil || timestamp != 0 {
		t.Fatal("foreign device resolved")
	}
	if timestamp, err := o.ObserveIdle(context.Background(), deviceID, time.Now().Add(time.Minute).UnixMilli()); err == nil || timestamp != 0 {
		t.Fatal("observation preceded local stop")
	}
	if calls.Load() != 2 {
		t.Fatal("invalid identity/stop sent a request")
	}
}
func TestObserverRejectsHTTPFailuresWithoutRedirectOrRetry(t *testing.T) {
	for _, mode := range []string{"busy", "redirect", "gzip", "large", "malformed", "active-job", "cached"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch mode {
				case "cached":
					w.Header().Set("Age", "60")
					fmt.Fprint(w, status("Idle", ""))
				case "busy":
					w.WriteHeader(503)
				case "redirect":
					http.Redirect(w, r, "/redirect-target", 302)
				case "gzip":
					w.Header().Set("Content-Encoding", "gzip")
					fmt.Fprint(w, status("Idle", ""))
				case "large":
					fmt.Fprint(w, strings.Repeat("x", 5000))
				case "malformed":
					fmt.Fprint(w, "broken xml")
				case "active-job":
					fmt.Fprint(w, status("Idle", job("Processing", "")))
				}
			}))
			defer server.Close()
			timestamp, err := observer(t, server.URL+"/eSCL", 5*time.Second).ObserveIdle(context.Background(), deviceID, time.Now().UnixMilli())
			if err == nil || timestamp != 0 || calls.Load() != 1 {
				t.Fatal("bad response accepted/followed/retried", err, calls.Load())
			}
		})
	}
}
func TestObserverCancellationAndTLSValidation(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		ready := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(ready); <-r.Context().Done() }))
		defer server.Close()
		o := observer(t, server.URL+"/eSCL", 5*time.Second)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := o.ObserveIdle(ctx, deviceID, time.Now().UnixMilli()); done <- err }()
		select {
		case <-ready:
		case err := <-done:
			t.Fatalf("status request ended before handler readiness: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("status handler never became ready")
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("status cancellation hung")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer server.Close()
		timestamp, err := observer(t, server.URL+"/eSCL", 30*time.Millisecond).ObserveIdle(context.Background(), deviceID, time.Now().UnixMilli())
		if err == nil || timestamp != 0 {
			t.Fatal("timeout accepted")
		}
	})
	t.Run("tls", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, status("Idle", "")) }))
		defer server.Close()
		timestamp, err := observer(t, server.URL+"/eSCL", 5*time.Second).ObserveIdle(context.Background(), deviceID, time.Now().UnixMilli())
		if err == nil || timestamp != 0 {
			t.Fatal("untrusted TLS accepted")
		}
	})
}
func TestObserverRejectsUnsafeRegistration(t *testing.T) {
	for _, endpoint := range []string{"file:///tmp/status", "http://user:secret@scanner/eSCL", "http://scanner/eSCL?token=x", "http://scanner/eSCL?", "http://scanner/eSCL#fragment", "http://scanner/eSCL\n", "http://scanner:99999/eSCL"} {
		if _, err := New([]Device{{ID: deviceID, Endpoint: endpoint}}, Options{Timeout: time.Second, MaxResponseBytes: 4096}); err == nil {
			t.Errorf("accepted %q", endpoint)
		}
	}
	if _, err := New([]Device{{ID: deviceID, Endpoint: "http://scanner/eSCL"}, {ID: deviceID, Endpoint: "http://other/eSCL"}}, Options{Timeout: time.Second, MaxResponseBytes: 4096}); err == nil {
		t.Fatal("duplicate identity accepted")
	}
}

func TestObserverCancellationWhileReadingBody(t *testing.T) {
	ready := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		fmt.Fprint(w, "<unfinished>")
		w.(http.Flusher).Flush()
		close(ready)
		<-r.Context().Done()
	}))
	defer server.Close()
	o := observer(t, server.URL+"/eSCL", 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := o.ObserveIdle(ctx, deviceID, time.Now().UnixMilli()); done <- err }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("body request ended before handler readiness: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("body handler never became ready")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("body cancellation lost", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("body cancellation hung")
	}
}
