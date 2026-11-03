package escl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fixtureUUID = "12abcdef-3456-789a-bcde-1234567890ab"

func caps(uuid string) string {
	return `<s:ScannerCapabilities xmlns:s="` + scanNS + `" xmlns:p="` + pwgNS + `"><p:Version>2.0</p:Version><s:UUID>` + uuid + `</s:UUID><s:SerialNumber>SERIAL_NOT_EXPORTED</s:SerialNumber><s:AdminURI>http://127.0.0.1:1/never-follow</s:AdminURI></s:ScannerCapabilities>`
}
func TestCapabilityIdentityCanonicalizesUUIDWithoutURLOrSerialFallback(t *testing.T) {
	want, ok := capabilityIdentity([]byte(caps(fixtureUUID)))
	if !ok || !strings.HasPrefix(want, "physical_") || len(want) != 73 {
		t.Fatal("missing opaque identity")
	}
	for _, uuid := range []string{strings.ToUpper(fixtureUUID), "urn:uuid:" + fixtureUUID, "URN:UUID:" + strings.ToUpper(fixtureUUID)} {
		got, ok := capabilityIdentity([]byte(caps(uuid)))
		if !ok || got != want {
			t.Fatal("noncanonical alias")
		}
	}
	for name, data := range map[string]string{
		"missing":   strings.ReplaceAll(caps(fixtureUUID), "<s:UUID>"+fixtureUUID+"</s:UUID>", ""),
		"duplicate": strings.Replace(caps(fixtureUUID), "</s:UUID>", "</s:UUID><s:UUID>"+fixtureUUID+"</s:UUID>", 1),
		"nested":    caps("<s:Value>" + fixtureUUID + "</s:Value>"), "nil": caps("00000000-0000-0000-0000-000000000000"), "max": caps("ffffffff-ffff-ffff-ffff-ffffffffffff"),
		"braces": caps("{" + fixtureUUID + "}"), "compact": caps(strings.ReplaceAll(fixtureUUID, "-", "")), "invalid hex": caps(strings.Replace(fixtureUUID, "a", "x", 1)),
		"wrong namespace": strings.ReplaceAll(caps(fixtureUUID), scanNS, "urn:wrong"), "wrong root": status("Idle", ""), "version": strings.ReplaceAll(caps(fixtureUUID), "<p:Version>2.0</p:Version>", ""),
		"dtd": `<!DOCTYPE x [<!ENTITY x "replacement">]>` + caps(fixtureUUID), "trailing": caps(fixtureUUID) + "junk", "second root": caps(fixtureUUID) + caps(fixtureUUID), "too deep": caps(strings.Repeat("<x>", 40) + strings.Repeat("</x>", 40)),
	} {
		t.Run(name, func(t *testing.T) {
			if id, ok := capabilityIdentity([]byte(data)); ok || id != "" {
				t.Fatal("ambiguous advertised identity accepted")
			}
		})
	}
}
func TestIdentityObserverFreshPinnedReadOnlyAndRedacted(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/eSCL/ScannerCapabilities" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Accept-Encoding") != "identity" || r.Header.Get("Cache-Control") != "no-cache, no-store" {
			t.Error("unexpected request")
		}
		fmt.Fprint(w, caps(fixtureUUID))
	}))
	defer server.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	o := observer(t, server.URL+"/eSCL/", time.Second)
	var previous IdentityObservation
	for range 2 {
		began := time.Now().UnixMilli()
		got, err := o.ObserveIdentity(context.Background(), deviceID)
		if err != nil || got.DeviceID != deviceID || got.ObservedAtMS < began || got.PhysicalID == "" {
			t.Fatal("identity observation failed", err)
		}
		if previous.PhysicalID != "" && got.PhysicalID != previous.PhysicalID {
			t.Fatal("identity not stable")
		}
		encoded, _ := json.Marshal(got)
		if string(encoded) != `"[redacted]"` || strings.Contains(fmt.Sprintf("%+v %#v", got, got), fixtureUUID) {
			t.Fatal("identity leaked")
		}
		previous = got
	}
	if calls.Load() != 2 {
		t.Fatal("cached/retried observation")
	}
	if got, err := o.ObserveIdentity(context.Background(), "unregistered_scanner"); err == nil || got.PhysicalID != "" {
		t.Fatal("unregistered identity fetched")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := o.ObserveIdentity(ctx, deviceID); err == nil || got.PhysicalID != "" {
		t.Fatal("cancelled observation returned identity")
	}
	if calls.Load() != 2 {
		t.Fatal("invalid request performed IO")
	}
}
func TestIdentityObserverRejectsNetworkCacheEncodingAndUntrustedTLS(t *testing.T) {
	for _, mode := range []string{"redirect", "cached", "gzip", "oversize", "chunked oversize", "missing uuid", "error", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch mode {
				case "redirect":
					http.Redirect(w, r, "/other", http.StatusFound)
				case "cached":
					w.Header().Set("Age", "2")
					fmt.Fprint(w, caps(fixtureUUID))
				case "gzip":
					w.Header().Set("Content-Encoding", "gzip")
					fmt.Fprint(w, caps(fixtureUUID))
				case "oversize":
					fmt.Fprint(w, strings.Repeat("x", 5000))
				case "chunked oversize":
					w.(http.Flusher).Flush()
					fmt.Fprint(w, strings.Repeat("x", 5000))
				case "missing uuid":
					fmt.Fprint(w, status("Idle", ""))
				case "error":
					w.WriteHeader(500)
				case "timeout":
					<-r.Context().Done()
				}
			}))
			defer server.Close()
			timeout := 5 * time.Second
			if mode == "timeout" {
				timeout = 20 * time.Millisecond
			}
			o := observer(t, server.URL+"/eSCL", timeout)
			if got, err := o.ObserveIdentity(context.Background(), deviceID); err == nil || got.PhysicalID != "" {
				t.Fatal("invalid observation accepted")
			}
			if calls.Load() > 1 || mode != "timeout" && calls.Load() != 1 {
				t.Fatal("redirect/retry issued extra request")
			}
		})
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, caps(fixtureUUID)) }))
	defer server.Close()
	o := observer(t, server.URL+"/eSCL", time.Second)
	if got, err := o.ObserveIdentity(context.Background(), deviceID); err == nil || got.PhysicalID != "" {
		t.Fatal("TLS bypass")
	}
}
