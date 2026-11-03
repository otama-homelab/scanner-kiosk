//go:build linux

package scannerdriver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"scanner-kiosk/internal/scanner"
)

type verifyFunc func(context.Context, string, string) error

type renamedBackend struct {
	*backend
	id string
}

func (b *renamedBackend) DeviceID() string { return b.id }

func (f verifyFunc) VerifyIdentity(ctx context.Context, id, physical string) error {
	return f(ctx, id, physical)
}
func TestBoundDriverChecksIdentityBeforeInspectionCaptureAndAroundIdle(t *testing.T) {
	d, native, idle, work := fixture(t)
	defer d.Close(context.Background())
	work.Snapshot.PhysicalID = "physical_bound_fixture_01"
	work.Snapshot.HandoffID = "handoff_bound_fixture_01"
	calls := 0
	d.identity = verifyFunc(func(_ context.Context, id, physical string) error {
		calls++
		if id != deviceID || physical != work.Snapshot.PhysicalID {
			t.Fatal("wrong expected identity")
		}
		if calls == 2 && native.captures != 0 {
			t.Fatal("identity check after acquisition")
		}
		return nil
	})
	capturer, plan, err := d.Resolve(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = capturer.Capture(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err = d.ObserveIdle(context.Background(), deviceID, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if calls != 4 || native.captures != 1 || idle.calls != 1 {
		t.Fatal("missing identity boundary", calls, native.captures, idle.calls)
	}
}
func TestIdentityLossNeverCapturesOrAcceptsReplacementIdle(t *testing.T) {
	for _, phase := range []string{"resolve", "capture", "idle before", "idle after", "missing verifier", "downgrade"} {
		t.Run(phase, func(t *testing.T) {
			d, native, idle, work := fixture(t)
			work.Snapshot.PhysicalID = "physical_bound_fixture_01"
			work.Snapshot.HandoffID = "handoff_bound_fixture_01"
			calls := 0
			failAt := map[string]int{"resolve": 1, "capture": 2, "idle before": 3, "idle after": 4}[phase]
			d.identity = verifyFunc(func(context.Context, string, string) error {
				calls++
				if calls == failAt {
					return errors.Join(scanner.ErrUnresolved, ErrUnavailable)
				}
				return nil
			})
			if phase == "missing verifier" {
				d.identity = nil
			}
			capturer, plan, err := d.Resolve(context.Background(), work)
			if phase == "resolve" || phase == "missing verifier" {
				if !errors.Is(err, scanner.ErrUnresolved) || capturer != nil || native.selections != 0 {
					t.Fatal("identity loss inspected native backend", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if phase == "downgrade" {
					work.Job.ID = "job_bound_downgrade_001"
					work.Snapshot.PhysicalID = ""
					work.Snapshot.HandoffID = ""
					if _, _, err = d.Resolve(context.Background(), work); !errors.Is(err, scanner.ErrUnresolved) {
						t.Fatal("bound device downgraded", err)
					}
				} else {
					attempt, captureErr := capturer.Capture(context.Background(), plan)
					if phase == "capture" {
						if !errors.Is(captureErr, scanner.ErrUnresolved) || attempt != nil || native.captures != 0 {
							t.Fatal("changed identity started capture", captureErr)
						}
					} else if captureErr != nil {
						t.Fatal(captureErr)
					}
				}
			}
			if _, err = d.ObserveIdle(context.Background(), deviceID, time.Now().UnixMilli()); !errors.Is(err, scanner.ErrUnresolved) {
				t.Fatal("replacement idle released device", err)
			}
			if phase == "idle after" && idle.calls != 1 {
				t.Fatal("missing post-status identity check")
			}
			if phase != "idle after" && idle.calls != 0 {
				t.Fatal("idle trusted before identity")
			}
			if err = d.Close(context.Background()); !errors.Is(err, scanner.ErrUnresolved) {
				t.Fatal("unknown binding lost cleanup ownership", err)
			}
		})
	}
}
func TestUnavailableIdentityBeforeNativeStartDoesNotForgeUncertainty(t *testing.T) {
	d, native, _, work := fixture(t)
	defer d.Close(context.Background())
	work.Snapshot.PhysicalID = "physical_bound_fixture_01"
	work.Snapshot.HandoffID = "handoff_bound_fixture_01"
	calls := 0
	d.identity = verifyFunc(func(context.Context, string, string) error {
		calls++
		if calls == 2 {
			return ErrUnavailable
		}
		return nil
	})
	capturer, plan, err := d.Resolve(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := capturer.Capture(context.Background(), plan)
	if !errors.Is(err, ErrUnavailable) || errors.Is(err, scanner.ErrUnresolved) || attempt == nil || attempt.StoppedAtMS() <= 0 || native.captures != 0 {
		t.Fatal("lost known no-start evidence", err)
	}
	if _, err = d.ObserveIdle(context.Background(), deviceID, time.Now().UnixMilli()); err != nil {
		t.Fatal("fresh original identity/idle could not reconcile no-start", err)
	}
}
func TestConfiguredIdentityObservationUsesGuardAndExactRegisteredEndpoint(t *testing.T) {
	c, guard := configuredFixture(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/eSCL/ScannerCapabilities" {
			t.Error("unexpected request")
		}
		fmt.Fprint(w, `<s:ScannerCapabilities xmlns:s="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:p="http://www.pwg.org/schemas/2010/12/sm"><p:Version>2.0</p:Version><s:UUID>12abcdef-3456-789a-bcde-1234567890ab</s:UUID></s:ScannerCapabilities>`)
	}))
	defer server.Close()
	c.Scanners[0].Endpoint = server.URL + "/eSCL"
	session, err := Configure(context.Background(), c, guard)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	if calls != 0 {
		t.Fatal("constructor probed device")
	}
	observation, err := session.ObserveIdentity(context.Background(), c.Scanners[0].ID)
	if err != nil || observation.PhysicalID == "" {
		t.Fatal(err)
	}
	state := session.driver.devices[c.Scanners[0].ID]
	if err = state.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = session.driver.identity.VerifyIdentity(context.Background(), c.Scanners[0].ID, observation.PhysicalID); err != nil {
		t.Fatal(err)
	}
	if err = session.driver.identity.VerifyIdentity(context.Background(), c.Scanners[0].ID, "physical_changed_fixture"); !errors.Is(err, scanner.ErrUnresolved) {
		t.Fatal("wrong identity accepted")
	}
	state.release()
	guard.Close()
	previous := calls
	if _, err = session.ObserveIdentity(context.Background(), c.Scanners[0].ID); !errors.Is(err, scanner.ErrUnresolved) || calls != previous {
		t.Fatal("identity IO after guard loss")
	}
}

func TestConfiguredIdentityObservationPinsWorkspaceUntilClose(t *testing.T) {
	c, guard := configuredFixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `<s:ScannerCapabilities xmlns:s="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:p="http://www.pwg.org/schemas/2010/12/sm"><p:Version>2.0</p:Version><s:UUID>12abcdef-3456-789a-bcde-1234567890ab</s:UUID></s:ScannerCapabilities>`)
	}))
	defer server.Close()
	c.Scanners[0].Endpoint = server.URL + "/eSCL"
	session, err := Configure(context.Background(), c, guard)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := session.ObserveIdentity(ctx, c.Scanners[0].ID); done <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("probe did not start")
	}
	closing, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err = session.Close(closing)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("Close did not join identity probe", err)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = session.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = session.ObserveIdentity(context.Background(), c.Scanners[0].ID); !errors.Is(err, ErrClosed) {
		t.Fatal("closed identity observer still accessed workspace", err)
	}
}

