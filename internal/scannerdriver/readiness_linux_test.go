//go:build linux

package scannerdriver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestApplicationReadinessPinsIdentityAndRejectsBusyUnknownAndLostGuard(t *testing.T) {
	c, guard := configuredFixture(t)
	var uuid atomic.Value
	uuid.Store("12abcdef-3456-789a-bcde-1234567890ab")
	var busy atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/eSCL/ScannerCapabilities" {
			fmt.Fprintf(w, `<s:ScannerCapabilities xmlns:s="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:p="http://www.pwg.org/schemas/2010/12/sm"><p:Version>2.0</p:Version><s:UUID>%s</s:UUID></s:ScannerCapabilities>`, uuid.Load())
			return
		}
		state := "Idle"
		if busy.Load() {
			state = "Processing"
		}
		fmt.Fprintf(w, `<s:ScannerStatus xmlns:s="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:p="http://www.pwg.org/schemas/2010/12/sm"><p:Version>2.0</p:Version><p:State>%s</p:State></s:ScannerStatus>`, state)
	}))
	defer server.Close()
	c.Scanners[0].Endpoint = server.URL + "/eSCL"
	s, err := Configure(context.Background(), c, guard)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	id := c.Scanners[0].ID
	p, err := s.ObserveApplicationReadiness(context.Background(), id, 0, 1)
	if err != nil || p.PhysicalID == "" || p.HandoffID != guard.ID() || p.OwnershipConfirmedAtMS > p.LocalStoppedAtMS || p.LocalStoppedAtMS > p.IdleObservedAtMS {
		t.Fatal("missing bound readiness", err)
	}
	busy.Store(true)
	if _, err = s.ObserveApplicationReadiness(context.Background(), id, 1, 1); err == nil {
		t.Fatal("busy accepted")
	}
	busy.Store(false)
	uuid.Store("22abcdef-3456-789a-bcde-1234567890ab")
	if _, err = s.ObserveApplicationReadiness(context.Background(), id, 1, 1); err == nil {
		t.Fatal("replacement hardware accepted")
	}
	uuid.Store("12abcdef-3456-789a-bcde-1234567890ab")
	if _, err = s.ObserveApplicationReadiness(context.Background(), id, 1, 1); err == nil {
		t.Fatal("uncertainty erased by idle")
	}
	guard.Close()
	if _, err = s.ObserveApplicationReadiness(context.Background(), id, 1, 1); err == nil {
		t.Fatal("guard loss accepted")
	}
}
