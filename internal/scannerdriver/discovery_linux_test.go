//go:build linux

package scannerdriver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannerdiscovery"
	"strings"
	"testing"
)

func TestDiscoveryAddsWithoutNativeCaptureAndDeduplicatesPhysicalID(t *testing.T) {
	c, guard := configuredFixture(t)
	c.Scanners = nil
	c.ScannerDiscovery = true
	session, err := Configure(context.Background(), c, guard)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "text/xml")
		w.Write([]byte(`<scan:ScannerCapabilities xmlns:scan="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:pwg="http://www.pwg.org/schemas/2010/12/sm"><pwg:Version>2.0</pwg:Version><scan:UUID>11111111-2222-3333-4444-555555555555</scan:UUID></scan:ScannerCapabilities>`))
	}))
	defer server.Close()
	for index, path := range []string{"/eSCL", "/other/eSCL"} {
		added, err := session.Discover(context.Background(), scannerdiscovery.Candidate{Name: "Canon", Endpoint: server.URL + path})
		if err != nil || added != (index == 0) {
			t.Fatal(added, err)
		}
	}
	if len(session.IDs()) != 1 || len(session.Devices()) != 1 || session.Devices()[0].Name != "Canon" {
		t.Fatal("bad registry")
	}
	for _, request := range requests {
		if !strings.HasPrefix(request, "GET ") || !strings.HasSuffix(request, "/ScannerCapabilities") {
			t.Fatal("discovery acquired", request)
		}
	}
	if err := session.before(context.Background()); err != nil {
		t.Fatal("registered workspace invalid", err)
	}
}

func TestDiscoveryKeepsManualIdentityAndLabel(t *testing.T) {
	c, guard := configuredFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<scan:ScannerCapabilities xmlns:scan="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:pwg="http://www.pwg.org/schemas/2010/12/sm"><pwg:Version>2.0</pwg:Version><scan:UUID>11111111-2222-3333-4444-555555555555</scan:UUID></scan:ScannerCapabilities>`))
	}))
	defer server.Close()
	c.Scanners[0].Endpoint = server.URL + "/manual/eSCL"
	session, err := Configure(context.Background(), c, guard)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	added, err := session.Discover(context.Background(), scannerdiscovery.Candidate{Name: "Advertisement", Endpoint: server.URL + "/eSCL"})
	if err != nil || added || len(session.IDs()) != 1 || session.Devices()[0].Name != c.Scanners[0].Name || session.IDs()[0] != c.Scanners[0].ID {
		t.Fatal("manual registration replaced", added, err, session.Devices())
	}
}

func TestRegistrationKeepsDeviceGatesAndGlobalJobUse(t *testing.T) {
	d, native, _, work := fixture(t)
	defer d.Close(context.Background())
	original := d.device(deviceID)
	if _, _, err := d.Resolve(context.Background(), work); err != nil {
		t.Fatal(err)
	}
	second := "scanner_registered_002"
	if err := d.register(Device{ID: second, Backend: &namedBackend{native, second}}, ""); err != nil {
		t.Fatal(err)
	}
	if d.device(deviceID) != original {
		t.Fatal("existing gate replaced")
	}
	work.Snapshot.DeviceID = second
	if _, _, err := d.Resolve(context.Background(), work); !errors.Is(err, scanner.ErrUnresolved) || native.selections != 1 {
		t.Fatal("registration reset shared job fence", err)
	}
	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := d.register(Device{ID: "scanner_after_close", Backend: &namedBackend{native, "scanner_after_close"}}, ""); err == nil {
		t.Fatal("registered after close")
	}
}