func TestIdentityLossIsDeviceLocal(t *testing.T) {
	_, first, idle, work := fixture(t)
	_, other, _, _ := fixture(t)
	second := &renamedBackend{backend: other, id: "scanner_bound_second_01"}
	d, err := New(Options{BootID: bootID, Devices: []Device{{ID: deviceID, Backend: first}, {ID: second.id, Backend: second}}, Observer: idle, MaxBytes: 1 << 20, MaxPixels: 1000000, IdentityVerifier: verifyFunc(func(_ context.Context, id, physical string) error {
		if id == deviceID {
			return scanner.ErrUnresolved
		}
		return nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	work.Snapshot.PhysicalID = "physical_bound_first_01"
	work.Snapshot.HandoffID = "handoff_bound_fixture_01"
	if _, _, err = d.Resolve(context.Background(), work); !errors.Is(err, scanner.ErrUnresolved) {
		t.Fatal("lost first binding accepted")
	}
	work.Job.ID = "job_bound_second_001"
	work.Snapshot.DeviceID = second.id
	work.Snapshot.PhysicalID = "physical_bound_second_01"
	capturer, plan, err := d.Resolve(context.Background(), work)
	if err != nil {
		t.Fatal("unrelated device was blocked", err)
	}
	if _, err = capturer.Capture(context.Background(), plan); err != nil || other.captures != 1 || first.captures != 0 {
		t.Fatal("device-local capture isolation", err)
	}
	if err = d.Close(context.Background()); !errors.Is(err, scanner.ErrUnresolved) {
		t.Fatal("first binding uncertainty discarded")
	}
}
